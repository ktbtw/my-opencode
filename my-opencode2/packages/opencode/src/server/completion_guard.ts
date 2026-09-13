export type CompletionMode = "build" | "plan" | string

export type CompletionGuardState = {
  tools: Record<
    string,
    {
      name?: string
      status: string
      failed?: boolean
      completed?: boolean
    }
  >
  patches: Record<
    string,
    {
      files: string[]
    }
  >
  artifacts: Record<string, true>
  approvals: Record<string, "pending" | "resolved">
  questions: Record<string, "pending" | "resolved">
  todos: Record<
    string,
    {
      status: string
    }
  >
  mcpJobs: Record<
    string,
    {
      status: string
    }
  >
  roundStart: {
    completedTools: Record<string, true>
    patches: Record<string, true>
    artifacts: Record<string, true>
    completedTodos: Record<string, true>
    finalTextLength: number
  }
  model?: CompletionModelState
  retryCount: number
  stagnationCount: number
  missingArtifactRetryCount: number
  providerErrorRetryCount: number
  outputLengthRetryCount: number
  lastReason?: CompletionDecisionReason
}

export type CompletionModelState = {
  mode?: CompletionMode
  finalText?: string
  finalTextPreview?: string
  finishReason?: string
  usage?: Record<string, unknown>
}

export type CompletionCounts = {
  toolCount: number
  runningToolCount: number
  completedToolCount: number
  failedToolCount: number
  patchCount: number
  patchFileCount: number
  artifactCount: number
  pendingApprovalCount: number
  pendingQuestionCount: number
  unresolvedTodoCount: number
  completedTodoCount: number
  pendingMcpJobCount: number
  failedMcpJobCount: number
  roundCompletedToolCount: number
  roundPatchCount: number
  roundArtifactCount: number
  roundCompletedTodoCount: number
  retryCount: number
  stagnationCount: number
  missingArtifactRetryCount: number
  providerErrorRetryCount: number
  outputLengthRetryCount: number
  hasRealAction: boolean
  hasRoundProgress: boolean
}

export type CompletionDecisionReason =
  | "allowed"
  | "non_build_agent"
  | "goal_active"
  | "has_completed_tool"
  | "has_patch"
  | "has_artifact"
  | "missing_artifact"
  | "tool_failed_with_explanation"
  | "text_completion"
  | "direct_text_answer"
  | "promise_without_action"
  | "waiting_user_action"
  | "provider_error"
  | "output_length"
  | "content_filter"
  | "unresolved_todo"
  | "pending_mcp_job"
  | "continuation_limit"
  | "stalled"
  | "pending_tool"
  | "pending_approval"
  | "pending_question"
  | "failed_tool_without_explanation"
  | "no_real_action"
  | "no_real_action_after_retry"

export type CompletionDecision =
  | {
      type: "allow"
      reason: CompletionDecisionReason
      counts: CompletionCounts
      textSignal: CompletionTextSignal
    }
  | {
      type: "continue"
      reason: CompletionDecisionReason
      message: string
      counts: CompletionCounts
      textSignal: CompletionTextSignal
    }
  | {
      type: "fail"
      reason: CompletionDecisionReason
      error: string
      counts: CompletionCounts
      textSignal: CompletionTextSignal
    }

export type CompletionTextSignal = {
  promiseWithoutAction: boolean
  waitingUserAction: boolean
  hasFailureSummary: boolean
  hasCompletionSummary: boolean
  hasDeliveryClaim: boolean
}

export type CompletionMetadata = {
  source: "completion_guard"
  decision: CompletionDecision["type"]
  mode: CompletionMode
  reason: CompletionDecisionReason
  retry_count: number
  stagnation_count: number
  action_state: CompletionCounts
  final_text_preview: string
  finish_reason?: string
  usage?: Record<string, unknown>
  text_signal: CompletionTextSignal
}

