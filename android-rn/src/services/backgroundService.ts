import BackgroundActions from 'react-native-background-actions';
import { connectivityService } from './connectivityService';
import { syncOrchestrator } from './syncOrchestrator';

type OnSyncRequestCallback = () => void;

const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

class BackgroundSyncService {
  private onSyncRequest: OnSyncRequestCallback | null = null;
  private lastLog: (msg: string) => void = () => {};
  private isStarted = false;

  async start(onLog: (msg: string) => void): Promise<void> {
    this.lastLog = onLog;

    if (this.isStarted) {
      this.lastLog('[Background] Already started');
      return;
    }

    try {
      this.lastLog('[Background] Starting background service');

      const task = async (taskData: any) => {
        this.lastLog('[Background] Background task started');

        while (BackgroundActions.isRunning()) {
          // Wait 5 minutes
          await sleep(5 * 60 * 1000);

          // Check if online before syncing
          const isOnline = connectivityService.getIsOnline();
          if (isOnline) {
            this.lastLog('[Background] Periodic sync triggered');
            if (this.onSyncRequest) {
              try {
                this.onSyncRequest();
              } catch (error) {
                this.lastLog(`[Background] Sync request error: ${error}`);
              }
            }
          } else {
            this.lastLog('[Background] Offline, skipping periodic sync');
          }
        }

        this.lastLog('[Background] Background task stopped');
      };

      await BackgroundActions.start(task, {
        taskName: 'ObsyncianSync',
        taskTitle: 'Obsyncian',
        taskDesc: 'Syncing your vault in the background',
        taskIcon: {
          name: 'ic_launcher',
          type: 'mipmap',
        },
        color: '#ff00ff',
        linkingURI: 'obsyncian://',
      });

      this.isStarted = true;
      this.lastLog('[Background] Background service started');
    } catch (error) {
      this.lastLog(`[Background] Start error: ${error}`);
      throw error;
    }
  }

  async stop(): Promise<void> {
    try {
      await BackgroundActions.stop();
      this.isStarted = false;
      this.lastLog('[Background] Background service stopped');
    } catch (error) {
      this.lastLog(`[Background] Stop error: ${error}`);
    }
  }

  setOnSyncRequest(callback: OnSyncRequestCallback): void {
    this.onSyncRequest = callback;
  }
}

export const backgroundSyncService = new BackgroundSyncService();
