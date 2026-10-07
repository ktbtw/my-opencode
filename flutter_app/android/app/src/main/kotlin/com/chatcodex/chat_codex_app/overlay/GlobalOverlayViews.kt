package com.chatcodex.chat_codex_app.overlay

import android.content.Context
import android.content.res.ColorStateList
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.Paint
import android.graphics.Path
import android.graphics.Typeface
import android.graphics.drawable.GradientDrawable
import android.graphics.drawable.RippleDrawable
import android.view.Gravity
import android.view.View
import android.widget.ImageButton
import android.widget.ImageView
import android.widget.LinearLayout
import android.widget.TextView
import com.chatcodex.chat_codex_app.R

/**
 * 悬浮助手的通用 UI 工具与自定义 View。
 *
 * 包含 dp 换算、圆角背景、水波纹点击背景、图标按钮、Agent 列表行、缩放拖柄，
 * 以及用 Canvas 手绘的品牌标记 / 缩放手柄等基础视图构件。
 */

/** dp 转像素。 */
internal fun Context.dp(value: Int): Int = (value * resources.displayMetrics.density).toInt()

/** 生成圆角背景，可选描边。 */
internal fun GlobalOverlayService.roundedBackground(
    fillColor: Int,
    radiusDp: Int,
    strokeColor: Int? = null,
): GradientDrawable = GradientDrawable().apply {
    setColor(fillColor)
    cornerRadius = dp(radiusDp).toFloat()
    strokeColor?.let { setStroke(dp(1), it) }
}

/** 生成带描边的圆角背景（参数顺序为 fill / stroke / radius）。 */
internal fun GlobalOverlayService.roundedBackground(
    fill: Int,
    stroke: Int,
    radius: Int,
): GradientDrawable = GradientDrawable().apply {
    setColor(fill); setStroke(dp(1), stroke); cornerRadius = dp(radius).toFloat()
}

/** 生成带水波纹反馈的圆角点击背景。 */
internal fun GlobalOverlayService.touchSurface(fill: Int, radius: Int, dark: Boolean = false): RippleDrawable =
    RippleDrawable(
        ColorStateList.valueOf(if (dark) 0x24FFFFFF else 0x143666B9),
        roundedBackground(fill, radius),
        roundedBackground(Color.WHITE, radius),
    )

/** 生成统一的图标按钮（带点击反馈与无障碍描述）。 */
internal fun GlobalOverlayService.menuIcon(icon: Int, label: String, action: () -> Unit): ImageButton =
    ImageButton(this).apply {
        setImageResource(icon)
        setColorFilter(0xFF59677B.toInt())
        background = touchSurface(0xFFF0F3F7.toInt(), 12)
        setPadding(dp(12), dp(12), dp(12), dp(12))
        contentDescription = label
        setOnClickListener { action() }
    }

/** Agent 状态中文文案。 */
internal fun GlobalOverlayService.agentStatusLabel(agent: AgentState): String = when {
    pendingQuestionsByTaskId.values.any { questionBelongsToAgent(it, agent) } -> "需回答"
    agent.busy -> "工作中"
    agent.online -> "空闲"
    else -> "离线"
}

/** Agent 状态对应文字颜色。 */
internal fun GlobalOverlayService.agentStatusColor(agent: AgentState): Int = when {
    pendingQuestionsByTaskId.values.any { questionBelongsToAgent(it, agent) } -> 0xFFB45309.toInt()
    agent.busy -> 0xFF1D4ED8.toInt()
    agent.online -> 0xFF15803D.toInt()
    else -> 0xFF64748B.toInt()
}

