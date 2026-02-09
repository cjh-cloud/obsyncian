import 'dart:async';
import 'dart:convert';

import 'package:aws_sns_api/sns-2010-03-31.dart' as sns_api;
import 'package:aws_sqs_api/sqs-2012-11-05.dart' as sqs_api;
import 'package:shared_aws_api/shared.dart';

import '../models/obsyncian_config.dart';
import 'aws_signing_client.dart';

/// Debounce time to avoid triggering multiple syncs from rapid S3 events.
const _sqsDebounceTime = Duration(seconds: 2);

/// Listens for S3 change notifications via a temporary SQS queue subscribed
/// to an SNS topic. Mirrors the Go app's SQSListener.
///
/// Lifecycle:
///   1. [configure] — set credentials & topic ARN
///   2. [start] — creates queue, subscribes to SNS, begins long-polling
///   3. [stop] — unsubscribes, deletes queue, stops polling
class SQSListenerService {
  late sqs_api.SQS _sqsClient;
  late sns_api.SNS _snsClient;

  String _snsTopicArn = '';
  String _deviceId = '';
  String _queueUrl = '';
  String _queueArn = '';
  String _subscriptionArn = '';

  bool _running = false;
  Timer? _debounceTimer;

  AwsSigningClient? _sqsSigningClient;
  AwsSigningClient? _snsSigningClient;

  /// Called (debounced) when a cloud change notification is received.
  void Function()? onCloudChange;

  /// Called with log messages.
  void Function(String message)? onLog;

  SQSListenerService();

  bool get isRunning => _running;

  void configure(ObsyncianConfig config) {
    // Close previous signing clients if any
    _sqsSigningClient?.close();
    _snsSigningClient?.close();

    _snsTopicArn = config.snsTopicArn;
    _deviceId = config.id;

    final creds = AwsClientCredentials(
      accessKey: config.credentials.key,
      secretKey: config.credentials.secret,
    );

    // Use the official AWS SigV4 signer via custom HTTP clients
    _sqsSigningClient = AwsSigningClient(
      accessKey: config.credentials.key,
      secretKey: config.credentials.secret,
      region: config.awsRegion,
      service: 'sqs',
    );
    _snsSigningClient = AwsSigningClient(
      accessKey: config.credentials.key,
      secretKey: config.credentials.secret,
      region: config.awsRegion,
      service: 'sns',
    );

    _sqsClient = sqs_api.SQS(
      region: config.awsRegion,
      credentials: creds,
      client: _sqsSigningClient!,
    );
    _snsClient = sns_api.SNS(
      region: config.awsRegion,
      credentials: creds,
      client: _snsSigningClient!,
    );
  }

  /// Start listening. Creates the SQS queue, subscribes to SNS, and begins
  /// long-polling for messages.
  Future<void> start() async {
    if (_running || _snsTopicArn.isEmpty) return;

    try {
      await _createQueue();
      await _subscribeToSNS();
      _running = true;
      _log('SQS listener started');
      // Begin polling in the background
      _pollLoop();
    } catch (e) {
      _log('Failed to start SQS listener: $e');
      await _cleanup();
      rethrow;
    }
  }

  /// Stop the listener and clean up AWS resources.
  Future<void> stop() async {
    if (!_running) return;
    _running = false;
    _debounceTimer?.cancel();
    _debounceTimer = null;
    await _cleanup();
    _log('SQS listener stopped');
  }

  // ---------------------------------------------------------------------------
  // Queue management
  // ---------------------------------------------------------------------------

