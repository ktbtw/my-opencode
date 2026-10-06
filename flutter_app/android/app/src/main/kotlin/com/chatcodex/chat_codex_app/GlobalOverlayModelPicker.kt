package com.chatcodex.chat_codex_app

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
import org.json.JSONArray
import org.json.JSONObject

/**
 * 模型与思考强度选择：对话输入区的模型入口、模型选择面板、
 * 思考强度变体菜单，以及模型/设置数据的加载与解析。
 */

/** 构建输入区的模型选择入口按钮（未选择时显示「默认模型」）。 */
internal fun GlobalOverlayService.buildModelSelector(agent: AgentState): View {
    val models = modelsByMachineId[agent.machineId]
    val settingsLoaded = agent.key in modelSettingsLoadedKeys
    val selectedRef = selectedModelsByAgentKey[agent.key].orEmpty()
    val selectedModel = models?.firstOrNull { it.metaKey == selectedRef }
    val loading = agent.machineId in modelLoadingMachineIds || agent.key in modelSettingsLoadingKeys
    val error = modelErrorsByMachineId[agent.machineId].orEmpty()
    val label = when {
        loading && (models == null || !settingsLoaded) -> "加载中"
        error.isNotBlank() && models == null -> "加载失败"
        selectedModel != null -> selectedModel.modelId
        selectedRef.isNotBlank() -> selectedRef.substringAfterLast('/')
        else -> "默认模型"
    }
    return LinearLayout(this).apply {
        gravity = Gravity.CENTER_VERTICAL
        setPadding(dp(7), 0, dp(6), 0)
        background = roundedBackground(
            if (selectedRef.isNotBlank()) 0xFFEFF6FF.toInt() else 0xFFF8FAFD.toInt(),
            if (selectedRef.isNotBlank()) 0xFF93B8F8.toInt() else 0xFFD0DDEF.toInt(),
            6,
        )
        contentDescription = "切换模型"
        addView(ImageView(this@buildModelSelector).apply {
            setImageResource(R.drawable.ic_overlay_model)
            if (selectedRef.isBlank()) setColorFilter(0xFF64748B.toInt(), android.graphics.PorterDuff.Mode.SRC_IN)
        }, LinearLayout.LayoutParams(dp(14), dp(14)))
        addView(TextView(this@buildModelSelector).apply {
            text = label
            textSize = 12f
            includeFontPadding = false
            gravity = Gravity.CENTER_VERTICAL
            maxLines = 1
            ellipsize = android.text.TextUtils.TruncateAt.END
            setPadding(dp(4), 0, dp(3), 0)
            setTextColor(if (selectedRef.isNotBlank()) 0xFF2563EB.toInt() else 0xFF64748B.toInt())
        }, LinearLayout.LayoutParams(0, -1, 1f))
        addView(ImageView(this@buildModelSelector).apply {
            setImageResource(R.drawable.ic_overlay_expand_more)
            if (selectedRef.isBlank()) setColorFilter(0xFF94A3B8.toInt(), android.graphics.PorterDuff.Mode.SRC_IN)
        }, LinearLayout.LayoutParams(dp(14), dp(14)))
        setOnClickListener {
            if (models == null) {
                ensureModelsLoaded(agent, retry = true)
                toast(if (loading) "正在加载模型" else "正在重新加载模型")
            } else {
                modelPickerKey = agent.key
                updatePanelContent()
            }
        }
    }.apply {
        layoutParams = LinearLayout.LayoutParams(dp(150), dp(30)).also { it.leftMargin = dp(6) }
    }
}

/** 当前 Agent 已选中的模型（未选择返回 null）。 */
internal fun GlobalOverlayService.selectedModelFor(agent: AgentState): OverlayModel? {
    val selectedRef = selectedModelsByAgentKey[agent.key].orEmpty()
    if (selectedRef.isBlank()) return null
    return modelsByMachineId[agent.machineId]?.firstOrNull { it.metaKey == selectedRef }
}

