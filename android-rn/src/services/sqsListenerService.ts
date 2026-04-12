import {
  SQSClient,
  CreateQueueCommand,
  GetQueueAttributesCommand,
  SetQueueAttributesCommand,
  ReceiveMessageCommand,
  DeleteMessageCommand,
  DeleteQueueCommand,
} from '@aws-sdk/client-sqs';
import {
  SNSClient,
  SubscribeCommand,
  UnsubscribeCommand,
} from '@aws-sdk/client-sns';
import { ObsyncianConfig } from '../models/config';

type OnCloudChangeCallback = () => void;

class SQSListenerService {
  private sqsClient: SQSClient | null = null;
  private snsClient: SNSClient | null = null;
  private running = false;
  private queueUrl: string = '';
  private subscriptionArn: string = '';
  private deviceId: string = '';
  private snsTopicArn: string = '';
  private debounceTimer: NodeJS.Timeout | null = null;
  private onCloudChangeCallback: OnCloudChangeCallback | null = null;
  private lastLog: (msg: string) => void = () => {};
  private backoffMs = 0;
  private maxBackoffMs = 60000; // 60 seconds

  async start(config: ObsyncianConfig, onLog: (msg: string) => void): Promise<void> {
    if (!config.snsTopicArn) {
      onLog('[SQS] No SNS topic configured, skipping SQS listener');
      return;
    }

    this.lastLog = onLog;
    this.deviceId = config.id;
    this.snsTopicArn = config.snsTopicArn;

    this.sqsClient = new SQSClient({
      region: config.region,
      credentials: {
        accessKeyId: config.credentials.key,
        secretAccessKey: config.credentials.secret,
      },
    });

    this.snsClient = new SNSClient({
      region: config.region,
      credentials: {
        accessKeyId: config.credentials.key,
        secretAccessKey: config.credentials.secret,
      },
    });

    try {
      // Create SQS queue
      const queueName = `obsyncian-${this.deviceId}`;
      this.lastLog(`[SQS] Creating queue: ${queueName}`);

      const createQueueResponse = await this.sqsClient.send(
        new CreateQueueCommand({
          QueueName: queueName,
          Attributes: {
            ReceiveMessageWaitTimeSeconds: '20', // long poll
            MessageRetentionPeriod: '300', // 5 minutes
            VisibilityTimeout: '30',
          },
        }),
      );

      this.queueUrl = createQueueResponse.QueueUrl || '';
      if (!this.queueUrl) throw new Error('Failed to create SQS queue');

      this.lastLog(`[SQS] Queue created: ${this.queueUrl}`);

      // Subscribe to SNS
      this.lastLog('[SQS] Subscribing to SNS topic');

      // Get queue ARN
      const queueArn = await this.getQueueArn();

      // Set queue policy to allow SNS
      await this.setQueuePolicy(queueArn);

      const subscribeResponse = await this.snsClient.send(
        new SubscribeCommand({
          TopicArn: this.snsTopicArn,
          Protocol: 'sqs',
          Endpoint: queueArn,
        }),
      );

      this.subscriptionArn = subscribeResponse.SubscriptionArn || '';
      this.lastLog(`[SQS] Subscribed to SNS: ${this.subscriptionArn}`);

      this.running = true;
      this.backoffMs = 0;
      this.pollLoop();
    } catch (error) {
      this.lastLog(`[SQS] Start error: ${error}`);
      throw error;
    }
  }

  async stop(): Promise<void> {
    this.running = false;

    if (this.debounceTimer) {
      clearTimeout(this.debounceTimer);
      this.debounceTimer = null;
    }

    try {
      // Unsubscribe from SNS
      if (this.subscriptionArn && this.snsClient) {
        this.lastLog('[SQS] Unsubscribing from SNS');
        await this.snsClient.send(
          new UnsubscribeCommand({
            SubscriptionArn: this.subscriptionArn,
          }),
        );
      }

      // Delete queue
      if (this.queueUrl && this.sqsClient) {
        this.lastLog('[SQS] Deleting queue');
        await this.sqsClient.send(
          new DeleteQueueCommand({
            QueueUrl: this.queueUrl,
          }),
        );
      }

      this.lastLog('[SQS] Listener stopped');
    } catch (error) {
      this.lastLog(`[SQS] Stop error: ${error}`);
    }
  }