  Future<void> _createQueue() async {
    final queueName = 'obsyncian-$_deviceId';

    final createResult = await _sqsClient.createQueue(
      queueName: queueName,
      attributes: {
        sqs_api.QueueAttributeName.receiveMessageWaitTimeSeconds: '20',
        sqs_api.QueueAttributeName.messageRetentionPeriod: '300',
        sqs_api.QueueAttributeName.visibilityTimeout: '30',
      },
    );

    _queueUrl = createResult.queueUrl ?? '';
    if (_queueUrl.isEmpty) throw Exception('Failed to get queue URL');

    // Get queue ARN
    final attrResult = await _sqsClient.getQueueAttributes(
      queueUrl: _queueUrl,
      attributeNames: [sqs_api.QueueAttributeName.queueArn],
    );
    _queueArn = attrResult.attributes?[sqs_api.QueueAttributeName.queueArn] ?? '';
    if (_queueArn.isEmpty) throw Exception('Failed to get queue ARN');

    // Set queue policy to allow SNS to send messages
    final policy = jsonEncode({
      'Version': '2012-10-17',
      'Statement': [
        {
          'Sid': 'AllowSNS',
          'Effect': 'Allow',
          'Principal': {'Service': 'sns.amazonaws.com'},
          'Action': 'sqs:SendMessage',
          'Resource': _queueArn,
          'Condition': {
            'ArnEquals': {'aws:SourceArn': _snsTopicArn},
          },
        }
      ],
    });

    await _sqsClient.setQueueAttributes(
      queueUrl: _queueUrl,
      attributes: {sqs_api.QueueAttributeName.policy: policy},
    );
  }

  Future<void> _subscribeToSNS() async {
    final result = await _snsClient.subscribe(
      topicArn: _snsTopicArn,
      protocol: 'sqs',
      endpoint: _queueArn,
    );
    _subscriptionArn = result.subscriptionArn ?? '';
  }

  // ---------------------------------------------------------------------------
  // Polling
  // ---------------------------------------------------------------------------

  Future<void> _pollLoop() async {
    while (_running) {
      try {
        await _receiveAndProcess();
      } catch (e) {
        if (!_running) return; // shutting down
        _log('SQS poll error: $e');
        // Back off briefly on error before retrying
        await Future.delayed(const Duration(seconds: 5));
      }
    }
  }

  Future<void> _receiveAndProcess() async {
    final result = await _sqsClient.receiveMessage(
      queueUrl: _queueUrl,
      maxNumberOfMessages: 10,
      waitTimeSeconds: 20,
    );

    if (result.messages == null || result.messages!.isEmpty) return;

    bool shouldNotify = false;

    for (final msg in result.messages!) {
      if (_shouldTriggerSync(msg)) {
        shouldNotify = true;
      }

      // Delete the message after processing
      if (msg.receiptHandle != null) {
        try {
          await _sqsClient.deleteMessage(
            queueUrl: _queueUrl,
            receiptHandle: msg.receiptHandle!,
          );
        } catch (e) {
          _log('Failed to delete SQS message: $e');
        }
      }
    }

    if (shouldNotify) {
      _triggerDebouncedNotification();
    }
  }

  bool _shouldTriggerSync(sqs_api.Message msg) {
    if (msg.body == null) return false;
    // Trigger sync for all S3 events. Self-filtering is handled by the
    // sync orchestrator when checking DynamoDB timestamps.
    return true;
  }

  void _triggerDebouncedNotification() {
    _debounceTimer?.cancel();
    _debounceTimer = Timer(_sqsDebounceTime, () {
      onCloudChange?.call();
    });
  }

  // ---------------------------------------------------------------------------
  // Cleanup
  // ---------------------------------------------------------------------------

  Future<void> _cleanup() async {
    // Unsubscribe from SNS
    if (_subscriptionArn.isNotEmpty &&
        _subscriptionArn != 'pending confirmation') {
      try {
        await _snsClient.unsubscribe(subscriptionArn: _subscriptionArn);
      } catch (e) {
        _log('Failed to unsubscribe from SNS: $e');
      }
    }

    // Delete the queue
    if (_queueUrl.isNotEmpty) {
      try {
        await _sqsClient.deleteQueue(queueUrl: _queueUrl);
      } catch (e) {
        _log('Failed to delete SQS queue: $e');
      }
    }

    _queueUrl = '';
    _queueArn = '';
    _subscriptionArn = '';
  }

  void _log(String message) {
    onLog?.call(message);
  }
}
