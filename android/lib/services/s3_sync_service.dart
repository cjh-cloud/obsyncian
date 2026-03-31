import 'dart:io';

import 'package:aws_s3_api/s3-2006-03-01.dart' as s3;
import 'package:crypto/crypto.dart';
import 'package:path/path.dart' as p;
import 'package:shared_aws_api/shared.dart';

import '../models/obsyncian_config.dart';
import 'aws_signing_client.dart';

/// Represents a file entry for diff comparison.
class SyncEntry {
  final String key;
  final String? etag; // S3 ETag or local MD5 hex
  final DateTime? lastModified;

  const SyncEntry({required this.key, this.etag, this.lastModified});
}

/// Result of a sync diff operation.
class SyncDiff {
  /// Keys to upload (local -> cloud).
  final List<String> toUpload;

  /// Keys to download (cloud -> local).
  final List<String> toDownload;

  /// Keys to delete at the destination.
  final List<String> toDelete;

  const SyncDiff({
    required this.toUpload,
    required this.toDownload,
    required this.toDelete,
  });

  bool get hasChanges =>
      toUpload.isNotEmpty || toDownload.isNotEmpty || toDelete.isNotEmpty;

  @override
  String toString() =>
      'SyncDiff(upload: ${toUpload.length}, download: ${toDownload.length}, delete: ${toDelete.length})';
}

/// Service for syncing files between a local directory and an S3 bucket.
///
/// Implements bidirectional sync with delete semantics matching Go's
/// `s3sync.WithDelete()`.
class S3SyncService {
  late s3.S3 _s3Client;
  late String _bucket;
  late String _localPath;
  AwsSigningClient? _signingClient;

  /// Callback for streaming progress logs.
  void Function(String message)? onProgress;

  S3SyncService();

  /// Initialise the S3 client from config.
  void configure(ObsyncianConfig config) {
    // Close previous signing client if any
    _signingClient?.close();

    _bucket = config.cloud;
    _localPath = config.local;

    // Use the official AWS SigV4 signer via a custom HTTP client
    _signingClient = AwsSigningClient(
      accessKey: config.credentials.key,
      secretKey: config.credentials.secret,
      region: config.awsRegion,
      service: 's3',
    );

    _s3Client = s3.S3(
      region: config.awsRegion,
      credentials: AwsClientCredentials(
        accessKey: config.credentials.key,
        secretKey: config.credentials.secret,
      ),
      client: _signingClient!,
    );
  }

  // ---------------------------------------------------------------------------
  // Public sync operations
  // ---------------------------------------------------------------------------

  /// Sync from local to S3 (upload local changes, delete remote orphans).
  Future<void> syncUp() async {
    _log('Syncing local -> S3...');
    final localEntries = await _listLocalFiles();
    final remoteEntries = await _listRemoteObjects();

    final localMap = {for (final e in localEntries) e.key: e};
    final remoteMap = {for (final e in remoteEntries) e.key: e};

    // Upload new/changed files
    for (final entry in localEntries) {
      final remote = remoteMap[entry.key];
      if (remote == null || remote.etag != entry.etag) {
        await _uploadFile(entry.key);
      }
    }

    // Delete remote files not present locally (WithDelete semantics)
    for (final key in remoteMap.keys) {
      if (!localMap.containsKey(key)) {
        await _deleteRemoteObject(key);
      }
    }

    _log('Sync up complete.');
  }

  /// Sync from S3 to local (download remote changes, delete local orphans).
  Future<void> syncDown() async {
    _log('Syncing S3 -> local...');
    final localEntries = await _listLocalFiles();
    final remoteEntries = await _listRemoteObjects();

    final localMap = {for (final e in localEntries) e.key: e};
    final remoteMap = {for (final e in remoteEntries) e.key: e};

    // Download new/changed files
    for (final entry in remoteEntries) {
      final local = localMap[entry.key];
      if (local == null || local.etag != entry.etag) {
        await _downloadFile(entry.key);
      }
    }

    // Delete local files not present in S3 (WithDelete semantics)
    for (final key in localMap.keys) {
      if (!remoteMap.containsKey(key)) {
        _deleteLocalFile(key);
      }
    }

    _log('Sync down complete.');
  }