/** Agent 列表中的单行（头像 + 名称 + 设备 + 状态）。 */
internal fun GlobalOverlayService.buildAgentRow(agent: AgentState): View {
    val selected = agent.key == selectedKey
    val row = LinearLayout(this).apply {
        orientation = LinearLayout.HORIZONTAL
        gravity = Gravity.CENTER_VERTICAL
        setPadding(dp(12), dp(12), dp(10), dp(12))
        background = touchSurface(if (selected) 0xFFEDF3FF.toInt() else Color.WHITE, 16)
        isFocusable = true
        contentDescription = "${agent.name}，${agent.deviceName}，${agentStatusLabel(agent)}，打开对话"
        setOnClickListener {
            selectedKey = agent.key
            conversationKey = agent.key
            updatePanelContent()
        }
    }
    row.addView(TextView(this).apply {
        text = agent.name.ifBlank { "A" }.take(1)
        textSize = 17f
        typeface = Typeface.create("sans-serif-medium", Typeface.NORMAL)
        gravity = Gravity.CENTER
        includeFontPadding = false
        setTextColor(if (selected) 0xFF3666B9.toInt() else 0xFF687589.toInt())
        background = roundedBackground(if (selected) 0xFFDDE9FF.toInt() else 0xFFF0F3F7.toInt(), 12)
    }, LinearLayout.LayoutParams(dp(40), dp(40)))
    val info = LinearLayout(this).apply {
        orientation = LinearLayout.VERTICAL
        gravity = Gravity.CENTER_VERTICAL
        setPadding(dp(12), 0, dp(6), 0)
    }
    info.addView(TextView(this).apply {
        text = agent.name.ifBlank { "Agent" }
        textSize = 14f
        typeface = Typeface.create("sans-serif-medium", Typeface.NORMAL)
        setTextColor(0xFF202734.toInt())
        maxLines = 1
        ellipsize = android.text.TextUtils.TruncateAt.END
    })
    info.addView(TextView(this).apply {
        text = agent.deviceName.ifBlank { "在线设备" }
        textSize = 11f
        setTextColor(0xFF778294.toInt())
        setPadding(0, dp(4), 0, 0)
        maxLines = 1
        ellipsize = android.text.TextUtils.TruncateAt.END
    })
    row.addView(info, LinearLayout.LayoutParams(0, -2, 1f))
    row.addView(TextView(this).apply {
        text = "• ${agentStatusLabel(agent)}"
        gravity = Gravity.CENTER_VERTICAL
        textSize = 11f
        setTextColor(agentStatusColor(agent))
    }, LinearLayout.LayoutParams(-2, -2))
    row.addView(ImageView(this).apply {
        setImageResource(R.drawable.ic_overlay_back)
        rotation = 180f
        setColorFilter(0xFF9AA5B5.toInt())
    }, LinearLayout.LayoutParams(dp(16), dp(16)).also { it.leftMargin = dp(6) })
    return row.apply {
        layoutParams = LinearLayout.LayoutParams(-1, -2).also { it.bottomMargin = dp(8) }
    }
}

/** 面板底部的缩放提示行。 */
internal fun GlobalOverlayService.buildResizeHandle(): View {
    val row = LinearLayout(this).apply { gravity = Gravity.CENTER_VERTICAL }
    row.addView(TextView(this).apply {
        text = "拖动顶部移动 · 右下角调整大小"
        textSize = 10f
        setTextColor(0xFF778294.toInt())
    }, LinearLayout.LayoutParams(0, -2, 1f))
    row.addView(ResizeHandleView(this).apply {
        contentDescription = "调整面板大小"
        setOnTouchListener(ResizeTouchListener())
    }, LinearLayout.LayoutParams(dp(34), dp(24)))
    return row.apply { layoutParams = LinearLayout.LayoutParams(-1, dp(24)) }
}

/** 品牌标记：手绘的箭头 + 下划线，颜色随状态变化。 */
internal class BrandMarkView(context: Context) : View(context) {
    var accentColor: Int = 0xFFE2E8F0.toInt()
        set(value) { if (field != value) { field = value; invalidate() } }
    private val markPaint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        style = Paint.Style.STROKE
        strokeCap = Paint.Cap.ROUND
        strokeJoin = Paint.Join.ROUND
    }
    private val chevron = Path()

    override fun onDraw(canvas: Canvas) {
        super.onDraw(canvas)
        markPaint.color = accentColor
        markPaint.strokeWidth = resources.displayMetrics.density * 1.8f
        chevron.reset()
        chevron.moveTo(width * .25f, height * .32f)
        chevron.lineTo(width * .44f, height * .50f)
        chevron.lineTo(width * .25f, height * .68f)
        canvas.drawPath(chevron, markPaint)
        canvas.drawLine(width * .53f, height * .68f, width * .75f, height * .68f, markPaint)
    }
}

/** 面板右下角的缩放手柄：三条斜线。 */
internal class ResizeHandleView(context: Context) : View(context) {
    private val paint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        color = 0xFF7AA7E8.toInt()
        strokeWidth = context.dp(2).toFloat()
        strokeCap = Paint.Cap.ROUND
    }

    override fun onDraw(canvas: Canvas) {
        super.onDraw(canvas)
        val right = width - context.dp(4).toFloat()
        val bottom = height - context.dp(3).toFloat()
        for (offset in 0..2) {
            val length = context.dp(5 + offset * 5).toFloat()
            canvas.drawLine(right - length, bottom, right, bottom - length, paint)
        }
    }
}
