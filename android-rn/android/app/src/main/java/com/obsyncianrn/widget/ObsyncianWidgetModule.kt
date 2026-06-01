package com.obsyncianrn.widget

import android.appwidget.AppWidgetManager
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.os.Build
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

  @ReactMethod
  fun startKeepAlive(promise: Promise) {
    try {
      val context = reactApplicationContext
      val intent = Intent(context, SyncKeepAliveService::class.java)
      if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
        context.startForegroundService(intent)
      } else {
        context.startService(intent)
      }
      promise.resolve(null)
    } catch (e: Exception) {
      promise.reject("KEEP_ALIVE_ERROR", e.message, e)
    }
  }

  @ReactMethod
  fun stopKeepAlive(promise: Promise) {
    try {
      val context = reactApplicationContext
      val intent = Intent(context, SyncKeepAliveService::class.java)
      context.stopService(intent)
      promise.resolve(null)
    } catch (e: Exception) {
      promise.reject("KEEP_ALIVE_ERROR", e.message, e)
    }
  }
}
