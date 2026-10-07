package com.chatcodex.chat_codex_app.overlay

import android.os.Handler
import android.os.Looper
import io.flutter.plugin.common.EventChannel
import org.json.JSONObject

/** 在界面可见时，把原生后台服务的通知变更转发给 Flutter。 */
object NotificationEventBridge : EventChannel.StreamHandler {
    private val handler = Handler(Looper.getMainLooper())
    private var sink: EventChannel.EventSink? = null

    override fun onListen(arguments: Any?, events: EventChannel.EventSink?) {
        sink = events
    }

    override fun onCancel(arguments: Any?) {
        sink = null
    }

    fun diagnostic(event: String, data: Map<String, Any> = emptyMap()) {
        emit("notification.diagnostic", JSONObject(data).put("event", event).toString())
    }

    fun emit(type: String, data: String) {
        handler.post {
            sink?.success(mapOf("type" to type, "data" to data))
        }
    }
}
