package com.chatcodex.chat_codex_app

import android.os.Build
import android.provider.Settings
import android.util.Log
import android.content.Intent
import android.net.Uri
import cn.jpush.android.api.JPushInterface
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel
import io.flutter.plugin.common.EventChannel
import io.flutter.plugin.common.EventChannel.EventSink

class MainActivity : FlutterActivity() {
    companion object {
        @Volatile
        private var appForeground = false

        fun isAppForeground(): Boolean = appForeground
    }

    private val channelName = "chat_codex/push_service"
    private val tag = "JPushMainActivity"
    private var launchPayload: Map<String, String> = notificationPayload(intent)
    private var notificationSink: EventSink? = null

    override fun onResume() {
        super.onResume()
        setAppForeground(true)
    }

    override fun onPause() {
        setAppForeground(false)
        super.onPause()
    }

    private fun setAppForeground(foreground: Boolean) {
        appForeground = foreground
        getSharedPreferences("FlutterSharedPreferences", MODE_PRIVATE)
            .edit()
            .putBoolean("chat_codex_app_foreground", foreground)
            .apply()
    }

    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)
        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, channelName).setMethodCallHandler { call, result ->
            when (call.method) {
                "initialize" -> {
                    JPushInterface.setDebugMode(true)
                    JPushInterface.init(applicationContext)
                    Log.i(tag, "initialize called")
                    result.success(null)
                }
                "getRegistrationId" -> {
                    val cachedRegistrationId = JPushBridge.getRegistrationId(this)
                    if (cachedRegistrationId.isNotBlank()) {
                        Log.i(tag, "getRegistrationId hit cache registrationId=$cachedRegistrationId")
                        result.success(cachedRegistrationId)
                    } else {
                        val registrationId = JPushInterface.getRegistrationID(applicationContext).orEmpty()
                        Log.i(tag, "getRegistrationId fetched from SDK registrationId=$registrationId")
                        if (registrationId.isNotBlank()) {
                            JPushBridge.saveRegistrationId(this, registrationId)
                        }
                        result.success(registrationId)
                    }
                }
                "getDeviceId" -> result.success(Settings.Secure.getString(contentResolver, Settings.Secure.ANDROID_ID) ?: "")
                "getDeviceBrand" -> result.success(Build.BRAND ?: "")
                "getDeviceModel" -> result.success(Build.MODEL ?: "")
                "getLaunchPayload" -> {
                    result.success(launchPayload)
                    launchPayload = emptyMap()
                }
                else -> result.notImplemented()
            }
        }
        MethodChannel(
            flutterEngine.dartExecutor.binaryMessenger,
            "chat_codex/global_overlay",
        ).setMethodCallHandler { call, result ->
            when (call.method) {
                "canDrawOverlays" -> result.success(
                    Build.VERSION.SDK_INT < Build.VERSION_CODES.M ||
                        Settings.canDrawOverlays(this),
                )
                "requestPermission" -> {
                    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) {
                        startActivity(
                            Intent(
                                Settings.ACTION_MANAGE_OVERLAY_PERMISSION,
                                Uri.parse("package:$packageName"),
                            ),
                        )
                    }
                    result.success(null)
                }
                "start" -> result.success(GlobalOverlayService.start(this))
                "startTaskNotifications" -> result.success(GlobalOverlayService.startTaskNotifications(this))
                "stop" -> {
                    GlobalOverlayService.stop(this)
                    result.success(null)
                }
                "isRunning" -> result.success(GlobalOverlayService.isRunning())
                else -> result.notImplemented()
            }
        }
        EventChannel(
            flutterEngine.dartExecutor.binaryMessenger,
            "chat_codex/global_overlay_events",
        ).setStreamHandler(GlobalOverlayEventBridge)
        EventChannel(
            flutterEngine.dartExecutor.binaryMessenger,
            "chat_codex/notification_events",
        ).setStreamHandler(NotificationEventBridge)
        EventChannel(
            flutterEngine.dartExecutor.binaryMessenger,
            "chat_codex/notification_intents",
        ).setStreamHandler(object : EventChannel.StreamHandler {
            override fun onListen(arguments: Any?, events: EventSink?) {
                notificationSink = events
                if (launchPayload.isNotEmpty()) {
                    events?.success(launchPayload)
                    launchPayload = emptyMap()
                }
            }

            override fun onCancel(arguments: Any?) {
                notificationSink = null
            }
        })
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        val payload = notificationPayload(intent)
        if (payload.isNotEmpty()) {
            launchPayload = payload
            notificationSink?.success(payload)
        }
    }

    private fun notificationPayload(intent: Intent?): Map<String, String> {
        val type = intent?.getStringExtra("notification_type")?.trim().orEmpty()
        if (type.isEmpty()) return emptyMap()
        return mapOf(
            "type" to type,
            "task_id" to (intent?.getStringExtra("task_id") ?: ""),
            "session_id" to (intent?.getStringExtra("session_id") ?: ""),
            "agent_id" to (intent?.getStringExtra("agent_id") ?: ""),
            "machine_id" to (intent?.getStringExtra("machine_id") ?: ""),
            "project_id" to (intent?.getStringExtra("project_id") ?: ""),
            "project_root" to (intent?.getStringExtra("project_root") ?: ""),
        )
    }
}
