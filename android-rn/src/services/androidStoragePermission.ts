import { NativeModules, PermissionsAndroid, Platform } from 'react-native';

type ObsyncianStorageNative = {
  canManageExternalStorage: () => Promise<boolean>;
  openManageAllFilesAccess: () => Promise<void>;
};

const native = NativeModules.ObsyncianStorage as ObsyncianStorageNative | undefined;

/** True for volumes like /storage/ABCD-1234/... (not primary /storage/emulated/0). */
export function isRemovableExternalVaultPath(vaultPath: string): boolean {
  if (Platform.OS !== 'android') return false;
  if (!vaultPath.startsWith('/storage/')) return false;
  if (vaultPath.startsWith('/storage/emulated/')) return false;
  return true;
}

/**
 * Android: request legacy READ/WRITE where still applicable, and for removable SD paths on API 30+
 * open the "All files access" screen if the app does not already have broad file access.
 */
export async function ensureAndroidVaultStorageAccess(
  vaultPath: string,
  onLog?: (msg: string) => void,
): Promise<void> {
  if (Platform.OS !== 'android' || !vaultPath) return;

  const api = Platform.Version as number;

  if (api <= 32) {
    try {
      const read = PermissionsAndroid.PERMISSIONS.READ_EXTERNAL_STORAGE;
      const granted = await PermissionsAndroid.check(read);
      if (!granted) {
        onLog?.('[Permissions] Requesting read access to storage');
        const result = await PermissionsAndroid.request(read);
        onLog?.(`[Permissions] READ_EXTERNAL_STORAGE: ${result}`);
      }
    } catch (e) {
      onLog?.(`[Permissions] READ_EXTERNAL_STORAGE error: ${e}`);
    }
  }

  if (api <= 29) {
    try {
      const write = PermissionsAndroid.PERMISSIONS.WRITE_EXTERNAL_STORAGE;
      const granted = await PermissionsAndroid.check(write);
      if (!granted) {
        onLog?.('[Permissions] Requesting write access to storage');
        const result = await PermissionsAndroid.request(write);
        onLog?.(`[Permissions] WRITE_EXTERNAL_STORAGE: ${result}`);
      }
    } catch (e) {
      onLog?.(`[Permissions] WRITE_EXTERNAL_STORAGE error: ${e}`);
    }
  }

  if (!isRemovableExternalVaultPath(vaultPath)) return;
  if (api < 30 || !native?.canManageExternalStorage || !native.openManageAllFilesAccess) return;

  try {
    const ok = await native.canManageExternalStorage();
    if (ok) return;
    onLog?.(
      '[Permissions] Vault is on removable storage. Open system settings and enable “All files access” for Obsyncian, then run sync again.',
    );
    await native.openManageAllFilesAccess();
  } catch (e) {
    onLog?.(`[Permissions] All-files-access flow error: ${e}`);
  }
}
