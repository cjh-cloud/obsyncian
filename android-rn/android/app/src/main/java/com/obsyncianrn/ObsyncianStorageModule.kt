package com.obsyncianrn

import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.os.Environment
import android.os.PowerManager
import android.provider.Settings
import com.facebook.react.bridge.Promise
import com.facebook.react.bridge.ReactApplicationContext
import com.facebook.react.bridge.ReactContextBaseJavaModule
import com.facebook.react.bridge.ReactMethod

/**
 * Native helpers for storage permissions and battery-optimization exemption.
 * Exposed to JS as `NativeModules.ObsyncianStorage`.
 */
class ObsyncianStorageModule(reactContext: ReactApplicationContext) :
  ReactContextBaseJavaModule(reactContext) {

  override fun getName(): String = "ObsyncianStorage"

  // ── Storage ──────────────────────────────────────────────────────────

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

  // ── Battery optimization ─────────────────────────────────────────────

  /** True if this app is already exempt from Doze / battery optimization. */
  @ReactMethod
  fun isIgnoringBatteryOptimizations(promise: Promise) {
    if (Build.VERSION.SDK_INT < Build.VERSION_CODES.M) {
      promise.resolve(true) // Doze doesn't exist before Android 6
      return
    }
    try {
      val pm = reactApplicationContext.getSystemService(Context.POWER_SERVICE) as PowerManager
      promise.resolve(pm.isIgnoringBatteryOptimizations(reactApplicationContext.packageName))
    } catch (e: Exception) {
      promise.reject("BATTERY_CHECK", e.message, e)
    }
  }

  /**
   * Open the system dialog asking the user to exempt this app from battery optimization.
   * Uses ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS which shows a direct yes/no prompt.
   */
  @ReactMethod
  fun requestIgnoreBatteryOptimizations(promise: Promise) {
    if (Build.VERSION.SDK_INT < Build.VERSION_CODES.M) {
      promise.resolve(null)
      return
    }
    try {
      val ctx = reactApplicationContext
      val intent = Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS).apply {
        data = Uri.parse("package:${ctx.packageName}")
        addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
      }
      ctx.startActivity(intent)
      promise.resolve(null)
    } catch (e: Exception) {
      promise.reject("BATTERY_REQUEST", e.message, e)
    }
  }
}
