import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../data/auth_repository.dart';

final authRepositoryProvider = Provider((ref) => AuthRepository());

class AuthState {
  final bool loading;
  final String? error;
  const AuthState({this.loading = false, this.error});
}

class AuthNotifier extends StateNotifier<AuthState> {
  final AuthRepository _repo;
  AuthNotifier(this._repo) : super(const AuthState());

  Future<bool> login(String username, String password) async {
    state = const AuthState(loading: true);
    try {
      await _repo.login(username.trim(), password);
      state = const AuthState();
      return true;
    } catch (e) {
      state = AuthState(error: e.toString());
      return false;
    }
  }

  Future<void> logout() async {
    await _repo.logout();
    state = const AuthState();
  }

  void clearError() {
    state = const AuthState();
  }
}

final authProvider = StateNotifierProvider<AuthNotifier, AuthState>(
  (ref) => AuthNotifier(ref.watch(authRepositoryProvider)),
);
