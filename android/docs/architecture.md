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
│  (owns all services, wires callbacks)    │
└──┬──────┬──────┬──────┬──────┬──────┬───┘
   │      │      │      │      │      │
   ▼      ▼      ▼      ▼      ▼      ▼
Config  Sync   S3Sync  Dynamo  SQS   File
Service Orch.  Service  DB    Listener Watcher
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
| `AppState` | `lib/providers/app_state.dart` | Central `ChangeNotifier`. Owns service instances, wires callbacks (file change -> sync, reconnect -> sync, SQS notification -> sync). Exposes config, sync state, connectivity, and log entries to the UI. |

### Service Layer

| Service | File | Responsibility |
|---------|------|----------------|
| `ConfigService` | `lib/services/config_service.dart` | Loads `config.json` via file picker; persists the selected path in `SharedPreferences`. |
| `SyncOrchestrator` | `lib/services/sync_orchestrator.dart` | Core sync algorithm (mirrors Go's `handleSyncAsync`). Coordinates S3SyncService and DynamoDBService. |
| `S3SyncService` | `lib/services/s3_sync_service.dart` | Lists local/remote files, computes diffs, uploads, downloads, deletes. Implements bidirectional sync with delete semantics. |
| `DynamoDBService` | `lib/services/dynamodb_service.dart` | Scans for latest sync timestamp, gets/creates/updates user items. |
| `SQSListenerService` | `lib/services/sqs_listener_service.dart` | Creates temp SQS queue, subscribes to SNS, long-polls for messages, debounces notifications, cleans up on stop. |
| `ConnectivityService` | `lib/services/connectivity_service.dart` | Monitors network state via `connectivity_plus`. Fires `onReconnect` callback on offline-to-online transition. |
| `FileWatcherService` | `lib/services/file_watcher_service.dart` | Watches local vault directory for file changes. Debounces events before triggering sync. |
| `BackgroundSyncService` | `lib/services/background_service.dart` | Android foreground service via `flutter_background_service`. Periodic 5-minute sync fallback. |

### Models

| Model | File | Responsibility |
|-------|------|----------------|
| `ObsyncianConfig` | `lib/models/obsyncian_config.dart` | Config model matching Go's `ObsyncianConfig` struct. JSON serialisation. |
| `Credentials` | `lib/models/obsyncian_config.dart` | AWS access key + secret key pair. |

## Data Flow: Sync Cycle

1. **Trigger**: file change / SQS notification / manual button / periodic timer / reconnect
2. `AppState.triggerSync()` -> `SyncOrchestrator.sync()`
3. `SyncOrchestrator._handleSync()`:
   a. `DynamoDBService.getLatestSync()` — scan table for newest timestamp
   b. Decide: sync up, sync down, or both (same logic as Go app)
   c. `S3SyncService.syncDown()` / `S3SyncService.syncUp()` / `S3SyncService.dryRunSyncUp()`
   d. `DynamoDBService.updateTimestamp()` after successful sync up
4. Progress messages streamed to `AppState._logs` -> UI rebuilds

## Sync Algorithm (mirrors `handleSyncAsync` in Go)

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

if another user synced more recently AND we haven't seen that timestamp:
    sync down (S3 -> local)

dry-run: compare local files vs S3 objects
if local changes detected:
    sync up (local -> S3)
    update our timestamp in DynamoDB
```

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
| `watcher` | File system change events |
| `provider` | State management |
| `shared_preferences` | Persist config path |
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
