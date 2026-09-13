package com.chatcodex.chat_codex_app

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.AlertDialog
import android.app.Service
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.content.res.ColorStateList
import android.graphics.Color
import android.graphics.drawable.ColorDrawable
import android.graphics.Canvas
import android.graphics.Paint
import android.graphics.Path
import android.graphics.Typeface
import android.graphics.drawable.GradientDrawable
import android.graphics.PixelFormat
import android.net.ConnectivityManager
import android.net.Network
import android.net.Uri
import android.os.Build
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import android.provider.Settings
import android.text.Editable
import android.text.InputType
import android.text.SpannableStringBuilder
import android.text.Spanned
import android.text.TextWatcher
import android.text.style.BackgroundColorSpan
import android.text.style.ForegroundColorSpan
import android.text.style.RelativeSizeSpan
import android.text.style.StyleSpan
import android.text.style.TypefaceSpan
import android.util.Base64
import android.util.Log
import android.view.Gravity
import android.view.MotionEvent
import android.view.View
import android.view.ViewConfiguration
import android.view.WindowManager
import android.view.animation.DecelerateInterpolator
import android.widget.EditText
import android.widget.FrameLayout
import android.widget.ImageButton
import android.widget.ImageView
import android.widget.Button
import android.widget.CheckBox
import android.widget.LinearLayout
import android.widget.HorizontalScrollView
import android.widget.PopupWindow
import android.widget.RadioButton
import android.widget.ScrollView
import android.widget.TextView
import android.widget.Toast
import androidx.core.app.NotificationCompat
import org.json.JSONArray
import org.json.JSONObject
import java.io.BufferedReader
import java.io.InputStreamReader
import java.net.HttpURLConnection
import java.net.URL
import java.security.MessageDigest
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit

class GlobalOverlayService : Service() {
    private class MaxHeightScrollView(
        context: Context,
        private val maxHeight: Int,
    ) : ScrollView(context) {
        override fun onMeasure(widthMeasureSpec: Int, heightMeasureSpec: Int) {
            val constrainedHeight = View.MeasureSpec.makeMeasureSpec(
                maxHeight,
                View.MeasureSpec.AT_MOST,
            )
            super.onMeasure(widthMeasureSpec, constrainedHeight)
        }
    }

