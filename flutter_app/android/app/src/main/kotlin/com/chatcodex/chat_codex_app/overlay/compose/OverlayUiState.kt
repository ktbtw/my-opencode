package com.chatcodex.chat_codex_app.overlay.compose

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue

/** 悬浮助手展示用的 Agent 概要。 */
data class AgentUi(
    /** 服务端唯一键（机器 + Agent + 项目），点击切换对话时回传。 */
    val key: String,
    val agentId: String,
    val name: String,
    val device: String,
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

/** 待发送的附件。 */
data class AttachmentUi(val name: String)

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
    /** 当前对话所属 Agent 的 key，Agent 条据此高亮。 */
    var activeAgentKey by mutableStateOf("")
    var completion by mutableStateOf<CompletionUi?>(null)
    var messages by mutableStateOf<List<ChatUi>>(emptyList())
    var busy by mutableStateOf(false)
    /** 当前会话所属 Agent 是否在跑任务（全屏页用它显示「正在执行…」）。 */
    var activeBusy by mutableStateOf(false)
    var input by mutableStateOf("")
    var sessions by mutableStateOf<List<SessionUi>>(emptyList())
    var currentSessionId by mutableStateOf("")
    var modelLabel by mutableStateOf("")
    var attachments by mutableStateOf<List<AttachmentUi>>(emptyList())
    var voiceActive by mutableStateOf(false)
    var voiceText by mutableStateOf("")
    var voiceError by mutableStateOf("")
    var ocrBusy by mutableStateOf(false)
    var ocrText by mutableStateOf("")
    var ocrError by mutableStateOf("")

    fun switchTo(target: FloatingMode) {
        if (target == mode) return
        previousMode = mode
        mode = target
    }

    fun backToBall() {
        switchTo(FloatingMode.BALL)
    }
}
