package com.chatcodex.chat_codex_app.overlay

import android.app.AlertDialog
import android.app.Service
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.net.ConnectivityManager
import android.net.Uri
import android.os.Build
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import android.provider.Settings
import android.util.Log
import android.view.MotionEvent
import android.view.View
import android.view.ViewConfiguration
import android.view.WindowManager
import android.widget.EditText
import android.widget.FrameLayout
import android.widget.ImageButton
import android.widget.LinearLayout
import android.widget.PopupWindow
import android.widget.ScrollView
import android.widget.TextView
import org.json.JSONObject
import java.net.HttpURLConnection
import java.util.concurrent.Executors

/**
 * 全局悬浮助手前台服务。
 *
 * 本文件只保留「类外壳」：对外静态入口、全部共享状态字段、生命周期回调，
 * 以及拖拽 / 缩放的触摸监听器。具体功能按职责拆分到同包下的多个扩展函数文件：
 *
 * - GlobalOverlayShell        悬浮球与面板的外壳、几何位置、整体调度
 * - GlobalOverlayViews        通用 UI 工具与自定义 View
 * - GlobalOverlayConversation 对话面板、输入框、Markdown 渲染
 * - GlobalOverlayQuestion     Agent 提问面板
 * - GlobalOverlayModelPicker  模型 / 思考强度选择
 * - GlobalOverlaySessions     历史会话与最近回复加载
 * - GlobalOverlayCompletions  Agent 状态与任务完成队列
 * - GlobalOverlayActions      发送消息、关闭确认等用户操作
 * - GlobalOverlayApi          HTTP 请求与 Token 刷新
 * - GlobalOverlayStream       SSE 事件流与网络回调
 * - GlobalOverlayNotifications 通知与通知游标
 */
