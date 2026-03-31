import 'dart:async';

import 'package:connectivity_plus/connectivity_plus.dart';

/// Monitors network connectivity and exposes a stream of online/offline state.
///
/// When connectivity is restored after being offline, [onReconnect] is called
/// so the sync orchestrator can trigger an immediate sync.
class ConnectivityService {
  final Connectivity _connectivity = Connectivity();
  StreamSubscription<List<ConnectivityResult>>? _subscription;

  bool _isOnline = true;
  bool get isOnline => _isOnline;

  /// Called when the device transitions from offline to online.
  void Function()? onReconnect;

  /// Stream of connectivity state changes.
  final _controller = StreamController<bool>.broadcast();
  Stream<bool> get onlineStream => _controller.stream;

  /// Start monitoring connectivity.
  Future<void> start() async {
    // Check initial state
    final results = await _connectivity.checkConnectivity();
    _updateState(results);

    _subscription = _connectivity.onConnectivityChanged.listen(_updateState);
  }

  void _updateState(List<ConnectivityResult> results) {
    final wasOnline = _isOnline;
    _isOnline = results.any((r) => r != ConnectivityResult.none);
    _controller.add(_isOnline);

    if (!wasOnline && _isOnline) {
      // Transitioned from offline -> online
      onReconnect?.call();
    }
  }

  /// Stop monitoring.
  void stop() {
    _subscription?.cancel();
    _subscription = null;
  }

  void dispose() {
    stop();
    _controller.close();
  }
}
