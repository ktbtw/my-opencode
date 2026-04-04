import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/storage/auth_storage.dart';
import '../data/auth_api.dart';
import '../domain/operator_profile.dart';

class AuthState {
  const AuthState({
    required this.initialized,
    required this.loading,
    required this.accessToken,
    required this.operator,
  });

  final bool initialized;
  final bool loading;
  final String accessToken;
  final OperatorProfile? operator;

  bool get isAuthenticated => accessToken.isNotEmpty && operator != null;

  AuthState copyWith({
    bool? initialized,
    bool? loading,
    String? accessToken,
    OperatorProfile? operator,
    bool clearOperator = false,
  }) {
    return AuthState(
      initialized: initialized ?? this.initialized,
      loading: loading ?? this.loading,
      accessToken: accessToken ?? this.accessToken,
      operator: clearOperator ? null : (operator ?? this.operator),
    );
  }

  static const empty = AuthState(
    initialized: false,
    loading: false,
    accessToken: '',
    operator: null,
  );
}

class AuthController extends StateNotifier<AuthState> {
  AuthController(this._api, this._storage) : super(AuthState.empty) {
    bootstrap();
  }

  final AuthApi _api;
  final AuthStorage _storage;

  Future<void> bootstrap() async {
    final saved = await _storage.read();
    final token = saved['access_token'] ?? '';
    final operatorKey = saved['operator_key'] ?? '';
    final name = saved['name'] ?? '';
    final username = saved['username'] ?? '';

    if (token.isEmpty || operatorKey.isEmpty) {
      state = state.copyWith(initialized: true);
      return;
    }

    state = state.copyWith(
      initialized: true,
      accessToken: token,
      operator: OperatorProfile(
        id: 0,
        operatorUid: '',
        username: username,
        name: name,
        operatorKey: operatorKey,
      ),
    );
  }

  Future<void> login(String username, String password) async {
    state = state.copyWith(loading: true);
    try {
      final result = await _api.login(username: username, password: password);
      await _storage.write(
        accessToken: result.accessToken,
        operatorKey: result.operator.operatorKey,
        name: result.operator.name,
        username: result.operator.username,
      );
      state = AuthState(
        initialized: true,
        loading: false,
        accessToken: result.accessToken,
        operator: result.operator,
      );
    } catch (_) {
      state = state.copyWith(loading: false);
      rethrow;
    }
  }

  Future<void> logout() async {
    await _storage.clear();
    state = const AuthState(
      initialized: true,
      loading: false,
      accessToken: '',
      operator: null,
    );
  }
}

final authApiProvider = Provider((ref) => const AuthApi());
final authStorageProvider = Provider((ref) => const AuthStorage());
final authControllerProvider = StateNotifierProvider<AuthController, AuthState>((ref) {
  return AuthController(
    ref.watch(authApiProvider),
    ref.watch(authStorageProvider),
  );
});