class GlobalOverlayService : Service() {
    companion object {
        /** 附件选择完成后由 OverlayFilePickerActivity 广播的动作（对外公开）。 */
        const val ACTION_FILE_SELECTED = "com.chatcodex.chat_codex_app.overlay.FILE_SELECTED"

        private var instance: GlobalOverlayService? = null

        fun start(context: Context): Boolean {
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M &&
                !Settings.canDrawOverlays(context)
            ) return false
            val intent = Intent(context, GlobalOverlayService::class.java).setAction(ACTION_START)
            return runCatching {
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                    context.startForegroundService(intent)
                } else {
                    context.startService(intent)
                }
            }.onFailure { error ->
                Log.e(TAG, "overlay service start failed", error)
            }.isSuccess
        }

        fun startTaskNotifications(context: Context): Boolean {
            val enabled = context.getSharedPreferences(FLUTTER_PREFS, Context.MODE_PRIVATE)
                .getBoolean("flutter.background_notification_enabled_v1", true)
            if (!enabled && instance == null) return true
            val intent = Intent(context, GlobalOverlayService::class.java)
                .setAction(ACTION_START_NOTIFICATIONS)
            return runCatching {
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                    context.startForegroundService(intent)
                } else {
                    context.startService(intent)
                }
            }.onFailure { error ->
                Log.e(TAG, "task notification service start failed", error)
            }.isSuccess
        }

        fun stop(context: Context) {
            context.getSharedPreferences(FLUTTER_PREFS, Context.MODE_PRIVATE)
                .edit()
                .putString(ENABLED_PREF_KEY, "false")
                .apply()
            instance?.disableOverlay()
        }

        fun isRunning(): Boolean = instance?.stopping != true && instance?.overlayEnabled == true

        fun currentStatus(): Map<String, Any>? = instance?.statusSnapshot()
    }

    // ---- 基础设施：窗口、线程、生命周期标志 ----
    internal lateinit var windowManager: WindowManager
    internal val handler = Handler(Looper.getMainLooper())
    internal val executor = Executors.newCachedThreadPool()
    internal var root: FrameLayout? = null
    internal var composeUiState: com.chatcodex.chat_codex_app.overlay.compose.OverlayUiState? = null
    internal var overlayLifecycleOwner: com.chatcodex.chat_codex_app.overlay.compose.OverlayLifecycleOwner? = null
    internal var overlayEnabled = false
    @Volatile internal var notificationCursor = 0L
    @Volatile internal var notificationCursorInitialized = false
    @Volatile internal var notificationCursorScope = ""
    @Volatile internal var backgroundNotificationEnabled = true
    internal var windowParams: WindowManager.LayoutParams? = null
    internal var expanded = false
    internal var composing = false
    internal var selectedKey = ""
    internal var conversationKey = ""
    internal var modelPickerKey = ""
    internal var sessionPanelKey = ""
    internal val attachments = mutableListOf<OverlayAttachment>()

    // ---- Agent 与模型状态 ----
    internal var agents: List<AgentState> = emptyList()
    internal val modelsByMachineId = mutableMapOf<String, List<OverlayModel>>()
    internal val defaultModelByMachineId = mutableMapOf<String, String>()
    internal val modelLoadingMachineIds = mutableSetOf<String>()
    internal val modelErrorsByMachineId = mutableMapOf<String, String>()
    internal val selectedModelsByAgentKey = mutableMapOf<String, String>()
    internal val selectedVariantsByAgentKey = mutableMapOf<String, String>()
    internal val modelSettingsLoadingKeys = mutableSetOf<String>()
    internal val modelSettingsLoadedKeys = mutableSetOf<String>()

    // ---- 最近回复 / 会话缓存 ----
    internal val latestRepliesByAgentKey = mutableMapOf<String, String>()
    internal val latestSessionIdsByAgentKey = mutableMapOf<String, String>()
    internal val latestSentMessagesByAgentKey = mutableMapOf<String, String>()
    internal val latestSentStatesByAgentKey = mutableMapOf<String, String>()
    internal val latestSentTaskIdsByAgentKey = mutableMapOf<String, String>()
    internal val latestReplyLoadingKeys = mutableSetOf<String>()
    internal val latestReplyLoadedKeys = mutableSetOf<String>()
    internal val newConversationKeys = mutableSetOf<String>()
    internal val sessionsByAgentKey = mutableMapOf<String, List<OverlaySession>>()
    internal val sessionLoadingKeys = mutableSetOf<String>()

    // ---- 提问面板状态 ----
    internal val pendingQuestionsByTaskId = mutableMapOf<String, OverlayQuestion>()
    internal val questionDraftAnswersByTaskId = mutableMapOf<String, MutableList<MutableList<String>>>()
    internal val questionCustomAnswersByTaskId = mutableMapOf<String, MutableList<String>>()
    internal val questionSubmittingTaskIds = mutableSetOf<String>()
    internal val animatedQuestionRequestIds = mutableSetOf<String>()

    // ---- 任务完成队列 ----
    internal val completions = ArrayDeque<CompletionItem>()
    internal val deferredCompletions = ArrayDeque<CompletionItem>()
    internal var currentCompletion: CompletionItem? = null
    internal var showQueuePrompt = false
    internal val seenCompletionTaskIds = LinkedHashSet<String>()

    // ---- 视图引用 ----
    internal var input: EditText? = null
    internal var sendButton: ImageButton? = null
    internal var attachmentLabel: TextView? = null
    internal var panelView: LinearLayout? = null

    // ---- 面板几何 ----
    internal var panelWidthPx = 0
    internal var panelHeightPx = 0
    internal var panelX = -1
    internal var panelY = -1
    internal var bubbleX = -1
    internal var bubbleY = -1

    // ---- 网络与事件流 ----
    @Volatile internal var stopping = false
    @Volatile internal var streamConnected = false
    @Volatile internal var streamLoopRunning = false
    @Volatile internal var lastStreamError = ""
    @Volatile internal var lastStreamEventAt = 0L
    @Volatile internal var lastStreamActivityAt = 0L
    @Volatile internal var notificationReconcileRunning = false
    @Volatile internal var lastApiError = ""
    internal val tokenRefreshLock = Any()
    @Volatile internal var streamConnection: HttpURLConnection? = null
    internal var networkCallback: ConnectivityManager.NetworkCallback? = null
    @Volatile internal var destroyed = false

    // ---- 通知投递 ----
    internal val pendingNotificationChanges = mutableMapOf<Long, JSONObject>()
    @Volatile internal var notificationDeliveryBlocked = false

    internal var notificationRetryScheduledAt = 0L

    internal val notificationRetry = object : Runnable {
        override fun run() {
            notificationRetryScheduledAt = 0L
            if (destroyed) return
            retryPendingNotifications()
            if (notificationDeliveryBlocked || pendingNotificationChanges.isNotEmpty()) {
                scheduleNotificationRetry()
            }
        }
    }

    internal val notificationHealthCheck = object : Runnable {
        override fun run() {
            if (destroyed) return
            if (backgroundNotificationEnabled) {
                // 诊断：后台通知链路的关键状态，定位「收不到完成提醒」时看这一行。
                Log.i(
                    TAG,
                    "health check enabled=true foreground=${isChatCodexForeground()} " +
                        "streamConnected=$streamConnected cursor=$notificationCursor " +
                        "pending=${pendingNotificationChanges.size} blocked=$notificationDeliveryBlocked",
                )
                ensureHealthyEventStream("periodic")
                if (!isChatCodexForeground()) reconcileNotificationFeed("periodic")
            } else {
                Log.i(TAG, "health check enabled=false (后台通知已关闭)")
            }
            handler.postDelayed(this, NOTIFICATION_RECONCILE_INTERVAL_MS)
        }
    }

    private val attachmentReceiver = object : BroadcastReceiver() {
        override fun onReceive(context: Context, intent: Intent) {
            val raw = intent.getStringExtra("uri") ?: return
            val attachment = OverlayAttachment(Uri.parse(raw), intent.getStringExtra("name") ?: "附件")
            if (attachments.none { it.uri == attachment.uri }) attachments.add(attachment)
            handler.postDelayed({
                if (!destroyed) {
                    expanded = true
                    renderOverlay()
                }
            }, 150)
        }
    }

    override fun onCreate() {
        super.onCreate()
        stopping = false
        instance = this
        // 服务启动时只做后台通知。只有用户显式发送 ACTION_START 时才启用悬浮面板。
        overlayEnabled = false
        val prefs = getSharedPreferences(FLUTTER_PREFS, MODE_PRIVATE)
        val base = prefs.getString("flutter.base_url", "https://www.xyapi.top/codex").orEmpty().trimEnd('/')
        loadNotificationCursorScope(prefs, base)
        backgroundNotificationEnabled = prefs.getBoolean("flutter.background_notification_enabled_v1", true)
        windowManager = getSystemService(WINDOW_SERVICE) as WindowManager
        createNotificationChannel()
        startForeground(NOTIFICATION_ID, buildNotification())
        registerNetworkCallback()
        handler.postDelayed(notificationHealthCheck, NOTIFICATION_RECONCILE_INTERVAL_MS)
        Log.i(
            TAG,
            "service created notificationOnly=${!overlayEnabled} " +
                "notificationsAvailable=${notificationsAvailable()} " +
                "backgroundNotificationEnabled=$backgroundNotificationEnabled",
        )
        if (overlayEnabled) {
            restoreOverlayGeometry()
            registerAttachmentReceiver()
            renderOverlay()
        }
        if (backgroundNotificationEnabled) {
            connectEventStream()
        } else {
            Log.i(TAG, "background notification disabled by user, skipping event stream connection")
        }
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent?.action == ACTION_START && Settings.canDrawOverlays(this) && !overlayEnabled) {
            overlayEnabled = true
            restoreOverlayGeometry()
            registerAttachmentReceiver()
            renderOverlay()
        }

        // 服务重启时重新读取后台通知开关
        if (intent?.action == ACTION_START_NOTIFICATIONS) {
            val prefs = getSharedPreferences(FLUTTER_PREFS, MODE_PRIVATE)
            val newEnabled = prefs.getBoolean("flutter.background_notification_enabled_v1", true)
            if (newEnabled != backgroundNotificationEnabled) {
                Log.i(TAG, "background notification setting changed: $backgroundNotificationEnabled -> $newEnabled")
                backgroundNotificationEnabled = newEnabled
            }
            val oldScope = notificationCursorScope
            val base = prefs.getString("flutter.base_url", "https://www.xyapi.top/codex").orEmpty().trimEnd('/')
            loadNotificationCursorScope(prefs, base)
            if (newEnabled) {
                ensureHealthyEventStream("start-command", networkChanged = oldScope != notificationCursorScope)
                reconcileNotificationFeed("start-command")
                if (pendingNotificationChanges.isNotEmpty()) scheduleNotificationRetry()
            } else {
                streamConnected = false
                val connection = streamConnection
                executor.execute { runCatching { connection?.disconnect() } }
                GlobalOverlayEventBridge.emit(statusSnapshot())
                Log.i(TAG, "SSE disabled by user preference")
                if (!overlayEnabled) {
                    stopSelfResult(startId)
                }
            }
        }

        return START_STICKY
    }

    override fun onDestroy() {
        saveOverlayGeometry()
        handler.removeCallbacksAndMessages(null)
        destroyed = true
        val connection = streamConnection
        streamConnection = null
        // Do not wait on a blocked network reader inside Service.onDestroy.
        // This closer outlives executor.shutdownNow() below.
        if (connection != null) {
            Thread({ runCatching { connection.disconnect() } }, "notification-stream-close").apply {
                isDaemon = true
                start()
            }
        }
        unregisterNetworkCallback()
        executor.shutdownNow()
        runCatching { unregisterReceiver(attachmentReceiver) }
        root?.let { runCatching { windowManager.removeViewImmediate(it) } }
        root = null
        instance = null
        GlobalOverlayEventBridge.emit(mapOf("running" to false, "connected" to false))
        super.onDestroy()
    }

    internal fun disableOverlay() {
        val disable = {
            if (!destroyed) {
                saveOverlayGeometry()
                overlayEnabled = false
                                        runCatching { unregisterReceiver(attachmentReceiver) }
                root?.let { runCatching { windowManager.removeViewImmediate(it) } }
                root = null
                panelView = null
                sendButton = null
                GlobalOverlayEventBridge.emit(mapOf("running" to false, "connected" to streamConnected))
            }
        }
        if (Looper.myLooper() == Looper.getMainLooper()) {
            disable()
        } else {
            handler.post(disable)
        }
    }

    override fun onBind(intent: Intent?): IBinder? = null

    internal fun registerAttachmentReceiver() {
        val filter = IntentFilter(ACTION_FILE_SELECTED)
        if (Build.VERSION.SDK_INT >= 33) {
            registerReceiver(attachmentReceiver, filter, RECEIVER_NOT_EXPORTED)
        } else {
            @Suppress("DEPRECATION")
            registerReceiver(attachmentReceiver, filter)
        }
    }

    /** 拖拽悬浮球 / 面板的触摸监听器。 */
    internal inner class DragTouchListener : View.OnTouchListener {
        private var downX = 0f
        private var downY = 0f
        private var startX = 0
        private var startY = 0
        private var moved = false
        private var longPressTriggered = false
        private var pendingLongPress: Runnable? = null

        private fun cancelLongPress() {
            pendingLongPress?.let(handler::removeCallbacks)
            pendingLongPress = null
        }

        override fun onTouch(view: View, event: MotionEvent): Boolean {
            val params = windowParams ?: return false
            when (event.actionMasked) {
                MotionEvent.ACTION_DOWN -> {
                    downX = event.rawX; downY = event.rawY
                    startX = params.x; startY = params.y; moved = false
                    longPressTriggered = false
                    pendingLongPress = Runnable {
                        if (!moved) longPressTriggered = view.performLongClick()
                    }.also {
                        handler.postDelayed(it, ViewConfiguration.getLongPressTimeout().toLong())
                    }
                    return true
                }
                MotionEvent.ACTION_MOVE -> {
                    val dx = (event.rawX - downX).toInt()
                    val dy = (event.rawY - downY).toInt()
                    if (kotlin.math.abs(dx) > dp(6) || kotlin.math.abs(dy) > dp(6)) {
                        moved = true
                        cancelLongPress()
                    }
                    if (moved) {
                        val width = root?.width ?: view.width
                        val height = root?.height ?: view.height
                        val maxX = (resources.displayMetrics.widthPixels - width).coerceAtLeast(0)
                        val maxY = (resources.displayMetrics.heightPixels - height).coerceAtLeast(0)
                        params.x = (startX + dx).coerceIn(0, maxX)
                        params.y = (startY + dy).coerceIn(0, maxY)
                        if (!expanded) {
                            bubbleX = params.x
                            bubbleY = params.y
                        } else {
                            panelX = params.x
                            panelY = params.y
                        }
                        root?.let { windowManager.updateViewLayout(it, params) }
                    }
                    return true
                }
                MotionEvent.ACTION_UP -> {
                    cancelLongPress()
                    if (moved) saveOverlayGeometry()
                    if (!moved && !longPressTriggered) view.performClick()
                    return true
                }
                MotionEvent.ACTION_CANCEL -> {
                    cancelLongPress()
                    return true
                }
            }
            return true
        }
    }

    /** 面板右下角缩放的触摸监听器。 */
    internal inner class ResizeTouchListener : View.OnTouchListener {
        private var downX = 0f
        private var downY = 0f
        private var startWidth = 0
        private var startHeight = 0

        override fun onTouch(view: View, event: MotionEvent): Boolean {
            when (event.actionMasked) {
                MotionEvent.ACTION_DOWN -> {
                    downX = event.rawX
                    downY = event.rawY
                    startWidth = panelWidthPx
                    startHeight = panelHeightPx
                    return true
                }
                MotionEvent.ACTION_MOVE -> {
                    val params = windowParams ?: return true
                    val maxWidth = (resources.displayMetrics.widthPixels - params.x - dp(8)).coerceAtLeast(dp(300))
                    val maxHeight = (resources.displayMetrics.heightPixels - params.y - dp(32)).coerceAtLeast(dp(380))
                    panelWidthPx = (startWidth + (event.rawX - downX).toInt()).coerceIn(dp(300), maxWidth)
                    panelHeightPx = (startHeight + (event.rawY - downY).toInt()).coerceIn(dp(380), maxHeight)
                    panelView?.layoutParams?.let {
                        it.width = panelWidthPx
                        it.height = panelHeightPx
                        panelView?.layoutParams = it
                    }
                    root?.requestLayout()
                    return true
                }
                MotionEvent.ACTION_UP -> {
                    saveOverlayGeometry()
                    view.performClick()
                    return true
                }
            }
            return true
        }
    }
}

/** 悬浮层的运行状态快照（供事件桥与 Flutter 侧查询）。 */
internal fun GlobalOverlayService.statusSnapshot(): Map<String, Any> = mapOf(
    "running" to (!stopping && !destroyed && overlayEnabled),
    "mode" to (composeUiState?.mode?.name ?: "BALL"),
    "agents" to agents.count { it.online },
    "busy" to agents.count { it.online && it.taskId.isNotBlank() },
    "stream_connected" to streamConnected,
    "has_completion" to (currentCompletion != null),
)