export function createCompletionGuardState(): CompletionGuardState {
  return {
    tools: {},
    patches: {},
    artifacts: {},
    approvals: {},
    questions: {},
    todos: {},
    mcpJobs: {},
    roundStart: {
      completedTools: {},
      patches: {},
      artifacts: {},
      completedTodos: {},
      finalTextLength: 0,
    },
    retryCount: 0,
    stagnationCount: 0,
    missingArtifactRetryCount: 0,
    providerErrorRetryCount: 0,
    outputLengthRetryCount: 0,
  }
}

export function beginCompletionRound(state: CompletionGuardState) {
  state.roundStart = {
    completedTools: Object.fromEntries(
      Object.entries(state.tools)
        .filter(([, tool]) => tool.completed || tool.status === "completed")
        .map(([key]) => [key, true]),
    ),
    patches: Object.fromEntries(Object.keys(state.patches).map((key) => [key, true])),
    artifacts: Object.fromEntries(Object.keys(state.artifacts).map((key) => [key, true])),
    completedTodos: Object.fromEntries(
      Object.entries(state.todos)
        .filter(([, todo]) => todoComplete(todo.status))
        .map(([key]) => [key, true]),
    ),
    finalTextLength: state.model?.finalText?.length ?? 0,
  }
  state.model = undefined
}

export function rememberCompletionPart(state: CompletionGuardState, part: unknown) {
  if (!part || typeof part !== "object") return
  const value = part as Record<string, any>
  const id = stringValue(value.id) || stringValue(value.callID)
  if (value.type === "patch") {
    const files = Array.isArray(value.files) ? value.files.filter((item) => typeof item === "string") : []
    const key = `patch:${id || stringValue(value.hash) || files.join("|") || Object.keys(state.patches).length + 1}`
    state.patches[key] = { files }
    return
  }
  if (value.type !== "tool") return
  rememberMcpJob(state, value)
  const tool = stringValue(value.tool)
  if (!tool) return
  if (tool === "todowrite") {
    rememberTodos(state, value)
    return
  }
  const key = `tool:${id || tool}`
  const status = stringValue(value.state?.status) || "unknown"
  state.tools[key] = {
    name: tool,
    status,
    completed: status === "completed",
    failed: status === "error" || status === "failed",
  }
}

export function rememberCompletionArtifacts(
  state: CompletionGuardState,
  artifacts:
    | Array<{
        path?: string
        relativePath?: string
        relative_path?: string
        filename?: string
        name?: string
      }>
    | unknown[],
  files: unknown[],
) {
  for (const item of artifacts) {
    if (!item || typeof item !== "object") continue
    const value = item as Record<string, unknown>
    const key =
      stringValue(value.path) ||
      stringValue(value.relativePath) ||
      stringValue(value.relative_path) ||
      stringValue(value.filename) ||
      stringValue(value.name)
    if (key) state.artifacts[key] = true
  }
  for (const item of files) {
    if (!item || typeof item !== "object") continue
    const value = item as Record<string, unknown>
    const key = stringValue(value.path) || stringValue(value.filename) || stringValue(value.name)
    if (key) state.artifacts[key] = true
  }
}

export function markCompletionApproval(
  state: CompletionGuardState,
  id: string | undefined,
  status: "pending" | "resolved",
) {
  if (!id) return
  state.approvals[id] = status
}

export function markCompletionQuestion(
  state: CompletionGuardState,
  id: string | undefined,
  status: "pending" | "resolved",
) {
  if (!id) return
  state.questions[id] = status
}

export function incrementCompletionRetry(
  state: CompletionGuardState,
  reason: CompletionDecisionReason,
  counts?: CompletionCounts,
) {
  state.retryCount += 1
  state.stagnationCount = counts?.hasRoundProgress ? 0 : state.stagnationCount + 1
  state.missingArtifactRetryCount = reason === "missing_artifact" ? state.missingArtifactRetryCount + 1 : 0
  state.providerErrorRetryCount = reason === "provider_error" ? state.providerErrorRetryCount + 1 : 0
  state.outputLengthRetryCount = reason === "output_length" ? state.outputLengthRetryCount + 1 : 0
  state.lastReason = reason
}