  setOnCloudChange(callback: OnCloudChangeCallback): void {
    this.onCloudChangeCallback = callback;
  }

  private async pollLoop(): Promise<void> {
    while (this.running) {
      if (this.backoffMs > 0) {
        // Wait before next poll (exponential backoff)
        await this.sleep(this.backoffMs);
      }

      try {
        await this.receiveAndProcess();
      } catch (error) {
        if (!this.running) return;

        this.lastLog(`[SQS] Poll error: ${error}`);
        // On error, apply backoff
        this.backoffMs = Math.min(
          this.backoffMs === 0 ? 5000 : this.backoffMs * 2,
          this.maxBackoffMs,
        );
      }
    }
  }

  private async receiveAndProcess(): Promise<void> {
    if (!this.sqsClient) throw new Error('SQS client not initialized');

    const response = await this.sqsClient.send(
      new ReceiveMessageCommand({
        QueueUrl: this.queueUrl,
        MaxNumberOfMessages: 10,
        WaitTimeSeconds: 20,
      }),
    );

    if (!response.Messages || response.Messages.length === 0) {
      // No messages: increase backoff
      this.backoffMs = Math.min(
        this.backoffMs === 0 ? 5000 : this.backoffMs * 2,
        this.maxBackoffMs,
      );
      return;
    }

    // Messages received: reset backoff
    this.backoffMs = 0;

    // Delete messages
    for (const message of response.Messages) {
      if (message.ReceiptHandle) {
        try {
          await this.sqsClient.send(
            new DeleteMessageCommand({
              QueueUrl: this.queueUrl,
              ReceiptHandle: message.ReceiptHandle,
            }),
          );
        } catch (error) {
          this.lastLog(`[SQS] Failed to delete message: ${error}`);
        }
      }
    }

    // Trigger sync with debounce (2 seconds)
    this.debouncedSync();
  }

  private debouncedSync(): void {
    if (this.debounceTimer) {
      clearTimeout(this.debounceTimer);
    }

    this.debounceTimer = setTimeout(() => {
      this.lastLog('[SQS] Cloud change detected, triggering sync');
      if (this.onCloudChangeCallback) {
        try {
          this.onCloudChangeCallback();
        } catch (error) {
          this.lastLog(`[SQS] onCloudChange callback error: ${error}`);
        }
      }
      this.debounceTimer = null;
    }, 2000);
  }

  private async getQueueArn(): Promise<string> {
    if (!this.sqsClient) throw new Error('SQS client not initialized');

    const resp = await this.sqsClient.send(
      new GetQueueAttributesCommand({
        QueueUrl: this.queueUrl,
        AttributeNames: ['QueueArn'],
      }),
    );

    const arn = resp.Attributes?.QueueArn;
    if (!arn) throw new Error('Could not retrieve queue ARN');
    return arn;
  }

  private async setQueuePolicy(queueArn: string): Promise<void> {
    if (!this.sqsClient) return;

    const policy = JSON.stringify({
      Version: '2012-10-17',
      Statement: [
        {
          Effect: 'Allow',
          Principal: { Service: 'sns.amazonaws.com' },
          Action: 'sqs:SendMessage',
          Resource: queueArn,
          Condition: {
            ArnEquals: { 'aws:SourceArn': this.snsTopicArn },
          },
        },
      ],
    });

    await this.sqsClient.send(
      new SetQueueAttributesCommand({
        QueueUrl: this.queueUrl,
        Attributes: { Policy: policy },
      }),
    );
  }

  private sleep(ms: number): Promise<void> {
    return new Promise((resolve) => setTimeout(resolve, ms));
  }
}

export const sqsListenerService = new SQSListenerService();
