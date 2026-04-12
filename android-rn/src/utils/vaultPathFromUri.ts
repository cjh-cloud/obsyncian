import { Platform } from 'react-native';

const ANDROID_EXTERNAL_STORAGE_AUTHORITY = 'com.android.externalstorage.documents';

/**
 * Maps a directory picker URI to an absolute filesystem path for use with react-native-fs.
 * Supports file:// URIs and Android primary/SD volume SAF tree URIs. Cloud-only trees return null.
 */
export function vaultUriToFilesystemPath(uri: string): string | null {
  const trimmed = uri.trim();
  if (!trimmed) {
    return null;
  }

  if (trimmed.startsWith('file://')) {
    let path = trimmed.replace(/^file:\/\//, '');
    try {
      path = decodeURIComponent(path);
    } catch {
      // keep path as-is
    }
    return path;
  }

  if (Platform.OS !== 'android') {
    return null;
  }

  const marker = `${ANDROID_EXTERNAL_STORAGE_AUTHORITY}/tree/`;
  const treeIdx = trimmed.indexOf(marker);
  if (treeIdx === -1) {
    return null;
  }

  let encoded = trimmed.slice(treeIdx + marker.length);
  encoded = encoded.split('#')[0] ?? encoded;

  let decoded: string;
  try {
    decoded = decodeURIComponent(encoded);
  } catch {
    return null;
  }

  if (decoded.startsWith('raw:')) {
    const raw = decoded.slice('raw:'.length).replace(/^\/+/, '');
    return raw.startsWith('/') ? raw : `/${raw}`;
  }

  const colon = decoded.indexOf(':');
  if (colon === -1) {
    return null;
  }

  const volume = decoded.slice(0, colon);
  const relative = decoded.slice(colon + 1).replace(/^\/+/, '');

  if (volume === 'primary') {
    return `/storage/emulated/0/${relative}`;
  }

  return `/storage/${volume}/${relative}`;
}
