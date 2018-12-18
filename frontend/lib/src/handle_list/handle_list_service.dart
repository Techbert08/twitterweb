import 'dart:async';

import '../../app_config.dart';
import 'package:angular/core.dart';
import 'package:firebase/firebase.dart' as fb;
import 'package:firebase/firestore.dart';
import 'package:http/http.dart';

/// Handle is a task in the backend being fetched.
class Handle {
  /// id is the Twitter ID of this handle.
  String id;

  /// name is the human-readable name of this handle.
  String name;

  /// done is true if the handle is Finished and ready for download.
  bool done;

  /// status is plumbed to the frontend and communicates the last action taken
  /// on behalf of this handle.
  String status;

  /// downloadURL is a Firebase Storage URL that will download the completed
  /// graph
  String downloadURL;

  /// remaining indicates how many fetches remain to be performed.
  int remaining;

  /// updateDownloadUrl asynchronously populates the downloadURL property if
  /// the task is done.
  updateDownloadUrl(fb.Storage storage, String uid) {
    if (done) {
      var ref = storage.ref("graphs/" + uid + "/" + id);
      ref
          .getDownloadURL()
          .then((uri) => downloadURL = uri.toString())
          .catchError((e) => status = e.toString());
    }
  }
}

/// HandleListService mediates most calls to the fetch backend.
@Injectable()
class HandleListService {

  /// store is the Firestore service containing handles being fetched.
  final Firestore _store;

  /// auth indicates when a user is signed in.
  final fb.Auth _auth;

  /// storage enables the creation of Download URLs for completed handles.
  final fb.Storage _storage;

  /// client connects to the backend.
  final Client _client;

  /// config wraps various options required to connect to the backend.
  final AppConfig _config;

  HandleListService(
      this._store, this._auth, this._client, this._storage, this._config);

  /// getHandleList emits the complete list of handles when any of them are
  /// updated on the backend.
  Stream<List<Handle>> getHandleList() {
    var user = _auth.currentUser;
    return _store
        .collection("User")
        .doc(user.uid)
        .collection("RootHandle")
        .orderBy("Node.ScreenName")
        .onSnapshot
        .map((snap) {
      List<Handle> handles = new List();
      for (var doc in snap.docs) {
        var handle = new Handle()
          ..id = doc.data()["Node"]["TwitterID"] ?? ""
          ..done = doc.data()["Node"]["Done"] ?? false
          ..status = doc.data()["Status"] ?? ""
          ..downloadURL = doc.data()["DownloadURL"] ?? ""
          ..remaining = doc.data()["Remaining"] ?? 0
          ..name = doc.data()["Node"]["ScreenName"] ?? ""
          ..updateDownloadUrl(_storage, _auth.currentUser.uid);
        handles.add(handle);
      }
      return handles;
    });
  }

  /// add adds a new fetch task to the backend identified by Twitter handle.
  Future<void> add(String newHandle) {
    if (_auth.currentUser == null) {
      return Future.error("Not logged in");
    }
    return _auth.currentUser.getIdToken().then((token) {
      return _client.post(_config.apiEndpoint + "/addHandle", body: {
        "handle": newHandle,
        "auth": token,
      });
    });
  }

  /// remove deletes a fetch task identified by Twitter ID.
  Future<void> remove(String id) {
    if (_auth.currentUser == null) {
      return Future.error("Not logged in");
    }
    return _auth.currentUser.getIdToken().then((token) {
      return _client.post(_config.apiEndpoint + "/deleteHandle", body: {
        "id": id,
        "auth": token,
      });
    });
  }
}
