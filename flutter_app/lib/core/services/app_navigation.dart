import 'package:flutter/material.dart';

final GlobalKey<NavigatorState> appNavigatorKey = GlobalKey<NavigatorState>(
  debugLabel: 'appNavigator',
);

BuildContext? get appNavigatorContext =>
    appNavigatorKey.currentContext ??
    appNavigatorKey.currentState?.overlay?.context;
