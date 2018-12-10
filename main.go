package main

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	firebase "firebase.google.com/go"
	"github.com/dghubble/go-twitter/twitter"
)

// downloadPrefix is the URL component that prefixes a URL that downloads completed Graph files.
const downloadPrefix = "/download/"

// workerPrefix is the URL component that prefixes a URL that will fetch data for a user.
const workerPrefix = "/worker/"

// statusPrefix is the URL component that prefixes a URL that prints status information for a job being fetched.
const statusPrefix = "/status/"

// deletePrefix is the URL component that prefixes a URL that deletes a handle.
const deletePrefix = "/delete/"

// Compiled templates used in various requests requests.
var (
	deleteTemplate = template.Must(template.ParseFiles("delete.html"))
	indexTemplate  = template.Must(template.ParseFiles("index.html"))
	loginTemplate  = template.Must(template.ParseFiles("login.html"))
	statusTemplate = template.Must(template.ParseFiles("status.html"))
)

// noticer is implemented by a type that can receive an error.  This is primarily used to uniformly print
// errors into template parameters that are otherwise different.
type noticer interface {
	setNotice(error)
}

// Handle is a part of the indexParams struct that has information about a particular job being fetched.
type Handle struct {
	Name        string
	StatusURL   string
	DownloadURL string
}

// authorizeParams is the parameter type taken by authorizeTemplate.
type authorizeParams struct {
	Notice       string
	AuthorizeURL string
	RequestToken string
	PIN          string
}

// setNotice places the error into the Notice field for display.
func (p *authorizeParams) setNotice(err error) {
	p.Notice = err.Error()
}

// indexParams is the parameter type taken by indexTemplate.
type indexParams struct {
	Notice  string
	Handle  string
	Handles []*Handle
}

// setNotice places the error into the Notice field for display.
func (p *indexParams) setNotice(err error) {
	p.Notice = err.Error()
}

// statusParams is the parameter type taken by statusTemplate.
type statusParams struct {
	Notice         string
	Handle         string
	DownloadURL    string
	TwitterID      string
	TickURL        string
	FriendsCount   int
	FollowersCount int
	EnqueuedCount  int
	RemainingCount int
	DeleteURL      string
}

// setNotice places an error into a uniform place in the template.
func (p *statusParams) setNotice(err error) {
	p.Notice = err.Error()
}

// deleteParams is the parameter type taken by deleteTemplate.
type deleteParams struct {
	Notice    string
	Handle    string
	DeleteURL string
	BackURL   string
}

// setNotice places an error into a uniform place in the template.
func (p *deleteParams) setNotice(err error) {
	p.Notice = err.Error()
}

// loginParams is the parameter type taken by loginTemplate.
type loginParams struct {
	Notice string
}

// setNotice places an error into a uniform place in the template.
func (p *loginParams) setNotice(err error) {
	p.Notice = err.Error()
}

// User represents a single user of the system.  The Access fields
// represent Twitter OAuth credentials, and LoginID ties the struct
// back to an AppEngine user.
type User struct {
	AccessToken  string
	AccessSecret string
	LoginID      string
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
}

// FetchedHandle holds a friend or follower of a RootHandle.
type FetchedHandle struct {
	ParentID string
	Node     GephiNode
}

// main registers the handlers for this web application.
func main() {
	http.HandleFunc(downloadPrefix, downloadHandler)
	http.HandleFunc(statusPrefix, statusHandler)
	http.HandleFunc(workerPrefix, workerHandler)
	http.HandleFunc(deletePrefix, deleteHandler)
	http.HandleFunc("/", indexHandler)
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
		log.Printf("Defaulting to port %s", port)
	}

	log.Printf("Listening on port %s", port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%s", port), nil))
}

