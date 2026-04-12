import React, { useEffect } from 'react';
import { AppState, AppStateStatus, StatusBar } from 'react-native';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import { HomeScreen } from './src/screens/HomeScreen';
import {
  useAppStore,
  initializeAppStore,
} from './src/store/appStore';
import { connectivityService } from './src/services/connectivityService';
import { syncOrchestrator } from './src/services/syncOrchestrator';
import { sqsListenerService } from './src/services/sqsListenerService';
import { backgroundSyncService } from './src/services/backgroundService';

let isInitialized = false;

function App() {
  const { config, vaultPath, addLog, triggerSync } = useAppStore();

  useEffect(() => {
    // Initialize app on mount
    if (isInitialized) return;
    isInitialized = true;

    const initApp = async () => {
      try {
        await initializeAppStore();
        await connectivityService.checkInitialState();
        connectivityService.start();

        // Load config and initialize services
        const state = useAppStore.getState();
        if (state.config && state.vaultPath) {
          await syncOrchestrator.init(state.config, state.vaultPath, state.isOnline);

          if (state.config.snsTopicArn) {
            await sqsListenerService.start(state.config, state.addLog);
          }

          await backgroundSyncService.start(state.addLog);
        }
      } catch (error) {
        console.error('App initialization error:', error);
        useAppStore.getState().addLog(`[App] Initialization error: ${error}`);
      }
    };

    initApp();

    // App lifecycle listener
    const handleAppStateChange = (state: AppStateStatus) => {
      if (state === 'active') {
        addLog('[App] Resumed, triggering sync');
        triggerSync();
      } else if (state === 'background') {
        addLog('[App] Paused, triggering sync');
        triggerSync();
      }
    };

    const subscription = AppState.addEventListener('change', handleAppStateChange);

    return () => {
      subscription.remove();
    };
  }, []);

  // Re-initialize services when config changes
  useEffect(() => {
    if (!config || !vaultPath) return;

    const reInit = async () => {
      try {
        await sqsListenerService.stop();
        await backgroundSyncService.stop();

        await syncOrchestrator.init(config, vaultPath, useAppStore.getState().isOnline);

        if (config.snsTopicArn) {
          await sqsListenerService.start(config, useAppStore.getState().addLog);
        }

        await backgroundSyncService.start(useAppStore.getState().addLog);
      } catch (error) {
        console.error('App re-initialization error:', error);
        useAppStore.getState().addLog(`[App] Re-initialization error: ${error}`);
      }
    };

    reInit();
  }, [config, vaultPath]);

  return (
    <SafeAreaProvider>
      <StatusBar barStyle="dark-content" />
      <HomeScreen />
    </SafeAreaProvider>
  );
}

export default App;
