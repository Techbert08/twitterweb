package main

import (
	"context"

	"cloud.google.com/go/firestore"
	firebase "firebase.google.com/go"
	"github.com/dghubble/go-twitter/twitter"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

// newDatastoreClient returns a client good for connecting to the Cloud Firestore.
func newFirestoreClient(ctx context.Context) (*firestore.Client, error) {
	// Use the application default credentials
	conf := &firebase.Config{ProjectID: ProjectID}
	app, err := firebase.NewApp(ctx, conf)
	if err != nil {
		return nil, err
	}
	client, err := app.Firestore(ctx)
	if err != nil {
		return nil, err
	}
	return client, nil
}

// getUserRef returns the document reference of the given string user ID.
func getUserRef(client *firestore.Client, userID string) *firestore.DocumentRef {
	return client.Collection("User").Doc(userID)
}

// getApplicationUser retrieves the given user.  Returns nil if that user does not exist.
func getApplicationUser(ctx context.Context, client *firestore.Client, userID string) (*User, error) {
	docsnap, err := getUserRef(client, userID).Get(ctx)
	if err != nil {
		if grpc.Code(err) == codes.NotFound {
			return nil, nil
		}
		return nil, err
	}
	var user User
	if err := docsnap.DataTo(&user); err != nil {
		return nil, err
	}
	return &user, nil
}

// saveApplicationUser persists a newly authorized user to the backing table.
func saveApplicationUser(ctx context.Context, client *firestore.Client, userID string, name string, accessToken string, accessSecret string) error {
	user := &User{
		LoginID:      userID,
		ScreenName:   name,
		AccessToken:  accessToken,
		AccessSecret: accessSecret,
	}
	if _, err := getUserRef(client, userID).Set(ctx, user); err != nil {
		return err
	}
	return nil
}

// getRootHandleFromString gets a single root handle identified by twitterID and owned by userID.
func getRootHandleFromString(ctx context.Context, client *firestore.Client, userID string, twitterID string) (*RootHandle, error) {
	docsnap, err := getUserRef(client, userID).Collection("RootHandle").Doc(twitterID).Get(ctx)
	if err != nil {
		return nil, err
	}
	var rootHandle RootHandle
	if err := docsnap.DataTo(&rootHandle); err != nil {
		return nil, err
	}
	return &rootHandle, nil
}

// getRootHandleTransaction reloads a single root handle within a Transaction.
func getRootHandleTransaction(ctx context.Context, client *firestore.Client, tx *firestore.Transaction, handle *RootHandle) (*RootHandle, error) {
	docsnap, err := tx.Get(getUserRef(client, handle.LoginID).Collection("RootHandle").Doc(handle.Node.TwitterID))
	if err != nil {
		return nil, err
	}
	var rootHandle RootHandle
	if err := docsnap.DataTo(&rootHandle); err != nil {
		return nil, err
	}
	return &rootHandle, nil
}

// updateRootHandleStatus overwrites just the Status of the given RootHandle in the database.
// This feeds an error back to the frontend.
func updateRootHandleStatus(ctx context.Context, client *firestore.Client, msg string, handle *RootHandle) error {
	ref := getUserRef(client, handle.LoginID).Collection("RootHandle").Doc(handle.Node.TwitterID)
	if _, err := ref.Update(ctx, []firestore.Update{{Path: "Status", Value: msg}}); err != nil {
		return err
	}
	return nil
}

// getRootHandlePerUser gets at most one unfinished root handle for each user in the system.
func getRootHandlePerUser(ctx context.Context, client *firestore.Client) ([]*RootHandle, error) {
	iter := client.Collection("User").Documents(ctx)
	defer iter.Stop()
	var rootHandles []*RootHandle
	for {
		userDoc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		rootHandle, err := getUnfinishedRootHandle(ctx, client, userDoc.Ref.ID)
		if err != nil {
			return nil, err
		}
		if rootHandle == nil {
			continue
		}
		rootHandles = append(rootHandles, rootHandle)
	}
	return rootHandles, nil
}

// getUnfinishedRootHandle gets a single root handle to work on for the passed in user.
// Returns nil with no error if there is no work to do for this user.
func getUnfinishedRootHandle(ctx context.Context, client *firestore.Client, userID string) (*RootHandle, error) {
	iter := getUserRef(client, userID).Collection("RootHandle").Where("Node.Done", "==", false).Limit(1).Documents(ctx)
	defer iter.Stop()
	handleDoc, err := iter.Next()
	if err == iterator.Done {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var rootHandle RootHandle
	if err := handleDoc.DataTo(&rootHandle); err != nil {
		return nil, err
	}
	return &rootHandle, nil
}

// getUnfinishedFetchedHandle gets a single user to "hydrate". Returns nil if there is no work to do.
func getUnfinishedFetchHandle(ctx context.Context, client *firestore.Client, tx *firestore.Transaction, userID string, rootHandle *RootHandle) (*FetchedHandle, error) {
	iter := tx.Documents(getUserRef(client, userID).Collection("RootHandle").Doc(rootHandle.Node.TwitterID).Collection("FetchedHandle").Where("Node.Done", "==", false).Limit(1))
	defer iter.Stop()
	handleDoc, err := iter.Next()
	if err == iterator.Done {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var fetchedHandle FetchedHandle
	if err := handleDoc.DataTo(&fetchedHandle); err != nil {
		return nil, err
	}
	return &fetchedHandle, nil
}

// deleteRootHandle deletes a handle and its component pieces from the firestore.
func deleteRootHandle(ctx context.Context, client *firestore.Client, rootHandle *RootHandle) error {
	batch := client.Batch()
	numBatched := 0
	rootRef := getUserRef(client, rootHandle.LoginID).Collection("RootHandle").Doc(rootHandle.Node.TwitterID)
	iter := rootRef.Collection("FetchedHandle").DocumentRefs(ctx)
	for {
		fetchedDoc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return err
		}
		batch.Delete(fetchedDoc)
		numBatched++
		if numBatched >= 500 {
			if _, err := batch.Commit(ctx); err != nil {
				return err
			}
			batch = client.Batch()
			numBatched = 0
		}
	}
	if numBatched > 0 {
		if _, err := batch.Commit(ctx); err != nil {
			return err
		}
	}
	if _, err := rootRef.Delete(ctx); err != nil {
		return err
	}
	return nil
}

// getDoneJobs gets the slice of all completed fetch jobs for this user and root handle.
func getDoneJobs(ctx context.Context, client *firestore.Client, rootHandle *RootHandle) ([]*FetchedHandle, error) {
	var fetchedHandles []*FetchedHandle
	iter := getUserRef(client, rootHandle.LoginID).Collection("RootHandle").Doc(rootHandle.Node.TwitterID).Collection("FetchedHandle").Where("Node.Done", "==", true).Documents(ctx)
	defer iter.Stop()
	for {
		fetchedDoc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		var fetchedHandle FetchedHandle
		if err := fetchedDoc.DataTo(&fetchedHandle); err != nil {
			return nil, err
		}
		fetchedHandles = append(fetchedHandles, &fetchedHandle)
	}
	return fetchedHandles, nil
}

// saveRootHandle saves the given handle back to the firestore.
func saveRootHandle(ctx context.Context, client *firestore.Client, rootHandle *RootHandle) error {
	docRef := getUserRef(client, rootHandle.LoginID).Collection("RootHandle").Doc(rootHandle.Node.TwitterID)
	if _, err := docRef.Set(ctx, rootHandle); err != nil {
		return err
	}
	return nil
}

// saveRootHandleTransaction saves the given handle back to the firestore.
func saveRootHandleTransaction(ctx context.Context, client *firestore.Client, tx *firestore.Transaction, rootHandle *RootHandle) error {
	docRef := getUserRef(client, rootHandle.LoginID).Collection("RootHandle").Doc(rootHandle.Node.TwitterID)
	if err := tx.Set(docRef, rootHandle); err != nil {
		return err
	}
	return nil
}

// newFetchedHandles saves the slice of TwitterIDs as fetch handles to the firestore.
func newFetchedHandles(ctx context.Context, client *firestore.Client, userID string, relationship string, parentID string, twitterIDs []string) error {
	handleCollection := getUserRef(client, userID).Collection("RootHandle").Doc(parentID).Collection("FetchedHandle")
	batch := client.Batch()
	numBatched := 0
	// Firestore only handles writes up to 500 documents.
	for _, twitterID := range twitterIDs {
		fetched := &FetchedHandle{
			ParentID: parentID,
			Node: GephiNode{
				TwitterID:    twitterID,
				Relationship: relationship,
			},
		}
		batch.Set(handleCollection.Doc(twitterID), fetched)
		numBatched++
		if numBatched >= 500 {
			if _, err := batch.Commit(ctx); err != nil {
				return err
			}
			batch = client.Batch()
			numBatched = 0
		}
	}
	if numBatched > 0 {
		if _, err := batch.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}

// hydrateHandle inflates the given FetchedHandle with data from the twitter User object
func hydrateHandle(ctx context.Context, client *firestore.Client, tx *firestore.Transaction, userID string, twitterUser *twitter.User, fetchedHandle *FetchedHandle) error {
	fetchedHandle.Node.FriendsCount = twitterUser.FriendsCount
	fetchedHandle.Node.FollowersCount = twitterUser.FollowersCount
	fetchedHandle.Node.ScreenName = twitterUser.ScreenName
	fetchedHandle.Node.Done = true
	fetchedHandle.Node.ProfileURL = twitterUser.URL
	fetchedHandle.Node.Description = twitterUser.Description
	if len(fetchedHandle.Node.Description) > 500 {
		fetchedHandle.Node.Description = fetchedHandle.Node.Description[:500]
	}
	fetchedHandle.Node.ProfileImageURL = twitterUser.ProfileImageURL
	ref := getUserRef(client, userID).Collection("RootHandle").Doc(fetchedHandle.ParentID).Collection("FetchedHandle").Doc(fetchedHandle.Node.TwitterID)
	if err := tx.Set(ref, fetchedHandle); err != nil {
		return err
	}
	return nil
}

// newRootHandle records the fetched Twitter user to the firestore as a new graph root to be expanded.
// Fails if the handle is already being fetched.
func newRootHandle(ctx context.Context, client *firestore.Client, userID string, user *twitter.User) error {
	rootHandle := &RootHandle{
		LoginID: userID,
		Node: GephiNode{
			TwitterID:       user.IDStr,
			ScreenName:      user.ScreenName,
			Relationship:    "Root",
			FollowersCount:  user.FollowersCount,
			FriendsCount:    user.FriendsCount,
			Done:            false,
			ProfileURL:      user.URL,
			Description:     user.Description,
			ProfileImageURL: user.ProfileImageURLHttps,
		},
		FollowersCursor: -1,
		FriendsCursor:   -1,
		Status:          "Preparing to fetch",
		Remaining:       -1,
		PrepareGraph:    false,
	}
	if len(rootHandle.Node.Description) > 500 {
		rootHandle.Node.Description = rootHandle.Node.Description[:500]
	}
	ref := getUserRef(client, userID).Collection("RootHandle").Doc(user.IDStr)
	if _, err := ref.Create(ctx, rootHandle); err != nil {
		return err
	}
	return nil
}