// enqueueHandle uses the connected Twitter client to enqueue a request for handle to be fetched.
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
	if rootHandle.FollowersCursor != 0 {
		addedIDs, nextCursor, err := addFollowersPage(client, &rootHandle.Node, rootHandle.FollowersCursor)
		if err != nil {
			return "", err
		}
		rootHandle.FollowersCursor = nextCursor
		if err := newFetchedHandles(ctx, dataClient, loginID, "Follower", rootHandle.Node.TwitterID, addedIDs); err != nil {
			return "", err
		}
		if err := saveRootHandle(ctx, dataClient, rootHandle); err != nil {
			return "", err
		}
		return fmt.Sprintf("Fetched %v followers", len(addedIDs)), nil
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
		if err := saveRootHandle(ctx, dataClient, rootHandle); err != nil {
			return "", err
		}
		return fmt.Sprintf("Fetched %v friends", len(addedIDs)), nil
	}
	fetchedHandle, err := getUnfinishedFetchHandle(ctx, dataClient, loginID, rootHandle)
	if err != nil {
		return "", err
	}
	if fetchedHandle == nil {
		rootHandle.Node.Done = true
		if err := saveRootHandle(ctx, dataClient, rootHandle); err != nil {
			return "", err
		}
		return "Marked Done", nil
	}
	twitterUser, err := getTwitterUser(client, fetchedHandle.Node.TwitterID)
	if err != nil {
		return "", err
	}
	if twitterUser.FriendsCount != 0 && twitterUser.FriendsCount <= 5000 {
		_, _, err := addFriendsPage(client, &fetchedHandle.Node, -1)
		if err != nil {
			return "", err
		}
	}
	if twitterUser.FollowersCount != 0 && twitterUser.FollowersCount <= 5000 {
		_, _, err := addFollowersPage(client, &fetchedHandle.Node, -1)
		if err != nil {
			return "", err
		}
	}
	if err := hydrateHandle(ctx, dataClient, loginID, twitterUser, fetchedHandle); err != nil {
		return "", err
	}
	return fmt.Sprintf("Hydrated %v", fetchedHandle.Node.TwitterID), nil
}

// logError logs the given error and returns a 500 response.  It is meant to be used in a headless Worker thread.
func logError(ctx context.Context, w http.ResponseWriter, loginID string, err error) {
	s := fmt.Sprintf("worker error: (%v) %v", loginID, err)
	log.Printf(s)
	http.Error(w, s, http.StatusInternalServerError)
}

// appendError logs the given error to the log, and appends it to the given

// workerHandler processes URLs starting with workerPrefix(?/$USERID)(?/$TWITTERID), updating the state machine.
// If USERID and TWITTERID are specified, advance that user and handle.
// If just USERID is specified, advance that user.
// If neither, advance all users.
func workerHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if r.Header.Get("X-Appengine-Cron") != "true" {
		loginID, err := getFirebaseUser(ctx, r)
		if err != nil || loginID == "" || !isAdmin(loginID) {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
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
			log.Printf(s)
			fmt.Fprintf(w, s)
			continue
		}
		status, err := runTick(ctx, client, dataClient, rootHandle.LoginID, rootHandle)
		if err != nil {
			s := fmt.Sprintf("worker error: (%v) %v", rootHandle.LoginID, err)
			log.Printf(s)
			fmt.Fprintf(w, s)
			continue
		}
		fmt.Fprintf(w, `Updated %v: %v`, rootHandle.LoginID, status)
	}
}

// downloadHandler processes URLs like downloadPrefix/$TWITTERID, offering to download a Gephi file for that twitterID
func downloadHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	loginID, err := getFirebaseUser(ctx, r)
	if err != nil || loginID == "" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	dataClient, err := newFirestoreClient(ctx)
	if err != nil {
		http.Error(w, fmt.Sprintf("error connecting to datastore: %v", err), http.StatusInternalServerError)
		return
	}
	defer dataClient.Close()
	rootHandle, err := getRootHandleFromString(ctx, dataClient, loginID, strings.TrimPrefix(r.URL.Path, downloadPrefix))
	if err != nil {
		http.Error(w, fmt.Sprintf("error getting root handle: %v", err), http.StatusInternalServerError)
		return
	}
	fetchedHandles, err := getDoneJobs(ctx, dataClient, rootHandle)
	if err != nil {
		http.Error(w, fmt.Sprintf("error getting handles: %v", err), http.StatusInternalServerError)
		return
	}
	content := buildGephiFile(rootHandle, fetchedHandles)
	w.Header().Add("Content-Disposition", fmt.Sprintf("Attachment; filename=%v.gml", rootHandle.Node.ScreenName))
	http.ServeContent(w, r, rootHandle.Node.ScreenName+".gml", time.Now(), bytes.NewReader(content))
}

