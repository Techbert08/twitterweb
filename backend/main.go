package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"cloud.google.com/go/storage"
	firebase "firebase.google.com/go"
	"github.com/dghubble/go-twitter/twitter"
)

// workerPrefix is the URL component that prefixes a URL that will fetch data for a user.
const workerPrefix = "/worker/"

// updateUserPrefix is the URL of the handler that updates a user with Twitter credentials.
const updateUserPrefix = "/updateUser"

// addHandlePrefix enqueues a new Handle for fetching.
const addHandlePrefix = "/addHandle"

//deleteHandlePrefix handles the cancellation and deletion of a fetch task.
const deleteHandlePrefix = "/deleteHandle"

// User represents a single user of the system.  The Access fields
// represent Twitter OAuth credentials, and LoginID ties the struct
// back to a Firebase user.
type User struct {
	AccessToken  string
	AccessSecret string
	LoginID      string
	ScreenName   string
}

// GephiNode is a Gephi node in the graph, containing its identity,
// relationship to the root, and edges.
type GephiNode struct {
	TwitterID       string
	ScreenName      string
	Relationship    string
	FriendsCount    int
	FollowersCount  int
	FriendIDs       []string
	FollowerIDs     []string
	Done            bool
	ProfileURL      string
	Description     string
	ProfileImageURL string
}

// RootHandle is a top level handle to fetch.  All of its friends and
// followers will eventually be added as FetchedHandles linking back
// to this.
type RootHandle struct {
	LoginID         string
	Node            GephiNode
	FollowersCursor int64
	FriendsCursor   int64
	Status          string
	Remaining       int
	PrepareGraph    bool
}

// FetchedHandle holds a friend or follower of a RootHandle.
type FetchedHandle struct {
	ParentID string
	Node     GephiNode
}

// main registers the handlers for this web application.
func main() {
    http.HandleFunc(workerPrefix, workerHandler)
	http.HandleFunc(updateUserPrefix, updateUserHandler)
	http.HandleFunc(addHandlePrefix, addHandleHandler)
	http.HandleFunc(deleteHandlePrefix, deleteHandleHandler)
	http.HandleFunc("/", indexHandler)
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
		log.Printf("Defaulting to port %s", port)
	}

	log.Printf("Listening on port %s", port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%s", port), nil))
}

// enqueueHandle uses the connected Twitter client to enqueue a request for the handle to be fetched.
// It will use the credentials of loginID to do this.  The TwitterID of the fetched user is returned.
func enqueueHandle(ctx context.Context, client *twitter.Client, dataClient *firestore.Client, loginID string, handle string) (string, error) {
	user, err := getTwitterUserByName(client, handle)
	if err != nil {
		return "", err
	}
	if err := newRootHandle(ctx, dataClient, loginID, user); err != nil {
		return "", err
	}
	if err != nil {
		return "", err
	}
	return user.IDStr, nil
}

