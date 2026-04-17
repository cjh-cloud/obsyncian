import { create } from 'zustand';
import { ObsyncianConfig } from '../models/config';
import { syncOrchestrator, SyncState } from '../services/syncOrchestrator';
import { connectivityService } from '../services/connectivityService';
import { configService } from '../services/configService';
import { sqsListenerService } from '../services/sqsListenerService';
import { backgroundSyncService } from '../services/backgroundService';

export interface AppStore {
  config: ObsyncianConfig | null;
  vaultPath: string | null;
  syncState: SyncState;
  isOnline: boolean;
  logs: string[];
  maxLogs: number;

  // Actions
  setConfig: (config: ObsyncianConfig | null) => void;
  setVaultPath: (path: string | null) => void;
  setSyncState: (state: SyncState) => void;
  setIsOnline: (isOnline: boolean) => void;
  addLog: (msg: string) => void;
  clearLogs: () => void;
  triggerSync: () => Promise<void>;
}

export const useAppStore = create<AppStore>((set, get) => ({
  config: null,
  vaultPath: null,
  syncState: 'idle',
  isOnline: true,
  logs: [],
  maxLogs: 500,

  setConfig: (config) => set({ config }),

  setVaultPath: (path) => set({ vaultPath: path }),

  setSyncState: (state) => {
    set({ syncState: state });

    // Update the foreground-service notification to reflect current state
    switch (state) {
      case 'syncing':
        backgroundSyncService.updateNotification('Syncing your vault\u2026');
        break;
      case 'idle': {
        const time = new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
        backgroundSyncService.updateNotification(`Vault synced \u2014 last sync ${time}`);
        break;
      }
      case 'error':
        backgroundSyncService.updateNotification('Sync failed \u2014 will retry');
        break;
      case 'offline':
        backgroundSyncService.updateNotification('Offline \u2014 waiting for connection');
        break;
    }
  },

  setIsOnline: (isOnline) => {
    set({ isOnline });
    syncOrchestrator.setIsOnline(isOnline);
  },

  addLog: (msg) => {
    set((state) => {
      const newLogs = [...state.logs, msg];
      // Keep only last 500 logs
      if (newLogs.length > state.maxLogs) {
        newLogs.shift();
      }
      return { logs: newLogs };
    });
  },

  clearLogs: () => set({ logs: [] }),

  triggerSync: async () => {
    const state = get();
    if (!state.config) {
      get().addLog('[App] No config loaded, cannot sync');
      return;
    }

    try {
      await syncOrchestrator.sync(state.config.id);
    } catch (error) {
      get().addLog(`[App] Sync error: ${error}`);
    }
  },
}));

// Setup service callbacks
export async function initializeAppStore(): Promise<void> {
  const store = useAppStore.getState();

  // Sync orchestrator
  syncOrchestrator.setOnStateChange((state) => {
    store.setSyncState(state);
  });

  syncOrchestrator.setOnLog((msg) => {
    store.addLog(msg);
  });

  syncOrchestrator.setOnSyncProgress((progress) => {
    switch (progress.phase) {
      case 'listing':
        backgroundSyncService.updateNotification('Comparing local and cloud files\u2026');
        break;
      case 'uploading':
        backgroundSyncService.updateNotification(
          `Uploading ${progress.current}/${progress.total} files\u2026`,
        );
        break;
      case 'downloading':
        backgroundSyncService.updateNotification(
          `Downloading ${progress.current}/${progress.total} files\u2026`,
        );
        break;
      case 'deleting':
        backgroundSyncService.updateNotification(
          `Cleaning up ${progress.current}/${progress.total}\u2026`,
        );
        break;
      // 'done' is handled by setSyncState → 'idle'
    }
  });

  // Connectivity service
  await connectivityService.checkInitialState();
  store.setIsOnline(connectivityService.getIsOnline());

  connectivityService.onReconnect(() => {
    store.addLog('[App] Reconnected to internet, triggering sync');
    backgroundSyncService.updateNotification('Back online \u2014 syncing\u2026');
    store.triggerSync();
  });

  // SQS listener
  sqsListenerService.setOnCloudChange(() => {
    store.addLog('[App] Cloud change detected via SQS');
    backgroundSyncService.updateNotification('Cloud update received \u2014 syncing\u2026');
    store.triggerSync();
  });

  // Background service
  backgroundSyncService.setOnSyncRequest(() => {
    store.triggerSync();
  });

  // Load saved config
  const savedConfig = await configService.loadSavedConfig();
  if (savedConfig) {
    store.setConfig(savedConfig.config);

    const vaultPath = await configService.getVaultPath();
    if (vaultPath) {
      store.setVaultPath(vaultPath);
    }
  }
}
