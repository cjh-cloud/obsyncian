import 'dart:async';
import 'dart:io';

import 'package:watcher/watcher.dart';

/// Watches a local directory for file changes and triggers a debounced callback.
///
/// Mirrors the Go app's `FileWatcher`:
///   - Recursive directory watching
///   - Skips hidden directories (.git, .obsidian)
///   - Debounced change detection (default 500ms)
class FileWatcherService {
  DirectoryWatcher? _watcher;
  StreamSubscription<WatchEvent>? _subscription;
  Timer? _debounceTimer;
  final Duration debounceTime;

  /// Called (debounced) when file changes are detected.
  void Function()? onChange;

  FileWatcherService({this.debounceTime = const Duration(milliseconds: 500)});

  /// Start watching the given directory path.
  void start(String directoryPath) {
    stop(); // clean up any existing watcher

    final dir = Directory(directoryPath);
    if (!dir.existsSync()) return;

    _watcher = DirectoryWatcher(directoryPath);
    _subscription = _watcher!.events.listen((event) {
      // Skip hidden files/directories
      if (_isHiddenPath(event.path)) return;
      _triggerDebounced();
    });
  }

  /// Stop watching.
  void stop() {
    _debounceTimer?.cancel();
    _debounceTimer = null;
    _subscription?.cancel();
    _subscription = null;
    _watcher = null;
  }

  void _triggerDebounced() {
    _debounceTimer?.cancel();
    _debounceTimer = Timer(debounceTime, () {
      onChange?.call();
    });
  }

  /// Check if any segment of the path starts with a dot.
  bool _isHiddenPath(String path) {
    final segments = path.split(Platform.pathSeparator);
    return segments.any((s) => s.startsWith('.'));
  }

  void dispose() {
    stop();
  }
}
