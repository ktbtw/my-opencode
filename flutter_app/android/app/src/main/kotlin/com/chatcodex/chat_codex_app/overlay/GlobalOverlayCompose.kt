package com.chatcodex.chat_codex_app.overlay

import android.graphics.PixelFormat
import android.util.Log
import android.os.Build
import android.view.Gravity
import android.view.KeyEvent
import android.view.View
import android.view.WindowManager
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.ComposeView
import androidx.compose.ui.platform.ViewCompositionStrategy
import com.chatcodex.chat_codex_app.overlay.compose.OverlayLifecycleOwner
import androidx.savedstate.setViewTreeSavedStateRegistryOwner
import androidx.lifecycle.setViewTreeLifecycleOwner
import com.chatcodex.chat_codex_app.overlay.compose.CompletionUi
import com.chatcodex.chat_codex_app.overlay.compose.FloatingMode
import com.chatcodex.chat_codex_app.overlay.compose.FloatingOverlayRoot
import com.chatcodex.chat_codex_app.overlay.compose.OverlayUiState

/** 悬浮层 UI 状态（服务侧直接改字段，Compose 自动重组）。 */
internal fun GlobalOverlayService.syncComposeState() {
    syncComposeStateFromService(ensureComposeState())
}

internal val GlobalOverlayService.overlayLifecycle: OverlayLifecycleOwner
    get() {
        val existing = overlayLifecycleOwner
        if (existing != null) return existing
        val created = OverlayLifecycleOwner().also { it.attach() }
        overlayLifecycleOwner = created
        return created
    }

internal fun GlobalOverlayService.ensureComposeState(): OverlayUiState {
    val existing = composeUiState
    if (existing != null) return existing
    val created = OverlayUiState()
    composeUiState = created
    return created
}

/** 按当前模式计算悬浮窗尺寸（px）。 */
private fun GlobalOverlayService.composeWindowSize(mode: FloatingMode): Pair<Int, Int> {
    val metrics = resources.displayMetrics
    return when (mode) {
        FloatingMode.BALL -> dp(168) to dp(168)
        FloatingMode.VOICE_BALL -> dp(240) to dp(240)
        FloatingMode.SCREEN_OCR -> {
            val width = dp(340).coerceAtMost(metrics.widthPixels - dp(24))
            val height = dp(480).coerceAtMost(metrics.heightPixels - dp(120))
            width to height
        }
        FloatingMode.WINDOW -> {
            val width = dp(360).coerceAtMost(metrics.widthPixels - dp(24))
            val height = dp(560).coerceAtMost(metrics.heightPixels - dp(120))
            width to height
        }
        FloatingMode.FULLSCREEN -> {
            val statusBar = resources.getIdentifier("status_bar_height", "dimen", "android")
                .let { if (it > 0) resources.getDimensionPixelSize(it) else 0 }
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
                // 沉浸层要盖住状态栏（Operit 也这么做，内容自己按 status bar 高度让位），
                // 配合下面 fitInsetsTypes = 0，窗口才能真正顶到屏幕最上面。
                metrics.widthPixels to metrics.heightPixels
            } else {
                metrics.widthPixels to (metrics.heightPixels - statusBar)
            }
        }
        FloatingMode.RESULT_DISPLAY -> dp(340) to dp(200)
    }
}

