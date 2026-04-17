# Sync Notifications & Persistent SQS Queue

**Date:** 2026-04-17
**Status:** Draft

## Overview

Two changes to the Android Flutter app:

1. **Sync notifications** — Show a status bar notification when a sync starts, update it on completion with a file change summary (or error).
2. **Persistent SQS queue** — Stop deleting the SQS queue when the listener stops. Keep the queue alive permanently per device; only drop the SNS subscription on stop.

Both changes must not degrade performance.

## Feature 1: Sync Notifications

### New: `SyncNotificationService`

A new service class at `android/lib/services/sync_notification_service.dart` wrapping `flutter_local_notifications` (already a dependency in pubspec).

**Public API:**

```dart
class SyncNotificationService {
  Future<void> initialise();           // Set up notification channel, request permission
  Future<void> showSyncStarted();      // "Syncing your vault..."
  Future<void> showSyncCompleted(SyncResult result); // "Sync complete — 3 uploaded, 2 downloaded"
  Future<void> showSyncFailed(String error);          // "Sync failed — <error>"
}
```

**Notification behavior:**

- Uses a single notification ID so updates replace (not stack) the previous notification
- Uses a dedicated Android notification channel (e.g. `obsyncian_sync_status`) separate from the foreground service channel
- Completion/failure notifications auto-dismiss after ~10 seconds via `timeoutAfter` on `AndroidNotificationDetails`
- If the sync finds no changes, shows "Sync complete — no changes"
- If `POST_NOTIFICATIONS` permission is denied (Android 13+), notifications are silently skipped — syncs still work

### Modified: `S3SyncService` — return `SyncDiff` from sync operations

`syncUp()` and `syncDown()` currently return `Future<void>`. They already compute local-vs-remote diffs internally but discard the counts. Change both to return `Future<SyncDiff>` (the `SyncDiff` class already exists in this file with `toUpload`, `toDownload`, `toDelete` lists).

No additional AWS calls — the diffs are already computed before performing operations.

### Modified: `SyncOrchestrator` — return `SyncResult`

The `sync()` method currently returns `Future<void>`. Change it to return `Future<SyncResult>`:

```dart
class SyncResult {
  final int uploaded;
  final int downloaded;
  final int deleted;
  final bool hadChanges;
}
```

The orchestrator has 5 code paths in `_handleSync()`, each producing a `SyncResult`:

1. **Empty table (first device):** `syncUp()` returns `SyncDiff` — use `toUpload.length` for uploaded count
2. **New user:** `syncDown()` returns `SyncDiff` — use `toDownload.length` for downloaded count
3. **Another user synced more recently:** `syncDown()` returns `SyncDiff` — same as above
4. **Dry-run detects local changes:** `dryRunSyncUp()` already returns `SyncDiff` — use those counts
5. **No changes:** return `SyncResult(0, 0, 0, hadChanges: false)`

### Modified: `AppState` — wire up notifications

- Owns a `SyncNotificationService` instance
- Calls `initialise()` during `AppState.initialise()` (requests notification permission)
- In `triggerSync()`: call `showSyncStarted()` before `_orchestrator.sync()`, then `showSyncCompleted(result)` or `showSyncFailed(error)` based on the outcome

### Performance impact

None. Posting an Android notification is a single platform channel call (sub-millisecond). The `SyncResult` data class adds no AWS calls — counts come from existing diff computation.

## Feature 2: Persistent SQS Queue

### Modified: `SQSListenerService._cleanup()`

**Current behavior (lines 243-266 of `sqs_listener_service.dart`):**
1. Unsubscribe from SNS
2. Delete the SQS queue

**New behavior:**
1. Unsubscribe from SNS
2. ~~Delete the SQS queue~~ — removed

The queue (`obsyncian-{deviceId}`) persists permanently. On restart:

- `_createQueue()` is idempotent — AWS returns the existing queue URL if the name matches
- `_subscribeToSNS()` re-subscribes (also idempotent)
- `setQueueAttributes` re-applies the policy (no-op if unchanged)

### Queue configuration (unchanged)

- Message retention: 5 minutes (no change — subscription is dropped on stop, so no messages accumulate while offline)
- Long poll wait: 20 seconds
- Visibility timeout: 30 seconds

### Performance impact

Positive — removing `deleteQueue` eliminates one AWS API call on stop.

## Files Changed

| File | Change |
|------|--------|
| `android/lib/services/sync_notification_service.dart` | **New** — notification wrapper |
| `android/lib/services/s3_sync_service.dart` | Return `SyncDiff` from `syncUp()` and `syncDown()` |
| `android/lib/services/sync_orchestrator.dart` | Return `SyncResult` from `sync()`, aggregate diffs |
| `android/lib/providers/app_state.dart` | Wire up `SyncNotificationService`, use `SyncResult` |
| `android/lib/services/sqs_listener_service.dart` | Remove `deleteQueue` from `_cleanup()` |

## Edge Cases

- **Rapid syncs:** Single notification ID prevents stacking — each sync replaces the previous notification
- **No changes detected:** Shows "Sync complete — no changes" instead of zeros
- **Notification permission denied:** Syncs work normally, notifications silently skipped
- **Queue attributes mismatch on restart:** `setQueueAttributes` on each start ensures policy is always correct
- **App killed by OS:** Queue survives. On next launch, `createQueue` finds the existing queue. No orphaned resources.

## What Does NOT Change

- Foreground service notification (ID 888) — untouched
- Background sync timer (5-minute fallback) — untouched
- Sync orchestrator algorithm — unchanged, only return type changes
- No new dependencies — `flutter_local_notifications` already in pubspec
