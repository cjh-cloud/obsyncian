import {
  SQSClient,
  CreateQueueCommand,
  GetQueueUrlCommand,
  GetQueueAttributesCommand,
  SetQueueAttributesCommand,
  ReceiveMessageCommand,
  DeleteMessageCommand,
} from '@aws-sdk/client-sqs';
import {
  SNSClient,
  SubscribeCommand,
} from '@aws-sdk/client-sns';
import BackgroundTimer from 'react-native-background-timer';
import { ObsyncianConfig } from '../models/config';

type OnCloudChangeCallback = () => void;

class SQSListenerService {
  private sqsClient: SQSClient | null = null;
  private snsClient: SNSClient | null = null;
  private queueUrl: string = '';
  private subscriptionArn: string = '';
  private deviceId: string = '';
  private snsTopicArn: string = '';
  private debounceTimerId: number | null = null;
  private polling = false; // guard against overlapping polls
  private onCloudChangeCallback: OnCloudChangeCallback | null = null;
  private lastLog: (msg: string) => void = () => {};

  /**
   * Create SQS queue + SNS subscription. Does NOT start polling.
   * Call `startPolling()` after setup.
   */
  async setup(config: ObsyncianConfig, onLog: (msg: string) => void): Promise<void> {
    if (!config.snsTopicArn) {
      onLog('[SQS] No SNS topic configured, skipping SQS setup');
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
      const queueName = `obsyncian-${this.deviceId}`;
      this.queueUrl = await this.getOrCreateQueue(queueName);
      this.lastLog(`[SQS] Queue ready: ${this.queueUrl}`);

      const queueArn = await this.getQueueArn();
      await this.setQueuePolicy(queueArn);

      this.lastLog('[SQS] Subscribing to SNS topic');
      const subscribeResponse = await this.snsClient.send(
        new SubscribeCommand({
          TopicArn: this.snsTopicArn,
          Protocol: 'sqs',
          Endpoint: queueArn,
        }),
      );

      this.subscriptionArn = subscribeResponse.SubscriptionArn || '';
      this.lastLog(`[SQS] Subscribed to SNS: ${this.subscriptionArn}`);
    } catch (error) {
      this.lastLog(`[SQS] Setup error: ${error}`);
      throw error;
    }
  }

  get isReady(): boolean {
    return !!this.queueUrl && !!this.sqsClient;
  }

  /**
   * Called every tick by the background service's unified timer.
   * Short-polls SQS once. Non-blocking: skips if a previous poll is still in flight.
   */
  pollOnce(): void {
    if (!this.isReady) return;
    this.tick();
  }

  async stop(): Promise<void> {
    if (this.debounceTimerId !== null) {
      BackgroundTimer.clearTimeout(this.debounceTimerId);
      this.debounceTimerId = null;
    }

    // Keep queue alive — recreating after delete has a 60s cooldown that causes
    // startup failures. SNS subscription also persists so messages keep arriving.
    this.lastLog('[SQS] Listener stopped (queue + subscription kept for next startup)');
    this.queueUrl = '';
    this.subscriptionArn = '';
  }

  setOnCloudChange(callback: OnCloudChangeCallback): void {
    this.onCloudChangeCallback = callback;
  }

  private async tick(): Promise<void> {
    if (this.polling || !this.sqsClient) return; // skip if previous poll still running
    this.polling = true;

    try {
      const response = await this.sqsClient.send(
        new ReceiveMessageCommand({
          QueueUrl: this.queueUrl,
          MaxNumberOfMessages: 10,
          WaitTimeSeconds: 0, // short poll — returns immediately
        }),
      );

      if (response.Messages && response.Messages.length > 0) {
        // Delete messages
        for (const msg of response.Messages) {
          if (msg.ReceiptHandle) {
            try {
              await this.sqsClient.send(
                new DeleteMessageCommand({
                  QueueUrl: this.queueUrl,
                  ReceiptHandle: msg.ReceiptHandle,
                }),
              );
            } catch (e) {
              this.lastLog(`[SQS] Delete message error: ${e}`);
            }
          }
        }
        this.debouncedSync();
      }
    } catch (error) {
      this.lastLog(`[SQS] Poll error: ${error}`);
    } finally {
      this.polling = false;
    }
  }

  private debouncedSync(): void {
    if (this.debounceTimerId !== null) {
      BackgroundTimer.clearTimeout(this.debounceTimerId);
    }

    this.debounceTimerId = BackgroundTimer.setTimeout(() => {
      this.lastLog('[SQS] Cloud change detected, triggering sync');
      if (this.onCloudChangeCallback) {
        try {
          this.onCloudChangeCallback();
        } catch (error) {
          this.lastLog(`[SQS] onCloudChange callback error: ${error}`);
        }
      }
      this.debounceTimerId = null;
    }, 2000);
  }

  /** Get existing queue URL, or create if it doesn't exist. */
  private async getOrCreateQueue(queueName: string): Promise<string> {
    if (!this.sqsClient) throw new Error('SQS client not initialized');

    // Try to get existing queue first
    try {
      const resp = await this.sqsClient.send(
        new GetQueueUrlCommand({ QueueName: queueName }),
      );
      if (resp.QueueUrl) {
        this.lastLog(`[SQS] Using existing queue: ${queueName}`);
        return resp.QueueUrl;
      }
    } catch (e: any) {
      // QueueDoesNotExist → fall through to create
      if (e?.name !== 'QueueDoesNotExist' && e?.name !== 'AWS.SimpleQueueService.NonExistentQueue') {
        throw e;
      }
    }

    // Queue doesn't exist — create it
    this.lastLog(`[SQS] Creating queue: ${queueName}`);
    const resp = await this.sqsClient.send(
      new CreateQueueCommand({
        QueueName: queueName,
        Attributes: {
          ReceiveMessageWaitTimeSeconds: '0',
          MessageRetentionPeriod: '300',
          VisibilityTimeout: '30',
        },
      }),
    );

    if (!resp.QueueUrl) throw new Error('Failed to create SQS queue');
    return resp.QueueUrl;
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
}

export const sqsListenerService = new SQSListenerService();
