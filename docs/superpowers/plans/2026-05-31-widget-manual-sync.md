# Widget Manual Sync Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove unreliable background sync infrastructure and add an Android home screen widget with a manual sync button that triggers sync via HeadlessJS.

**Architecture:** Native Kotlin widget (`AppWidgetProvider`) handles button taps, starts a short-lived foreground service, which launches a HeadlessJS task. The JS task reuses the existing `syncOrchestrator` to perform a single sync cycle, writes results to SharedPreferences via a native module, and the widget updates its display.

**Tech Stack:** React Native 0.84, Kotlin (Android native widget/service), HeadlessJS, SharedPreferences, existing AWS SDK sync infrastructure.

---

## File Structure

### Files to delete:
- `src/services/backgroundService.ts`
- `src/services/sqsListenerService.ts`

### Files to create:
- `src/services/syncHeadlessTask.ts` — HeadlessJS task that runs one sync cycle
- `android/app/src/main/java/com/obsyncianrn/widget/ObsyncianWidget.kt` — AppWidgetProvider
- `android/app/src/main/java/com/obsyncianrn/widget/SyncForegroundService.kt` — short-lived foreground service
- `android/app/src/main/java/com/obsyncianrn/widget/ObsyncianWidgetModule.kt` — native module for SharedPreferences bridge
- `android/app/src/main/java/com/obsyncianrn/widget/ObsyncianWidgetPackage.kt` — ReactPackage for the module
- `android/app/src/main/res/layout/obsyncian_widget.xml` — widget layout
- `android/app/src/main/res/xml/obsyncian_widget_info.xml` — widget metadata
- `android/app/src/main/res/drawable/widget_background.xml` — dark rounded background
- `android/app/src/main/res/drawable/sync_button_background.xml` — sync button circle

### Files to modify:
- `index.js` — register HeadlessJS task
- `App.tsx` — remove background service lifecycle
- `src/store/appStore.ts` — remove background service references
- `android/app/src/main/java/com/obsyncianrn/MainApplication.kt` — add widget package, register HeadlessJS
- `android/app/src/main/AndroidManifest.xml` — update permissions, add widget/service declarations
- `package.json` — remove unused dependencies

---

## Task 1: Remove background sync dependencies from JS

**Files:**
- Modify: `android-rn/src/store/appStore.ts`
- Modify: `android-rn/App.tsx`
- Delete: `android-rn/src/services/backgroundService.ts`
- Delete: `android-rn/src/services/sqsListenerService.ts`

- [ ] **Step 1: Remove backgroundService and sqsListenerService imports and usage from appStore.ts**

Replace the full contents of `src/store/appStore.ts` with:

```typescript
import { create } from 'zustand';
import { ObsyncianConfig } from '../models/config';
import { syncOrchestrator, SyncState } from '../services/syncOrchestrator';
import { connectivityService } from '../services/connectivityService';
import { configService } from '../services/configService';

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

  setSyncState: (state) => set({ syncState: state }),

  setIsOnline: (isOnline) => {
    set({ isOnline });
    syncOrchestrator.setIsOnline(isOnline);
  },

  addLog: (msg) => {
    set((state) => {
      const newLogs = [...state.logs, msg];
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

export async function initializeAppStore(): Promise<void> {
  const store = useAppStore.getState();

  syncOrchestrator.setOnStateChange((state) => {
    store.setSyncState(state);
  });

  syncOrchestrator.setOnLog((msg) => {
    store.addLog(msg);
  });

  // Connectivity service
  await connectivityService.checkInitialState();
  store.setIsOnline(connectivityService.getIsOnline());

  connectivityService.onReconnect(() => {
    store.addLog('[App] Reconnected to internet, triggering sync');
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
```

- [ ] **Step 2: Simplify App.tsx to remove background service lifecycle**

Replace the full contents of `App.tsx` with:

```typescript
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
```

- [ ] **Step 3: Delete the background service files**

```bash
cd android-rn
rm src/services/backgroundService.ts
rm src/services/sqsListenerService.ts
```

- [ ] **Step 4: Remove unused dependencies from package.json**

Remove these from `dependencies` in `package.json`:
- `react-native-background-actions`
- `react-native-background-timer`
- `@aws-sdk/client-sqs`
- `@aws-sdk/client-sns`

Remove from `devDependencies`:
- `@types/react-native-background-timer`

Then run:

```bash
cd android-rn && npm install
```

