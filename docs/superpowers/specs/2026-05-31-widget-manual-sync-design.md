# Widget Manual Sync Design

## Summary

Remove all background syncing infrastructure from the Obsyncian React Native app and replace it with an Android home screen widget featuring a manual sync button. The widget uses HeadlessJS to run sync in a short-lived foreground service triggered by user tap.

## Motivation

The background sync system (foreground service + 2s timer tick + SQS polling) does not reliably sync when the app is not in the foreground. Android aggressively kills persistent background services. A user-initiated widget tap is a fundamentally different pattern: short-lived, explicit user intent, and well-tolerated by Android's power management.

## What Gets Removed

- `src/services/backgroundService.ts` — foreground service, timer tick, battery optimization
- `src/services/sqsListenerService.ts` — SQS polling, SNS subscription
- `react-native-background-actions` dependency
- `react-native-background-timer` dependency
- `@aws-sdk/client-sqs` dependency
- `@aws-sdk/client-sns` dependency
- Foreground service permissions in AndroidManifest (`FOREGROUND_SERVICE`, `FOREGROUND_SERVICE_DATA_SYNC`, `WAKE_LOCK`, `REQUEST_IGNORE_BATTERY_OPTIMIZATIONS`, `POST_NOTIFICATIONS`)
- The `<service>` declaration for `RNBackgroundActionsTask` in AndroidManifest
- All `backgroundSyncService` references in `appStore.ts` and `App.tsx`
- The notification-based progress updates in `appStore.ts`

## What Stays

- `src/services/syncOrchestrator.ts` — core sync logic (unchanged)
- `src/services/s3SyncService.ts` — S3 upload/download
- `src/services/dynamoDBService.ts` — DynamoDB state tracking
- `src/services/fileStatCache.ts` — local file stat cache
- `src/services/connectivityService.ts` — network state detection
- `src/services/configService.ts` — config loading/saving
- `src/screens/HomeScreen.tsx` — Manual Sync button in-app
- App lifecycle listener (sync on foreground/background transitions)

## Widget Architecture

### Components

1. **`ObsyncianWidget.kt`** — `AppWidgetProvider` subclass. Renders the 2x1 widget, handles button tap via `onReceive()`, starts the foreground service.

2. **`SyncForegroundService.kt`** — Short-lived foreground service. Shows a "Syncing..." notification, starts HeadlessJS task, waits for completion signal, updates widget RemoteViews, stops itself.

3. **`ObsyncianWidgetModule.kt`** — Native module exposed to JS. Methods:
   - `setWidgetState(status: String, lastSyncTime: String)` — writes to SharedPreferences, triggers widget update
   - `getWidgetState(): Promise<Object>` — returns current widget state

4. **`src/services/syncHeadlessTask.ts`** — Registered HeadlessJS task that performs one sync cycle.

5. **Widget layout XML** — `res/layout/obsyncian_widget.xml`

6. **Widget metadata XML** — `res/xml/obsyncian_widget_info.xml`

### Data Flow

```
User taps widget sync button
  -> AppWidgetProvider.onReceive() receives SYNC_ACTION intent
  -> Starts SyncForegroundService
  -> Service shows "Syncing..." notification
  -> Service starts HeadlessJS task ("SyncTask")
  -> syncHeadlessTask.ts executes:
     1. Load config from AsyncStorage
     2. Check connectivity
     3. If offline: write "offline" to SharedPreferences, exit
     4. Init syncOrchestrator
     5. Run syncOrchestrator.sync(deviceId)
     6. Write result (status + timestamp) to SharedPreferences
     7. Signal completion
  -> Service receives completion
  -> Service updates widget RemoteViews with new state
  -> Service stops itself (notification dismissed)
```

### HeadlessJS Task Details

Runs in a fresh JS context (no shared state with running app). Must independently:
- Load config from AsyncStorage via `configService.loadSavedConfig()`
- Load vault path from AsyncStorage via `configService.getVaultPath()`
- Initialize `syncOrchestrator`
- Run sync
- Write results to SharedPreferences via `ObsyncianWidgetModule`

Registered in `index.js` via `AppRegistry.registerHeadlessTask('SyncTask', () => syncHeadlessTask)`.

### Shared State (SharedPreferences)

Key: `obsyncian_widget_state`

Values stored:
- `status`: one of `idle`, `syncing`, `error`, `offline`, `not_synced`
- `lastSyncTime`: ISO timestamp string of last successful sync

Both the HeadlessJS task (via native module) and the widget provider read/write this.

## Widget UI

### Layout (2x1)

```
+----------------------------------+
|  [sync icon]  |  * idle  2:35 PM |
+----------------------------------+
```

- Left: circular sync button with refresh icon, tappable
- Right: colored status dot + status text + last sync time

### Visual States

| State      | Dot Color | Display Text     |
|------------|-----------|------------------|
| idle       | Green     | idle * 2:35 PM   |
| syncing    | Yellow    | syncing...       |
| error      | Red       | error * 2:35 PM  |
| offline    | Red       | offline          |
| not_synced | Grey      | not synced       |

### Widget Metadata

- Min width: 2 cells (~110dp)
- Min height: 1 cell (~40dp)
- Not resizable
- Update period: 0 (manual updates only via SharedPreferences writes)
- Dark background, rounded corners, white text

## AndroidManifest Changes

### Permissions removed:
- `WAKE_LOCK`
- `REQUEST_IGNORE_BATTERY_OPTIMIZATIONS`

### Permissions kept (still needed for widget sync service):
- `FOREGROUND_SERVICE`
- `FOREGROUND_SERVICE_DATA_SYNC`
- `POST_NOTIFICATIONS`

### New declarations:
- `<receiver>` for `ObsyncianWidget` with `APPWIDGET_UPDATE` intent filter
- `<service>` for `SyncForegroundService` with `foregroundServiceType="dataSync"`
- `<meta-data>` pointing to `obsyncian_widget_info.xml`

## App Store Changes

The `appStore.ts` simplifications:
- Remove all `backgroundSyncService` imports and references
- Remove notification update logic from `setSyncState`
- Remove sync progress notification updates from `initializeAppStore`
- Remove `sqsListenerService` callback setup
- Remove `backgroundSyncService.setOnSyncRequest` callback
- Keep connectivity monitoring (reconnect triggers sync if app is open)

## App.tsx Changes

- Remove `backgroundSyncService` import and lifecycle management
- Keep `syncOrchestrator` initialization when config is available
- Keep app lifecycle listener (sync on foreground/background)
- Remove the re-init effect that restarts the background service on config change (replace with just re-initializing syncOrchestrator)

## Error Handling

- If HeadlessJS task fails: status written as "error", widget shows red dot
- If device is offline: detected before sync attempt, widget shows "offline"
- If config not loaded: task exits early, widget shows "not synced"
- Android kills the service after ~60s: acceptable since sync typically completes in 10-30s. If it's killed mid-sync, the next manual tap will complete it.

## Testing

- Unit test the headless task logic (mock syncOrchestrator)
- Manual test: add widget to home screen, tap sync, verify files sync
- Manual test: tap sync while offline, verify "offline" state shown
- Manual test: tap sync with app open simultaneously, verify no crash
