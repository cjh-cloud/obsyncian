import NetInfo from '@react-native-community/netinfo';

type OnReconnectCallback = () => void;

class ConnectivityService {
  private isOnline = true;
  private onReconnectCallbacks: OnReconnectCallback[] = [];
  private unsubscribe: (() => void) | null = null;

  async checkInitialState(): Promise<void> {
    const state = await NetInfo.fetch();
    this.isOnline = state.isConnected ?? false;
  }

  start(): void {
    this.unsubscribe = NetInfo.addEventListener((state) => {
      const wasOnline = this.isOnline;
      this.isOnline = state.isConnected ?? false;

      // Offline -> online transition
      if (!wasOnline && this.isOnline) {
        console.log('[ConnectivityService] Reconnected to internet');
        this.onReconnectCallbacks.forEach((cb) => {
          try {
            cb();
          } catch (e) {
            console.error('[ConnectivityService] onReconnect callback error:', e);
          }
        });
      }
    });
  }

  stop(): void {
    if (this.unsubscribe) {
      this.unsubscribe();
      this.unsubscribe = null;
    }
  }

  getIsOnline(): boolean {
    return this.isOnline;
  }

  onReconnect(callback: OnReconnectCallback): void {
    this.onReconnectCallbacks.push(callback);
  }

  offReconnect(callback: OnReconnectCallback): void {
    this.onReconnectCallbacks = this.onReconnectCallbacks.filter((cb) => cb !== callback);
  }
}

export const connectivityService = new ConnectivityService();