- [ ] **Step 5: Verify the app still builds**

```bash
cd android-rn && npx react-native run-android
```

Verify the app launches, shows the HomeScreen, and the Manual Sync button works when config is loaded.

- [ ] **Step 6: Commit**

```bash
cd android-rn
git add -A
git commit -m "feat: remove background sync infrastructure

Remove backgroundService, sqsListenerService, and related dependencies.
The app now only syncs on manual trigger or app foreground."
```

---

## Task 2: Create the native widget module (SharedPreferences bridge)

**Files:**
- Create: `android-rn/android/app/src/main/java/com/obsyncianrn/widget/ObsyncianWidgetModule.kt`
- Create: `android-rn/android/app/src/main/java/com/obsyncianrn/widget/ObsyncianWidgetPackage.kt`
- Modify: `android-rn/android/app/src/main/java/com/obsyncianrn/MainApplication.kt`

- [ ] **Step 1: Create the widget native module**

Create `android/app/src/main/java/com/obsyncianrn/widget/ObsyncianWidgetModule.kt`:

```kotlin
package com.obsyncianrn.widget

import android.appwidget.AppWidgetManager
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import com.facebook.react.bridge.Promise
import com.facebook.react.bridge.ReactApplicationContext
import com.facebook.react.bridge.ReactContextBaseJavaModule
import com.facebook.react.bridge.ReactMethod
import com.facebook.react.bridge.WritableNativeMap

class ObsyncianWidgetModule(reactContext: ReactApplicationContext) :
  ReactContextBaseJavaModule(reactContext) {

  override fun getName(): String = "ObsyncianWidget"

  companion object {
    const val PREFS_NAME = "obsyncian_widget"
    const val KEY_STATUS = "status"
    const val KEY_LAST_SYNC_TIME = "last_sync_time"

    fun getWidgetState(context: Context): Pair<String, String> {
      val prefs = context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
      val status = prefs.getString(KEY_STATUS, "not_synced") ?: "not_synced"
      val lastSyncTime = prefs.getString(KEY_LAST_SYNC_TIME, "") ?: ""
      return Pair(status, lastSyncTime)
    }

    fun setWidgetState(context: Context, status: String, lastSyncTime: String) {
      val prefs = context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
      prefs.edit()
        .putString(KEY_STATUS, status)
        .putString(KEY_LAST_SYNC_TIME, lastSyncTime)
        .apply()

      val intent = Intent(context, ObsyncianWidget::class.java).apply {
        action = AppWidgetManager.ACTION_APPWIDGET_UPDATE
        val widgetManager = AppWidgetManager.getInstance(context)
        val ids = widgetManager.getAppWidgetIds(ComponentName(context, ObsyncianWidget::class.java))
        putExtra(AppWidgetManager.EXTRA_APPWIDGET_IDS, ids)
      }
      context.sendBroadcast(intent)
    }
  }

  @ReactMethod
  fun setWidgetState(status: String, lastSyncTime: String, promise: Promise) {
    try {
      Companion.setWidgetState(reactApplicationContext, status, lastSyncTime)
      promise.resolve(null)
    } catch (e: Exception) {
      promise.reject("WIDGET_STATE_ERROR", e.message, e)
    }
  }

  @ReactMethod
  fun getWidgetState(promise: Promise) {
    try {
      val (status, lastSyncTime) = Companion.getWidgetState(reactApplicationContext)
      val map = WritableNativeMap()
      map.putString("status", status)
      map.putString("lastSyncTime", lastSyncTime)
      promise.resolve(map)
    } catch (e: Exception) {
      promise.reject("WIDGET_STATE_ERROR", e.message, e)
    }
  }
}
```

- [ ] **Step 2: Create the ReactPackage for the widget module**

Create `android/app/src/main/java/com/obsyncianrn/widget/ObsyncianWidgetPackage.kt`:

```kotlin
package com.obsyncianrn.widget

import com.facebook.react.ReactPackage
import com.facebook.react.bridge.NativeModule
import com.facebook.react.bridge.ReactApplicationContext
import com.facebook.react.uimanager.ViewManager

class ObsyncianWidgetPackage : ReactPackage {
  override fun createNativeModules(reactContext: ReactApplicationContext): List<NativeModule> =
    listOf(ObsyncianWidgetModule(reactContext))

  override fun createViewManagers(reactContext: ReactApplicationContext): List<ViewManager<*, *>> =
    emptyList()
}
```