private fun GlobalOverlayService.composeLayoutParams(mode: FloatingMode): WindowManager.LayoutParams {
    val (width, height) = composeWindowSize(mode)
    val type = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
        WindowManager.LayoutParams.TYPE_APPLICATION_OVERLAY
    } else {
        @Suppress("DEPRECATION")
        WindowManager.LayoutParams.TYPE_PHONE
    }
    // 面板里有输入框，窗口必须真聚焦才能拿到 IME 目标：
    // NOT_FOCUSABLE 会拒绝键盘，NOT_FOCUSABLE|ALT_FOCUSABLE_IM 实测也弹不出来。
    // 球 / 语音球 / 结果卡保持 NOT_FOCUSABLE，避免平时无谓地抢走 App 焦点。
    val needsInputFocus = mode == FloatingMode.WINDOW || mode == FloatingMode.FULLSCREEN
    var flags = WindowManager.LayoutParams.FLAG_NOT_TOUCH_MODAL or
        (if (needsInputFocus) 0 else WindowManager.LayoutParams.FLAG_NOT_FOCUSABLE)
    // 注意：APPLICATION_OVERLAY 上 FLAG_LAYOUT_IN_SCREEN 不生效，窗口仍从状态栏下方开始，
    // 全屏尺寸要自己减掉状态栏高度。
    return WindowManager.LayoutParams(
        width,
        height,
        type,
        flags,
        PixelFormat.TRANSLUCENT,
    ).apply {
        gravity = Gravity.TOP or Gravity.START
        if (needsInputFocus) {
            // 悬浮窗上 ADJUST_RESIZE / ADJUST_PAN 都不生效（实测窗口纹丝不动），
            // 由 setTypingLayout / 沉浸页自己让位。
            softInputMode = WindowManager.LayoutParams.SOFT_INPUT_ADJUST_NOTHING
        }
        if (mode == FloatingMode.FULLSCREEN && Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
            // 只忽略状态栏/导航栏 inset（否则 APPLICATION_OVERLAY 永远被摆在状态栏下面），
            // 但必须保留 IME inset：全部置 0 时输入法窗口会被摆到屏幕上半部、键盘根本不出现。
            fitInsetsTypes = android.view.WindowInsets.Type.ime()
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
                layoutInDisplayCutoutMode =
                    WindowManager.LayoutParams.LAYOUT_IN_DISPLAY_CUTOUT_MODE_ALWAYS
            }
        }
        if (mode == FloatingMode.FULLSCREEN && Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            // 真背景模糊：blurBehindRadius 需要配套 FLAG_BLUR_BEHIND 才会被 SurfaceFlinger 采纳。
            runCatching {
                // 注意：这里必须改 LayoutParams 自己的 flags，外层那个局部变量只是构造参数。
                @Suppress("DEPRECATION")
                this.flags = this.flags or WindowManager.LayoutParams.FLAG_BLUR_BEHIND
                // 36dp(108px) 糊得太狠：浅色界面被糊成一片均匀灰，完全没有「透」的感觉。
                // 16dp(48px) 还能看出后面卡片/色块的轮廓，磨砂才像玻璃。
                setBlurBehindRadius(dp(16))
            }
            Log.i(TAG, "blur behind=$blurBehindRadius flags=$flags")
        }
        val metrics = resources.displayMetrics
        when (mode) {
            FloatingMode.BALL -> {
                x = if (bubbleX >= 0) bubbleX else metrics.widthPixels - width - dp(8)
                y = if (bubbleY >= 0) bubbleY else metrics.heightPixels / 2
            }
            FloatingMode.WINDOW, FloatingMode.RESULT_DISPLAY, FloatingMode.VOICE_BALL,
            FloatingMode.SCREEN_OCR -> {
                x = if (panelX >= 0) panelX else (metrics.widthPixels - width) / 2
                y = if (panelY >= 0) panelY else (metrics.heightPixels - height) / 2
            }
            FloatingMode.FULLSCREEN -> {
                x = 0
                y = 0
            }
        }
    }
}

