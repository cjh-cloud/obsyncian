import 'dart:async';

import 'package:shared_preferences/shared_preferences.dart';

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
///   4. If another user synced more recently -> sync down
///   5. Only if we did NOT sync down: dry-run local vs S3 -> sync up if changes
///   6. Update DynamoDB timestamp after sync up
///
/// The dry-run is skipped after a sync-down to prevent a race condition where
/// S3 changes between the sync-down and dry-run would be misinterpreted as
/// local changes, causing the mobile to overwrite newer cloud content.
class SyncOrchestrator {
  static const _lastSyncedTsKey = 'obsyncian_last_synced_timestamp';

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

  Future<void> configure(ObsyncianConfig config) async {
    _config = config;
    _s3Service.configure(config);
    _s3Service.onProgress = _log;
    _dynamoDBService.configure(config);
    await _loadLastSyncedTimestamp();
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
  ///
  /// After syncing down, the dry-run check is skipped for this cycle to prevent
  /// a race condition: if S3 changes between our sync-down and the dry-run,
  /// the diff would be misinterpreted as local changes. The next sync trigger
  /// (SQS notification, manual button, or lifecycle event) will handle any
  /// genuine local changes.
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

    bool didSyncDown = false;

    // 2. Check if our user exists
    final ourItem = await _dynamoDBService.getUser(_config.id);

    if (ourItem == null) {
      _log('User ${_config.id} not found in table. Creating & syncing down...');
      await _dynamoDBService.createUser(_config.id);
      _log('Syncing down from S3...');
      await _s3Service.syncDown();
      await _saveLastSyncedTimestamp(latestSync.timestamp);
      _log('Finished syncing down (new user).');
      didSyncDown = true;
    } else {
      // 3. Check if we need to sync down
      final needsSyncDown = _config.id != latestSync.userId &&
          latestSync.timestamp.compareTo(ourItem.timestamp) >= 0 &&
          _lastSyncedTimestamp.compareTo(latestSync.timestamp) < 0;

      if (needsSyncDown) {
        _log('Not synced with Cloud. Syncing down from S3...');
        await _s3Service.syncDown();
        await _saveLastSyncedTimestamp(latestSync.timestamp);
        _log('Finished syncing down.');
        didSyncDown = true;
      } else {
        _log('Already synced with Cloud.');
      }
    }

    // 4. Skip the dry-run if we just synced down — local files now match the
    //    cloud snapshot we pulled. Any diff at this point would be a race
    //    (another device syncing up during our sync-down) not a real local edit.
    if (didSyncDown) {
      _log('Skipping local change check after sync-down (race prevention).');
      return;
    }

    // 5. Check for local changes via dry-run
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

  Future<void> _loadLastSyncedTimestamp() async {
    final prefs = await SharedPreferences.getInstance();
    _lastSyncedTimestamp = prefs.getString(_lastSyncedTsKey) ?? '';
  }

  Future<void> _saveLastSyncedTimestamp(String ts) async {
    _lastSyncedTimestamp = ts;
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_lastSyncedTsKey, ts);
  }

  void dispose() {
    _logController.close();
    _stateController.close();
  }
}
