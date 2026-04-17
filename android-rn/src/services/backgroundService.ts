import BackgroundActions from 'react-native-background-actions';
import BackgroundTimer from 'react-native-background-timer';
import { NativeModules, PermissionsAndroid, Platform } from 'react-native';
import { connectivityService } from './connectivityService';
import { sqsListenerService } from './sqsListenerService';
import { ObsyncianConfig } from '../models/config';

type ObsyncianStorageNative = {
  isIgnoringBatteryOptimizations: () => Promise<boolean>;
  requestIgnoreBatteryOptimizations: () => Promise<void>;
};

const native = NativeModules.ObsyncianStorage as ObsyncianStorageNative | undefined;

type OnSyncRequestCallback = () => void;

/**
 * Single unified tick interval (ms). Every tick:
 *  1) Polls SQS for messages
 *  2) Checks if periodic sync is due
 *  3) Fires sync if needed
 *
 * 2 s is fast enough that each tick also acts as a JS-thread keepalive:
 * the native Handler wakes the thread, which processes any pending
 * HTTP-response events from an in-flight sync, allowing the async
 * chain to advance one step. A 5-step sync completes in ~10 s.
 */
const TICK_MS = 2_000;

/** 5-minute periodic sync fallback. */
const PERIODIC_SYNC_MS = 5 * 60 * 1000;

class BackgroundSyncService {
  private onSyncRequest: OnSyncRequestCallback | null = null;
  private lastLog: (msg: string) => void = () => {};
  private isStarted = false;
  private lastPeriodicSyncAt = 0;

  // ── permissions ──────────────────────────────────────────────────────

  private async requestNotificationPermission(): Promise<void> {
    if (Platform.OS !== 'android') return;
    if ((Platform.Version as number) < 33) return;

    try {
      const granted = await PermissionsAndroid.check(
        PermissionsAndroid.PERMISSIONS.POST_NOTIFICATIONS,
      );
      if (granted) return;

      this.lastLog('[Background] Requesting notification permission');
      const result = await PermissionsAndroid.request(
        PermissionsAndroid.PERMISSIONS.POST_NOTIFICATIONS,
      );
      this.lastLog(`[Background] POST_NOTIFICATIONS: ${result}`);
    } catch (error) {
      this.lastLog(`[Background] Notification permission error: ${error}`);
    }
  }

  private async requestBatteryOptimizationExemption(): Promise<void> {
    if (Platform.OS !== 'android') return;
    if (!native?.isIgnoringBatteryOptimizations) return;

    try {
      const exempt = await native.isIgnoringBatteryOptimizations();
      if (exempt) {
        this.lastLog('[Background] Already exempt from battery optimization');
        return;
      }

      this.lastLog('[Background] Requesting battery optimization exemption');
      await native.requestIgnoreBatteryOptimizations();
    } catch (error) {
      this.lastLog(`[Background] Battery optimization request error: ${error}`);
    }
  }

  // ── lifecycle ────────────────────────────────────────────────────────

  async start(config: ObsyncianConfig | null, onLog: (msg: string) => void): Promise<void> {
    this.lastLog = onLog;

    if (this.isStarted) {
      this.lastLog('[Background] Already started');
      return;
    }

    try {
      await this.requestNotificationPermission();
      await this.requestBatteryOptimizationExemption();
      this.lastLog('[Background] Starting background service');

      // Foreground service — keeps process alive + shows notification.
      const task = async (_taskData: any) => {
        this.lastLog('[Background] Foreground service task running');
        await new Promise<void>(() => {}); // never resolves
      };

      await BackgroundActions.start(task, {
        taskName: 'ObsyncianSync',
        taskTitle: 'Obsyncian',
        taskDesc: 'Running \u2014 waiting for changes',
        taskIcon: { name: 'ic_launcher', type: 'mipmap' },
        color: '#ff00ff',
        linkingURI: 'obsyncian://',
      });

      this.isStarted = true;
      this.lastLog('[Background] Foreground service started');

      // --- SQS setup ---
      if (config?.snsTopicArn) {
        try {
          await sqsListenerService.setup(config, this.lastLog);
          this.lastLog('[Background] SQS queue ready');
        } catch (error) {
          this.lastLog(`[Background] SQS setup failed: ${error}`);
        }
      }

      // --- Single unified background loop (recommended Android API) ---
      this.lastPeriodicSyncAt = Date.now();
      BackgroundTimer.runBackgroundTimer(() => this.tick(), TICK_MS);

      this.lastLog('[Background] Background timer started');
    } catch (error) {
      this.lastLog(`[Background] Start error: ${error}`);
      throw error;
    }
  }

  async stop(): Promise<void> {
    try {
      BackgroundTimer.stopBackgroundTimer();
      await sqsListenerService.stop();
      await BackgroundActions.stop();
      this.isStarted = false;
      this.lastLog('[Background] Background service stopped');
    } catch (error) {
      this.lastLog(`[Background] Stop error: ${error}`);
    }
  }

  // ── tick ──────────────────────────────────────────────────────────────

  /**
   * Fires every TICK_MS from a native Android Handler. Three jobs:
   *  1. Poll SQS (short-poll, returns immediately)
   *  2. Periodic sync check (every 5 min)
   *  3. Wakes JS thread so pending HTTP-response events from an
   *     in-flight sync are processed (acts as keepalive)
   */
  private tick(): void {
    // 1. SQS poll
    if (sqsListenerService.isReady) {
      sqsListenerService.pollOnce();
    }

    // 2. Periodic sync
    const now = Date.now();
    if (now - this.lastPeriodicSyncAt >= PERIODIC_SYNC_MS) {
      this.lastPeriodicSyncAt = now;
      if (connectivityService.getIsOnline()) {
        this.lastLog('[Background] Periodic sync triggered');
        this.requestSync();
      }
    }

    // 3. (Implicit) — the mere execution of this callback wakes the
    //    JS thread, flushing any queued HTTP-response events and
    //    advancing in-flight sync await chains.
  }

  private requestSync(): void {
    if (!this.onSyncRequest) return;
    try {
      this.onSyncRequest();
    } catch (error) {
      this.lastLog(`[Background] Sync request error: ${error}`);
    }
  }

  // ── notification ─────────────────────────────────────────────────────

  async updateNotification(description: string): Promise<void> {
    if (!this.isStarted) return;
    try {
      await BackgroundActions.updateNotification({
        taskTitle: 'Obsyncian',
        taskDesc: description,
      });
    } catch (error) {
      console.log('[Background] updateNotification error:', error);
    }
  }

  setOnSyncRequest(callback: OnSyncRequestCallback): void {
    this.onSyncRequest = callback;
  }
}

export const backgroundSyncService = new BackgroundSyncService();