/** 用 Compose 重建悬浮层（替代原来的 View 拼装实现）。 */
internal fun GlobalOverlayService.renderComposeOverlay() {
    if (stopping || destroyed || !overlayEnabled) return
    val state = ensureComposeState()
    syncComposeStateFromService(state)
    val mode = state.mode
    val params = composeLayoutParams(mode)
    root?.let { runCatching { windowManager.removeView(it) } }
    panelView = null
    sendButton = null
    val lifecycleOwner = overlayLifecycle
    val view = ComposeView(this).apply {
        setViewCompositionStrategy(ViewCompositionStrategy.DisposeOnDetachedFromWindow)
        setViewTreeLifecycleOwner(lifecycleOwner)
        setViewTreeSavedStateRegistryOwner(lifecycleOwner)
        setContent {
            MaterialTheme(
                colorScheme = lightColorScheme(
                    primary = Color(0xFF2563EB),
                    onPrimary = Color(0xFFFFFFFF),
                    background = Color(0xFFF2F6FC),
                    onBackground = Color(0xFF0F1729),
                    surface = Color(0xFFFFFFFF),
                    onSurface = Color(0xFF0F1729),
                ),
            ) {
                FloatingOverlayRoot(
                    state = state,
                    onMove = { dx, dy -> moveComposeOverlay(params, dx, dy) },
                    onDragStart = { },
                    onDragEnd = {
                        if (state.mode == FloatingMode.BALL) {
                            bubbleX = params.x
                            bubbleY = params.y
                            saveOverlayGeometry()
                        } else {
                            panelX = params.x
                            panelY = params.y
                            saveOverlayGeometry()
                        }
                    },
                    onSend = { text -> sendComposeMessage(text) },
                    onSelectSession = { sessionId -> selectComposeSession(sessionId) },
                    onSelectAgent = { key -> selectComposeAgent(key) },
                    onTypingChanged = { typing -> setTypingLayout(typing) },
                    onNewSession = { startComposeNewSession() },
                    onExpand = { applyComposeMode(FloatingMode.FULLSCREEN) },
                    onCollapseToWindow = { applyComposeMode(FloatingMode.WINDOW) },
                    onCollapse = { applyComposeMode(FloatingMode.BALL) },
                    onBallTap = { applyComposeMode(FloatingMode.WINDOW) },
                    onAttach = { pickAttachment() },
                    onVoiceStart = { startVoiceBall() },
                    onVoiceStop = { stopVoiceBall() },
                    onOcrStart = { startScreenOcr() },
                    onOcrClose = { closeScreenOcr() },
                    onOcrSend = { sendScreenOcrText() },
                    onRemoveAttachment = { index -> removeAttachment(index) },
                )
            }
        }
    }
    val container = OverlayRootLayout(this).apply {
        // ComposeView 会沿父链查找 owner，容器上也设一份更稳妥。
        setViewTreeLifecycleOwner(lifecycleOwner)
        setViewTreeSavedStateRegistryOwner(lifecycleOwner)
        onBackPressed = { handleOverlayBack() }
        addView(
            view,
            android.widget.FrameLayout.LayoutParams(
                android.widget.FrameLayout.LayoutParams.MATCH_PARENT,
                android.widget.FrameLayout.LayoutParams.MATCH_PARENT,
            ),
        )
    }
    root = container
    windowParams = params
    runCatching { windowManager.addView(container, params) }
    container.alpha = 0f
    container.animate().alpha(1f).setDuration(180L).start()
    Log.i(TAG, "compose overlay rendered mode=$mode size=${params.width}x${params.height}")
}

/** 模式切换时重建窗口（尺寸/焦点标记都不同）。 */
internal fun GlobalOverlayService.applyComposeMode(mode: FloatingMode) {
    val state = ensureComposeState()
    state.switchTo(mode)
    // 同步 expanded，否则后续内容更新会把球重新弹回窗口。
    expanded = mode != FloatingMode.BALL
    if (mode != FloatingMode.WINDOW) {
        // 离开面板时清掉打字布局的还原点，避免下次进入时还原到错误位置。
        imeRestoreY = -1
        imeRestoreHeight = 0
    }
    if (mode == FloatingMode.WINDOW) {
        // 打开面板时补一次历史会话，否则会话条里永远只有「新对话」。
        val agent = agents.firstOrNull { it.key == conversationKey }
            ?: agents.firstOrNull { it.online }
        if (agent != null) {
            // 先锚定会话 key：ensureSessionsLoaded 的回调只在 conversationKey 匹配时
            // 才把结果同步进 Compose 状态，不锚定的话历史浮层第一次打开永远是空的。
            conversationKey = agent.key
            selectedKey = agent.key
            ensureSessionsLoaded(agent)
            ensureLatestReplyLoaded(agent)
            updatePanelContent()
        }
    }
    renderComposeOverlay()
}

