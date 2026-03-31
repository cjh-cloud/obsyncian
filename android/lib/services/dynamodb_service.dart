import 'package:aws_dynamodb_api/dynamodb-2012-08-10.dart';

import '../models/obsyncian_config.dart';
import 'aws_signing_client.dart';

/// Represents a sync timestamp item in the Obsyncian DynamoDB table.
class SyncItem {
  final String userId;
  final String timestamp;

  const SyncItem({required this.userId, required this.timestamp});
}

/// Service for tracking sync timestamps in DynamoDB.
///
/// Mirrors the Go app's DynamoDB operations:
///   - Scan for latest sync across all users
///   - Get/create/update individual user entries
///
/// The DynamoDB table ("Obsyncian") has a primary key of "UserId" (String)
/// and a "Timestamp" attribute (String, format "YYYYMMDDHHmmss").
class DynamoDBService {
  static const tableName = 'Obsyncian';

  late DynamoDB _client;
  AwsSigningClient? _signingClient;

  DynamoDBService();

  void configure(ObsyncianConfig config) {
    // Close previous signing client if any
    _signingClient?.close();

    // Use the official AWS SigV4 signer via a custom HTTP client
    _signingClient = AwsSigningClient(
      accessKey: config.credentials.key,
      secretKey: config.credentials.secret,
      region: config.awsRegion,
      service: 'dynamodb',
    );

    _client = DynamoDB(
      region: config.awsRegion,
      credentials: AwsClientCredentials(
        accessKey: config.credentials.key,
        secretKey: config.credentials.secret,
      ),
      client: _signingClient!,
    );
  }

  /// Scan the table to find the latest sync timestamp across all users.
  /// Returns null if the table is empty.
  Future<SyncItem?> getLatestSync() async {
    final allItems = <SyncItem>[];
    Map<String, AttributeValue>? lastEvaluatedKey;

    do {
      final result = await _client.scan(
        tableName: tableName,
        projectionExpression: 'UserId, #ts',
        expressionAttributeNames: {'#ts': 'Timestamp'},
        exclusiveStartKey: lastEvaluatedKey,
      );

      if (result.items != null) {
        for (final item in result.items!) {
          final userId = item['UserId']?.s;
          final timestamp = item['Timestamp']?.s;
          if (userId != null && timestamp != null) {
            allItems.add(SyncItem(userId: userId, timestamp: timestamp));
          }
        }
      }

      lastEvaluatedKey = result.lastEvaluatedKey;
    } while (lastEvaluatedKey != null && lastEvaluatedKey.isNotEmpty);

    if (allItems.isEmpty) return null;

    // Sort descending by timestamp, take the latest
    allItems.sort((a, b) => b.timestamp.compareTo(a.timestamp));
    return allItems.first;
  }

  /// Get a specific user's item from the table. Returns null if not found.
  Future<SyncItem?> getUser(String userId) async {
    final result = await _client.getItem(
      tableName: tableName,
      key: {'UserId': AttributeValue(s: userId)},
    );

    if (result.item == null) return null;

    final ts = result.item!['Timestamp']?.s;
    if (ts == null) return null;

    return SyncItem(userId: userId, timestamp: ts);
  }

  /// Create a new user entry with the current timestamp.
  Future<void> createUser(String userId) async {
    final timestamp = _nowTimestamp();
    await _client.putItem(
      tableName: tableName,
      item: {
        'UserId': AttributeValue(s: userId),
        'Timestamp': AttributeValue(s: timestamp),
      },
    );
  }

  /// Update the user's timestamp to now.
  Future<void> updateTimestamp(String userId) async {
    final timestamp = _nowTimestamp();
    await _client.updateItem(
      tableName: tableName,
      key: {'UserId': AttributeValue(s: userId)},
      updateExpression: 'SET #ts = :ts',
      expressionAttributeNames: {'#ts': 'Timestamp'},
      expressionAttributeValues: {':ts': AttributeValue(s: timestamp)},
      returnValues: ReturnValue.updatedNew,
    );
  }

  /// Generate a timestamp string in the format used by the Go app: "YYYYMMDDHHmmss".
  /// Uses local time (not UTC) to match the Go app's `time.Now().Format(...)`.
  String _nowTimestamp() {
    final now = DateTime.now();
    return '${now.year.toString().padLeft(4, '0')}'
        '${now.month.toString().padLeft(2, '0')}'
        '${now.day.toString().padLeft(2, '0')}'
        '${now.hour.toString().padLeft(2, '0')}'
        '${now.minute.toString().padLeft(2, '0')}'
        '${now.second.toString().padLeft(2, '0')}';
  }
}
