# Obsyncian Android App - Specifications

## Overview

Obsyncian for Android is a Flutter-based mobile application that synchronises local Obsidian vault files with AWS S3. It is the companion to the Go desktop TUI application, sharing the same `config.json` format, DynamoDB sync-tracking table, and SQS/SNS notification infrastructure.

## Functional Requirements

### FR-1: Config File Selection

- On first launch the app presents a prominent "Select config.json" button.
- A system file picker allows the user to browse the device for the file.
- The selected path is persisted so subsequent launches load it automatically.
- The user can change or clear the config from the overflow menu.

### FR-2: Bidirectional S3 Sync

- **Sync down**: download new/changed objects from S3 to the local vault; delete local files that no longer exist in S3.
- **Sync up**: upload new/changed local files to S3; delete S3 objects that no longer exist locally.
- Sync includes `--delete` semantics (matching Go's `s3sync.WithDelete()`).
- File comparison uses MD5 hashes (matching S3 ETags for non-multipart objects).

### FR-3: DynamoDB Timestamp Tracking

- The `Obsyncian` DynamoDB table tracks the latest sync timestamp per user/device.
- On each sync cycle the app:
  1. Scans for the globally latest timestamp.
  2. Determines whether to sync down, sync up, or both.
  3. Updates its own timestamp after a successful sync up.
- Timestamp format: `YYYYMMDDHHmmss` (matching the Go app).

### FR-4: Event-Driven Sync (SQS/SNS)

- If `snsTopicArn` is configured, the app creates a temporary SQS queue, subscribes it to the SNS topic, and long-polls for S3 change notifications.
- Notifications are debounced (2 s) to avoid triggering multiple syncs from rapid changes.
- The queue and subscription are cleaned up when the listener stops.

### FR-5: Offline Handling

- When the device has no internet connectivity, sync attempts are skipped.
- The app monitors connectivity state via `connectivity_plus`.
- When connectivity is restored, an immediate sync cycle is triggered.
- The UI displays the current online/offline state.

### FR-6: Background Sync

- An Android foreground service keeps the app alive when backgrounded.
- A persistent notification ("Obsyncian - Syncing your vault in the background") is shown.
- A periodic fallback sync runs every 5 minutes in case events are missed.

### FR-7: Local File Watching

- The local vault directory is watched for file system events.
- Changes are debounced (500 ms) before triggering a sync cycle.
- Hidden files/directories (starting with `.`) are ignored.

## Non-Functional Requirements

- Built with Flutter (latest stable) targeting Android.
- Uses Material 3 design with system dark/light theme support.
- Keeps a bounded in-memory log (max 500 entries) displayed in a scrollable list.
- AWS credentials are read from the config file; no in-app credential entry.