/** 构建思考强度选择入口（仅当模型支持变体时显示）。 */
internal fun GlobalOverlayService.buildVariantSelector(agent: AgentState, model: OverlayModel): View {
    val selected = selectedVariantsByAgentKey[agent.key]
        .orEmpty()
        .takeIf { value -> model.variants.any { it.equals(value, ignoreCase = true) } }
        .orEmpty()
    return LinearLayout(this).apply {
        gravity = Gravity.CENTER_VERTICAL
        setPadding(dp(7), 0, dp(7), 0)
        background = roundedBackground(
            if (selected.isNotBlank()) 0xFFEFF6FF.toInt() else 0xFFF8FAFD.toInt(),
            if (selected.isNotBlank()) 0xFF93B8F8.toInt() else 0xFFD0DDEF.toInt(),
            6,
        )
        addView(ImageView(this@buildVariantSelector).apply {
            setImageResource(R.drawable.ic_overlay_thinking)
            if (selected.isBlank()) setColorFilter(0xFF64748B.toInt(), android.graphics.PorterDuff.Mode.SRC_IN)
        }, LinearLayout.LayoutParams(dp(14), dp(14)))
        addView(TextView(this@buildVariantSelector).apply {
            text = thinkingVariantLabel(selected)
            textSize = 11f
            includeFontPadding = false
            gravity = Gravity.CENTER_VERTICAL
            setPadding(dp(4), 0, 0, 0)
            setTextColor(if (selected.isNotBlank()) 0xFF2563EB.toInt() else 0xFF64748B.toInt())
        })
        setOnClickListener { showVariantMenu(this, agent, model, selected) }
        layoutParams = LinearLayout.LayoutParams(-2, dp(30)).also { it.leftMargin = dp(6) }
    }
}

/** 弹出思考强度变体选择菜单。 */
internal fun GlobalOverlayService.showVariantMenu(anchor: View, agent: AgentState, model: OverlayModel, selected: String) {
    modelPickerPopup?.dismiss()
    val content = LinearLayout(this).apply {
        orientation = LinearLayout.VERTICAL
        setPadding(dp(6), dp(6), dp(6), dp(6))
        background = roundedBackground(Color.WHITE, 0xFFCBD8E8.toInt(), 10)
    }
    val options = listOf("") + model.variants
    options.forEach { variant ->
        content.addView(TextView(this).apply {
            text = thinkingVariantLabel(variant)
            textSize = 13f
            gravity = Gravity.CENTER_VERTICAL
            typeface = if (variant == selected) Typeface.DEFAULT_BOLD else Typeface.DEFAULT
            setTextColor(if (variant == selected) 0xFF2563EB.toInt() else 0xFF243247.toInt())
            setPadding(dp(12), 0, dp(12), 0)
            setBackgroundColor(if (variant == selected) 0xFFEFF6FF.toInt() else Color.TRANSPARENT)
            setOnClickListener { selectVariant(agent, variant) }
        }, LinearLayout.LayoutParams(dp(150), dp(42)))
    }
    modelPickerPopup = PopupWindow(content, dp(162), -2, true).apply {
        setBackgroundDrawable(ColorDrawable(Color.TRANSPARENT))
        isOutsideTouchable = true
        setOnDismissListener { modelPickerPopup = null }
        showAsDropDown(anchor, 0, -dp(42 * (options.size + 1)))
    }
}

