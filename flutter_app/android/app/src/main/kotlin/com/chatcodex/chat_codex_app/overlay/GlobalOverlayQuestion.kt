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
import com.chatcodex.chat_codex_app.R
import org.json.JSONArray
import org.json.JSONObject

/**
 * Agent 提问面板：当 Agent 向用户发问时，在对话面板底部弹出的选择卡片。
 *
 * 负责解析服务端下发的 question 数据、渲染单选/多选/自定义输入，
 * 以及把用户的回答（或拒绝）提交回服务端。
 */

/** 限制最大高度的滚动容器，用于提问卡片，避免遮挡整个屏幕。 */
internal class MaxHeightScrollView(
    context: Context,
    private val maxHeight: Int,
) : ScrollView(context) {
    override fun onMeasure(widthMeasureSpec: Int, heightMeasureSpec: Int) {
        val constrainedHeight = View.MeasureSpec.makeMeasureSpec(
            maxHeight,
            View.MeasureSpec.AT_MOST,
        )
        super.onMeasure(widthMeasureSpec, constrainedHeight)
    }
}

/** 构建提问面板：每题渲染标题、选项与自定义输入，底部为提交/拒绝操作。 */
internal fun GlobalOverlayService.buildQuestionPanel(question: OverlayQuestion): View {
    val submitting = question.taskId in questionSubmittingTaskIds
    val answers = questionDraftAnswersByTaskId.getOrPut(question.taskId) {
        MutableList(question.questions.size) { mutableListOf() }
    }
    while (answers.size < question.questions.size) answers.add(mutableListOf())
    val customAnswers = questionCustomAnswersByTaskId.getOrPut(question.taskId) {
        MutableList(question.questions.size) { "" }
    }
    while (customAnswers.size < question.questions.size) customAnswers.add("")
    val box = LinearLayout(this).apply {
        orientation = LinearLayout.VERTICAL
        setPadding(dp(16), dp(8), dp(16), dp(14))
        elevation = dp(12).toFloat()
        background = GradientDrawable().apply {
            setColor(Color.WHITE)
            cornerRadii = floatArrayOf(
                dp(14).toFloat(), dp(14).toFloat(),
                dp(14).toFloat(), dp(14).toFloat(),
                0f, 0f, 0f, 0f,
            )
        }
    }
    box.addView(View(this).apply {
        setBackgroundColor(0xFFD5E0EE.toInt())
    }, LinearLayout.LayoutParams(dp(36), dp(3)).also {
        it.gravity = Gravity.CENTER_HORIZONTAL
        it.bottomMargin = dp(10)
    })
    val heading = LinearLayout(this).apply { gravity = Gravity.CENTER_VERTICAL }
    heading.addView(TextView(this).apply {
        text = "?"
        gravity = Gravity.CENTER
        textSize = 11f
        typeface = Typeface.DEFAULT_BOLD
        setTextColor(0xFF2563EB.toInt())
        background = roundedBackground(Color.TRANSPARENT, 0xFF2563EB.toInt(), 8)
    }, LinearLayout.LayoutParams(dp(16), dp(16)))
    heading.addView(TextView(this).apply {
        text = "需要选择"
        textSize = 13f
        typeface = Typeface.DEFAULT_BOLD
        setTextColor(0xFF2563EB.toInt())
        includeFontPadding = false
        setPadding(dp(6), 0, 0, 0)
    }, LinearLayout.LayoutParams(0, dp(22), 1f))
    heading.addView(TextView(this).apply {
        text = "${question.questions.size} 题"
        gravity = Gravity.CENTER
        textSize = 11f
        typeface = Typeface.DEFAULT_BOLD
        setTextColor(0xFF2563EB.toInt())
        background = roundedBackground(0x142563EB.toInt(), 0x002563EB, 6)
        setPadding(dp(7), 0, dp(7), 0)
    }, LinearLayout.LayoutParams(-2, dp(24)))
    box.addView(heading)
    box.addView(TextView(this).apply {
        text = if (submitting) "回答已提交，Agent 正在继续工作" else "请完成下面的问题后提交，回答会继续当前任务。"
        textSize = 11f
        setTextColor(if (submitting) 0xFF16A34A.toInt() else 0xFF4A5D7A.toInt())
        setPadding(0, dp(5), 0, dp(10))
    })
    question.questions.forEachIndexed { index, item ->
        box.addView(TextView(this).apply {
            text = item.header.ifBlank { "问题 ${index + 1}" }
            textSize = 12f
            typeface = Typeface.DEFAULT_BOLD
            setTextColor(0xFF0F1729.toInt())
            includeFontPadding = false
        })
        box.addView(TextView(this).apply {
            text = item.question
            textSize = 13f
            setTextColor(0xFF4A5D7A.toInt())
            setPadding(0, dp(4), 0, dp(6))
            setTextIsSelectable(true)
        })
        item.options.forEach { option ->
            val optionText = option.label + option.description.takeIf { it.isNotBlank() }
                ?.let { "\n$it" }.orEmpty()
            if (item.multiple) {
                box.addView(CheckBox(this).apply {
                    text = optionText
                    textSize = 12f
                    setTextColor(0xFF0F1729.toInt())
                    buttonTintList = ColorStateList.valueOf(0xFF2563EB.toInt())
                    isChecked = answers[index].contains(option.label)
                    isEnabled = !submitting
                    setPadding(0, 0, 0, 0)
                    setOnCheckedChangeListener { _, checked ->
                        if (checked) {
                            if (!answers[index].contains(option.label)) answers[index].add(option.label)
                        } else {
                            answers[index].remove(option.label)
                        }
                    }
                }, LinearLayout.LayoutParams(-1, -2).also { it.topMargin = dp(2) })
            } else {
                box.addView(RadioButton(this).apply {
                    text = optionText
                    textSize = 12f
                    setTextColor(0xFF0F1729.toInt())
                    buttonTintList = ColorStateList.valueOf(0xFF2563EB.toInt())
                    isChecked = answers[index].firstOrNull() == option.label
                    isEnabled = !submitting
                    setPadding(0, 0, 0, 0)
                    setOnClickListener {
                        answers[index].clear()
                        answers[index].add(option.label)
                        if (index < customAnswers.size) customAnswers[index] = ""
                        updatePanelContent()
                    }
                }, LinearLayout.LayoutParams(-1, -2).also { it.topMargin = dp(2) })
            }
        }
        if (item.custom) {
            val custom = EditText(this).apply {
                hint = "输入其他回复"
                textSize = 13f
                setText(customAnswers[index])
                setTextColor(0xFF0F1729.toInt())
                setHintTextColor(0xFF8FA3BF.toInt())
                isEnabled = !submitting
                minLines = 1
                maxLines = 3
                inputType = InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_FLAG_MULTI_LINE
                setPadding(dp(9), dp(5), dp(9), dp(5))
                setCompoundDrawablesWithIntrinsicBounds(R.drawable.ic_overlay_edit, 0, 0, 0)
                compoundDrawablePadding = dp(4)
                background = roundedBackground(Color.WHITE, 0xFFD0DDEF.toInt(), 10)
                addTextChangedListener(object : TextWatcher {
                    override fun beforeTextChanged(s: CharSequence?, start: Int, count: Int, after: Int) = Unit
                    override fun onTextChanged(s: CharSequence?, start: Int, before: Int, count: Int) {
                        customAnswers[index] = s?.toString().orEmpty()
                        if (!item.multiple && customAnswers[index].trim().isNotEmpty()) answers[index].clear()
                    }
                    override fun afterTextChanged(s: Editable?) = Unit
                })
            }
            box.addView(custom, LinearLayout.LayoutParams(-1, dp(42)).also { it.topMargin = dp(6) })
        }
        if (index < question.questions.lastIndex) {
            box.addView(View(this).apply { setBackgroundColor(0xFFE8EFF8.toInt()) }, LinearLayout.LayoutParams(-1, dp(1)).also {
                it.topMargin = dp(12)
                it.bottomMargin = dp(12)
            })
        }
    }
    val actions = LinearLayout(this).apply {
        gravity = Gravity.CENTER_VERTICAL
        setPadding(0, dp(10), 0, 0)
    }
    actions.addView(TextView(this).apply {
        text = if (submitting) "等待中…" else "提交选择"
        textSize = 12f
        gravity = Gravity.CENTER
        typeface = Typeface.DEFAULT_BOLD
        setTextColor(if (submitting) 0xFF94A3B8.toInt() else Color.WHITE)
        background = roundedBackground(
            if (submitting) 0xFFE5E7EB.toInt() else 0xFF2563EB.toInt(),
            if (submitting) 0xFFD1D5DB.toInt() else 0xFF2563EB.toInt(),
            10,
        )
        isEnabled = !submitting
        setOnClickListener {
            val submitted = question.questions.mapIndexed { questionIndex, _ ->
                val result = answers[questionIndex].toMutableList()
                val custom = customAnswers[questionIndex].trim()
                if (custom.isNotEmpty() && !result.contains(custom)) result.add(custom)
                result
            }
            if (submitted.any { it.isEmpty() }) {
                toast("请先回答全部问题")
            } else {
                submitOverlayQuestion(question, submitted)
            }
        }
    }, LinearLayout.LayoutParams(0, dp(38), 1f))
    actions.addView(TextView(this).apply {
        text = "取消/拒绝"
        textSize = 12f
        gravity = Gravity.CENTER
        typeface = Typeface.DEFAULT_BOLD
        setTextColor(if (submitting) 0xFF94A3B8.toInt() else 0xFF2563EB.toInt())
        background = roundedBackground(Color.WHITE, 0xFFD0DDEF.toInt(), 10)
        isEnabled = !submitting
        setOnClickListener { submitOverlayQuestion(question, emptyList(), rejected = true) }
    }, LinearLayout.LayoutParams(0, dp(38), 1f).also { it.leftMargin = dp(8) })
    box.addView(actions)
    val shouldAnimate = animatedQuestionRequestIds.add(question.requestId)
    return MaxHeightScrollView(
        this,
        (panelHeightPx * 0.72f).toInt().coerceAtLeast(dp(260)),
    ).apply {
        isFillViewport = false
        isVerticalScrollBarEnabled = true
        overScrollMode = View.OVER_SCROLL_IF_CONTENT_SCROLLS
        elevation = dp(12).toFloat()
        addView(box, FrameLayout.LayoutParams(-1, -2))
        if (shouldAnimate) {
            alpha = 0f
            translationY = dp(28).toFloat()
            post {
                animate()
                    .alpha(1f)
                    .translationY(0f)
                    .setDuration(280L)
                    .setInterpolator(DecelerateInterpolator())
                    .start()
            }
        }
    }
}

/** 提交提问回答（rejected 为 true 时表示拒绝回答，Agent 将结束当前任务）。 */
internal fun GlobalOverlayService.submitOverlayQuestion(
    question: OverlayQuestion,
    answers: List<List<String>>,
    rejected: Boolean = false,
) {
    if (question.taskId in questionSubmittingTaskIds) return
    questionSubmittingTaskIds.add(question.taskId)
    refreshOverlayContent()
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
            refreshOverlayContent()
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