    companion object {
        private const val TAG = "GlobalOverlayService"
        const val ACTION_FILE_SELECTED = "com.chatcodex.chat_codex_app.overlay.FILE_SELECTED"
        private const val ACTION_START = "com.chatcodex.chat_codex_app.overlay.START"
        private const val ACTION_START_NOTIFICATIONS = "com.chatcodex.chat_codex_app.overlay.START_NOTIFICATIONS"
        private const val CHANNEL_ID = "global_agent_overlay"
        private const val COMPLETION_CHANNEL_ID = "task_completion_v3"
        private const val NOTIFICATION_RETRY_DELAY_MS = 15_000L
        private const val NOTIFICATION_ID = 1841
        private const val GEOMETRY_PREFS = "global_overlay_geometry"
        private const val FLUTTER_PREFS = "FlutterSharedPreferences"
        private const val ENABLED_PREF_KEY = "flutter.global_agent_overlay_enabled_v1"
        private const val PERMISSION_MODE_PREF_KEY = "flutter.permission_mode"
        private const val NOTIFICATION_CURSOR_PREF_PREFIX = "overlay_notification_cursor_v2"
        private const val PENDING_NOTIFICATION_PREF_KEY = "overlay_pending_notifications_v1"
        private const val MAX_PENDING_NOTIFICATION_CHANGES = 2000
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

    private data class AgentState(
        val machineId: String,
        val deviceName: String,
        val agentId: String,
        val name: String,
        val projectId: String,
        val projectRoot: String,
        val status: String,
        val taskId: String,
    ) {
        val online: Boolean
            get() = status.lowercase() !in setOf("offline", "stopped", "failed", "disabled")
        val busy: Boolean
            get() = taskId.isNotBlank() || status.lowercase() in setOf("starting", "restarting", "upgrading")
        val key: String
            get() = "$machineId\u0000$agentId\u0000$projectId"
    }

    private data class CompletionItem(
        val title: String,
        val summary: String,
        val taskId: String,
        var sessionId: String,
        var agentId: String,
        var machineId: String = "",
        var projectId: String = "",
        var projectRoot: String = "",
        var result: String = "",
    )

    private data class HttpResult(val statusCode: Int, val body: String)

    private data class RenderSettings(
        val markdown: Boolean,
        val latex: Boolean,
        val mermaid: Boolean,
        val html: Boolean,
    )

    private data class OverlayAttachment(val uri: Uri, val name: String)

    private data class OverlayQuestionOption(
        val label: String,
        val description: String,
    )

    private data class OverlayQuestionItem(
        val question: String,
        val header: String,
        val options: List<OverlayQuestionOption>,
        val multiple: Boolean,
        val custom: Boolean,
    )

    private data class OverlayQuestion(
        val taskId: String,
        val requestId: String,
        val sessionId: String,
        val agentKey: String,
        val agentId: String,
        val machineId: String,
        val projectId: String,
        val questions: List<OverlayQuestionItem>,
    )

    private data class OverlaySession(
        val sessionId: String,
        val summary: String,
        val updatedAt: String,
    )

    private data class OverlayModel(
        val providerId: String,
        val modelId: String,
        val name: String,
        val variants: Set<String>,
    ) {
        val metaKey: String
            get() = "$providerId/$modelId"
    }

    private lateinit var windowManager: WindowManager
    private val handler = Handler(Looper.getMainLooper())
    private val executor = Executors.newCachedThreadPool()
    private var root: FrameLayout? = null
    private var overlayEnabled = false
    @Volatile private var notificationCursor = 0L
    @Volatile private var notificationCursorInitialized = false
    @Volatile private var notificationCursorScope = ""
    @Volatile private var backgroundNotificationEnabled = true
    private var windowParams: WindowManager.LayoutParams? = null
    private var expanded = false
    private var composing = false
    private var selectedKey = ""
    private var conversationKey = ""
    private var modelPickerKey = ""
    private var sessionPanelKey = ""
    private val attachments = mutableListOf<OverlayAttachment>()
    private var agents: List<AgentState> = emptyList()
    private val modelsByMachineId = mutableMapOf<String, List<OverlayModel>>()
    private val defaultModelByMachineId = mutableMapOf<String, String>()
    private val modelLoadingMachineIds = mutableSetOf<String>()
    private val modelErrorsByMachineId = mutableMapOf<String, String>()
    private val selectedModelsByAgentKey = mutableMapOf<String, String>()
    private val selectedVariantsByAgentKey = mutableMapOf<String, String>()
    private val modelSettingsLoadingKeys = mutableSetOf<String>()
    private val modelSettingsLoadedKeys = mutableSetOf<String>()
    private val latestRepliesByAgentKey = mutableMapOf<String, String>()
    private val latestSessionIdsByAgentKey = mutableMapOf<String, String>()
    private val latestSentMessagesByAgentKey = mutableMapOf<String, String>()
    private val latestSentStatesByAgentKey = mutableMapOf<String, String>()
    private val latestSentTaskIdsByAgentKey = mutableMapOf<String, String>()
    private val latestReplyLoadingKeys = mutableSetOf<String>()
    private val latestReplyLoadedKeys = mutableSetOf<String>()
    private val newConversationKeys = mutableSetOf<String>()
    private val sessionsByAgentKey = mutableMapOf<String, List<OverlaySession>>()
    private val sessionLoadingKeys = mutableSetOf<String>()
    private val pendingQuestionsByTaskId = mutableMapOf<String, OverlayQuestion>()
    private val questionDraftAnswersByTaskId = mutableMapOf<String, MutableList<MutableList<String>>>()
    private val questionCustomAnswersByTaskId = mutableMapOf<String, MutableList<String>>()
    private val questionSubmittingTaskIds = mutableSetOf<String>()
    private val animatedQuestionRequestIds = mutableSetOf<String>()
    private val completions = ArrayDeque<CompletionItem>()
    private val deferredCompletions = ArrayDeque<CompletionItem>()
    private var currentCompletion: CompletionItem? = null
    private var showQueuePrompt = false
    private var input: EditText? = null
    private var sendButton: ImageButton? = null
    private var attachmentLabel: TextView? = null
    private var panelView: LinearLayout? = null
    private var conversationScroll: ScrollView? = null
    private var conversationScrollY = 0
    private var renderedConversationKey = ""
    private var bubbleContainer: FrameLayout? = null
    private var bubbleBrand: BrandMarkView? = null
    private var bubbleSummary: CompletionTickerView? = null
    private var closeConfirmationDialog: AlertDialog? = null
    @Volatile private var stopping = false
    private var modelPickerPopup: PopupWindow? = null
    @Volatile private var streamConnected = false
    @Volatile private var streamLoopRunning = false
    @Volatile private var lastStreamError = ""
    @Volatile private var lastStreamEventAt = 0L
    @Volatile private var lastApiError = ""
    private var panelWidthPx = 0
    private var panelHeightPx = 0
    private var panelX = -1
    private var panelY = -1
    private var bubbleX = -1
    private var bubbleY = -1
    private val tokenRefreshLock = Any()
    @Volatile private var streamConnection: HttpURLConnection? = null
    private var networkCallback: ConnectivityManager.NetworkCallback? = null
    @Volatile private var destroyed = false
    private val seenCompletionTaskIds = LinkedHashSet<String>()
    private val pendingNotificationChanges = mutableMapOf<Long, JSONObject>()
    @Volatile private var notificationDeliveryBlocked = false

    private val notificationRetry = object : Runnable {
        override fun run() {
            if (destroyed) return
            retryPendingNotifications()
            if (notificationDeliveryBlocked || pendingNotificationChanges.isNotEmpty()) {
                handler.postDelayed(this, NOTIFICATION_RETRY_DELAY_MS)
            }
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
        // The service starts in notification-only mode. The overlay is enabled
        // only when the user explicitly sends ACTION_START.
        overlayEnabled = false
        val prefs = getSharedPreferences(FLUTTER_PREFS, MODE_PRIVATE)
        val base = prefs.getString("flutter.base_url", "https://www.xyapi.top/codex").orEmpty().trimEnd('/')
        loadNotificationCursorScope(prefs, base)
        backgroundNotificationEnabled = prefs.getBoolean("flutter.background_notification_enabled_v1", true)
        loadPendingNotificationChanges(prefs)
        windowManager = getSystemService(WINDOW_SERVICE) as WindowManager
        createNotificationChannel()
        startForeground(NOTIFICATION_ID, buildNotification())
        registerNetworkCallback()
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

        // Reload background notification preference when service restarts
        if (intent?.action == ACTION_START_NOTIFICATIONS) {
            val prefs = getSharedPreferences(FLUTTER_PREFS, MODE_PRIVATE)
            val newEnabled = prefs.getBoolean("flutter.background_notification_enabled_v1", true)
            if (newEnabled != backgroundNotificationEnabled) {
                Log.i(TAG, "background notification setting changed: $backgroundNotificationEnabled -> $newEnabled")
                backgroundNotificationEnabled = newEnabled
            }
            if (newEnabled) {
                connectEventStream()
            } else {
                streamConnected = false
                streamConnection?.disconnect()
                streamConnection = null
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
        streamConnection?.disconnect()
        streamConnection = null
        unregisterNetworkCallback()
        modelPickerPopup?.dismiss()
        closeConfirmationDialog?.dismiss()
        closeConfirmationDialog = null
        executor.shutdownNow()
        runCatching { unregisterReceiver(attachmentReceiver) }
        root?.let { runCatching { windowManager.removeViewImmediate(it) } }
        root = null
        instance = null
        GlobalOverlayEventBridge.emit(mapOf("running" to false, "connected" to false))
        super.onDestroy()
    }

    private fun stopImmediately() {
        if (stopping) return
        stopping = true
        val stop = {
            handler.removeCallbacksAndMessages(null)
            destroyed = true
            closeConfirmationDialog?.dismiss()
            closeConfirmationDialog = null
            modelPickerPopup?.dismiss()
            modelPickerPopup = null
            root?.let { runCatching { windowManager.removeViewImmediate(it) } }
            root = null
            panelView = null
            bubbleContainer = null
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.N) {
                stopForeground(STOP_FOREGROUND_REMOVE)
            } else {
                @Suppress("DEPRECATION")
                stopForeground(true)
            }
            GlobalOverlayEventBridge.emit(mapOf("running" to false, "connected" to false))
        }
        if (Looper.myLooper() == Looper.getMainLooper()) {
            stop()
        } else {
            handler.postAtFrontOfQueue(stop)
        }
    }

    private fun disableOverlay() {
        val disable = {
            if (!destroyed) {
                saveOverlayGeometry()
                overlayEnabled = false
                closeConfirmationDialog?.dismiss()
                closeConfirmationDialog = null
                modelPickerPopup?.dismiss()
                modelPickerPopup = null
                runCatching { unregisterReceiver(attachmentReceiver) }
                root?.let { runCatching { windowManager.removeViewImmediate(it) } }
                root = null
                panelView = null
                bubbleContainer = null
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

    private fun registerAttachmentReceiver() {
        val filter = IntentFilter(ACTION_FILE_SELECTED)
        if (Build.VERSION.SDK_INT >= 33) {
            registerReceiver(attachmentReceiver, filter, RECEIVER_NOT_EXPORTED)
        } else {
            @Suppress("DEPRECATION")
            registerReceiver(attachmentReceiver, filter)
        }
    }

    private fun renderOverlay() {
        if (stopping || destroyed || !overlayEnabled) return
        root?.let { runCatching { windowManager.removeView(it) } }
        panelView = null
        conversationScroll = null
        sendButton = null
        bubbleContainer = null
        bubbleBrand = null
        bubbleSummary = null
        val container = FrameLayout(this)
        if (expanded) {
            container.addView(buildPanel(), FrameLayout.LayoutParams(panelWidthPx, panelHeightPx).apply {
                gravity = Gravity.END or Gravity.CENTER_VERTICAL
                rightMargin = dp(8)
            })
        } else {
            val bubble = buildBubble()
            container.addView(bubble, FrameLayout.LayoutParams(dp(48), dp(48)).apply {
                gravity = Gravity.END or Gravity.CENTER_VERTICAL
                rightMargin = dp(6)
            })
            updateBubble()
        }
        val freshWindow = windowParams == null
        val params = windowParams ?: WindowManager.LayoutParams(
            WindowManager.LayoutParams.WRAP_CONTENT,
            WindowManager.LayoutParams.WRAP_CONTENT,
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                WindowManager.LayoutParams.TYPE_APPLICATION_OVERLAY
            } else {
                @Suppress("DEPRECATION")
                WindowManager.LayoutParams.TYPE_PHONE
            },
            WindowManager.LayoutParams.FLAG_NOT_TOUCH_MODAL,
            PixelFormat.TRANSLUCENT,
        ).apply {
            gravity = Gravity.TOP or Gravity.START
        }
        val contentWidth = if (expanded) panelWidthPx + dp(8) else dp(54)
        val contentHeight = if (expanded) panelHeightPx else dp(48)
        val maxX = (resources.displayMetrics.widthPixels - contentWidth).coerceAtLeast(0)
        val maxY = (resources.displayMetrics.heightPixels - contentHeight).coerceAtLeast(0)
        val savedX = if (expanded) panelX else bubbleX
        val savedY = if (expanded) panelY else bubbleY
        if (savedX >= 0 && savedY >= 0) {
            params.x = savedX.coerceIn(0, maxX)
            params.y = savedY.coerceIn(0, maxY)
        } else if (freshWindow || expanded) {
            params.x = maxX
            params.y = maxY / 2
        } else {
            params.x = params.x.coerceIn(0, maxX)
            params.y = params.y.coerceIn(0, maxY)
        }
        params.gravity = Gravity.TOP or Gravity.START
        if (expanded) {
            panelX = params.x
            panelY = params.y
        } else {
            bubbleX = params.x
            bubbleY = params.y
        }
        params.flags = if (expanded) {
            WindowManager.LayoutParams.FLAG_NOT_TOUCH_MODAL
        } else {
            WindowManager.LayoutParams.FLAG_NOT_TOUCH_MODAL or
                WindowManager.LayoutParams.FLAG_NOT_FOCUSABLE
        }
        windowParams = params
        root = container
        runCatching { windowManager.addView(container, params) }
    }

    private fun restoreOverlayGeometry() {
        val prefs = getSharedPreferences(GEOMETRY_PREFS, MODE_PRIVATE)
        val maxPanelWidth = (resources.displayMetrics.widthPixels - dp(24)).coerceAtLeast(dp(300))
        val maxPanelHeight = (resources.displayMetrics.heightPixels - dp(120)).coerceAtLeast(dp(380))
        panelWidthPx = dp(prefs.getInt("panel_width_dp", 360)).coerceIn(dp(300), maxPanelWidth)
        panelHeightPx = dp(prefs.getInt("panel_height_dp", 600)).coerceIn(dp(380), maxPanelHeight)
        panelX = prefs.getInt("panel_x", -1)
        panelY = prefs.getInt("panel_y", -1)
        bubbleX = prefs.getInt("bubble_x", -1)
        bubbleY = prefs.getInt("bubble_y", -1)
    }

    private fun saveOverlayGeometry() {
        val density = resources.displayMetrics.density.coerceAtLeast(1f)
        getSharedPreferences(GEOMETRY_PREFS, MODE_PRIVATE).edit()
            .putInt("panel_width_dp", (panelWidthPx / density + 0.5f).toInt())
            .putInt("panel_height_dp", (panelHeightPx / density + 0.5f).toInt())
            .putInt("panel_x", panelX)
            .putInt("panel_y", panelY)
            .putInt("bubble_x", bubbleX)
            .putInt("bubble_y", bubbleY)
            .apply()
    }

    private fun buildBubble(): View {
        val wrapper = FrameLayout(this).apply {
            setPadding(dp(3), dp(3), dp(3), dp(3))
            elevation = 0f
            contentDescription = "全局助手，长按关闭"
            setOnClickListener {
                if (!expanded) {
                    windowParams?.let {
                        bubbleX = it.x
                        bubbleY = it.y
                    }
                    val pendingQuestion = pendingQuestionsByTaskId.values.firstOrNull { question ->
                        agents.any { it.online && questionBelongsToAgent(question, it) }
                    }
                    if (pendingQuestion != null) {
                        conversationKey = agents.firstOrNull { it.online && questionBelongsToAgent(pendingQuestion, it) }?.key.orEmpty()
                    }
                    val pending = currentCompletion ?: completions.firstOrNull() ?: deferredCompletions.firstOrNull()
                    if (conversationKey.isBlank() && pending != null) {
                        conversationKey = pending?.let { completion ->
                            agents.firstOrNull { agent ->
                                agent.agentId == completion.agentId &&
                                    (completion.machineId.isBlank() || agent.machineId == completion.machineId)
                            }?.key
                        }.orEmpty()
                    }
                    expanded = true
                } else {
                    expanded = false
                }
                renderOverlay()
            }
            setOnLongClickListener {
                showStopConfirmation()
                true
            }
            setOnTouchListener(DragTouchListener())
        }
        bubbleContainer = wrapper
        bubbleBrand = BrandMarkView(this).also {
            wrapper.addView(it, FrameLayout.LayoutParams(dp(38), dp(29)).apply {
                gravity = Gravity.TOP or Gravity.CENTER_HORIZONTAL
                topMargin = dp(2)
            })
        }
        bubbleSummary = CompletionTickerView(this).apply {
            contentDescription = "Agent 状态或完成消息"
        }.also { wrapper.addView(it, FrameLayout.LayoutParams(dp(36), dp(11)).apply {
            gravity = Gravity.BOTTOM or Gravity.CENTER_HORIZONTAL
            bottomMargin = dp(3)
        }) }
        return wrapper
    }

    private fun updateBubble() {
        val completion = currentCompletion ?: completions.firstOrNull() ?: deferredCompletions.firstOrNull()
        val pendingQuestionCount = pendingQuestionsByTaskId.size
        val hasQuestion = pendingQuestionCount > 0
        val hasMessage = completion != null && !hasQuestion
        val stateColor = when {
            hasQuestion -> 0xFFB45309.toInt()
            currentCompletion != null || completions.isNotEmpty() -> 0xFF15803D.toInt()
            deferredCompletions.isNotEmpty() -> 0xFFB45309.toInt()
            !streamConnected -> 0xFF64748B.toInt()
            agents.any { it.online && it.busy } -> 0xFF2563EB.toInt()
            else -> 0xFF64748B.toInt()
        }
        val stateLabel = when {
            hasQuestion -> "需回答"
            currentCompletion != null || completions.isNotEmpty() -> "完成"
            deferredCompletions.isNotEmpty() -> "待处理"
            !streamConnected -> "连接中"
            agents.any { it.online && it.busy } -> "工作中"
            else -> "空闲"
        }
        val online = agents.count { it.online }
        val busy = agents.count { it.online && it.busy }
        bubbleContainer?.apply {
            background = roundedBackground(
                if (hasMessage) 0xFF102A43.toInt() else if (hasQuestion) 0xFFFFFBEB.toInt() else 0xFFF8FBFF.toInt(),
                if (hasMessage) 0xFF22C55E.toInt() else if (hasQuestion) 0xFFF59E0B.toInt() else 0xFF78A8EE.toInt(),
                26,
            )
        }
        bubbleBrand?.accentColor = if (hasMessage) 0xFF86EFAC.toInt() else 0xFF2F6BF2.toInt()
        val bubbleText = if (hasQuestion) {
                "$pendingQuestionCount 个 Agent 等待回答"
            } else if (hasMessage) {
                val pending = completions.size + deferredCompletions.size + if (currentCompletion == null) 0 else 1
                "$pending 条 · ${completion?.result?.ifBlank { completion.summary }?.ifBlank { completion.title }.orEmpty()}"
            } else {
                if (online == 0) stateLabel else "$stateLabel $busy/$online"
            }
        bubbleSummary?.setTickerText(
            bubbleText,
            if (hasQuestion) 0xFFFFF7ED.toInt() else if (hasMessage) 0xFFE7FFF0.toInt() else stateColor,
            scroll = hasMessage || hasQuestion,
        )
        GlobalOverlayEventBridge.emit(statusSnapshot())
    }

    private fun agentStatusBackgroundForColor(color: Int): Int = when (color) {
        0xFF2563EB.toInt() -> 0xFFEFF6FF.toInt()
        0xFF15803D.toInt() -> 0xFFECFDF5.toInt()
        0xFFB45309.toInt() -> 0xFFFFFBEB.toInt()
        else -> 0xFFF1F5F9.toInt()
    }

    private fun statusSnapshot(): Map<String, Any> = mapOf(
        "running" to true,
        "connected" to streamConnected,
        "notification_enabled" to backgroundNotificationEnabled,
        "stream_loop_running" to streamLoopRunning,
        "last_stream_error" to lastStreamError,
        "last_event_at" to lastStreamEventAt,
        "device_count" to agents.filter { it.online }.map { it.machineId }.distinct().size,
        "online_agent_count" to agents.count { it.online },
        "busy_agent_count" to agents.count { it.busy },
        "pending_question_count" to pendingQuestionsByTaskId.size,
        "pending_completion_count" to (completions.size + deferredCompletions.size + if (currentCompletion == null) 0 else 1),
    )

    private fun buildPanel(): LinearLayout {
        val panel = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(dp(12), dp(12), dp(12), dp(10))
            background = roundedBackground(0xFFF7FAFF.toInt(), 0xFFD0DDEF.toInt(), 18)
        }
        panelView = panel
        populatePanel(panel)
        return panel
    }

    private fun populatePanel(panel: LinearLayout) {
        panel.removeAllViews()
        agents.firstOrNull { it.key == modelPickerKey && it.online }?.let {
            populateModelPickerPanel(panel, it)
            return
        }
        modelPickerKey = ""
        agents.firstOrNull { it.key == sessionPanelKey && it.online }?.let {
            populateSessionPanel(panel, it)
            return
        }
        sessionPanelKey = ""
        val conversationAgent = agents.firstOrNull { it.key == conversationKey && it.online }
        if (conversationAgent != null) {
            populateConversationPanel(panel, conversationAgent)
            return
        }
        conversationKey = ""
        val header = LinearLayout(this).apply { gravity = Gravity.CENTER_VERTICAL }
        header.addView(TextView(this).apply {
            text = "⋮⋮"
            gravity = Gravity.CENTER
            textSize = 18f
            setTextColor(0xFF94A3B8.toInt())
            contentDescription = "移动面板"
            setOnTouchListener(DragTouchListener())
        }, LinearLayout.LayoutParams(dp(28), dp(40)))
        header.addView(TextView(this).apply {
            text = "Agent 工作台"
            textSize = 18f
            typeface = Typeface.DEFAULT_BOLD
            setTextColor(0xFF0F1729.toInt())
            setOnTouchListener(DragTouchListener())
        }, LinearLayout.LayoutParams(0, dp(40), 1f))
        header.addView(TextView(this).apply {
            text = "收起"
            textSize = 12f
            gravity = Gravity.CENTER
            setTextColor(0xFF2563EB.toInt())
            background = roundedBackground(0xFFEFF6FF.toInt(), 0xFFBFDBFE.toInt(), 10)
            setOnClickListener { expanded = false; renderOverlay() }
        }, LinearLayout.LayoutParams(dp(56), dp(32)))
        panel.addView(header)
        panel.addView(TextView(this).apply {
            val onlineAgents = agents.filter { it.online }
            val devices = onlineAgents.map { it.machineId }.distinct().size
            val connection = if (streamConnected) "实时连接" else "正在重连"
            text = "$devices 台设备  ·  ${onlineAgents.count { it.busy }} 个工作中  ·  $connection"
            textSize = 11f
            setTextColor(0xFF7B8DA6.toInt())
            setPadding(0, 0, 0, dp(10))
        })
        if (showQueuePrompt && currentCompletion == null && completions.isEmpty() && deferredCompletions.isNotEmpty()) {
            panel.addView(buildQueuePrompt())
        }
        val scroll = ScrollView(this)
        scroll.isFillViewport = true
        val list = LinearLayout(this).apply { orientation = LinearLayout.VERTICAL }
        val onlineAgents = agents.filter { it.online }
        if (onlineAgents.isEmpty()) {
            list.addView(TextView(this).apply {
                text = if (agents.isEmpty() && !streamConnected) "正在同步 Agent 状态…" else "暂无在线 Agent"
                textSize = 12f
                setTextColor(0xFF8FA3BF.toInt())
                setPadding(0, dp(12), 0, dp(12))
            })
        } else {
            onlineAgents.take(6).forEach { list.addView(buildAgentRow(it)) }
        }
        scroll.addView(list)
        panel.addView(scroll, LinearLayout.LayoutParams(-1, 0, 1f))
        panel.addView(buildResizeHandle())
    }

    private fun populateConversationPanel(panel: LinearLayout, agent: AgentState) {
        ensureLatestReplyLoaded(agent)
        ensureModelsLoaded(agent)
        ensureSessionsLoaded(agent)
        val header = LinearLayout(this).apply { gravity = Gravity.CENTER_VERTICAL }
        header.addView(ImageButton(this).apply {
            setImageResource(R.drawable.ic_overlay_back)
            background = ColorDrawable(Color.TRANSPARENT)
            setPadding(dp(8), dp(8), dp(8), dp(8))
            contentDescription = "返回 Agent 列表"
            setOnClickListener {
                conversationKey = ""
                refreshOverlayContent()
            }
        }, LinearLayout.LayoutParams(dp(36), dp(42)))
        header.addView(ImageButton(this).apply {
            contentDescription = "历史会话"
            setImageResource(R.drawable.ic_overlay_history)
            background = ColorDrawable(Color.TRANSPARENT)
            setPadding(dp(8), dp(8), dp(8), dp(8))
            setOnClickListener {
                sessionPanelKey = agent.key
                updatePanelContent()
            }
        }, LinearLayout.LayoutParams(dp(36), dp(42)))
        header.addView(TextView(this).apply {
            text = agent.name.ifBlank { "Agent" }
            textSize = 17f
            typeface = Typeface.DEFAULT_BOLD
            setTextColor(0xFF0F1729.toInt())
            includeFontPadding = false
            gravity = Gravity.CENTER_VERTICAL
            maxLines = 1
            ellipsize = android.text.TextUtils.TruncateAt.END
            setOnTouchListener(DragTouchListener())
        }, LinearLayout.LayoutParams(0, dp(42), 1f))
        header.addView(ImageButton(this).apply {
            contentDescription = "收起悬浮面板"
            setImageResource(R.drawable.ic_overlay_collapse)
            background = ColorDrawable(Color.TRANSPARENT)
            setPadding(dp(8), dp(8), dp(8), dp(8))
            setOnClickListener { expanded = false; renderOverlay() }
        }, LinearLayout.LayoutParams(dp(36), dp(42)))
        header.addView(ImageButton(this).apply {
            contentDescription = "新建对话"
            setImageResource(R.drawable.ic_overlay_new)
            background = ColorDrawable(Color.TRANSPARENT)
            setPadding(dp(8), dp(8), dp(8), dp(8))
            setOnClickListener { startNewConversation(agent) }
        }, LinearLayout.LayoutParams(dp(36), dp(42)))
        panel.addView(header)
        panel.addView(TextView(this).apply {
            text = "${agent.deviceName}  ·  ${agentStatusLabel(agent)}"
            textSize = 11f
            setTextColor(0xFF7B8DA6.toInt())
            includeFontPadding = false
            setPadding(dp(36), 0, 0, dp(10))
        })

        val latestCompletion = sequenceOf(currentCompletion)
            .plus(completions.asSequence())
            .plus(deferredCompletions.asSequence())
            .filterNotNull()
            .firstOrNull { it.agentId == agent.agentId && (it.machineId.isBlank() || it.machineId == agent.machineId) }
        val latestReply = latestRepliesByAgentKey[agent.key]
            .orEmpty()
            .ifBlank { latestCompletion?.result?.ifBlank { latestCompletion.summary }.orEmpty() }
        val loadingLatestReply = agent.key in latestReplyLoadingKeys && latestReply.isBlank()
        val latestSentMessage = latestSentMessagesByAgentKey[agent.key].orEmpty()
        val latestSentState = latestSentStatesByAgentKey[agent.key].orEmpty()
        val pendingQuestion = pendingQuestionsByTaskId.values.firstOrNull { questionBelongsToAgent(it, agent) }
        val messageArea = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(dp(12), dp(12), dp(12), dp(12))
            background = roundedBackground(Color.WHITE, 0xFFE1E9F3.toInt(), 10)
            addView(TextView(this@GlobalOverlayService).apply {
                text = "最近回复"
                textSize = 11f
                setTextColor(0xFF8A99AD.toInt())
                setPadding(0, 0, 0, dp(7))
            })
            if (latestReply.isNotBlank()) {
                addView(buildRenderedMessage(latestReply))
            } else {
                addView(TextView(this@GlobalOverlayService).apply {
                    text = if (loadingLatestReply) "正在加载最近回复..." else "暂无最近回复"
                    textSize = 13f
                    setTextColor(0xFF94A3B8.toInt())
                    setTextIsSelectable(true)
                })
            }
            if (latestSentMessage.isNotBlank()) {
                addView(TextView(this@GlobalOverlayService).apply {
                    text = when (latestSentState) {
                        "sending" -> "你刚刚发送 · 发送中"
                        "failed" -> "你刚刚发送 · 发送失败"
                        else -> "你刚刚发送"
                    }
                    textSize = 11f
                    setTextColor(if (latestSentState == "failed") 0xFFDC2626.toInt() else 0xFF8A99AD.toInt())
                    gravity = Gravity.END
                    setPadding(0, dp(14), 0, dp(5))
                })
                addView(TextView(this@GlobalOverlayService).apply {
                    text = latestSentMessage
                    textSize = 13f
                    setTextColor(if (latestSentState == "failed") 0xFFDC2626.toInt() else 0xFF2563EB.toInt())
                    gravity = Gravity.END
                    setTextIsSelectable(true)
                })
            }
            if (latestCompletion != null && !agent.busy) {
                addView(LinearLayout(this@GlobalOverlayService).apply {
                    gravity = Gravity.CENTER_VERTICAL
                    setPadding(0, dp(14), 0, 0)
                    addView(TextView(this@GlobalOverlayService).apply {
                        text = "回复已完成"
                        textSize = 11f
                        includeFontPadding = false
                        setTextColor(0xFF15803D.toInt())
                    }, LinearLayout.LayoutParams(0, dp(34), 1f))
                    addView(TextView(this@GlobalOverlayService).apply {
                        text = "本轮已完成"
                        textSize = 12f
                        gravity = Gravity.CENTER
                        includeFontPadding = false
                        setTextColor(0xFF166534.toInt())
                        background = roundedBackground(0xFFECFDF5.toInt(), 0xFF86EFAC.toInt(), 8)
                        setOnClickListener { acknowledgeCompletionFor(agent) }
                    }, LinearLayout.LayoutParams(dp(92), dp(34)))
                }, LinearLayout.LayoutParams(-1, dp(34)))
            }
        }
        conversationScroll = ScrollView(this).apply {
            addView(messageArea)
            post { scrollTo(0, conversationScrollY.coerceAtLeast(0)) }
            setOnScrollChangeListener { _, _, scrollY, _, _ -> conversationScrollY = scrollY }
        }
        val conversationBody = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            addView(conversationScroll, LinearLayout.LayoutParams(-1, 0, 1f))
            addView(buildComposer(agent), LinearLayout.LayoutParams(-1, -2))
        }
        val conversationLayer = FrameLayout(this)
        conversationLayer.addView(conversationBody, FrameLayout.LayoutParams(-1, -1))
        if (pendingQuestion != null) {
            conversationLayer.addView(
                buildQuestionPanel(pendingQuestion),
                FrameLayout.LayoutParams(-1, -2, Gravity.BOTTOM),
            )
        }
        panel.addView(conversationLayer, LinearLayout.LayoutParams(-1, 0, 1f))
        panel.addView(buildResizeHandle())
    }

    private fun buildQuestionPanel(question: OverlayQuestion): View {
        val submitting = question.taskId in questionSubmittingTaskIds
        val answers = questionDraftAnswersByTaskId.getOrPut(question.taskId) {
            MutableList(question.questions.size) { mutableListOf() }
        }
        while (answers.size < question.questions.size) answers.add(mutableListOf())
        val customAnswers = questionCustomAnswersByTaskId.getOrPut(question.taskId) {
            MutableList(question.questions.size) { "" }
        }
        while (customAnswers.size < question.questions.size) customAnswers.add("")
        val box = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(dp(16), dp(8), dp(16), dp(14))
            elevation = dp(12).toFloat()
            background = GradientDrawable().apply {
                setColor(Color.WHITE)
                cornerRadii = floatArrayOf(
                    dp(14).toFloat(), dp(14).toFloat(),
                    dp(14).toFloat(), dp(14).toFloat(),
                    0f, 0f, 0f, 0f,
                )
            }
        }
        box.addView(View(this).apply {
            setBackgroundColor(0xFFD5E0EE.toInt())
        }, LinearLayout.LayoutParams(dp(36), dp(3)).also {
            it.gravity = Gravity.CENTER_HORIZONTAL
            it.bottomMargin = dp(10)
        })
        val heading = LinearLayout(this).apply { gravity = Gravity.CENTER_VERTICAL }
        heading.addView(TextView(this).apply {
            text = "?"
            gravity = Gravity.CENTER
            textSize = 11f
            typeface = Typeface.DEFAULT_BOLD
            setTextColor(0xFF2563EB.toInt())
            background = roundedBackground(Color.TRANSPARENT, 0xFF2563EB.toInt(), 8)
        }, LinearLayout.LayoutParams(dp(16), dp(16)))
        heading.addView(TextView(this).apply {
            text = "需要选择"
            textSize = 13f
            typeface = Typeface.DEFAULT_BOLD
            setTextColor(0xFF2563EB.toInt())
            includeFontPadding = false
            setPadding(dp(6), 0, 0, 0)
        }, LinearLayout.LayoutParams(0, dp(22), 1f))
        heading.addView(TextView(this).apply {
            text = "${question.questions.size} 题"
            gravity = Gravity.CENTER
            textSize = 11f
            typeface = Typeface.DEFAULT_BOLD
            setTextColor(0xFF2563EB.toInt())
            background = roundedBackground(0x142563EB.toInt(), 0x002563EB, 6)
            setPadding(dp(7), 0, dp(7), 0)
        }, LinearLayout.LayoutParams(-2, dp(24)))
        box.addView(heading)
        box.addView(TextView(this).apply {
            text = if (submitting) "回答已提交，Agent 正在继续工作" else "请完成下面的问题后提交，回答会继续当前任务。"
            textSize = 11f
            setTextColor(if (submitting) 0xFF16A34A.toInt() else 0xFF4A5D7A.toInt())
            setPadding(0, dp(5), 0, dp(10))
        })
        question.questions.forEachIndexed { index, item ->
            box.addView(TextView(this).apply {
                text = item.header.ifBlank { "问题 ${index + 1}" }
                textSize = 12f
                typeface = Typeface.DEFAULT_BOLD
                setTextColor(0xFF0F1729.toInt())
                includeFontPadding = false
            })
            box.addView(TextView(this).apply {
                text = item.question
                textSize = 13f
                setTextColor(0xFF4A5D7A.toInt())
                setPadding(0, dp(4), 0, dp(6))
                setTextIsSelectable(true)
            })
            item.options.forEach { option ->
                val optionText = option.label + option.description.takeIf { it.isNotBlank() }
                    ?.let { "\n$it" }.orEmpty()
                if (item.multiple) {
                    box.addView(CheckBox(this).apply {
                        text = optionText
                        textSize = 12f
                        setTextColor(0xFF0F1729.toInt())
                        buttonTintList = ColorStateList.valueOf(0xFF2563EB.toInt())
                        isChecked = answers[index].contains(option.label)
                        isEnabled = !submitting
                        setPadding(0, 0, 0, 0)
                        setOnCheckedChangeListener { _, checked ->
                            if (checked) {
                                if (!answers[index].contains(option.label)) answers[index].add(option.label)
                            } else {
                                answers[index].remove(option.label)
                            }
                        }
                    }, LinearLayout.LayoutParams(-1, -2).also { it.topMargin = dp(2) })
                } else {
                    box.addView(RadioButton(this).apply {
                        text = optionText
                        textSize = 12f
                        setTextColor(0xFF0F1729.toInt())
                        buttonTintList = ColorStateList.valueOf(0xFF2563EB.toInt())
                        isChecked = answers[index].firstOrNull() == option.label
                        isEnabled = !submitting
                        setPadding(0, 0, 0, 0)
                        setOnClickListener {
                            answers[index].clear()
                            answers[index].add(option.label)
                            if (index < customAnswers.size) customAnswers[index] = ""
                            updatePanelContent()
                        }
                    }, LinearLayout.LayoutParams(-1, -2).also { it.topMargin = dp(2) })
                }
            }
            if (item.custom) {
                val custom = EditText(this).apply {
                    hint = "输入其他回复"
                    textSize = 13f
                    setText(customAnswers[index])
                    setTextColor(0xFF0F1729.toInt())
                    setHintTextColor(0xFF8FA3BF.toInt())
                    isEnabled = !submitting
                    minLines = 1
                    maxLines = 3
                    inputType = InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_FLAG_MULTI_LINE
                    setPadding(dp(9), dp(5), dp(9), dp(5))
                    setCompoundDrawablesWithIntrinsicBounds(R.drawable.ic_overlay_edit, 0, 0, 0)
                    compoundDrawablePadding = dp(4)
                    background = roundedBackground(Color.WHITE, 0xFFD0DDEF.toInt(), 10)
                    addTextChangedListener(object : TextWatcher {
                        override fun beforeTextChanged(s: CharSequence?, start: Int, count: Int, after: Int) = Unit
                        override fun onTextChanged(s: CharSequence?, start: Int, before: Int, count: Int) {
                            customAnswers[index] = s?.toString().orEmpty()
                            if (!item.multiple && customAnswers[index].trim().isNotEmpty()) answers[index].clear()
                        }
                        override fun afterTextChanged(s: Editable?) = Unit
                    })
                }
                box.addView(custom, LinearLayout.LayoutParams(-1, dp(42)).also { it.topMargin = dp(6) })
            }
            if (index < question.questions.lastIndex) {
                box.addView(View(this).apply { setBackgroundColor(0xFFE8EFF8.toInt()) }, LinearLayout.LayoutParams(-1, dp(1)).also {
                    it.topMargin = dp(12)
                    it.bottomMargin = dp(12)
                })
            }
        }
        val actions = LinearLayout(this).apply {
            gravity = Gravity.CENTER_VERTICAL
            setPadding(0, dp(10), 0, 0)
        }
        actions.addView(TextView(this).apply {
            text = if (submitting) "等待中…" else "提交选择"
            textSize = 12f
            gravity = Gravity.CENTER
            typeface = Typeface.DEFAULT_BOLD
            setTextColor(if (submitting) 0xFF94A3B8.toInt() else Color.WHITE)
            background = roundedBackground(
                if (submitting) 0xFFE5E7EB.toInt() else 0xFF2563EB.toInt(),
                if (submitting) 0xFFD1D5DB.toInt() else 0xFF2563EB.toInt(),
                10,
            )
            isEnabled = !submitting
            setOnClickListener {
                val submitted = question.questions.mapIndexed { questionIndex, _ ->
                    val result = answers[questionIndex].toMutableList()
                    val custom = customAnswers[questionIndex].trim()
                    if (custom.isNotEmpty() && !result.contains(custom)) result.add(custom)
                    result
                }
                if (submitted.any { it.isEmpty() }) {
                    toast("请先回答全部问题")
                } else {
                    submitOverlayQuestion(question, submitted)
                }
            }
        }, LinearLayout.LayoutParams(0, dp(38), 1f))
        actions.addView(TextView(this).apply {
            text = "取消/拒绝"
            textSize = 12f
            gravity = Gravity.CENTER
            typeface = Typeface.DEFAULT_BOLD
            setTextColor(if (submitting) 0xFF94A3B8.toInt() else 0xFF2563EB.toInt())
            background = roundedBackground(Color.WHITE, 0xFFD0DDEF.toInt(), 10)
            isEnabled = !submitting
            setOnClickListener { submitOverlayQuestion(question, emptyList(), rejected = true) }
        }, LinearLayout.LayoutParams(0, dp(38), 1f).also { it.leftMargin = dp(8) })
        box.addView(actions)
        val shouldAnimate = animatedQuestionRequestIds.add(question.requestId)
        return MaxHeightScrollView(
            this,
            (panelHeightPx * 0.72f).toInt().coerceAtLeast(dp(260)),
        ).apply {
            isFillViewport = false
            isVerticalScrollBarEnabled = true
            overScrollMode = View.OVER_SCROLL_IF_CONTENT_SCROLLS
            elevation = dp(12).toFloat()
            addView(box, FrameLayout.LayoutParams(-1, -2))
            if (shouldAnimate) {
                alpha = 0f
                translationY = dp(28).toFloat()
                post {
                    animate()
                        .alpha(1f)
                        .translationY(0f)
                        .setDuration(280L)
                        .setInterpolator(DecelerateInterpolator())
                        .start()
                }
            }
        }
    }

    private fun buildAgentRow(agent: AgentState): View {
        val row = LinearLayout(this).apply {
            orientation = LinearLayout.HORIZONTAL
            gravity = Gravity.CENTER_VERTICAL
            setPadding(dp(12), dp(8), dp(10), dp(8))
            background = roundedBackground(
                if (agent.key == selectedKey) 0xFFEFF6FF.toInt() else Color.WHITE,
                if (agent.key == selectedKey) 0xFF2563EB.toInt() else 0xFFE8EFF8.toInt(),
                10,
            )
            setOnClickListener {
                selectedKey = agent.key
                conversationKey = agent.key
                updatePanelContent()
            }
        }
        val info = LinearLayout(this).apply { orientation = LinearLayout.VERTICAL; gravity = Gravity.CENTER_VERTICAL }
        info.addView(TextView(this).apply {
            text = agent.name.ifBlank { "Agent" }
            textSize = 13f
            typeface = Typeface.DEFAULT_BOLD
            setTextColor(0xFF0F1729.toInt())
        })
        info.addView(TextView(this).apply {
            text = "${agent.deviceName}  ·  ${agent.projectRoot.ifBlank { "未绑定项目" }}"
            textSize = 11f
            setTextColor(0xFF4A5D7A.toInt())
            maxLines = 1
            ellipsize = android.text.TextUtils.TruncateAt.END
        })
        row.addView(info, LinearLayout.LayoutParams(0, -1, 1f))
        val status = TextView(this).apply {
            text = agentStatusLabel(agent)
            gravity = Gravity.CENTER
            textSize = 10f
            typeface = Typeface.DEFAULT_BOLD
            setTextColor(agentStatusColor(agent))
            background = roundedBackground(agentStatusBackground(agent), agentStatusColor(agent), 9)
        }
        row.addView(status, LinearLayout.LayoutParams(dp(52), dp(28)))
        return row.apply {
            layoutParams = LinearLayout.LayoutParams(-1, dp(64)).also { it.bottomMargin = dp(7) }
        }
    }

    private fun buildModelSelector(agent: AgentState): View {
        val models = modelsByMachineId[agent.machineId]
        val settingsLoaded = agent.key in modelSettingsLoadedKeys
        val selectedRef = selectedModelsByAgentKey[agent.key].orEmpty()
        val selectedModel = models?.firstOrNull { it.metaKey == selectedRef }
        val loading = agent.machineId in modelLoadingMachineIds || agent.key in modelSettingsLoadingKeys
        val error = modelErrorsByMachineId[agent.machineId].orEmpty()
        val label = when {
            loading && (models == null || !settingsLoaded) -> "加载中"
            error.isNotBlank() && models == null -> "加载失败"
            selectedModel != null -> selectedModel.modelId
            selectedRef.isNotBlank() -> selectedRef.substringAfterLast('/')
            else -> "默认模型"
        }
        return LinearLayout(this).apply {
            gravity = Gravity.CENTER_VERTICAL
            setPadding(dp(7), 0, dp(6), 0)
            background = roundedBackground(
                if (selectedRef.isNotBlank()) 0xFFEFF6FF.toInt() else 0xFFF8FAFD.toInt(),
                if (selectedRef.isNotBlank()) 0xFF93B8F8.toInt() else 0xFFD0DDEF.toInt(),
                6,
            )
            contentDescription = "切换模型"
            addView(ImageView(this@GlobalOverlayService).apply {
                setImageResource(R.drawable.ic_overlay_model)
                if (selectedRef.isBlank()) setColorFilter(0xFF64748B.toInt(), android.graphics.PorterDuff.Mode.SRC_IN)
            }, LinearLayout.LayoutParams(dp(14), dp(14)))
            addView(TextView(this@GlobalOverlayService).apply {
                text = label
                textSize = 12f
                includeFontPadding = false
                gravity = Gravity.CENTER_VERTICAL
                maxLines = 1
                ellipsize = android.text.TextUtils.TruncateAt.END
                setPadding(dp(4), 0, dp(3), 0)
                setTextColor(if (selectedRef.isNotBlank()) 0xFF2563EB.toInt() else 0xFF64748B.toInt())
            }, LinearLayout.LayoutParams(0, -1, 1f))
            addView(ImageView(this@GlobalOverlayService).apply {
                setImageResource(R.drawable.ic_overlay_expand_more)
                if (selectedRef.isBlank()) setColorFilter(0xFF94A3B8.toInt(), android.graphics.PorterDuff.Mode.SRC_IN)
            }, LinearLayout.LayoutParams(dp(14), dp(14)))
            setOnClickListener {
                if (models == null) {
                    ensureModelsLoaded(agent, retry = true)
                    toast(if (loading) "正在加载模型" else "正在重新加载模型")
                } else {
                    modelPickerKey = agent.key
                    updatePanelContent()
                }
            }
        }.apply {
            layoutParams = LinearLayout.LayoutParams(dp(150), dp(30)).also { it.leftMargin = dp(6) }
        }
    }

    private fun selectedModelFor(agent: AgentState): OverlayModel? {
        val selectedRef = selectedModelsByAgentKey[agent.key].orEmpty()
        if (selectedRef.isBlank()) return null
        return modelsByMachineId[agent.machineId]?.firstOrNull { it.metaKey == selectedRef }
    }

    private fun buildVariantSelector(agent: AgentState, model: OverlayModel): View {
        val selected = selectedVariantsByAgentKey[agent.key]
            .orEmpty()
            .takeIf { value -> model.variants.any { it.equals(value, ignoreCase = true) } }
            .orEmpty()
        return LinearLayout(this).apply {
            gravity = Gravity.CENTER_VERTICAL
            setPadding(dp(7), 0, dp(7), 0)
            background = roundedBackground(
                if (selected.isNotBlank()) 0xFFEFF6FF.toInt() else 0xFFF8FAFD.toInt(),
                if (selected.isNotBlank()) 0xFF93B8F8.toInt() else 0xFFD0DDEF.toInt(),
                6,
            )
            addView(ImageView(this@GlobalOverlayService).apply {
                setImageResource(R.drawable.ic_overlay_thinking)
                if (selected.isBlank()) setColorFilter(0xFF64748B.toInt(), android.graphics.PorterDuff.Mode.SRC_IN)
            }, LinearLayout.LayoutParams(dp(14), dp(14)))
            addView(TextView(this@GlobalOverlayService).apply {
                text = thinkingVariantLabel(selected)
                textSize = 11f
                includeFontPadding = false
                gravity = Gravity.CENTER_VERTICAL
                setPadding(dp(4), 0, 0, 0)
                setTextColor(if (selected.isNotBlank()) 0xFF2563EB.toInt() else 0xFF64748B.toInt())
            })
            setOnClickListener { showVariantMenu(this, agent, model, selected) }
            layoutParams = LinearLayout.LayoutParams(-2, dp(30)).also { it.leftMargin = dp(6) }
        }
    }

    private fun showVariantMenu(anchor: View, agent: AgentState, model: OverlayModel, selected: String) {
        modelPickerPopup?.dismiss()
        val content = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(dp(6), dp(6), dp(6), dp(6))
            background = roundedBackground(Color.WHITE, 0xFFCBD8E8.toInt(), 10)
        }
        val options = listOf("") + model.variants
        options.forEach { variant ->
            content.addView(TextView(this).apply {
                text = thinkingVariantLabel(variant)
                textSize = 13f
                gravity = Gravity.CENTER_VERTICAL
                typeface = if (variant == selected) Typeface.DEFAULT_BOLD else Typeface.DEFAULT
                setTextColor(if (variant == selected) 0xFF2563EB.toInt() else 0xFF243247.toInt())
                setPadding(dp(12), 0, dp(12), 0)
                setBackgroundColor(if (variant == selected) 0xFFEFF6FF.toInt() else Color.TRANSPARENT)
                setOnClickListener { selectVariant(agent, variant) }
            }, LinearLayout.LayoutParams(dp(150), dp(42)))
        }
        modelPickerPopup = PopupWindow(content, dp(162), -2, true).apply {
            setBackgroundDrawable(ColorDrawable(Color.TRANSPARENT))
            isOutsideTouchable = true
            setOnDismissListener { modelPickerPopup = null }
            showAsDropDown(anchor, 0, -dp(42 * (options.size + 1)))
        }
    }

