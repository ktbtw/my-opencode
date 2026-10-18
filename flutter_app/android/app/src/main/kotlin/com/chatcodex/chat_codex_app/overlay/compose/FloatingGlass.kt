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
    tint: Color = Color(0xFFFFFFFF),
    alpha: Float = 1f,
): Modifier {
    val shape = RoundedCornerShape(corner)
    return this
        .shadow(
            18.dp,
            shape,
            ambientColor = Color(0xFF0F1729).copy(alpha = 0.16f),
            spotColor = Color(0xFF0F1729).copy(alpha = 0.20f),
        )
        .background(
            brush = Brush.verticalGradient(
                listOf(
                    tint.copy(alpha = alpha),
                    tint.copy(alpha = alpha),
                ),
            ),
            shape = shape,
        )
        .border(
            width = 1.dp,
            brush = Brush.verticalGradient(
                listOf(
                    Color(0xFFD0DDEF),
                    Color(0xFFE8EFF8),
                ),
            ),
            shape = shape,
        )
}
