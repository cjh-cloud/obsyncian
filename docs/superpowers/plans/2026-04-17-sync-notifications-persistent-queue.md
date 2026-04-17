# Sync Notifications & Persistent SQS Queue Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add status bar notifications for sync events and stop deleting the SQS queue on app stop.

**Architecture:** Two independent changes. (1) A new `SyncNotificationService` wraps `flutter_local_notifications` to show/update a notification during sync. The `S3SyncService` and `SyncOrchestrator` are modified to return structured results so counts can be displayed. `AppState` wires everything together. (2) The SQS listener's `_cleanup()` method is changed to only unsubscribe from SNS, leaving the queue intact.

**Tech Stack:** Flutter/Dart, `flutter_local_notifications`, AWS SQS/SNS via `aws_sqs_api`/`aws_sns_api`

**Spec:** `docs/superpowers/specs/2026-04-17-sync-notifications-persistent-queue-design.md`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `android/lib/services/sync_notification_service.dart` | **New** — wraps `flutter_local_notifications`, manages a single notification ID for sync status |
| `android/lib/services/s3_sync_service.dart` | **Modify** — `syncUp()` and `syncDown()` return `SyncDiff` instead of `void` |
| `android/lib/services/sync_orchestrator.dart` | **Modify** — new `SyncResult` class, `sync()` returns `Future<SyncResult>`, `_handleSync()` returns `SyncResult` |
| `android/lib/providers/app_state.dart` | **Modify** — owns `SyncNotificationService`, calls notification methods around `triggerSync()` |
| `android/lib/services/sqs_listener_service.dart` | **Modify** — remove `deleteQueue` call from `_cleanup()` |
| `android/test/services/sync_notification_service_test.dart` | **New** — unit tests for notification service |
| `android/test/services/s3_sync_service_test.dart` | **New** — unit tests for SyncDiff return values |
| `android/test/services/sync_orchestrator_test.dart` | **New** — unit tests for SyncResult return values |

---

### Task 1: Persistent SQS Queue — Remove `deleteQueue` from `_cleanup()`

This is the simplest change and fully independent of the notification work.

**Files:**
- Modify: `android/lib/services/sqs_listener_service.dart:243-266`

- [ ] **Step 1: Modify `_cleanup()` to skip queue deletion**

In `android/lib/services/sqs_listener_service.dart`, replace the `_cleanup()` method (lines 243-266) with a version that only unsubscribes from SNS:

```dart
  Future<void> _cleanup() async {
    // Unsubscribe from SNS
    if (_subscriptionArn.isNotEmpty &&
        _subscriptionArn != 'pending confirmation') {
      try {
        await _snsClient.unsubscribe(subscriptionArn: _subscriptionArn);
      } catch (e) {
        _log('Failed to unsubscribe from SNS: $e');
      }
    }

    // Queue is intentionally NOT deleted — it persists across app restarts.
    // createQueue() is idempotent and will reuse the existing queue on next start.

    _queueUrl = '';
    _queueArn = '';
    _subscriptionArn = '';
  }
```

- [ ] **Step 2: Verify the app builds**

Run from `android/`:
```bash
cd android && flutter build apk --debug 2>&1 | tail -5
```
Expected: `BUILD SUCCESSFUL`

- [ ] **Step 3: Commit**

```bash
git add android/lib/services/sqs_listener_service.dart
git commit -m "feat: keep SQS queue alive across app restarts

Remove deleteQueue call from _cleanup(). The queue persists permanently
per device. createQueue() is idempotent and reuses the existing queue
on restart. SNS subscription is still dropped on stop and re-created
on start."
```

---

### Task 2: Modify `S3SyncService` to return `SyncDiff` from `syncUp()` and `syncDown()`

**Files:**
- Modify: `android/lib/services/s3_sync_service.dart:91-142`
- Test: `android/test/services/s3_sync_service_test.dart`

- [ ] **Step 1: Write unit tests for `SyncDiff` return values**

