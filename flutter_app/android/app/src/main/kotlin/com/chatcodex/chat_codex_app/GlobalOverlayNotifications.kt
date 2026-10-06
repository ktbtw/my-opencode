package com.chatcodex.chat_codex_app

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.SharedPreferences
import android.graphics.Color
import android.media.AudioAttributes
import android.os.Build
import android.provider.Settings
import android.util.Log
import androidx.core.app.NotificationCompat
import org.json.JSONArray
import org.json.JSONObject
import java.security.MessageDigest

/**
 * 通知与通知游标。
 *
 * 负责前台常驻通知、任务完成提醒通知，以及通知变更的游标（cursor）管理与
 * 投递失败重试队列。游标按「服务器地址 + 登录身份」做隔离，避免串号。
 */

/** 构建前台常驻通知。 */
internal fun GlobalOverlayService.buildNotification(): Notification = NotificationCompat.Builder(this, CHANNEL_ID)
    .setSmallIcon(R.drawable.ic_overlay_send)
    .setContentTitle(if (overlayEnabled) "全局助手已开启" else "后台任务通知已开启")
    .setContentText(if (overlayEnabled) "悬浮层会显示 Agent 状态和完成消息" else "任务完成后会在通知栏提醒你")
    .setOngoing(true)
    .build()

/** 判断系统通知与完成提醒渠道是否可用。 */
internal fun GlobalOverlayService.notificationsAvailable(): Boolean {
    val manager = getSystemService(NotificationManager::class.java)
    val enabled = Build.VERSION.SDK_INT < Build.VERSION_CODES.N || manager.areNotificationsEnabled()
    val channelImportance = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
        manager.getNotificationChannel(COMPLETION_CHANNEL_ID)?.importance
            ?: NotificationManager.IMPORTANCE_NONE
    } else {
        NotificationManager.IMPORTANCE_DEFAULT
    }
    return enabled && channelImportance != NotificationManager.IMPORTANCE_NONE
}

/** 弹出任务完成通知，返回是否成功投递。 */
internal fun GlobalOverlayService.showTaskCompletionNotification(item: CompletionItem): Boolean {
    val manager = getSystemService(NotificationManager::class.java)
    val enabled = Build.VERSION.SDK_INT < Build.VERSION_CODES.N || manager.areNotificationsEnabled()
    val channel = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
        manager.getNotificationChannel(COMPLETION_CHANNEL_ID)
    } else {
        null
    }
    Log.i(
        TAG,
        "posting task notification taskId=${item.taskId} enabled=$enabled " +
            "channelImportance=${channel?.importance ?: -1} channel=${COMPLETION_CHANNEL_ID}",
    )
    if (!enabled || channel?.importance == NotificationManager.IMPORTANCE_NONE) {
        Log.w(TAG, "task notification not posted: notifications unavailable taskId=${item.taskId}")
        return false
    }
    val intent = Intent(this, MainActivity::class.java).apply {
        flags = Intent.FLAG_ACTIVITY_CLEAR_TOP or Intent.FLAG_ACTIVITY_SINGLE_TOP
        putExtra("notification_type", "task_completed")
        putExtra("task_id", item.taskId)
        putExtra("session_id", item.sessionId)
        putExtra("agent_id", item.agentId)
        putExtra("machine_id", item.machineId)
        putExtra("project_id", item.projectId)
        putExtra("project_root", item.projectRoot)
    }
    val pendingIntent = PendingIntent.getActivity(
        this,
        item.taskId.hashCode(),
        intent,
        PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
    )
    val summary = item.summary.trim().let { value ->
        when {
            value.isEmpty() -> "任务已完成，可以查看详细结果"
            value.length <= 240 -> value
            else -> value.take(237) + "..."
        }
    }
    val posted = runCatching {
        manager.notify(
            item.taskId.hashCode(),
            NotificationCompat.Builder(this, COMPLETION_CHANNEL_ID)
                .setSmallIcon(R.drawable.ic_overlay_send)
                .setColor(Color.rgb(47, 107, 242))
                .setContentTitle(item.title)
                .setContentText(summary)
                .setStyle(NotificationCompat.BigTextStyle().bigText(summary))
                .setContentIntent(pendingIntent)
                .setAutoCancel(true)
                .setCategory(NotificationCompat.CATEGORY_STATUS)
                .setPriority(NotificationCompat.PRIORITY_HIGH)
                .setDefaults(NotificationCompat.DEFAULT_SOUND or NotificationCompat.DEFAULT_VIBRATE)
                .build(),
        )
    }.onFailure { error ->
        Log.e(TAG, "task notification post failed taskId=${item.taskId}", error)
    }.isSuccess
    if (!posted) return false
    Log.i(TAG, "task notification posted taskId=${item.taskId} notificationId=${item.taskId.hashCode()}")
    return true
}

