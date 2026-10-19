package com.chatcodex.chat_codex_app.overlay.compose

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.layout.size
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp

/**
 * 悬浮层的语义图标。
 *
 * Material 图标核心包没有「全屏 / 回形针 / 屏幕」这三个，用 Canvas 画，
 * 保证图形含义和动作一致（别再拿 ⌄ 当全屏、拿喇叭当录音）。
 */

/** 全屏：四个向外展开的直角括号，和「放大到全屏」语义一致。 */
@Composable
fun ExpandGlyph(size: Dp = 18.dp, color: Color) {
    Canvas(modifier = Modifier.size(size)) {
        val s = this.size
        val stroke = s.width * 0.11f
        val len = s.width * 0.30f
        // 左上
        drawLine(color, Offset(0f, len), Offset(0f, 0f), stroke, StrokeCap.Round)
        drawLine(color, Offset(0f, 0f), Offset(len, 0f), stroke, StrokeCap.Round)
        // 右上
        drawLine(color, Offset(s.width - len, 0f), Offset(s.width, 0f), stroke, StrokeCap.Round)
        drawLine(color, Offset(s.width, 0f), Offset(s.width, len), stroke, StrokeCap.Round)
        // 左下
        drawLine(color, Offset(0f, s.height - len), Offset(0f, s.height), stroke, StrokeCap.Round)
        drawLine(color, Offset(0f, s.height), Offset(len, s.height), stroke, StrokeCap.Round)
        // 右下
        drawLine(color, Offset(s.width - len, s.height), Offset(s.width, s.height), stroke, StrokeCap.Round)
        drawLine(color, Offset(s.width, s.height), Offset(s.width, s.height - len), stroke, StrokeCap.Round)
    }
}

/**
 * 附件：文档 + 折角。
 * 15dp 下回形针会糊成一个「0」，文档轮廓更清楚，语义也更接近「附加文件」。
 */
@Composable
fun AttachmentGlyph(size: Dp = 18.dp, color: Color) {
    Canvas(modifier = Modifier.size(size)) {
        val s = this.size
        val stroke = s.width * 0.10f
        val body = Path().apply {
            moveTo(s.width * 0.22f, s.height * 0.06f)
            lineTo(s.width * 0.60f, s.height * 0.06f)
            lineTo(s.width * 0.78f, s.height * 0.26f)
            lineTo(s.width * 0.78f, s.height * 0.94f)
            lineTo(s.width * 0.22f, s.height * 0.94f)
            close()
        }
        drawPath(body, color, style = Stroke(width = stroke, cap = StrokeCap.Round))
        drawLine(
            color,
            Offset(s.width * 0.60f, s.height * 0.06f),
            Offset(s.width * 0.60f, s.height * 0.26f),
            stroke,
            StrokeCap.Round,
        )
        drawLine(
            color,
            Offset(s.width * 0.60f, s.height * 0.26f),
            Offset(s.width * 0.78f, s.height * 0.26f),
            stroke,
            StrokeCap.Round,
        )
        drawLine(
            color,
            Offset(s.width * 0.34f, s.height * 0.48f),
            Offset(s.width * 0.66f, s.height * 0.48f),
            stroke,
            StrokeCap.Round,
        )
        drawLine(
            color,
            Offset(s.width * 0.34f, s.height * 0.64f),
            Offset(s.width * 0.66f, s.height * 0.64f),
            stroke,
            StrokeCap.Round,
        )
    }
}

/** 屏幕内容：显示器轮廓 + 底座。 */
@Composable
fun ScreenGlyph(size: Dp = 18.dp, color: Color) {
    Canvas(modifier = Modifier.size(size)) {
        val s = this.size
        val stroke = s.width * 0.10f
        drawRoundRect(
            color = color,
            topLeft = Offset(s.width * 0.06f, s.height * 0.10f),
            size = Size(s.width * 0.88f, s.height * 0.62f),
            cornerRadius = CornerRadius(s.width * 0.12f, s.width * 0.12f),
            style = Stroke(width = stroke),
        )
        drawLine(
            color,
            Offset(s.width * 0.36f, s.height * 0.90f),
            Offset(s.width * 0.64f, s.height * 0.90f),
            strokeWidth = stroke,
            cap = StrokeCap.Round,
        )
    }
}

/**
 * 麦克风：胶囊 + 拾音环 + 支杆 + 底座。
 *
 * 之前 0.13 的线宽配 0.32 宽的胶囊，缩到 17~20dp 后胶囊和半环几乎粘在一起，
 * 看起来像「高脚杯」。这里收窄胶囊（0.26）、压细线宽（0.10，且不低于 1.05dp），
 * 半环下移留出间隙，15dp 也能一眼认出是话筒。
 */
@Composable
fun MicIconGlyph(size: Dp = 18.dp, color: Color) {
    Canvas(modifier = Modifier.size(size)) {
        val s = this.size
        val stroke = maxOf(s.width * 0.10f, 1.05.dp.toPx())
        // 胶囊
        drawRoundRect(
            color = color,
            topLeft = Offset(s.width * 0.37f, s.height * 0.06f),
            size = Size(s.width * 0.26f, s.height * 0.46f),
            cornerRadius = CornerRadius(s.width * 0.13f, s.width * 0.13f),
        )
        // 拾音环：半圆，两端略高于胶囊底部，和胶囊留出视觉间隙
        drawArc(
            color = color,
            startAngle = 0f,
            sweepAngle = 180f,
            useCenter = false,
            topLeft = Offset(s.width * 0.20f, s.height * 0.30f),
            size = Size(s.width * 0.60f, s.height * 0.40f),
            style = Stroke(width = stroke, cap = StrokeCap.Round),
        )
        // 支杆
        drawLine(
            color,
            Offset(s.width * 0.5f, s.height * 0.70f),
            Offset(s.width * 0.5f, s.height * 0.86f),
            stroke,
            StrokeCap.Round,
        )
        // 底座
        drawLine(
            color,
            Offset(s.width * 0.32f, s.height * 0.91f),
            Offset(s.width * 0.68f, s.height * 0.91f),
            stroke,
            StrokeCap.Round,
        )
    }
}
