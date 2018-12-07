firebase.auth().onAuthStateChanged(function(user) {
    if (user) {
        user.getIdToken().then(function(accessToken) {
            document.cookie = "Authorization=" + accessToken;
            location.reload(true);
        });
    }
});
var uiConfig = {
    signInSuccessUrl: '/',
    callbacks: {
        signInSuccessWithAuthResult: function(authResult, redirectUrl) {
            document.cookie = "Authorization=" + authResult.user.accessToken;
            document.cookie = "Token=" + authResult.credential.accessToken;
            document.cookie = "Secret=" +authResult.credential.secret;
            return true;
        },
    },
    signInOptions: [
        firebase.auth.TwitterAuthProvider.PROVIDER_ID
    ],
};
var ui = new firebaseui.auth.AuthUI(firebase.auth());
ui.start('#firebaseui-auth-container', uiConfig);