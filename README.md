# TwitterWeb

This directory contains an App Engine Go project that roughly implements the Twecoll command line utility at
https://github.com/jdevoo/twecoll

## Set Up

Set up a Google Cloud project.  This will give a Project ID.

Add Firebase to that project.  This will link the Firebase console to the Cloud console.  Within Firebase,
activate Firebase Authentication for Twitter.  Note the provided box for the OAuth redirect endpoint.  You'll
need that later.

Set up a Twitter Developer account and create an application. This application should permit sign-in.  After
creation the Key and Secret are displayed.  Copy those into Firebase Authentication, and copy the Firebase
Authentication OAuth redirect URL into Twitter.

Now clone this repository into the project.  Launch the Cloud Shell and pull down the repository into it:

    git clone https://source.developers.google.com/p/murmur-148211/r/twitterweb twitterweb

Inside the app code, make the following adjustments:

1.  Inside `constants.go`, add the Cloud project ID, the Twitter Key and the Twitter Secret from before.
1.  Go to Project Settings in Firebase and click `Add Firebase to your web app`.  Copy those settings into
    `js/firebase.js` 

Finally, deploy it to App Engine:

    cd twitterweb
    gcloud app deploy app.yaml cron.yaml index.yaml

After a few moments the application should go live at http://${PROJECTID}.appspot.com  From there log in with a Twitter account,
and input a handle to start fetching.  Twitter is rate-limited to 15 queries every 15 minutes, so a
background task fetches one handle per minute until complete.  A download link is offered when done.

## Local Testing

Simply use the dev server:

    dev_appserver.py app.yaml