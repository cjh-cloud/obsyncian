# Obsyncian React Native Android App

A React Native rewrite of the Obsyncian Android app, migrated from Flutter. This app syncs an Obsidian vault to AWS S3 with battery optimizations and event-driven updates.

## Architecture

### Services

- **configService**: Loads and persists config.json using document picker
- **s3SyncService**: Bidirectional S3 sync with stat-based change detection
- **dynamoDBService**: DynamoDB timestamp tracking for multi-device coordination
- **sqsListenerService**: SQS long-polling with exponential backoff (battery optimization)
- **connectivityService**: Network state monitoring via @react-native-community/netinfo
- **backgroundSyncService**: Android foreground service for background syncing
- **syncOrchestrator**: Core sync algorithm (mirrors Go desktop app)
- **fileStatCache**: Caches file mtime/size/md5 to avoid re-hashing all files on every sync

### State Management

Zustand store (`appStore.ts`) with the following state:
- `config`: Loaded ObsyncianConfig
- `vaultPath`: Local vault directory path
- `syncState`: idle | syncing | error | offline
- `isOnline`: Network connectivity state
- `logs`: Bounded list (max 500) of sync operation logs

## Battery Optimizations

Compared to the Flutter version:

1. **Stat-based file change detection**: Only MD5-hash files whose mtime or size has changed
2. **SQS exponential backoff**: When idle (no messages for N polls), backoff doubles (5s → 10s → 20s → ... → 60s max)
3. **Background sync connectivity check**: Periodic 5-minute sync skips if offline
4. **File stat caching**: Persisted to AsyncStorage, survives app restarts

## Key Features

- Single-screen UI with config picker and sync log display
- Manual sync button and status indicator
- Offline detection with automatic retry on reconnect
- App lifecycle sync (on resume/pause)
- SQS/SNS event-driven sync when cloud content changes
- Persistent state across app restarts

## Building

```bash
cd android-rn
npm install
npx react-native run-android
```

## Project Structure

```
src/
├── models/config.ts              # Config types
├── services/
│   ├── configService.ts
│   ├── s3SyncService.ts
│   ├── dynamoDBService.ts
│   ├── sqsListenerService.ts
│   ├── connectivityService.ts
│   ├── backgroundService.ts
│   ├── syncOrchestrator.ts
│   └── fileStatCache.ts
├── store/appStore.ts             # Zustand store
└── screens/HomeScreen.tsx        # Main UI
```

## Known Issues & Future Improvements

1. **Directory picker**: Currently uses Alert.prompt() for vault path. A real directory picker would be better.
2. **SQS queue policy**: The queue policy setup is stubbed out and needs implementation via SetQueueAttributes
3. **Multipart ETag handling**: Conservative comparison (size-based) for multipart uploads; proper handling could re-download unchanged large files
4. **File watching**: Uses stat-based detection; native Android FileObserver module would be more efficient
5. **DynamoDB table scan**: Still does full scan for latest timestamp; should use GSI for large numbers of devices

## Testing

1. Pick a valid config.json that points to an S3 bucket
2. Set the vault path to an Obsidian vault directory
3. Tap "Manual Sync" to trigger a sync cycle
4. Upload a file to S3 via AWS Console; within ~22s the app should detect it and sync down
5. Edit a local file; on next app resume the app should sync up the changes
