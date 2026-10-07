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

/**
 * 历史会话面板与「最近回复」加载。
 *
 * 负责拉取指定 Agent 的会话列表、切换会话、新建对话，
 * 以及按 Agent / 会话维度加载最近一条任务的回复内容。
 */

/** 构建历史会话面板：会话列表 + 新建对话入口。 */
internal fun GlobalOverlayService.populateSessionPanel(panel: LinearLayout, agent: AgentState) {
    val sessions = sessionsByAgentKey[agent.key].orEmpty()
    val header = LinearLayout(this).apply {
        gravity = Gravity.CENTER_VERTICAL
        addView(ImageButton(this@populateSessionPanel).apply {
            setImageResource(R.drawable.ic_overlay_back)
            background = ColorDrawable(Color.TRANSPARENT)
            setPadding(dp(8), dp(8), dp(8), dp(8))
            contentDescription = "返回对话"
            setOnClickListener {
                sessionPanelKey = ""
                updatePanelContent()
            }
        }, LinearLayout.LayoutParams(dp(36), dp(48)))
        addView(TextView(this@populateSessionPanel).apply {
            text = "历史会话"
            textSize = 13f
            typeface = Typeface.DEFAULT_BOLD
            setTextColor(0xFF0F1729.toInt())
            includeFontPadding = false
            gravity = Gravity.CENTER_VERTICAL
        }, LinearLayout.LayoutParams(0, dp(48), 1f))
        addView(ImageButton(this@populateSessionPanel).apply {
            contentDescription = "新建对话"
            setImageResource(R.drawable.ic_overlay_add)
            background = roundedBackground(0xFFEFF6FF.toInt(), 0xFFEFF6FF.toInt(), 6)
            setPadding(dp(8), dp(8), dp(8), dp(8))
            setOnClickListener { startNewConversation(agent) }
        }, LinearLayout.LayoutParams(dp(36), dp(36)).also { it.rightMargin = dp(6) })
    }
    panel.addView(header)
    panel.addView(View(this).apply { setBackgroundColor(0xFFE2E8F0.toInt()) }, LinearLayout.LayoutParams(-1, dp(1)))
    val rows = LinearLayout(this).apply { orientation = LinearLayout.VERTICAL }
    if (sessions.isEmpty()) {
        rows.addView(TextView(this).apply {
            text = if (agent.key in sessionLoadingKeys) "正在加载历史对话..." else "暂无历史对话"
            textSize = 12f
            gravity = Gravity.CENTER
            setTextColor(0xFF94A3B8.toInt())
        }, LinearLayout.LayoutParams(-1, dp(72)))
    } else {
        sessions.forEach { session -> rows.addView(buildSessionOption(agent, session)) }
    }
    panel.addView(ScrollView(this).apply { addView(rows) }, LinearLayout.LayoutParams(-1, 0, 1f))
    panel.addView(buildResizeHandle())
}

/** 会话列表中的单行（摘要 + 时间），当前会话高亮。 */
internal fun GlobalOverlayService.buildSessionOption(agent: AgentState, session: OverlaySession): View =
    LinearLayout(this).apply {
        orientation = LinearLayout.VERTICAL
        gravity = Gravity.CENTER_VERTICAL
        setPadding(dp(10), dp(8), dp(10), dp(8))
        val selected = latestSessionIdsByAgentKey[agent.key] == session.sessionId && agent.key !in newConversationKeys
        background = roundedBackground(
            if (selected) 0xFFEFF6FF.toInt() else Color.TRANSPARENT,
            if (selected) 0xFF93B8F8.toInt() else Color.TRANSPARENT,
            7,
        )
        addView(TextView(this@buildSessionOption).apply {
            text = session.summary
            textSize = 12f
            includeFontPadding = false
            maxLines = 2
            ellipsize = android.text.TextUtils.TruncateAt.END
            setTextColor(if (selected) 0xFF1D4ED8.toInt() else 0xFF243247.toInt())
        })
        addView(TextView(this@buildSessionOption).apply {
            text = formatSessionTime(session.updatedAt)
            textSize = 10f
            includeFontPadding = false
            setTextColor(0xFF94A3B8.toInt())
            maxLines = 1
        })
        setOnClickListener {
            selectSession(agent, session)
        }
        layoutParams = LinearLayout.LayoutParams(-1, dp(60)).also {
            it.leftMargin = dp(4); it.rightMargin = dp(4); it.topMargin = dp(3)
        }
    }

/** 切换到指定历史会话，并加载其最近回复。 */
internal fun GlobalOverlayService.selectSession(agent: AgentState, session: OverlaySession) {
    sessionPanelKey = ""
    newConversationKeys.remove(agent.key)
    latestSessionIdsByAgentKey[agent.key] = session.sessionId
    latestReplyLoadedKeys.remove(agent.key)
    latestReplyLoadingKeys.remove(agent.key)
    latestRepliesByAgentKey.remove(agent.key)
    ensureSessionLatestReplyLoaded(agent, session.sessionId)
    conversationScrollY = 0
    refreshOverlayContent()
}

/** 新建对话：清空当前会话与回复缓存。 */
internal fun GlobalOverlayService.startNewConversation(agent: AgentState) {
    modelPickerPopup?.dismiss()
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
    conversationScrollY = 0
    toast("已新建对话")
    refreshOverlayContent()
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
