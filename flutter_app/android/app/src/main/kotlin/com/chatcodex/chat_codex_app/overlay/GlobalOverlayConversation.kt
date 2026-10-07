package com.chatcodex.chat_codex_app.overlay

import android.content.Context
import android.graphics.Color
import android.graphics.Typeface
import android.graphics.drawable.ColorDrawable
import android.text.SpannableStringBuilder
import android.text.Spanned
import android.text.TextWatcher
import android.text.style.BackgroundColorSpan
import android.text.style.ForegroundColorSpan
import android.text.style.RelativeSizeSpan
import android.text.style.StyleSpan
import android.text.style.TypefaceSpan
import android.view.Gravity
import android.view.View
import android.widget.EditText
import android.widget.FrameLayout
import android.widget.HorizontalScrollView
import android.widget.ImageButton
import android.widget.ImageView
import android.widget.LinearLayout
import android.widget.ScrollView
import android.widget.TextView
import com.chatcodex.chat_codex_app.R

/**
 * 对话面板：最近回复展示、附件预览、输入框（含模型/思考强度入口），
 * 以及轻量 Markdown 渲染（标题、列表、代码块、行内粗体/行内码/链接）。
 */

/** 构建单个 Agent 的对话面板：头部操作栏 + 最近回复区 + 输入区。 */
internal fun GlobalOverlayService.populateConversationPanel(panel: LinearLayout, agent: AgentState) {
    ensureLatestReplyLoaded(agent)
    ensureModelsLoaded(agent)
    ensureSessionsLoaded(agent)
    val header = LinearLayout(this).apply { gravity = Gravity.CENTER_VERTICAL }
    header.addView(menuIcon(R.drawable.ic_overlay_back, "返回 Agent 列表") {
        conversationKey = ""
        refreshOverlayContent()
    }, LinearLayout.LayoutParams(dp(44), dp(44)))
    header.addView(TextView(this).apply {
        text = agent.name.ifBlank { "Agent" }
        textSize = 16f
        typeface = Typeface.create("sans-serif-medium", Typeface.NORMAL)
        setTextColor(0xFF202734.toInt())
        includeFontPadding = false
        gravity = Gravity.CENTER_VERTICAL
        setPadding(dp(10), 0, dp(6), 0)
        maxLines = 1
        ellipsize = android.text.TextUtils.TruncateAt.END
        contentDescription = "${agent.name}，拖动移动面板"
        setOnTouchListener(DragTouchListener())
    }, LinearLayout.LayoutParams(0, dp(48), 1f))
    header.addView(menuIcon(R.drawable.ic_overlay_history, "历史会话") {
        sessionPanelKey = agent.key
        updatePanelContent()
    }, LinearLayout.LayoutParams(dp(44), dp(44)).also { it.leftMargin = dp(4) })
    header.addView(menuIcon(R.drawable.ic_overlay_new, "新建对话") {
        startNewConversation(agent)
    }, LinearLayout.LayoutParams(dp(44), dp(44)).also { it.leftMargin = dp(4) })
    header.addView(menuIcon(R.drawable.ic_overlay_collapse, "收起悬浮面板") {
        expanded = false
        renderOverlay()
    }, LinearLayout.LayoutParams(dp(44), dp(44)).also { it.leftMargin = dp(4) })
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
        background = roundedBackground(Color.WHITE, 16)
        addView(TextView(this@populateConversationPanel).apply {
            text = "最近回复"
            textSize = 11f
            setTextColor(0xFF8A99AD.toInt())
            setPadding(0, 0, 0, dp(7))
        })
        if (latestReply.isNotBlank()) {
            addView(buildRenderedMessage(latestReply))
        } else {
            addView(TextView(this@populateConversationPanel).apply {
                text = if (loadingLatestReply) "正在加载最近回复..." else "暂无最近回复"
                textSize = 13f
                setTextColor(0xFF94A3B8.toInt())
                setTextIsSelectable(true)
            })
        }
        if (latestSentMessage.isNotBlank()) {
            addView(TextView(this@populateConversationPanel).apply {
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
            addView(TextView(this@populateConversationPanel).apply {
                text = latestSentMessage
                textSize = 13f
                setTextColor(if (latestSentState == "failed") 0xFFDC2626.toInt() else 0xFF2563EB.toInt())
                gravity = Gravity.END
                setTextIsSelectable(true)
            })
        }
        if (latestCompletion != null && !agent.busy) {
            addView(LinearLayout(this@populateConversationPanel).apply {
                gravity = Gravity.CENTER_VERTICAL
                setPadding(0, dp(14), 0, 0)
                addView(TextView(this@populateConversationPanel).apply {
                    text = "回复已完成"
                    textSize = 11f
                    includeFontPadding = false
                    setTextColor(0xFF15803D.toInt())
                }, LinearLayout.LayoutParams(0, dp(34), 1f))
                addView(TextView(this@populateConversationPanel).apply {
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

/** 构建底部输入区：附件入口、模型/思考强度选择、输入框、发送按钮、附件预览。 */
internal fun GlobalOverlayService.buildComposer(agent: AgentState): View {
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
        addView(ImageView(this@buildComposer).apply {
            setImageResource(R.drawable.ic_overlay_attach)
            scaleType = ImageView.ScaleType.CENTER_INSIDE
        }, LinearLayout.LayoutParams(dp(14), dp(14)))
        addView(TextView(this@buildComposer).apply {
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
        inputType = android.text.InputType.TYPE_CLASS_TEXT or android.text.InputType.TYPE_TEXT_FLAG_MULTI_LINE
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

/** 附件缩略图横向列表。 */
internal fun GlobalOverlayService.buildAttachmentPreviews(): View {
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

/** 单个附件缩略图卡片（图片显示缩略图，其它显示文件图标），右上角可移除。 */
internal fun GlobalOverlayService.buildAttachmentPreview(index: Int, attachment: OverlayAttachment): View {
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

/** 从 Flutter 侧设置读取消息渲染开关。 */
internal fun GlobalOverlayService.currentRenderSettings(): RenderSettings {
    val prefs = getSharedPreferences(FLUTTER_PREFS, Context.MODE_PRIVATE)
    return RenderSettings(
        markdown = prefs.getBoolean("flutter.markdownRender", true),
        latex = prefs.getBoolean("flutter.latexRender", true),
        mermaid = prefs.getBoolean("flutter.mermaidRender", true),
        html = prefs.getBoolean("flutter.htmlPreview", true),
    )
}

/** 把消息内容按当前渲染设置转成可显示的 TextView。 */
internal fun GlobalOverlayService.buildRenderedMessage(content: String): TextView {
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

/** 轻量 Markdown 渲染：支持标题、无序列表、代码块与行内样式。 */
internal fun GlobalOverlayService.markdownSpannable(content: String, settings: RenderSettings): CharSequence {
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

/** 追加一行等宽代码样式文本。 */
internal fun GlobalOverlayService.appendStyledLine(result: SpannableStringBuilder, line: String, color: Int, label: Boolean) {
    val start = result.length
    result.append(line)
    result.setSpan(TypefaceSpan("monospace"), start, result.length, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
    result.setSpan(ForegroundColorSpan(color), start, result.length, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
    if (label) result.setSpan(StyleSpan(Typeface.BOLD), start, result.length, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
    else result.setSpan(BackgroundColorSpan(0xFFF1F5F9.toInt()), start, result.length, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
    result.append('\n')
}

/** 追加一行行内 Markdown（粗体 / 行内码 / 链接）。 */
internal fun GlobalOverlayService.appendInlineMarkdown(result: SpannableStringBuilder, line: String) {
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

/** 根据发送中状态切换发送按钮的可用态与透明度。 */
internal fun GlobalOverlayService.updateComposingUi() {
    sendButton?.apply {
        isEnabled = !composing
        alpha = if (composing) 0.55f else 1f
    }
}

/** 把对话区滚动到底部。 */
internal fun GlobalOverlayService.scrollConversationToBottom() {
    conversationScroll?.post { conversationScroll?.fullScroll(View.FOCUS_DOWN) }
}