/** 主应用是否在前台。 */
internal fun GlobalOverlayService.isChatCodexForeground(): Boolean = MainActivity.isAppForeground()

/** 创建前台通知与完成提醒两个通知渠道。 */
internal fun GlobalOverlayService.createNotificationChannel() {
    if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return
    getSystemService(NotificationManager::class.java).createNotificationChannel(
        NotificationChannel(CHANNEL_ID, "全局助手", NotificationManager.IMPORTANCE_LOW),
    )
    val completionChannel = NotificationChannel(
        COMPLETION_CHANNEL_ID,
        "任务完成提醒",
        NotificationManager.IMPORTANCE_HIGH,
    ).apply {
        description = "后台任务完成时提醒"
        enableVibration(true)
        vibrationPattern = longArrayOf(0, 240, 120, 240)
        enableLights(true)
        setSound(
            Settings.System.DEFAULT_NOTIFICATION_URI,
            AudioAttributes.Builder()
                .setUsage(AudioAttributes.USAGE_NOTIFICATION)
                .build(),
        )
    }
    getSystemService(NotificationManager::class.java).createNotificationChannel(completionChannel)
}

/**
 * 加载当前身份对应的通知游标。
 *
 * 用「服务器地址 + 登录身份」的 SHA-256 作为作用域，切换账号/服务器时各自独立，
 * 避免把 A 账号的游标用到 B 账号上。
 */
@Synchronized
internal fun GlobalOverlayService.loadNotificationCursorScope(prefs: SharedPreferences, base: String) {
    val identity = prefs.getString("flutter.operator_key", "").orEmpty()
        .ifBlank { prefs.getString("flutter.username", "").orEmpty() }
    val digest = MessageDigest.getInstance("SHA-256")
        .digest("$base|$identity".toByteArray(Charsets.UTF_8))
        .joinToString("") { byte -> (byte.toInt() and 0xff).toString(16).padStart(2, '0') }
    if (digest == notificationCursorScope) return
    notificationCursorScope = digest
    notificationCursor = prefs.getLong("$NOTIFICATION_CURSOR_PREF_PREFIX:$digest", 0L)
    notificationCursorInitialized = prefs.getBoolean("$NOTIFICATION_CURSOR_PREF_PREFIX:$digest:initialized", false)
    Log.i(TAG, "notification cursor scope loaded initialized=$notificationCursorInitialized cursor=$notificationCursor")
}

/** 记录并持久化通知游标（只前进，不回退）。 */
@Synchronized
internal fun GlobalOverlayService.recordNotificationCursor(cursor: Long) {
    if (cursor < 0 || (notificationCursorInitialized && cursor <= notificationCursor)) return
    val scope = notificationCursorScope
    if (scope.isBlank()) return
    val saved = getSharedPreferences(FLUTTER_PREFS, Context.MODE_PRIVATE)
        .edit()
        .putLong("$NOTIFICATION_CURSOR_PREF_PREFIX:$scope", cursor)
        .putBoolean("$NOTIFICATION_CURSOR_PREF_PREFIX:$scope:initialized", true)
        .commit()
    if (!saved) {
        Log.w(TAG, "notification cursor persist failed cursor=$cursor")
        return
    }
    notificationCursor = cursor
    notificationCursorInitialized = true
    Log.i(TAG, "notification cursor updated cursor=$cursor")
}

/**
 * 处理一条通知变更：已读/删除直接撤销，未读则在后台时弹出完成通知；
 * 通知不可用或投递失败时进入待重试队列，游标不越过这些待投递项。
 */