    private fun selectVariant(agent: AgentState, variant: String) {
        val previous = selectedVariantsByAgentKey[agent.key].orEmpty()
        selectedVariantsByAgentKey[agent.key] = variant
        modelPickerPopup?.dismiss()
        updatePanelContent()
        executor.execute {
            val saved = apiRequest(
                "/api/agents/${Uri.encode(agent.agentId)}/settings",
                "POST",
                JSONObject().put("selected_variant", variant),
            ) != null
            handler.post {
                if (destroyed || saved) return@post
                selectedVariantsByAgentKey[agent.key] = previous
                if (conversationKey == agent.key) updatePanelContent()
                toast("思考强度切换失败，请重试")
            }
        }
    }

    private fun thinkingVariantLabel(value: String): String = when (value.trim().lowercase()) {
        "", "_auto", "auto" -> "自动"
        "none", "off", "disabled" -> "关闭"
        "minimal" -> "最小"
        "low" -> "低"
        "medium" -> "中"
        "high" -> "高"
        "xhigh" -> "极高"
        "max" -> "最大"
        "enabled", "on" -> "开启"
        else -> value.trim()
    }

    private fun populateModelPickerPanel(panel: LinearLayout, agent: AgentState) {
        val models = modelsByMachineId[agent.machineId].orEmpty()
        val currentRef = selectedModelsByAgentKey[agent.key].orEmpty()
        val header = LinearLayout(this).apply {
            gravity = Gravity.CENTER_VERTICAL
            addView(ImageButton(this@GlobalOverlayService).apply {
                setImageResource(R.drawable.ic_overlay_back)
                background = ColorDrawable(Color.TRANSPARENT)
                setPadding(dp(8), dp(8), dp(8), dp(8))
                contentDescription = "返回对话"
                setOnClickListener {
                    modelPickerKey = ""
                    updatePanelContent()
                }
            }, LinearLayout.LayoutParams(dp(36), dp(44)))
            addView(TextView(this@GlobalOverlayService).apply {
                text = "选择模型"
                textSize = 16f
                typeface = Typeface.DEFAULT_BOLD
                setTextColor(0xFF0F1729.toInt())
                includeFontPadding = false
                gravity = Gravity.CENTER_VERTICAL
            }, LinearLayout.LayoutParams(0, dp(44), 1f))
            if (currentRef.isNotBlank()) addView(TextView(this@GlobalOverlayService).apply {
                text = "重置为默认"
                textSize = 12f
                gravity = Gravity.CENTER
                includeFontPadding = false
                setTextColor(0xFF2563EB.toInt())
                setOnClickListener { selectModel(agent, null) }
            }, LinearLayout.LayoutParams(dp(88), dp(40)))
        }
        panel.addView(header)
        val rows = LinearLayout(this).apply { orientation = LinearLayout.VERTICAL }
        fun renderOptions(query: String) {
            rows.removeAllViews()
            val normalized = query.trim().lowercase()
            val filtered = if (normalized.isBlank()) models else models.filter {
                it.name.lowercase().contains(normalized) || it.metaKey.lowercase().contains(normalized)
            }
            if (filtered.isEmpty()) {
                rows.addView(TextView(this).apply {
                    text = if (models.isEmpty()) "暂无可用模型\n请确保 opencode 服务已启动" else "无匹配模型"
                    textSize = 13f
                    gravity = Gravity.CENTER
                    setTextColor(0xFF94A3B8.toInt())
                }, LinearLayout.LayoutParams(-1, dp(120)))
            } else {
                filtered.groupBy { it.providerId }.forEach { (provider, providerModels) ->
                    rows.addView(TextView(this).apply {
                        text = provider
                        textSize = 11f
                        typeface = Typeface.DEFAULT_BOLD
                        includeFontPadding = false
                        setTextColor(0xFF94A3B8.toInt())
                        setPadding(dp(10), dp(12), dp(10), dp(4))
                    })
                    providerModels.forEach { model ->
                        rows.addView(buildModelOption(agent, model, currentRef == model.metaKey))
                    }
                }
            }
        }
        panel.addView(EditText(this).apply {
            hint = "搜索模型..."
            textSize = 12f
            setSingleLine(true)
            inputType = InputType.TYPE_CLASS_TEXT
            setCompoundDrawablesWithIntrinsicBounds(R.drawable.ic_overlay_search, 0, 0, 0)
            compoundDrawablePadding = dp(6)
            setPadding(dp(10), 0, dp(10), 0)
            background = roundedBackground(0xFFF8FAFD.toInt(), 0xFFD0DDEF.toInt(), 8)
            addTextChangedListener(object : TextWatcher {
                override fun beforeTextChanged(text: CharSequence?, start: Int, count: Int, after: Int) = Unit
                override fun onTextChanged(text: CharSequence?, start: Int, before: Int, count: Int) {
                    renderOptions(text?.toString().orEmpty())
                }
                override fun afterTextChanged(text: Editable?) = Unit
            })
        }, LinearLayout.LayoutParams(-1, dp(40)).also {
            it.leftMargin = dp(4); it.rightMargin = dp(4); it.bottomMargin = dp(8)
        })
        renderOptions("")
        panel.addView(View(this).apply { setBackgroundColor(0xFFE2E8F0.toInt()) }, LinearLayout.LayoutParams(-1, dp(1)))
        panel.addView(ScrollView(this).apply { addView(rows) }, LinearLayout.LayoutParams(-1, 0, 1f))
        panel.addView(buildResizeHandle())
    }