private fun GlobalOverlayService.moveComposeOverlay(
    params: WindowManager.LayoutParams,
    dx: Float,
    dy: Float,
) {
    val metrics = resources.displayMetrics
    val maxX = (metrics.widthPixels - params.width).coerceAtLeast(0)
    val maxY = (metrics.heightPixels - params.height).coerceAtLeast(0)
    params.x = (params.x + dx.toInt()).coerceIn(0, maxX)
    params.y = (params.y + dy.toInt()).coerceIn(0, maxY)
    root?.let { runCatching { windowManager.updateViewLayout(it, params) } }
}

private fun GlobalOverlayService.sendComposeMessage(text: String) {
    val state = ensureComposeState()
    state.messages = state.messages + com.chatcodex.chat_codex_app.overlay.compose.ChatUi("user", text)
    // 发完就把面板放回原位，别一直停在打字位置。
    setTypingLayout(false)
    sendTextMessage(text)
}

/** 把服务侧状态同步进 Compose 状态。 */
private fun GlobalOverlayService.syncComposeStateFromService(state: OverlayUiState) {
    state.agents = agents.map {
        com.chatcodex.chat_codex_app.overlay.compose.AgentUi(
            key = it.key,
            agentId = it.agentId,
            name = it.name,
            device = it.deviceName,
            project = it.projectId,
            online = it.online,
            busy = it.taskId.isNotBlank(),
        )
    }
    state.busy = agents.any { it.taskId.isNotBlank() }
    val completion = currentCompletion
    state.completion = completion?.let {
        CompletionUi(it.title, it.summary, it.taskId)
    }
    // 当前会话对应的 Agent 与历史会话列表
    val activeKey = conversationKey.ifBlank {
        agents.firstOrNull { it.online }?.key.orEmpty()
    }
    val activeAgent = agents.firstOrNull { it.key == activeKey }
    state.activeAgentKey = activeKey
    state.sessions = (sessionsByAgentKey[activeKey] ?: emptyList()).map {
        com.chatcodex.chat_codex_app.overlay.compose.SessionUi(
            sessionId = it.sessionId,
            summary = it.summary,
            updatedAt = it.updatedAt,
        )
    }
    // 会话 chip 的选中态必须按 sessionId 走；以前这里塞的是 Agent key，永远匹配不上。
    state.currentSessionId = latestSessionIdsByAgentKey[activeKey].orEmpty()
    state.attachments = attachments.map {
        com.chatcodex.chat_codex_app.overlay.compose.AttachmentUi(it.name)
    }
    state.modelLabel = activeAgent?.let { agent ->
        runCatching { selectedModelFor(agent)?.name.orEmpty() }.getOrDefault("")
    }.orEmpty()
    state.activeBusy = activeAgent?.taskId?.isNotBlank() == true
}

/** 切换历史会话。 */
internal fun GlobalOverlayService.selectComposeSession(sessionId: String) {
    val agent = agents.firstOrNull { it.key == conversationKey }
        ?: agents.firstOrNull { it.online }
        ?: return
    val session = (sessionsByAgentKey[agent.key] ?: emptyList())
        .firstOrNull { it.sessionId == sessionId } ?: return
    // 选会话前先锚定 conversationKey：否则 ensureSessionLatestReplyLoaded 的
    // 回调会因为 conversationKey 为空而丢弃回复，切了会话还是空白。
    conversationKey = agent.key
    selectedKey = agent.key
    selectSession(agent, session)
    val state = ensureComposeState()
    state.messages = emptyList()
    state.input = ""
    state.currentSessionId = session.sessionId
    updatePanelContent()
}

