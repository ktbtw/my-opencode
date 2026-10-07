package com.chatcodex.chat_codex_app.overlay

import android.util.Log
import android.view.Gravity
import android.view.View
import android.widget.LinearLayout
import android.widget.TextView
import org.json.JSONArray
import org.json.JSONObject

/**
 * Agent 状态与任务完成队列。
 *
 * 负责处理 SSE 下发的设备快照 / Agent 增删 / 任务更新事件，维护 Agent 列表与
 * 最近发送消息状态，并管理「任务已完成」提示的队列（当前展示 / 排队 / 稍后处理）。
 */

/** 应用设备快照（整体替换 Agent 列表）。 */
internal fun GlobalOverlayService.applySnapshot(machines: JSONArray) {
    val next = buildList {
        for (i in 0 until machines.length()) {
            val machine = machines.optJSONObject(i) ?: continue
            val machineId = machine.optString("machine_id")
            val deviceName = machine.optString("display_name")
                .ifBlank { machine.optString("hostname").ifBlank { machineId } }
            val list = machine.optJSONArray("agents") ?: JSONArray()
            for (j in 0 until list.length()) {
                parseAgent(list.optJSONObject(j), machineId, deviceName)?.let(::add)
            }
        }
    }
    if (agents == next) {
        updateBubble()
        return
    }
    agents = next
    normalizeOnlineSelection()
    refreshOverlayContent()
}

/** 从 JSON 解析单个 Agent（launcher 类型忽略，返回 null）。 */
internal fun GlobalOverlayService.parseAgent(item: JSONObject?, machineIdFallback: String = "", deviceNameFallback: String = ""): AgentState? {
    item ?: return null
    if (item.optString("kind") == "launcher") return null
    val machineId = item.optString("machine_id").ifBlank { machineIdFallback }
    val deviceName = deviceNameFallback.ifBlank { item.optString("hostname").ifBlank { machineId } }
    val project = item.optJSONArray("projects")?.optJSONObject(0)
    val projectId = item.optString("project_id").ifBlank { project?.optString("project_id").orEmpty() }
    val root = item.optString("project_root").ifBlank { project?.optString("root").orEmpty() }
    val fallback = root.replace('\\', '/').substringAfterLast('/').ifBlank { projectId }
    return AgentState(
        machineId, deviceName, item.optString("agent_id"),
        item.optString("name").ifBlank { fallback }, projectId, root,
        item.optString("status", "online"), item.optString("current_task_id"),
    )
}

/** 新增或更新单个 Agent。 */
internal fun GlobalOverlayService.upsertAgent(data: JSONObject) {
    val next = parseAgent(data) ?: return
    val index = agents.indexOfFirst { it.agentId == next.agentId && it.machineId == next.machineId }
    if (index >= 0 && agents[index] == next) {
        updateBubble()
        return
    }
    agents = if (index < 0) agents + next else agents.toMutableList().apply { this[index] = next }
    normalizeOnlineSelection()
    refreshOverlayContent()
}

/** 移除指定 Agent。 */
internal fun GlobalOverlayService.removeAgent(machineId: String, agentId: String) {
    agents = agents.filterNot { it.agentId == agentId && (machineId.isBlank() || it.machineId == machineId) }
    normalizeOnlineSelection()
    refreshOverlayContent()
}

/** 清理已失效的选中项与对话项（对应 Agent 已离线时）。 */
internal fun GlobalOverlayService.normalizeOnlineSelection() {
    if (selectedKey.isNotBlank() && agents.none { it.online && it.key == selectedKey }) selectedKey = ""
    if (conversationKey.isNotBlank() && agents.none { it.online && it.key == conversationKey }) {
        conversationKey = ""
        conversationScrollY = 0
        renderedConversationKey = ""
    }
}

