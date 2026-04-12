import AsyncStorage from '@react-native-async-storage/async-storage';

const CACHE_KEY = 'obsyncian_file_stat_cache';

export interface StatEntry {
  mtime: number; // milliseconds
  size: number; // bytes
  md5?: string; // cached MD5 hash (if available)
}

class FileStatCache {
  private cache: Map<string, StatEntry> = new Map();
  private loaded = false;

  async load(): Promise<void> {
    if (this.loaded) return;
    try {
      const data = await AsyncStorage.getItem(CACHE_KEY);
      if (data) {
        const obj = JSON.parse(data);
        this.cache = new Map(Object.entries(obj));
      }
    } catch (e) {
      console.log('FileStatCache.load error:', e);
      this.cache = new Map();
    }
    this.loaded = true;
  }

  async save(): Promise<void> {
    try {
      const obj = Object.fromEntries(this.cache);
      await AsyncStorage.setItem(CACHE_KEY, JSON.stringify(obj));
    } catch (e) {
      console.log('FileStatCache.save error:', e);
    }
  }

  get(relativePath: string): StatEntry | undefined {
    return this.cache.get(relativePath);
  }

  set(relativePath: string, entry: StatEntry): void {
    this.cache.set(relativePath, entry);
  }

  has(relativePath: string): boolean {
    return this.cache.has(relativePath);
  }

  clear(): void {
    this.cache.clear();
  }

  async clearStorage(): Promise<void> {
    this.cache.clear();
    try {
      await AsyncStorage.removeItem(CACHE_KEY);
    } catch (e) {
      console.log('FileStatCache.clearStorage error:', e);
    }
  }

}

export const fileStatCache = new FileStatCache();