export function rememberCompletionModel(
  state: CompletionGuardState,
  input: {
    mode?: CompletionMode
    finalText?: string
    finishReason?: unknown
    usage?: Record<string, unknown>
  },
) {
  const finalText = normalizeText(input.finalText ?? "")
  state.model = {
    ...(state.model ?? {}),
    mode: input.mode ?? state.model?.mode,
    finalText,
    finalTextPreview: finalText.slice(0, 800),
    finishReason: stringValue(input.finishReason) || state.model?.finishReason,
    usage: input.usage ?? state.model?.usage,
  }
}

export function completionCounts(state: CompletionGuardState): CompletionCounts {
  const tools = Object.values(state.tools)
  const patchValues = Object.values(state.patches)
  const completedToolCount = tools.filter((tool) => tool.completed || tool.status === "completed").length
  const failedToolCount = tools.filter(
    (tool) => tool.failed || tool.status === "error" || tool.status === "failed",
  ).length
  const runningToolCount = tools.filter((tool) => {
    if (tool.completed || tool.failed) return false
    return tool.status === "running" || tool.status === "pending" || tool.status === "unknown"
  }).length
  const patchFileCount = patchValues.reduce((count, patch) => count + Math.max(patch.files.length, 1), 0)
  const artifactCount = Object.keys(state.artifacts).length
  const completedTodoCount = Object.values(state.todos).filter((todo) => todoComplete(todo.status)).length
  const unresolvedTodoCount = Object.values(state.todos).filter((todo) => !todoComplete(todo.status)).length
  const pendingMcpJobCount = Object.values(state.mcpJobs).filter((job) => job.status === "running").length
  const failedMcpJobCount = Object.values(state.mcpJobs).filter(
    (job) => job.status === "failed" || job.status === "cancelled",
  ).length
  const roundCompletedToolCount = Object.entries(state.tools).filter(
    ([key, tool]) => (tool.completed || tool.status === "completed") && state.roundStart.completedTools[key] !== true,
  ).length
  const roundPatchCount = Object.keys(state.patches).filter((key) => state.roundStart.patches[key] !== true).length
  const roundArtifactCount = Object.keys(state.artifacts).filter(
    (key) => state.roundStart.artifacts[key] !== true,
  ).length
  const roundCompletedTodoCount = Object.entries(state.todos).filter(
    ([key, todo]) => todoComplete(todo.status) && state.roundStart.completedTodos[key] !== true,
  ).length
  const counts = {
    toolCount: tools.length,
    runningToolCount,
    completedToolCount,
    failedToolCount,
    patchCount: patchValues.length,
    patchFileCount,
    artifactCount,
    pendingApprovalCount: Object.values(state.approvals).filter((status) => status === "pending").length,
    pendingQuestionCount: Object.values(state.questions).filter((status) => status === "pending").length,
    unresolvedTodoCount,
    completedTodoCount,
    pendingMcpJobCount,
    failedMcpJobCount,
    roundCompletedToolCount,
    roundPatchCount,
    roundArtifactCount,
    roundCompletedTodoCount,
    retryCount: state.retryCount,
    stagnationCount: state.stagnationCount,
    missingArtifactRetryCount: state.missingArtifactRetryCount,
    providerErrorRetryCount: state.providerErrorRetryCount,
    outputLengthRetryCount: state.outputLengthRetryCount,
    hasRealAction: false,
    hasRoundProgress: false,
  }
  counts.hasRealAction =
    counts.toolCount > 0 || counts.patchCount > 0 || counts.artifactCount > 0 || Object.keys(state.mcpJobs).length > 0
  counts.hasRoundProgress = roundCompletedToolCount + roundPatchCount + roundArtifactCount + roundCompletedTodoCount > 0
  return counts
}