- [ ] **Step 3: Register the package in MainApplication.kt**

Replace the contents of `android/app/src/main/java/com/obsyncianrn/MainApplication.kt` with:

```kotlin
package com.obsyncianrn

import android.app.Application
import com.facebook.react.PackageList
import com.facebook.react.ReactApplication
import com.facebook.react.ReactHost
import com.facebook.react.ReactNativeApplicationEntryPoint.loadReactNative
import com.facebook.react.defaults.DefaultReactHost.getDefaultReactHost
import com.obsyncianrn.widget.ObsyncianWidgetPackage

class MainApplication : Application(), ReactApplication {

  override val reactHost: ReactHost by lazy {
    getDefaultReactHost(
      context = applicationContext,
      packageList =
        PackageList(this).packages.apply {
          add(ObsyncianStoragePackage())
          add(ObsyncianWidgetPackage())
        },
    )
  }

  override fun onCreate() {
    super.onCreate()
    loadReactNative(this)
  }
}
```

- [ ] **Step 4: Commit**

```bash
cd android-rn
git add -A
git commit -m "feat: add native widget module for SharedPreferences bridge"
```

---

## Task 3: Create the HeadlessJS sync task

**Files:**
- Create: `android-rn/src/services/syncHeadlessTask.ts`
- Modify: `android-rn/index.js`

- [ ] **Step 1: Create the headless task**

Create `src/services/syncHeadlessTask.ts`:

```typescript
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
```

- [ ] **Step 2: Register the headless task in index.js**

Replace the contents of `index.js` with:

```javascript
/**
 * @format
 */

// Hermes does not provide crypto, ReadableStream, or URL — all needed by AWS SDK v3.
import { install } from 'react-native-quick-crypto';
install();

import { ReadableStream } from 'web-streams-polyfill';
globalThis.ReadableStream = globalThis.ReadableStream || ReadableStream;

import 'react-native-url-polyfill/auto';

if (typeof globalThis.structuredClone === 'undefined') {
  globalThis.structuredClone = (obj) => JSON.parse(JSON.stringify(obj));
}

import { AppRegistry } from 'react-native';
import App from './App';
import { name as appName } from './app.json';

AppRegistry.registerComponent(appName, () => App);

AppRegistry.registerHeadlessTask('SyncTask', () => require('./src/services/syncHeadlessTask').default);
```

- [ ] **Step 3: Commit**

```bash
cd android-rn
git add -A
git commit -m "feat: add HeadlessJS sync task for widget-triggered sync"
```

---

## Task 4: Create the widget layout and drawable resources

**Files:**
- Create: `android-rn/android/app/src/main/res/drawable/widget_background.xml`
- Create: `android-rn/android/app/src/main/res/drawable/sync_button_background.xml`
- Create: `android-rn/android/app/src/main/res/layout/obsyncian_widget.xml`
- Create: `android-rn/android/app/src/main/res/xml/obsyncian_widget_info.xml`

- [ ] **Step 1: Create widget background drawable**

Create `android/app/src/main/res/drawable/widget_background.xml`:

```xml
<?xml version="1.0" encoding="utf-8"?>
<shape xmlns:android="http://schemas.android.com/apk/res/android"
    android:shape="rectangle">
    <solid android:color="#FF2D2D2D" />
    <corners android:radius="16dp" />
</shape>
```

- [ ] **Step 2: Create sync button background drawable**

Create `android/app/src/main/res/drawable/sync_button_background.xml`:

```xml
<?xml version="1.0" encoding="utf-8"?>
<shape xmlns:android="http://schemas.android.com/apk/res/android"
    android:shape="oval">
    <solid android:color="#FF3D3D3D" />
</shape>
```

- [ ] **Step 3: Create the widget layout**

Create `android/app/src/main/res/layout/obsyncian_widget.xml`:

