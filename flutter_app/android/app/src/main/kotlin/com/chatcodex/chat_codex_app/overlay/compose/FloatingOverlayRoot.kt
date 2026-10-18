package com.chatcodex.chat_codex_app.overlay.compose

import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.togetherWith
import androidx.compose.foundation.background
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
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material.icons.filled.Send
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.delay

private val TextPrimary = Color(0xFFF2F5FF)
private val TextSecondary = Color(0xFF9AA7C7)
private val AccentBlue = Color(0xFF0A84FF)

/**
 * 悬浮层根组件：球 / 悬浮窗 / 沉浸全屏 / 结果卡片四种形态。
 * 状态来自 [OverlayUiState]，服务侧直接改字段即可驱动 UI。
 */
@Composable
fun FloatingOverlayRoot(
    state: OverlayUiState,
    onMove: (dx: Float, dy: Float) -> Unit = { _, _ -> },
    onDragStart: () -> Unit = {},
    onDragEnd: () -> Unit = {},
    onSend: (String) -> Unit = {},
    onClose: () -> Unit = {},
) {
    AnimatedContent(
        targetState = state.mode,
        transitionSpec = {
            fadeIn(tween(180)) togetherWith fadeOut(tween(140))
        },
        label = "overlayMode",
    ) { mode ->
        when (mode) {
            FloatingMode.BALL -> Box(
                modifier = Modifier.fillMaxSize(),
                contentAlignment = Alignment.Center,
            ) {
                SiriBall(
                    state = when {
                        state.busy -> BallState.Loading
                        state.completion != null -> BallState.Replying
                        else -> BallState.Idle
                    },
                    onClick = { state.switchTo(FloatingMode.WINDOW) },
                    onDragStart = onDragStart,
                    onDrag = onMove,
                    onDragEnd = onDragEnd,
                )
            }

            FloatingMode.WINDOW -> ChatWindow(
                state = state,
                immersive = false,
                onSend = onSend,
                onClose = { state.backToBall() },
                onExpand = { state.switchTo(FloatingMode.FULLSCREEN) },
            )

            FloatingMode.FULLSCREEN -> Box(modifier = Modifier.fillMaxSize()) {
                Box(
                    modifier = Modifier
                        .fillMaxSize()
                        .background(Color.Black.copy(alpha = 0.34f))
                        .clickable { state.backToBall() },
                )
                ChatWindow(
                    state = state,
                    immersive = true,
                    onSend = onSend,
                    onClose = { state.backToBall() },
                    onExpand = null,
                )
            }

            FloatingMode.RESULT_DISPLAY -> {
                val completion = state.completion
                LaunchedEffect(completion?.taskId) {
                    if (completion != null) {
                        delay(3200)
                        if (state.mode == FloatingMode.RESULT_DISPLAY) state.backToBall()
                    }
                }
                Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                    Column(
                        modifier = Modifier
                            .widthIn(max = 320.dp)
                            .floatingGlass(corner = 24.dp)
                            .padding(18.dp),
                    ) {
                        Text(
                            text = completion?.title ?: "任务已完成",
                            color = TextPrimary,
                            fontSize = 15.sp,
                            fontWeight = FontWeight.SemiBold,
                        )
                        Spacer(Modifier.height(8.dp))
                        Text(
                            text = completion?.summary.orEmpty().ifBlank { "可以查看详细结果" },
                            color = TextSecondary,
                            fontSize = 13.sp,
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun ChatWindow(
    state: OverlayUiState,
    immersive: Boolean,
    onSend: (String) -> Unit,
    onClose: () -> Unit,
    onExpand: (() -> Unit)?,
) {
    val listState = rememberLazyListState()
    LaunchedEffect(state.messages.size) {
        if (state.messages.isNotEmpty()) {
            listState.animateScrollToItem(state.messages.lastIndex)
        }
    }
    val modifier = if (immersive) {
        Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 72.dp)
            .floatingGlass(corner = 30.dp, alpha = 0.9f)
    } else {
        Modifier
            .widthIn(max = 360.dp)
            .floatingGlass(corner = 26.dp)
    }
    Column(modifier = modifier.padding(16.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = if (immersive) "全局助手" else "助手",
                    color = TextPrimary,
                    fontSize = 16.sp,
                    fontWeight = FontWeight.SemiBold,
                )
                Text(
                    text = when {
                        state.busy -> "思考中…"
                        state.agents.isEmpty() -> "暂无在线 Agent"
                        else -> "${state.agents.size} 个 Agent · ${state.agents.first().name}"
                    },
                    color = TextSecondary,
                    fontSize = 12.sp,
                )
            }
            if (onExpand != null) {
                IconButton(onClick = onExpand) {
                    Icon(Icons.Filled.KeyboardArrowDown, contentDescription = "全屏", tint = TextSecondary)
                }
            }
            IconButton(onClick = onClose) {
                Icon(Icons.Filled.Close, contentDescription = "收起", tint = TextSecondary)
            }
        }
        Spacer(Modifier.height(10.dp))
        Box(modifier = Modifier.weight(1f, fill = immersive)) {
            if (state.messages.isEmpty()) {
                Column(modifier = Modifier.padding(vertical = 24.dp)) {
                    Text("有什么可以帮你？", color = TextPrimary, fontSize = 15.sp)
                    Spacer(Modifier.height(6.dp))
                    Text("可以直接派活给已连接的 Agent", color = TextSecondary, fontSize = 12.sp)
                }
            } else {
                LazyColumn(state = listState) {
                    items(state.messages) { message ->
                        val mine = message.role == "user"
                        Row(
                            modifier = Modifier.fillMaxWidth().padding(vertical = 4.dp),
                            horizontalArrangement = if (mine) Arrangement.End else Arrangement.Start,
                        ) {
                            Text(
                                text = message.text,
                                color = if (mine) Color.White else TextPrimary,
                                fontSize = 13.sp,
                                modifier = Modifier
                                    .widthIn(max = 260.dp)
                                    .clip(RoundedCornerShape(14.dp))
                                    .background(
                                        if (mine) AccentBlue.copy(alpha = 0.9f)
                                        else Color.White.copy(alpha = 0.08f),
                                    )
                                    .padding(horizontal = 12.dp, vertical = 8.dp),
                            )
                        }
                    }
                }
            }
        }
        Spacer(Modifier.height(10.dp))
        Row(verticalAlignment = Alignment.CenterVertically) {
            BasicTextField(
                value = state.input,
                onValueChange = { state.input = it },
                singleLine = true,
                textStyle = TextStyle(color = TextPrimary, fontSize = 14.sp),
                cursorBrush = SolidColor(AccentBlue),
                modifier = Modifier
                    .weight(1f)
                    .clip(CircleShape)
                    .background(Color.White.copy(alpha = 0.08f))
                    .padding(horizontal = 14.dp, vertical = 10.dp),
                decorationBox = { inner ->
                    if (state.input.isEmpty()) {
                        Text("发消息给 Agent…", color = TextSecondary, fontSize = 14.sp)
                    }
                    inner()
                },
            )
            Spacer(Modifier.size(8.dp))
            Box(
                modifier = Modifier
                    .size(40.dp)
                    .clip(CircleShape)
                    .background(AccentBlue)
                    .clickable {
                        val text = state.input.trim()
                        if (text.isNotEmpty()) {
                            state.input = ""
                            onSend(text)
                        }
                    },
                contentAlignment = Alignment.Center,
            ) {
                Icon(Icons.Filled.Send, contentDescription = "发送", tint = Color.White)
            }
        }
    }
}
