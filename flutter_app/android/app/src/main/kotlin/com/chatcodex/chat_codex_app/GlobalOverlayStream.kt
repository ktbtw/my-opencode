package com.chatcodex.chat_codex_app

import android.content.Context
import android.net.ConnectivityManager
import android.net.Network
import android.os.Build
import android.util.Log
import org.json.JSONArray
import org.json.JSONObject
import java.net.HttpURLConnection
import java.net.URL
import java.util.concurrent.TimeUnit

/**
 * SSE 事件流：与后端 /api/overlay/events 建立长连接，接收设备/Agent/任务/通知事件，
 * 断线自动退避重连；并在网络恢复时主动重连。
 */

/** 启动事件流循环（单例，已在运行则不重复启动）。 */
internal fun GlobalOverlayService.connectEventStream() {
    synchronized(this) {
        if (destroyed || !backgroundNotificationEnabled || streamLoopRunning) return
        streamLoopRunning = true
    }
    executor.execute {
        try {
            var retrySeconds = 1L
            while (!destroyed && backgroundNotificationEnabled && !Thread.currentThread().isInterrupted) {
                val streamResult = runCatching { readEventStream() }
                streamResult.exceptionOrNull()?.let { error ->
                    lastStreamError = error.message.orEmpty().ifBlank { error::class.simpleName.orEmpty() }
                    Log.w(TAG, "event stream disconnected: ${error.message}", error)
                }
                val connected = streamResult.isSuccess
                handler.post {
                    streamConnected = false
                    refreshOverlayContent()
                    GlobalOverlayEventBridge.emit(statusSnapshot())
                }
                if (destroyed || !backgroundNotificationEnabled) return@execute
                if (connected) retrySeconds = 1L
                runCatching { TimeUnit.SECONDS.sleep(retrySeconds) }
                retrySeconds = (retrySeconds * 2).coerceAtMost(30L)
            }
        } finally {
            synchronized(this) { streamLoopRunning = false }
        }
    }
}

/** 注册默认网络回调，网络恢复时重连事件流。 */
internal fun GlobalOverlayService.registerNetworkCallback() {
    if (Build.VERSION.SDK_INT < Build.VERSION_CODES.N) return
    val manager = getSystemService(ConnectivityManager::class.java) ?: return
    val callback = object : ConnectivityManager.NetworkCallback() {
        override fun onAvailable(network: Network) {
            if (!destroyed && backgroundNotificationEnabled) connectEventStream()
        }

        override fun onLost(network: Network) {
            streamConnection?.disconnect()
        }
    }
    networkCallback = callback
    runCatching { manager.registerDefaultNetworkCallback(callback) }
        .onFailure { error -> Log.w(TAG, "network callback registration failed", error) }
}

/** 注销网络回调。 */
internal fun GlobalOverlayService.unregisterNetworkCallback() {
    if (Build.VERSION.SDK_INT < Build.VERSION_CODES.N) return
    val callback = networkCallback ?: return
    networkCallback = null
    val manager = getSystemService(ConnectivityManager::class.java) ?: return
    runCatching { manager.unregisterNetworkCallback(callback) }
}

/** 读取事件流：处理 401 重登，逐行解析 SSE 事件并分发。 */
internal fun GlobalOverlayService.readEventStream() {
    val prefs = getSharedPreferences(FLUTTER_PREFS, Context.MODE_PRIVATE)
    val base = prefs.getString("flutter.base_url", "https://www.xyapi.top/codex").orEmpty().trimEnd('/')
    check(base.isNotBlank())
    loadNotificationCursorScope(prefs, base)
    var retriedAuth = false
    while (!destroyed && backgroundNotificationEnabled) {
        val token = prefs.getString("flutter.access_token", "").orEmpty()
        check(token.isNotBlank())
        val connection = (URL("$base/api/overlay/events").openConnection() as HttpURLConnection).apply {
            requestMethod = "GET"
            connectTimeout = 10_000
            readTimeout = 0
            setRequestProperty("Authorization", "Bearer $token")
            setRequestProperty("Accept", "text/event-stream")
            setRequestProperty("Cache-Control", "no-cache")
            // Always send a cursor. A fresh identity uses 0 so the server
            // can replay durable notification changes instead of jumping
            // straight to the current head.
            setRequestProperty("X-Overlay-Notification-Cursor", notificationCursor.toString())
        }
        streamConnection = connection
        try {
            val statusCode = connection.responseCode
            Log.i(TAG, "event stream response status=$statusCode base=$base")
            if (statusCode == HttpURLConnection.HTTP_UNAUTHORIZED && !retriedAuth && refreshAccessToken(token)) {
                retriedAuth = true
                continue
            }
            check(statusCode in 200..299)
            handler.post {
                streamConnected = true
                lastStreamError = ""
                refreshOverlayContent()
                GlobalOverlayEventBridge.emit(statusSnapshot())
            }
            connection.inputStream.bufferedReader(Charsets.UTF_8).use { reader ->
                var eventType = ""
                val data = StringBuilder()
                while (!destroyed) {
                    val line = reader.readLine() ?: break
                    when {
                        line.startsWith("event:") -> eventType = line.substringAfter(':').trim()
                        line.startsWith("data:") -> {
                            if (data.isNotEmpty()) data.append('\n')
                            data.append(line.substringAfter(':').trimStart())
                        }
                        line.isEmpty() -> {
                            if (eventType.isNotBlank() && data.isNotEmpty()) {
                                lastStreamEventAt = System.currentTimeMillis()
                                Log.i(TAG, "event stream received type=$eventType bytes=${data.length}")
                                dispatchStreamEvent(eventType, data.toString())
                            }
                            eventType = ""
                            data.clear()
                        }
                    }
                }
            }
            return
        } finally {
            streamConnection = null
            connection.disconnect()
        }
    }
}

/** 把单条 SSE 事件分发到对应的处理函数（在主线程执行）。 */
internal fun GlobalOverlayService.dispatchStreamEvent(eventType: String, rawData: String) {
    val data = runCatching { JSONObject(rawData) }.getOrNull() ?: run {
        Log.w(TAG, "event stream invalid json type=$eventType bytes=${rawData.length}")
        return
    }
    handler.post {
        when (eventType) {
            "snapshot" -> {
                applySnapshot(data.optJSONArray("devices") ?: JSONArray())
                val snapshotCursor = data.optLong("notification_cursor", -1L)
                if (snapshotCursor >= 0 && !notificationDeliveryBlocked && pendingNotificationChanges.isEmpty()) {
                    recordNotificationCursor(snapshotCursor)
                } else if (snapshotCursor >= 0) {
                    Log.w(TAG, "notification cursor held at $notificationCursor; pending delivery through $snapshotCursor")
                }
            }
            "agent.upsert" -> upsertAgent(data)
            "agent.remove" -> removeAgent(data.optString("machine_id"), data.optString("agent_id"))
            "task.updated" -> applyTaskUpdate(data)
            "notification.updated" -> applyNotificationUpdate(data)
        }
        if (eventType == "notification.updated") {
            NotificationEventBridge.emit(eventType, rawData)
        }
    }
}
