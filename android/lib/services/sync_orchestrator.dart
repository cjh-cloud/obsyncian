import 'dart:async';

import '../models/obsyncian_config.dart';
import 'dynamodb_service.dart';
import 's3_sync_service.dart';

/// Current state of the sync engine.
enum SyncState { idle, syncing, error, offline }

/// Orchestrates sync operations between local files and S3/DynamoDB.
///
/// This mirrors the Go app's `handleSyncAsync` logic in `sync_logic.go`:
///   1. Get latest cloud sync from DynamoDB
///   2. If table empty -> sync up, create user
///   3. If user doesn't exist -> create user, sync down
///   4. If another user synced more recently -> sync down, then check local
///   5. Dry-run local vs S3 -> sync up if changes
///   6. Update DynamoDB timestamp after sync up
class SyncOrchestrator {
  final S3SyncService _s3Service;
  final DynamoDBService _dynamoDBService;

  late ObsyncianConfig _config;

  SyncState _state = SyncState.idle;
  String _lastSyncedTimestamp = '';
  bool _isSyncQueued = false;

  /// Stream of log messages for the UI.
  final _logController = StreamController<String>.broadcast();
  Stream<String> get logStream => _logController.stream;

  /// Stream of state changes.
  final _stateController = StreamController<SyncState>.broadcast();
  Stream<SyncState> get stateStream => _stateController.stream;

  SyncState get state => _state;
  String get lastSyncedTimestamp => _lastSyncedTimestamp;

  SyncOrchestrator({
    required S3SyncService s3Service,
    required DynamoDBService dynamoDBService,
  })  : _s3Service = s3Service,
        _dynamoDBService = dynamoDBService;

  void configure(ObsyncianConfig config) {
    _config = config;
    _s3Service.configure(config);
    _s3Service.onProgress = _log;
    _dynamoDBService.configure(config);
  }

  /// Run a full sync cycle. If a sync is already in progress, queues another
  /// cycle to run when the current one completes.
  Future<void> sync() async {
    if (_state == SyncState.syncing) {
      _isSyncQueued = true;
      _log('Sync already in progress, queued another cycle.');
      return;
    }

    _setState(SyncState.syncing);

    try {
      await _handleSync();
      _setState(SyncState.idle);
    } catch (e) {
      _log('Sync error: $e');
      _setState(SyncState.error);
    }

    // If another sync was requested while we were busy, run it now.
    if (_isSyncQueued) {
      _isSyncQueued = false;
      sync(); // fire-and-forget; it will set state itself
    }
  }

  /// The core sync algorithm, matching `handleSyncAsync` from the Go app.
  Future<void> _handleSync() async {
    _log('Last local sync: ${DateTime.now()}');
    _log('Config: region=${_config.awsRegion}, bucket=${_config.cloud}, '
        'key=${_config.credentials.key.substring(0, 4)}...');

    // 1. Get latest cloud sync
    _log('Scanning DynamoDB for latest sync...');
    final latestSync = await _dynamoDBService.getLatestSync();

    if (latestSync == null) {
      // Table is empty — this must be the first device ever.
      _log('Table is empty. Syncing up...');
      await _s3Service.syncUp();
      await _dynamoDBService.createUser(_config.id);
      await _dynamoDBService.updateTimestamp(_config.id);
      _log('Finished syncing up (new table).');
      return;
    }

    _log('Latest cloud sync: ${latestSync.timestamp} by ${latestSync.userId}');

    // 2. Check if our user exists
    final ourItem = await _dynamoDBService.getUser(_config.id);

    if (ourItem == null) {
      _log('User ${_config.id} not found in table. Creating & syncing down...');
      await _dynamoDBService.createUser(_config.id);
      _log('Syncing down from S3...');
      await _s3Service.syncDown();
      _lastSyncedTimestamp = latestSync.timestamp;
      _log('Finished syncing down (new user).');
      // Fall through to check local changes
    } else {
      // 3. Check if we need to sync down
      final needsSyncDown = _config.id != latestSync.userId &&
          latestSync.timestamp.compareTo(ourItem.timestamp) >= 0 &&
          _lastSyncedTimestamp.compareTo(latestSync.timestamp) < 0;

      if (needsSyncDown) {
        _log('Not synced with Cloud. Syncing down from S3...');
        await _s3Service.syncDown();
        _lastSyncedTimestamp = latestSync.timestamp;
        _log('Finished syncing down.');
      } else {
        _log('Already synced with Cloud.');
      }
    }

    // 4. Check for local changes via dry-run
    _log('Checking for local changes (dry-run S3 diff)...');
    final diff = await _s3Service.dryRunSyncUp();

    if (diff.hasChanges) {
      _log('Local changes detected: $diff. Syncing up...');
      await _s3Service.syncUp();
      await _dynamoDBService.updateTimestamp(_config.id);
      _log('Finished syncing up.');
    } else {
      _log('No local changes to sync.');
    }
  }

  void _setState(SyncState newState) {
    _state = newState;
    _stateController.add(newState);
  }

  void _log(String message) {
    _logController.add(message);
  }

  void dispose() {
    _logController.close();
    _stateController.close();
  }
}