  /// Perform a dry run comparing local to S3. Returns the diff without
  /// making any changes. Used to detect if local changes need syncing up.
  Future<SyncDiff> dryRunSyncUp() async {
    final localEntries = await _listLocalFiles();
    final remoteEntries = await _listRemoteObjects();

    final localMap = {for (final e in localEntries) e.key: e};
    final remoteMap = {for (final e in remoteEntries) e.key: e};

    final toUpload = <String>[];
    final toDelete = <String>[];

    for (final entry in localEntries) {
      final remote = remoteMap[entry.key];
      if (remote == null || remote.etag != entry.etag) {
        toUpload.add(entry.key);
      }
    }

    for (final key in remoteMap.keys) {
      if (!localMap.containsKey(key)) {
        toDelete.add(key);
      }
    }

    return SyncDiff(toUpload: toUpload, toDownload: [], toDelete: toDelete);
  }

  // ---------------------------------------------------------------------------
  // S3 operations
  // ---------------------------------------------------------------------------

  /// List all objects in the S3 bucket.
  Future<List<SyncEntry>> _listRemoteObjects() async {
    final entries = <SyncEntry>[];
    String? continuationToken;

    do {
      final result = await _s3Client.listObjectsV2(
        bucket: _bucket,
        continuationToken: continuationToken,
      );

      if (result.contents != null) {
        for (final obj in result.contents!) {
          if (obj.key == null) continue;
          // Apply same skip rules as local listing for consistency
          if (_shouldSkip(obj.key!)) continue;
          // Strip surrounding quotes from ETag
          final etag = obj.eTag?.replaceAll('"', '');
          entries.add(SyncEntry(
            key: obj.key!,
            etag: etag,
            lastModified: obj.lastModified,
          ));
        }
      }

      continuationToken =
          (result.isTruncated ?? false) ? result.nextContinuationToken : null;
    } while (continuationToken != null);

    return entries;
  }

  /// Upload a local file to S3.
  Future<void> _uploadFile(String key) async {
    final file = File(p.join(_localPath, key));
    if (!await file.exists()) return;

    _log('  Upload: $key');
    final bytes = await file.readAsBytes();
    await _s3Client.putObject(
      bucket: _bucket,
      key: key,
      body: bytes,
    );
  }

  /// Download an S3 object to a local file.
  Future<void> _downloadFile(String key) async {
    _log('  Download: $key');
    final result = await _s3Client.getObject(bucket: _bucket, key: key);

    if (result.body == null) return;

    final file = File(p.join(_localPath, key));
    await file.parent.create(recursive: true);
    await file.writeAsBytes(result.body!);
  }

  /// Delete an object from S3.
  Future<void> _deleteRemoteObject(String key) async {
    _log('  Delete remote: $key');
    await _s3Client.deleteObject(bucket: _bucket, key: key);
  }

  // ---------------------------------------------------------------------------
  // Local file operations
  // ---------------------------------------------------------------------------

  /// Walk the local vault directory and build a list of entries with MD5 hashes.
  Future<List<SyncEntry>> _listLocalFiles() async {
    final entries = <SyncEntry>[];
    final dir = Directory(_localPath);
    if (!await dir.exists()) return entries;

    await for (final entity in dir.list(recursive: true)) {
      if (entity is! File) continue;

      final relativePath = p.relative(entity.path, from: _localPath);
      // Only skip .git — all other files (including .obsidian) are synced,
      // matching the Go app's s3sync behaviour.
      if (_shouldSkip(relativePath)) continue;

      final bytes = await entity.readAsBytes();
      final md5Hash = md5.convert(bytes).toString();

      entries.add(SyncEntry(
        key: relativePath,
        etag: md5Hash,
        lastModified: await entity.lastModified(),
      ));
    }

    return entries;
  }

  /// Delete a local file by its relative key.
  void _deleteLocalFile(String key) {
    _log('  Delete local: $key');
    final file = File(p.join(_localPath, key));
    if (file.existsSync()) {
      file.deleteSync();
    }
  }

  /// Check if a file should be excluded from sync.
  /// Only `.git` is skipped — `.obsidian` and other dotfiles are synced
  /// to match the Go app's s3sync behaviour.
  bool _shouldSkip(String relativePath) {
    final parts = p.split(relativePath);
    return parts.any((part) => part == '.git');
  }

  void _log(String message) {
    onProgress?.call(message);
  }
}
