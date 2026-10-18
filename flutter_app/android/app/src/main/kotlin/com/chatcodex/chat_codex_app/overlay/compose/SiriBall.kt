package com.chatcodex.chat_codex_app.overlay.compose

import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.gestures.detectDragGestures
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.size
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.BlendMode
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.drawscope.DrawScope
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import kotlin.math.cos
import kotlin.math.sin

/** 球体状态：空闲 / 按下 / 思考中 / 回复中。 */
enum class BallState { Idle, Pressing, Loading, Replying }

// 与 App 主色一致：主蓝 + 靛蓝 + 天蓝 + 浅蓝
private val MainColor = Color(0xFF2563EB)
private val AccentPurple = Color(0xFF6366F1)
private val AccentPink = Color(0xFF38BDF8)
private val AccentCyan = Color(0xFFBFD7FF)

/**
 * Siri 风格悬浮球：Canvas 绘制、四色光斑、呼吸/涟漪动效、拖拽与点击。
 * 视觉与交互对齐 Operit 的 SiriBall，去掉语音相关依赖。
 */
@Composable
fun SiriBall(
    ballSize: Dp = 56.dp,
    state: BallState = BallState.Idle,
    onClick: () -> Unit = {},
    onDragStart: () -> Unit = {},
    onDrag: (dx: Float, dy: Float) -> Unit = { _, _ -> },
    onDragEnd: () -> Unit = {},
) {
    var pressed by remember { mutableStateOf(false) }
    val isPressed = pressed || state == BallState.Pressing
    val pressScale by animateFloatAsState(
        targetValue = if (isPressed) 0.88f else 1f,
        animationSpec = tween(120),
        label = "ballPressScale",
    )

    val infinite = rememberInfiniteTransition(label = "siri")
    val rotation by infinite.animateFloat(
        initialValue = 0f,
        targetValue = 360f,
        animationSpec = infiniteRepeatable(tween(6000, easing = LinearEasing)),
        label = "rotation",
    )
    val breathe by infinite.animateFloat(
        initialValue = 0.94f,
        targetValue = 1.06f,
        animationSpec = infiniteRepeatable(tween(2200, easing = LinearEasing)),
        label = "breathe",
    )
    val ripple1Scale by infinite.animateFloat(
        initialValue = 1f,
        targetValue = 2.2f,
        animationSpec = infiniteRepeatable(tween(2600, easing = LinearEasing)),
        label = "ripple1Scale",
    )
    val ripple1Alpha by infinite.animateFloat(
        initialValue = 0.5f,
        targetValue = 0f,
        animationSpec = infiniteRepeatable(tween(2600, easing = LinearEasing)),
        label = "ripple1Alpha",
    )
    val ripple2Scale by infinite.animateFloat(
        initialValue = 1f,
        targetValue = 2.2f,
        animationSpec = infiniteRepeatable(tween(2600, delayMillis = 850, easing = LinearEasing)),
        label = "ripple2Scale",
    )
    val ripple2Alpha by infinite.animateFloat(
        initialValue = 0.4f,
        targetValue = 0f,
        animationSpec = infiniteRepeatable(tween(2600, delayMillis = 850, easing = LinearEasing)),
        label = "ripple2Alpha",
    )
    val loadingRotation by infinite.animateFloat(
        initialValue = 0f,
        targetValue = 360f,
        animationSpec = infiniteRepeatable(tween(1200, easing = LinearEasing)),
        label = "loadingRotation",
    )

    Box(
        modifier = Modifier
            .fillMaxSize()
            .clickable(
                interactionSource = remember { MutableInteractionSource() },
                indication = null,
                onClick = { onClick() },
            ),
        contentAlignment = Alignment.Center,
    ) {
        Canvas(modifier = Modifier.size(ballSize * 3f)) {
            val center = Offset(size.width / 2f, size.height / 2f)
            val baseRadius = (size.minDimension / 2f) * pressScale * 0.33f

            // 1. 三层音波扩散
            drawCircle(
                brush = Brush.radialGradient(
                    colors = listOf(
                        Color.Transparent,
                        MainColor.copy(alpha = ripple2Alpha * 0.15f),
                        Color.Transparent,
                    ),
                    center = center,
                    radius = baseRadius * ripple2Scale,
                ),
                center = center,
                radius = baseRadius * ripple2Scale,
            )
            drawCircle(
                brush = Brush.radialGradient(
                    colors = listOf(
                        Color.Transparent,
                        AccentPurple.copy(alpha = ripple1Alpha * 0.2f),
                        Color.Transparent,
                    ),
                    center = center,
                    radius = baseRadius * ripple1Scale,
                ),
                center = center,
                radius = baseRadius * ripple1Scale,
            )

            // 2. 底部光晕
            drawCircle(
                brush = Brush.radialGradient(
                    colors = listOf(
                        MainColor.copy(alpha = 0.3f),
                        AccentPurple.copy(alpha = 0.2f),
                        Color.Transparent,
                    ),
                    center = center,
                    radius = baseRadius * breathe * 0.7f,
                ),
                center = center,
                radius = baseRadius * breathe * 0.7f,
                blendMode = BlendMode.Screen,
            )

            // 3. 四个流动色斑
            drawColorBlob(center, baseRadius, rotation, baseRadius * 0.2f * breathe, MainColor, 0.7f)
            drawColorBlob(center, baseRadius, rotation + 90f, baseRadius * 0.25f * breathe, AccentPurple, 0.65f)
            drawColorBlob(center, baseRadius, rotation + 180f, baseRadius * 0.22f * breathe, AccentPink, 0.6f)
            drawColorBlob(center, baseRadius, rotation + 270f, baseRadius * 0.23f * breathe, AccentCyan, 0.68f)

            // 4. 核心光球
            drawCircle(
                brush = Brush.radialGradient(
                    colors = listOf(
                        Color.White.copy(alpha = 0.7f),
                        MainColor.copy(alpha = 0.6f),
                        AccentPurple.copy(alpha = 0.5f),
                        Color.Transparent,
                    ),
                    center = center,
                    radius = baseRadius * 0.65f * breathe,
                ),
                center = center,
                radius = baseRadius * 0.65f * breathe,
                blendMode = BlendMode.Screen,
            )

            // 5. 左上高光
            val highlight = Offset(center.x - baseRadius * 0.25f, center.y - baseRadius * 0.25f)
            drawCircle(
                brush = Brush.radialGradient(
                    colors = listOf(
                        Color.White.copy(alpha = 0.5f),
                        Color.White.copy(alpha = 0.25f),
                        Color.Transparent,
                    ),
                    center = highlight,
                    radius = baseRadius * 0.35f,
                ),
                center = highlight,
                radius = baseRadius * 0.35f,
                blendMode = BlendMode.Screen,
            )

            // 6. 思考中：旋转弧线
            if (state == BallState.Loading) {
                val sweepStart = loadingRotation
                drawArc(
                    brush = Brush.sweepGradient(
                        colors = listOf(
                            Color.Transparent,
                            AccentCyan.copy(alpha = 0.9f),
                            AccentPurple.copy(alpha = 0.9f),
                            Color.Transparent,
                        ),
                        center = center,
                    ),
                    startAngle = sweepStart,
                    sweepAngle = 120f,
                    useCenter = false,
                    topLeft = Offset(center.x - baseRadius * 1.1f, center.y - baseRadius * 1.1f),
                    size = androidx.compose.ui.geometry.Size(baseRadius * 2.2f, baseRadius * 2.2f),
                    style = androidx.compose.ui.graphics.drawscope.Stroke(
                        width = baseRadius * 0.12f,
                        cap = androidx.compose.ui.graphics.StrokeCap.Round,
                    ),
                )
            }
        }
    }
}

private fun DrawScope.drawColorBlob(
    center: Offset,
    radius: Float,
    angle: Float,
    distance: Float,
    color: Color,
    size: Float,
) {
    val rad = Math.toRadians(angle.toDouble())
    val x = center.x + cos(rad).toFloat() * distance
    val y = center.y + sin(rad).toFloat() * distance
    val blobRadius = radius * size
    drawCircle(
        brush = Brush.radialGradient(
            colors = listOf(
                color.copy(alpha = 0.9f),
                color.copy(alpha = 0.35f),
                Color.Transparent,
            ),
            center = Offset(x, y),
            radius = blobRadius,
        ),
        center = Offset(x, y),
        radius = blobRadius,
        blendMode = BlendMode.Screen,
    )
}
