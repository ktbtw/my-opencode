package com.chatcodex.chat_codex_app.overlay.compose

import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

private val VoiceBlue = Color(0xFF2563EB)
private val VoiceTextPrimary = Color(0xFF0F1729)
private val VoiceTextSecondary = Color(0xFF4A5D7A)
private val VoiceError = Color(0xFFDC2626)

/** 语音球：聆听时球体放大 + 外圈呼吸，下方显示实时转写。 */
@Composable
fun VoiceBall(
    active: Boolean,
    text: String,
    error: String,
    onStop: () -> Unit,
) {
    val infinite = rememberInfiniteTransition(label = "voiceBall")
    val pulse by infinite.animateFloat(
        initialValue = 0f,
        targetValue = 1f,
        animationSpec = infiniteRepeatable(
            animation = tween(1500, easing = LinearEasing),
            repeatMode = RepeatMode.Restart,
        ),
        label = "voicePulse",
    )

    Column(
        modifier = Modifier.fillMaxSize(),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        // 球体与话筒放在同一个 Box 里，保证同心
        Box(modifier = Modifier.size(160.dp), contentAlignment = Alignment.Center) {
            Canvas(modifier = Modifier.fillMaxSize()) {
                val center = Offset(size.width / 2f, size.height / 2f)
                val base = size.minDimension * 0.26f
                listOf(pulse, (pulse + 0.5f) % 1f).forEach { phase ->
                    drawCircle(
                        color = VoiceBlue.copy(alpha = (1f - phase) * 0.26f),
                        radius = base * (1f + phase * 1.15f),
                        center = center,
                    )
                }
                drawCircle(color = VoiceBlue, radius = base, center = center)
            }
            Box(
                modifier = Modifier
                    .size(64.dp)
                    .clip(CircleShape)
                    .background(Color.White.copy(alpha = 0.22f))
                    .clickable(onClick = onStop),
                contentAlignment = Alignment.Center,
            ) {
                MicGlyph(glyphSize = 26.dp)
            }
        }
        Text(
            text = when {
                text.isNotBlank() -> text
                error.isNotBlank() -> error
                active -> "正在聆听…"
                else -> "点按说话"
            },
            color = if (error.isNotBlank() && text.isBlank()) VoiceError else VoiceTextPrimary,
            fontSize = 12.sp,
            textAlign = TextAlign.Center,
            maxLines = 2,
            modifier = Modifier
                .padding(top = 6.dp, start = 16.dp, end = 16.dp)
                .clip(RoundedCornerShape(10.dp))
                .background(Color.White.copy(alpha = 0.94f))
                .padding(horizontal = 10.dp, vertical = 6.dp),
        )
    }
}

/** 用基础图元画话筒，避免引入扩展图标库。 */
@Composable
internal fun MicGlyph(color: Color = Color.White, glyphSize: androidx.compose.ui.unit.Dp = 26.dp) {
    Canvas(modifier = Modifier.size(glyphSize)) {
        val w = size.width
        val body = color
        // 话筒头
        drawRoundRect(
            color = body,
            topLeft = Offset(w * 0.37f, w * 0.08f),
            size = Size(w * 0.26f, w * 0.44f),
            cornerRadius = CornerRadius(w * 0.13f),
        )
        // 拾音支架
        drawArc(
            color = body,
            startAngle = 0f,
            sweepAngle = 180f,
            useCenter = false,
            topLeft = Offset(w * 0.24f, w * 0.28f),
            size = Size(w * 0.52f, w * 0.40f),
            style = Stroke(width = w * 0.07f),
        )
        // 底座
        drawLine(
            color = body,
            start = Offset(w * 0.5f, w * 0.70f),
            end = Offset(w * 0.5f, w * 0.84f),
            strokeWidth = w * 0.07f,
        )
        drawLine(
            color = body,
            start = Offset(w * 0.34f, w * 0.86f),
            end = Offset(w * 0.66f, w * 0.86f),
            strokeWidth = w * 0.07f,
        )
    }
}
