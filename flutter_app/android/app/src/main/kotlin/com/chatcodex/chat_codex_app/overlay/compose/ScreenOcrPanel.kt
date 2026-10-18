package com.chatcodex.chat_codex_app.overlay.compose

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

private val OcrTextPrimary = Color(0xFF0F1729)
private val OcrTextSecondary = Color(0xFF4A5D7A)
private val OcrTextMuted = Color(0xFF8FA3BF)
private val OcrBorder = Color(0xFFD0DDEF)
private val OcrSurface = Color(0xFFF2F6FC)
private val OcrBlue = Color(0xFF2563EB)
private val OcrError = Color(0xFFDC2626)

/** 屏幕识别结果面板：识别中 / 失败 / 文本三种状态。 */
@Composable
fun ScreenOcrPanel(
    busy: Boolean,
    text: String,
    error: String,
    onClose: () -> Unit,
    onRetry: () -> Unit,
    onSend: () -> Unit,
) {
    val clipboard = LocalClipboardManager.current
    Column(
        modifier = Modifier
            .widthIn(max = 340.dp)
            .floatingGlass(corner = 26.dp)
            .padding(16.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Column(modifier = Modifier.weight(1f)) {
                Text("屏幕识别", color = OcrTextPrimary, fontSize = 16.sp, fontWeight = FontWeight.SemiBold)
                Text(
                    text = when {
                        busy -> "正在读取当前画面…"
                        error.isNotBlank() -> "没有拿到文字"
                        text.isNotBlank() -> "已识别 ${text.length} 个字符"
                        else -> "点按下方重新识别"
                    },
                    color = OcrTextSecondary,
                    fontSize = 12.sp,
                )
            }
            IconButton(onClick = onClose) {
                Icon(Icons.Filled.Close, contentDescription = "关闭", tint = OcrTextSecondary)
            }
        }
        Spacer(Modifier.height(10.dp))
        Box(
            modifier = Modifier
                .fillMaxWidth()
                .height(300.dp)
                .clip(RoundedCornerShape(14.dp))
                .background(OcrSurface)
                .border(1.dp, OcrBorder, RoundedCornerShape(14.dp))
                .padding(12.dp),
        ) {
            when {
                error.isNotBlank() -> Text(error, color = OcrError, fontSize = 13.sp)
                text.isNotBlank() -> Text(
                    text = text,
                    color = OcrTextPrimary,
                    fontSize = 13.sp,
                    modifier = Modifier.verticalScroll(rememberScrollState()),
                )
                else -> Text("正在识别…", color = OcrTextMuted, fontSize = 13.sp)
            }
        }
        Spacer(Modifier.height(12.dp))
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.fillMaxSize()) {
            OcrAction("复制", enabled = text.isNotBlank(), primary = false) {
                if (text.isNotBlank()) clipboard.setText(AnnotatedString(text))
            }
            OcrAction("重新识别", enabled = !busy, primary = false, onClick = onRetry)
            OcrAction("发给助手", enabled = text.isNotBlank(), primary = true, onClick = onSend)
        }
    }
}

@Composable
private fun androidx.compose.foundation.layout.RowScope.OcrAction(
    label: String,
    enabled: Boolean,
    primary: Boolean,
    onClick: () -> Unit,
) {
    Box(
        modifier = Modifier
            .weight(1f)
            .height(40.dp)
            .clip(RoundedCornerShape(12.dp))
            .background(
                when {
                    !enabled -> OcrSurface
                    primary -> OcrBlue
                    else -> Color.White
                },
            )
            .border(
                1.dp,
                when {
                    !enabled -> OcrBorder
                    primary -> OcrBlue
                    else -> OcrBorder
                },
                RoundedCornerShape(12.dp),
            )
            .clickable(enabled = enabled, onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            text = label,
            color = when {
                !enabled -> OcrTextMuted
                primary -> Color.White
                else -> OcrTextSecondary
            },
            fontSize = 13.sp,
        )
    }
}
