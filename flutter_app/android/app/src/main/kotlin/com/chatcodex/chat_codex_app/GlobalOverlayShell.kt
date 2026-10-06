package com.chatcodex.chat_codex_app

import android.content.Context
import android.graphics.PixelFormat
import android.graphics.Typeface
import android.os.Build
import android.view.Gravity
import android.view.View
import android.view.WindowManager
import android.view.animation.DecelerateInterpolator
import android.widget.FrameLayout
import android.widget.LinearLayout
import android.widget.ScrollView
import android.widget.TextView

/**
 * 悬浮助手的外壳层：悬浮球与展开面板的创建、几何位置读写、以及整体刷新调度。
 *
 * 这里决定当前显示「悬浮球」还是「完整面板」，并把面板内容交给
 * populatePanel 按当前状态分派到对话 / 模型选择 / 历史会话等子面板。
 */

/** 重新构建并挂载悬浮窗（悬浮球或面板），并播放开启动画。 */
internal fun GlobalOverlayService.renderOverlay() {
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
        container.addView(bubble, FrameLayout.LayoutParams(dp(116), dp(48)).apply {
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
    val contentWidth = if (expanded) panelWidthPx + dp(8) else dp(122)
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
    // Animate only the newly opened surface, never the streaming content.
    val surface = container.getChildAt(0)
    surface?.apply {
        pivotX = if (params.x > resources.displayMetrics.widthPixels / 2) {
            (if (expanded) panelWidthPx else dp(116)).toFloat()
        } else 0f
        pivotY = 0f
        alpha = 0f
        scaleX = if (expanded) .94f else .88f
        scaleY = scaleX
        animate().alpha(1f).scaleX(1f).scaleY(1f)
            .setDuration(if (expanded) 260L else 200L)
            .setInterpolator(DecelerateInterpolator(1.6f)).start()
    }
}

/** 从偏好读取面板尺寸与位置。 */
internal fun GlobalOverlayService.restoreOverlayGeometry() {
    val prefs = getSharedPreferences(GEOMETRY_PREFS, Context.MODE_PRIVATE)
    val maxPanelWidth = (resources.displayMetrics.widthPixels - dp(24)).coerceAtLeast(dp(300))
    val maxPanelHeight = (resources.displayMetrics.heightPixels - dp(120)).coerceAtLeast(dp(380))
    panelWidthPx = dp(prefs.getInt("panel_width_dp", 360)).coerceIn(dp(300), maxPanelWidth)
    panelHeightPx = dp(prefs.getInt("panel_height_dp", 600)).coerceIn(dp(380), maxPanelHeight)
    panelX = prefs.getInt("panel_x", -1)
    panelY = prefs.getInt("panel_y", -1)
    bubbleX = prefs.getInt("bubble_x", -1)
    bubbleY = prefs.getInt("bubble_y", -1)
}

/** 把面板尺寸与位置写回偏好。 */
internal fun GlobalOverlayService.saveOverlayGeometry() {
    val density = resources.displayMetrics.density.coerceAtLeast(1f)
    getSharedPreferences(GEOMETRY_PREFS, Context.MODE_PRIVATE).edit()
        .putInt("panel_width_dp", (panelWidthPx / density + 0.5f).toInt())
        .putInt("panel_height_dp", (panelHeightPx / density + 0.5f).toInt())
        .putInt("panel_x", panelX)
        .putInt("panel_y", panelY)
        .putInt("bubble_x", bubbleX)
        .putInt("bubble_y", bubbleY)
        .apply()
}

/** 构建收起状态下的悬浮球。 */
internal fun GlobalOverlayService.buildBubble(): View {
    val wrapper = FrameLayout(this).apply {
        setPadding(dp(8), dp(4), dp(8), dp(4))
        elevation = dp(3).toFloat()
        clipToOutline = true
        isFocusable = true
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
        wrapper.addView(it, FrameLayout.LayoutParams(dp(28), dp(28)).apply {
            gravity = Gravity.START or Gravity.CENTER_VERTICAL
        })
    }
    bubbleSummary = TextView(this).apply {
        textSize = 12f
        typeface = Typeface.create("sans-serif-medium", Typeface.NORMAL)
        includeFontPadding = false
        gravity = Gravity.CENTER_VERTICAL
        maxLines = 1
        ellipsize = android.text.TextUtils.TruncateAt.END
        importantForAccessibility = View.IMPORTANT_FOR_ACCESSIBILITY_NO
    }.also { wrapper.addView(it, FrameLayout.LayoutParams(-1, -1).apply {
        leftMargin = dp(36)
    }) }
    return wrapper
}

/** 根据当前状态刷新悬浮球的文案与颜色。 */
internal fun GlobalOverlayService.updateBubble() {
    val completion = currentCompletion ?: completions.firstOrNull() ?: deferredCompletions.firstOrNull()
    val pendingQuestionCount = pendingQuestionsByTaskId.size
    val hasQuestion = pendingQuestionCount > 0
    val hasMessage = completion != null && !hasQuestion
    val busy = agents.count { it.online && it.busy }
    val stateColor = when {
        hasQuestion -> 0xFF94651D.toInt()
        hasMessage -> 0xFF287850.toInt()
        !streamConnected -> 0xFF778294.toInt()
        busy > 0 -> 0xFF3666B9.toInt()
        else -> 0xFF28374D.toInt()
    }
    val stateLabel = when {
        hasQuestion -> "$pendingQuestionCount 待回答"
        hasMessage -> "新回复"
        !streamConnected -> "连接中"
        busy > 0 -> "$busy 工作中"
        else -> "助手"
    }
    bubbleContainer?.apply {
        background = touchSurface(0xFFF7FAFF.toInt(), 24)
        contentDescription = "全局助手，$stateLabel，点击展开，长按关闭"
    }
    bubbleBrand?.accentColor = stateColor
    bubbleSummary?.apply {
        setTextColor(stateColor)
        if (text.toString() != stateLabel) {
            text = stateLabel
            alpha = .4f
            animate().alpha(1f).setDuration(180L).start()
        }
    }
    GlobalOverlayEventBridge.emit(statusSnapshot())
}

/** 对外（Flutter 侧）暴露的运行时状态快照。 */
internal fun GlobalOverlayService.statusSnapshot(): Map<String, Any> = mapOf(
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

/** 构建展开状态下的主面板容器。 */
internal fun GlobalOverlayService.buildPanel(): LinearLayout {
    val panel = LinearLayout(this).apply {
        orientation = LinearLayout.VERTICAL
        setPadding(dp(16), dp(14), dp(16), dp(8))
        background = roundedBackground(0xFFFAFBFD.toInt(), 0xFFE4E8EF.toInt(), 24)
        elevation = dp(4).toFloat()
        clipToOutline = true
    }
    panelView = panel
    populatePanel(panel)
    return panel
}

/** 按当前状态把内容填充进面板（模型选择 / 历史会话 / 对话 / Agent 列表）。 */
internal fun GlobalOverlayService.populatePanel(panel: LinearLayout) {
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
    header.addView(BrandMarkView(this).apply {
        accentColor = 0xFFE2E8F0.toInt()
        background = roundedBackground(0xFF202734.toInt(), 12)
        contentDescription = "移动全局助手面板"
        setOnTouchListener(DragTouchListener())
    }, LinearLayout.LayoutParams(dp(38), dp(38)))
    header.addView(LinearLayout(this).apply {
        orientation = LinearLayout.VERTICAL
        gravity = Gravity.CENTER_VERTICAL
        setPadding(dp(10), 0, 0, 0)
        contentDescription = "Agent 工作台，拖动移动面板"
        setOnTouchListener(DragTouchListener())
        addView(TextView(this@populatePanel).apply {
            text = "Agent 工作台"
            textSize = 17f
            typeface = Typeface.create("sans-serif-medium", Typeface.NORMAL)
            setTextColor(0xFF202734.toInt())
            includeFontPadding = false
        })
        addView(TextView(this@populatePanel).apply {
            text = if (streamConnected) "实时连接 · 随时继续对话" else "正在重新连接…"
            textSize = 11f
            setTextColor(0xFF778294.toInt())
            setPadding(0, dp(4), 0, 0)
        })
    }, LinearLayout.LayoutParams(0, dp(52), 1f))
    header.addView(menuIcon(R.drawable.ic_overlay_collapse, "收起悬浮面板") {
        expanded = false
        renderOverlay()
    }, LinearLayout.LayoutParams(dp(44), dp(44)))
    panel.addView(header)
    panel.addView(LinearLayout(this).apply {
        gravity = Gravity.CENTER_VERTICAL
        setPadding(0, dp(16), 0, dp(12))
        addView(TextView(this@populatePanel).apply {
            text = "在线项目"
            textSize = 12f
            typeface = Typeface.create("sans-serif-medium", Typeface.NORMAL)
            setTextColor(0xFF687589.toInt())
        }, LinearLayout.LayoutParams(0, -2, 1f))
        addView(TextView(this@populatePanel).apply {
            val online = agents.filter { it.online }
            text = "${online.size} 个在线  /  ${online.count { it.busy }} 个工作中"
            textSize = 11f
            setTextColor(0xFF778294.toInt())
        })
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
        onlineAgents.forEach { list.addView(buildAgentRow(it)) }
    }
    scroll.addView(list)
    panel.addView(scroll, LinearLayout.LayoutParams(-1, 0, 1f))
    panel.addView(buildResizeHandle())
}

/** 刷新当前可见的悬浮内容（展开刷面板，收起刷悬浮球）。 */
internal fun GlobalOverlayService.refreshOverlayContent() {
    if (expanded) updatePanelContent() else refreshBubble()
}

/** 刷新悬浮球：未挂载则整体重建，否则只更新文案。 */
internal fun GlobalOverlayService.refreshBubble() {
    if (root == null) renderOverlay() else updateBubble()
}

/** 重绘面板内容，并保留输入框草稿与对话滚动位置。 */
internal fun GlobalOverlayService.updatePanelContent() {
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