// returnError returns the given error in the template and sets the return code.
func returnError(ctx context.Context, w http.ResponseWriter, t *template.Template, p noticer, err error) {
	w.WriteHeader(http.StatusInternalServerError)
	p.setNotice(err)
	if err := t.Execute(w, p); err != nil {
		log.Printf(err.Error())
	}
}

// statusHandler processes URLs like statusPrefix/$TWITTERID, printing a template showing how far that process has progressed.
func statusHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	loginID, err := getFirebaseUser(ctx, r)
	if err != nil || loginID == "" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	params := &statusParams{}
	dataClient, err := newFirestoreClient(ctx)
	if err != nil {
		returnError(ctx, w, statusTemplate, params, err)
		return
	}
	defer dataClient.Close()
	rootHandle, err := getRootHandleFromString(ctx, dataClient, loginID, strings.TrimPrefix(r.URL.Path, statusPrefix))
	if err != nil {
		returnError(ctx, w, statusTemplate, params, err)
		return
	}
	params.Handle = rootHandle.Node.ScreenName
	params.FriendsCount = rootHandle.Node.FriendsCount
	params.FollowersCount = rootHandle.Node.FollowersCount
	if isAdmin(loginID) {
		params.TwitterID = rootHandle.Node.TwitterID
		params.TickURL = makeDebugTickUrl(loginID, rootHandle.Node.TwitterID)
	}
	if isAdmin(loginID) || rootHandle.Node.Done {
		params.DownloadURL = makeDownloadUrl(rootHandle.Node.TwitterID)
	}
	params.DeleteURL = makeDeleteUrl(rootHandle.Node.TwitterID)

	enqueuedCount, err := countEnqueued(ctx, dataClient, rootHandle)
	if err != nil {
		returnError(ctx, w, statusTemplate, params, err)
		return
	}
	params.EnqueuedCount = enqueuedCount
	remainingCount, err := countRemaining(ctx, dataClient, rootHandle)
	if err != nil {
		returnError(ctx, w, statusTemplate, params, err)
		return
	}
	params.RemainingCount = remainingCount
	if err := statusTemplate.Execute(w, params); err != nil {
		log.Printf(err.Error())
	}
}

// makeStatusUrl builds a URL suitable for viewing the status of the given Twitter ID.
func makeStatusUrl(twitterID string) string {
	return statusPrefix + twitterID
}

// makeDownloadUrl builds a URL that will download the graph rooted at twitterID.
func makeDownloadUrl(twitterID string) string {
	return downloadPrefix + twitterID
}

// makeDebugTickUrl builds an admin-only URL that will force advance the state machine.
func makeDebugTickUrl(loginID string, twitterID string) string {
	return workerPrefix + loginID + "/" + twitterID
}

// makeDeleteUrl builds a URL that will delete the current Twitter handle.
func makeDeleteUrl(twitterID string) string {
	return deletePrefix + twitterID
}

