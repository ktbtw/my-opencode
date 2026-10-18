package com.chatcodex.chat_codex_app.overlay

import android.graphics.PixelFormat
import android.util.Log
import android.os.Build
import android.view.Gravity
import android.view.View
import android.view.WindowManager
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
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
        FloatingMode.WINDOW -> {
            val width = dp(360).coerceAtMost(metrics.widthPixels - dp(24))
            val height = dp(560).coerceAtMost(metrics.heightPixels - dp(120))
            width to height
        }
        FloatingMode.FULLSCREEN -> metrics.widthPixels to metrics.heightPixels
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
    // 全程 NOT_FOCUSABLE：悬浮层不需要窗口焦点，聚焦反而会抢走 App 的输入焦点。
    val flags = WindowManager.LayoutParams.FLAG_NOT_FOCUSABLE or
        WindowManager.LayoutParams.FLAG_NOT_TOUCH_MODAL
    return WindowManager.LayoutParams(
        width,
        height,
        type,
        flags,
        PixelFormat.TRANSLUCENT,
    ).apply {
        gravity = Gravity.TOP or Gravity.START
        val metrics = resources.displayMetrics
        when (mode) {
            FloatingMode.BALL -> {
                x = if (bubbleX >= 0) bubbleX else metrics.widthPixels - width - dp(8)
                y = if (bubbleY >= 0) bubbleY else metrics.heightPixels / 2
            }
            FloatingMode.WINDOW, FloatingMode.RESULT_DISPLAY -> {
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
                colorScheme = darkColorScheme(
                    primary = Color(0xFF0A84FF),
                    background = Color(0xFF0B1020),
                    surface = Color(0xFF111A30),
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
                    onNewSession = { startComposeNewSession() },
                    onExpand = { applyComposeMode(FloatingMode.FULLSCREEN) },
                    onCollapse = { applyComposeMode(FloatingMode.BALL) },
                    onBallTap = { applyComposeMode(FloatingMode.WINDOW) },
                )
            }
        }
    }
    val container = android.widget.FrameLayout(this).apply {
        // ComposeView 会沿父链查找 owner，容器上也设一份更稳妥。
        setViewTreeLifecycleOwner(lifecycleOwner)
        setViewTreeSavedStateRegistryOwner(lifecycleOwner)
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
    sendTextMessage(text)
}

/** 把服务侧状态同步进 Compose 状态。 */
private fun GlobalOverlayService.syncComposeStateFromService(state: OverlayUiState) {
    state.agents = agents.map {
        com.chatcodex.chat_codex_app.overlay.compose.AgentUi(
            agentId = it.agentId,
            name = it.name,
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
    state.sessions = (sessionsByAgentKey[activeKey] ?: emptyList()).map {
        com.chatcodex.chat_codex_app.overlay.compose.SessionUi(
            sessionId = it.sessionId,
            summary = it.summary,
            updatedAt = it.updatedAt,
        )
    }
    state.currentSessionId = conversationKey
    state.modelLabel = activeAgent?.let { agent ->
        runCatching { selectedModelFor(agent)?.name.orEmpty() }.getOrDefault("")
    }.orEmpty()
}

/** 切换历史会话。 */
internal fun GlobalOverlayService.selectComposeSession(sessionId: String) {
    val agent = agents.firstOrNull { it.key == conversationKey }
        ?: agents.firstOrNull { it.online }
        ?: return
    val session = (sessionsByAgentKey[agent.key] ?: emptyList())
        .firstOrNull { it.sessionId == sessionId } ?: return
    selectSession(agent, session)
    syncComposeState()
}

/** 开始新对话。 */
internal fun GlobalOverlayService.startComposeNewSession() {
    val agent = agents.firstOrNull { it.key == conversationKey }
        ?: agents.firstOrNull { it.online }
        ?: return
    startNewConversation(agent)
    syncComposeState()
}
