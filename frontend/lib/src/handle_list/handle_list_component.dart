import 'dart:async';

import 'package:angular/angular.dart';
import 'package:angular_components/angular_components.dart';

import 'handle_list_service.dart';

@Component(
  selector: 'handle-list',
  styleUrls: ['handle_list_component.css'],
  templateUrl: 'handle_list_component.html',
  directives: [
    MaterialButtonComponent,
    MaterialCheckboxComponent,
    MaterialDialogComponent,
    MaterialFabComponent,
    MaterialIconComponent,
    MaterialYesNoButtonsComponent,
    ModalComponent,
    materialInputDirectives,
    NgFor,
    NgIf,
  ],
  providers: [ClassProvider(HandleListService), overlayBindings],
)
class HandleListComponent implements OnInit, OnDestroy {
  /// handles holds the list of handles being fetched.
  List<Handle> handles = [];

  /// handleListService manages the request and deletion of handles to be
  /// fetched.
  final HandleListService _handleListService;

  /// sub manages the subscription to Firestore for updates to the list of
  /// handles.
  StreamSubscription<List<Handle>> _sub;

  /// newHandle backs a text box to capture a new handle to save.
  String newHandle = "";

  /// displayError backs a notification area that communicateserrors.
  String displayError = "";

  /// handleToDelete is a state variable that holds the ID of a handle to
  /// delete. The dialog is visible when this is nonempty.
  String handleToDelete = "";

  HandleListComponent(this._handleListService);

  @override
  void ngOnInit() {
    _sub = _handleListService.getHandleList().listen((data) => handles = data);
  }

  @override
  void ngOnDestroy() {
    _sub.cancel();
  }

  /// add adds a new handle to be fetched.
  void add() {
    _handleListService
        .add(newHandle)
        .then((r) => displayError = "")
        .catchError((e) => displayError = e.toString());
    newHandle = '';
  }

  /// remove deletes a fetch task from the backend.
  void remove() {
    _handleListService
        .remove(handleToDelete)
        .then((r) => displayError = "")
        .catchError((e) => displayError = e.toString());
    handleToDelete = "";
  }
}