/** 开始新对话。 */
internal fun GlobalOverlayService.startComposeNewSession() {
    val agent = agents.firstOrNull { it.key == conversationKey }
        ?: agents.firstOrNull { it.online }
        ?: return
    conversationKey = agent.key
    selectedKey = agent.key
    startNewConversation(agent)
    val state = ensureComposeState()
    state.messages = emptyList()
    state.input = ""
    state.currentSessionId = ""
    updatePanelContent()
}

/** 悬浮窗内切换 Agent：改选中项、清掉上一位 Agent 的本地消息，再拉会话列表。 */
internal fun GlobalOverlayService.selectComposeAgent(key: String) {
    if (key.isBlank() || key == conversationKey) return
    val agent = agents.firstOrNull { it.key == key } ?: return
    selectedKey = key
    conversationKey = key
    val state = ensureComposeState()
    state.messages = emptyList()
    state.input = ""
    ensureSessionsLoaded(agent)
    ensureLatestReplyLoaded(agent)
    updatePanelContent()
}

/** 返回键：全屏退回悬浮窗，其余形态退回球。 */
internal fun GlobalOverlayService.handleOverlayBack() {
    when (composeUiState?.mode) {
        FloatingMode.FULLSCREEN -> applyComposeMode(FloatingMode.WINDOW)
        FloatingMode.BALL -> Unit
        null -> Unit
        else -> applyComposeMode(FloatingMode.BALL)
    }
}

/**
 * 悬浮窗根布局。
 *
 * 面板为了弹键盘必须可聚焦，可聚焦后返回键会被这个没有 Activity 的窗口吃掉。
 * 在根 View 的 dispatchKeyEvent 里先截一层，把返回键变成「收起面板」，
 * 否则面板一开着，返回键就是死的。
 */
private class OverlayRootLayout(context: android.content.Context) : android.widget.FrameLayout(context) {
    var onBackPressed: (() -> Unit)? = null
    private var backPressedDown = false

    override fun dispatchKeyEvent(event: KeyEvent): Boolean {
        if (event.keyCode == KeyEvent.KEYCODE_BACK) {
            when (event.action) {
                KeyEvent.ACTION_DOWN -> {
                    backPressedDown = true
                    return true
                }
                KeyEvent.ACTION_UP -> {
                    if (backPressedDown) {
                        backPressedDown = false
                        onBackPressed?.invoke()
                        return true
                    }
                }
            }
        }
        return super.dispatchKeyEvent(event)
    }
}


/**
 * 打字布局：输入框获得焦点时把面板整体顶到屏幕上半区并压矮，
 * 输入框就会落在键盘上方；失焦后还原原来的位置和高度。
 *
 * 悬浮窗不是应用主窗口，ADJUST_RESIZE / ADJUST_PAN 都不会动它，
 * IME inset 也拿不到（实测 ime=0），所以这里按屏幕比例直接让位。
 */
internal fun GlobalOverlayService.setTypingLayout(active: Boolean) {
    val params = windowParams ?: return
    if (composeUiState?.mode != FloatingMode.WINDOW) return
    val screenHeight = resources.displayMetrics.heightPixels
    if (active) {
        if (imeRestoreY < 0) {
            imeRestoreY = params.y
            imeRestoreHeight = params.height
        }
        val targetHeight = (screenHeight * 0.52f).toInt().coerceAtMost(imeRestoreHeight)
        val targetY = dp(12)
        if (params.height == targetHeight && params.y == targetY) return
        params.height = targetHeight
        params.y = targetY
        root?.let { runCatching { windowManager.updateViewLayout(it, params) } }
    } else {
        if (imeRestoreY < 0) return
        params.y = imeRestoreY
        params.height = imeRestoreHeight
        imeRestoreY = -1
        imeRestoreHeight = 0
        root?.let { runCatching { windowManager.updateViewLayout(it, params) } }
    }
}
