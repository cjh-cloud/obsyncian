import 'dart:convert';

/// AWS credentials for S3/DynamoDB/SQS/SNS access.
class Credentials {
  final String key;
  final String secret;

  const Credentials({required this.key, required this.secret});

  factory Credentials.fromJson(Map<String, dynamic> json) {
    return Credentials(
      key: (json['key'] as String? ?? '').trim(),
      secret: (json['secret'] as String? ?? '').trim(),
    );
  }

  Map<String, dynamic> toJson() => {'key': key, 'secret': secret};
}

/// Configuration model matching the Go app's ObsyncianConfig struct.
///
/// Fields:
///   - id: unique device/user UUID
///   - local: local vault directory path
///   - cloud: S3 bucket name (e.g. "obsyncian")
///   - provider: cloud provider ("AWS")
///   - snsTopicArn: SNS topic ARN for sync notifications
///   - knowledgeBaseId: optional Bedrock KB ID
///   - dataSourceId: optional Bedrock data source ID
///   - region: AWS region (defaults to "ap-southeast-2")
///   - credentials: AWS access key + secret
class ObsyncianConfig {
  final String id;
  final String local;
  final String cloud;
  final String provider;
  final String snsTopicArn;
  final String knowledgeBaseId;
  final String dataSourceId;
  final String region;
  final Credentials credentials;

  const ObsyncianConfig({
    required this.id,
    required this.local,
    required this.cloud,
    required this.provider,
    this.snsTopicArn = '',
    this.knowledgeBaseId = '',
    this.dataSourceId = '',
    this.region = 'ap-southeast-2',
    required this.credentials,
  });

  /// The effective AWS region; falls back to ap-southeast-2 if empty.
  String get awsRegion => region.isNotEmpty ? region : 'ap-southeast-2';

  /// Whether this config has enough fields populated to start syncing.
  bool get isValid =>
      id.isNotEmpty &&
      local.isNotEmpty &&
      cloud.isNotEmpty &&
      credentials.key.isNotEmpty &&
      credentials.secret.isNotEmpty;

  /// Whether SQS/SNS event-driven sync is configured.
  bool get hasSnsTopicArn => snsTopicArn.isNotEmpty;

  factory ObsyncianConfig.fromJson(Map<String, dynamic> json) {
    return ObsyncianConfig(
      id: (json['id'] as String? ?? '').trim(),
      local: (json['local'] as String? ?? '').trim(),
      cloud: (json['cloud'] as String? ?? '').trim(),
      provider: (json['provider'] as String? ?? '').trim(),
      snsTopicArn: (json['snsTopicArn'] as String? ?? '').trim(),
      knowledgeBaseId: (json['knowledgeBaseId'] as String? ?? '').trim(),
      dataSourceId: (json['dataSourceId'] as String? ?? '').trim(),
      region: (json['region'] as String? ?? 'ap-southeast-2').trim(),
      credentials: json['credentials'] != null
          ? Credentials.fromJson(json['credentials'] as Map<String, dynamic>)
          : const Credentials(key: '', secret: ''),
    );
  }

  Map<String, dynamic> toJson() => {
        'id': id,
        'local': local,
        'cloud': cloud,
        'provider': provider,
        'snsTopicArn': snsTopicArn,
        'knowledgeBaseId': knowledgeBaseId,
        'dataSourceId': dataSourceId,
        'region': region,
        'credentials': credentials.toJson(),
      };

  /// Create a copy with an overridden local vault path.
  /// Used on Android to replace the desktop `local` path with an
  /// Android-accessible directory.
  ObsyncianConfig copyWithLocal(String newLocalPath) {
    return ObsyncianConfig(
      id: id,
      local: newLocalPath,
      cloud: cloud,
      provider: provider,
      snsTopicArn: snsTopicArn,
      knowledgeBaseId: knowledgeBaseId,
      dataSourceId: dataSourceId,
      region: region,
      credentials: credentials,
    );
  }

  /// Parse a config from a raw JSON string.
  static ObsyncianConfig fromJsonString(String jsonString) {
    final map = jsonDecode(jsonString) as Map<String, dynamic>;
    return ObsyncianConfig.fromJson(map);
  }

  @override
  String toString() =>
      'ObsyncianConfig(id: $id, local: $local, cloud: $cloud, region: $awsRegion)';
}
