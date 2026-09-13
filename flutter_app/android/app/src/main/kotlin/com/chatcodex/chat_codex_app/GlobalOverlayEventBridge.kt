package com.chatcodex.chat_codex_app

import android.os.Handler
import android.os.Looper
import io.flutter.plugin.common.EventChannel

object GlobalOverlayEventBridge : EventChannel.StreamHandler {
    private val handler = Handler(Looper.getMainLooper())
    private var sink: EventChannel.EventSink? = null

    override fun onListen(arguments: Any?, events: EventChannel.EventSink?) {
        sink = events
        GlobalOverlayService.currentStatus()?.let(::emit)
    }

    override fun onCancel(arguments: Any?) {
        sink = null
    }

    fun emit(event: Map<String, Any>) {
        handler.post { sink?.success(event) }
    }
}