export function completionTextSignal(text: string): CompletionTextSignal {
  const value = normalizeText(text)
  return {
    promiseWithoutAction: hasForwardLookingIntent(value),
    waitingUserAction: hasWaitingUserAction(value),
    hasFailureSummary: hasFailureSummary(value),
    hasCompletionSummary: hasCompletionSummary(value),
    hasDeliveryClaim: hasDeliveryClaim(value),
  }
}

function hasDeliveryClaim(text: string) {
  if (!text) return false
  if (/(?:没有|未|暂无|不存在|失败).{0,12}(?:交付|下载|附件|文件)/i.test(text)) return false
  return /(?:\.chatcodex-artifacts[\\/]|交付文件|可下载(?:文件|包|链接)?|文件(?:已|已经)(?:生成|打包|交付|发送|保存)|(?:已|已经)(?:生成|打包|交付|发送).{0,24}(?:文件|压缩包|安装包)|deliver(?:ed|able|y)?|download(?:able)?|attachment|export(?:ed)?|package(?:d)?)/i.test(
    text,
  )
}

export function evaluateCompletion(input: {
  state: CompletionGuardState
  mode?: CompletionMode
  text: string
  goalActive?: boolean
  deliveryRequired?: boolean
}): CompletionDecision {
  const mode = input.mode ?? "build"
  const counts = completionCounts(input.state)
  const textSignal = completionTextSignal(input.text)
  const finishReason = normalizeFinishReason(input.state.model?.finishReason)
  const deliveryRequired = input.deliveryRequired === true || textSignal.hasDeliveryClaim

  if (finishReason === "length" && input.text.length > input.state.roundStart.finalTextLength) {
    counts.hasRoundProgress = true
  }

  if (mode !== "build") return { type: "allow", reason: "non_build_agent", counts, textSignal }

  if (finishReason === "content-filter") {
    return {
      type: "fail",
      reason: "content_filter",
      error: "模型输出被内容过滤器终止",
      counts,
      textSignal,
    }
  }
  if (finishReason === "error") {
    return continueOrFail(input.state, "provider_error", counts, textSignal)
  }
  if (finishReason === "length") {
    return continueOrFail(input.state, "output_length", counts, textSignal)
  }

  if (deliveryRequired && counts.artifactCount === 0) {
    return continueOrFail(input.state, "missing_artifact", counts, textSignal)
  }

  if (input.goalActive) return { type: "allow", reason: "goal_active", counts, textSignal }

  if (counts.pendingApprovalCount > 0) {
    return continueOrFail(input.state, "pending_approval", counts, textSignal)
  }
  if (counts.pendingQuestionCount > 0) {
    return continueOrFail(input.state, "pending_question", counts, textSignal)
  }
  if (counts.runningToolCount > 0) {
    return continueOrFail(input.state, "pending_tool", counts, textSignal)
  }
  if (counts.pendingMcpJobCount > 0) {
    return continueOrFail(input.state, "pending_mcp_job", counts, textSignal)
  }
  if (textSignal.waitingUserAction) {
    return continueOrFail(input.state, "waiting_user_action", counts, textSignal)
  }
  if (counts.unresolvedTodoCount > 0) {
    return continueOrFail(input.state, "unresolved_todo", counts, textSignal)
  }

  if (textSignal.promiseWithoutAction) {
    return continueOrFail(input.state, "promise_without_action", counts, textSignal)
  }

  if (counts.completedToolCount > 0) return { type: "allow", reason: "has_completed_tool", counts, textSignal }
  if (counts.patchCount > 0) return { type: "allow", reason: "has_patch", counts, textSignal }
  if (counts.artifactCount > 0) return { type: "allow", reason: "has_artifact", counts, textSignal }

  if (counts.failedToolCount > 0) {
    if (textSignal.hasFailureSummary) {
      return { type: "allow", reason: "tool_failed_with_explanation", counts, textSignal }
    }
    return continueOrFail(input.state, "failed_tool_without_explanation", counts, textSignal)
  }

  if (!counts.hasRealAction && input.state.retryCount > 0) {
    return continueOrFail(input.state, "no_real_action_after_retry", counts, textSignal)
  }

  if (textSignal.hasCompletionSummary && !textSignal.promiseWithoutAction) {
    return { type: "allow", reason: "text_completion", counts, textSignal }
  }

  if (normalizeText(input.text)) {
    return { type: "allow", reason: "direct_text_answer", counts, textSignal }
  }

  return continueOrFail(
    input.state,
    input.state.retryCount > 0 ? "no_real_action_after_retry" : "no_real_action",
    counts,
    textSignal,
  )
}