/** 切换思考强度并同步到服务端（失败回滚）。 */
internal fun GlobalOverlayService.selectVariant(agent: AgentState, variant: String) {
    val previous = selectedVariantsByAgentKey[agent.key].orEmpty()
    selectedVariantsByAgentKey[agent.key] = variant
    modelPickerPopup?.dismiss()
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
internal fun GlobalOverlayService.populateModelPickerPanel(panel: LinearLayout, agent: AgentState) {
    val models = modelsByMachineId[agent.machineId].orEmpty()
    val currentRef = selectedModelsByAgentKey[agent.key].orEmpty()
    val header = LinearLayout(this).apply {
        gravity = Gravity.CENTER_VERTICAL
        addView(ImageButton(this@populateModelPickerPanel).apply {
            setImageResource(R.drawable.ic_overlay_back)
            background = ColorDrawable(Color.TRANSPARENT)
            setPadding(dp(8), dp(8), dp(8), dp(8))
            contentDescription = "返回对话"
            setOnClickListener {
                modelPickerKey = ""
                updatePanelContent()
            }
        }, LinearLayout.LayoutParams(dp(36), dp(44)))
        addView(TextView(this@populateModelPickerPanel).apply {
            text = "选择模型"
            textSize = 16f
            typeface = Typeface.DEFAULT_BOLD
            setTextColor(0xFF0F1729.toInt())
            includeFontPadding = false
            gravity = Gravity.CENTER_VERTICAL
        }, LinearLayout.LayoutParams(0, dp(44), 1f))
        if (currentRef.isNotBlank()) addView(TextView(this@populateModelPickerPanel).apply {
            text = "重置为默认"
            textSize = 12f
            gravity = Gravity.CENTER
            includeFontPadding = false
            setTextColor(0xFF2563EB.toInt())
            setOnClickListener { selectModel(agent, null) }
        }, LinearLayout.LayoutParams(dp(88), dp(40)))
    }
    panel.addView(header)
    val rows = LinearLayout(this).apply { orientation = LinearLayout.VERTICAL }
    fun renderOptions(query: String) {
        rows.removeAllViews()
        val normalized = query.trim().lowercase()
        val filtered = if (normalized.isBlank()) models else models.filter {
            it.name.lowercase().contains(normalized) || it.metaKey.lowercase().contains(normalized)
        }
        if (filtered.isEmpty()) {
            rows.addView(TextView(this).apply {
                text = if (models.isEmpty()) "暂无可用模型\n请确保 opencode 服务已启动" else "无匹配模型"
                textSize = 13f
                gravity = Gravity.CENTER
                setTextColor(0xFF94A3B8.toInt())
            }, LinearLayout.LayoutParams(-1, dp(120)))
        } else {
            filtered.groupBy { it.providerId }.forEach { (provider, providerModels) ->
                rows.addView(TextView(this).apply {
                    text = provider
                    textSize = 11f
                    typeface = Typeface.DEFAULT_BOLD
                    includeFontPadding = false
                    setTextColor(0xFF94A3B8.toInt())
                    setPadding(dp(10), dp(12), dp(10), dp(4))
                })
                providerModels.forEach { model ->
                    rows.addView(buildModelOption(agent, model, currentRef == model.metaKey))
                }
            }
        }
    }
    panel.addView(EditText(this).apply {
        hint = "搜索模型..."
        textSize = 12f
        setSingleLine(true)
        inputType = InputType.TYPE_CLASS_TEXT
        setCompoundDrawablesWithIntrinsicBounds(R.drawable.ic_overlay_search, 0, 0, 0)
        compoundDrawablePadding = dp(6)
        setPadding(dp(10), 0, dp(10), 0)
        background = roundedBackground(0xFFF8FAFD.toInt(), 0xFFD0DDEF.toInt(), 8)
        addTextChangedListener(object : TextWatcher {
            override fun beforeTextChanged(text: CharSequence?, start: Int, count: Int, after: Int) = Unit
            override fun onTextChanged(text: CharSequence?, start: Int, before: Int, count: Int) {
                renderOptions(text?.toString().orEmpty())
            }
            override fun afterTextChanged(text: Editable?) = Unit
        })
    }, LinearLayout.LayoutParams(-1, dp(40)).also {
        it.leftMargin = dp(4); it.rightMargin = dp(4); it.bottomMargin = dp(8)
    })
    renderOptions("")
    panel.addView(View(this).apply { setBackgroundColor(0xFFE2E8F0.toInt()) }, LinearLayout.LayoutParams(-1, dp(1)))
    panel.addView(ScrollView(this).apply { addView(rows) }, LinearLayout.LayoutParams(-1, 0, 1f))
    panel.addView(buildResizeHandle())
}

/** 模型列表中的单个选项行。 */
internal fun GlobalOverlayService.buildModelOption(agent: AgentState, model: OverlayModel, selected: Boolean): View =
    LinearLayout(this).apply {
        gravity = Gravity.CENTER_VERTICAL
        setPadding(dp(14), dp(6), dp(10), dp(6))
        setBackgroundColor(if (selected) 0xFFEFF6FF.toInt() else Color.TRANSPARENT)
        addView(LinearLayout(this@buildModelOption).apply {
            orientation = LinearLayout.VERTICAL
            gravity = Gravity.CENTER_VERTICAL
            addView(TextView(this@buildModelOption).apply {
                text = model.modelId
                textSize = 13f
                includeFontPadding = false
                typeface = if (selected) Typeface.DEFAULT_BOLD else Typeface.DEFAULT
                maxLines = 1
                ellipsize = android.text.TextUtils.TruncateAt.END
                setTextColor(if (selected) 0xFF2563EB.toInt() else 0xFF243247.toInt())
            })
            if (model.name != model.modelId) addView(TextView(this@buildModelOption).apply {
                text = model.name
                textSize = 11f
                includeFontPadding = false
                maxLines = 1
                ellipsize = android.text.TextUtils.TruncateAt.END
                setTextColor(0xFF94A3B8.toInt())
            })
        }, LinearLayout.LayoutParams(0, -1, 1f))
        addView(TextView(this@buildModelOption).apply {
            text = if (selected) "✓" else ""
            textSize = 15f
            gravity = Gravity.CENTER
            includeFontPadding = false
            setTextColor(0xFF2563EB.toInt())
        }, LinearLayout.LayoutParams(dp(24), -1))
        setOnClickListener { selectModel(agent, model) }
        layoutParams = LinearLayout.LayoutParams(-1, dp(52))
    }

/** 选择模型（传 null 表示恢复设备默认），并同步到服务端（失败回滚）。 */
internal fun GlobalOverlayService.selectModel(agent: AgentState, model: OverlayModel?) {
    val oldModel = selectedModelsByAgentKey[agent.key].orEmpty()
    val oldVariant = selectedVariantsByAgentKey[agent.key].orEmpty()
    val nextModel = model?.metaKey.orEmpty()
    val nextVariant = oldVariant.takeIf { model != null && it in model.variants }.orEmpty()
    selectedModelsByAgentKey[agent.key] = nextModel
    selectedVariantsByAgentKey[agent.key] = nextVariant
    modelPickerKey = ""
    updatePanelContent()
    executor.execute {
        val body = JSONObject().put("selected_model", nextModel)
        if (nextVariant != oldVariant) body.put("selected_variant", nextVariant)
        val saved = apiRequest("/api/agents/${Uri.encode(agent.agentId)}/settings", "POST", body) != null
        handler.post {
            if (destroyed) return@post
            if (saved) {
                toast(if (model == null) "已使用设备默认模型" else "已切换到 ${model.name}")
            } else {
                selectedModelsByAgentKey[agent.key] = oldModel
                selectedVariantsByAgentKey[agent.key] = oldVariant
                if (conversationKey == agent.key) updatePanelContent()
                toast("模型切换失败，请重试")
            }
        }
    }
}

/** 解析 /api/models 响应，返回（模型列表, 默认模型）。 */
internal fun GlobalOverlayService.parseModelsResponse(raw: String): Pair<List<OverlayModel>, String>? = runCatching {
    val data = JSONObject(raw)
    val connected = buildSet {
        val values = data.optJSONArray("connected") ?: JSONArray()
        for (i in 0 until values.length()) values.optString(i).takeIf { it.isNotBlank() }?.let(::add)
    }
    val result = mutableListOf<OverlayModel>()
    val providers = data.optJSONArray("all") ?: JSONArray()
    for (i in 0 until providers.length()) {
        val provider = providers.optJSONObject(i) ?: continue
        val providerId = provider.optString("id")
        if (providerId.isBlank() || providerId !in connected) continue
        val providerName = provider.optString("name").ifBlank { providerId }
        val models = provider.optJSONObject("models") ?: continue
        val modelIds = models.keys()
        while (modelIds.hasNext()) {
            val modelId = modelIds.next()
            val model = models.optJSONObject(modelId) ?: continue
            if (model.optString("status", "active").equals("deprecated", ignoreCase = true)) continue
            val modelName = model.optString("name").ifBlank { modelId }
            val variants = buildSet {
                when (val rawVariants = model.opt("variants")) {
                    is JSONObject -> {
                        val keys = rawVariants.keys()
                        while (keys.hasNext()) keys.next().takeIf { it.isNotBlank() }?.let(::add)
                    }
                    is JSONArray -> for (j in 0 until rawVariants.length()) {
                        rawVariants.optString(j).takeIf { it.isNotBlank() }?.let(::add)
                    }
                }
            }
            result.add(OverlayModel(providerId, modelId, "$providerName / $modelName", variants))
        }
    }
    result.sortWith(compareBy(String.CASE_INSENSITIVE_ORDER) { it.name })
    result to data.optJSONObject("default")?.optString("model").orEmpty()
}.getOrNull()

/** 加载指定设备的模型列表（可重试），并顺带加载 Agent 模型设置。 */
internal fun GlobalOverlayService.ensureModelsLoaded(agent: AgentState, retry: Boolean = false) {
    if (retry) modelErrorsByMachineId.remove(agent.machineId)
    if ((modelsByMachineId.containsKey(agent.machineId) || agent.machineId in modelLoadingMachineIds) && !retry) {
        ensureModelSettingsLoaded(agent)
        return
    }
    if (agent.machineId !in modelLoadingMachineIds) {
        modelLoadingMachineIds.add(agent.machineId)
        executor.execute {
            val path = "/api/models?machine_id=${Uri.encode(agent.machineId)}"
            val raw = apiRaw(path)
            val parsed = raw?.let(::parseModelsResponse)
            val error = lastApiError
            handler.post {
                modelLoadingMachineIds.remove(agent.machineId)
                if (parsed != null) {
                    modelsByMachineId[agent.machineId] = parsed.first
                    defaultModelByMachineId[agent.machineId] = parsed.second
                    modelErrorsByMachineId.remove(agent.machineId)
                } else {
                    modelErrorsByMachineId[agent.machineId] = error.ifBlank { "模型加载失败" }
                }
                if (conversationKey == agent.key && expanded) updatePanelContent()
            }
        }
    }
    ensureModelSettingsLoaded(agent)
}

/** 加载 Agent 的模型设置（当前选中模型与思考强度）。 */
internal fun GlobalOverlayService.ensureModelSettingsLoaded(agent: AgentState) {
    if (agent.key in modelSettingsLoadedKeys || !modelSettingsLoadingKeys.add(agent.key)) return
    executor.execute {
        val settings = apiRequest("/api/agents/${Uri.encode(agent.agentId)}/settings")
        handler.post {
            modelSettingsLoadingKeys.remove(agent.key)
            if (settings != null) {
                selectedModelsByAgentKey[agent.key] = settings.optString("selected_model").trim()
                selectedVariantsByAgentKey[agent.key] = settings.optString("selected_variant").trim()
                modelSettingsLoadedKeys.add(agent.key)
            }
            if (conversationKey == agent.key && expanded) updatePanelContent()
        }
    }
}
