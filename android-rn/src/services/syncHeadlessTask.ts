import { NativeModules } from 'react-native';
import { configService } from './configService';
import { syncOrchestrator } from './syncOrchestrator';
import { connectivityService } from './connectivityService';

const { ObsyncianWidget } = NativeModules;

async function syncHeadlessTask(): Promise<void> {
  try {
    await ObsyncianWidget.setWidgetState('syncing', '');

    const savedConfig = await configService.loadSavedConfig();
    if (!savedConfig) {
      await ObsyncianWidget.setWidgetState('not_synced', '');
      return;
    }

    const vaultPath = await configService.getVaultPath();
    if (!vaultPath) {
      await ObsyncianWidget.setWidgetState('not_synced', '');
      return;
    }

    await connectivityService.checkInitialState();
    const isOnline = connectivityService.getIsOnline();

    if (!isOnline) {
      await ObsyncianWidget.setWidgetState('offline', '');
      return;
    }

    await syncOrchestrator.init(savedConfig.config, vaultPath, isOnline);
    await syncOrchestrator.sync(savedConfig.config.id);

    const now = new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
    await ObsyncianWidget.setWidgetState('idle', now);
  } catch (error) {
    console.error('[HeadlessTask] Sync error:', error);
    const now = new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
    await ObsyncianWidget.setWidgetState('error', now);
  }
}

export default syncHeadlessTask;