export function completionContinueParts(
  decision: Extract<CompletionDecision, { type: "continue" }>,
  artifactDir?: string,
) {
  if (decision.reason === "waiting_user_action") {
    return [
      {
        type: "text" as const,
        text: [
          "系统检测到当前任务需要用户先完成外部操作。",
          "请立即调用 question 工具发出一个简短的确认问题并等待用户回复；保持 custom 开启，让用户也可以直接输入实际情况。",
          "不要继续调用依赖该操作结果的工具，也不要只重复等待说明。收到回复后，再从当前计划继续执行。",
        ].join("\n"),
        synthetic: true,
        metadata: { completion_guard_continue: true, reason: decision.reason },
      },
    ]
  }
  return [
    {
      type: "text" as const,
      text: [
        "系统检测到上一轮 build 模式尚未达到可完成状态。",
        decision.message,
        "请继续完成用户任务，优先执行一个可验证的具体动作，例如读取/修改文件、运行命令、生成产物，或给出真实阻塞原因。",
        "不要只输出计划、承诺或下一步说明。",
        ...(decision.reason === "missing_artifact"
          ? [
              ...(artifactDir
                ? [
                    `当前任务专属交付目录为 "${artifactDir}/"。只认并写入这个目录，不要复用其他 task_... 目录中的文件。`,
                  ]
                : []),
              "上一轮面向用户的正文尚未展示。完成交付文件后，请重新输出一份完整、自洽、可独立阅读的最终答复，只说明最终结果和文件名，不要写出路径。",
              "不要引用上一轮答复，不要描述内部校验或修正过程。",
            ]
          : []),
      ].join("\n"),
      synthetic: true,
      metadata: { completion_guard_continue: true, reason: decision.reason },
    },
  ]
}

export function completionFailureDetail(input: {
  state: CompletionGuardState
  decision: CompletionDecision
  agent?: string
  model?: { providerID?: string; modelID?: string }
  variant?: string
  text: string
  promptAttempts?: unknown[]
}) {
  const metadata = completionMetadata({
    state: input.state,
    decision: input.decision,
    mode: input.agent ?? input.state.model?.mode ?? "build",
    text: input.text,
  })
  return JSON.stringify(
    {
      reason: input.decision.reason,
      agent: input.agent ?? "build",
      model: input.model
        ? {
            provider_id: input.model.providerID,
            model_id: input.model.modelID,
          }
        : undefined,
      variant: input.variant,
      decision: metadata.decision,
      action_state: metadata.action_state,
      text_signal: metadata.text_signal,
      retry_count: metadata.retry_count,
      finish_reason: metadata.finish_reason,
      usage: metadata.usage,
      prompt_attempts: input.promptAttempts ?? [],
      final_text_length: normalizeText(input.text).length,
    },
    null,
    2,
  )
}

