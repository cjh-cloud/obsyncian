import React, { useEffect } from 'react';
import { AppState, AppStateStatus, StatusBar } from 'react-native';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import { HomeScreen } from './src/screens/HomeScreen';
import { useAppStore, initializeAppStore } from './src/store/appStore';
import { connectivityService } from './src/services/connectivityService';
import { syncOrchestrator } from './src/services/syncOrchestrator';

let isInitialized = false;

function App() {
  const { config, vaultPath, addLog, triggerSync } = useAppStore();

  useEffect(() => {
    if (isInitialized) return;
    isInitialized = true;

    const initApp = async () => {
      try {
        await initializeAppStore();
        await connectivityService.checkInitialState();
        connectivityService.start();

        const state = useAppStore.getState();
        if (state.config && state.vaultPath) {
          await syncOrchestrator.init(state.config, state.vaultPath, state.isOnline);
        }
      } catch (error) {
        console.error('App initialization error:', error);
        useAppStore.getState().addLog(`[App] Initialization error: ${error}`);
      }
    };

    initApp();

    const handleAppStateChange = (state: AppStateStatus) => {
      if (state === 'active') {
        addLog('[App] Resumed, triggering sync');
        triggerSync();
      }
    };

    const subscription = AppState.addEventListener('change', handleAppStateChange);

    return () => {
      subscription.remove();
    };
  }, []);

  useEffect(() => {
    if (!config || !vaultPath) return;

    const reInit = async () => {
      try {
        await syncOrchestrator.init(config, vaultPath, useAppStore.getState().isOnline);
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