```xml
<?xml version="1.0" encoding="utf-8"?>
<LinearLayout xmlns:android="http://schemas.android.com/apk/res/android"
    android:layout_width="match_parent"
    android:layout_height="match_parent"
    android:background="@drawable/widget_background"
    android:gravity="center_vertical"
    android:orientation="horizontal"
    android:padding="8dp">

    <ImageButton
        android:id="@+id/sync_button"
        android:layout_width="40dp"
        android:layout_height="40dp"
        android:background="@drawable/sync_button_background"
        android:contentDescription="Sync"
        android:src="@android:drawable/ic_popup_sync"
        android:scaleType="centerInside"
        android:padding="8dp" />

    <LinearLayout
        android:layout_width="0dp"
        android:layout_height="wrap_content"
        android:layout_weight="1"
        android:layout_marginStart="8dp"
        android:gravity="center_vertical"
        android:orientation="horizontal">

        <View
            android:id="@+id/status_dot"
            android:layout_width="8dp"
            android:layout_height="8dp"
            android:background="@android:color/holo_green_light" />

        <TextView
            android:id="@+id/status_text"
            android:layout_width="wrap_content"
            android:layout_height="wrap_content"
            android:layout_marginStart="6dp"
            android:text="not synced"
            android:textColor="#FFFFFFFF"
            android:textSize="12sp"
            android:singleLine="true" />

    </LinearLayout>

</LinearLayout>
```

- [ ] **Step 4: Create widget info metadata**

Create `android/app/src/main/res/xml/obsyncian_widget_info.xml`:

```xml
<?xml version="1.0" encoding="utf-8"?>
<appwidget-provider xmlns:android="http://schemas.android.com/apk/res/android"
    android:minWidth="110dp"
    android:minHeight="40dp"
    android:updatePeriodMillis="0"
    android:initialLayout="@layout/obsyncian_widget"
    android:resizeMode="none"
    android:widgetCategory="home_screen"
    android:description="@string/widget_description" />
```

- [ ] **Step 5: Add widget description string**

Add to `android/app/src/main/res/values/strings.xml`:

```xml
<resources>
    <string name="app_name">ObsyncianRN</string>
    <string name="widget_description">Sync your Obsidian vault</string>
    <string name="sync_notification_channel">Sync</string>
</resources>
```

- [ ] **Step 6: Commit**

```bash
cd android-rn
git add -A
git commit -m "feat: add widget layout and drawable resources"
```

---

## Task 5: Create the AppWidgetProvider and foreground service

**Files:**
- Create: `android-rn/android/app/src/main/java/com/obsyncianrn/widget/ObsyncianWidget.kt`
- Create: `android-rn/android/app/src/main/java/com/obsyncianrn/widget/SyncForegroundService.kt`

- [ ] **Step 1: Create the AppWidgetProvider**

Create `android/app/src/main/java/com/obsyncianrn/widget/ObsyncianWidget.kt`:

```kotlin
package com.obsyncianrn.widget

import android.app.PendingIntent
import android.appwidget.AppWidgetManager
import android.appwidget.AppWidgetProvider
import android.content.Context
import android.content.Intent
import android.graphics.Color
import android.graphics.drawable.GradientDrawable
import android.os.Build
import android.widget.RemoteViews
import com.obsyncianrn.R

class ObsyncianWidget : AppWidgetProvider() {

  companion object {
    const val ACTION_SYNC = "com.obsyncianrn.SYNC"
  }

  override fun onUpdate(context: Context, appWidgetManager: AppWidgetManager, appWidgetIds: IntArray) {
    for (appWidgetId in appWidgetIds) {
      updateWidget(context, appWidgetManager, appWidgetId)
    }
  }

  override fun onReceive(context: Context, intent: Intent) {
    super.onReceive(context, intent)

    if (intent.action == ACTION_SYNC) {
      val serviceIntent = Intent(context, SyncForegroundService::class.java)
      if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
        context.startForegroundService(serviceIntent)
      } else {
        context.startService(serviceIntent)
      }
    }
  }

  private fun updateWidget(context: Context, appWidgetManager: AppWidgetManager, appWidgetId: Int) {
    val views = RemoteViews(context.packageName, R.layout.obsyncian_widget)

    // Set up sync button click
    val syncIntent = Intent(context, ObsyncianWidget::class.java).apply {
      action = ACTION_SYNC
    }
    val pendingIntent = PendingIntent.getBroadcast(
      context,
      0,
      syncIntent,
      PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
    )
    views.setOnClickPendingIntent(R.id.sync_button, pendingIntent)

    // Load state from SharedPreferences
    val (status, lastSyncTime) = ObsyncianWidgetModule.getWidgetState(context)

    // Status dot color
    val dotColor = when (status) {
      "idle" -> Color.parseColor("#51cf66")
      "syncing" -> Color.parseColor("#ffc93c")
      "error" -> Color.parseColor("#ff6b6b")
      "offline" -> Color.parseColor("#ff6b6b")
      else -> Color.parseColor("#999999")
    }
    views.setInt(R.id.status_dot, "setBackgroundColor", dotColor)

    // Status text
    val statusText = when (status) {
      "syncing" -> "syncing..."
      "offline" -> "offline"
      "not_synced" -> "not synced"
      else -> if (lastSyncTime.isNotEmpty()) "$status • $lastSyncTime" else status
    }
    views.setTextViewText(R.id.status_text, statusText)

    appWidgetManager.updateAppWidget(appWidgetId, views)
  }
}
```