Create `android/test/services/s3_sync_service_test.dart`:

```dart
import 'package:flutter_test/flutter_test.dart';
import 'package:obsyncian/services/s3_sync_service.dart';

void main() {
  group('SyncDiff', () {
    test('hasChanges is true when there are uploads', () {
      const diff = SyncDiff(toUpload: ['a.md'], toDownload: [], toDelete: []);
      expect(diff.hasChanges, isTrue);
    });

    test('hasChanges is true when there are downloads', () {
      const diff = SyncDiff(toUpload: [], toDownload: ['b.md'], toDelete: []);
      expect(diff.hasChanges, isTrue);
    });

    test('hasChanges is true when there are deletes', () {
      const diff = SyncDiff(toUpload: [], toDownload: [], toDelete: ['c.md']);
      expect(diff.hasChanges, isTrue);
    });

    test('hasChanges is false when empty', () {
      const diff = SyncDiff(toUpload: [], toDownload: [], toDelete: []);
      expect(diff.hasChanges, isFalse);
    });

    test('toString includes counts', () {
      const diff = SyncDiff(
        toUpload: ['a.md', 'b.md'],
        toDownload: ['c.md'],
        toDelete: [],
      );
      expect(diff.toString(), contains('upload: 2'));
      expect(diff.toString(), contains('download: 1'));
      expect(diff.toString(), contains('delete: 0'));
    });
  });
}
```

- [ ] **Step 2: Run the test to verify it passes**

The `SyncDiff` class already exists and these tests exercise it. Run:
```bash
cd android && flutter test test/services/s3_sync_service_test.dart -v
```
Expected: All 5 tests PASS (these test the existing `SyncDiff` class)

- [ ] **Step 3: Modify `syncUp()` to return `SyncDiff`**

In `android/lib/services/s3_sync_service.dart`, replace the `syncUp()` method (lines 91-115) with:

```dart
  /// Sync from local to S3 (upload local changes, delete remote orphans).
  /// Returns a [SyncDiff] describing what was synced.
  Future<SyncDiff> syncUp() async {
    _log('Syncing local -> S3...');
    final localEntries = await _listLocalFiles();
    final remoteEntries = await _listRemoteObjects();

    final localMap = {for (final e in localEntries) e.key: e};
    final remoteMap = {for (final e in remoteEntries) e.key: e};

    final uploaded = <String>[];
    final deleted = <String>[];

    // Upload new/changed files
    for (final entry in localEntries) {
      final remote = remoteMap[entry.key];
      if (remote == null || remote.etag != entry.etag) {
        await _uploadFile(entry.key);
        uploaded.add(entry.key);
      }
    }

    // Delete remote files not present locally (WithDelete semantics)
    for (final key in remoteMap.keys) {
      if (!localMap.containsKey(key)) {
        await _deleteRemoteObject(key);
        deleted.add(key);
      }
    }

    _log('Sync up complete.');
    return SyncDiff(toUpload: uploaded, toDownload: [], toDelete: deleted);
  }
```

- [ ] **Step 4: Modify `syncDown()` to return `SyncDiff`**

In `android/lib/services/s3_sync_service.dart`, replace the `syncDown()` method (lines 118-142) with:

```dart
  /// Sync from S3 to local (download remote changes, delete local orphans).
  /// Returns a [SyncDiff] describing what was synced.
  Future<SyncDiff> syncDown() async {
    _log('Syncing S3 -> local...');
    final localEntries = await _listLocalFiles();
    final remoteEntries = await _listRemoteObjects();

    final localMap = {for (final e in localEntries) e.key: e};
    final remoteMap = {for (final e in remoteEntries) e.key: e};

    final downloaded = <String>[];
    final deleted = <String>[];

    // Download new/changed files
    for (final entry in remoteEntries) {
      final local = localMap[entry.key];
      if (local == null || local.etag != entry.etag) {
        await _downloadFile(entry.key);
        downloaded.add(entry.key);
      }
    }

    // Delete local files not present in S3 (WithDelete semantics)
    for (final key in localMap.keys) {
      if (!remoteMap.containsKey(key)) {
        _deleteLocalFile(key);
        deleted.add(key);
      }
    }

    _log('Sync down complete.');
    return SyncDiff(toUpload: [], toDownload: downloaded, toDelete: deleted);
  }
```

