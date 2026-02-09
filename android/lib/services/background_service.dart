import 'dart:async';

import 'package:flutter_background_service/flutter_background_service.dart';

/// Initialises and manages an Android foreground service so that sync
/// operations continue when the app is backgrounded.
///
/// Communication between the foreground UI and the background isolate is done
/// via the `invoke` / `on` message passing API provided by
/// `flutter_background_service`.
class BackgroundSyncService {
  static const _periodicSyncInterval = Duration(minutes: 5);

  /// Configure the background service. Call this once from `main()`.
  static Future<void> initialise() async {
    final service = FlutterBackgroundService();

    await service.configure(
      androidConfiguration: AndroidConfiguration(
        onStart: _onStart,
        autoStart: false,
        autoStartOnBoot: false,
        isForegroundMode: true,
        initialNotificationTitle: 'Obsyncian',
        initialNotificationContent: 'Syncing your vault in the background',
        foregroundServiceNotificationId: 888,
        foregroundServiceTypes: [AndroidForegroundType.dataSync],
      ),
      iosConfiguration: IosConfiguration(
        autoStart: false,
      ),
    );
  }

  /// Start the background service.
  static Future<void> start() async {
    final service = FlutterBackgroundService();
    final running = await service.isRunning();
    if (!running) {
      await service.startService();
    }
  }

  /// Stop the background service.
  static Future<void> stop() async {
    final service = FlutterBackgroundService();
    service.invoke('stop');
  }

  /// Tell the background service to trigger a sync now.
  static void requestSync() {
    final service = FlutterBackgroundService();
    service.invoke('sync');
  }

  /// Listen for log messages coming from the background isolate.
  static Stream<Map<String, dynamic>?> get logStream {
    final service = FlutterBackgroundService();
    return service.on('log');
  }

  /// The entry point for the background isolate.
  /// Must be a top-level or static function.
  @pragma('vm:entry-point')
  static void _onStart(ServiceInstance service) {
    // Periodic sync fallback — in case events are missed
    Timer.periodic(_periodicSyncInterval, (_) {
      service.invoke('sync');
    });

    // Listen for stop command from UI
    service.on('stop').listen((_) {
      service.stopSelf();
    });

    // The sync request is handled by the UI-side listener (see AppState).
    // The background service simply keeps the process alive and periodically
    // sends sync requests to the UI via invoke.
    service.on('sync').listen((_) {
      // Forward back to UI to trigger actual sync
      service.invoke('sync');
    });
  }
}
