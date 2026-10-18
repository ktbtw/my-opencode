package com.chatcodex.chat_codex_app.overlay.compose

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp

/** 悬浮面板的玻璃质感：半透明底 + 高光描边 + 柔和阴影。 */
@Composable
fun Modifier.floatingGlass(
    corner: Dp = 28.dp,
    tint: Color = Color(0xFF0B1020),
    alpha: Float = 0.82f,
): Modifier {
    val shape = RoundedCornerShape(corner)
    return this
        .shadow(24.dp, shape, ambientColor = Color.Black, spotColor = Color.Black)
        .background(
            brush = Brush.verticalGradient(
                listOf(
                    tint.copy(alpha = alpha),
                    tint.copy(alpha = (alpha - 0.06f).coerceAtLeast(0f)),
                ),
            ),
            shape = shape,
        )
        .border(
            width = 1.dp,
            brush = Brush.verticalGradient(
                listOf(
                    Color.White.copy(alpha = 0.28f),
                    Color.White.copy(alpha = 0.06f),
                ),
            ),
            shape = shape,
        )
}
