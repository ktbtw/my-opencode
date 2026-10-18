package com.chatcodex.chat_codex_app.overlay.compose

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue

/** 悬浮助手展示用的 Agent 概要。 */
data class AgentUi(
    val agentId: String,
    val name: String,
    val project: String,
    val online: Boolean,
    val busy: Boolean,
)

/** 一轮任务完成的结果卡片。 */
data class CompletionUi(
    val title: String,
    val summary: String,
    val taskId: String,
)

/** 历史会话条目。 */
data class SessionUi(
    val sessionId: String,
    val summary: String,
    val updatedAt: String,
)

/** 一条对话消息。 */
data class ChatUi(
    val role: String,
    val text: String,
)

/**
 * 悬浮层的唯一状态源：模式、Agent 列表、完成结果、对话内容。
 * 服务侧直接改这些字段，Compose 会自动重组。
 */
class OverlayUiState {
    var mode by mutableStateOf(FloatingMode.BALL)
    var previousMode by mutableStateOf(FloatingMode.BALL)
    var agents by mutableStateOf<List<AgentUi>>(emptyList())
    var completion by mutableStateOf<CompletionUi?>(null)
    var messages by mutableStateOf<List<ChatUi>>(emptyList())
    var busy by mutableStateOf(false)
    var input by mutableStateOf("")
    var sessions by mutableStateOf<List<SessionUi>>(emptyList())
    var currentSessionId by mutableStateOf("")
    var modelLabel by mutableStateOf("")

    fun switchTo(target: FloatingMode) {
        if (target == mode) return
        previousMode = mode
        mode = target
    }

    fun backToBall() {
        switchTo(FloatingMode.BALL)
    }
}
