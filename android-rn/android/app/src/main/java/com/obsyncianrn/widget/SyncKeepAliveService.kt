package com.obsyncianrn.widget

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.Service
import android.content.Intent
import android.os.Build
import android.os.IBinder
import com.obsyncianrn.R

class SyncKeepAliveService : Service() {

  companion object {
    const val CHANNEL_ID = "obsyncian_sync"
    const val NOTIFICATION_ID = 1001
  }

  override fun onBind(intent: Intent?): IBinder? = null

  override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
    createNotificationChannel()
    startForeground(NOTIFICATION_ID, buildNotification())
    return START_NOT_STICKY
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
