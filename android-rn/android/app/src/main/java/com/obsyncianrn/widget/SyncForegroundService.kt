package com.obsyncianrn.widget

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.content.Intent
import android.os.Build
import android.util.Log
import com.facebook.react.HeadlessJsTaskService
import com.facebook.react.bridge.Arguments
import com.facebook.react.jstasks.HeadlessJsTaskConfig
import com.obsyncianrn.R

class SyncForegroundService : HeadlessJsTaskService() {

  companion object {
    const val TAG = "SyncForegroundService"
    const val CHANNEL_ID = "obsyncian_sync"
    const val NOTIFICATION_ID = 1001
  }

  override fun onCreate() {
    super.onCreate()
    Log.d(TAG, "onCreate")
    createNotificationChannel()
  }

  override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
    Log.d(TAG, "onStartCommand")
    val notification = buildNotification()
    startForeground(NOTIFICATION_ID, notification)
    return super.onStartCommand(intent, flags, startId)
  }

  override fun getTaskConfig(intent: Intent?): HeadlessJsTaskConfig? {
    Log.d(TAG, "getTaskConfig called")
    return HeadlessJsTaskConfig(
      "SyncTask",
      Arguments.createMap(),
      60000,
      true
    )
  }

  override fun onHeadlessJsTaskFinish(taskId: Int) {
    Log.d(TAG, "onHeadlessJsTaskFinish: $taskId")
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
