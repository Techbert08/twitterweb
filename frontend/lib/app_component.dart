import 'package:angular/angular.dart';
import 'package:angular_components/angular_components.dart';
import 'package:firebase_dart_ui/firebase_dart_ui.dart';
import 'package:firebase/firebase.dart' as fb;
import 'package:http/http.dart';

import 'app_config.dart';
import 'src/handle_list/handle_list_component.dart';
import 'src/oauth_credential.dart';

import 'dart:async';
import 'dart:js';

@Component(
  selector: 'my-app',
  styleUrls: [
    'app_component.css',
    'package:angular_components/app_layout/layout.scss.css',
  ],
  templateUrl: 'app_component.html',
  directives: [
    FirebaseAuthUIComponent,
    HandleListComponent,
    MaterialButtonComponent,
    NgIf,
  ],
)
class AppComponent implements OnInit, OnDestroy {
  // _uiConfig as demanded by the FirebaseUI widget.
  UIConfig _uiConfig;

  /// auth is the Firebase authentication service.
  final fb.Auth _auth;

  /// client is the HTTP Client used for communicating with the
  /// backend.
  final Client _client;

  /// config pulls in information needed for backend communication.
  final AppConfig _config;

  /// firebaseToken holds the retrieved login token.  It's extracted out because
  /// the order in which Twitter credentials arrive and auth.currentUser becomes
  /// valid appears arbitrary.
  String _firebaseToken;

  /// displayName holds the current user's display name.  It is also extracted
  /// because auth.currentUser may not be valid yet.
  String _displayName;

  /// twitterToken is the user credential immediately following the login
  /// redirect.
  String _twitterToken;

  /// twitterSecret is the other component of that login credential.
  String _twitterSecret;

  /// sub receives notifications when a user's login state changes.
  StreamSubscription<fb.User> _sub;

  /// displayError is plumbed to the frontend to print various errors.
  String displayError = "";

  AppComponent(this._auth, this._client, this._config);

  /// logout logs out the given user from Firebase.
  Future<Null> logout() async {
    await _auth.signOut();
  }

  /// updateUser transmits Twitter credentials to the backend if all present.
  /// The necessary information is split between the onAuthStateChanged event
  /// and signInSuccess
  void updateUser() {
    if (_twitterToken != null &&
        _twitterSecret != null &&
        _displayName != null &&
        _firebaseToken != null) {
      _client
          .post(_config.apiEndpoint + "/updateUser", body: {
            "name": _displayName,
            "auth": _firebaseToken,
            "token": _twitterToken,
            "secret": _twitterSecret
          })
          .then((r) => displayError = "")
          .catchError((e) => displayError = e.toString());
    }
  }

  /// ngOnInit subscribes to login events and notifies the backend when they
  /// occur.  Only talk to the backend if Twitter credentials are present.
  @override
  void ngOnInit() {
    _sub = _auth.onAuthStateChanged.listen((user) {
      if (user == null) {
        _twitterToken = null;
        _twitterSecret = null;
        _displayName = null;
        _firebaseToken = null;
      } else {
        _displayName = user.displayName;
        user.getIdToken().then((token) {
          _firebaseToken = token;
          updateUser();
        });
      }
    });
  }

  @override
  void ngOnDestroy() {
    _sub.cancel();
  }

  /// signInSuccess is an interop callback passed to FirebaseUI.  It captures
  /// OAuth credentials but cannot get the Firebase token.  That isn't available
  /// yet.
  bool _signInSuccess(fb.UserCredential authResult, String redirectUrl) {
    _twitterToken = (authResult.credential as OAuthCredential).accessToken;
    _twitterSecret = (authResult.credential as OAuthCredential).secret;
    updateUser();

    // returning false gets rid of the double page load (don't redirect to /)
    return false;
  }

  /// getUIConfig returns the UI configuration needed by FirebaseUI.
  UIConfig getUIConfig() {
    if (_uiConfig == null) {
      _uiConfig = new UIConfig(
          signInSuccessUrl: '/',
          signInOptions: [
            fb.TwitterAuthProvider.PROVIDER_ID,
          ],
          signInFlow: "redirect",
          credentialHelper: ACCOUNT_CHOOSER,
          callbacks: new Callbacks(
            signInSuccessWithAuthResult: allowInterop(_signInSuccess),
          ));
    }
    return _uiConfig;
  }

  /// isAuthenticated returns true if a user is currently signed in.
  bool isAuthenticated() => _auth.currentUser != null;
}
