import React, { useCallback, useState } from 'react';
import {
  View,
  Text,
  StyleSheet,
  TouchableOpacity,
  ActivityIndicator,
  SafeAreaView,
  ScrollView,
  Alert,
  Modal,
} from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { useAppStore } from '../store/appStore';
import { configService, VaultPathUnmappedError } from '../services/configService';

const LOGS_SCROLL_MAX_HEIGHT = 220;

export const HomeScreen: React.FC = () => {
  const insets = useSafeAreaInsets();
  const {
    config,
    vaultPath,
    syncState,
    isOnline,
    logs,
    setConfig,
    setVaultPath,
    triggerSync,
    clearLogs,
  } = useAppStore();

  const [menuVisible, setMenuVisible] = useState(false);

  const handlePickVaultPath = useCallback(async () => {
    try {
      const path = await configService.pickVaultFolder();
      if (path) {
        setVaultPath(path);
      }
    } catch (error) {
      if (error instanceof VaultPathUnmappedError) {
        Alert.alert('Cannot use this folder', error.message);
        return;
      }
      Alert.alert('Error', `Failed to pick vault folder: ${error}`);
    }
  }, [setVaultPath]);

  const handlePickConfig = useCallback(async () => {
    try {
      const result = await configService.pickConfigFile();
      if (result) {
        setConfig(result.config);
        handlePickVaultPath();
      }
    } catch (error) {
      Alert.alert('Error', `Failed to pick config: ${error}`);
    }
  }, [handlePickVaultPath]);

  const handleChangeConfig = useCallback(() => {
    setMenuVisible(false);
    handlePickConfig();
  }, [handlePickConfig]);

  const handleChangeVaultFolder = useCallback(() => {
    setMenuVisible(false);
    handlePickVaultPath();
  }, [handlePickVaultPath]);

  const handleClearConfig = useCallback(async () => {
    setMenuVisible(false);
    Alert.alert('Clear Config', 'Are you sure you want to clear the configuration?', [
      { text: 'Cancel', onPress: () => {}, style: 'cancel' },
      {
        text: 'Clear',
        onPress: async () => {
          await configService.clearSavedConfig();
          setConfig(null);
          setVaultPath(null);
          clearLogs();
        },
        style: 'destructive',
      },
    ]);
  }, []);

  const renderLogItem = (item: string, index: number) => (
    <Text key={index} style={styles.logText}>
      {item}
    </Text>
  );

  const getStatusColor = () => {
    if (!isOnline) return '#ff6b6b';
    if (syncState === 'syncing') return '#ffc93c';
    if (syncState === 'error') return '#ff6b6b';
    return '#51cf66';
  };

  return (
    <SafeAreaView style={styles.container}>
      <View style={styles.header}>
        <Text style={styles.title}>Obsyncian</Text>
        {config && (
          <TouchableOpacity onPress={() => setMenuVisible(true)} style={styles.menuButton}>
            <Text style={styles.menuText}>⋮</Text>
          </TouchableOpacity>
        )}
      </View>

      <Modal
        transparent
        visible={menuVisible}
        onRequestClose={() => setMenuVisible(false)}
        animationType="none"
      >
        <TouchableOpacity
          style={styles.menuOverlay}
          onPress={() => setMenuVisible(false)}
          activeOpacity={1}
        >
          <View style={styles.menuContent}>
            <TouchableOpacity
              style={styles.menuItem}
              onPress={handleChangeConfig}
            >
              <Text style={styles.menuItemText}>Change Config</Text>
            </TouchableOpacity>
            {vaultPath ? (
              <TouchableOpacity style={styles.menuItem} onPress={handleChangeVaultFolder}>
                <Text style={styles.menuItemText}>Change Vault Folder</Text>
              </TouchableOpacity>
            ) : null}
            <TouchableOpacity
              style={styles.menuItem}
              onPress={handleClearConfig}
            >
              <Text style={[styles.menuItemText, styles.menuItemDanger]}>Clear Config</Text>
            </TouchableOpacity>
          </View>
        </TouchableOpacity>
      </Modal>

      {!config ? (
        <View style={styles.noConfigContainer}>
          <Text style={styles.noConfigText}>No configuration loaded</Text>
          <TouchableOpacity style={styles.primaryButton} onPress={handlePickConfig}>
            <Text style={styles.buttonText}>Select config.json</Text>
          </TouchableOpacity>
        </View>
      ) : !vaultPath ? (
        <View style={styles.noConfigContainer}>
          <Text style={styles.noConfigText}>Vault folder not set</Text>
          <TouchableOpacity style={styles.primaryButton} onPress={handlePickVaultPath}>
            <Text style={styles.buttonText}>Choose Vault Folder</Text>
          </TouchableOpacity>
        </View>
      ) : (
        <ScrollView
          style={styles.content}
          contentContainerStyle={{
            paddingBottom: Math.max(insets.bottom, 12) + 20,
          }}
          keyboardShouldPersistTaps="handled"
        >
          {/* Status Card */}
          <View style={styles.statusCard}>
            <View style={styles.statusRow}>
              <Text style={styles.statusLabel}>Status:</Text>
              <View style={[styles.statusBadge, { backgroundColor: getStatusColor() }]}>
                <Text style={styles.statusValue}>
                  {!isOnline ? 'Offline' : syncState === 'syncing' ? 'Syncing' : syncState}
                </Text>
              </View>
            </View>
            {syncState === 'syncing' && <ActivityIndicator size="small" color="#51cf66" />}
          </View>

          {/* Config Card */}
          <View style={styles.configCard}>
            <Text style={styles.cardTitle}>Configuration</Text>
            <View style={styles.configRow}>
              <Text style={styles.configLabel}>ID:</Text>
              <Text style={styles.configValue}>{config.id}</Text>
            </View>
            <View style={styles.configRow}>
              <Text style={styles.configLabel}>Bucket:</Text>
              <Text style={styles.configValue}>{config.cloud}</Text>
            </View>
            <View style={styles.configRow}>
              <Text style={styles.configLabel}>Region:</Text>
              <Text style={styles.configValue}>{config.region}</Text>
            </View>
            <View style={styles.configRow}>
              <Text style={styles.configLabel}>Vault:</Text>
              <Text style={styles.configValue} numberOfLines={1}>
                {vaultPath}
              </Text>
            </View>
          </View>

          {/* Sync Button */}
          <TouchableOpacity
            style={[styles.syncButton, syncState === 'syncing' && styles.syncButtonDisabled]}
            onPress={triggerSync}
            disabled={syncState === 'syncing'}
          >
            <Text style={styles.syncButtonText}>
              {syncState === 'syncing' ? 'Syncing...' : 'Manual Sync'}
            </Text>
          </TouchableOpacity>

          {/* Logs — nested ScrollView so log lines scroll independently of the main screen */}
          <View style={styles.logsContainer}>
            <Text style={styles.logsTitle}>Sync Log</Text>
            <ScrollView
              style={styles.logsList}
              contentContainerStyle={styles.logsListInner}
              nestedScrollEnabled
              showsVerticalScrollIndicator
            >
              {logs.length === 0 ? (
                <Text style={styles.noLogsText}>No sync activity yet</Text>
              ) : (
                logs.map((log, index) => renderLogItem(log, index))
              )}
            </ScrollView>
            {logs.length > 0 && (
              <TouchableOpacity style={styles.clearButton} onPress={clearLogs}>
                <Text style={styles.clearButtonText}>Clear Log</Text>
              </TouchableOpacity>
            )}
          </View>
        </ScrollView>
      )}
    </SafeAreaView>
  );
};

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: '#f5f5f5',
  },
  header: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    paddingHorizontal: 16,
    paddingVertical: 12,
    backgroundColor: '#fff',
    borderBottomWidth: 1,
    borderBottomColor: '#e0e0e0',
  },
  title: {
    fontSize: 24,
    fontWeight: 'bold',
    color: '#333',
  },
  menuButton: {
    padding: 8,
  },
  menuText: {
    fontSize: 24,
    color: '#666',
  },
  menuOverlay: {
    flex: 1,
    backgroundColor: 'rgba(0, 0, 0, 0.5)',
    justifyContent: 'flex-start',
    alignItems: 'flex-end',
  },
  menuContent: {
    backgroundColor: '#fff',
    borderRadius: 8,
    margin: 16,
    minWidth: 150,
    shadowColor: '#000',
    shadowOffset: { width: 0, height: 2 },
    shadowOpacity: 0.25,
    shadowRadius: 3.84,
    elevation: 5,
  },
  menuItem: {
    paddingHorizontal: 16,
    paddingVertical: 12,
    borderBottomWidth: 1,
    borderBottomColor: '#f0f0f0',
  },
  menuItemText: {
    fontSize: 14,
    color: '#333',
  },
  menuItemDanger: {
    color: '#ff6b6b',
  },
  noConfigContainer: {
    flex: 1,
    justifyContent: 'center',
    alignItems: 'center',
    paddingHorizontal: 16,
  },
  noConfigText: {
    fontSize: 16,
    color: '#666',
    marginBottom: 24,
    textAlign: 'center',
  },
  primaryButton: {
    backgroundColor: '#007AFF',
    paddingVertical: 12,
    paddingHorizontal: 32,
    borderRadius: 8,
  },
  buttonText: {
    color: '#fff',
    fontSize: 16,
    fontWeight: '600',
  },
  content: {
    flex: 1,
    padding: 16,
  },
  statusCard: {
    backgroundColor: '#fff',
    borderRadius: 8,
    padding: 16,
    marginBottom: 12,
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
  },
  statusRow: {
    flexDirection: 'row',
    alignItems: 'center',
  },
  statusLabel: {
    fontSize: 14,
    fontWeight: '600',
    color: '#333',
    marginRight: 8,
  },
  statusBadge: {
    paddingVertical: 4,
    paddingHorizontal: 12,
    borderRadius: 12,
  },
  statusValue: {
    color: '#fff',
    fontSize: 12,
    fontWeight: '600',
  },
  configCard: {
    backgroundColor: '#fff',
    borderRadius: 8,
    padding: 16,
    marginBottom: 12,
  },
  cardTitle: {
    fontSize: 14,
    fontWeight: '600',
    color: '#333',
    marginBottom: 12,
  },
  configRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    marginBottom: 8,
  },
  configLabel: {
    fontSize: 12,
    color: '#666',
    fontWeight: '600',
  },
  configValue: {
    fontSize: 12,
    color: '#333',
    flex: 1,
    textAlign: 'right',
    marginLeft: 8,
  },
  syncButton: {
    backgroundColor: '#51cf66',
    paddingVertical: 12,
    borderRadius: 8,
    marginBottom: 12,
    alignItems: 'center',
  },
  syncButtonDisabled: {
    opacity: 0.6,
  },
  syncButtonText: {
    color: '#fff',
    fontSize: 16,
    fontWeight: '600',
  },
  logsContainer: {
    backgroundColor: '#fff',
    borderRadius: 8,
    padding: 12,
    marginBottom: 8,
  },
  logsTitle: {
    fontSize: 14,
    fontWeight: '600',
    color: '#333',
    marginBottom: 8,
  },
  logsList: {
    maxHeight: LOGS_SCROLL_MAX_HEIGHT,
    backgroundColor: '#f9f9f9',
    borderRadius: 4,
  },
  logsListInner: {
    padding: 8,
  },
  logText: {
    fontSize: 11,
    color: '#666',
    fontFamily: 'Courier New',
    marginBottom: 4,
  },
  noLogsText: {
    fontSize: 12,
    color: '#999',
    fontStyle: 'italic',
  },
  clearButton: {
    marginTop: 8,
    paddingVertical: 8,
    alignItems: 'center',
  },
  clearButtonText: {
    fontSize: 12,
    color: '#007AFF',
    fontWeight: '600',
  },
});
