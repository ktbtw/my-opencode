package com.chatcodex.chat_codex_app.overlay.compose

import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.togetherWith
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
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material.icons.filled.Send
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import kotlin.math.roundToInt
import androidx.compose.ui.unit.IntOffset
import androidx.compose.runtime.setValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.gestures.detectVerticalDragGestures
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.delay

// 与 Flutter 侧 AppColors 保持一致
private val TextPrimary = Color(0xFF0F1729)
private val TextSecondary = Color(0xFF4A5D7A)
private val AccentBlue = Color(0xFF2563EB)
private val AccentBlueDeep = Color(0xFF1D4ED8)
private val SurfaceBackground = Color(0xFFF2F6FC)
private val ChipBackground = Color(0xFFEFF6FF)
private val BorderLight = Color(0xFFD0DDEF)
private val StatusOnline = Color(0xFF16A34A)
private val StatusWarning = Color(0xFFD97706)
private val StatusMuted = Color(0xFF8FA3BF)
private val TextMuted = Color(0xFF8FA3BF)

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
    onSelectSession: (String) -> Unit = {},
    onNewSession: () -> Unit = {},
    onExpand: () -> Unit = {},
    onCollapse: () -> Unit = {},
    onBallTap: (() -> Unit)? = null,
    onAttach: () -> Unit = {},
    onRemoveAttachment: (Int) -> Unit = {},
    onVoiceStart: () -> Unit = {},
    onVoiceStop: () -> Unit = {},
) {
    // 实验 C：去掉 AnimatedContent，直接按 mode 渲染，验证过渡层是否吞掉触摸。
    run {
        val mode = state.mode
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
                    onClick = { onBallTap?.invoke() ?: state.switchTo(FloatingMode.WINDOW) },
                    onDragStart = onDragStart,
                    onDrag = onMove,
                    onDragEnd = onDragEnd,
                )
            }

            FloatingMode.VOICE_BALL -> VoiceBall(
                active = state.voiceActive,
                text = state.voiceText,
                error = state.voiceError,
                onStop = onVoiceStop,
            )

            FloatingMode.WINDOW -> Column(
                modifier = Modifier.fillMaxSize(),
                verticalArrangement = Arrangement.Center,
            ) {
                ChatWindow(
                    state = state,
                    immersive = false,
                    onSend = onSend,
                    onClose = onCollapse,
                    onExpand = onExpand,
                    onSelectSession = onSelectSession,
                    onNewSession = onNewSession,
                    onAttach = onAttach,
                    onRemoveAttachment = onRemoveAttachment,
                    onVoiceStart = onVoiceStart,
                    onVoiceStop = onVoiceStop,
                )
            }

            FloatingMode.FULLSCREEN -> {
                var dragOffset by remember { mutableFloatStateOf(0f) }
                Box(modifier = Modifier.fillMaxSize()) {
                    Box(
                        modifier = Modifier
                            .fillMaxSize()
                            .background(Color(0xFF0F1729).copy(alpha = 0.30f))
                            .clickable { state.backToBall() },
                    )
                    Column(
                        modifier = Modifier
                            .fillMaxSize()
                            .offset { IntOffset(0, dragOffset.roundToInt()) }
                            .padding(top = 36.dp)
                            .pointerInput(Unit) {
                                detectVerticalDragGestures(
                                    onDragEnd = {
                                        if (dragOffset > 120f) state.backToBall()
                                        dragOffset = 0f
                                    },
                                    onDragCancel = { dragOffset = 0f },
                                ) { _, dragAmount ->
                                    if (dragAmount > 0 || dragOffset > 0) {
                                        dragOffset = (dragOffset + dragAmount).coerceAtLeast(0f)
                                    }
                                }
                            },
                    ) {
                        ChatWindow(
                            state = state,
                            immersive = true,
                            onSend = onSend,
                            onClose = onCollapse,
                            onExpand = null,
                            onSelectSession = onSelectSession,
                            onNewSession = onNewSession,
                            onAttach = onAttach,
                            onRemoveAttachment = onRemoveAttachment,
                            onVoiceStart = onVoiceStart,
                            onVoiceStop = onVoiceStop,
                        )
                    }
                }
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
    onSelectSession: (String) -> Unit,
    onNewSession: () -> Unit,
    onAttach: () -> Unit,
    onRemoveAttachment: (Int) -> Unit,
    onVoiceStart: () -> Unit,
    onVoiceStop: () -> Unit,
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
            .floatingGlass(corner = 30.dp)
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
                        state.modelLabel.isNotBlank() ->
                            "${state.agents.size} 个 Agent · ${state.modelLabel}"
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
        if (immersive) {
            Spacer(Modifier.height(10.dp))
            AgentStrip(state = state)
        }
        if (state.sessions.isNotEmpty() || state.currentSessionId.isNotBlank()) {
            Spacer(Modifier.height(8.dp))
            androidx.compose.foundation.lazy.LazyRow(
                horizontalArrangement = Arrangement.spacedBy(6.dp),
            ) {
                item {
                    SessionChip(
                        label = "新对话",
                        selected = state.currentSessionId.isBlank(),
                        onClick = onNewSession,
                    )
                }
                items(state.sessions.size) { index ->
                    val session = state.sessions[index]
                    SessionChip(
                        label = session.summary.ifBlank { session.sessionId.take(8) },
                        selected = session.sessionId == state.currentSessionId,
                        onClick = { onSelectSession(session.sessionId) },
                    )
                }
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
                                        if (mine) AccentBlue
                                        else SurfaceBackground,
                                    )
                                    .padding(horizontal = 12.dp, vertical = 8.dp),
                            )
                        }
                    }
                }
            }
        }
        if (state.attachments.isNotEmpty()) {
            Spacer(Modifier.height(6.dp))
            androidx.compose.foundation.lazy.LazyRow(
                horizontalArrangement = Arrangement.spacedBy(6.dp),
            ) {
                items(state.attachments.size) { index ->
                    val attachment = state.attachments[index]
                    Row(
                        modifier = Modifier
                            .clip(RoundedCornerShape(10.dp))
                            .background(ChipBackground)
                            .border(1.dp, BorderLight, RoundedCornerShape(10.dp))
                            .clickable { onRemoveAttachment(index) }
                            .padding(horizontal = 9.dp, vertical = 6.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Text(
                            text = attachment.name,
                            color = TextSecondary,
                            fontSize = 11.sp,
                            maxLines = 1,
                            modifier = Modifier.widthIn(max = 180.dp),
                        )
                        Spacer(Modifier.size(4.dp))
                        Icon(
                            Icons.Filled.Close,
                            contentDescription = "移除附件",
                            tint = TextMuted,
                            modifier = Modifier.size(12.dp),
                        )
                    }
                }
            }
        }
        Spacer(Modifier.height(10.dp))
        Row(verticalAlignment = Alignment.CenterVertically) {
            IconButton(onClick = onAttach, modifier = Modifier.size(40.dp)) {
                Icon(Icons.Filled.Add, contentDescription = "添加附件", tint = TextSecondary)
            }
            IconButton(onClick = onVoiceStart, modifier = Modifier.size(40.dp)) {
                MicGlyph(color = TextSecondary, glyphSize = 22.dp)
            }
            Spacer(Modifier.size(4.dp))
            BasicTextField(
                value = state.input,
                onValueChange = { state.input = it },
                singleLine = true,
                textStyle = TextStyle(color = TextPrimary, fontSize = 14.sp),
                cursorBrush = SolidColor(AccentBlue),
                modifier = Modifier
                    .weight(1f)
                    .clip(CircleShape)
                    .background(SurfaceBackground)
                    .border(1.dp, BorderLight, CircleShape)
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

/** 顶部 Agent 横滑条：显示在线状态与忙碌状态。 */
@Composable
private fun AgentStrip(state: OverlayUiState) {
    if (state.agents.isEmpty()) return
    androidx.compose.foundation.lazy.LazyRow(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 8.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        items(state.agents.size) { index ->
            val agent = state.agents[index]
            Row(
                modifier = Modifier
                    .clip(RoundedCornerShape(14.dp))
                    .background(ChipBackground)
                    .border(1.dp, BorderLight, RoundedCornerShape(14.dp))
                    .padding(horizontal = 12.dp, vertical = 8.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Box(
                    modifier = Modifier
                        .size(7.dp)
                        .clip(CircleShape)
                        .background(
                            when {
                                !agent.online -> StatusMuted
                                agent.busy -> StatusWarning
                                else -> StatusOnline
                            },
                        ),
                )
                Spacer(Modifier.size(6.dp))
                Text(
                    text = agent.name.ifBlank { agent.agentId.take(8) },
                    color = TextPrimary,
                    fontSize = 12.sp,
                )
                if (agent.busy) {
                    Spacer(Modifier.size(6.dp))
                    Text("运行中", color = StatusWarning, fontSize = 10.sp)
                }
            }
        }
    }
}

/** 会话标签。 */
@Composable
private fun SessionChip(label: String, selected: Boolean, onClick: () -> Unit) {
    Text(
        text = label,
        color = if (selected) Color.White else TextSecondary,
        fontSize = 11.sp,
        maxLines = 1,
        modifier = Modifier
            .widthIn(max = 140.dp)
            .clip(RoundedCornerShape(12.dp))
            .background(if (selected) AccentBlue else ChipBackground)
            .border(1.dp, if (selected) AccentBlue else BorderLight, RoundedCornerShape(12.dp))
            .clickable(onClick = onClick)
            .padding(horizontal = 10.dp, vertical = 6.dp),
    )
}