- [ ] **Step 2: Create the foreground service**

Create `android/app/src/main/java/com/obsyncianrn/widget/SyncForegroundService.kt`:

```kotlin
package com.obsyncianrn.widget

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.Service
import android.content.Intent
import android.os.Build
import android.os.IBinder
import com.facebook.react.HeadlessJsTaskService
import com.facebook.react.bridge.Arguments
import com.facebook.react.jstasks.HeadlessJsTaskConfig
import com.obsyncianrn.R

class SyncForegroundService : HeadlessJsTaskService() {

  companion object {
    const val CHANNEL_ID = "obsyncian_sync"
    const val NOTIFICATION_ID = 1001
  }

  override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
    createNotificationChannel()

    val notification = buildNotification()
    startForeground(NOTIFICATION_ID, notification)

    return super.onStartCommand(intent, flags, startId)
  }

  override fun getTaskConfig(intent: Intent?): HeadlessJsTaskConfig {
    return HeadlessJsTaskConfig(
      "SyncTask",
      Arguments.createMap(),
      60000,
      true
    )
  }

  override fun onHeadlessJsTaskFinish(taskId: Int) {
    super.onHeadlessJsTaskFinish(taskId)
    stopSelf()
  }

  private fun createNotificationChannel() {
    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
      val channel = NotificationChannel(
        CHANNEL_ID,
        "Sync",
        NotificationManager.IMPORTANCE_LOW
      ).apply {
        description = "Shows when vault is syncing"
      }
      val manager = getSystemService(NotificationManager::class.java)
      manager.createNotificationChannel(channel)
    }
  }

  private fun buildNotification(): Notification {
    val builder = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
      Notification.Builder(this, CHANNEL_ID)
    } else {
      @Suppress("DEPRECATION")
      Notification.Builder(this)
    }

    return builder
      .setContentTitle("Obsyncian")
      .setContentText("Syncing your vault...")
      .setSmallIcon(R.mipmap.ic_launcher)
      .build()
  }
}
```

- [ ] **Step 3: Commit**

```bash
cd android-rn
git add -A
git commit -m "feat: add widget provider and sync foreground service"
```

---

## Task 6: Update AndroidManifest and wire everything together

**Files:**
- Modify: `android-rn/android/app/src/main/AndroidManifest.xml`

- [ ] **Step 1: Replace AndroidManifest.xml**

Replace the full contents of `android/app/src/main/AndroidManifest.xml` with:

```xml
<manifest xmlns:android="http://schemas.android.com/apk/res/android"
    xmlns:tools="http://schemas.android.com/tools">

    <uses-permission android:name="android.permission.INTERNET" />
    <uses-permission android:name="android.permission.FOREGROUND_SERVICE" />
    <uses-permission android:name="android.permission.FOREGROUND_SERVICE_DATA_SYNC" />
    <uses-permission android:name="android.permission.POST_NOTIFICATIONS" />
    <!-- Legacy storage (API ≤32); helps some primary-storage paths -->
    <uses-permission android:name="android.permission.READ_EXTERNAL_STORAGE" android:maxSdkVersion="32" />
    <uses-permission
        android:name="android.permission.WRITE_EXTERNAL_STORAGE"
        android:maxSdkVersion="29"
        tools:ignore="ScopedStorage" />
    <!-- Required to read/write arbitrary paths on removable SD with react-native-fs -->
    <uses-permission
        android:name="android.permission.MANAGE_EXTERNAL_STORAGE"
        tools:ignore="ScopedStorage" />

    <application
      android:name=".MainApplication"
      android:label="@string/app_name"
      android:icon="@mipmap/ic_launcher"
      android:roundIcon="@mipmap/ic_launcher_round"
      android:allowBackup="false"
      android:theme="@style/AppTheme"
      android:usesCleartextTraffic="${usesCleartextTraffic}"
      android:supportsRtl="true">
      <activity
        android:name=".MainActivity"
        android:label="@string/app_name"
        android:configChanges="keyboard|keyboardHidden|orientation|screenLayout|screenSize|smallestScreenSize|uiMode"
        android:launchMode="singleTask"
        android:windowSoftInputMode="adjustResize"
        android:exported="true">
        <intent-filter>
            <action android:name="android.intent.action.MAIN" />
            <category android:name="android.intent.category.LAUNCHER" />
        </intent-filter>
      </activity>

      <!-- Widget -->
      <receiver
        android:name=".widget.ObsyncianWidget"
        android:exported="true">
        <intent-filter>
          <action android:name="android.appwidget.action.APPWIDGET_UPDATE" />
          <action android:name="com.obsyncianrn.SYNC" />
        </intent-filter>
        <meta-data
          android:name="android.appwidget.provider"
          android:resource="@xml/obsyncian_widget_info" />
      </receiver>

      <!-- Sync foreground service (HeadlessJS) -->
      <service
        android:name=".widget.SyncForegroundService"
        android:foregroundServiceType="dataSync"
        android:exported="false" />

    </application>
</manifest>
```

