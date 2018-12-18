@JS()
library oauth_credential;

import 'package:firebase/firebase.dart' as fb;
import 'package:js/js.dart';

/// OAuthCredential plugs a gap in the Dart wrapper around firebase auth.
/// That library does not map provider details of the credential.
@anonymous
@JS()
abstract class OAuthCredential extends fb.AuthCredential {
  external String get accessToken;
  external String get idToken;
  external String get secret;
}