export function completionMetadata(input: {
  state: CompletionGuardState
  decision: CompletionDecision
  mode?: CompletionMode
  text?: string
}): CompletionMetadata {
  const text = normalizeText(input.text ?? input.state.model?.finalText ?? "")
  const mode = input.mode ?? input.state.model?.mode ?? "build"
  return {
    source: "completion_guard",
    decision: input.decision.type,
    mode,
    reason: input.decision.reason,
    retry_count: input.state.retryCount,
    stagnation_count: input.state.stagnationCount,
    action_state: input.decision.counts,
    final_text_preview: text.slice(0, 800),
    finish_reason: input.state.model?.finishReason,
    usage: input.state.model?.usage,
    text_signal: input.decision.textSignal,
  }
}

function continueOrFail(
  state: CompletionGuardState,
  reason: Exclude<
    CompletionDecisionReason,
    | "allowed"
    | "non_build_agent"
    | "goal_active"
    | "has_completed_tool"
    | "has_patch"
    | "has_artifact"
    | "tool_failed_with_explanation"
    | "text_completion"
    | "direct_text_answer"
    | "content_filter"
  >,
  counts: CompletionCounts,
  textSignal: CompletionTextSignal,
): CompletionDecision {
  const message = reasonMessage(reason)
  if (reason === "provider_error" && state.providerErrorRetryCount >= 3) {
    return {
      type: "fail",
      reason: "continuation_limit",
      error: "模型连续异常次数已达到上限",
      counts,
      textSignal,
    }
  }
  if (reason === "missing_artifact" && state.missingArtifactRetryCount >= 2) {
    return {
      type: "fail",
      reason: "continuation_limit",
      error: "连续三轮未检测到可下载交付文件",
      counts,
      textSignal,
    }
  }
  if (reason === "output_length" && state.outputLengthRetryCount >= 12) {
    return {
      type: "fail",
      reason: "continuation_limit",
      error: "模型连续因输出长度中断的次数已达到上限",
      counts,
      textSignal,
    }
  }
  if (!counts.hasRoundProgress && state.stagnationCount + 1 >= 2) {
    return {
      type: "fail",
      reason: "stalled",
      error: reason === "provider_error" ? "模型连续异常且没有产生新进度" : "Build 模式连续两轮没有产生新进度",
      counts,
      textSignal,
    }
  }
  return {
    type: "continue",
    reason,
    message,
    counts,
    textSignal,
  }
}

function reasonMessage(reason: CompletionDecisionReason) {
  switch (reason) {
    case "pending_approval":
      return "当前仍有待处理的权限审批，不能直接结束。"
    case "pending_question":
      return "当前仍有待处理的问题回复，不能直接结束。"
    case "pending_tool":
      return "当前仍有工具调用处于运行或未知状态，不能直接结束。"
    case "pending_mcp_job":
      return "当前仍有异步 MCP 作业在运行，请使用对应的 job 工具等待并获取结果。"
    case "unresolved_todo":
      return "当前执行计划仍有未完成项，不能直接结束。"
    case "waiting_user_action":
      return "当前执行依赖用户先完成外部操作，应通过 question 工具进入等待状态。"
    case "provider_error":
      return "模型以 error 状态结束，本轮不能标记为完成。"
    case "output_length":
      return "模型输出达到长度限制，需要从中断位置继续。"
    case "missing_artifact":
      return "用户要求可下载交付物，但当前没有检测到可下载 artifact。请把最终文件写入系统指定的交付目录，重新列出并检查文件后再结束。"
    case "promise_without_action":
      return "当前回复仍在描述后续动作，任务尚未完成。"
    case "failed_tool_without_explanation":
      return "当前只有失败的工具调用，且没有说明失败原因或阻塞信息。"
    case "no_real_action_after_retry":
    case "no_real_action":
    default:
      return "当前没有记录到真实工具动作、文件补丁或产物。"
  }
}