// deleteHandler processes Delete URLs.  On GET it prints a confirmation page.  On POST it does it.
func deleteHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	params := &deleteParams{}
	loginID, err := getFirebaseUser(ctx, r)
	if err != nil || loginID == "" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	dataClient, err := newFirestoreClient(ctx)
	if err != nil {
		returnError(ctx, w, deleteTemplate, params, err)
		return
	}
	defer dataClient.Close()
	rootHandle, err := getRootHandleFromString(ctx, dataClient, loginID, strings.TrimPrefix(r.URL.Path, deletePrefix))
	if err != nil {
		returnError(ctx, w, deleteTemplate, params, err)
		return
	}
	params.Handle = rootHandle.Node.ScreenName
	params.DeleteURL = makeDeleteUrl(rootHandle.Node.TwitterID)
	params.BackURL = makeStatusUrl(rootHandle.Node.TwitterID)
	if r.Method == "GET" {
		if err := deleteTemplate.Execute(w, params); err != nil {
			log.Printf(err.Error())
		}
		return
	}
	// It's a POST.  Delete the user.
	err = deleteRootHandle(ctx, dataClient, rootHandle)
	if err != nil {
		returnError(ctx, w, deleteTemplate, params, err)
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

// Returns the user ID of the logged in user, if known.  Returns ("", nil) if the user is simply not
// logged in, or "", err if some internal fault occured.
func getFirebaseUser(ctx context.Context, r *http.Request) (string, error) {
	cookie, err := r.Cookie("Authorization")
	if err != nil {
		return "", nil
	}
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
	t, err := authClient.VerifyIDToken(ctx, cookie.Value)
	if err != nil {
		return "", err
	}
	return t.UID, nil
}

// indexHandler processes all URLs not matched by a more specific rule.  It prints the index.html or login.html
// template showing the status of the system for this user.
func indexHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	ctx := r.Context()
	loginID, err := getFirebaseUser(ctx, r)
	if err != nil {
		returnError(ctx, w, loginTemplate, &loginParams{}, err)
		return
	}
	if loginID == "" {
		if err := loginTemplate.Execute(w, &loginParams{}); err != nil {
			log.Printf(err.Error())
		}
		return
	}
	params := &indexParams{}
	dataClient, err := newFirestoreClient(ctx)
	if err != nil {
		returnError(ctx, w, indexTemplate, params, err)
		return
	}
	defer dataClient.Close()
	appUser, err := getApplicationUser(ctx, dataClient, loginID)
	if err != nil {
		returnError(ctx, w, indexTemplate, params, err)
		return
	}
	cookie, err := r.Cookie("Token")
	accessToken := ""
	if err == nil {
		accessToken = cookie.Value
	}
	cookie, err = r.Cookie("Secret")
	accessSecret := ""
	if err == nil {
		accessSecret = cookie.Value
	}
	if accessToken != "" && accessSecret != "" {
		if appUser == nil || appUser.AccessToken != accessToken || appUser.AccessSecret != accessSecret {
			if err := saveApplicationUser(ctx, dataClient, loginID, accessToken, accessSecret); err != nil {
				returnError(ctx, w, indexTemplate, params, err)
				return
			}
		}
	}

	rootHandles, err := getRootHandles(ctx, dataClient, loginID)
	if err != nil {
		returnError(ctx, w, indexTemplate, params, err)
		return
	}
	for _, r := range rootHandles {
		h := &Handle{
			Name:      r.Node.ScreenName,
			StatusURL: makeStatusUrl(r.Node.TwitterID),
		}
		if isAdmin(loginID) || r.Node.Done {
			h.DownloadURL = makeDownloadUrl(r.Node.TwitterID)
		}
		params.Handles = append(params.Handles, h)
	}
	if r.Method == "GET" {
		if err := indexTemplate.Execute(w, params); err != nil {
			log.Printf(err.Error())
		}
		return
	}
	// It's a POST request, so handle the form submission.
	handle := r.FormValue("handle")
	client, err := newUserTwitterClient(ctx, dataClient, loginID)
	if err != nil {
		returnError(ctx, w, indexTemplate, params, err)
		return
	}
	twitterID, err := enqueueHandle(ctx, client, dataClient, loginID, handle)
	if err != nil {
		returnError(ctx, w, indexTemplate, params, err)
		return
	}
	http.Redirect(w, r, makeStatusUrl(twitterID), http.StatusFound)
}
