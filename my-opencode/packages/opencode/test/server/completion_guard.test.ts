import { describe, expect, test } from "bun:test"
import {
  beginCompletionRound,
  completionContinueParts,
  createCompletionGuardState,
  evaluateCompletion,
  incrementCompletionRetry,
  rememberCompletionArtifacts,
  rememberCompletionPart,
} from "../../src/server/completion_guard"

describe("completion guard", () => {
  test("routes external user actions through the question tool before unresolved todos", () => {
    const state = createCompletionGuardState()
    beginCompletionRound(state)
    rememberCompletionPart(state, {
      id: "todo-call",
      type: "tool",
      tool: "todowrite",
      state: {
        status: "completed",
        metadata: { todos: [{ id: "todo-1", content: "读取战斗数据", status: "in_progress" }] },
      },
    })

    const decision = evaluateCompletion({
      state,
      mode: "build",
      text: "请进入一场战斗，完成后回复我，我再继续读取实时数据。",
    })

    expect(decision.type).toBe("continue")
    expect(decision.reason).toBe("waiting_user_action")
    if (decision.type !== "continue") return
    expect(completionContinueParts(decision)[0]?.text).toContain("调用 question 工具")
    expect(completionContinueParts(decision)[0]?.text).toContain("保持 custom 开启")
  })

  test("does not stop a long build while every continuation makes progress", () => {
    const state = createCompletionGuardState()
    rememberCompletionPart(state, {
      id: "todo-call",
      type: "tool",
      tool: "todowrite",
      state: {
        status: "completed",
        metadata: { todos: [{ id: "todo-1", content: "完成迁移", status: "in_progress" }] },
      },
    })

    for (let round = 0; round < 20; round++) {
      beginCompletionRound(state)
      rememberCompletionPart(state, {
        id: `tool-${round}`,
        type: "tool",
        tool: "bash",
        state: { status: "completed" },
      })
      const decision = evaluateCompletion({ state, mode: "build", text: `已完成第 ${round + 1} 步。` })
      expect(decision.type).toBe("continue")
      expect(decision.reason).toBe("unresolved_todo")
      incrementCompletionRetry(state, decision.reason, decision.counts)
    }

    expect(state.retryCount).toBe(20)
    expect(state.stagnationCount).toBe(0)
  })

  test("uses the latest todo snapshot instead of retaining removed items", () => {
    const state = createCompletionGuardState()
    rememberCompletionPart(state, {
      id: "todo-call-1",
      type: "tool",
      tool: "todowrite",
      state: {
        status: "completed",
        metadata: {
          todos: [
            { id: "old", content: "旧任务", status: "in_progress" },
            { id: "kept", content: "保留任务", status: "pending" },
          ],
        },
      },
    })
    rememberCompletionPart(state, {
      id: "todo-call-2",
      type: "tool",
      tool: "todowrite",
      state: {
        status: "completed",
        metadata: { todos: [{ id: "kept", content: "保留任务", status: "completed" }] },
      },
    })

    expect(state.todos).toEqual({ kept: { status: "completed" } })
    expect(evaluateCompletion({ state, mode: "build", text: "任务已经完成。" }).reason).toBe("text_completion")
  })

  test("requires a real artifact when delivery is explicitly required", () => {
    const state = createCompletionGuardState()
    beginCompletionRound(state)

    const missing = evaluateCompletion({
      state,
      mode: "build",
      text: "报告已经生成。",
      deliveryRequired: true,
    })
    expect(missing.type).toBe("continue")
    expect(missing.reason).toBe("missing_artifact")

    rememberCompletionArtifacts(state, [{ relative_path: ".chatcodex-artifacts/task/report.pdf" }], [])
    const complete = evaluateCompletion({
      state,
      mode: "build",
      text: "已生成报告，文件已放入交付目录。",
      deliveryRequired: true,
    })
    expect(complete.type).toBe("allow")
    expect(complete.reason).toBe("has_artifact")
  })

  test("requires a current-task artifact when the final text claims delivery", () => {
    const state = createCompletionGuardState()
    beginCompletionRound(state)

    const missing = evaluateCompletion({
      state,
      mode: "build",
      text: "交付文件已发送到 .chatcodex-artifacts/task_previous/report.zip",
    })
    expect(missing.type).toBe("continue")
    expect(missing.reason).toBe("missing_artifact")

    rememberCompletionArtifacts(state, [{ relative_path: ".chatcodex-artifacts/task_current/report.zip" }], [])
    const complete = evaluateCompletion({
      state,
      mode: "build",
      text: "交付文件已发送到 .chatcodex-artifacts/task_current/report.zip",
    })
    expect(complete.type).toBe("allow")
    expect(complete.reason).toBe("has_artifact")
  })

  test("stops retrying when required artifacts remain missing despite new tool actions", () => {
    const state = createCompletionGuardState()

    for (let attempt = 0; attempt < 2; attempt++) {
      beginCompletionRound(state)
      rememberCompletionPart(state, {
        type: "tool",
        callID: `delivery-check-${attempt}`,
        tool: "bash",
        state: { status: "completed" },
      })
      const decision = evaluateCompletion({
        state,
        mode: "build",
        text: "已重新检查交付目录。",
        deliveryRequired: true,
      })
      expect(decision.type).toBe("continue")
      expect(decision.reason).toBe("missing_artifact")
      incrementCompletionRetry(state, decision.reason, decision.counts)
    }

    beginCompletionRound(state)
    rememberCompletionPart(state, {
      type: "tool",
      callID: "delivery-check-final",
      tool: "bash",
      state: { status: "completed" },
    })
    const failed = evaluateCompletion({
      state,
      mode: "build",
      text: "已再次检查交付目录。",
      deliveryRequired: true,
    })
    expect(failed.type).toBe("fail")
    expect(failed.reason).toBe("continuation_limit")
  })

  test("does not require artifacts for ordinary build replies", () => {
    const state = createCompletionGuardState()
    beginCompletionRound(state)
    const decision = evaluateCompletion({
      state,
      mode: "build",
      text: "你好，有什么可以帮你？",
    })
    expect(decision.type).toBe("allow")
    expect(decision.reason).toBe("direct_text_answer")
  })

})