function hasForwardLookingIntent(text: string) {
  if (!text) return false
  const chinesePromise =
    /(我会|我将|我准备|准备开始|接下来(?:我会|会|将|需要|继续)|下一步(?:我会|会|将)|继续(?:生成|构建|实现|修改|测试|校验|检查|完成)|马上(?:开始|执行)|将(?:开始|执行|实现|修改|生成|构建|测试|校验)|会(?:继续|开始|执行|实现|修改|生成|构建|测试|校验))/
  const englishPromise =
    /\b(i will|i'll|i am going to|i’m going to|next i will|will (?:create|implement|modify|build|test|verify|check|continue)|going to (?:create|implement|modify|build|test|verify|check|continue))\b/i
  return chinesePromise.test(text) || englishPromise.test(text)
}

function hasWaitingUserAction(text: string) {
  if (!text) return false
  const chinese =
    /(?:请|需要|麻烦|等待)(?:你|您|用户)?[\s\S]{0,80}(?:操作|点击|打开|进入|启动|安装|登录|授权|确认|完成|复现|测试)[\s\S]{0,80}(?:后|之后|完成时|完成了)[\s\S]{0,40}(?:回复|告诉|确认|通知|反馈)/
  const chineseReplyFirst =
    /(?:操作|点击|打开|进入|启动|安装|登录|授权|确认|完成|复现|测试)[\s\S]{0,80}(?:后|之后)[\s\S]{0,40}(?:回复|告诉|确认|通知|反馈)(?:我|一下)?/
  const english =
    /\b(?:please|need you to|wait for you to)[\s\S]{0,100}(?:open|enter|start|install|log in|authorize|confirm|complete|reproduce|test)[\s\S]{0,100}(?:then|after|once)[\s\S]{0,50}(?:reply|tell me|confirm|let me know|report back)\b/i
  return chinese.test(text) || chineseReplyFirst.test(text) || english.test(text)
}

function hasFailureSummary(text: string) {
  return /(失败|错误|报错|无法|不能|权限|被拒绝|超时|阻塞|failed|error|unable|cannot|permission|denied|timeout|blocked)/i.test(
    text,
  )
}

function hasCompletionSummary(text: string) {
  return /(已完成|完成了|已经|修复|实现|更新|修改|生成|验证|测试通过|done|completed|fixed|implemented|updated|verified)/i.test(
    text,
  )
}

function normalizeText(text: string) {
  return String(text ?? "")
    .replace(/\u001b\[[0-9;]*m/g, "")
    .replace(/\r\n/g, "\n")
    .trim()
}

function stringValue(value: unknown) {
  return typeof value === "string" && value.trim() ? value.trim() : undefined
}

function normalizeFinishReason(value: string | undefined) {
  const reason = value?.trim().toLowerCase().replaceAll("_", "-")
  if (reason === "tool-calls" || reason === "stop" || reason === "length" || reason === "error") return reason
  if (reason === "content-filter") return reason
  return reason || "unknown"
}

function todoComplete(status: string) {
  return status === "completed" || status === "cancelled"
}

function rememberTodos(state: CompletionGuardState, part: Record<string, any>) {
  const metadata = part.state?.metadata
  let todos = Array.isArray(metadata?.todos) ? metadata.todos : undefined
  if (!todos && typeof part.state?.output === "string") {
    try {
      const parsed = JSON.parse(part.state.output)
      if (Array.isArray(parsed)) todos = parsed
    } catch {}
  }
  if (!todos) return
  const next: CompletionGuardState["todos"] = {}
  for (const [index, item] of todos.entries()) {
    if (!item || typeof item !== "object") continue
    const id = stringValue(item.id) || `${index}:${stringValue(item.content) ?? "todo"}`
    next[id] = { status: stringValue(item.status) ?? "pending" }
  }
  state.todos = next
}

function rememberMcpJob(state: CompletionGuardState, part: Record<string, any>) {
  const metadata = part.state?.metadata
  const job = metadata?.structuredContent?.mcp_job ?? metadata?.mcp_job
  if (!job || typeof job !== "object") return
  const id = stringValue(job.id)
  if (!id) return
  state.mcpJobs[id] = { status: stringValue(job.status) ?? "running" }
}
