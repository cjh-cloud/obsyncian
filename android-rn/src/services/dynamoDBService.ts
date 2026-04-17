import {
  DynamoDBClient,
  ScanCommand,
  GetItemCommand,
  PutItemCommand,
  UpdateItemCommand,
} from '@aws-sdk/client-dynamodb';
import { ObsyncianConfig } from '../models/config';

const TABLE_NAME = 'Obsyncian';

export interface UserSync {
  userId: string;
  timestamp: string; // YYYYMMDDHHmmss (UTC)
}

class DynamoDBService {
  private client: DynamoDBClient | null = null;
  private lastLog: (msg: string) => void = () => {};

  async init(config: ObsyncianConfig, onLog: (msg: string) => void): Promise<void> {
    this.lastLog = onLog;
    this.client = new DynamoDBClient({
      region: config.region,
      credentials: {
        accessKeyId: config.credentials.key,
        secretAccessKey: config.credentials.secret,
      },
    });
  }

  async getLatestSync(): Promise<UserSync | null> {
    if (!this.client) throw new Error('DynamoDBService not initialized');

    try {
      this.lastLog('[DynamoDB] Scanning for latest timestamp');
      let latest: UserSync | null = null;
      let exclusiveStartKey: any;

      while (true) {
        const response = await this.client.send(
          new ScanCommand({
            TableName: TABLE_NAME,
            ExclusiveStartKey: exclusiveStartKey,
          }),
        );

        if (response.Items) {
          for (const item of response.Items) {
            const userId = item.UserId?.S;
            const timestamp = item.Timestamp?.S;

            if (userId && timestamp) {
              if (!latest || timestamp > latest.timestamp) {
                latest = { userId, timestamp };
              }
            }
          }
        }

        if (!response.LastEvaluatedKey) break;
        exclusiveStartKey = response.LastEvaluatedKey;
      }

      if (latest) {
        this.lastLog(`[DynamoDB] Latest sync: ${latest.userId} @ ${latest.timestamp}`);
      } else {
        this.lastLog('[DynamoDB] Table is empty');
      }

      return latest;
    } catch (error) {
      this.lastLog(`[DynamoDB] getLatestSync error: ${error}`);
      throw error;
    }
  }

  async getUser(userId: string): Promise<UserSync | null> {
    if (!this.client) throw new Error('DynamoDBService not initialized');

    try {
      const response = await this.client.send(
        new GetItemCommand({
          TableName: TABLE_NAME,
          Key: {
            UserId: { S: userId },
          },
        }),
      );

      if (response.Item?.Timestamp?.S) {
        return {
          userId,
          timestamp: response.Item.Timestamp.S,
        };
      }

      return null;
    } catch (error) {
      this.lastLog(`[DynamoDB] getUser error: ${error}`);
      throw error;
    }
  }

  async createUser(userId: string): Promise<void> {
    if (!this.client) throw new Error('DynamoDBService not initialized');

    const timestamp = this.nowTimestamp();
    this.lastLog(`[DynamoDB] Creating user ${userId} with timestamp ${timestamp}`);

    try {
      await this.client.send(
        new PutItemCommand({
          TableName: TABLE_NAME,
          Item: {
            UserId: { S: userId },
            Timestamp: { S: timestamp },
          },
        }),
      );
    } catch (error) {
      this.lastLog(`[DynamoDB] createUser error: ${error}`);
      throw error;
    }
  }

  async updateTimestamp(userId: string): Promise<void> {
    if (!this.client) throw new Error('DynamoDBService not initialized');

    const timestamp = this.nowTimestamp();
    this.lastLog(`[DynamoDB] Updating ${userId} timestamp to ${timestamp}`);

    try {
      await this.client.send(
        new UpdateItemCommand({
          TableName: TABLE_NAME,
          Key: {
            UserId: { S: userId },
          },
          UpdateExpression: 'SET #ts = :ts',
          ExpressionAttributeNames: {
            '#ts': 'Timestamp',
          },
          ExpressionAttributeValues: {
            ':ts': { S: timestamp },
          },
        }),
      );
    } catch (error) {
      this.lastLog(`[DynamoDB] updateTimestamp error: ${error}`);
      throw error;
    }
  }

  private nowTimestamp(): string {
    // Local time, not UTC — matches the Go app's time.Now().Format("20060102150405")
    // and the Flutter app's DateTime.now(). All devices must use the same convention
    // so that lexicographic timestamp comparison in the orchestrator works correctly.
    const now = new Date();
    const year = now.getFullYear();
    const month = String(now.getMonth() + 1).padStart(2, '0');
    const day = String(now.getDate()).padStart(2, '0');
    const hour = String(now.getHours()).padStart(2, '0');
    const minute = String(now.getMinutes()).padStart(2, '0');
    const second = String(now.getSeconds()).padStart(2, '0');

    return `${year}${month}${day}${hour}${minute}${second}`;
  }
}

export const dynamoDBService = new DynamoDBService();
