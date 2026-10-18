package com.chatcodex.chat_codex_app.overlay

import android.content.Context
import android.content.res.ColorStateList
import android.graphics.Color
import android.graphics.Typeface
import android.graphics.drawable.GradientDrawable
import android.net.Uri
import android.text.Editable
import android.text.InputType
import android.text.TextWatcher
import android.util.Log
import android.view.Gravity
import android.view.View
import android.view.animation.DecelerateInterpolator
import android.widget.CheckBox
import android.widget.EditText
import android.widget.FrameLayout
import android.widget.LinearLayout
import android.widget.RadioButton
import android.widget.ScrollView
import android.widget.TextView
import org.json.JSONArray
import org.json.JSONObject

internal fun GlobalOverlayService.submitOverlayQuestion(
    question: OverlayQuestion,
    answers: List<List<String>>,
    rejected: Boolean = false,
) {
    if (question.taskId in questionSubmittingTaskIds) return
    questionSubmittingTaskIds.add(question.taskId)
    updatePanelContent()
    executor.execute {
        val answerJson = JSONArray().apply {
            answers.forEach { answer ->
                put(JSONArray().apply { answer.forEach(::put) })
            }
        }
        val body = JSONObject()
            .put("request_id", question.requestId)
            .put("answers", answerJson)
            .put("rejected", rejected)
        val result = apiRequest("/api/tasks/${Uri.encode(question.taskId)}/question", "POST", body)
        val failureMessage = lastApiError
        handler.post {
            if (result != null) {
                toast(if (rejected) "已拒绝问题，Agent 将结束当前任务" else "回答已提交，Agent 继续工作")
            } else {
                questionSubmittingTaskIds.remove(question.taskId)
                toast(failureMessage.ifBlank { "回答提交失败，请稍后重试" })
            }
            updatePanelContent()
        }
    }
}

/** 判断某个提问是否属于指定 Agent。 */
internal fun GlobalOverlayService.questionBelongsToAgent(question: OverlayQuestion, agent: AgentState): Boolean {
    return question.agentKey == agent.key || (
        question.agentId == agent.agentId &&
            (question.machineId.isBlank() || question.machineId == agent.machineId) &&
            (question.projectId.isBlank() || question.projectId == agent.projectId)
        )
}

/** 从任务数据里解析出提问内容，非提问任务返回 null。 */
internal fun GlobalOverlayService.parseOverlayQuestion(
    task: JSONObject,
    matchingAgents: List<AgentState>,
): OverlayQuestion? {
    val taskId = task.optString("task_id")
    val question = task.optJSONObject("question") ?: return null
    val requestId = question.optString("request_id")
    if (taskId.isBlank() || requestId.isBlank()) return null
    val items = buildList {
        val questions = question.optJSONArray("questions") ?: JSONArray()
        for (index in 0 until questions.length()) {
            val item = questions.optJSONObject(index) ?: continue
            val options = buildList {
                val values = item.optJSONArray("options") ?: JSONArray()
                for (optionIndex in 0 until values.length()) {
                    val option = values.optJSONObject(optionIndex) ?: continue
                    val label = option.optString("label")
                    if (label.isNotBlank()) {
                        add(OverlayQuestionOption(label, option.optString("description")))
                    }
                }
            }
            add(
                OverlayQuestionItem(
                    item.optString("question"),
                    item.optString("header"),
                    options,
                    item.optBoolean("multiple", false),
                    item.optBoolean("custom", true),
                ),
            )
        }
    }
    if (items.isEmpty()) return null
    val machineId = task.optString("machine_id")
    val agentId = task.optString("agent_id")
    val projectId = task.optString("project_id")
    val agentKey = matchingAgents.firstOrNull()?.key
        ?: "$machineId\u0000$agentId\u0000$projectId"
    return OverlayQuestion(
        taskId,
        requestId,
        task.optString("session_id").ifBlank { question.optString("session_id") },
        agentKey,
        agentId,
        machineId,
        projectId,
        items,
    )
}
