import AsyncStorage from '@react-native-async-storage/async-storage';
import { ObsyncianConfig } from '../models/config';
import { s3SyncService } from './s3SyncService';
import { dynamoDBService } from './dynamoDBService';
import { fileStatCache } from './fileStatCache';
import { ensureAndroidVaultStorageAccess } from './androidStoragePermission';

const LAST_SYNCED_TIMESTAMP_KEY = 'obsyncian_last_synced_timestamp';

export type SyncState = 'idle' | 'syncing' | 'error' | 'offline';

type OnStateChangeCallback = (state: SyncState) => void;
type OnLogCallback = (msg: string) => void;

class SyncOrchestrator {
  private isSyncing = false;
  private isSyncQueued = false;
  private lastSyncedTimestamp: string = '';
  private onStateChange: OnStateChangeCallback | null = null;
  private onLog: OnLogCallback | null = null;
  private isOnline = true;
  private vaultPath: string = '';

  async init(config: ObsyncianConfig, vaultPath: string, isOnline: boolean): Promise<void> {
    this.vaultPath = vaultPath;
    this.isOnline = isOnline;
    const onLog = (msg: string) => this.log(msg);

    // Load last synced timestamp
    const saved = await AsyncStorage.getItem(LAST_SYNCED_TIMESTAMP_KEY);
    if (saved) {
      this.lastSyncedTimestamp = saved;
      this.log(`[Sync] Loaded last synced timestamp: ${saved}`);
    }

    // Init services
    await s3SyncService.init(config, vaultPath, onLog);
    await dynamoDBService.init(config, onLog);
    await fileStatCache.load();
  }

  setOnStateChange(callback: OnStateChangeCallback): void {
    this.onStateChange = callback;
  }

  setOnLog(callback: OnLogCallback): void {
    this.onLog = callback;
  }

  setIsOnline(isOnline: boolean): void {
    this.isOnline = isOnline;
  }

  async sync(deviceId: string): Promise<void> {
    if (this.isSyncing) {
      this.log('[Sync] Already syncing, queueing next sync');
      this.isSyncQueued = true;
      return;
    }

    this.isSyncing = true;
    this.changeState('syncing');

    try {
      await this.handleSync(deviceId);

      if (this.isSyncQueued) {
        this.log('[Sync] Queued sync triggered');
        this.isSyncQueued = false;
        await this.sync(deviceId);
      }

      this.changeState('idle');
    } catch (error) {
      this.log(`[Sync] Error: ${error}`);
      this.changeState('error');
    } finally {
      this.isSyncing = false;
    }
  }

  private async handleSync(deviceId: string): Promise<void> {
    this.log('[Sync] Starting sync cycle');

    if (!this.isOnline) {
      this.log('[Sync] Offline, skipping sync');
      this.changeState('offline');
      return;
    }

    await ensureAndroidVaultStorageAccess(this.vaultPath, (msg) => this.log(msg));

    // 1. Scan DynamoDB for latest timestamp
    const latestSync = await dynamoDBService.getLatestSync();

    // 2. If table is empty
    if (!latestSync) {
      this.log('[Sync] Table empty, syncing up and creating user');
      await s3SyncService.syncUp();
      await dynamoDBService.createUser(deviceId);
      await dynamoDBService.updateTimestamp(deviceId);
      return;
    }

    // 3. Check if our user exists
    const ourUser = await dynamoDBService.getUser(deviceId);

    if (!ourUser) {
      this.log('[Sync] User not found in table, creating and syncing down');
      await dynamoDBService.createUser(deviceId);
      await s3SyncService.syncDown();
      // Remember the DynamoDB cloud revision we pulled (same as Flutter).
      await this.saveLastPulledCloudRevision(latestSync.timestamp);
      return;
    }

    // 4. Sync down only if cloud has a newer revision than the last one we pulled (YYYYMMDD… UTC strings).
    const shouldSyncDown =
      latestSync.userId !== deviceId &&
      latestSync.timestamp >= ourUser.timestamp &&
      (!this.lastSyncedTimestamp || this.lastSyncedTimestamp < latestSync.timestamp);

    if (shouldSyncDown) {
      this.log(
        `[Sync] Cloud has a newer revision (${latestSync.userId} @ ${latestSync.timestamp}), syncing down`,
      );
      await s3SyncService.syncDown();
      await this.saveLastPulledCloudRevision(latestSync.timestamp);
      return;
    }

    // 5. Dry run to check for local changes
    this.log('[Sync] Checking for local changes');
    const diff = await s3SyncService.dryRun();

    if (diff.toUpload.length > 0 || diff.toDelete.length > 0) {
      this.log('[Sync] Local changes detected, syncing up');
      await s3SyncService.syncUp();
      await dynamoDBService.updateTimestamp(deviceId);
    } else {
      this.log('[Sync] No local changes detected');
    }

    // Save file stat cache
    await fileStatCache.save();
  }

  /**
   * Persist the DynamoDB “latest cloud sync” timestamp we have fully applied locally.
   * Must be the same YYYYMMDDHHmmss string as in the Obsyncian table (not wall-clock).
   */
  private async saveLastPulledCloudRevision(timestamp: string): Promise<void> {
    this.lastSyncedTimestamp = timestamp;
    await AsyncStorage.setItem(LAST_SYNCED_TIMESTAMP_KEY, timestamp);
    this.log(`[Sync] Saved last pulled cloud revision: ${timestamp}`);
  }

  private log(msg: string): void {
    const timestamp = new Date().toLocaleTimeString();
    const logMsg = `[${timestamp}] ${msg}`;
    console.log(logMsg);
    if (this.onLog) {
      this.onLog(logMsg);
    }
  }

  private changeState(state: SyncState): void {
    if (this.onStateChange) {
      this.onStateChange(state);
    }
  }
}

export const syncOrchestrator = new SyncOrchestrator();
