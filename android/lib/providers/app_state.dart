import 'dart:async';
import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:flutter/widgets.dart';
import 'package:permission_handler/permission_handler.dart';

import '../models/obsyncian_config.dart';
import '../services/background_service.dart';
import '../services/config_service.dart';
import '../services/connectivity_service.dart';
import '../services/dynamodb_service.dart';
import '../services/s3_sync_service.dart';
import '../services/sqs_listener_service.dart';
import '../services/sync_orchestrator.dart';

/// Central application state, exposed to the widget tree via [ChangeNotifierProvider].
///
/// Owns all services and wires them together:
///   ConfigService -> loads config
///   SyncOrchestrator -> runs sync logic
///   ConnectivityService -> pauses/resumes on network changes
///   SQSListenerService -> triggers sync on cloud changes
///   BackgroundSyncService -> keeps the process alive when backgrounded
///
/// Implements [WidgetsBindingObserver] to sync on app lifecycle transitions:
///   - resumed (foreground): pull cloud changes
///   - paused (background): push local changes
class AppState extends ChangeNotifier with WidgetsBindingObserver {
  // -- Services ---------------------------------------------------------------
  final ConfigService _configService = ConfigService();
  final S3SyncService _s3Service = S3SyncService();
  final DynamoDBService _dynamoDBService = DynamoDBService();
  final SQSListenerService _sqsListener = SQSListenerService();
  final ConnectivityService _connectivity = ConnectivityService();
  late final SyncOrchestrator _orchestrator;

  // -- State ------------------------------------------------------------------
  ObsyncianConfig? _config;
  ObsyncianConfig? get config => _config;

  /// Whether the AWS config is loaded but we still need an Android vault path.
  bool _needsVaultPath = false;
  bool get needsVaultPath => _needsVaultPath;

  /// The raw config from the JSON file (before vault path override).
  ObsyncianConfig? _rawConfig;

  bool get isConfigured =>
      _config != null && _config!.isValid && !_needsVaultPath;

  SyncState get syncState => _orchestrator.state;
  bool get isOnline => _connectivity.isOnline;

  final List<String> _logs = [];
  List<String> get logs => List.unmodifiable(_logs);

  // -- Subscriptions ----------------------------------------------------------
  final List<StreamSubscription> _subscriptions = [];

  AppState() {
    _orchestrator = SyncOrchestrator(
      s3Service: _s3Service,
      dynamoDBService: _dynamoDBService,
    );
  }

  /// Initialise: try to load a previously-saved config, start connectivity
  /// monitoring, and listen for background-service sync requests.
  Future<void> initialise() async {
    WidgetsBinding.instance.addObserver(this);

    // Listen to sync log stream
    _subscriptions.add(
      _orchestrator.logStream.listen((msg) {
        _addLog(msg);
      }),
    );

    // Listen to sync state changes
    _subscriptions.add(
      _orchestrator.stateStream.listen((_) {
        notifyListeners();
      }),
    );

    // Start connectivity monitoring
    _connectivity.onReconnect = _onReconnect;
    await _connectivity.start();
    _subscriptions.add(
      _connectivity.onlineStream.listen((_) {
        notifyListeners();
      }),
    );

    // Listen for background service sync requests (sent on the 'sync' channel)
    _subscriptions.add(
      BackgroundSyncService.syncRequestStream.listen((_) {
        triggerSync();
      }),
    );

    // Try loading saved config
    try {
      final savedConfig = await _configService.loadSavedConfig();
      if (savedConfig != null && savedConfig.isValid) {
        _rawConfig = savedConfig;
        await _resolveVaultPathAndApply(savedConfig);
      }
    } catch (e) {
      _addLog('Failed to load saved config: $e');
    }
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    switch (state) {
      case AppLifecycleState.resumed:
        _addLog('App resumed. Syncing...');
        triggerSync();
      case AppLifecycleState.paused:
        _addLog('App paused. Syncing...');
        triggerSync();
      default:
        break;
    }
  }

  /// Let the user pick a config.json file. Returns true if config was loaded.
  Future<bool> pickConfig() async {
    try {
      final config = await _configService.pickConfigFile();
      if (config == null) return false;

      if (!config.isValid) {
        _addLog('Invalid config: missing required fields (id, local, cloud, credentials).');
        return false;
      }

      _rawConfig = config;
      await _resolveVaultPathAndApply(config);
      return true;
    } catch (e) {
      _addLog('Error picking config: $e');
      return false;
    }
  }

  /// Let the user pick the Android-local vault directory.
  /// Called when the config's `local` path isn't writable on this device.
  Future<bool> pickVaultDirectory() async {
    try {
      final path = await _configService.pickVaultDirectory();
      if (path == null) return false;

      final base = _rawConfig ?? _config;
      if (base == null) return false;

      final resolved = base.copyWithLocal(path);
      _needsVaultPath = false;
      await _applyConfig(resolved);
      return true;
    } catch (e) {
      _addLog('Error picking vault directory: $e');
      return false;
    }
  }

