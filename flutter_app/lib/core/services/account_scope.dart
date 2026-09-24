import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../storage/app_storage.dart';

/// Token refresh retains this scope; logout, account and server changes reset it.
final accountScopeProvider = Provider<String>((ref) {
  final subscription = AppStorage.authIdentityChanges.listen((_) {
    ref.invalidateSelf();
  });
  ref.onDispose(subscription.cancel);
  return AppStorage.initialized ? AppStorage.storageIdentity : '';
});
