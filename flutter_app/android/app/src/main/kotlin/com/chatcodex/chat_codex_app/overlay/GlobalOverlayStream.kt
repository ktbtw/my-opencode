package com.chatcodex.chat_codex_app.overlay

import android.content.Context
import android.net.ConnectivityManager
import android.net.Network
import android.os.Build
import android.os.SystemClock
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
            // A rapid off/on setting change may try to restart while the old
            // loop is still exiting. Recheck after releasing its singleton slot.
            handler.post { if (!destroyed && backgroundNotificationEnabled) connectEventStream() }
        }
    }
}

/** 注册默认网络回调，网络恢复时重连事件流。 */
internal fun GlobalOverlayService.registerNetworkCallback() {
    if (Build.VERSION.SDK_INT < Build.VERSION_CODES.N) return
    val manager = getSystemService(ConnectivityManager::class.java) ?: return
    var currentNetwork: Network? = null
    val callback = object : ConnectivityManager.NetworkCallback() {
        override fun onAvailable(network: Network) {
            val changed = currentNetwork != null && currentNetwork != network
            currentNetwork = network
            handler.post {
                if (!destroyed && backgroundNotificationEnabled) {
                    ensureHealthyEventStream("network-available", networkChanged = changed)
                    reconcileNotificationFeed("network-available")
                }
            }
        }

        override fun onLost(network: Network) {
            // A late onLost for the old Wi-Fi must not close a new mobile socket.
            if (currentNetwork != network) return
            currentNetwork = null
            // disconnect() can wait for an in-flight read on some Android
            // implementations. Keep it off the UI / lifecycle thread.
            val connection = streamConnection
            if (!destroyed) executor.execute { runCatching { connection?.disconnect() } }
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
    val requestScope = notificationCursorScope
    var retriedAuth = false
    while (!destroyed && backgroundNotificationEnabled) {
        val token = prefs.getString("flutter.access_token", "").orEmpty()
        check(token.isNotBlank())
        val connection = (URL("$base/api/overlay/events").openConnection() as HttpURLConnection).apply {
            requestMethod = "GET"
            connectTimeout = 10_000
            readTimeout = NOTIFICATION_STREAM_READ_TIMEOUT_MS
            setRequestProperty("Authorization", "Bearer $token")
            setRequestProperty("Accept", "text/event-stream")
            setRequestProperty("Cache-Control", "no-cache")
            // 始终携带游标。全新身份从 0 开始，让服务端重放已持久化的
            // 通知变更，而不是直接跳到当前最新位置。
            setRequestProperty("X-Overlay-Notification-Cursor", notificationCursor.toString())
        }
        streamConnection = connection
        lastStreamActivityAt = SystemClock.elapsedRealtime()
        try {
            val statusCode = connection.responseCode
            Log.i(TAG, "event stream response status=$statusCode base=$base")
            if (statusCode == HttpURLConnection.HTTP_UNAUTHORIZED && !retriedAuth && refreshAccessToken(token)) {
                retriedAuth = true
                continue
            }
            check(statusCode in 200..299)
            handler.post {
                if (destroyed || !backgroundNotificationEnabled || requestScope != notificationCursorScope) return@post
                streamConnected = true
                lastStreamError = ""
                refreshOverlayContent()
                GlobalOverlayEventBridge.emit(statusSnapshot())
            }
            connection.inputStream.bufferedReader(Charsets.UTF_8).use { reader ->
                var eventType = ""
                val data = StringBuilder()
                while (!destroyed && backgroundNotificationEnabled && requestScope == notificationCursorScope) {
                    val line = reader.readLine() ?: break
                    // Include SSE comments/heartbeats, not only business events.
                    lastStreamActivityAt = SystemClock.elapsedRealtime()
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
                                dispatchStreamEvent(eventType, data.toString(), requestScope)
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
internal fun GlobalOverlayService.dispatchStreamEvent(eventType: String, rawData: String, requestScope: String = notificationCursorScope) {
    val data = runCatching { JSONObject(rawData) }.getOrNull() ?: run {
        Log.w(TAG, "event stream invalid json type=$eventType bytes=${rawData.length}")
        return
    }
    handler.post {
        if (destroyed || requestScope != notificationCursorScope) return@post
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

/** 检查连接存活；网络切换/心跳丢失时强制断开旧连接，由单例循环重放通知。 */
internal fun GlobalOverlayService.ensureHealthyEventStream(reason: String, networkChanged: Boolean = false) {
    if (destroyed || !backgroundNotificationEnabled) return
    val idleMs = SystemClock.elapsedRealtime() - lastStreamActivityAt
    val connection = streamConnection
    if (connection != null && (networkChanged || idleMs >= NOTIFICATION_STREAM_STALE_MS)) {
        Log.w(TAG, "notification stream reconnect reason=$reason idleMs=$idleMs cursor=$notificationCursor")
        NotificationEventBridge.diagnostic("stream_reconnect", mapOf("reason" to reason, "idle_ms" to idleMs, "cursor" to notificationCursor))
        executor.execute { runCatching { connection.disconnect() } }
    }
    connectEventStream()
}

/**
 * 后台独立读取持久通知 feed。SSE 半开、代理缓冲等情况下仍有补发路径。
 * 首次 cursor=0 的 REST 接口返回最近快照（不是完整分页），因此初次重放
 * 仍交给 SSE；已建立非零检查点后才使用 REST 增量分页。
 */
internal fun GlobalOverlayService.reconcileNotificationFeed(reason: String) {
    if (destroyed || !backgroundNotificationEnabled || notificationCursor <= 0L) return
    synchronized(this) {
        if (notificationReconcileRunning) return
        notificationReconcileRunning = true
    }
    val scope = notificationCursorScope
    val startCursor = notificationCursor
    executor.execute {
        try {
            var cursor = startCursor
            val latest = mutableMapOf<String, JSONObject>()
            for (pageIndex in 0 until 10) {
                if (destroyed || !backgroundNotificationEnabled || scope != notificationCursorScope) return@execute
                val page = apiRequest("/api/notifications?after=$cursor&limit=200")
                    ?: error(lastApiError.ifBlank { "notification reconciliation failed" })
                val items = page.optJSONArray("items") ?: JSONArray()
                for (index in 0 until items.length()) {
                    val change = items.optJSONObject(index) ?: continue
                    val operationId = change.optString("operation_id")
                    if (operationId.isBlank()) continue
                    val previous = latest[operationId]
                    if (previous == null || previous.optLong("version") < change.optLong("version")) {
                        latest[operationId] = change
                    }
                }
                val next = page.optLong("cursor", cursor)
                if (next <= cursor) break
                cursor = next
                if (items.length() < 200) break
            }
            val changes = latest.values.sortedBy { it.optLong("version") }
            handler.post {
                if (destroyed || !backgroundNotificationEnabled || scope != notificationCursorScope) return@post
                for (change in changes) {
                    applyNotificationUpdate(change)
                    NotificationEventBridge.emit("notification.updated", change.toString())
                }
                if (!notificationDeliveryBlocked && pendingNotificationChanges.isEmpty()) recordNotificationCursor(cursor)
                if (changes.isNotEmpty()) {
                    Log.i(TAG, "notification reconciliation reason=$reason from=$startCursor through=$cursor changes=${changes.size}")
                    NotificationEventBridge.diagnostic("feed_reconciled", mapOf("reason" to reason, "from" to startCursor, "through" to cursor, "changes" to changes.size))
                }
            }
        } catch (error: Exception) {
            Log.w(TAG, "notification reconciliation failed reason=$reason: ${error.message}")
            NotificationEventBridge.diagnostic("feed_reconcile_failed", mapOf("reason" to reason, "error" to error.javaClass.simpleName))
        } finally {
            notificationReconcileRunning = false
        }
    }
}
