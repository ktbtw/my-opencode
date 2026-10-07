package com.chatcodex.chat_codex_app.overlay

/**
 * 悬浮助手（GlobalOverlayService）相关的全局常量。
 *
 * 这些常量原本定义在 GlobalOverlayService 的 companion object 内，拆分文件后
 * 提升为顶层常量，使各个拆分包中的扩展函数都能直接按名称访问。
 */

/** 日志标签。 */
internal const val TAG = "GlobalOverlayService"

/** 用户显式开启悬浮面板时发送的服务动作。 */
internal const val ACTION_START = "com.chatcodex.chat_codex_app.overlay.START"

/** 仅启动后台任务通知（不显示悬浮面板）时发送的服务动作。 */
internal const val ACTION_START_NOTIFICATIONS = "com.chatcodex.chat_codex_app.overlay.START_NOTIFICATIONS"

/** Flutter 侧写入的 SharedPreferences 文件名（两端共享同一份存储）。 */
internal const val FLUTTER_PREFS = "FlutterSharedPreferences"

/** 悬浮球/面板几何信息（位置与尺寸）的存储文件名。 */
internal const val GEOMETRY_PREFS = "global_overlay_geometry"

/** 用户是否启用悬浮助手的偏好键。 */
internal const val ENABLED_PREF_KEY = "flutter.global_agent_overlay_enabled_v1"

/** 权限模式（ask / auto-approve / deny）的偏好键。 */
internal const val PERMISSION_MODE_PREF_KEY = "flutter.permission_mode"

/** 前台常驻通知的渠道 ID。 */
internal const val CHANNEL_ID = "global_agent_overlay"

/** 任务完成提醒通知的渠道 ID。 */
internal const val COMPLETION_CHANNEL_ID = "task_completion_v3"

/** 前台常驻通知 ID。 */
internal const val NOTIFICATION_ID = 1841

/** 通知投递失败后的重试间隔（毫秒）。 */
internal const val NOTIFICATION_RETRY_DELAY_MS = 15_000L

/** 通知游标（cursor）持久化的键前缀。 */
internal const val NOTIFICATION_CURSOR_PREF_PREFIX = "overlay_notification_cursor_v2"

/** 待投递通知变更队列持久化的键。 */
internal const val PENDING_NOTIFICATION_PREF_KEY = "overlay_pending_notifications_v1"

/** 待投递通知变更队列的最大长度，防止无限增长。 */
internal const val MAX_PENDING_NOTIFICATION_CHANGES = 2000

/** 前后台切换临界点的二次确认窗口。 */
internal const val NOTIFICATION_FOREGROUND_RECHECK_MS = 750L

/** 服务端每 20 秒发送心跳；连接 65 秒无数据即重连，避免半开连接永久等待。 */
internal const val NOTIFICATION_STREAM_READ_TIMEOUT_MS = 65_000
internal const val NOTIFICATION_STREAM_STALE_MS = 65_000L

/** 独立于长连接的持久通知补偿同步。 */
internal const val NOTIFICATION_RECONCILE_INTERVAL_MS = 60_000L
