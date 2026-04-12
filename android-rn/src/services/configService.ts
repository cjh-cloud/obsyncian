import AsyncStorage from '@react-native-async-storage/async-storage';
import RNFS from 'react-native-fs';
import {
  pick,
  keepLocalCopy,
  pickDirectory,
  types,
  errorCodes,
  isErrorWithCode,
} from '@react-native-documents/picker';
import { ObsyncianConfig, parseConfig, isValidConfig } from '../models/config';
import { vaultUriToFilesystemPath } from '../utils/vaultPathFromUri';

const CONFIG_PATH_KEY = 'obsyncian_config_path';
const VAULT_PATH_KEY = 'obsyncian_vault_path';

/** Picked folder could not be mapped to a local path (e.g. Google Drive). */
export class VaultPathUnmappedError extends Error {
  constructor(public readonly pickedUri: string) {
    super(
      'That folder is not on-device storage we can sync from. Pick a folder under internal storage (for example Documents), not a cloud-only location.',
    );
    this.name = 'VaultPathUnmappedError';
  }
}

class ConfigService {
  async pickConfigFile(): Promise<{ path: string; config: ObsyncianConfig } | null> {
    try {
      const [picked] = await pick({
        type: [types.json],
      });

      if (!picked || picked.error || !picked.hasRequestedType) {
        return null;
      }

      const fileName = picked.name ?? 'config.json';
      const fileToCopy =
        picked.isVirtual && picked.convertibleToMimeTypes?.length
          ? {
              uri: picked.uri,
              fileName,
              convertVirtualFileToType:
                picked.convertibleToMimeTypes.find(m => m.mimeType === 'application/json')
                  ?.mimeType ?? picked.convertibleToMimeTypes[0].mimeType,
            }
          : { uri: picked.uri, fileName };

      const [copyResult] = await keepLocalCopy({
        files: [fileToCopy],
        destination: 'documentDirectory',
      });

      if (copyResult.status !== 'success') {
        return null;
      }

      const path = copyResult.localUri;
      const content = await RNFS.readFile(path, 'utf8');
      const config = parseConfig(content);

      if (!isValidConfig(config)) {
        throw new Error('Invalid config: missing required fields');
      }

      // Save the path
      await AsyncStorage.setItem(CONFIG_PATH_KEY, path);

      return { path, config };
    } catch (error) {
      if (isErrorWithCode(error) && error.code === errorCodes.OPERATION_CANCELED) {
        return null;
      }
      throw error;
    }
  }

  async loadSavedConfig(): Promise<{ path: string; config: ObsyncianConfig } | null> {
    try {
      const path = await AsyncStorage.getItem(CONFIG_PATH_KEY);
      if (!path) return null;

      // Check if file still exists
      const exists = await RNFS.exists(path);
      if (!exists) {
        await this.clearSavedConfig();
        return null;
      }

      const content = await RNFS.readFile(path, 'utf8');
      const config = parseConfig(content);

      if (!isValidConfig(config)) {
        await this.clearSavedConfig();
        return null;
      }

      return { path, config };
    } catch (error) {
      console.log('ConfigService.loadSavedConfig error:', error);
      await this.clearSavedConfig();
      return null;
    }
  }

  async saveVaultPath(path: string): Promise<void> {
    await AsyncStorage.setItem(VAULT_PATH_KEY, path);
  }

  async getVaultPath(): Promise<string | null> {
    return AsyncStorage.getItem(VAULT_PATH_KEY);
  }

  /**
   * System folder picker. Resolves to a filesystem path usable with react-native-fs.
   * @returns path, or null if the user cancelled.
   */
  async pickVaultFolder(): Promise<string | null> {
    try {
      const result = await pickDirectory({ requestLongTermAccess: true });
      const fsPath = vaultUriToFilesystemPath(result.uri);
      if (!fsPath) {
        throw new VaultPathUnmappedError(result.uri);
      }
      const exists = await RNFS.exists(fsPath);
      if (!exists) {
        throw new VaultPathUnmappedError(result.uri);
      }
      const stat = await RNFS.stat(fsPath);
      if (!stat.isDirectory()) {
        throw new Error('The selected item is not a folder');
      }
      await this.saveVaultPath(fsPath);
      return fsPath;
    } catch (error) {
      if (isErrorWithCode(error) && error.code === errorCodes.OPERATION_CANCELED) {
        return null;
      }
      throw error;
    }
  }

  async clearSavedConfig(): Promise<void> {
    await AsyncStorage.multiRemove([CONFIG_PATH_KEY, VAULT_PATH_KEY]);
  }
}

export const configService = new ConfigService();