- [ ] **Step 5: Verify the app builds**

```bash
cd android && flutter build apk --debug 2>&1 | tail -5
```
Expected: `BUILD SUCCESSFUL`

- [ ] **Step 6: Run tests**

```bash
cd android && flutter test test/services/s3_sync_service_test.dart -v
```
Expected: All tests PASS

- [ ] **Step 7: Commit**

```bash
git add android/lib/services/s3_sync_service.dart android/test/services/s3_sync_service_test.dart
git commit -m "feat: return SyncDiff from syncUp() and syncDown()

Both methods now return a SyncDiff with the list of files uploaded,
downloaded, and deleted. No new AWS calls — counts come from the
existing diff computation. This enables notifications to show file
change summaries."
```

---

### Task 3: Modify `SyncOrchestrator` to return `SyncResult`

**Files:**
- Modify: `android/lib/services/sync_orchestrator.dart`
- Test: `android/test/services/sync_orchestrator_test.dart`

- [ ] **Step 1: Write unit tests for `SyncResult`**

Create `android/test/services/sync_orchestrator_test.dart`:

```dart
import 'package:flutter_test/flutter_test.dart';
import 'package:obsyncian/services/sync_orchestrator.dart';

void main() {
  group('SyncResult', () {
    test('hadChanges is true when files were uploaded', () {
      const result = SyncResult(uploaded: 3, downloaded: 0, deleted: 0);
      expect(result.hadChanges, isTrue);
    });

    test('hadChanges is true when files were downloaded', () {
      const result = SyncResult(uploaded: 0, downloaded: 2, deleted: 0);
      expect(result.hadChanges, isTrue);
    });

    test('hadChanges is true when files were deleted', () {
      const result = SyncResult(uploaded: 0, downloaded: 0, deleted: 1);
      expect(result.hadChanges, isTrue);
    });

    test('hadChanges is false when no changes', () {
      const result = SyncResult(uploaded: 0, downloaded: 0, deleted: 0);
      expect(result.hadChanges, isFalse);
    });

    test('noChanges factory returns zero counts', () {
      const result = SyncResult.noChanges();
      expect(result.uploaded, 0);
      expect(result.downloaded, 0);
      expect(result.deleted, 0);
      expect(result.hadChanges, isFalse);
    });
  });
}
```

- [ ] **Step 2: Add `SyncResult` class to `sync_orchestrator.dart`**

In `android/lib/services/sync_orchestrator.dart`, add the `SyncResult` class after the `SyncState` enum (after line 10):

```dart
/// Result of a sync cycle, with counts of files changed.
class SyncResult {
  final int uploaded;
  final int downloaded;
  final int deleted;

  bool get hadChanges => uploaded > 0 || downloaded > 0 || deleted > 0;

  const SyncResult({
    required this.uploaded,
    required this.downloaded,
    required this.deleted,
  });

  const SyncResult.noChanges()
      : uploaded = 0,
        downloaded = 0,
        deleted = 0;
}
```

- [ ] **Step 3: Run tests to verify `SyncResult` works**

```bash
cd android && flutter test test/services/sync_orchestrator_test.dart -v
```
Expected: All 5 tests PASS

- [ ] **Step 4: Modify `sync()` to return `Future<SyncResult>`**

In `android/lib/services/sync_orchestrator.dart`, replace the `sync()` method (lines 64-86) with:

