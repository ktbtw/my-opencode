package com.chatcodex.chat_codex_app.overlay

import android.graphics.Color
import android.graphics.Typeface
import android.graphics.drawable.ColorDrawable
import android.net.Uri
import android.text.Editable
import android.text.InputType
import android.text.TextWatcher
import android.view.Gravity
import android.view.View
import android.widget.EditText
import android.widget.ImageButton
import android.widget.ImageView
import android.widget.LinearLayout
import android.widget.PopupWindow
import android.widget.ScrollView
import android.widget.TextView
import com.chatcodex.chat_codex_app.R
import org.json.JSONArray
import org.json.JSONObject

internal fun GlobalOverlayService.selectedModelFor(agent: AgentState): OverlayModel? {
    val selectedRef = selectedModelsByAgentKey[agent.key].orEmpty()
    if (selectedRef.isBlank()) return null
    return modelsByMachineId[agent.machineId]?.firstOrNull { it.metaKey == selectedRef }
}

/** 构建思考强度选择入口（仅当模型支持变体时显示）。 */

internal fun GlobalOverlayService.selectVariant(agent: AgentState, variant: String) {
    val previous = selectedVariantsByAgentKey[agent.key].orEmpty()
    selectedVariantsByAgentKey[agent.key] = variant
    updatePanelContent()
    executor.execute {
        val saved = apiRequest(
            "/api/agents/${Uri.encode(agent.agentId)}/settings",
            "POST",
            JSONObject().put("selected_variant", variant),
        ) != null
        handler.post {
            if (destroyed || saved) return@post
            selectedVariantsByAgentKey[agent.key] = previous
            if (conversationKey == agent.key) updatePanelContent()
            toast("思考强度切换失败，请重试")
        }
    }
}

/** 思考强度英文值转中文展示文案。 */

internal fun GlobalOverlayService.thinkingVariantLabel(value: String): String = when (value.trim().lowercase()) {
    "", "_auto", "auto" -> "自动"
    "none", "off", "disabled" -> "关闭"
    "minimal" -> "最小"
    "low" -> "低"
    "medium" -> "中"
    "high" -> "高"
    "xhigh" -> "极高"
    "max" -> "最大"
    "enabled", "on" -> "开启"
    else -> value.trim()
}

/** 构建模型选择面板：搜索框 + 按 provider 分组的模型列表。 */
