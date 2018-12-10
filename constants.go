package main

// The Application ID of this project.  This connects to the datastore and Firebase.
const ProjectID = "PROJECT_ID"

// The Twitter Consumer Key of the developer application to use.
const TwitterConsumerKey = "TWITTER_CONSUMER_KEY"

// The Twitter Consumer Secret of the developer application to use.
const TwitterConsumerSecret = "TWITTER_CONSUMER_SECRET"

// Returns whether the given user should be considered an Admin.  Copy this from the
// Firebase Authentication console to offer extra options to this user.
func isAdmin(uid string) bool {
	admins := map[string]bool{
		"ADMIN": true,
	}
	return admins[uid]
}