- [ ] **Step 2: Commit**

```bash
cd android-rn
git add -A
git commit -m "feat: update AndroidManifest with widget and service declarations"
```

---

## Task 7: Remove battery optimization native code

**Files:**
- Modify: `android-rn/android/app/src/main/java/com/obsyncianrn/ObsyncianStorageModule.kt`

- [ ] **Step 1: Remove battery optimization methods from ObsyncianStorageModule**

Replace the full contents of `android/app/src/main/java/com/obsyncianrn/ObsyncianStorageModule.kt` with:

```kotlin
package com.obsyncianrn

import android.content.Intent
import android.net.Uri
import android.os.Build
import android.os.Environment
import android.provider.Settings
import com.facebook.react.bridge.Promise
import com.facebook.react.bridge.ReactApplicationContext
import com.facebook.react.bridge.ReactContextBaseJavaModule
import com.facebook.react.bridge.ReactMethod

class ObsyncianStorageModule(reactContext: ReactApplicationContext) :
  ReactContextBaseJavaModule(reactContext) {

  override fun getName(): String = "ObsyncianStorage"

  @ReactMethod
  fun canManageExternalStorage(promise: Promise) {
    if (Build.VERSION.SDK_INT < Build.VERSION_CODES.R) {
      promise.resolve(true)
      return
    }
    promise.resolve(Environment.isExternalStorageManager())
  }

  @ReactMethod
  fun openManageAllFilesAccess(promise: Promise) {
    try {
      val ctx = reactApplicationContext
      val intent =
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
          Intent(Settings.ACTION_MANAGE_APP_ALL_FILES_ACCESS_PERMISSION).apply {
            data = Uri.parse("package:${ctx.packageName}")
          }
        } else {
          Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS).apply {
            data = Uri.parse("package:${ctx.packageName}")
          }
        }
      intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
      ctx.startActivity(intent)
      promise.resolve(null)
    } catch (e: Exception) {
      promise.reject("OPEN_SETTINGS", e.message, e)
    }
  }
}
```

- [ ] **Step 2: Commit**

```bash
cd android-rn
git add -A
git commit -m "refactor: remove battery optimization code from storage module"
```

---

## Task 8: Build and verify

**Files:** None (verification only)

- [ ] **Step 1: Run a clean build**

```bash
cd android-rn/android && ./gradlew clean assembleDebug
```

Expected: BUILD SUCCESSFUL

- [ ] **Step 2: Install and test on device/emulator**

```bash
cd android-rn && npx react-native run-android
```

Verify:
1. App launches normally
2. Manual Sync button in the app works
3. No crash on startup

- [ ] **Step 3: Test the widget**

On the device/emulator:
1. Long-press home screen → Widgets
2. Find "ObsyncianRN" widget
3. Add the 2x1 widget to home screen
4. Verify it shows "not synced" with grey dot
5. Tap the sync button
6. Verify dot turns yellow (syncing), then green (idle) with a time
7. Verify the sync actually completed (check vault files)

- [ ] **Step 4: Test error states**

1. Turn off WiFi/data
2. Tap widget sync button
3. Verify dot turns red and shows "offline"
4. Turn WiFi back on
5. Tap sync again
6. Verify it syncs successfully

- [ ] **Step 5: Final commit (if any fixes needed)**

```bash
cd android-rn
git add -A
git commit -m "fix: address widget testing issues"
```
