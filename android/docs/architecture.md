# Obsyncian Android App - Architecture

## High-Level Architecture

```
┌─────────────────────────────────────────┐
│                 UI Layer                │
│              HomeScreen                 │
│   (config picker, sync log, status)     │
└──────────────┬──────────────────────────┘
               │ Provider (ChangeNotifier)
┌──────────────▼──────────────────────────┐
│              AppState                    │
│  (owns services, lifecycle observer)     │
└──┬──────┬──────┬──────┬──────┬──────────┘
   │      │      │      │      │
   ▼      ▼      ▼      ▼      ▼
Config  Sync   S3Sync  Dynamo  SQS    Background
Service Orch.  Service  DB    Listener  Sync
               Service        Service  Service
                  │      │      │
                  ▼      ▼      ▼
              ┌──────────────────┐
              │    AWS Cloud     │
              │ S3  DynamoDB     │
              │ SQS  SNS        │
              └──────────────────┘
```

## Component Descriptions

### UI Layer

| Component | File | Responsibility |
|-----------|------|----------------|
| `HomeScreen` | `lib/screens/home_screen.dart` | Single-screen UI: config picker (unconfigured), sync status bar, config summary card, scrollable log, manual sync FAB. |

### State Management

| Component | File | Responsibility |
|-----------|------|----------------|
| `AppState` | `lib/providers/app_state.dart` | Central `ChangeNotifier` with `WidgetsBindingObserver`. Owns service instances, wires callbacks (lifecycle -> sync, reconnect -> sync, SQS notification -> sync). Exposes config, sync state, connectivity, and log entries to the UI. |

### Service Layer

| Service | File | Responsibility |
|---------|------|----------------|
| `ConfigService` | `lib/services/config_service.dart` | Loads `config.json` via file picker; persists the selected path in `SharedPreferences`. |
| `SyncOrchestrator` | `lib/services/sync_orchestrator.dart` | Core sync algorithm (mirrors Go's `handleSyncAsync`). Coordinates S3SyncService and DynamoDBService. |
| `S3SyncService` | `lib/services/s3_sync_service.dart` | Lists local/remote files, computes diffs, uploads, downloads, deletes. Implements bidirectional sync with delete semantics. |
| `DynamoDBService` | `lib/services/dynamodb_service.dart` | Scans for latest sync timestamp, gets/creates/updates user items. |
| `SQSListenerService` | `lib/services/sqs_listener_service.dart` | Creates temp SQS queue, subscribes to SNS, long-polls for messages, debounces notifications, cleans up on stop. |
| `ConnectivityService` | `lib/services/connectivity_service.dart` | Monitors network state via `connectivity_plus`. Fires `onReconnect` callback on offline-to-online transition. |
| `BackgroundSyncService` | `lib/services/background_service.dart` | Android foreground service via `flutter_background_service`. Periodic 5-minute sync fallback. |

### Models

| Model | File | Responsibility |
|-------|------|----------------|
| `ObsyncianConfig` | `lib/models/obsyncian_config.dart` | Config model matching Go's `ObsyncianConfig` struct. JSON serialisation. |
| `Credentials` | `lib/models/obsyncian_config.dart` | AWS access key + secret key pair. |

## Data Flow: Sync Cycle

1. **Trigger**: SQS notification / app lifecycle (resume/pause) / manual button / periodic timer / reconnect
2. `AppState.triggerSync()` -> `SyncOrchestrator.sync()`
3. `SyncOrchestrator._handleSync()`:
   a. `DynamoDBService.getLatestSync()` — scan table for newest timestamp
   b. Decide: sync down or check local changes (never both in one cycle)
   c. `S3SyncService.syncDown()` or `S3SyncService.dryRunSyncUp()` + `S3SyncService.syncUp()`
   d. `DynamoDBService.updateTimestamp()` after successful sync up
4. Progress messages streamed to `AppState._logs` -> UI rebuilds

## Sync Algorithm

```
scan DynamoDB for latest timestamp
if table empty:
    sync up (local -> S3)
    create user entry
    return

get our user entry
if user not found:
    create user entry
    sync down (S3 -> local)
    return  <-- skip dry-run after sync-down

if another user synced more recently AND we haven't seen that timestamp:
    sync down (S3 -> local)
    return  <-- skip dry-run after sync-down

dry-run: compare local files vs S3 objects
if local changes detected:
    sync up (local -> S3)
    update our timestamp in DynamoDB
```

The dry-run is intentionally skipped after a sync-down. This prevents a race
condition where S3 changes between the sync-down and the dry-run would be
misinterpreted as local edits, causing the mobile to overwrite newer cloud
content. Local changes are picked up on the next sync trigger.

`_lastSyncedTimestamp` is persisted in SharedPreferences to survive app restarts.

## Key Packages

| Package | Purpose |
|---------|---------|
| `aws_s3_api` | S3 operations (ListObjectsV2, PutObject, GetObject, DeleteObject) |
| `aws_dynamodb_api` | DynamoDB operations (Scan, GetItem, PutItem, UpdateItem) |
| `aws_sqs_api` | SQS operations (CreateQueue, ReceiveMessage, DeleteMessage, etc.) |
| `aws_sns_api` | SNS operations (Subscribe, Unsubscribe) |
| `file_picker` | Android SAF-compatible file picker |
| `connectivity_plus` | Network state monitoring |
| `flutter_background_service` | Android foreground service |
| `provider` | State management |
| `shared_preferences` | Persist config path and sync state |
| `crypto` | MD5 hashing for ETag comparison |

## Configuration

The app reads a `config.json` file with this structure:

```json
{
  "id": "device-uuid",
  "local": "/path/to/obsidian/vault",
  "cloud": "s3-bucket-name",
  "provider": "AWS",
  "snsTopicArn": "arn:aws:sns:region:account:topic",
  "knowledgeBaseId": "",
  "dataSourceId": "",
  "region": "ap-southeast-2",
  "credentials": {
    "key": "AKIA...",
    "secret": "..."
  }
}
```

This is the same format used by the Go desktop app, allowing both clients to share the same AWS infrastructure.