// runTick will advance the state machine one step for the requested Twitter handle.
func runTick(ctx context.Context, client *twitter.Client, dataClient *firestore.Client, loginID string, rootHandle *RootHandle) (string, error) {
	if rootHandle.Node.Done {
		return "", fmt.Errorf("User was already done: %v", rootHandle.Node.TwitterID)
	}
	if rootHandle.PrepareGraph {
		config := &firebase.Config{
			StorageBucket: ProjectID + ".appspot.com",
		}
		app, err := firebase.NewApp(ctx, config)
		if err != nil {
			return "", err
		}
		storageClient, err := app.Storage(ctx)
		if err != nil {
			return "", err
		}
		bucket, err := storageClient.DefaultBucket()
		if err != nil {
			return "", err
		}
		fetchedHandles, err := getDoneJobs(ctx, dataClient, rootHandle)
		if err != nil {
			return "", fmt.Errorf("error getting handles: %v", err)
		}
		obj := bucket.Object("graphs/" + rootHandle.LoginID + "/" + rootHandle.Node.TwitterID)
		content := buildGephiFile(rootHandle, fetchedHandles)
		writer := obj.NewWriter(ctx)
		_, err = writer.Write(content)
		if err != nil {
			closeErr := writer.Close()
			return "", fmt.Errorf("error writing %v (onClose: %v)", err, closeErr)
		}
		err = writer.Close()
		if err != nil {
			return "", err
		}
		_, err = obj.Update(ctx, storage.ObjectAttrsToUpdate{
          ContentDisposition: fmt.Sprintf("Attachment; filename=%v.gml", rootHandle.Node.ScreenName),
        })
        if err != nil {
			return "", err
		}
		// Clear the message to empty the UI since it will be replaced with the Download link.
		rootHandle.Status = ""
		rootHandle.PrepareGraph = false
		rootHandle.Node.Done = true
		if err := saveRootHandle(ctx, dataClient, rootHandle); err != nil {
			return "", err
		}
		return "Graph built", nil
	}
	if rootHandle.FollowersCursor != 0 {
		addedIDs, nextCursor, err := addFollowersPage(client, &rootHandle.Node, rootHandle.FollowersCursor)
		if err != nil {
			return "", err
		}
		rootHandle.FollowersCursor = nextCursor
		if err := newFetchedHandles(ctx, dataClient, loginID, "Follower", rootHandle.Node.TwitterID, addedIDs); err != nil {
			return "", err
		}
		msg := fmt.Sprintf("Fetched %v follower IDs", len(addedIDs))
		rootHandle.Status = msg
		if err := saveRootHandle(ctx, dataClient, rootHandle); err != nil {
			return "", err
		}
		return msg, nil
	}
	if rootHandle.FriendsCursor != 0 {
		addedIDs, nextCursor, err := addFriendsPage(client, &rootHandle.Node, rootHandle.FriendsCursor)
		if err != nil {
			return "", err
		}
		rootHandle.FriendsCursor = nextCursor
		if err := newFetchedHandles(ctx, dataClient, loginID, "Friend", rootHandle.Node.TwitterID, addedIDs); err != nil {
			return "", err
		}
		msg := fmt.Sprintf("Fetched %v friend IDs", len(addedIDs))
		rootHandle.Status = msg
		if err := saveRootHandle(ctx, dataClient, rootHandle); err != nil {
			return "", err
		}
		return msg, nil
	}
	if rootHandle.Remaining == -1 {
		unique := make(map[string]bool)
		for _, friend := range rootHandle.Node.FriendIDs {
			unique[friend] = true
		}
		for _, follower := range rootHandle.Node.FollowerIDs {
			unique[follower] = true
		}
		msg := fmt.Sprintf("Enqueued %v handles", len(unique))
		rootHandle.Status = msg
		rootHandle.Remaining = len(unique)
		if err := saveRootHandle(ctx, dataClient, rootHandle); err != nil {
			return "", err
		}
		return msg, nil
	}
	tMsg := ""
	tErr := dataClient.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		// Reload the root handle inside the transaction to keep the count accurate in case two updates
		// are in flight.
		rootHandle, err := getRootHandleTransaction(ctx, dataClient, tx, rootHandle)
		fetchedHandle, err := getUnfinishedFetchHandle(ctx, dataClient, tx, loginID, rootHandle)
		if err != nil {
			return err
		}
		if fetchedHandle == nil {
			rootHandle.PrepareGraph = true
			tMsg = "Preparing graph"
			rootHandle.Status = tMsg
			rootHandle.Remaining = 0
			if err := saveRootHandleTransaction(ctx, dataClient, tx, rootHandle); err != nil {
				return err
			}
			return nil
		}
		twitterUser, err := getTwitterUser(client, fetchedHandle.Node.TwitterID)
		if err != nil {
			return err
		}
		if twitterUser.FriendsCount != 0 && twitterUser.FriendsCount <= 5000 {
			_, _, err := addFriendsPage(client, &fetchedHandle.Node, -1)
			if err != nil {
				return err
			}
		}
		if twitterUser.FollowersCount != 0 && twitterUser.FollowersCount <= 5000 {
			_, _, err := addFollowersPage(client, &fetchedHandle.Node, -1)
			if err != nil {
				return err
			}
		}
		if err := hydrateHandle(ctx, dataClient, tx, loginID, twitterUser, fetchedHandle); err != nil {
			return err
		}
		tMsg = fmt.Sprintf("Fetched %v", fetchedHandle.Node.ScreenName)
		rootHandle.Status = tMsg
		rootHandle.Remaining--
		if err := saveRootHandleTransaction(ctx, dataClient, tx, rootHandle); err != nil {
			return err
		}
		return nil
	})
	if tErr != nil {
		return "", tErr
	}
	return tMsg, nil
}

// logError logs the given error and returns a 500 response.  It is meant to be used in a headless Worker thread.
func logError(ctx context.Context, w http.ResponseWriter, loginID string, err error) {
	s := fmt.Sprintf("worker error: (%v) %v", loginID, err)
	log.Printf(s)
	http.Error(w, s, http.StatusInternalServerError)
}