/** 应用任务更新事件：维护提问、发送状态、Agent 忙碌态与完成队列。 */
internal fun GlobalOverlayService.applyTaskUpdate(task: JSONObject) {
    val taskId = task.optString("task_id")
    val agentId = task.optString("agent_id")
    val machineId = task.optString("machine_id")
    val projectId = task.optString("project_id")
    val status = task.optString("status").lowercase()
    Log.i(
        TAG,
        "task update taskId=$taskId status=$status agentId=$agentId " +
            "foreground=${isChatCodexForeground()} seen=${seenCompletionTaskIds.contains(taskId)}",
    )
    val matchingAgents = agents.filter { agent ->
        agent.agentId == agentId &&
            (machineId.isBlank() || agent.machineId == machineId) &&
            (projectId.isBlank() || agent.projectId == projectId)
    }
    val parsedQuestion = parseOverlayQuestion(task, matchingAgents)
    if (parsedQuestion != null) {
        val previous = pendingQuestionsByTaskId[taskId]
        pendingQuestionsByTaskId[taskId] = parsedQuestion
        if (previous?.requestId != parsedQuestion.requestId) {
            questionDraftAnswersByTaskId.remove(taskId)
            questionCustomAnswersByTaskId.remove(taskId)
            questionSubmittingTaskIds.remove(taskId)
        }
    } else if (taskId.isNotBlank() && pendingQuestionsByTaskId.remove(taskId) != null) {
        questionDraftAnswersByTaskId.remove(taskId)
        questionCustomAnswersByTaskId.remove(taskId)
        questionSubmittingTaskIds.remove(taskId)
    }
    val active = status in setOf("pending", "dispatched", "started", "running", "cancelling", "waiting_approval", "question_asked") ||
        parsedQuestion != null
    matchingAgents.forEach { agent ->
        val sentTaskId = latestSentTaskIdsByAgentKey[agent.key]
        if (sentTaskId != null && sentTaskId != taskId) {
            latestSentMessagesByAgentKey.remove(agent.key)
            latestSentStatesByAgentKey.remove(agent.key)
            latestSentTaskIdsByAgentKey.remove(agent.key)
        } else if (sentTaskId == taskId && !active) {
            latestSentMessagesByAgentKey.remove(agent.key)
            latestSentStatesByAgentKey.remove(agent.key)
            latestSentTaskIdsByAgentKey.remove(agent.key)
        }
    }
    agents = agents.map { agent ->
        if (matchingAgents.any { it.key == agent.key }) agent.copy(taskId = if (active) taskId else "") else agent
    }
    var completionAdded = false
    if (status == "completed" && taskId.isNotBlank() && seenCompletionTaskIds.add(taskId)) {
        completionAdded = true
        while (seenCompletionTaskIds.size > 100) seenCompletionTaskIds.remove(seenCompletionTaskIds.first())
        val agentName = agents.firstOrNull { it.agentId == agentId }?.name.orEmpty()
        val result = task.optString("result")
        agents.firstOrNull { it.agentId == agentId }?.let { agent ->
            if (result.isNotBlank()) {
                latestRepliesByAgentKey[agent.key] = result
                latestReplyLoadedKeys.add(agent.key)
            }
        }
        val item = CompletionItem(
            if (agentName.isBlank()) "Agent 已完成" else "$agentName 已完成",
            result,
            taskId,
            task.optString("session_id"),
            agentId,
            task.optString("machine_id"),
            task.optString("project_id"),
            task.optString("project_root"),
            result,
        )
        if (expanded && composing) deferredCompletions.addLast(item)
        else if (currentCompletion == null) currentCompletion = item
        else completions.addLast(item)
        Log.i(TAG, "task completion accepted taskId=$taskId summaryLength=${result.length}")
        showQueuePrompt = false
    }
    val conversationOpen = expanded && conversationKey.isNotBlank()
    if (conversationOpen && active && !completionAdded) updateBubble() else refreshOverlayContent()
}

/** 关闭当前完成卡片。 */
internal fun GlobalOverlayService.dismissCurrentCompletion() {
    currentCompletion = null
}

/** 判断某条完成记录是否属于指定 Agent。 */
internal fun GlobalOverlayService.completionBelongsToAgent(item: CompletionItem, agent: AgentState): Boolean =
    item.agentId == agent.agentId &&
        (item.machineId.isBlank() || item.machineId == agent.machineId) &&
        (item.projectId.isBlank() || item.projectId == agent.projectId)

