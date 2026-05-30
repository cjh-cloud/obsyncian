import 'package:flutter_local_notifications/flutter_local_notifications.dart';
import 'package:permission_handler/permission_handler.dart';

import 'sync_orchestrator.dart';

/// Manages status bar notifications for sync events.
///
/// Uses a single notification ID so each sync replaces the previous
/// notification rather than stacking. The notification channel is separate
/// from the foreground service channel.
class SyncNotificationService {
  static const _notificationId = 100;
  static const _channelId = 'obsyncian_sync_status';
  static const _channelName = 'Sync Status';
  static const _channelDescription = 'Notifications for vault sync events';

  /// Auto-dismiss timeout for completion/failure notifications.
  static const _dismissTimeout = Duration(seconds: 10);

  final FlutterLocalNotificationsPlugin _plugin =
      FlutterLocalNotificationsPlugin();

  bool _hasPermission = false;

  /// Initialise the notification plugin and request permission.
  Future<void> initialise() async {
    try {
      const androidSettings =
          AndroidInitializationSettings('@mipmap/ic_launcher');
      const settings = InitializationSettings(android: androidSettings);
      await _plugin.initialize(settings: settings);

      // Request notification permission (Android 13+). If denied, we silently
      // skip notifications — syncs still work.
      final status = await Permission.notification.request();
      _hasPermission = status.isGranted;
    } catch (_) {
      // Platform not available (e.g., in tests). Notifications disabled.
      _hasPermission = false;
    }
  }

  /// Show a "Syncing your vault..." notification.
  Future<void> showSyncStarted() async {
    if (!_hasPermission) return;

    const details = AndroidNotificationDetails(
      _channelId,
      _channelName,
      channelDescription: _channelDescription,
      importance: Importance.low,
      priority: Priority.low,
      ongoing: true,
      autoCancel: false,
    );

    await _plugin.show(
      id: _notificationId,
      title: 'Obsyncian',
      body: 'Syncing your vault...',
      notificationDetails: const NotificationDetails(android: details),
    );
  }

  /// Update the notification with sync results.
  Future<void> showSyncCompleted(SyncResult result) async {
    if (!_hasPermission) return;

    final body = formatCompletionBody(result);

    final details = AndroidNotificationDetails(
      _channelId,
      _channelName,
      channelDescription: _channelDescription,
      importance: Importance.low,
      priority: Priority.low,
      ongoing: false,
      autoCancel: true,
      timeoutAfter: _dismissTimeout.inMilliseconds,
    );

    await _plugin.show(
      id: _notificationId,
      title: 'Sync complete',
      body: body,
      notificationDetails: NotificationDetails(android: details),
    );
  }

  /// Update the notification with an error message.
  Future<void> showSyncFailed(String error) async {
    if (!_hasPermission) return;

    final details = AndroidNotificationDetails(
      _channelId,
      _channelName,
      channelDescription: _channelDescription,
      importance: Importance.defaultImportance,
      priority: Priority.defaultPriority,
      ongoing: false,
      autoCancel: true,
      timeoutAfter: _dismissTimeout.inMilliseconds,
    );

    await _plugin.show(
      id: _notificationId,
      title: 'Sync failed',
      body: error,
      notificationDetails: NotificationDetails(android: details),
    );
  }

  /// Format the body text for a sync completion notification.
  ///
  /// Exposed as a static method for testability (no plugin dependency).
  static String formatCompletionBody(SyncResult result) {
    if (!result.hadChanges) return 'No changes';

    final parts = <String>[];
    if (result.uploaded > 0) parts.add('${result.uploaded} uploaded');
    if (result.downloaded > 0) parts.add('${result.downloaded} downloaded');
    if (result.deleted > 0) parts.add('${result.deleted} deleted');
    return parts.join(', ');
  }
}