// workerHandler processes URLs starting with workerPrefix(?/$USERID)(?/$TWITTERID), updating the state machine.
// If USERID and TWITTERID are specified, advance that user and handle.
// If just USERID is specified, advance that user.
// If neither, advance all users.
func workerHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if r.Header.Get("X-Appengine-Cron") != "true" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	} else if time.Now().Minute()%10 == 0 {
		const SkipMessage = "Skipping tick"
		log.Printf(SkipMessage)
		fmt.Fprintf(w, SkipMessage)
		return
	}
	args := strings.Split(strings.TrimPrefix(r.URL.Path, workerPrefix), "/")
	var rootHandles []*RootHandle
	dataClient, err := newFirestoreClient(ctx)
	if err != nil {
		logError(ctx, w, "", err)
		return
	}
	defer dataClient.Close()
	if len(args) == 2 {
		loginID := args[0]
		TwitterID := args[1]
		rootHandle, err := getRootHandleFromString(ctx, dataClient, loginID, TwitterID)
		if err != nil {
			logError(ctx, w, loginID, err)
			return
		}
		rootHandles = append(rootHandles, rootHandle)
	} else if len(args) == 1 && len(args[0]) > 0 {
		loginID := args[0]
		rootHandle, err := getUnfinishedRootHandle(ctx, dataClient, loginID)
		if err != nil {
			logError(ctx, w, loginID, err)
			return
		}
		rootHandles = append(rootHandles, rootHandle)
	} else {
		handles, err := getRootHandlePerUser(ctx, dataClient)
		if err != nil {
			logError(ctx, w, "", err)
			return
		}
		rootHandles = handles
	}
	if len(rootHandles) == 0 || rootHandles[0].Node.Done {
		fmt.Fprintf(w, "User done")
		return
	}
	for _, rootHandle := range rootHandles {
		client, err := newUserTwitterClient(ctx, dataClient, rootHandle.LoginID)
		if err != nil {
			s := fmt.Sprintf("twitter error: (%v) %v", rootHandle.LoginID, err)
			if tErr := updateRootHandleStatus(ctx, dataClient, s, rootHandle); err != nil {
				s = s + fmt.Sprintf(" and couldn't save: %v", tErr)
			}
			log.Printf(s)
			fmt.Fprintf(w, s)
			continue
		}
		status, err := runTick(ctx, client, dataClient, rootHandle.LoginID, rootHandle)
		if err != nil {
			s := fmt.Sprintf("worker error: (%v) %v", rootHandle.LoginID, err)
			if tErr := updateRootHandleStatus(ctx, dataClient, s, rootHandle); err != nil {
				s = s + fmt.Sprintf(" and couldn't save: %v", tErr)
			}
			log.Printf(s)
			fmt.Fprintf(w, s)
			continue
		}
		fmt.Fprintf(w, `Updated %v: %v`, rootHandle.LoginID, status)
	}
}

// getFirebaseUserFromToken returns the user ID of the logged in user.
func getFirebaseUserFromToken(ctx context.Context, token string) (string, error) {
	config := &firebase.Config{
		ProjectID: ProjectID,
	}
	app, err := firebase.NewApp(ctx, config)
	if err != nil {
		return "", err
	}
	authClient, err := app.Auth(ctx)
	if err != nil {
		return "", err
	}
	t, err := authClient.VerifyIDToken(ctx, token)
	if err != nil {
		return "", err
	}
	return t.UID, nil
}

// addHandleHandler enqueues a new handle for fetching.  Its POST body should include:
// auth - the Firebase token
// handle - the handle to fetch.
func addHandleHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	authToken := r.FormValue("auth")
	loginID, err := getFirebaseUserFromToken(ctx, authToken)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "failed to validate firebase token: %v", err)
		return
	}
	dataClient, err := newFirestoreClient(ctx)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "failed to load firestore: %v", err)
		return
	}
	defer dataClient.Close()
	client, err := newUserTwitterClient(ctx, dataClient, loginID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "failed to connect Twitter: %v", err)
		return
	}
	_, err = enqueueHandle(ctx, client, dataClient, loginID, r.FormValue("handle"))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "failed to load handle: %v", err)
		return
	}
}

// deleteHandleHandler deletes a fetch task on behalf of a user.  The POST body
// should contain:
// auth - the Firebase token
// id - the TwitterID of the handle to delete.
func deleteHandleHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	authToken := r.FormValue("auth")
	loginID, err := getFirebaseUserFromToken(ctx, authToken)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "failed to validate firebase token: %v", err)
		return
	}
	dataClient, err := newFirestoreClient(ctx)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "failed to load firestore: %v", err)
		return
	}
	defer dataClient.Close()
	rootHandle, err := getRootHandleFromString(ctx, dataClient, loginID, r.FormValue("id"))
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(w, "could not find identified user: %v", err)
		return
	}
	err = deleteRootHandle(ctx, dataClient, rootHandle)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "failed to delete handle: %v", err)
		return
	}
}

// updateUserHandler implements a POST handler that captures a user's Twitter
// credentials for later use in background fetch tasks.
// The post contents should contain:
// auth - the Firebase token
// name - the user's handle
// token - the Twitter token
// secret - the Twitter secret.
func updateUserHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	authToken := r.FormValue("auth")
	loginID, err := getFirebaseUserFromToken(ctx, authToken)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "failed to validate firebase token: %v", err)
		return
	}
	accessToken := r.FormValue("token")
	accessSecret := r.FormValue("secret")
	if accessToken == "" || accessSecret == "" {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "twitter tokens not provided")
		return
	}
	dataClient, err := newFirestoreClient(ctx)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "failed to load firestore: %v", err)
		return
	}
	defer dataClient.Close()
	appUser, err := getApplicationUser(ctx, dataClient, loginID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "failed to load firebase user: %v", err)
		return
	}
	if appUser == nil || appUser.AccessToken != accessToken || appUser.AccessSecret != accessSecret {
		if err := saveApplicationUser(ctx, dataClient, loginID, r.FormValue("name"), accessToken, accessSecret); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "failed to update user: %v", err)
			return
		}
	}
}

// indexHandler redirects to the frontend client served from Firebase hosting.
func indexHandler(w http.ResponseWriter, r *http.Request) {
    http.Redirect(w, r, "https://"+ProjectID+".firebaseapp.com/", http.StatusFound)
    return
}