/** 在对话面板中确认「本轮已完成」，移除该 Agent 对应的完成记录。 */
internal fun GlobalOverlayService.acknowledgeCompletionFor(agent: AgentState) {
    if (currentCompletion?.let { completionBelongsToAgent(it, agent) } == true) {
        currentCompletion = null
        promoteNextCompletion()
    } else {
        completions.removeAll { completionBelongsToAgent(it, agent) }
        deferredCompletions.removeAll { completionBelongsToAgent(it, agent) }
    }
    showQueuePrompt = currentCompletion == null &&
        completions.isEmpty() &&
        deferredCompletions.isNotEmpty()
    refreshOverlayContent()
}

/** 从排队队列取出下一条完成记录作为当前展示。 */
internal fun GlobalOverlayService.promoteNextCompletion() {
    if (currentCompletion != null) return
    currentCompletion = if (completions.isEmpty()) null else completions.removeFirst()
    currentCompletion?.let {
        selectedKey = agents.firstOrNull { agent -> agent.online && agent.agentId == it.agentId }?.key.orEmpty()
    }
}

/** 从「稍后处理」队列取出下一条完成记录作为当前展示。 */
internal fun GlobalOverlayService.promoteDeferredCompletion() {
    if (currentCompletion != null || deferredCompletions.isEmpty()) return
    currentCompletion = deferredCompletions.removeFirst()
    currentCompletion?.let {
        selectedKey = agents.firstOrNull { agent -> agent.online && agent.agentId == it.agentId }?.key.orEmpty()
    }
}

/** 把当前完成记录移到「稍后处理」队列。 */
internal fun GlobalOverlayService.deferCurrentCompletion() {
    currentCompletion?.let { deferredCompletions.addLast(it) }
    currentCompletion = null
    promoteNextCompletion()
    showQueuePrompt = currentCompletion == null && completions.isEmpty() && deferredCompletions.isNotEmpty()
}

/** 构建「还有 N 个 Agent 已完成」的稍后处理提示卡片。 */
internal fun GlobalOverlayService.buildQueuePrompt(): View {
    val box = LinearLayout(this).apply {
        orientation = LinearLayout.VERTICAL
        setPadding(dp(10), dp(9), dp(10), dp(9))
        background = roundedBackground(0xFFFFFBEB.toInt(), 0xFFFCD34D.toInt(), 10)
    }
    box.addView(TextView(this).apply {
        text = "还有 ${deferredCompletions.size} 个 Agent 已完成"
        textSize = 13f
        typeface = android.graphics.Typeface.DEFAULT_BOLD
        setTextColor(0xFF92400E.toInt())
    })
    box.addView(TextView(this).apply {
        text = "可以现在继续查看，也可以稍后从悬浮图标继续处理。"
        textSize = 11f
        setTextColor(0xFF92400E.toInt())
        setPadding(0, dp(4), 0, dp(4))
    })
    val actions = LinearLayout(this).apply { gravity = Gravity.END }
    actions.addView(TextView(this).apply {
        text = "继续处理"
        textSize = 12f
        gravity = Gravity.CENTER
        setTextColor(0xFF92400E.toInt())
        setPadding(dp(12), 0, dp(12), 0)
        setOnClickListener {
            showQueuePrompt = false
            promoteDeferredCompletion()
            refreshOverlayContent()
        }
    }, LinearLayout.LayoutParams(dp(86), dp(32)))
    actions.addView(TextView(this).apply {
        text = "稍后"
        textSize = 12f
        gravity = Gravity.CENTER
        setTextColor(0xFF4A5D7A.toInt())
        setOnClickListener {
            showQueuePrompt = false
            expanded = false
            refreshOverlayContent()
        }
    }, LinearLayout.LayoutParams(dp(52), dp(32)))
    box.addView(actions)
    return box.apply {
        layoutParams = LinearLayout.LayoutParams(-1, dp(106)).also { it.bottomMargin = dp(8) }
    }
}
