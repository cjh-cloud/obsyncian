package com.obsyncianrn

import android.content.Intent
import android.os.Build
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import com.facebook.react.ReactActivity
import com.facebook.react.ReactActivityDelegate
import com.facebook.react.defaults.DefaultNewArchitectureEntryPoint.fabricEnabled
import com.facebook.react.defaults.DefaultReactActivityDelegate
import com.obsyncianrn.widget.SyncKeepAliveService

class MainActivity : ReactActivity() {

  private var shouldMoveToBack = false

  override fun getMainComponentName(): String = "ObsyncianRN"

  override fun createReactActivityDelegate(): ReactActivityDelegate =
      DefaultReactActivityDelegate(this, mainComponentName, fabricEnabled)

  override fun onCreate(savedInstanceState: Bundle?) {
    super.onCreate(savedInstanceState)
    handleWidgetIntent(intent)
  }

  override fun onNewIntent(intent: Intent) {
    super.onNewIntent(intent)
    handleWidgetIntent(intent)
  }

  override fun onResume() {
    super.onResume()
    if (shouldMoveToBack) {
      shouldMoveToBack = false
      // Start keep-alive immediately so the process survives backgrounding
      startKeepAliveService()
      // Give JS a moment to boot and start the sync, then move to background
      Handler(Looper.getMainLooper()).postDelayed({
        moveTaskToBack(true)
      }, 2000)
    }
  }

  private fun handleWidgetIntent(intent: Intent?) {
    if (intent?.getBooleanExtra("triggerSync", false) == true) {
      intent.removeExtra("triggerSync")
      shouldMoveToBack = true
    }
  }

  private fun startKeepAliveService() {
    val intent = Intent(this, SyncKeepAliveService::class.java)
    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
      startForegroundService(intent)
    } else {
      startService(intent)
    }
  }
}
