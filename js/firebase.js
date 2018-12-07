var config = {
    apiKey: "KEY",
    authDomain: "DOMAIN",
    databaseURL: "DATABASE",
    projectId: "PROJECT",
    storageBucket: "STORAGE",
    messagingSenderId: "SENDER"
};
firebase.initializeApp(config);
function logout() {
    document.cookie = "Authorization=; expires=Thu, 01 Jan 1970 00:00:00 GMT";
    document.cookie = "Secret=; expires=Thu, 01 Jan 1970 00:00:00 GMT";
    document.cookie = "Token=; expires=Thu, 01 Jan 1970 00:00:00 GMT";
    firebase.auth().signOut();
    return false;
}