    private fun buildModelOption(agent: AgentState, model: OverlayModel, selected: Boolean): View =
        LinearLayout(this).apply {
            gravity = Gravity.CENTER_VERTICAL
            setPadding(dp(14), dp(6), dp(10), dp(6))
            setBackgroundColor(if (selected) 0xFFEFF6FF.toInt() else Color.TRANSPARENT)
            addView(LinearLayout(this@GlobalOverlayService).apply {
                orientation = LinearLayout.VERTICAL
                gravity = Gravity.CENTER_VERTICAL
                addView(TextView(this@GlobalOverlayService).apply {
                    text = model.modelId
                    textSize = 13f
                    includeFontPadding = false
                    typeface = if (selected) Typeface.DEFAULT_BOLD else Typeface.DEFAULT
                    maxLines = 1
                    ellipsize = android.text.TextUtils.TruncateAt.END
                    setTextColor(if (selected) 0xFF2563EB.toInt() else 0xFF243247.toInt())
                })
                if (model.name != model.modelId) addView(TextView(this@GlobalOverlayService).apply {
                    text = model.name
                    textSize = 11f
                    includeFontPadding = false
                    maxLines = 1
                    ellipsize = android.text.TextUtils.TruncateAt.END
                    setTextColor(0xFF94A3B8.toInt())
                })
            }, LinearLayout.LayoutParams(0, -1, 1f))
            addView(TextView(this@GlobalOverlayService).apply {
                text = if (selected) "✓" else ""
                textSize = 15f
                gravity = Gravity.CENTER
                includeFontPadding = false
                setTextColor(0xFF2563EB.toInt())
            }, LinearLayout.LayoutParams(dp(24), -1))
            setOnClickListener { selectModel(agent, model) }
            layoutParams = LinearLayout.LayoutParams(-1, dp(52))
        }

    private fun selectModel(agent: AgentState, model: OverlayModel?) {
        val oldModel = selectedModelsByAgentKey[agent.key].orEmpty()
        val oldVariant = selectedVariantsByAgentKey[agent.key].orEmpty()
        val nextModel = model?.metaKey.orEmpty()
        val nextVariant = oldVariant.takeIf { model != null && it in model.variants }.orEmpty()
        selectedModelsByAgentKey[agent.key] = nextModel
        selectedVariantsByAgentKey[agent.key] = nextVariant
        modelPickerKey = ""
        updatePanelContent()
        executor.execute {
            val body = JSONObject().put("selected_model", nextModel)
            if (nextVariant != oldVariant) body.put("selected_variant", nextVariant)
            val saved = apiRequest("/api/agents/${Uri.encode(agent.agentId)}/settings", "POST", body) != null
            handler.post {
                if (destroyed) return@post
                if (saved) {
                    toast(if (model == null) "已使用设备默认模型" else "已切换到 ${model.name}")
                } else {
                    selectedModelsByAgentKey[agent.key] = oldModel
                    selectedVariantsByAgentKey[agent.key] = oldVariant
                    if (conversationKey == agent.key) updatePanelContent()
                    toast("模型切换失败，请重试")
                }
            }
        }
    }

