package com.chatcodex.chat_codex_app.overlay

import android.net.Uri

/**
 * 悬浮助手使用的数据模型定义。
 *
 * 这些类型原本是 GlobalOverlayService 的私有嵌套类，拆分文件后提升为
 * 顶层 internal 类型，供各个拆分包共同引用。
 */

/** Agent 的运行时状态（来自服务端 SSE 快照/增量事件）。 */
internal data class AgentState(
    /** 所属设备（机器）ID。 */
    val machineId: String,
    /** 设备展示名。 */
    val deviceName: String,
    /** Agent ID。 */
    val agentId: String,
    /** Agent 名称。 */
    val name: String,
    /** 当前项目 ID。 */
    val projectId: String,
    /** 当前项目根目录。 */
    val projectRoot: String,
    /** 状态字符串（online / offline / busy 等）。 */
    val status: String,
    /** 当前正在执行的任务 ID（空串表示空闲）。 */
    val taskId: String,
) {
    /** 是否在线：状态不属于离线/停止/失败/禁用集合。 */
    val online: Boolean
        get() = status.lowercase() !in setOf("offline", "stopped", "failed", "disabled")

    /** 是否忙碌：存在任务 ID，或处于启动/重启/升级等中间态。 */
    val busy: Boolean
        get() = taskId.isNotBlank() || status.lowercase() in setOf("starting", "restarting", "upgrading")

    /** 唯一键：机器 + Agent + 项目组合。 */
    val key: String
        get() = "$machineId\u0000$agentId\u0000$projectId"
}

/** 一条「任务已完成」记录，用于悬浮球提示与完成卡片。 */
internal data class CompletionItem(
    /** 卡片标题。 */
    val title: String,
    /** 摘要文本。 */
    val summary: String,
    /** 任务 ID。 */
    val taskId: String,
    /** 会话 ID。 */
    var sessionId: String,
    /** Agent ID。 */
    var agentId: String,
    /** 机器 ID。 */
    var machineId: String = "",
    /** 项目 ID。 */
    var projectId: String = "",
    /** 项目根目录。 */
    var projectRoot: String = "",
    /** 任务完整结果（用于展示与继续对话）。 */
    var result: String = "",
)

/** HTTP 响应结果（状态码 + 响应体）。 */
internal data class HttpResult(val statusCode: Int, val body: String)

/** 消息渲染开关（来自 Flutter 侧设置）。 */
internal data class RenderSettings(
    val markdown: Boolean,
    val latex: Boolean,
    val mermaid: Boolean,
    val html: Boolean,
)

/** 待发送的附件（内容 URI + 展示名）。 */
internal data class OverlayAttachment(val uri: Uri, val name: String)

/** 提问面板中的一个选项。 */
internal data class OverlayQuestionOption(
    val label: String,
    val description: String,
)

/** 提问面板中的一道题。 */
internal data class OverlayQuestionItem(
    val question: String,
    val header: String,
    val options: List<OverlayQuestionOption>,
    /** 是否多选。 */
    val multiple: Boolean,
    /** 是否允许自定义输入。 */
    val custom: Boolean,
)

/** 待回答的提问请求（Agent 向用户发问）。 */
internal data class OverlayQuestion(
    val taskId: String,
    val requestId: String,
    val sessionId: String,
    /** 关联 Agent 的唯一键。 */
    val agentKey: String,
    val agentId: String,
    val machineId: String,
    val projectId: String,
    val questions: List<OverlayQuestionItem>,
)

/** 历史会话条目。 */
internal data class OverlaySession(
    val sessionId: String,
    val summary: String,
    val updatedAt: String,
)

/** 可选的模型（带思考强度变体）。 */
internal data class OverlayModel(
    val providerId: String,
    val modelId: String,
    /** 展示名（provider / model）。 */
    val name: String,
    /** 支持的思考强度变体集合。 */
    val variants: Set<String>,
) {
    /** 唯一键：provider/model。 */
    val metaKey: String
        get() = "$providerId/$modelId"
}
