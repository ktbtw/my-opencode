package com.chatcodex.chat_codex_app.overlay.compose

import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.size
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.BlendMode
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.drawscope.DrawScope
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.input.pointer.positionChange
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import kotlin.math.PI
import kotlin.math.cos
import kotlin.math.sin

/** 球体状态：空闲 / 按下 / 思考中 / 回复中。 */
enum class BallState { Idle, Pressing, Loading, Replying }

/**
 * 配色直接照 Operit 的 SiriBall：iOS 蓝 / 紫 / 粉 / 青。
 * 之前按 App 主蓝改成一片同色系，四色光斑叠出来就成了一坨蓝紫污渍——
 * Siri 球的辨识度全在这四个高饱和色上。
 */
private val MainColor = Color(0xFF0A84FF)
private val AccentPurple = Color(0xFFBF5AF2)
private val AccentPink = Color(0xFFFF375F)
private val AccentCyan = Color(0xFF00D4FF)

/**
 * Siri 风格悬浮球，绘制逻辑复刻 Operit `ui/floating/ui/ball/SiriBall.kt`：
 *
 * 1. 底部蓝紫光晕
 * 2. 四个慢速旋转的彩色光斑（蓝/紫/粉/青）
 * 3. 中心明亮核心
 * 4. 左上玻璃高光
 * 5. 最外层柔和白边
 * 6. Loading 白色旋转弧
 *
 * 外围那几圈扩散/旋转的光环已经去掉：球贴在其他 App 上时会显得脏，
 * 而且和「安静的悬浮球」这个定位不符。
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
        targetValue = if (isPressed) 0.85f else 1f,
        animationSpec = tween(durationMillis = 200, easing = FastOutSlowInEasing),
        label = "pressScale",
    )

    val infinite = rememberInfiniteTransition(label = "siri")

    // 慢速旋转：15s 一圈，只让色斑缓慢流动，不做成风车。
    val rotation by infinite.animateFloat(
        initialValue = 0f,
        targetValue = 360f,
        animationSpec = infiniteRepeatable(tween(15000, easing = LinearEasing)),
        label = "rotation",
    )
    // 柔和呼吸
    val breathe by infinite.animateFloat(
        initialValue = 0.95f,
        targetValue = 1f,
        animationSpec = infiniteRepeatable(
            tween(3000, easing = FastOutSlowInEasing),
            repeatMode = RepeatMode.Reverse,
        ),
        label = "breathe",
    )

    // 轻点 / 拖动统一在一个 pointerInput 里处理：
    // 这里原来只有 clickable，按下事件被点击手势吃掉，onDrag* 回调从来没接上，
    // 所以球只能点不能拖。现在按下后累计位移，超过 touchSlop 升级为拖动并消费事件，
    // 没超过就按轻点处理。
    val currentOnClick by rememberUpdatedState(onClick)
    val currentOnDragStart by rememberUpdatedState(onDragStart)
    val currentOnDrag by rememberUpdatedState(onDrag)
    val currentOnDragEnd by rememberUpdatedState(onDragEnd)

    Box(
        modifier = Modifier
            .fillMaxSize()
            .pointerInput(Unit) {
                val touchSlop = viewConfiguration.touchSlop
                awaitEachGesture {
                    val down = awaitFirstDown(requireUnconsumed = false)
                    down.consume()
                    pressed = true
                    var dragging = false
                    var total = Offset.Zero
                    // 窗口位置按整数像素移动，小数部分留在本地累加，慢速拖动才不会丢位移。
                    var pendingX = 0f
                    var pendingY = 0f
                    try {
                        while (true) {
                            val event = awaitPointerEvent()
                            val change = event.changes.firstOrNull { it.id == down.id } ?: break
                            if (!change.pressed) {
                                change.consume()
                                break
                            }
                            val delta = change.positionChange()
                            total += delta
                            if (!dragging && total.getDistance() > touchSlop) {
                                dragging = true
                                currentOnDragStart()
                            }
                            if (dragging) {
                                change.consume()
                                pendingX += delta.x
                                pendingY += delta.y
                                val stepX = pendingX.toInt()
                                val stepY = pendingY.toInt()
                                if (stepX != 0 || stepY != 0) {
                                    pendingX -= stepX
                                    pendingY -= stepY
                                    currentOnDrag(stepX.toFloat(), stepY.toFloat())
                                }
                            }
                        }
                    } finally {
                        pressed = false
                        if (dragging) currentOnDragEnd()
                    }
                    if (!dragging) currentOnClick()
                }
            },
        contentAlignment = Alignment.Center,
    ) {
        Canvas(modifier = Modifier.size(ballSize * 3f)) {
            val center = Offset(size.width / 2f, size.height / 2f)
            // 画布是窗口的 3 倍，按 ballSize 直接给半径：
            // 之前取画布的 1/3 只有 55dp，四色光斑全挤在中心，糊成一团白。
            val baseRadius = ballSize.toPx() * 0.62f * pressScale

            // 1. 底部光晕：蓝紫底光，Screen 混合让色彩叠亮而不是叠灰。
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

            // 2. 四个慢速旋转的彩色光斑（相位差 90°）。
            drawColorBlob(center, baseRadius, rotation, baseRadius * 0.20f * breathe, MainColor, 0.70f)
            drawColorBlob(center, baseRadius, rotation + 90f, baseRadius * 0.25f * breathe, AccentPurple, 0.65f)
            drawColorBlob(center, baseRadius, rotation + 180f, baseRadius * 0.22f * breathe, AccentPink, 0.60f)
            drawColorBlob(center, baseRadius, rotation + 270f, baseRadius * 0.23f * breathe, AccentCyan, 0.68f)

            // 3. Loading：只加一层彩色呼吸光，不画任何圆环/旋转弧。
            // 别用白色——白色罩在球上会把四色光斑洗成灰白。
            if (state == BallState.Loading) {
                drawCircle(
                    brush = Brush.radialGradient(
                        colors = listOf(
                            MainColor.copy(alpha = 0.16f + 0.10f * breathe),
                            AccentPurple.copy(alpha = 0.10f),
                            Color.Transparent,
                        ),
                        center = center,
                        radius = baseRadius * 1.0f,
                    ),
                    center = center,
                    radius = baseRadius * 1.0f,
                    blendMode = BlendMode.Screen,
                )
            }

            // 4. 中心明亮核心：白 → 蓝 → 紫，做出“发光球心”。
            drawCircle(
                brush = Brush.radialGradient(
                    colors = listOf(
                        Color.White.copy(alpha = 0.55f),
                        MainColor.copy(alpha = 0.62f),
                        AccentPurple.copy(alpha = 0.55f),
                        Color.Transparent,
                    ),
                    center = center,
                    radius = baseRadius * 0.58f * breathe,
                ),
                center = center,
                radius = baseRadius * 0.58f * breathe,
                blendMode = BlendMode.Screen,
            )

            // 4. 左上玻璃高光。
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

            // 5. 最外层柔和白边：让球体边缘不硬，和背景过渡自然。
            drawCircle(
                brush = Brush.radialGradient(
                    colors = listOf(
                        Color.Transparent,
                        Color.White.copy(alpha = 0.05f),
                        Color.White.copy(alpha = 0.15f),
                        Color.Transparent,
                    ),
                    center = center,
                    radius = baseRadius * breathe,
                ),
                center = center,
                radius = baseRadius * breathe,
            )

        }
    }
}

/** 流动的彩色光斑（复刻 Operit 的 drawColorBlob）。 */
private fun DrawScope.drawColorBlob(
    center: Offset,
    radius: Float,
    angle: Float,
    distance: Float,
    color: Color,
    size: Float,
) {
    val rad = angle * PI / 180f
    val blobCenter = Offset(
        center.x + distance * cos(rad).toFloat(),
        center.y + distance * sin(rad).toFloat(),
    )
    drawCircle(
        brush = Brush.radialGradient(
            colors = listOf(
                color.copy(alpha = 0.8f),
                color.copy(alpha = 0.5f),
                color.copy(alpha = 0.2f),
                Color.Transparent,
            ),
            center = blobCenter,
            radius = radius * size,
        ),
        center = blobCenter,
        radius = radius * size,
        blendMode = BlendMode.Screen,
    )
}
