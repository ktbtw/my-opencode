package com.chatcodex.chat_codex_app.overlay

import android.app.AlertDialog
import android.content.Context
import android.content.Intent
import android.content.res.ColorStateList
import android.os.Build
import android.util.Base64
import android.util.TypedValue
import android.view.Gravity
import android.view.WindowManager
import android.widget.Button
import android.widget.ImageView
import android.widget.LinearLayout
import android.widget.TextView
import android.widget.Toast
import org.json.JSONArray
import org.json.JSONObject
import java.util.concurrent.TimeUnit

/**
 * 用户交互操作：发送消息、构建发送任务、关闭确认弹窗、附件选择与移除。
 */

/** 轻量 Toast 提示。 */
internal fun GlobalOverlayService.toast(message: String) = Toast.makeText(this, message, Toast.LENGTH_SHORT).show()

/** 发送当前输入框内容（或附件）给选中的 Agent。 */
internal fun GlobalOverlayService.sendCurrentMessage() {
    sendTextMessage(input?.text?.toString()?.trim().orEmpty())
}

/** 用指定文本发送（Compose 悬浮层没有 EditText，直接传文本）。 */
internal fun GlobalOverlayService.sendTextMessage(text: String) {
    if (composing) return toast("消息正在发送")
    val completion = currentCompletion
    val agent = completion?.let { item ->
        agents.firstOrNull {
            it.online && it.agentId == item.agentId &&
                (item.machineId.isBlank() || it.machineId == item.machineId)
        }
    }
        ?: agents.firstOrNull { it.online && it.key == selectedKey }
        ?: agents.firstOrNull { it.online }
    if (agent == null) return toast("暂无可用 Agent")
    if (text.isEmpty() && attachments.isEmpty()) return toast("请输入消息或添加附件")
    composing = true
    updateComposingUi()
    executor.execute {
        val task = createTask(agent, text, completion)
        val failureMessage = lastApiError
        handler.post {
            composing = false
            updateComposingUi()
            if (task != null) {
                val outgoingMessage = text.ifBlank {
                    if (attachments.size == 1) "附件：${attachments.first().name}" else "已发送 ${attachments.size} 个附件"
                }
                val taskId = task.optString("task_id")
                latestSentMessagesByAgentKey[agent.key] = outgoingMessage
                latestSentStatesByAgentKey[agent.key] = "sent"
                latestSentTaskIdsByAgentKey[agent.key] = taskId
                newConversationKeys.remove(agent.key)
                task.optString("session_id").takeIf { it.isNotBlank() }?.let {
                    latestSessionIdsByAgentKey[agent.key] = it
                }
                agents = agents.map { current ->
                    if (current.key == agent.key) current.copy(taskId = taskId) else current
                }
                input?.setText("")
                toast("已发送给 ${agent.name.ifBlank { "Agent" }}")
                attachments.clear()
                if (completion != null) {
                    dismissCurrentCompletion()
                    promoteNextCompletion()
                    showQueuePrompt = currentCompletion == null && completions.isEmpty() && deferredCompletions.isNotEmpty()
                }
                refreshOverlayContent()
                scrollConversationToBottom()
                verifyTaskAfterSend(agent, taskId)
            } else {
                latestSentMessagesByAgentKey.remove(agent.key)
                latestSentStatesByAgentKey.remove(agent.key)
                latestSentTaskIdsByAgentKey.remove(agent.key)
                toast(failureMessage.ifBlank { "发送失败，请稍后重试" })
            }
        }
    }
}

/** 发送后延迟回查一次任务状态，纠正服务端异步更新滞后。 */
internal fun GlobalOverlayService.verifyTaskAfterSend(agent: AgentState, taskId: String) {
    executor.execute {
        runCatching { TimeUnit.MILLISECONDS.sleep(1800) }
        if (destroyed) return@execute
        val task = apiRequest("/api/tasks/$taskId") ?: return@execute
        handler.post {
            if (!destroyed) applyTaskUpdate(task)
        }
    }
}

/** 构建发送任务请求体（文本 + 附件 + 模型/权限元数据）。 */
internal fun GlobalOverlayService.createTask(agent: AgentState, text: String, continuation: CompletionItem? = null): JSONObject? {
    val parts = JSONArray().apply {
        put(JSONObject().put("type", "text").put("text", text))
        attachments.forEach { attachment ->
            val bytes = runCatching { contentResolver.openInputStream(attachment.uri)?.use { it.readBytes() } }.getOrNull()
            if (bytes != null) {
                val mime = contentResolver.getType(attachment.uri) ?: "application/octet-stream"
                val name = attachment.name.ifBlank { "attachment" }
                put(JSONObject().put("type", "file").put("mime", mime).put("filename", name)
                    .put("url", "data:$mime;base64,${Base64.encodeToString(bytes, Base64.NO_WRAP)}"))
            }
        }
    }
    val permissionMode = getSharedPreferences(FLUTTER_PREFS, Context.MODE_PRIVATE)
        .getString(PERMISSION_MODE_PREF_KEY, "ask")
        .orEmpty()
        .trim()
        .lowercase()
        .let { mode ->
            if (mode == "auto-approve" || mode == "deny") mode else "ask"
        }
    val metadata = JSONObject()
        .put("permission_mode", permissionMode)
    selectedModelsByAgentKey[agent.key].orEmpty().takeIf { it.isNotBlank() }?.let {
        metadata.put("model", it)
    }
    selectedVariantsByAgentKey[agent.key].orEmpty().takeIf { it.isNotBlank() }?.let {
        metadata.put("variant", it)
    }
    val body = JSONObject()
        .put("agent_id", agent.agentId)
        .put("project_id", agent.projectId)
        .put("parts", parts)
        .put("metadata", metadata)
    val sessionId = continuation?.sessionId?.takeIf { it.isNotBlank() }
        ?: latestSessionIdsByAgentKey[agent.key]?.takeIf { agent.key !in newConversationKeys }
    if (!sessionId.isNullOrBlank()) body.put("session_id", sessionId)
    val task = apiRequest("/api/tasks", "POST", body) ?: return null
    if (task.optString("task_id").isBlank()) {
        lastApiError = "服务响应缺少任务编号"
        return null
    }
    return task
}