```dart
  /// Run a full sync cycle. If a sync is already in progress, queues another
  /// cycle to run when the current one completes.
  /// Returns a [SyncResult] with counts of files changed, or null if the
  /// sync was queued (not executed immediately).
  Future<SyncResult?> sync() async {
    if (_state == SyncState.syncing) {
      _isSyncQueued = true;
      _log('Sync already in progress, queued another cycle.');
      return null;
    }

    _setState(SyncState.syncing);

    SyncResult result;
    try {
      result = await _handleSync();
      _setState(SyncState.idle);
    } catch (e) {
      _log('Sync error: $e');
      _setState(SyncState.error);
      rethrow;
    }

    // If another sync was requested while we were busy, run it now.
    if (_isSyncQueued) {
      _isSyncQueued = false;
      sync(); // fire-and-forget; it will set state itself
    }

    return result;
  }
```

- [ ] **Step 5: Modify `_handleSync()` to return `SyncResult`**

In `android/lib/services/sync_orchestrator.dart`, replace `_handleSync()` (lines 95-166) with:

```dart
  /// The core sync algorithm, matching `handleSyncAsync` from the Go app.
  ///
  /// After syncing down, the dry-run check is skipped for this cycle to prevent
  /// a race condition: if S3 changes between our sync-down and the dry-run,
  /// the diff would be misinterpreted as local changes. The next sync trigger
  /// (SQS notification, manual button, or lifecycle event) will handle any
  /// genuine local changes.
  Future<SyncResult> _handleSync() async {
    _log('Last local sync: ${DateTime.now()}');
    _log('Config: region=${_config.awsRegion}, bucket=${_config.cloud}, '
        'key=${_config.credentials.key.substring(0, 4)}...');

    // 1. Get latest cloud sync
    _log('Scanning DynamoDB for latest sync...');
    final latestSync = await _dynamoDBService.getLatestSync();

    if (latestSync == null) {
      // Table is empty — this must be the first device ever.
      _log('Table is empty. Syncing up...');
      final diff = await _s3Service.syncUp();
      await _dynamoDBService.createUser(_config.id);
      await _dynamoDBService.updateTimestamp(_config.id);
      _log('Finished syncing up (new table).');
      return SyncResult(
        uploaded: diff.toUpload.length,
        downloaded: 0,
        deleted: diff.toDelete.length,
      );
    }

    _log('Latest cloud sync: ${latestSync.timestamp} by ${latestSync.userId}');

    // 2. Check if our user exists
    final ourItem = await _dynamoDBService.getUser(_config.id);

    if (ourItem == null) {
      _log('User ${_config.id} not found in table. Creating & syncing down...');
      await _dynamoDBService.createUser(_config.id);
      _log('Syncing down from S3...');
      final diff = await _s3Service.syncDown();
      await _saveLastSyncedTimestamp(latestSync.timestamp);
      _log('Finished syncing down (new user).');
      return SyncResult(
        uploaded: 0,
        downloaded: diff.toDownload.length,
        deleted: diff.toDelete.length,
      );
    }

    // 3. Check if we need to sync down
    final needsSyncDown = _config.id != latestSync.userId &&
        latestSync.timestamp.compareTo(ourItem.timestamp) >= 0 &&
        _lastSyncedTimestamp.compareTo(latestSync.timestamp) < 0;

    if (needsSyncDown) {
      _log('Not synced with Cloud. Syncing down from S3...');
      final diff = await _s3Service.syncDown();
      await _saveLastSyncedTimestamp(latestSync.timestamp);
      _log('Finished syncing down.');
      return SyncResult(
        uploaded: 0,
        downloaded: diff.toDownload.length,
        deleted: diff.toDelete.length,
      );
    }

    _log('Already synced with Cloud.');

    // 4. Skip the dry-run if we just synced down — handled above with early returns.
    // 5. Check for local changes via dry-run
    _log('Checking for local changes (dry-run S3 diff)...');
    final diff = await _s3Service.dryRunSyncUp();

    if (diff.hasChanges) {
      _log('Local changes detected: $diff. Syncing up...');
      await _s3Service.syncUp();
      await _dynamoDBService.updateTimestamp(_config.id);
      _log('Finished syncing up.');
      return SyncResult(
        uploaded: diff.toUpload.length,
        downloaded: 0,
        deleted: diff.toDelete.length,
      );
    }

    _log('No local changes to sync.');
    return const SyncResult.noChanges();
  }
```

