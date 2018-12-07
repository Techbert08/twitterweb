package main

import (
	"context"
	"strconv"
	"time"

	"cloud.google.com/go/datastore"
	"github.com/dghubble/go-twitter/twitter"
	"google.golang.org/api/iterator"
)

// newDatastoreClient returns a client good for connecting to the Cloud Datastore.
func newDatastoreClient(ctx context.Context) (*datastore.Client, error) {
	client, err := datastore.NewClient(ctx, ProjectID)
	if err != nil {
		return nil, err
	}
	return client, nil
}

// getUserKey returns the common ancestor key of the given string user ID.
func getUserKey(ctx context.Context, userID string) *datastore.Key {
	return datastore.NameKey("User", userID, nil)
}

// getApplicationUser retrieves the given user.  Returns nil if that user does not exist.
func getApplicationUser(ctx context.Context, client *datastore.Client, userID string) (*User, error) {
	var user User
	if err := client.Get(ctx, getUserKey(ctx, userID), &user); err != nil {
		if err == datastore.ErrNoSuchEntity {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}

// saveApplicationUser persists the newly authorized user ot the backing table.
func saveApplicationUser(ctx context.Context, client *datastore.Client, userID string, accessToken string, accessSecret string) error {
	user := &User{
		LoginID:      userID,
		AccessToken:  accessToken,
		AccessSecret: accessSecret,
	}
	if _, err := client.Put(ctx, getUserKey(ctx, userID), user); err != nil {
		return err
	}
	return nil
}

// getRootHandles gets a slice of all jobs being fetched by the provided user.
func getRootHandles(ctx context.Context, client *datastore.Client, userID string) ([]*RootHandle, error) {
	q := datastore.NewQuery("RootHandle").Ancestor(getUserKey(ctx, userID)).Order("Node.ScreenName")
	var rootHandles []*RootHandle
	if _, err := client.GetAll(ctx, q, &rootHandles); err != nil {
		return nil, err
	}
	return rootHandles, nil
}

// getRootHandleFromString gets a single root handle identified by twitterIDString owned by u.
// twitterIDString is a stringified version of the int64 ID.
func getRootHandleFromString(ctx context.Context, client *datastore.Client, userID string, twitterIDString string) (*RootHandle, error) {
	twitterID, err := strconv.ParseInt(twitterIDString, 10, 64)
	if err != nil {
		return nil, err
	}
	rootKey := datastore.IDKey("RootHandle", twitterID, getUserKey(ctx, userID))
	var rootHandle RootHandle
	if err := client.Get(ctx, rootKey, &rootHandle); err != nil {
		return nil, err
	}
	return &rootHandle, nil
}

// getRootHandlePerUser gets a single unfinished root handle for each user in the system.
func getRootHandlePerUser(ctx context.Context, client *datastore.Client) ([]*RootHandle, error) {
	q := datastore.NewQuery("RootHandle").Filter("Node.Done =", false)
	var rootHandles []*RootHandle
	if _, err := client.GetAll(ctx, q, &rootHandles); err != nil {
		return nil, err
	}
	// Map from user to single root handle
	m := make(map[string]*RootHandle)
	for _, handle := range rootHandles {
		m[handle.LoginID] = handle
	}
	var flattened []*RootHandle
	for _, handle := range m {
		flattened = append(flattened, handle)
	}
	return flattened, nil
}

// getUnfinishedRootHandle gets a single root handle to work on.  Returns nil with no error if there
// is no work to do.
func getUnfinishedRootHandle(ctx context.Context, client *datastore.Client, userID string) (*RootHandle, error) {
	q := datastore.NewQuery("RootHandle").Ancestor(getUserKey(ctx, userID)).Filter("Node.Done =", false).Limit(1)
	var rootHandles []*RootHandle
	if _, err := client.GetAll(ctx, q, &rootHandles); err != nil {
		return nil, err
	}
	if len(rootHandles) != 1 {
		return nil, nil
	}
	return rootHandles[0], nil
}

// getUnfinishedFetchedHandle gets a single user to "hydrate". Returns nil if there is no work to do.
func getUnfinishedFetchHandle(ctx context.Context, client *datastore.Client, userID string, rootHandle *RootHandle) (*FetchedHandle, error) {
	q := datastore.NewQuery("FetchedHandle").Ancestor(getUserKey(ctx, userID)).Filter("ParentID =", rootHandle.Node.TwitterID).Filter("Node.Done =", false).Limit(1)
	var fetchedHandles []*FetchedHandle
	if _, err := client.GetAll(ctx, q, &fetchedHandles); err != nil {
		return nil, err
	}
	if len(fetchedHandles) != 1 {
		return nil, nil
	}
	return fetchedHandles[0], nil
}

// deleteRootHandle deletes a handle and its component pieces from the datastore.
func deleteRootHandle(ctx context.Context, client *datastore.Client, rootHandle *RootHandle) error {
	var fetchedKeys []*datastore.Key
	var cursor *datastore.Cursor = nil
	for {
		q := datastore.NewQuery("FetchedHandle").Ancestor(getUserKey(ctx, rootHandle.LoginID)).Filter("ParentID =", rootHandle.Node.TwitterID).KeysOnly().Limit(1000)
		if cursor != nil {
			q = q.Start(*cursor)
		}
		child, _ := context.WithTimeout(ctx, 30*time.Second)
		t := client.Run(child, q)
		fetched := 0
		for {
			key, err := t.Next(nil)
			if err == iterator.Done {
				break
			}
			if err != nil {
				return err
			}
			fetchedKeys = append(fetchedKeys, key)
			fetched++
		}
		if fetched < 1000 {
			break
		}
		c, err := t.Cursor()
		if err != nil {
			return err
		}
		cursor = &c
	}
	// Must delete in blocks of 500 or less.
	for len(fetchedKeys) > 0 {
		var keysPage []*datastore.Key
		if len(fetchedKeys) > 500 {
			keysPage = fetchedKeys[:500]
			fetchedKeys = fetchedKeys[500:]
		} else {
			keysPage = fetchedKeys
			fetchedKeys = nil
		}
		if err := client.DeleteMulti(ctx, keysPage); err != nil {
			return err
		}
	}
	return client.Delete(ctx, datastore.IDKey("RootHandle", rootHandle.Node.TwitterID, getUserKey(ctx, rootHandle.LoginID)))
}

// getDoneJobs gets a slice of all completed fetch jobs for this user and graph root.
func getDoneJobs(ctx context.Context, client *datastore.Client, rootHandle *RootHandle) ([]*FetchedHandle, error) {
	var fetchedHandles []*FetchedHandle
	var cursor *datastore.Cursor = nil
	for {
		q := datastore.NewQuery("FetchedHandle").Ancestor(getUserKey(ctx, rootHandle.LoginID)).Filter("ParentID =", rootHandle.Node.TwitterID).Filter("Node.Done =", true).Limit(1000)
		if cursor != nil {
			q = q.Start(*cursor)
		}
		child, _ := context.WithTimeout(ctx, 30*time.Second)
		t := client.Run(child, q)
		fetched := 0
		for {
			var h FetchedHandle
			_, err := t.Next(&h)
			if err == iterator.Done {
				break
			}
			if err != nil {
				return nil, err
			}
			fetchedHandles = append(fetchedHandles, &h)
			fetched++
		}
		if fetched < 1000 {
			break
		}
		c, err := t.Cursor()
		if err != nil {
			return nil, err
		}
		cursor = &c
	}
	return fetchedHandles, nil
}

// countEnqueued counts the number of fetch tasks enqueued underneath the given rootHandle.
func countEnqueued(ctx context.Context, client *datastore.Client, rootHandle *RootHandle) (int, error) {
	q := datastore.NewQuery("FetchedHandle").Ancestor(getUserKey(ctx, rootHandle.LoginID)).Filter("ParentID =", rootHandle.Node.TwitterID)
	return client.Count(ctx, q)
}

// countRemaining counts the number of fetch tasks remaining to be done.
func countRemaining(ctx context.Context, client *datastore.Client, rootHandle *RootHandle) (int, error) {
	q := datastore.NewQuery("FetchedHandle").Ancestor(getUserKey(ctx, rootHandle.LoginID)).Filter("ParentID =", rootHandle.Node.TwitterID).Filter("Node.Done =", false)
	return client.Count(ctx, q)
}

// saveRootHandle saves the given handle back to the datastore.
func saveRootHandle(ctx context.Context, client *datastore.Client, rootHandle *RootHandle) error {
	rootKey := datastore.IDKey("RootHandle", rootHandle.Node.TwitterID, getUserKey(ctx, rootHandle.LoginID))
	if _, err := client.Put(ctx, rootKey, rootHandle); err != nil {
		return err
	}
	return nil
}

// newFetchedHandles saves the slice of TwitterIDs as fetch handles to the datastore.
func newFetchedHandles(ctx context.Context, client *datastore.Client, userID string, relationship string, parentID int64, twitterIDs []int64) error {
	var keys []*datastore.Key
	var fetched []*FetchedHandle
	for _, twitterID := range twitterIDs {
		fetched = append(fetched, &FetchedHandle{
			ParentID: parentID,
			Node: GephiNode{
				TwitterID:    twitterID,
				Relationship: relationship,
			},
		})
		keys = append(keys, datastore.IDKey("FetchedHandle", twitterID, getUserKey(ctx, userID)))
	}
	// AppEngine only supports 500 entities at a time, but if the entities are too big
	// it can fail anyway.  Do 50 at a time.
	for len(fetched) > 0 {
		var fetchedPage []*FetchedHandle
		var keysPage []*datastore.Key
		if len(fetched) > 50 {
			fetchedPage = fetched[:50]
			keysPage = keys[:50]
			fetched = fetched[50:]
			keys = keys[50:]
		} else {
			fetchedPage = fetched
			keysPage = keys
			fetched = nil
			keys = nil
		}
		if _, err := client.PutMulti(ctx, keysPage, fetchedPage); err != nil {
			return err
		}
	}
	return nil
}

// hydrateHandle inflates the given FetchedHandle with data from the twitter User object
func hydrateHandle(ctx context.Context, client *datastore.Client, userID string, twitterUser *twitter.User, fetchedHandle *FetchedHandle) error {
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
	fetchedKey := datastore.IDKey("FetchedHandle", fetchedHandle.Node.TwitterID, getUserKey(ctx, userID))
	if _, err := client.Put(ctx, fetchedKey, fetchedHandle); err != nil {
		return err
	}
	return nil
}

// newRootHandle records the fetched Twitter user to the datastore.
func newRootHandle(ctx context.Context, client *datastore.Client, userID string, user *twitter.User) error {
	rootHandle := &RootHandle{
		LoginID: userID,
		Node: GephiNode{
			TwitterID:       user.ID,
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
	}
	if len(rootHandle.Node.Description) > 500 {
		rootHandle.Node.Description = rootHandle.Node.Description[:500]
	}
	key := datastore.IDKey("RootHandle", user.ID, getUserKey(ctx, userID))
	if _, err := client.Put(ctx, key, rootHandle); err != nil {
		return err
	}
	return nil
}