  /// Clear the current config and stop all services.
  Future<void> clearConfig() async {
    await _stopServices();
    _config = null;
    _rawConfig = null;
    _needsVaultPath = false;
    await _configService.clearSavedConfig();
    _addLog('Config cleared.');
    notifyListeners();
  }

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
    await _orchestrator.sync();
  }

  // ---------------------------------------------------------------------------
  // Private helpers
  // ---------------------------------------------------------------------------

  /// Check if the config's `local` path works on this device.
  /// If a saved vault path exists, use that. If the path isn't writable,
  /// set [_needsVaultPath] to prompt the user to pick a directory.
  Future<void> _resolveVaultPathAndApply(ObsyncianConfig config) async {
    // 1. Check if we have a previously-saved Android vault path
    final savedVaultPath = await _configService.getSavedVaultPath();
    if (savedVaultPath != null && savedVaultPath.isNotEmpty) {
      final dir = Directory(savedVaultPath);
      if (await dir.exists()) {
        _addLog('Using saved vault path: $savedVaultPath');
        final resolved = config.copyWithLocal(savedVaultPath);
        await _applyConfig(resolved);
        return;
      }
      _addLog('Saved vault path no longer exists: $savedVaultPath');
    }

    // 2. Check if the config's original local path is writable
    if (await _isPathWritable(config.local)) {
      await _applyConfig(config);
      return;
    }

    // 3. Path isn't usable — ask the user to pick a vault directory
    _config = config;
    _needsVaultPath = true;
    _addLog(
      'The config\'s local path "${config.local}" is not accessible on '
      'this device. Please select a vault directory.',
    );
    notifyListeners();
  }

  /// Quick check whether a directory path exists and is writable.
  Future<bool> _isPathWritable(String path) async {
    try {
      final dir = Directory(path);
      if (!await dir.exists()) return false;
      final testFile = File('${dir.path}/.obsyncian_write_test');
      await testFile.writeAsString('test');
      await testFile.delete();
      return true;
    } catch (_) {
      return false;
    }
  }

  Future<void> _applyConfig(ObsyncianConfig config) async {
    // Stop any existing services first
    await _stopServices();

    _config = config;
    _needsVaultPath = false;
    _addLog('Config loaded: ${config.local} -> s3://${config.cloud}');

    // Ensure we have storage permissions before syncing
    final hasPermission = await _ensureStoragePermission(config.local);
    if (!hasPermission) {
      _addLog('Storage permission denied. Cannot sync.');
      notifyListeners();
      return;
    }

    // Configure all services
    await _orchestrator.configure(config);

    // Set up SQS listener if SNS topic is configured
    if (config.hasSnsTopicArn) {
      _sqsListener.configure(config);
      _sqsListener.onCloudChange = () => triggerSync();
      _sqsListener.onLog = (msg) => _addLog('[SQS] $msg');
      try {
        await _sqsListener.start();
      } catch (e) {
        _addLog('SQS listener failed to start: $e');
      }
    }

    // Start background service
    await BackgroundSyncService.start();

    notifyListeners();

    // Run initial sync
    if (_connectivity.isOnline) {
      await _orchestrator.sync();
    } else {
      _addLog('Device offline — sync will start when connectivity is restored.');
    }
  }

  /// Request storage permissions needed to read/write vault files.
  ///
  /// On Android 11+ (API 30+), broad file access requires MANAGE_EXTERNAL_STORAGE
  /// which opens a system settings page. On older versions, READ/WRITE_EXTERNAL_STORAGE
  /// runtime permissions suffice.
  Future<bool> _ensureStoragePermission(String vaultPath) async {
    // First, check if the vault directory already exists and is writable
    // (e.g. it's in app-internal storage and no special permission is needed)
    try {
      final dir = Directory(vaultPath);
      if (await dir.exists()) {
        // Try a quick write test
        final testFile = File('${dir.path}/.obsyncian_permission_test');
        await testFile.writeAsString('test');
        await testFile.delete();
        _addLog('Storage access verified.');
        return true;
      }
    } catch (_) {
      // Can't write — need to request permissions
    }

    // Android 11+ needs MANAGE_EXTERNAL_STORAGE
    if (await Permission.manageExternalStorage.isGranted) {
      _addLog('Storage permission already granted.');
      return true;
    }

    _addLog('Requesting storage permission...');
    final status = await Permission.manageExternalStorage.request();

    if (status.isGranted) {
      _addLog('Storage permission granted.');

      // Create the vault directory if it doesn't exist
      final dir = Directory(vaultPath);
      if (!await dir.exists()) {
        await dir.create(recursive: true);
        _addLog('Created vault directory: $vaultPath');
      }
      return true;
    }

    if (status.isPermanentlyDenied) {
      _addLog('Storage permission permanently denied. Please enable it in Settings > Apps > Obsyncian > Permissions.');
      await openAppSettings();
    }

    return false;
  }

  void _onReconnect() {
    _addLog('Connectivity restored. Syncing...');
    triggerSync();
  }

  Future<void> _stopServices() async {
    await _sqsListener.stop();
    await BackgroundSyncService.stop();
  }

  void _addLog(String message) {
    final timestamp = DateTime.now().toIso8601String().substring(11, 19);
    _logs.add('[$timestamp] $message');
    // Keep log buffer bounded
    if (_logs.length > 500) {
      _logs.removeRange(0, _logs.length - 500);
    }
    notifyListeners();
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    for (final sub in _subscriptions) {
      sub.cancel();
    }
    _subscriptions.clear();
    _connectivity.dispose();
    _orchestrator.dispose();
    super.dispose();
  }
}