internal fun GlobalOverlayService.applyNotificationUpdate(change: JSONObject) {
    if (!backgroundNotificationEnabled) return
    val version = change.optLong("version")
    if (version <= notificationCursor) return
    val operationId = change.optString("operation_id")
    val record = change.optJSONObject("record")
    if (pendingNotificationChanges.any { (pendingVersion, pending) ->
            pendingVersion > version && pending.optString("operation_id") == operationId
        }) return
    // A read receipt must overtake blocked delivery, including offline retries.
    // Do not advance the cursor past unrelated pending notifications.
    if (change.optBoolean("deleted") || record?.optBoolean("unread", true) == false) {
        pendingNotificationChanges.entries.removeAll { it.value.optString("operation_id") == operationId }
        val taskId = record?.optJSONObject("metadata")?.optString("task_id")
            .orEmpty().ifBlank { operationId.removePrefix("chat-task:") }
        if (operationId.startsWith("chat-task:") && taskId.isNotBlank()) {
            getSystemService(NotificationManager::class.java).cancel(taskId.hashCode())
        }
        persistPendingNotificationChanges()
        if (pendingNotificationChanges.isEmpty()) {
            notificationDeliveryBlocked = false
            recordNotificationCursor(version)
        } else {
            pendingNotificationChanges[version] = change
            persistPendingNotificationChanges()
        }
        return
    }
    if (notificationDeliveryBlocked) {
        pendingNotificationChanges[version] = change
        persistPendingNotificationChanges()
        scheduleNotificationRetry()
        Log.w(TAG, "notification delivery blocked; queued change version=$version")
        return
    }
    if (operationId.startsWith("chat-task:")) {
        val metadata = record?.optJSONObject("metadata") ?: JSONObject()
        val taskId = metadata.optString("task_id").ifBlank { operationId.removePrefix("chat-task:") }
        val unread = record?.optBoolean("unread", false) == true
        if (change.optBoolean("deleted") || !unread) {
            if (taskId.isNotBlank()) {
                getSystemService(NotificationManager::class.java).cancel(taskId.hashCode())
            }
            recordNotificationCursor(version)
            return
        }
        if (record?.optString("status") != "succeeded") {
            recordNotificationCursor(version)
            return
        }
        val item = CompletionItem(
            record.optString("title").ifBlank { "Agent 已完成" },
            record.optString("message"),
            taskId,
            metadata.optString("session_id"),
            metadata.optString("agent_id"),
            metadata.optString("machine_id"),
            metadata.optString("project_id"),
            metadata.optString("project_root"),
            record.optString("message"),
        )
        val foreground = isChatCodexForeground()
        Log.i(TAG, "notification change taskId=$taskId version=$version foreground=$foreground")
        if (!foreground && taskId.isNotBlank()) {
            if (!showTaskCompletionNotification(item)) {
                notificationDeliveryBlocked = true
                pendingNotificationChanges[version] = change
                persistPendingNotificationChanges()
                scheduleNotificationRetry()
                return
            }
        }
    }
    recordNotificationCursor(version)
}

/** 安排一次待投递通知的重试。 */
internal fun GlobalOverlayService.scheduleNotificationRetry() {
    handler.removeCallbacks(notificationRetry)
    handler.postDelayed(notificationRetry, NOTIFICATION_RETRY_DELAY_MS)
}

/** 重试投递之前失败的通知变更。 */
internal fun GlobalOverlayService.retryPendingNotifications() {
    if (pendingNotificationChanges.isEmpty()) {
        notificationDeliveryBlocked = false
        return
    }
    if (!backgroundNotificationEnabled) {
        Log.i(TAG, "notification retry postponed: background notifications disabled")
        return
    }
    if (!notificationsAvailable()) {
        Log.w(TAG, "notification retry postponed: notifications unavailable")
        return
    }
    notificationDeliveryBlocked = false
    val pending = pendingNotificationChanges.toSortedMap()
    pendingNotificationChanges.clear()
    persistPendingNotificationChanges()
    for ((_, change) in pending) {
        applyNotificationUpdate(change)
    }
    if (pendingNotificationChanges.isNotEmpty()) {
        persistPendingNotificationChanges()
        scheduleNotificationRetry()
    }
}

/** 启动时从偏好恢复待投递的通知变更队列。 */
internal fun GlobalOverlayService.loadPendingNotificationChanges(prefs: SharedPreferences) {
    val raw = prefs.getString(PENDING_NOTIFICATION_PREF_KEY, null).orEmpty()
    val array = runCatching { JSONArray(raw) }.getOrNull() ?: return
    for (index in 0 until array.length()) {
        val item = array.optJSONObject(index) ?: continue
        val version = item.optLong("version")
        val payload = item.optString("payload")
        if (version > 0 && payload.isNotBlank()) {
            runCatching { pendingNotificationChanges[version] = JSONObject(payload) }
        }
    }
    if (pendingNotificationChanges.isNotEmpty() && backgroundNotificationEnabled) {
        notificationDeliveryBlocked = true
        scheduleNotificationRetry()
    }
}

/** 持久化待投递的通知变更队列（按版本排序，仅保留最近若干条）。 */
internal fun GlobalOverlayService.persistPendingNotificationChanges() {
    val array = JSONArray()
    pendingNotificationChanges.toSortedMap()
        .toList()
        .takeLast(MAX_PENDING_NOTIFICATION_CHANGES)
        .forEach { (version, payload) ->
            array.put(JSONObject().put("version", version).put("payload", payload.toString()))
        }
    getSharedPreferences(FLUTTER_PREFS, Context.MODE_PRIVATE)
        .edit()
        .putString(PENDING_NOTIFICATION_PREF_KEY, array.toString())
        .commit()
}