- [ ] **Step 6: Verify the app builds**

```bash
cd android && flutter build apk --debug 2>&1 | tail -5
```
Expected: `BUILD SUCCESSFUL`

- [ ] **Step 7: Run tests**

```bash
cd android && flutter test test/services/sync_orchestrator_test.dart -v
```
Expected: All tests PASS

- [ ] **Step 8: Commit**

```bash
git add android/lib/services/sync_orchestrator.dart android/test/services/sync_orchestrator_test.dart
git commit -m "feat: return SyncResult from sync orchestrator

sync() now returns Future<SyncResult?> with counts of uploaded,
downloaded, and deleted files. Returns null when a sync is queued
(not immediately executed). All 5 code paths in _handleSync()
produce a SyncResult from the SyncDiff returned by S3SyncService."
```

---

### Task 4: Create `SyncNotificationService`

**Files:**
- Create: `android/lib/services/sync_notification_service.dart`
- Test: `android/test/services/sync_notification_service_test.dart`

- [ ] **Step 1: Write unit tests for notification message formatting**

Create `android/test/services/sync_notification_service_test.dart`:

```dart
import 'package:flutter_test/flutter_test.dart';
import 'package:obsyncian/services/sync_notification_service.dart';
import 'package:obsyncian/services/sync_orchestrator.dart';

void main() {
  group('SyncNotificationService.formatCompletionBody', () {
    test('shows "no changes" when nothing changed', () {
      const result = SyncResult.noChanges();
      expect(
        SyncNotificationService.formatCompletionBody(result),
        'No changes',
      );
    });

    test('shows upload count only', () {
      const result = SyncResult(uploaded: 3, downloaded: 0, deleted: 0);
      expect(
        SyncNotificationService.formatCompletionBody(result),
        '3 uploaded',
      );
    });

    test('shows download count only', () {
      const result = SyncResult(uploaded: 0, downloaded: 2, deleted: 0);
      expect(
        SyncNotificationService.formatCompletionBody(result),
        '2 downloaded',
      );
    });

    test('shows delete count only', () {
      const result = SyncResult(uploaded: 0, downloaded: 0, deleted: 1);
      expect(
        SyncNotificationService.formatCompletionBody(result),
        '1 deleted',
      );
    });

    test('shows multiple counts joined', () {
      const result = SyncResult(uploaded: 3, downloaded: 2, deleted: 1);
      expect(
        SyncNotificationService.formatCompletionBody(result),
        '3 uploaded, 2 downloaded, 1 deleted',
      );
    });

    test('shows upload and download without delete', () {
      const result = SyncResult(uploaded: 1, downloaded: 4, deleted: 0);
      expect(
        SyncNotificationService.formatCompletionBody(result),
        '1 uploaded, 4 downloaded',
      );
    });
  });
}
```

- [ ] **Step 2: Create the `SyncNotificationService` class**

Create `android/lib/services/sync_notification_service.dart`:

```dart
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
    const androidSettings =
        AndroidInitializationSettings('@mipmap/ic_launcher');
    const settings = InitializationSettings(android: androidSettings);
    await _plugin.initialize(settings);

    // Request notification permission (Android 13+). If denied, we silently
    // skip notifications — syncs still work.
    final status = await Permission.notification.request();
    _hasPermission = status.isGranted;
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
      _notificationId,
      'Obsyncian',
      'Syncing your vault...',
      const NotificationDetails(android: details),
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
      _notificationId,
      'Sync complete',
      body,
      NotificationDetails(android: details),
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
      _notificationId,
      'Sync failed',
      error,
      NotificationDetails(android: details),
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
```

