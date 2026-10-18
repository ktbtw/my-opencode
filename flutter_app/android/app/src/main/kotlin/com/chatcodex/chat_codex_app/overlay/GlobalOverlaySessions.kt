package com.chatcodex.chat_codex_app.overlay

import android.graphics.Color
import android.graphics.Typeface
import android.graphics.drawable.ColorDrawable
import android.net.Uri
import android.view.Gravity
import android.view.View
import android.widget.ImageButton
import android.widget.LinearLayout
import android.widget.ScrollView
import android.widget.TextView
import com.chatcodex.chat_codex_app.R
import org.json.JSONArray

internal fun GlobalOverlayService.selectSession(agent: AgentState, session: OverlaySession) {
    sessionPanelKey = ""
    newConversationKeys.remove(agent.key)
    latestSessionIdsByAgentKey[agent.key] = session.sessionId
    latestReplyLoadedKeys.remove(agent.key)
    latestReplyLoadingKeys.remove(agent.key)
    latestRepliesByAgentKey.remove(agent.key)
    ensureSessionLatestReplyLoaded(agent, session.sessionId)
    updatePanelContent()
}

/** 新建对话：清空当前会话与回复缓存。 */

internal fun GlobalOverlayService.startNewConversation(agent: AgentState) {
    sessionPanelKey = ""
    modelPickerKey = ""
    newConversationKeys.add(agent.key)
    latestSessionIdsByAgentKey.remove(agent.key)
    latestReplyLoadedKeys.add(agent.key)
    latestReplyLoadingKeys.remove(agent.key)
    latestRepliesByAgentKey.remove(agent.key)
    latestSentMessagesByAgentKey.remove(agent.key)
    latestSentStatesByAgentKey.remove(agent.key)
    latestSentTaskIdsByAgentKey.remove(agent.key)
    toast("已新建对话")
    updatePanelContent()
}

/** 按会话 ID 加载该会话最近一条任务的回复。 */

internal fun GlobalOverlayService.ensureSessionLatestReplyLoaded(agent: AgentState, sessionId: String) {
    executor.execute {
        val query = listOf(
            "session_id" to sessionId,
            "limit" to "1",
            "sort" to "created_desc",
        ).joinToString("&") { (key, value) -> "$key=${Uri.encode(value)}" }
        val raw = apiRaw("/api/tasks?$query")
        val task = runCatching { JSONArray(raw ?: "[]").optJSONObject(0) }.getOrNull()
        val reply = task?.optString("result").orEmpty()
        handler.post {
            if (latestSessionIdsByAgentKey[agent.key] != sessionId) return@post
            if (reply.isNotBlank()) latestRepliesByAgentKey[agent.key] = reply
            latestReplyLoadedKeys.add(agent.key)
            if (conversationKey == agent.key && expanded) updatePanelContent()
        }
    }
}

/** 简单截断 ISO 时间，仅保留到秒。 */

internal fun GlobalOverlayService.formatSessionTime(value: String): String {
    val normalized = value.trim().replace('T', ' ')
    return normalized.substringBefore('.').substringBefore('Z').ifBlank { "" }
}

/** 加载 Agent 最近一条已完成任务的回复（仅在尚无会话信息时）。 */

internal fun GlobalOverlayService.ensureLatestReplyLoaded(agent: AgentState) {
    if (agent.key in newConversationKeys || latestSessionIdsByAgentKey[agent.key].orEmpty().isNotBlank()) return
    if (agent.key in latestReplyLoadedKeys || !latestReplyLoadingKeys.add(agent.key)) return
    executor.execute {
        val query = listOf(
            "agent_id" to agent.agentId,
            "machine_id" to agent.machineId,
            "project_id" to agent.projectId,
            "status" to "completed",
            "limit" to "1",
            "sort" to "created_desc",
        ).joinToString("&") { (key, value) -> "$key=${Uri.encode(value)}" }
        val raw = apiRaw("/api/tasks?$query")
        val reply = runCatching {
            JSONArray(raw ?: "[]").optJSONObject(0)?.optString("result").orEmpty()
        }.getOrDefault("")
        handler.post {
            latestReplyLoadingKeys.remove(agent.key)
            latestReplyLoadedKeys.add(agent.key)
            val latestTask = runCatching { JSONArray(raw ?: "[]").optJSONObject(0) }.getOrNull()
            if (reply.isNotBlank()) latestRepliesByAgentKey[agent.key] = reply
            latestTask?.optString("session_id")?.takeIf { it.isNotBlank() }?.let {
                latestSessionIdsByAgentKey[agent.key] = it
            }
            if (conversationKey == agent.key && expanded) updatePanelContent()
        }
    }
}

/** 加载指定 Agent 的历史会话列表。 */

internal fun GlobalOverlayService.ensureSessionsLoaded(agent: AgentState, retry: Boolean = false) {
    if (!retry && (agent.key in sessionLoadingKeys || sessionsByAgentKey.containsKey(agent.key))) return
    if (!sessionLoadingKeys.add(agent.key)) return
    executor.execute {
        val query = listOf(
            "agent_id" to agent.agentId,
            "machine_id" to agent.machineId,
            "project_id" to agent.projectId,
            "limit" to "50",
        ).joinToString("&") { (key, value) -> "$key=${Uri.encode(value)}" }
        val raw = apiRaw("/api/sessions?$query")
        val sessions = runCatching {
            val array = JSONArray(raw ?: "[]")
            buildList {
                for (i in 0 until array.length()) {
                    val item = array.optJSONObject(i) ?: continue
                    val sessionId = item.optString("session_id").trim()
                    if (sessionId.isBlank()) continue
                    add(
                        OverlaySession(
                            sessionId,
                            item.optString("summary").ifBlank { "未命名对话" },
                            item.optString("updated_at").ifBlank { item.optString("created_at") },
                        ),
                    )
                }
            }
        }.getOrDefault(emptyList())
        handler.post {
            sessionLoadingKeys.remove(agent.key)
            sessionsByAgentKey[agent.key] = sessions
            if (conversationKey == agent.key && expanded) updatePanelContent()
        }
    }
}
