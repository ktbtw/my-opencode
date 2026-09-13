package com.chatcodex.chat_codex_app

import android.os.Handler
import android.os.Looper
import io.flutter.plugin.common.EventChannel

/** Bridges notification changes from the native background service to Flutter when visible. */
object NotificationEventBridge : EventChannel.StreamHandler {
    private val handler = Handler(Looper.getMainLooper())
    private var sink: EventChannel.EventSink? = null

    override fun onListen(arguments: Any?, events: EventChannel.EventSink?) {
        sink = events
    }

    override fun onCancel(arguments: Any?) {
        sink = null
    }

    fun emit(type: String, data: String) {
        handler.post {
            sink?.success(mapOf("type" to type, "data" to data))
        }
    }
}