    private fun buildCompletionCard(): View {
        val item = currentCompletion ?: return TextView(this)
        val box = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(dp(10), dp(9), dp(10), dp(9))
            background = roundedBackground(0xFFECFDF5.toInt(), 0xFF86EFAC.toInt(), 10)
        }
        box.addView(TextView(this).apply {
            text = item.title.ifBlank { "Agent 已完成" }
            textSize = 13f
            typeface = Typeface.DEFAULT_BOLD
            setTextColor(0xFF166534.toInt())
        })
        box.addView(TextView(this).apply {
            text = item.result.ifBlank { item.summary }
            textSize = 12f
            setTextColor(0xFF166534.toInt())
            maxLines = 5
            setPadding(0, dp(5), 0, 0)
            setTextIsSelectable(true)
        })
        val waiting = completions.size + deferredCompletions.size
        if (waiting > 0) box.addView(TextView(this).apply {
            text = "还有 $waiting 个 Agent 完成待处理"
            textSize = 11f
            setTextColor(0xFF4A5D7A.toInt())
            setPadding(0, dp(5), 0, 0)
        })
        val actions = LinearLayout(this).apply {
            gravity = Gravity.END or Gravity.CENTER_VERTICAL
            addView(TextView(this@GlobalOverlayService).apply {
                text = if (waiting > 0) "下一个" else "收起"
                textSize = 12f
                gravity = Gravity.CENTER
                setTextColor(0xFF166534.toInt())
                setPadding(dp(12), 0, dp(8), 0)
                setOnClickListener {
                    if (waiting > 0) {
                        dismissCurrentCompletion()
                        promoteNextCompletion()
                    } else {
                        dismissCurrentCompletion()
                    }
                    refreshOverlayContent()
                }
            }, LinearLayout.LayoutParams(dp(68), dp(32)))
            if (waiting > 0) addView(TextView(this@GlobalOverlayService).apply {
                text = "稍后"
                textSize = 12f
                gravity = Gravity.CENTER
                setTextColor(0xFF4A5D7A.toInt())
                setOnClickListener {
                    deferCurrentCompletion()
                    refreshOverlayContent()
                }
            }, LinearLayout.LayoutParams(dp(52), dp(32)))
        }
        box.addView(actions)
        return box.apply {
            layoutParams = LinearLayout.LayoutParams(-1, dp(154)).also { it.bottomMargin = dp(8) }
        }
    }

    private fun buildQueuePrompt(): View {
        val box = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(dp(10), dp(9), dp(10), dp(9))
            background = roundedBackground(0xFFFFFBEB.toInt(), 0xFFFCD34D.toInt(), 10)
        }
        box.addView(TextView(this).apply {
            text = "还有 ${deferredCompletions.size} 个 Agent 已完成"
            textSize = 13f
            typeface = Typeface.DEFAULT_BOLD
            setTextColor(0xFF92400E.toInt())
        })
        box.addView(TextView(this).apply {
            text = "可以现在继续查看，也可以稍后从悬浮图标继续处理。"
            textSize = 11f
            setTextColor(0xFF92400E.toInt())
            setPadding(0, dp(4), 0, dp(4))
        })
        val actions = LinearLayout(this).apply { gravity = Gravity.END }
        actions.addView(TextView(this).apply {
            text = "继续处理"
            textSize = 12f
            gravity = Gravity.CENTER
            setTextColor(0xFF92400E.toInt())
            setPadding(dp(12), 0, dp(12), 0)
            setOnClickListener {
                showQueuePrompt = false
                promoteDeferredCompletion()
                refreshOverlayContent()
            }
        }, LinearLayout.LayoutParams(dp(86), dp(32)))
        actions.addView(TextView(this).apply {
            text = "稍后"
            textSize = 12f
            gravity = Gravity.CENTER
            setTextColor(0xFF4A5D7A.toInt())
            setOnClickListener {
                showQueuePrompt = false
                expanded = false
                refreshOverlayContent()
            }
        }, LinearLayout.LayoutParams(dp(52), dp(32)))
        box.addView(actions)
        return box.apply {
            layoutParams = LinearLayout.LayoutParams(-1, dp(106)).also { it.bottomMargin = dp(8) }
        }
    }

    private fun buildComposer(agent: AgentState): View {
        val box = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(0, dp(8), 0, 0)
        }
        attachmentLabel = TextView(this).apply {
            textSize = 11f
            setTextColor(0xFF7B8DA6.toInt())
            includeFontPadding = false
            gravity = Gravity.CENTER_VERTICAL
            text = "添加附件"
            maxLines = 1
            ellipsize = android.text.TextUtils.TruncateAt.END
            setPadding(0, 0, 0, dp(3))
        }
        box.addView(attachmentLabel, LinearLayout.LayoutParams(-1, dp(22)))
        val attachmentRow = LinearLayout(this).apply { gravity = Gravity.START or Gravity.CENTER_VERTICAL }
        attachmentRow.addView(LinearLayout(this).apply {
            gravity = Gravity.CENTER
            setPadding(dp(8), 0, dp(10), 0)
            background = roundedBackground(0xFFF8FAFD.toInt(), 0xFFD0DDEF.toInt(), 8)
            contentDescription = "添加附件"
            addView(ImageView(this@GlobalOverlayService).apply {
                setImageResource(R.drawable.ic_overlay_attach)
                scaleType = ImageView.ScaleType.CENTER_INSIDE
            }, LinearLayout.LayoutParams(dp(14), dp(14)))
            addView(TextView(this@GlobalOverlayService).apply {
                text = "附件"
                textSize = 12f
                setTextColor(0xFF4A5D7A.toInt())
                gravity = Gravity.CENTER
                includeFontPadding = false
            }, LinearLayout.LayoutParams(dp(34), -1))
            setOnClickListener { pickAttachment() }
        }, LinearLayout.LayoutParams(dp(72), dp(30)))
        attachmentRow.addView(buildModelSelector(agent))
        selectedModelFor(agent)?.takeIf { it.variants.isNotEmpty() }?.let { model ->
            attachmentRow.addView(buildVariantSelector(agent, model))
        }
        box.addView(HorizontalScrollView(this).apply {
            isHorizontalScrollBarEnabled = false
            addView(attachmentRow)
        }, LinearLayout.LayoutParams(-1, dp(36)))
        val line = LinearLayout(this).apply { gravity = Gravity.CENTER_VERTICAL }
        input = EditText(this).apply {
            hint = "让选中的 Agent 继续工作"
            textSize = 13f
            minLines = 1
            maxLines = 3
            inputType = InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_FLAG_MULTI_LINE
            setPadding(dp(10), dp(6), dp(10), dp(6))
            background = roundedBackground(Color.WHITE, 0xFFD0DDEF.toInt(), 10)
        }
        line.addView(input, LinearLayout.LayoutParams(0, dp(44), 1f))
        sendButton = ImageButton(this).apply {
            contentDescription = "发送消息"
            setImageResource(R.drawable.ic_overlay_send)
            background = roundedBackground(0xFF2563EB.toInt(), 0xFF2563EB.toInt(), 8)
            setPadding(dp(12), dp(12), dp(12), dp(12))
            setOnClickListener { sendCurrentMessage() }
            isEnabled = !composing
            alpha = if (composing) 0.55f else 1f
        }
        line.addView(sendButton, LinearLayout.LayoutParams(dp(44), dp(44)).also { it.leftMargin = dp(8) })
        box.addView(line)
        if (attachments.isNotEmpty()) box.addView(buildAttachmentPreviews())
        return box
    }

    private fun buildAttachmentPreviews(): View {
        val row = LinearLayout(this).apply { orientation = LinearLayout.HORIZONTAL }
        attachments.forEachIndexed { index, attachment ->
            row.addView(buildAttachmentPreview(index, attachment), LinearLayout.LayoutParams(dp(68), dp(138)).also {
                it.rightMargin = dp(7)
            })
        }
        return HorizontalScrollView(this).apply {
            isHorizontalScrollBarEnabled = false
            addView(row)
            layoutParams = LinearLayout.LayoutParams(-1, dp(144)).also { it.topMargin = dp(6) }
        }
    }

    private fun buildAttachmentPreview(index: Int, attachment: OverlayAttachment): View {
        val card = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            gravity = Gravity.CENTER_HORIZONTAL
            background = ColorDrawable(Color.TRANSPARENT)
        }
        val imageFrame = FrameLayout(this)
        val mime = contentResolver.getType(attachment.uri).orEmpty()
        imageFrame.addView(ImageView(this).apply {
            if (mime.startsWith("image/")) {
                setImageURI(attachment.uri)
                scaleType = ImageView.ScaleType.CENTER_CROP
            } else {
                setImageResource(android.R.drawable.ic_menu_save)
                setColorFilter(0xFF4A5D7A.toInt(), android.graphics.PorterDuff.Mode.SRC_IN)
                setPadding(dp(12), dp(12), dp(12), dp(12))
            }
            background = ColorDrawable(Color.TRANSPARENT)
        }, FrameLayout.LayoutParams(-1, -1))
        imageFrame.addView(ImageButton(this).apply {
            contentDescription = "取消附件"
            setImageResource(R.drawable.ic_overlay_close)
            background = ColorDrawable(Color.TRANSPARENT)
            setPadding(dp(5), dp(5), dp(5), dp(5))
            setOnClickListener { removeAttachment(index) }
        }, FrameLayout.LayoutParams(dp(26), dp(26), Gravity.TOP or Gravity.END))
        card.addView(imageFrame, LinearLayout.LayoutParams(dp(63), dp(112)))
        card.addView(TextView(this).apply {
            text = attachment.name.ifBlank { "附件" }
            textSize = 10f
            gravity = Gravity.CENTER
            setTextColor(0xFF4A5D7A.toInt())
            maxLines = 1
            ellipsize = android.text.TextUtils.TruncateAt.MIDDLE
            setPadding(0, dp(3), 0, 0)
        }, LinearLayout.LayoutParams(-1, dp(22)))
        return card
    }

    private fun buildResizeHandle(): View {
        val row = LinearLayout(this).apply { gravity = Gravity.END or Gravity.CENTER_VERTICAL }
        row.addView(ResizeHandleView(this).apply {
            contentDescription = "调整面板大小"
            setOnTouchListener(ResizeTouchListener())
        }, LinearLayout.LayoutParams(dp(34), dp(24)))
        return row.apply { layoutParams = LinearLayout.LayoutParams(-1, dp(24)) }
    }

    private fun pickAttachment() {
        expanded = false
        renderOverlay()
        startActivity(Intent(this, OverlayFilePickerActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
    }

    private fun removeAttachment(index: Int) {
        if (index !in attachments.indices) return
        attachments.removeAt(index)
        refreshOverlayContent()
    }

    private fun currentRenderSettings(): RenderSettings {
        val prefs = getSharedPreferences("FlutterSharedPreferences", MODE_PRIVATE)
        return RenderSettings(
            markdown = prefs.getBoolean("flutter.markdownRender", true),
            latex = prefs.getBoolean("flutter.latexRender", true),
            mermaid = prefs.getBoolean("flutter.mermaidRender", true),
            html = prefs.getBoolean("flutter.htmlPreview", true),
        )
    }

    private fun buildRenderedMessage(content: String): TextView {
        val settings = currentRenderSettings()
        return TextView(this).apply {
            textSize = 13f
            setTextColor(0xFF243247.toInt())
            setTextIsSelectable(true)
            setLineSpacing(0f, 1.35f)
            text = if (settings.markdown) markdownSpannable(content, settings)
            else content
        }
    }

    private fun markdownSpannable(content: String, settings: RenderSettings): CharSequence {
        val result = SpannableStringBuilder()
        var inCode = false
        var codeLanguage = ""
        content.replace("\r\n", "\n").split('\n').forEachIndexed { index, rawLine ->
            val line = rawLine.trimEnd()
            if (line.startsWith("```")) {
                if (inCode) {
                    inCode = false
                    codeLanguage = ""
                } else {
                    inCode = true
                    codeLanguage = line.removePrefix("```").trim().lowercase()
                    if (codeLanguage.isNotBlank()) {
                        appendStyledLine(result, codeLanguage.uppercase(), 0xFF4A5D7A.toInt(), true)
                    }
                }
            } else if (inCode) {
                val color = when {
                    codeLanguage in setOf("latex", "math") && !settings.latex -> 0xFF64748B.toInt()
                    codeLanguage == "mermaid" && !settings.mermaid -> 0xFF64748B.toInt()
                    codeLanguage in setOf("html", "css", "javascript", "js") && !settings.html -> 0xFF64748B.toInt()
                    else -> 0xFF334155.toInt()
                }
                appendStyledLine(result, line, color, false)
            } else if (line.trimStart().startsWith("#")) {
                val title = line.trimStart().trimStart('#').trim()
                val start = result.length
                result.append(title)
                result.setSpan(StyleSpan(Typeface.BOLD), start, result.length, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
                result.setSpan(RelativeSizeSpan(1.12f), start, result.length, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
                result.append('\n')
            } else if (line.trimStart().matches(Regex("^[-*+]\\s+.*"))) {
                appendInlineMarkdown(result, "• " + line.trimStart().substring(2))
                result.append('\n')
            } else {
                appendInlineMarkdown(result, line)
                result.append('\n')
            }
            if (index == content.replace("\r\n", "\n").split('\n').lastIndex && result.endsWith("\n")) {
                result.delete(result.length - 1, result.length)
            }
        }
        return result
    }

    private fun appendStyledLine(result: SpannableStringBuilder, line: String, color: Int, label: Boolean) {
        val start = result.length
        result.append(line)
        result.setSpan(TypefaceSpan("monospace"), start, result.length, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
        result.setSpan(ForegroundColorSpan(color), start, result.length, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
        if (label) result.setSpan(StyleSpan(Typeface.BOLD), start, result.length, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
        else result.setSpan(BackgroundColorSpan(0xFFF1F5F9.toInt()), start, result.length, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
        result.append('\n')
    }

    private fun appendInlineMarkdown(result: SpannableStringBuilder, line: String) {
        val pattern = Regex("(\\*\\*|__)(.+?)(\\*\\*|__)|`([^`]+)`|(https?://\\S+)")
        var cursor = 0
        pattern.findAll(line).forEach { match ->
            result.append(line.substring(cursor, match.range.first))
            val start = result.length
            val value = match.groups[2]?.value ?: match.groups[4]?.value ?: match.groups[5]?.value.orEmpty()
            result.append(value)
            when {
                match.groups[2] != null -> result.setSpan(StyleSpan(Typeface.BOLD), start, result.length, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
                match.groups[4] != null -> result.setSpan(TypefaceSpan("monospace"), start, result.length, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
                match.groups[5] != null -> result.setSpan(ForegroundColorSpan(0xFF2563EB.toInt()), start, result.length, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
            }
            cursor = match.range.last + 1
        }
        result.append(line.substring(cursor))
    }

    private fun ensureLatestReplyLoaded(agent: AgentState) {
        if (agent.key in newConversationKeys || latestSessionIdsByAgentKey[agent.key].orEmpty().isNotBlank()) return
        if (agent.key in latestReplyLoadedKeys || !latestReplyLoadingKeys.add(agent.key)) return
        executor.execute {
            val query = listOf(
                "agent_id" to agent.agentId,
                "machine_id" to agent.machineId,
                "project_id" to agent.projectId,
                "status" to "completed",
                "limit" to "1",
                "sort" to "created_desc",
            ).joinToString("&") { (key, value) -> "$key=${Uri.encode(value)}" }
            val raw = apiRaw("/api/tasks?$query")
            val reply = runCatching {
                JSONArray(raw ?: "[]").optJSONObject(0)?.optString("result").orEmpty()
            }.getOrDefault("")
            handler.post {
                latestReplyLoadingKeys.remove(agent.key)
                latestReplyLoadedKeys.add(agent.key)
                val latestTask = runCatching { JSONArray(raw ?: "[]").optJSONObject(0) }.getOrNull()
                if (reply.isNotBlank()) latestRepliesByAgentKey[agent.key] = reply
                latestTask?.optString("session_id")?.takeIf { it.isNotBlank() }?.let {
                    latestSessionIdsByAgentKey[agent.key] = it
                }
                if (conversationKey == agent.key && expanded) updatePanelContent()
            }
        }
    }

    private fun ensureSessionsLoaded(agent: AgentState, retry: Boolean = false) {
        if (!retry && (agent.key in sessionLoadingKeys || sessionsByAgentKey.containsKey(agent.key))) return
        if (!sessionLoadingKeys.add(agent.key)) return
        executor.execute {
            val query = listOf(
                "agent_id" to agent.agentId,
                "machine_id" to agent.machineId,
                "project_id" to agent.projectId,
                "limit" to "50",
            ).joinToString("&") { (key, value) -> "$key=${Uri.encode(value)}" }
            val raw = apiRaw("/api/sessions?$query")
            val sessions = runCatching {
                val array = JSONArray(raw ?: "[]")
                buildList {
                    for (i in 0 until array.length()) {
                        val item = array.optJSONObject(i) ?: continue
                        val sessionId = item.optString("session_id").trim()
                        if (sessionId.isBlank()) continue
                        add(
                            OverlaySession(
                                sessionId,
                                item.optString("summary").ifBlank { "未命名对话" },
                                item.optString("updated_at").ifBlank { item.optString("created_at") },
                            ),
                        )
                    }
                }
            }.getOrDefault(emptyList())
            handler.post {
                sessionLoadingKeys.remove(agent.key)
                sessionsByAgentKey[agent.key] = sessions
                if (conversationKey == agent.key && expanded) updatePanelContent()
            }
        }
    }

    private fun populateSessionPanel(panel: LinearLayout, agent: AgentState) {
        val sessions = sessionsByAgentKey[agent.key].orEmpty()
        val header = LinearLayout(this).apply {
            gravity = Gravity.CENTER_VERTICAL
            addView(ImageButton(this@GlobalOverlayService).apply {
                setImageResource(R.drawable.ic_overlay_back)
                background = ColorDrawable(Color.TRANSPARENT)
                setPadding(dp(8), dp(8), dp(8), dp(8))
                contentDescription = "返回对话"
                setOnClickListener {
                    sessionPanelKey = ""
                    updatePanelContent()
                }
            }, LinearLayout.LayoutParams(dp(36), dp(48)))
            addView(TextView(this@GlobalOverlayService).apply {
                text = "历史会话"
                textSize = 13f
                typeface = Typeface.DEFAULT_BOLD
                setTextColor(0xFF0F1729.toInt())
                includeFontPadding = false
                gravity = Gravity.CENTER_VERTICAL
            }, LinearLayout.LayoutParams(0, dp(48), 1f))
            addView(ImageButton(this@GlobalOverlayService).apply {
                contentDescription = "新建对话"
                setImageResource(R.drawable.ic_overlay_add)
                background = roundedBackground(0xFFEFF6FF.toInt(), 0xFFEFF6FF.toInt(), 6)
                setPadding(dp(8), dp(8), dp(8), dp(8))
                setOnClickListener { startNewConversation(agent) }
            }, LinearLayout.LayoutParams(dp(36), dp(36)).also { it.rightMargin = dp(6) })
        }
        panel.addView(header)
        panel.addView(View(this).apply { setBackgroundColor(0xFFE2E8F0.toInt()) }, LinearLayout.LayoutParams(-1, dp(1)))
        val rows = LinearLayout(this).apply { orientation = LinearLayout.VERTICAL }
        if (sessions.isEmpty()) {
            rows.addView(TextView(this).apply {
                text = if (agent.key in sessionLoadingKeys) "正在加载历史对话..." else "暂无历史对话"
                textSize = 12f
                gravity = Gravity.CENTER
                setTextColor(0xFF94A3B8.toInt())
            }, LinearLayout.LayoutParams(-1, dp(72)))
        } else {
            sessions.forEach { session -> rows.addView(buildSessionOption(agent, session)) }
        }
        panel.addView(ScrollView(this).apply { addView(rows) }, LinearLayout.LayoutParams(-1, 0, 1f))
        panel.addView(buildResizeHandle())
    }

    private fun buildSessionOption(agent: AgentState, session: OverlaySession): View =
        LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            gravity = Gravity.CENTER_VERTICAL
            setPadding(dp(10), dp(8), dp(10), dp(8))
            val selected = latestSessionIdsByAgentKey[agent.key] == session.sessionId && agent.key !in newConversationKeys
            background = roundedBackground(
                if (selected) 0xFFEFF6FF.toInt() else Color.TRANSPARENT,
                if (selected) 0xFF93B8F8.toInt() else Color.TRANSPARENT,
                7,
            )
            addView(TextView(this@GlobalOverlayService).apply {
                text = session.summary
                textSize = 12f
                includeFontPadding = false
                maxLines = 2
                ellipsize = android.text.TextUtils.TruncateAt.END
                setTextColor(if (selected) 0xFF1D4ED8.toInt() else 0xFF243247.toInt())
            })
            addView(TextView(this@GlobalOverlayService).apply {
                text = formatSessionTime(session.updatedAt)
                textSize = 10f
                includeFontPadding = false
                setTextColor(0xFF94A3B8.toInt())
                maxLines = 1
            })
            setOnClickListener {
                selectSession(agent, session)
            }
            layoutParams = LinearLayout.LayoutParams(-1, dp(60)).also {
                it.leftMargin = dp(4); it.rightMargin = dp(4); it.topMargin = dp(3)
            }
        }

    private fun selectSession(agent: AgentState, session: OverlaySession) {
        sessionPanelKey = ""
        newConversationKeys.remove(agent.key)
        latestSessionIdsByAgentKey[agent.key] = session.sessionId
        latestReplyLoadedKeys.remove(agent.key)
        latestReplyLoadingKeys.remove(agent.key)
        latestRepliesByAgentKey.remove(agent.key)
        ensureSessionLatestReplyLoaded(agent, session.sessionId)
        conversationScrollY = 0
        refreshOverlayContent()
    }

    private fun startNewConversation(agent: AgentState) {
        modelPickerPopup?.dismiss()
        sessionPanelKey = ""
        modelPickerKey = ""
        newConversationKeys.add(agent.key)
        latestSessionIdsByAgentKey.remove(agent.key)
        latestReplyLoadedKeys.add(agent.key)
        latestReplyLoadingKeys.remove(agent.key)
        latestRepliesByAgentKey.remove(agent.key)
        latestSentMessagesByAgentKey.remove(agent.key)
        latestSentStatesByAgentKey.remove(agent.key)
        latestSentTaskIdsByAgentKey.remove(agent.key)
        conversationScrollY = 0
        toast("已新建对话")
        refreshOverlayContent()
    }

    private fun ensureSessionLatestReplyLoaded(agent: AgentState, sessionId: String) {
        executor.execute {
            val query = listOf(
                "session_id" to sessionId,
                "limit" to "1",
                "sort" to "created_desc",
            ).joinToString("&") { (key, value) -> "$key=${Uri.encode(value)}" }
            val raw = apiRaw("/api/tasks?$query")
            val task = runCatching { JSONArray(raw ?: "[]").optJSONObject(0) }.getOrNull()
            val reply = task?.optString("result").orEmpty()
            handler.post {
                if (latestSessionIdsByAgentKey[agent.key] != sessionId) return@post
                if (reply.isNotBlank()) latestRepliesByAgentKey[agent.key] = reply
                latestReplyLoadedKeys.add(agent.key)
                if (conversationKey == agent.key && expanded) updatePanelContent()
            }
        }
    }

    private fun formatSessionTime(value: String): String {
        val normalized = value.trim().replace('T', ' ')
        return normalized.substringBefore('.').substringBefore('Z').ifBlank { "" }
    }

    private fun ensureModelsLoaded(agent: AgentState, retry: Boolean = false) {
        if (retry) modelErrorsByMachineId.remove(agent.machineId)
        if ((modelsByMachineId.containsKey(agent.machineId) || agent.machineId in modelLoadingMachineIds) && !retry) {
            ensureModelSettingsLoaded(agent)
            return
        }
        if (agent.machineId !in modelLoadingMachineIds) {
            modelLoadingMachineIds.add(agent.machineId)
            executor.execute {
                val path = "/api/models?machine_id=${Uri.encode(agent.machineId)}"
                val raw = apiRaw(path)
                val parsed = raw?.let(::parseModelsResponse)
                val error = lastApiError
                handler.post {
                    modelLoadingMachineIds.remove(agent.machineId)
                    if (parsed != null) {
                        modelsByMachineId[agent.machineId] = parsed.first
                        defaultModelByMachineId[agent.machineId] = parsed.second
                        modelErrorsByMachineId.remove(agent.machineId)
                    } else {
                        modelErrorsByMachineId[agent.machineId] = error.ifBlank { "模型加载失败" }
                    }
                    if (conversationKey == agent.key && expanded) updatePanelContent()
                }
            }
        }
        ensureModelSettingsLoaded(agent)
    }

    private fun ensureModelSettingsLoaded(agent: AgentState) {
        if (agent.key in modelSettingsLoadedKeys || !modelSettingsLoadingKeys.add(agent.key)) return
        executor.execute {
            val settings = apiRequest("/api/agents/${Uri.encode(agent.agentId)}/settings")
            handler.post {
                modelSettingsLoadingKeys.remove(agent.key)
                if (settings != null) {
                    selectedModelsByAgentKey[agent.key] = settings.optString("selected_model").trim()
                    selectedVariantsByAgentKey[agent.key] = settings.optString("selected_variant").trim()
                    modelSettingsLoadedKeys.add(agent.key)
                }
                if (conversationKey == agent.key && expanded) updatePanelContent()
            }
        }
    }

    private fun parseModelsResponse(raw: String): Pair<List<OverlayModel>, String>? = runCatching {
        val data = JSONObject(raw)
        val connected = buildSet {
            val values = data.optJSONArray("connected") ?: JSONArray()
            for (i in 0 until values.length()) values.optString(i).takeIf { it.isNotBlank() }?.let(::add)
        }
        val result = mutableListOf<OverlayModel>()
        val providers = data.optJSONArray("all") ?: JSONArray()
        for (i in 0 until providers.length()) {
            val provider = providers.optJSONObject(i) ?: continue
            val providerId = provider.optString("id")
            if (providerId.isBlank() || providerId !in connected) continue
            val providerName = provider.optString("name").ifBlank { providerId }
            val models = provider.optJSONObject("models") ?: continue
            val modelIds = models.keys()
            while (modelIds.hasNext()) {
                val modelId = modelIds.next()
                val model = models.optJSONObject(modelId) ?: continue
                if (model.optString("status", "active").equals("deprecated", ignoreCase = true)) continue
                val modelName = model.optString("name").ifBlank { modelId }
                val variants = buildSet {
                    when (val rawVariants = model.opt("variants")) {
                        is JSONObject -> {
                            val keys = rawVariants.keys()
                            while (keys.hasNext()) keys.next().takeIf { it.isNotBlank() }?.let(::add)
                        }
                        is JSONArray -> for (j in 0 until rawVariants.length()) {
                            rawVariants.optString(j).takeIf { it.isNotBlank() }?.let(::add)
                        }
                    }
                }
                result.add(OverlayModel(providerId, modelId, "$providerName / $modelName", variants))
            }
        }
        result.sortWith(compareBy(String.CASE_INSENSITIVE_ORDER) { it.name })
        result to data.optJSONObject("default")?.optString("model").orEmpty()
    }.getOrNull()

    private fun submitOverlayQuestion(
        question: OverlayQuestion,
        answers: List<List<String>>,
        rejected: Boolean = false,
    ) {
        if (question.taskId in questionSubmittingTaskIds) return
        questionSubmittingTaskIds.add(question.taskId)
        refreshOverlayContent()
        executor.execute {
            val answerJson = JSONArray().apply {
                answers.forEach { answer ->
                    put(JSONArray().apply { answer.forEach(::put) })
                }
            }
            val body = JSONObject()
                .put("request_id", question.requestId)
                .put("answers", answerJson)
                .put("rejected", rejected)
            val result = apiRequest("/api/tasks/${Uri.encode(question.taskId)}/question", "POST", body)
            val failureMessage = lastApiError
            handler.post {
                if (result != null) {
                    toast(if (rejected) "已拒绝问题，Agent 将结束当前任务" else "回答已提交，Agent 继续工作")
                } else {
                    questionSubmittingTaskIds.remove(question.taskId)
                    toast(failureMessage.ifBlank { "回答提交失败，请稍后重试" })
                }
                refreshOverlayContent()
            }
        }
    }

    private fun sendCurrentMessage() {
        if (composing) return toast("消息正在发送")
        val text = input?.text?.toString()?.trim().orEmpty()
        val completion = currentCompletion
        val agent = completion?.let { item ->
            agents.firstOrNull {
                it.online && it.agentId == item.agentId &&
                    (item.machineId.isBlank() || it.machineId == item.machineId)
            }
        }
            ?: agents.firstOrNull { it.online && it.key == selectedKey }
            ?: agents.firstOrNull { it.online }
        if (agent == null) return toast("暂无可用 Agent")
        if (text.isEmpty() && attachments.isEmpty()) return toast("请输入消息或添加附件")
        composing = true
        updateComposingUi()
        executor.execute {
            val task = createTask(agent, text, completion)
            val failureMessage = lastApiError
            handler.post {
                composing = false
                updateComposingUi()
                if (task != null) {
                    val outgoingMessage = text.ifBlank {
                        if (attachments.size == 1) "附件：${attachments.first().name}" else "已发送 ${attachments.size} 个附件"
                    }
                    val taskId = task.optString("task_id")
                    latestSentMessagesByAgentKey[agent.key] = outgoingMessage
                    latestSentStatesByAgentKey[agent.key] = "sent"
                    latestSentTaskIdsByAgentKey[agent.key] = taskId
                    newConversationKeys.remove(agent.key)
                    task.optString("session_id").takeIf { it.isNotBlank() }?.let {
                        latestSessionIdsByAgentKey[agent.key] = it
                    }
                    agents = agents.map { current ->
                        if (current.key == agent.key) current.copy(taskId = taskId) else current
                    }
                    input?.setText("")
                    toast("已发送给 ${agent.name.ifBlank { "Agent" }}")
                    attachments.clear()
                    if (completion != null) {
                        dismissCurrentCompletion()
                        promoteNextCompletion()
                        showQueuePrompt = currentCompletion == null && completions.isEmpty() && deferredCompletions.isNotEmpty()
                    }
                    refreshOverlayContent()
                    scrollConversationToBottom()
                    verifyTaskAfterSend(agent, taskId)
                } else {
                    latestSentMessagesByAgentKey.remove(agent.key)
                    latestSentStatesByAgentKey.remove(agent.key)
                    latestSentTaskIdsByAgentKey.remove(agent.key)
                    toast(failureMessage.ifBlank { "发送失败，请稍后重试" })
                }
            }
        }
    }

    private fun updateComposingUi() {
        sendButton?.apply {
            isEnabled = !composing
            alpha = if (composing) 0.55f else 1f
        }
    }

    private fun verifyTaskAfterSend(agent: AgentState, taskId: String) {
        executor.execute {
            runCatching { TimeUnit.MILLISECONDS.sleep(1800) }
            if (destroyed) return@execute
            val task = apiRequest("/api/tasks/$taskId") ?: return@execute
            handler.post {
                if (!destroyed) applyTaskUpdate(task)
            }
        }
    }

    private fun scrollConversationToBottom() {
        conversationScroll?.post { conversationScroll?.fullScroll(View.FOCUS_DOWN) }
    }

    private fun createTask(agent: AgentState, text: String, continuation: CompletionItem? = null): JSONObject? {
        val parts = JSONArray().apply {
            put(JSONObject().put("type", "text").put("text", text))
            attachments.forEach { attachment ->
                val bytes = runCatching { contentResolver.openInputStream(attachment.uri)?.use { it.readBytes() } }.getOrNull()
                if (bytes != null) {
                    val mime = contentResolver.getType(attachment.uri) ?: "application/octet-stream"
                    val name = attachment.name.ifBlank { "attachment" }
                    put(JSONObject().put("type", "file").put("mime", mime).put("filename", name)
                        .put("url", "data:$mime;base64,${Base64.encodeToString(bytes, Base64.NO_WRAP)}"))
                }
            }
        }
        val permissionMode = getSharedPreferences(FLUTTER_PREFS, MODE_PRIVATE)
            .getString(PERMISSION_MODE_PREF_KEY, "ask")
            .orEmpty()
            .trim()
            .lowercase()
            .let { mode ->
                if (mode == "auto-approve" || mode == "deny") mode else "ask"
            }
        val metadata = JSONObject()
            .put("permission_mode", permissionMode)
        selectedModelsByAgentKey[agent.key].orEmpty().takeIf { it.isNotBlank() }?.let {
            metadata.put("model", it)
        }
        selectedVariantsByAgentKey[agent.key].orEmpty().takeIf { it.isNotBlank() }?.let {
            metadata.put("variant", it)
        }
        val body = JSONObject()
            .put("agent_id", agent.agentId)
            .put("project_id", agent.projectId)
            .put("parts", parts)
            .put("metadata", metadata)
        val sessionId = continuation?.sessionId?.takeIf { it.isNotBlank() }
            ?: latestSessionIdsByAgentKey[agent.key]?.takeIf { agent.key !in newConversationKeys }
        if (!sessionId.isNullOrBlank()) body.put("session_id", sessionId)
        val task = apiRequest("/api/tasks", "POST", body) ?: return null
        if (task.optString("task_id").isBlank()) {
            lastApiError = "服务响应缺少任务编号"
            return null
        }
        return task
    }

    private fun connectEventStream() {
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

    private fun registerNetworkCallback() {
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

    private fun unregisterNetworkCallback() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.N) return
        val callback = networkCallback ?: return
        networkCallback = null
        val manager = getSystemService(ConnectivityManager::class.java) ?: return
        runCatching { manager.unregisterNetworkCallback(callback) }
    }

    private fun readEventStream() {
        val prefs = getSharedPreferences("FlutterSharedPreferences", MODE_PRIVATE)
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

    private fun dispatchStreamEvent(eventType: String, rawData: String) {
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

    @Synchronized
    private fun loadNotificationCursorScope(prefs: android.content.SharedPreferences, base: String) {
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

    @Synchronized
    private fun recordNotificationCursor(cursor: Long) {
        if (cursor < 0 || (notificationCursorInitialized && cursor <= notificationCursor)) return
        val scope = notificationCursorScope
        if (scope.isBlank()) return
        val saved = getSharedPreferences(FLUTTER_PREFS, MODE_PRIVATE)
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

    private fun applyNotificationUpdate(change: JSONObject) {
        if (!backgroundNotificationEnabled) return
        val version = change.optLong("version")
        if (version <= notificationCursor) return
        if (notificationDeliveryBlocked) {
            pendingNotificationChanges[version] = change
            persistPendingNotificationChanges()
            scheduleNotificationRetry()
            Log.w(TAG, "notification delivery blocked; queued change version=$version")
            return
        }
        val operationId = change.optString("operation_id")
        val record = change.optJSONObject("record")
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

    private fun scheduleNotificationRetry() {
        handler.removeCallbacks(notificationRetry)
        handler.postDelayed(notificationRetry, NOTIFICATION_RETRY_DELAY_MS)
    }

    private fun retryPendingNotifications() {
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
            if (notificationDeliveryBlocked) break
        }
        if (pendingNotificationChanges.isNotEmpty()) {
            persistPendingNotificationChanges()
            scheduleNotificationRetry()
        }
    }

    private fun loadPendingNotificationChanges(prefs: android.content.SharedPreferences) {
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

    private fun persistPendingNotificationChanges() {
        val array = JSONArray()
        pendingNotificationChanges.toSortedMap()
            .toList()
            .takeLast(MAX_PENDING_NOTIFICATION_CHANGES)
            .forEach { (version, payload) ->
                array.put(JSONObject().put("version", version).put("payload", payload.toString()))
            }
        getSharedPreferences(FLUTTER_PREFS, MODE_PRIVATE)
            .edit()
            .putString(PENDING_NOTIFICATION_PREF_KEY, array.toString())
            .commit()
    }

    private fun applySnapshot(machines: JSONArray) {
        val next = buildList {
            for (i in 0 until machines.length()) {
                val machine = machines.optJSONObject(i) ?: continue
                val machineId = machine.optString("machine_id")
                val deviceName = machine.optString("display_name")
                    .ifBlank { machine.optString("hostname").ifBlank { machineId } }
                val list = machine.optJSONArray("agents") ?: JSONArray()
                for (j in 0 until list.length()) {
                    parseAgent(list.optJSONObject(j), machineId, deviceName)?.let(::add)
                }
            }
        }
        if (agents == next) {
            updateBubble()
            return
        }
        agents = next
        normalizeOnlineSelection()
        refreshOverlayContent()
    }

    private fun parseAgent(item: JSONObject?, machineIdFallback: String = "", deviceNameFallback: String = ""): AgentState? {
        item ?: return null
        if (item.optString("kind") == "launcher") return null
        val machineId = item.optString("machine_id").ifBlank { machineIdFallback }
        val deviceName = deviceNameFallback.ifBlank { item.optString("hostname").ifBlank { machineId } }
        val project = item.optJSONArray("projects")?.optJSONObject(0)
        val projectId = item.optString("project_id").ifBlank { project?.optString("project_id").orEmpty() }
        val root = item.optString("project_root").ifBlank { project?.optString("root").orEmpty() }
        val fallback = root.replace('\\', '/').substringAfterLast('/').ifBlank { projectId }
        return AgentState(
            machineId, deviceName, item.optString("agent_id"),
            item.optString("name").ifBlank { fallback }, projectId, root,
            item.optString("status", "online"), item.optString("current_task_id"),
        )
    }

    private fun upsertAgent(data: JSONObject) {
        val next = parseAgent(data) ?: return
        val index = agents.indexOfFirst { it.agentId == next.agentId && it.machineId == next.machineId }
        if (index >= 0 && agents[index] == next) {
            updateBubble()
            return
        }
        agents = if (index < 0) agents + next else agents.toMutableList().apply { this[index] = next }
        normalizeOnlineSelection()
        refreshOverlayContent()
    }

    private fun removeAgent(machineId: String, agentId: String) {
        agents = agents.filterNot { it.agentId == agentId && (machineId.isBlank() || it.machineId == machineId) }
        normalizeOnlineSelection()
        refreshOverlayContent()
    }

    private fun normalizeOnlineSelection() {
        if (selectedKey.isNotBlank() && agents.none { it.online && it.key == selectedKey }) selectedKey = ""
        if (conversationKey.isNotBlank() && agents.none { it.online && it.key == conversationKey }) {
            conversationKey = ""
            conversationScrollY = 0
            renderedConversationKey = ""
        }
    }

    private fun questionBelongsToAgent(question: OverlayQuestion, agent: AgentState): Boolean {
        return question.agentKey == agent.key || (
            question.agentId == agent.agentId &&
                (question.machineId.isBlank() || question.machineId == agent.machineId) &&
                (question.projectId.isBlank() || question.projectId == agent.projectId)
            )
    }

    private fun parseOverlayQuestion(
        task: JSONObject,
        matchingAgents: List<AgentState>,
    ): OverlayQuestion? {
        val taskId = task.optString("task_id")
        val question = task.optJSONObject("question") ?: return null
        val requestId = question.optString("request_id")
        if (taskId.isBlank() || requestId.isBlank()) return null
        val items = buildList {
            val questions = question.optJSONArray("questions") ?: JSONArray()
            for (index in 0 until questions.length()) {
                val item = questions.optJSONObject(index) ?: continue
                val options = buildList {
                    val values = item.optJSONArray("options") ?: JSONArray()
                    for (optionIndex in 0 until values.length()) {
                        val option = values.optJSONObject(optionIndex) ?: continue
                        val label = option.optString("label")
                        if (label.isNotBlank()) {
                            add(OverlayQuestionOption(label, option.optString("description")))
                        }
                    }
                }
                add(
                    OverlayQuestionItem(
                        item.optString("question"),
                        item.optString("header"),
                        options,
                        item.optBoolean("multiple", false),
                        item.optBoolean("custom", true),
                    ),
                )
            }
        }
        if (items.isEmpty()) return null
        val machineId = task.optString("machine_id")
        val agentId = task.optString("agent_id")
        val projectId = task.optString("project_id")
        val agentKey = matchingAgents.firstOrNull()?.key
            ?: "$machineId\u0000$agentId\u0000$projectId"
        return OverlayQuestion(
            taskId,
            requestId,
            task.optString("session_id").ifBlank { question.optString("session_id") },
            agentKey,
            agentId,
            machineId,
            projectId,
            items,
        )
    }

    private fun applyTaskUpdate(task: JSONObject) {
        val taskId = task.optString("task_id")
        val agentId = task.optString("agent_id")
        val machineId = task.optString("machine_id")
        val projectId = task.optString("project_id")
        val status = task.optString("status").lowercase()
        Log.i(
            TAG,
            "task update taskId=$taskId status=$status agentId=$agentId " +
                "foreground=${isChatCodexForeground()} seen=${seenCompletionTaskIds.contains(taskId)}",
        )
        val matchingAgents = agents.filter { agent ->
            agent.agentId == agentId &&
                (machineId.isBlank() || agent.machineId == machineId) &&
                (projectId.isBlank() || agent.projectId == projectId)
        }
        val parsedQuestion = parseOverlayQuestion(task, matchingAgents)
        if (parsedQuestion != null) {
            val previous = pendingQuestionsByTaskId[taskId]
            pendingQuestionsByTaskId[taskId] = parsedQuestion
            if (previous?.requestId != parsedQuestion.requestId) {
                questionDraftAnswersByTaskId.remove(taskId)
                questionCustomAnswersByTaskId.remove(taskId)
                questionSubmittingTaskIds.remove(taskId)
            }
        } else if (taskId.isNotBlank() && pendingQuestionsByTaskId.remove(taskId) != null) {
            questionDraftAnswersByTaskId.remove(taskId)
            questionCustomAnswersByTaskId.remove(taskId)
            questionSubmittingTaskIds.remove(taskId)
        }
        val active = status in setOf("pending", "dispatched", "started", "running", "cancelling", "waiting_approval", "question_asked") ||
            parsedQuestion != null
        matchingAgents.forEach { agent ->
            val sentTaskId = latestSentTaskIdsByAgentKey[agent.key]
            if (sentTaskId != null && sentTaskId != taskId) {
                latestSentMessagesByAgentKey.remove(agent.key)
                latestSentStatesByAgentKey.remove(agent.key)
                latestSentTaskIdsByAgentKey.remove(agent.key)
            } else if (sentTaskId == taskId && !active) {
                latestSentMessagesByAgentKey.remove(agent.key)
                latestSentStatesByAgentKey.remove(agent.key)
                latestSentTaskIdsByAgentKey.remove(agent.key)
            }
        }
        agents = agents.map { agent ->
            if (matchingAgents.any { it.key == agent.key }) agent.copy(taskId = if (active) taskId else "") else agent
        }
        var completionAdded = false
        if (status == "completed" && taskId.isNotBlank() && seenCompletionTaskIds.add(taskId)) {
            completionAdded = true
            while (seenCompletionTaskIds.size > 100) seenCompletionTaskIds.remove(seenCompletionTaskIds.first())
            val agentName = agents.firstOrNull { it.agentId == agentId }?.name.orEmpty()
            val result = task.optString("result")
            agents.firstOrNull { it.agentId == agentId }?.let { agent ->
                if (result.isNotBlank()) {
                    latestRepliesByAgentKey[agent.key] = result
                    latestReplyLoadedKeys.add(agent.key)
                }
            }
            val item = CompletionItem(
                if (agentName.isBlank()) "Agent 已完成" else "$agentName 已完成",
                result,
                taskId,
                task.optString("session_id"),
                agentId,
                task.optString("machine_id"),
                task.optString("project_id"),
                task.optString("project_root"),
                result,
            )
            if (expanded && composing) deferredCompletions.addLast(item)
            else if (currentCompletion == null) currentCompletion = item
            else completions.addLast(item)
            Log.i(TAG, "task completion accepted taskId=$taskId summaryLength=${result.length}")
            showQueuePrompt = false
        }
        val conversationOpen = expanded && conversationKey.isNotBlank()
        if (conversationOpen && active && !completionAdded) updateBubble() else refreshOverlayContent()
    }

    private fun dismissCurrentCompletion() {
        currentCompletion = null
    }

    private fun completionBelongsToAgent(item: CompletionItem, agent: AgentState): Boolean =
        item.agentId == agent.agentId &&
            (item.machineId.isBlank() || item.machineId == agent.machineId) &&
            (item.projectId.isBlank() || item.projectId == agent.projectId)

    private fun acknowledgeCompletionFor(agent: AgentState) {
        if (currentCompletion?.let { completionBelongsToAgent(it, agent) } == true) {
            currentCompletion = null
            promoteNextCompletion()
        } else {
            completions.removeAll { completionBelongsToAgent(it, agent) }
            deferredCompletions.removeAll { completionBelongsToAgent(it, agent) }
        }
        showQueuePrompt = currentCompletion == null &&
            completions.isEmpty() &&
            deferredCompletions.isNotEmpty()
        refreshOverlayContent()
    }

    private fun promoteNextCompletion() {
        if (currentCompletion != null) return
        currentCompletion = if (completions.isEmpty()) null else completions.removeFirst()
        currentCompletion?.let {
            selectedKey = agents.firstOrNull { agent -> agent.online && agent.agentId == it.agentId }?.key.orEmpty()
        }
    }

    private fun promoteDeferredCompletion() {
        if (currentCompletion != null || deferredCompletions.isEmpty()) return
        currentCompletion = deferredCompletions.removeFirst()
        currentCompletion?.let {
            selectedKey = agents.firstOrNull { agent -> agent.online && agent.agentId == it.agentId }?.key.orEmpty()
        }
    }

    private fun deferCurrentCompletion() {
        currentCompletion?.let { deferredCompletions.addLast(it) }
        currentCompletion = null
        promoteNextCompletion()
        showQueuePrompt = currentCompletion == null && completions.isEmpty() && deferredCompletions.isNotEmpty()
    }

    private fun refreshOverlayContent() {
        if (expanded) updatePanelContent() else refreshBubble()
    }

    private fun refreshBubble() {
        if (root == null) renderOverlay() else updateBubble()
    }

    private fun updatePanelContent() {
        val panel = panelView ?: return renderOverlay()
        val restoreConversationScroll = renderedConversationKey.isNotBlank() &&
            renderedConversationKey == conversationKey
        val savedScrollY = conversationScrollY
        if (!restoreConversationScroll) conversationScrollY = 0
        val draft = input?.text?.toString().orEmpty()
        panel.removeAllViews()
        populatePanel(panel)
        renderedConversationKey = conversationKey
        input?.setText(draft)
        input?.setSelection(draft.length)
        if (restoreConversationScroll) {
            conversationScroll?.post { conversationScroll?.scrollTo(0, savedScrollY) }
        }
        updateBubble()
    }

    private fun apiRequest(path: String, method: String = "GET", body: JSONObject? = null): JSONObject? =
        runCatching { JSONObject(apiRaw(path, method, body) ?: return null) }.getOrNull()

    private fun apiRaw(path: String, method: String = "GET", body: JSONObject? = null): String? {
        val prefs = getSharedPreferences("FlutterSharedPreferences", MODE_PRIVATE)
        val base = prefs.getString("flutter.base_url", "https://www.xyapi.top/codex").orEmpty().trimEnd('/')
        var token = prefs.getString("flutter.access_token", "").orEmpty()
        lastApiError = ""
        if (base.isBlank() || token.isBlank()) {
            lastApiError = "登录状态已失效，请重新打开应用"
            return null
        }
        var result = performApiRequest(base, path, method, body, token)
        if (result.statusCode == HttpURLConnection.HTTP_UNAUTHORIZED && refreshAccessToken(token)) {
            token = prefs.getString("flutter.access_token", "").orEmpty()
            result = performApiRequest(base, path, method, body, token)
        }
        if (result.statusCode in 200..299) return result.body
        lastApiError = parseApiError(result)
        return null
    }

    private fun performApiRequest(
        base: String,
        path: String,
        method: String,
        body: JSONObject?,
        token: String,
    ): HttpResult {
        val connection = (URL("$base$path").openConnection() as HttpURLConnection).apply {
            requestMethod = method
            connectTimeout = 8000
            readTimeout = 12000
            setRequestProperty("Authorization", "Bearer $token")
            setRequestProperty("Content-Type", "application/json")
            doInput = true
            if (body != null) {
                doOutput = true
                outputStream.use { it.write(body.toString().toByteArray(Charsets.UTF_8)) }
            }
        }
        return try {
            val statusCode = connection.responseCode
            val stream = if (statusCode in 200..299) connection.inputStream else connection.errorStream
            HttpResult(statusCode, stream?.use { BufferedReader(InputStreamReader(it, Charsets.UTF_8)).readText() }.orEmpty())
        } finally {
            connection.disconnect()
        }
    }

    private fun refreshAccessToken(staleToken: String): Boolean = synchronized(tokenRefreshLock) {
        val prefs = getSharedPreferences("FlutterSharedPreferences", MODE_PRIVATE)
        val currentToken = prefs.getString("flutter.access_token", "").orEmpty()
        if (currentToken.isNotBlank() && currentToken != staleToken) return@synchronized true
        val base = prefs.getString("flutter.base_url", "https://www.xyapi.top/codex").orEmpty().trimEnd('/')
        val username = prefs.getString("flutter.username", "").orEmpty()
        val password = prefs.getString("flutter.password", "").orEmpty()
        if (base.isBlank() || username.isBlank() || password.isBlank()) return@synchronized false
        val connection = (URL("$base/api/auth/login").openConnection() as HttpURLConnection).apply {
            requestMethod = "POST"
            connectTimeout = 8000
            readTimeout = 12000
            setRequestProperty("Content-Type", "application/json")
            doInput = true
            doOutput = true
        }
        try {
            val payload = JSONObject().put("username", username).put("password", password)
            connection.outputStream.use { it.write(payload.toString().toByteArray(Charsets.UTF_8)) }
            if (connection.responseCode !in 200..299) return@synchronized false
            val raw = connection.inputStream.use { BufferedReader(InputStreamReader(it, Charsets.UTF_8)).readText() }
            val token = runCatching { JSONObject(raw).optString("access_token") }.getOrDefault("")
            if (token.isBlank()) return@synchronized false
            prefs.edit().putString("flutter.access_token", token).commit()
        } finally {
            connection.disconnect()
        }
    }

    private fun parseApiError(result: HttpResult): String {
        val detail = runCatching { JSONObject(result.body).optString("error") }.getOrDefault("")
        return when {
            detail.isNotBlank() -> "发送失败：$detail"
            result.statusCode == HttpURLConnection.HTTP_UNAUTHORIZED -> "登录状态已失效，请重新打开应用"
            result.statusCode > 0 -> "发送失败（HTTP ${result.statusCode}）"
            else -> "发送失败，请稍后重试"
        }
    }

    private fun agentStatusLabel(agent: AgentState): String = when {
        pendingQuestionsByTaskId.values.any { questionBelongsToAgent(it, agent) } -> "需回答"
        agent.busy -> "工作中"
        agent.online -> "空闲"
        else -> "离线"
    }

    private fun agentStatusColor(agent: AgentState): Int = when {
        pendingQuestionsByTaskId.values.any { questionBelongsToAgent(it, agent) } -> 0xFFB45309.toInt()
        agent.busy -> 0xFF1D4ED8.toInt()
        agent.online -> 0xFF15803D.toInt()
        else -> 0xFF64748B.toInt()
    }

    private fun agentStatusBackground(agent: AgentState): Int = when {
        pendingQuestionsByTaskId.values.any { questionBelongsToAgent(it, agent) } -> 0xFFFFFBEB.toInt()
        agent.busy -> 0xFFEFF6FF.toInt()
        agent.online -> 0xFFECFDF5.toInt()
        else -> 0xFFF1F5F9.toInt()
    }
    private fun toast(message: String) = Toast.makeText(this, message, Toast.LENGTH_SHORT).show()

    private fun showStopConfirmation() {
        if (closeConfirmationDialog?.isShowing == true) return
        lateinit var dialog: AlertDialog
        val content = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(dp(22), dp(20), dp(22), dp(16))
            background = roundedBackground(0xFFFFFFFF.toInt(), 14)
        }
        val header = LinearLayout(this).apply {
            orientation = LinearLayout.HORIZONTAL
            gravity = Gravity.CENTER_VERTICAL
        }
        val icon = ImageView(this).apply {
            setImageResource(android.R.drawable.ic_menu_close_clear_cancel)
            imageTintList = android.content.res.ColorStateList.valueOf(0xFFDC2626.toInt())
            background = roundedBackground(0xFFFEE2E2.toInt(), 10)
            setPadding(dp(10), dp(10), dp(10), dp(10))
        }
        header.addView(icon, LinearLayout.LayoutParams(dp(40), dp(40)))
        val title = TextView(this).apply {
            text = "关闭全局助手？"
            setTextColor(0xFF0F1729.toInt())
            setTextSize(android.util.TypedValue.COMPLEX_UNIT_SP, 17f)
            setTypeface(typeface, Typeface.BOLD)
            setPadding(dp(12), dp(1), 0, 0)
        }
        header.addView(title, LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f))
        content.addView(header)

        val message = TextView(this).apply {
            text = "关闭后将不再显示悬浮图标，也不会在应用启动时自动恢复。"
            setTextColor(0xFF4A5D7A.toInt())
            setTextSize(android.util.TypedValue.COMPLEX_UNIT_SP, 14f)
            setPadding(0, dp(14), 0, 0)
        }
        content.addView(message)

        val actions = LinearLayout(this).apply {
            orientation = LinearLayout.HORIZONTAL
            gravity = Gravity.END
            setPadding(0, dp(20), 0, 0)
        }
        val cancelButton = Button(this).apply {
            text = "取消"
            setAllCaps(false)
            setTextColor(0xFF2563EB.toInt())
            background = roundedBackground(0xFFFFFFFF.toInt(), 8, 0xFFD0DDEF.toInt())
            setPadding(dp(14), 0, dp(14), 0)
            setOnClickListener { dialog.dismiss() }
        }
        val closeButton = Button(this).apply {
            text = "关闭"
            setAllCaps(false)
            setTextColor(0xFFFFFFFF.toInt())
            background = roundedBackground(0xFFDC2626.toInt(), 8)
            setPadding(dp(16), 0, dp(16), 0)
            setOnClickListener {
                dialog.dismiss()
                getSharedPreferences(FLUTTER_PREFS, MODE_PRIVATE)
                    .edit()
                    .putString(ENABLED_PREF_KEY, "false")
                    .apply()
                disableOverlay()
            }
        }
        actions.addView(cancelButton, LinearLayout.LayoutParams(dp(76), dp(40)))
        actions.addView(closeButton, LinearLayout.LayoutParams(dp(76), dp(40)).apply {
            leftMargin = dp(10)
        })
        content.addView(actions)

        dialog = AlertDialog.Builder(this)
            .setView(content)
            .create()
        dialog.window?.setType(
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                WindowManager.LayoutParams.TYPE_APPLICATION_OVERLAY
            } else {
                @Suppress("DEPRECATION")
                WindowManager.LayoutParams.TYPE_PHONE
            },
        )
        dialog.setOnShowListener {
            dialog.window?.setBackgroundDrawableResource(android.R.color.transparent)
            dialog.window?.setLayout(
                (resources.displayMetrics.widthPixels * 0.82f).toInt(),
                WindowManager.LayoutParams.WRAP_CONTENT,
            )
        }
        dialog.setOnDismissListener {
            if (closeConfirmationDialog === dialog) closeConfirmationDialog = null
        }
        closeConfirmationDialog = dialog
        runCatching { dialog.show() }.onFailure {
            closeConfirmationDialog = null
            toast("关闭确认框显示失败，请从应用内关闭")
        }
    }

    private fun roundedBackground(
        fillColor: Int,
        radiusDp: Int,
        strokeColor: Int? = null,
    ): GradientDrawable = GradientDrawable().apply {
        setColor(fillColor)
        cornerRadius = dp(radiusDp).toFloat()
        strokeColor?.let { setStroke(dp(1), it) }
    }

    private inner class DragTouchListener : View.OnTouchListener {
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

    private inner class ResizeTouchListener : View.OnTouchListener {
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

    private fun buildNotification(): Notification = NotificationCompat.Builder(this, CHANNEL_ID)
        .setSmallIcon(R.drawable.ic_overlay_send)
        .setContentTitle(if (overlayEnabled) "全局助手已开启" else "后台任务通知已开启")
        .setContentText(if (overlayEnabled) "悬浮层会显示 Agent 状态和完成消息" else "任务完成后会在通知栏提醒你")
        .setOngoing(true)
        .build()

    private fun notificationsAvailable(): Boolean {
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

    private fun showTaskCompletionNotification(item: CompletionItem): Boolean {
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

    private fun isChatCodexForeground(): Boolean = MainActivity.isAppForeground()

    private fun createNotificationChannel() {
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
                android.provider.Settings.System.DEFAULT_NOTIFICATION_URI,
                android.media.AudioAttributes.Builder()
                    .setUsage(android.media.AudioAttributes.USAGE_NOTIFICATION)
                    .build(),
            )
        }
        getSystemService(NotificationManager::class.java).createNotificationChannel(completionChannel)
    }

    private fun circleBackground(color: Int): android.graphics.drawable.GradientDrawable =
        android.graphics.drawable.GradientDrawable().apply {
            shape = android.graphics.drawable.GradientDrawable.OVAL
            setColor(color)
            setStroke(dp(2), Color.WHITE)
        }

    private fun roundedBackground(fill: Int, stroke: Int, radius: Int): android.graphics.drawable.GradientDrawable =
        android.graphics.drawable.GradientDrawable().apply {
            setColor(fill); setStroke(dp(1), stroke); cornerRadius = dp(radius).toFloat()
        }

    private fun dp(value: Int): Int = (value * resources.displayMetrics.density).toInt()

    private inner class BrandMarkView(context: Context) : View(context) {
        var accentColor: Int = 0xFF2F6BF2.toInt()
            set(value) {
                field = value
                invalidate()
            }
        private val markPaint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
            style = Paint.Style.STROKE
            strokeCap = Paint.Cap.SQUARE
            strokeJoin = Paint.Join.MITER
        }

        override fun onDraw(canvas: Canvas) {
            super.onDraw(canvas)
            markPaint.color = accentColor
            markPaint.strokeWidth = dp(3).toFloat()
            val left = width * 0.28f
            val right = width * 0.70f
            val middle = height * 0.42f
            val upper = height * 0.25f
            val lower = height * 0.59f
            canvas.drawPath(Path().apply {
                moveTo(left, upper)
                lineTo(right, middle)
                lineTo(left, lower)
            }, markPaint)
            markPaint.strokeWidth = dp(2).toFloat()
            canvas.drawLine(width * 0.28f, height * 0.76f, width * 0.72f, height * 0.76f, markPaint)
        }
    }

    private inner class CompletionTickerView(context: Context) : View(context) {
        private val paint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
            typeface = Typeface.DEFAULT_BOLD
            textAlign = Paint.Align.LEFT
        }
        private var tickerText = ""
        private var tickerColor = 0xFF64748B.toInt()
        private var shouldScroll = false
        private var offset = 0f
        private var segmentWidth = 0f
        private var lastFrameNanos = 0L
        private var animationScheduled = false
        private val tick = object : Runnable {
            override fun run() {
                animationScheduled = false
                if (!isAttachedToWindow || !shouldScroll) return
                if (segmentWidth <= 0f) {
                    scheduleNextFrame()
                    return
                }
                if (segmentWidth <= width) return
                val now = System.nanoTime()
                val elapsedSeconds = if (lastFrameNanos == 0L) 0f else {
                    ((now - lastFrameNanos) / 1_000_000_000f).coerceIn(0f, 0.05f)
                }
                lastFrameNanos = now
                offset -= dp(24) * elapsedSeconds
                if (offset <= -segmentWidth) offset += segmentWidth
                postInvalidateOnAnimation()
                scheduleNextFrame()
            }
        }

        private fun scheduleNextFrame() {
            if (animationScheduled || !isAttachedToWindow || !shouldScroll) return
            animationScheduled = true
            postOnAnimation(tick)
        }

        fun setTickerText(value: String, color: Int, scroll: Boolean) {
            val changed = tickerText != value || tickerColor != color || shouldScroll != scroll
            tickerText = value
            tickerColor = color
            shouldScroll = scroll
            if (changed) {
                offset = 0f
                lastFrameNanos = 0L
                segmentWidth = 0f
            }
            removeCallbacks(tick)
            animationScheduled = false
            if (scroll) scheduleNextFrame()
            postInvalidateOnAnimation()
        }

        override fun onAttachedToWindow() {
            super.onAttachedToWindow()
            lastFrameNanos = 0L
            scheduleNextFrame()
        }

        override fun onDetachedFromWindow() {
            removeCallbacks(tick)
            animationScheduled = false
            lastFrameNanos = 0L
            super.onDetachedFromWindow()
        }

        override fun onDraw(canvas: Canvas) {
            super.onDraw(canvas)
            if (tickerText.isBlank()) return
            paint.color = tickerColor
            paint.textSize = dp(6).toFloat()
            val metrics = paint.fontMetrics
            val baseline = height / 2f - (metrics.ascent + metrics.descent) / 2f
            val separator = "   ·   "
            val segment = if (shouldScroll) "$tickerText$separator" else tickerText
            segmentWidth = paint.measureText(segment)
            canvas.save()
            canvas.clipRect(0f, 0f, width.toFloat(), height.toFloat())
            if (shouldScroll && segmentWidth > width) {
                canvas.drawText(segment, offset, baseline, paint)
                canvas.drawText(segment, offset + segmentWidth, baseline, paint)
            } else {
                val x = (width - paint.measureText(tickerText)) / 2f
                canvas.drawText(tickerText, x.coerceAtLeast(0f), baseline, paint)
            }
            canvas.restore()
        }
    }

    private inner class AttachmentIconView(context: Context) : View(context) {
        private val paint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
            color = 0xFF4A5D7A.toInt()
            style = Paint.Style.STROKE
            strokeWidth = dp(2).toFloat()
            strokeCap = Paint.Cap.ROUND
            strokeJoin = Paint.Join.ROUND
        }

        override fun onDraw(canvas: Canvas) {
            super.onDraw(canvas)
            val w = width.toFloat()
            val h = height.toFloat()
            canvas.drawPath(Path().apply {
                moveTo(w * 0.68f, h * 0.20f)
                cubicTo(w * 0.84f, h * 0.04f, w * 1.02f, h * 0.24f, w * 0.86f, h * 0.40f)
                lineTo(w * 0.48f, h * 0.78f)
                cubicTo(w * 0.34f, h * 0.92f, w * 0.12f, h * 0.76f, w * 0.28f, h * 0.60f)
                lineTo(w * 0.62f, h * 0.26f)
                cubicTo(w * 0.70f, h * 0.18f, w * 0.82f, h * 0.30f, w * 0.74f, h * 0.38f)
                lineTo(w * 0.44f, h * 0.68f)
            }, paint)
        }
    }

    private inner class ResizeHandleView(context: Context) : View(context) {
        private val paint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
            color = 0xFF7AA7E8.toInt()
            strokeWidth = dp(2).toFloat()
            strokeCap = Paint.Cap.ROUND
        }

        override fun onDraw(canvas: Canvas) {
            super.onDraw(canvas)
            val right = width - dp(4).toFloat()
            val bottom = height - dp(3).toFloat()
            for (offset in 0..2) {
                val length = dp(5 + offset * 5).toFloat()
                canvas.drawLine(right - length, bottom, right, bottom - length, paint)
            }
        }
    }
}