/** 打开系统文件选择器（收起悬浮面板，避免遮挡）。 */
internal fun GlobalOverlayService.pickAttachment() {
    expanded = false
    renderOverlay()
    startActivity(Intent(this, OverlayFilePickerActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
}

/** 移除指定下标的附件。 */
internal fun GlobalOverlayService.removeAttachment(index: Int) {
    if (index !in attachments.indices) return
    attachments.removeAt(index)
    refreshOverlayContent()
}

/** 长按悬浮球弹出的「关闭全局助手」确认框。 */
internal fun GlobalOverlayService.showStopConfirmation() {
    if (closeConfirmationDialog?.isShowing == true) return
    lateinit var dialog: AlertDialog
    val content = LinearLayout(this).apply {
        orientation = LinearLayout.VERTICAL
        setPadding(dp(22), dp(20), dp(22), dp(16))
        background = roundedBackground(0xFFFFFFFF.toInt(), 14)
    }
    val header = LinearLayout(this).apply {
        orientation = LinearLayout.HORIZONTAL
        gravity = Gravity.CENTER_VERTICAL
    }
    val icon = ImageView(this).apply {
        setImageResource(android.R.drawable.ic_menu_close_clear_cancel)
        imageTintList = ColorStateList.valueOf(0xFFDC2626.toInt())
        background = roundedBackground(0xFFFEE2E2.toInt(), 10)
        setPadding(dp(10), dp(10), dp(10), dp(10))
    }
    header.addView(icon, LinearLayout.LayoutParams(dp(40), dp(40)))
    val title = TextView(this).apply {
        text = "关闭全局助手？"
        setTextColor(0xFF0F1729.toInt())
        setTextSize(TypedValue.COMPLEX_UNIT_SP, 17f)
        setTypeface(typeface, android.graphics.Typeface.BOLD)
        setPadding(dp(12), dp(1), 0, 0)
    }
    header.addView(title, LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f))
    content.addView(header)

    val message = TextView(this).apply {
        text = "关闭后将不再显示悬浮图标，也不会在应用启动时自动恢复。"
        setTextColor(0xFF4A5D7A.toInt())
        setTextSize(TypedValue.COMPLEX_UNIT_SP, 14f)
        setPadding(0, dp(14), 0, 0)
    }
    content.addView(message)

    val actions = LinearLayout(this).apply {
        orientation = LinearLayout.HORIZONTAL
        gravity = Gravity.END
        setPadding(0, dp(20), 0, 0)
    }
    val cancelButton = Button(this).apply {
        text = "取消"
        setAllCaps(false)
        setTextColor(0xFF2563EB.toInt())
        background = roundedBackground(0xFFFFFFFF.toInt(), 8, 0xFFD0DDEF.toInt())
        setPadding(dp(14), 0, dp(14), 0)
        setOnClickListener { dialog.dismiss() }
    }
    val closeButton = Button(this).apply {
        text = "关闭"
        setAllCaps(false)
        setTextColor(0xFFFFFFFF.toInt())
        background = roundedBackground(0xFFDC2626.toInt(), 8)
        setPadding(dp(16), 0, dp(16), 0)
        setOnClickListener {
            dialog.dismiss()
            getSharedPreferences(FLUTTER_PREFS, Context.MODE_PRIVATE)
                .edit()
                .putString(ENABLED_PREF_KEY, "false")
                .apply()
            disableOverlay()
        }
    }
    actions.addView(cancelButton, LinearLayout.LayoutParams(dp(76), dp(40)))
    actions.addView(closeButton, LinearLayout.LayoutParams(dp(76), dp(40)).apply {
        leftMargin = dp(10)
    })
    content.addView(actions)

    dialog = AlertDialog.Builder(this)
        .setView(content)
        .create()
    dialog.window?.setType(
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            WindowManager.LayoutParams.TYPE_APPLICATION_OVERLAY
        } else {
            @Suppress("DEPRECATION")
            WindowManager.LayoutParams.TYPE_PHONE
        },
    )
    dialog.setOnShowListener {
        dialog.window?.setBackgroundDrawableResource(android.R.color.transparent)
        dialog.window?.setLayout(
            (resources.displayMetrics.widthPixels * 0.82f).toInt(),
            WindowManager.LayoutParams.WRAP_CONTENT,
        )
    }
    dialog.setOnDismissListener {
        if (closeConfirmationDialog === dialog) closeConfirmationDialog = null
    }
    closeConfirmationDialog = dialog
    runCatching { dialog.show() }.onFailure {
        closeConfirmationDialog = null
        toast("关闭确认框显示失败，请从应用内关闭")
    }
}
