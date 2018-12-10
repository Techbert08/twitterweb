package main

import (
	"context"
	"strconv"

	"cloud.google.com/go/firestore"
	"github.com/dghubble/go-twitter/twitter"
	"github.com/dghubble/oauth1"
)

func newUserTwitterClient(ctx context.Context, dataClient *firestore.Client, userID string) (*twitter.Client, error) {
	user, err := getApplicationUser(ctx, dataClient, userID)
	if err != nil {
		return nil, err
	}
	config := oauth1.NewConfig(TwitterConsumerKey, TwitterConsumerSecret)
	token := oauth1.NewToken(user.AccessToken, user.AccessSecret)
	httpClient := config.Client(ctx, token)
	client := twitter.NewClient(httpClient)
	return client, nil
}

// permanentErrorMessage returns a non-empty description of the error if it is permanent.
// This captures suspended or deleted accounts.
func permanentErrorMessage(err error) string {
	e, ok := err.(twitter.APIError)
	if ok && len(e.Errors) > 0 {
		if e.Errors[0].Code == 63 {
			return "SUSPENDED"
		}
		if e.Errors[0].Code == 50 {
			return "NOT FOUND"
		}
	}
	return ""
}

// getTwitterUserByName gets the user identified by handle.
func getTwitterUserByName(client *twitter.Client, handle string) (*twitter.User, error) {
	user, _, err := client.Users.Show(&twitter.UserShowParams{
		ScreenName: handle,
	})
	if err != nil {
		if permanentErrorMessage(err) != "" {
			return &twitter.User{
				ScreenName:     handle,
				FriendsCount:   0,
				FollowersCount: 0,
			}, nil
		}
		return nil, err
	}
	return user, nil
}

// getTwitterUser gets the user identified by the given ID.
func getTwitterUser(client *twitter.Client, twitterID string) (*twitter.User, error) {
	twitterIDNum, err := strconv.ParseInt(twitterID, 10, 64)
	if err != nil {
		return nil, err
	}
	user, _, err := client.Users.Show(&twitter.UserShowParams{
		UserID: twitterIDNum,
	})
	if err != nil {
		if msg := permanentErrorMessage(err); msg != "" {
			return &twitter.User{
				IDStr:          twitterID,
				ScreenName:     msg,
				FriendsCount:   0,
				FollowersCount: 0,
			}, nil
		}
		return nil, err
	}
	return user, nil
}

// addFriendsPage retrieves one page of Friends from the given Node with an offset of cursor.
// It is appended to the existing node.  The new cursor is returned.
func addFriendsPage(client *twitter.Client, node *GephiNode, cursor int64) ([]string, int64, error) {
	twitterIDNum, err := strconv.ParseInt(node.TwitterID, 10, 64)
	if err != nil {
		return nil, 0, err
	}
	friends, _, err := client.Friends.IDs(&twitter.FriendIDParams{
		UserID: twitterIDNum,
		Cursor: cursor,
		Count:  5000,
	})
	if err != nil {
		return nil, 0, err
	}
	var addedIDs []string
	for _, friend := range friends.IDs {
		addedIDs = append(addedIDs, strconv.FormatInt(friend, 10))
	}
	node.FriendIDs = append(node.FriendIDs, addedIDs...)
	return addedIDs, friends.NextCursor, nil
}

// addFollowersPage retrieves one page of Followers from the given Node with an offset of cursor.
// It is appended to the existing node.  The new cursor is returned.
func addFollowersPage(client *twitter.Client, node *GephiNode, cursor int64) ([]string, int64, error) {
	twitterIDNum, err := strconv.ParseInt(node.TwitterID, 10, 64)
	if err != nil {
		return nil, 0, err
	}
	followers, _, err := client.Followers.IDs(&twitter.FollowerIDParams{
		UserID: twitterIDNum,
		Cursor: cursor,
		Count:  5000,
	})
	if err != nil {
		return nil, 0, err
	}
	var addedIDs []string
	for _, follower := range followers.IDs {
		addedIDs = append(addedIDs, strconv.FormatInt(follower, 10))
	}
	node.FollowerIDs = append(node.FollowerIDs, addedIDs...)
	return addedIDs, followers.NextCursor, nil
}
