# TwitterWeb

This repository contains a complete web application that roughly implements the Twecoll command line utility at
https://github.com/jdevoo/twecoll.  An AngularDart-based frontend served from Firebase Hosting manages
the UI, while an App Engine Go service manages the backend.  It makes full use of Firebase Authentication,
Storage, and the Firestore to do this.

## Set Up

Install the relevant development tools:

1.   [Google Go](https://golang.org/ )
1.   [AngularDart](https://webdev.dartlang.org/guides/get-started)
1.   [Google Cloud SDK](https://cloud.google.com/sdk/)
1.   [Firebase CLI](https://firebase.google.com/docs/cli/)
1.   [Git](https://git-scm.com/) of course.

Now provision cloud resources for it:

1.   [Create](https://console.cloud.google.com) a Google Cloud project. Note the Project ID.
1.   [Link](https://console.firebase.google.com) it to Firebase.
1.   [Create](https://developer.twitter.com) a Twitter developer account and application. It should permit
login.

## Configure Firebase

Inside the [Firebase Console](https://console.firebase.google.com) activate Firebase Authentication for Twitter.
Paste the OAuth redirect endpoint into your [Twitter](https://developer.twitter.com) application configuration.
Similarly paste the Twitter login credentials into the appropriate boxes in Firebase.

Enable Firestore and Storage.

## Edit the code

Clone this repository:

    git clone https://github.com/Techbert08/twitterweb.git twitterweb

Inside the app code, make the following adjustments:

1.  Inside `backend/constants.go`, add the Cloud project ID, the Twitter Key and the Twitter Secret from before.
1.  Inside `frontend/web/main.dart`, fill in the Firebase credentials from "Project Settings->Add Firebase to your web app"
in the [Firebase Console](https://console.firebase.google.com). Ensure the `apiEndpoint` is set, too.

## Deploy

Run the following:

    cd twitterweb/backend
    gcloud app deploy app.yaml cron.yaml
    cd ../frontend
    gsutil cors set cors.json gs://${PROJECTID}.appspot.com
    webdev build
    firebase deploy

The application should appear at https://${PROJECTID}.firebaseapp.com

From there log in with a Twitter account, and input a handle to start fetching.  Twitter is rate-limited to
15 queries every 15 minutes, so a background task fetches one handle per minute until complete. 
A download link is offered when done.

## Additional work

The backend code in Go should probably be a Cloud Function, but at present those aren't available in Go. If that
happens the application can fit entirely inside Firebase tooling.