- [ ] **Step 3: Run the tests**

```bash
cd android && flutter test test/services/sync_notification_service_test.dart -v
```
Expected: All 6 tests PASS

- [ ] **Step 4: Verify the app builds**

```bash
cd android && flutter build apk --debug 2>&1 | tail -5
```
Expected: `BUILD SUCCESSFUL`

- [ ] **Step 5: Commit**

```bash
git add android/lib/services/sync_notification_service.dart android/test/services/sync_notification_service_test.dart
git commit -m "feat: add SyncNotificationService for sync status notifications

New service wrapping flutter_local_notifications. Shows a notification
on sync start, updates it on completion with file counts or error.
Uses a single notification ID to prevent stacking. Auto-dismisses
completion/failure after 10 seconds. Silently skips if notification
permission is denied."
```

---

### Task 5: Wire up notifications in `AppState`

**Files:**
- Modify: `android/lib/providers/app_state.dart`

- [ ] **Step 1: Add the import**

In `android/lib/providers/app_state.dart`, add the import after the existing service imports (after line 15):

```dart
import '../services/sync_notification_service.dart';
```

- [ ] **Step 2: Add the service instance**

In `android/lib/providers/app_state.dart`, add the service field after the existing service fields. After the line:

```dart
  final ConnectivityService _connectivity = ConnectivityService();
```

Add:

```dart
  final SyncNotificationService _notifications = SyncNotificationService();
```

- [ ] **Step 3: Initialise the notification service**

In `android/lib/providers/app_state.dart`, inside the `initialise()` method, add the notification initialisation. After the line:

```dart
    WidgetsBinding.instance.addObserver(this);
```

Add:

```dart
    // Initialise sync notifications (requests permission on Android 13+)
    await _notifications.initialise();
```

- [ ] **Step 4: Update `triggerSync()` to show notifications**

In `android/lib/providers/app_state.dart`, replace the `triggerSync()` method (lines 181-191) with:

```dart
  /// Manually trigger a sync cycle.
  Future<void> triggerSync() async {
    if (!isConfigured) {
      _addLog('Cannot sync: no config loaded.');
      return;
    }
    if (!_connectivity.isOnline) {
      _addLog('Cannot sync: device is offline.');
      return;
    }

    await _notifications.showSyncStarted();
    try {
      final result = await _orchestrator.sync();
      if (result != null) {
        await _notifications.showSyncCompleted(result);
      }
    } catch (e) {
      await _notifications.showSyncFailed('$e');
    }
  }
```

- [ ] **Step 5: Verify the app builds**

```bash
cd android && flutter build apk --debug 2>&1 | tail -5
```
Expected: `BUILD SUCCESSFUL`

- [ ] **Step 6: Run all tests**

```bash
cd android && flutter test -v
```
Expected: All tests PASS

- [ ] **Step 7: Commit**

```bash
git add android/lib/providers/app_state.dart
git commit -m "feat: wire up sync notifications in AppState

AppState now owns a SyncNotificationService. triggerSync() shows a
'Syncing...' notification before starting and updates it with results
or error on completion. Notification permission is requested during
initialise()."
```

---

### Task 6: Final integration verification

- [ ] **Step 1: Run full test suite**

```bash
cd android && flutter test -v
```
Expected: All tests PASS

- [ ] **Step 2: Build release APK**

```bash
cd android && flutter build apk --release 2>&1 | tail -10
```
Expected: `BUILD SUCCESSFUL`

- [ ] **Step 3: Verify no analyzer warnings in changed files**

```bash
cd android && flutter analyze lib/services/sync_notification_service.dart lib/services/s3_sync_service.dart lib/services/sync_orchestrator.dart lib/providers/app_state.dart lib/services/sqs_listener_service.dart
```
Expected: `No issues found!`

- [ ] **Step 4: Final commit (if any fixes were needed)**

Only commit if there were fixes needed from the above checks. Otherwise, skip this step.
