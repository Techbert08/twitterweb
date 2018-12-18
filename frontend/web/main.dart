import 'package:angular/angular.dart';
import 'package:twitterdart/app_component.template.dart' as ng;
import 'package:twitterdart/app_config.dart';

import 'package:firebase/firebase.dart' as fb;
import 'package:firebase/firestore.dart';
import 'package:http/browser_client.dart';
import 'package:http/http.dart';

import 'main.template.dart' as self;

@GenerateInjector([
  ClassProvider(Client, useClass: BrowserClient),
  FactoryProvider(Firestore, fb.firestore),
  FactoryProvider(fb.Auth, fb.auth),
  FactoryProvider(fb.Storage, fb.storage),
  FactoryProvider(AppConfig, appConfigFactory)
])
final InjectorFactory injector = self.injector$Injector;

AppConfig appConfigFactory() =>
    AppConfig()..apiEndpoint = "https://PROJECTID.appspot.com";

void main() {
  fb.initializeApp(
      apiKey: "KEY",
      authDomain: "PROJECTID.firebaseapp.com",
      databaseURL: "https://PROJECTID.firebaseio.com",
      projectId: "PROJECTID",
      storageBucket: "PROJECTID.appspot.com",
      messagingSenderId: "SENDERID");
  runApp(ng.AppComponentNgFactory, createInjector: injector);
}
