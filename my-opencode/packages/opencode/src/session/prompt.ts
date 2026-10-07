import path from "path"
import os from "os"
import { SessionID, MessageID, PartID } from "./schema"
import { MessageV2 } from "./message-v2"
import * as Log from "@opencode-ai/core/util/log"
import { SessionRevert } from "./revert"
import * as Session from "./session"
import { Agent } from "../agent/agent"
import { Provider } from "@/provider/provider"
import { ModelID, ProviderID } from "../provider/schema"
import { type Tool as AITool, tool, jsonSchema } from "ai"
import type { JSONSchema7 } from "@ai-sdk/provider"
import { SessionCompaction } from "./compaction"
import { Bus } from "../bus"
import { SystemPrompt } from "./system"
import { Instruction } from "./instruction"
import { Plugin } from "../plugin"
import MAX_STEPS from "../session/prompt/max-steps.txt"
import { ToolRegistry } from "@/tool/registry"
import { MCP } from "../mcp"
import { LSP } from "@/lsp/lsp"
import { ulid } from "ulid"
import { ChildProcess, ChildProcessSpawner } from "effect/unstable/process"
import { CrossSpawnSpawner } from "@opencode-ai/core/cross-spawn-spawner"
import * as Stream from "effect/Stream"
import { Command } from "../command"
import { pathToFileURL, fileURLToPath } from "url"
import { Config } from "@/config/config"
import { ConfigMarkdown } from "@/config/markdown"
import { SessionSummary } from "./summary"
import { NamedError } from "@opencode-ai/core/util/error"
import { SessionProcessor } from "./processor"
import { Tool } from "@/tool/tool"
import { Permission } from "@/permission"
import { SessionStatus } from "./status"
import { LLM } from "./llm"
import { Shell } from "@/shell/shell"
import { ShellID } from "@/tool/shell/id"
import { AppFileSystem } from "@opencode-ai/core/filesystem"
import { Truncate } from "@/tool/truncate"
import { Image } from "@/image/image"
import { resolveDialect } from "@/image/dialect"
import { decodeDataUrl } from "@/util/data-url"
import { Process } from "@/util/process"
import { Cause, Effect, Exit, Latch, Layer, Option, Scope, Context, Schema, Types } from "effect"
import { rm } from "node:fs/promises"
import { existsSync, readdirSync } from "node:fs"
import * as EffectLogger from "@opencode-ai/core/effect/logger"
import { InstanceState } from "@/effect/instance-state"
import { TaskTool, type TaskPromptOps } from "@/tool/task"
import { SessionRunState } from "./run-state"
import { RuntimeFlags } from "@/effect/runtime-flags"
import { EventV2 } from "@opencode-ai/core/event"
import { EventV2Bridge } from "@/event-v2-bridge"
import { SessionEvent } from "@opencode-ai/core/session-event"
import { Auth } from "@/auth"
import { ModelV2 } from "@opencode-ai/core/model"
import { ProviderV2 } from "@opencode-ai/core/provider"
import { AgentAttachment, FileAttachment, ReferenceAttachment, Source } from "@opencode-ai/core/session-prompt"
import { Reference } from "@/reference/reference"
import * as DateTime from "effect/DateTime"
import { eq } from "@/storage/db"
import * as Database from "@/storage/db"
import { SessionTable } from "./session.sql"
import { referencePromptMetadata, referenceTextPart } from "./prompt/reference"
import { SessionReminders } from "./reminders"
import { SessionTools } from "./tools"
import { LLMEvent } from "@opencode-ai/llm"
import { SessionRetry } from "./retry"
import { Token } from "@/util/token"
import { thresholdPercent, usable } from "./overflow"
import { GlobalBus } from "@/bus/global"
import {
  hasApkTargetIntent,
  hasFridaIntent,
  hasVerifyIntent,
  isAndroidReverseAgent,
  isVerifyMcpConnected,
  verifyProjectConfigPath,
  verifyPrompt,
} from "./verify-config"

// @ts-ignore
globalThis.AI_SDK_LOG_WARNINGS = false

const decodeMessageInfo = Schema.decodeUnknownExit(MessageV2.Info)
const decodeMessagePart = Schema.decodeUnknownExit(MessageV2.Part)

const STRUCTURED_OUTPUT_DESCRIPTION = `Use this tool to return your final response in the requested structured format.

IMPORTANT:
- You MUST call this tool exactly once at the end of your response
- The input must be valid JSON matching the required schema
- Complete all necessary research and tool calls BEFORE calling this tool
- This tool provides your final answer - no further actions are taken after calling it`

const STRUCTURED_OUTPUT_SYSTEM_PROMPT = `IMPORTANT: The user has requested structured output. You MUST use the StructuredOutput tool to provide your final response. Do NOT respond with plain text - you MUST call the StructuredOutput tool with your answer formatted according to the schema.`

const log = Log.create({ service: "session.prompt" })
const elog = EffectLogger.create({ service: "session.prompt" })
const PROMPT_AUDIT_TEXT_LIMIT = 800
const PROMPT_AUDIT_ITEM_LIMIT = 12
const OUTPUT_LENGTH_CONTINUATION_LIMIT = 3
const OUTPUT_LENGTH_CONTINUATION_PROMPT =
  "Continue exactly where the previous response was truncated. Do not repeat completed content."
const PROVIDER_FINISH_ERROR_RETRY_DELAY = 1000
const PROVIDER_FINISH_ERROR_CONTINUATION_PROMPT =
  "The previous provider response ended unexpectedly. Continue from the established context, preserve completed tool results, and do not repeat completed tool calls."

function percent(value: number, total: number) {
  if (!Number.isFinite(value) || !Number.isFinite(total) || total <= 0) return 0
  return Math.max(0, Math.round((value / total) * 1000) / 10)
}

function clipPromptAuditText(value: string, limit = PROMPT_AUDIT_TEXT_LIMIT) {
  if (value.length <= limit) return value
  return `${value.slice(0, limit)}…[truncated ${value.length - limit} chars]`
}

function summarizePromptAuditValue(value: unknown, depth = 0): unknown {
  if (value === null || value === undefined) return value
  if (typeof value === "string") return clipPromptAuditText(value)
  if (typeof value === "number" || typeof value === "boolean") return value
  if (Array.isArray(value)) {
    const list = value.slice(0, PROMPT_AUDIT_ITEM_LIMIT).map((item) => summarizePromptAuditValue(item, depth + 1))
    if (value.length > PROMPT_AUDIT_ITEM_LIMIT) {
      list.push(`[truncated ${value.length - PROMPT_AUDIT_ITEM_LIMIT} items]`)
    }
    return list
  }
  if (typeof value === "object") {
    if (depth >= 4) return "[object]"
    const record = value as Record<string, unknown>
    const output: Record<string, unknown> = {}
    for (const [index, [key, next]] of Object.entries(record).entries()) {
      if (index >= PROMPT_AUDIT_ITEM_LIMIT) {
        output.__truncated__ = `${Object.keys(record).length - PROMPT_AUDIT_ITEM_LIMIT} keys`
        break
      }
      output[key] = summarizePromptAuditValue(next, depth + 1)
    }
    return output
  }
  return String(value)
}

function summarizePromptAuditMessage(message: MessageV2.WithParts) {
  const modelInfo =
    message.info.role === "user"
      ? {
          providerID: message.info.model.providerID,
          modelID: message.info.model.modelID,
          variant: message.info.model.variant,
        }
      : {
          providerID: message.info.providerID,
          modelID: message.info.modelID,
          variant: message.info.variant,
        }
  return {
    id: message.info.id,
    role: message.info.role,
    agent: message.info.agent,
    ...modelInfo,
    parts: message.parts.slice(0, PROMPT_AUDIT_ITEM_LIMIT).map((part) => {
      if (part.type === "text") {
        return {
          type: part.type,
          synthetic: part.synthetic === true,
          text: clipPromptAuditText(part.text),
        }
      }
      if (part.type === "file") {
        return {
          type: part.type,
          mime: part.mime,
          filename: part.filename,
          source: summarizePromptAuditValue(part.source),
          url: clipPromptAuditText(part.url, 160),
        }
      }
      if (part.type === "tool") {
        return {
          type: part.type,
          tool: part.tool,
          state: summarizePromptAuditValue(part.state),
        }
      }
      return summarizePromptAuditValue(part)
    }),
    part_count: message.parts.length,
  }
}

function emitLLMUsage(payload: Record<string, unknown>) {
  GlobalBus.emit("event", {
    payload: {
      type: "opencode.llm.usage",
      component: "llm",
      ...payload,
    },
  })
}

type EmitContextUsageInput = {
  stage: string
  sessionID: SessionID
  messageID: MessageID
  parentMessageID: MessageID
  model: Provider.Model
  agent: Agent.Info
  messages: MessageV2.WithParts[]
  system?: string[]
  cfg: Config.Info
  outputTokenMax?: number
  userInputText?: string
  tools?: Record<string, unknown>
  transformMessages?: boolean
}

type EmitPromptAuditInput = EmitContextUsageInput & {
  modelMessages: unknown[]
}

export interface Interface {
  readonly cancel: (sessionID: SessionID) => Effect.Effect<void>
  readonly prompt: (input: PromptInput) => Effect.Effect<MessageV2.WithParts, Image.Error>
  readonly loop: (input: LoopInput) => Effect.Effect<MessageV2.WithParts>
  readonly shell: (input: ShellInput) => Effect.Effect<MessageV2.WithParts, Session.BusyError>
  readonly command: (input: CommandInput) => Effect.Effect<MessageV2.WithParts, Image.Error>
  readonly resolvePromptParts: (template: string) => Effect.Effect<PromptInput["parts"]>
}

export class Service extends Context.Service<Service, Interface>()("@opencode/SessionPrompt") {}

export const layer = Layer.effect(
  Service,
  Effect.gen(function* () {
    const bus = yield* Bus.Service
    const status = yield* SessionStatus.Service
    const sessions = yield* Session.Service
    const auth = yield* Auth.Service
    const agents = yield* Agent.Service
    const provider = yield* Provider.Service
    const processor = yield* SessionProcessor.Service
    const compaction = yield* SessionCompaction.Service
    const plugin = yield* Plugin.Service
    const commands = yield* Command.Service
    const config = yield* Config.Service
    const permission = yield* Permission.Service
    const fsys = yield* AppFileSystem.Service
    const mcp = yield* MCP.Service
    const lsp = yield* LSP.Service
    const registry = yield* ToolRegistry.Service
    const truncate = yield* Truncate.Service
    const image = yield* Image.Service
    const spawner = yield* ChildProcessSpawner.ChildProcessSpawner
    const scope = yield* Scope.Scope
    const instruction = yield* Instruction.Service
    const state = yield* SessionRunState.Service
    const revert = yield* SessionRevert.Service
    const summary = yield* SessionSummary.Service
    const sys = yield* SystemPrompt.Service
    const llm = yield* LLM.Service
    const references = yield* Reference.Service
    const events = yield* EventV2Bridge.Service
    const flags = yield* RuntimeFlags.Service
    const ops = Effect.fn("SessionPrompt.ops")(function* () {
      return {
        cancel: (sessionID: SessionID) => cancel(sessionID),
        resolvePromptParts: (template: string) => resolvePromptParts(template),
        prompt: (input: PromptInput) => prompt(input).pipe(Effect.catch(Effect.die)),
        loop: (input: LoopInput) => loop(input),
      } satisfies TaskPromptOps
    })

    const emitContextUsage = Effect.fn("SessionPrompt.emitContextUsage")(function* (input: EmitContextUsageInput) {
      const msgs = structuredClone(input.messages)
      if (input.transformMessages !== false) {
        yield* plugin.trigger("experimental.chat.messages.transform", {}, { messages: msgs })
      }
      const modelMsgs = yield* MessageV2.toModelMessagesEffect(msgs, input.model)
      const system = input.system ?? []
      const compactionCountTokens =
        input.stage === "compaction_context_ready"
          ? Token.estimate(JSON.stringify({ system, messages: modelMsgs }))
          : undefined
      const estimatedUserInputTokens = Token.estimate(input.userInputText ?? "")
      const compactionThreshold = usable({
        cfg: input.cfg,
        model: input.model,
        outputTokenMax: input.outputTokenMax,
      })
      emitLLMUsage({
        stage: input.stage,
        session_id: input.sessionID,
        message_id: input.messageID,
        parent_message_id: input.parentMessageID,
        provider_id: input.model.providerID,
        model_id: input.model.id,
        agent: input.agent.name,
        message_count: msgs.length,
        model_message_count: modelMsgs.length,
        part_count: msgs.reduce((total, item) => total + item.parts.length, 0),
        tool_count: Object.keys(input.tools ?? {}).length,
        estimated_user_input_tokens: estimatedUserInputTokens,
        context_limit: input.model.limit.context,
        compaction_count_tokens: compactionCountTokens,
        context_usage_percent:
          compactionCountTokens === undefined ? undefined : percent(compactionCountTokens, input.model.limit.context),
        compaction_threshold_tokens: compactionThreshold,
        compaction_threshold_percent: thresholdPercent(input.cfg),
      })
    })

    const emitPromptAudit = Effect.fn("SessionPrompt.emitPromptAudit")(function* (input: EmitPromptAuditInput) {
      log.info("request audit", {
        stage: input.stage,
        sessionID: input.sessionID,
        messageID: input.messageID,
        parentMessageID: input.parentMessageID,
        providerID: input.model.providerID,
        modelID: input.model.id,
        agent: input.agent.name,
        system: summarizePromptAuditValue(input.system ?? []),
        userInputText: clipPromptAuditText(input.userInputText ?? ""),
        toolNames: Object.keys(input.tools ?? {}),
        sessionMessages: input.messages.slice(-PROMPT_AUDIT_ITEM_LIMIT).map(summarizePromptAuditMessage),
        modelMessages: summarizePromptAuditValue(input.modelMessages),
      })
    })

    const cancel = Effect.fn("SessionPrompt.cancel")(function* (sessionID: SessionID) {
      yield* elog.info("cancel", { sessionID })
      yield* state.cancel(sessionID)
    })

    const resolvePromptParts = Effect.fn("SessionPrompt.resolvePromptParts")(function* (template: string) {
      const ctx = yield* InstanceState.context
      const parts: Types.DeepMutable<PromptInput["parts"]> = [{ type: "text", text: template }]
      const files = ConfigMarkdown.files(template)
      const seen = new Set<string>()
      const mentionSource = (match: RegExpMatchArray) => {
        const start = match.index ?? 0
        return { value: match[0], start, end: start + match[0].length }
      }
      yield* Effect.forEach(
        files,
        Effect.fnUntraced(function* (match) {
          const name = match[1]
          if (!name) return
          if (seen.has(name)) return
          seen.add(name)

          const slash = name.indexOf("/")
          const alias = slash === -1 ? name : name.slice(0, slash)
          const reference = yield* references.get(alias)
          if (reference) {
            const source = mentionSource(match)
            if (reference.kind === "invalid") {
              parts.push(
                referenceTextPart({ reference, source, target: slash === -1 ? undefined : name.slice(slash + 1) }),
              )
              return
            }

            yield* references.ensure(reference.path)
            if (slash === -1) {
              parts.push(referenceTextPart({ reference, source }))
              return
            }

            const target = name.slice(slash + 1)
            const targetPath = path.resolve(reference.path, target)
            if (!AppFileSystem.contains(reference.path, targetPath)) {
              parts.push(
                referenceTextPart({
                  reference,
                  source,
                  target,
                  targetPath,
                  problem: `Path escapes configured reference @${alias}: ${target}`,
                }),
              )
              return
            }

            const info = yield* fsys.stat(targetPath).pipe(Effect.option)
            if (Option.isNone(info)) {
              parts.push(
                referenceTextPart({
                  reference,
                  source,
                  target,
                  targetPath,
                  problem: `Path does not exist inside configured reference @${alias}: ${target}`,
                }),
              )
              return
            }

            parts.push({
              type: "file",
              url: pathToFileURL(targetPath).href,
              filename: name,
              mime: info.value.type === "Directory" ? "application/x-directory" : "text/plain",
            })
            return
          }

          const filepath = name.startsWith("~/")
            ? path.join(os.homedir(), name.slice(2))
            : path.resolve(ctx.worktree, name)

          const info = yield* fsys.stat(filepath).pipe(Effect.option)
          if (Option.isNone(info)) {
            const found = yield* agents.get(name)
            if (found) parts.push({ type: "agent", name: found.name })
            return
          }
          const stat = info.value
          parts.push({
            type: "file",
            url: pathToFileURL(filepath).href,
            filename: name,
            mime: stat.type === "Directory" ? "application/x-directory" : "text/plain",
          })
        }),
        { concurrency: "unbounded", discard: true },
      )
      return parts
    })

    const title = Effect.fn("SessionPrompt.ensureTitle")(function* (input: {
      session: Session.Info
      history: MessageV2.WithParts[]
      providerID: ProviderID
      modelID: ModelID
    }) {
      if (input.session.parentID) return
      if (!Session.isDefaultTitle(input.session.title)) return

      const real = (m: MessageV2.WithParts) =>
        m.info.role === "user" && !m.parts.every((p) => "synthetic" in p && p.synthetic)
      const idx = input.history.findIndex(real)
      if (idx === -1) return
      if (input.history.filter(real).length !== 1) return

      const context = input.history.slice(0, idx + 1)
      const firstUser = context[idx]
      if (!firstUser || firstUser.info.role !== "user") return
      const firstInfo = firstUser.info

      const subtasks = firstUser.parts.filter((p): p is MessageV2.SubtaskPart => p.type === "subtask")
      const onlySubtasks = subtasks.length > 0 && firstUser.parts.every((p) => p.type === "subtask")

      const ag = yield* agents.get("title")
      if (!ag) return
      const mdl = ag.model
        ? yield* provider.getModel(ag.model.providerID, ag.model.modelID)
        : ((yield* provider.getSmallModel(input.providerID)) ??
          (yield* provider.getModel(input.providerID, input.modelID)))
      // 生图 / 生视频模型只提供生成接口，用来生成标题必然失败，直接跳过。
      if (isMediaOnlyModel(mdl)) return
      const msgs = onlySubtasks
        ? [{ role: "user" as const, content: subtasks.map((p) => p.prompt).join("\n") }]
        : yield* MessageV2.toModelMessagesEffect(context, mdl)
      const text = yield* llm
        .stream({
          agent: ag,
          user: firstInfo,
          system: [],
          small: true,
          tools: {},
          model: mdl,
          sessionID: input.session.id,
          retries: 2,
          messages: [{ role: "user", content: "Generate a title for this conversation:\n" }, ...msgs],
        })
        .pipe(
          Stream.filter(LLMEvent.is.textDelta),
          Stream.map((e) => e.text),
          Stream.mkString,
          Effect.orDie,
        )
      const cleaned = text
        .replace(/<think>[\s\S]*?<\/think>\s*/g, "")
        .split("\n")
        .map((line) => line.trim())
        .find((line) => line.length > 0)
      if (!cleaned) return
      const t = cleaned.length > 100 ? cleaned.substring(0, 97) + "..." : cleaned
      yield* sessions
        .setTitle({ sessionID: input.session.id, title: t })
        .pipe(Effect.catchCause((cause) => elog.error("failed to generate title", { error: Cause.squash(cause) })))
    })

    const handleSubtask = Effect.fn("SessionPrompt.handleSubtask")(function* (input: {
      task: MessageV2.SubtaskPart
      model: Provider.Model
      lastUser: MessageV2.User
      sessionID: SessionID
      session: Session.Info
      msgs: MessageV2.WithParts[]
    }) {
      const { task, model, lastUser, sessionID, session, msgs } = input
      const ctx = yield* InstanceState.context
      const promptOps = yield* ops()
      const { task: taskTool } = yield* registry.named()
      const taskModel = task.model ? yield* getModel(task.model.providerID, task.model.modelID, sessionID) : model
      const assistantMessage: MessageV2.Assistant = yield* sessions.updateMessage({
        id: MessageID.ascending(),
        role: "assistant",
        parentID: lastUser.id,
        sessionID,
        mode: task.agent,
        agent: task.agent,
        variant: lastUser.model.variant,
        path: { cwd: ctx.directory, root: ctx.worktree },
        cost: 0,
        tokens: { input: 0, output: 0, reasoning: 0, cache: { read: 0, write: 0 } },
        modelID: taskModel.id,
        providerID: taskModel.providerID,
        time: { created: Date.now() },
      })
      let part: MessageV2.ToolPart = yield* sessions.updatePart({
        id: PartID.ascending(),
        messageID: assistantMessage.id,
        sessionID: assistantMessage.sessionID,
        type: "tool",
        callID: ulid(),
        tool: TaskTool.id,
        state: {
          status: "running",
          input: {
            prompt: task.prompt,
            description: task.description,
            subagent_type: task.agent,
            command: task.command,
          },
          time: { start: Date.now() },
        },
      })
      const taskArgs = {
        prompt: task.prompt,
        description: task.description,
        subagent_type: task.agent,
        command: task.command,
      }
      yield* plugin.trigger(
        "tool.execute.before",
        { tool: TaskTool.id, sessionID, callID: part.id },
        { args: taskArgs },
      )

      const taskAgent = yield* agents.get(task.agent)
      if (!taskAgent) {
        const available = (yield* agents.list()).filter((a) => !a.hidden).map((a) => a.name)
        const hint = available.length ? ` Available agents: ${available.join(", ")}` : ""
        const error = new NamedError.Unknown({ message: `Agent not found: "${task.agent}".${hint}` })
        yield* bus.publish(Session.Event.Error, { sessionID, error: error.toObject() })
        throw error
      }

      let error: Error | undefined
      const taskAbort = new AbortController()
      const result = yield* taskTool
        .execute(taskArgs, {
          agent: task.agent,
          messageID: assistantMessage.id,
          sessionID,
          abort: taskAbort.signal,
          callID: part.callID,
          extra: { bypassAgentCheck: true, promptOps },
          messages: msgs,
          metadata: (val: { title?: string; metadata?: Record<string, any> }) =>
            Effect.gen(function* () {
              part = yield* sessions.updatePart({
                ...part,
                type: "tool",
                state: { ...part.state, ...val },
              } satisfies MessageV2.ToolPart)
            }),
          ask: (req: any) =>
            permission
              .ask({
                ...req,
                sessionID,
                ruleset: Permission.merge(taskAgent.permission, session.permission ?? []),
              })
              .pipe(Effect.orDie),
        })
        .pipe(
          Effect.catchCause((cause) => {
            const defect = Cause.squash(cause)
            error = defect instanceof Error ? defect : new Error(String(defect))
            log.error("subtask execution failed", { error, agent: task.agent, description: task.description })
            return Effect.void
          }),
          Effect.onInterrupt(() =>
            Effect.gen(function* () {
              taskAbort.abort()
              assistantMessage.finish = "tool-calls"
              assistantMessage.time.completed = Date.now()
              yield* sessions.updateMessage(assistantMessage)
              if (part.state.status === "running") {
                yield* sessions.updatePart({
                  ...part,
                  state: {
                    status: "error",
                    error: "Cancelled",
                    time: { start: part.state.time.start, end: Date.now() },
                    metadata: part.state.metadata,
                    input: part.state.input,
                  },
                } satisfies MessageV2.ToolPart)
              }
            }),
          ),
        )

      const attachments = result?.attachments?.map((attachment) => ({
        ...attachment,
        id: PartID.ascending(),
        sessionID,
        messageID: assistantMessage.id,
      }))

      yield* plugin.trigger(
        "tool.execute.after",
        { tool: TaskTool.id, sessionID, callID: part.id, args: taskArgs },
        result,
      )

      assistantMessage.finish = "tool-calls"
      assistantMessage.time.completed = Date.now()
      yield* sessions.updateMessage(assistantMessage)

      if (result && part.state.status === "running") {
        yield* sessions.updatePart({
          ...part,
          state: {
            status: "completed",
            input: part.state.input,
            title: result.title,
            metadata: result.metadata,
            output: result.output,
            attachments,
            time: { ...part.state.time, end: Date.now() },
          },
        } satisfies MessageV2.ToolPart)
      }

      if (!result) {
        yield* sessions.updatePart({
          ...part,
          state: {
            status: "error",
            error: error ? `Tool execution failed: ${error.message}` : "Tool execution failed",
            time: {
              start: part.state.status === "running" ? part.state.time.start : Date.now(),
              end: Date.now(),
            },
            metadata: part.state.status === "pending" ? undefined : part.state.metadata,
            input: part.state.input,
          },
        } satisfies MessageV2.ToolPart)
      }

      if (!task.command) return

      const summaryUserMsg: MessageV2.User = {
        id: MessageID.ascending(),
        sessionID,
        role: "user",
        time: { created: Date.now() },
        agent: lastUser.agent,
        model: lastUser.model,
      }
      yield* sessions.updateMessage(summaryUserMsg)
      yield* sessions.updatePart({
        id: PartID.ascending(),
        messageID: summaryUserMsg.id,
        sessionID,
        type: "text",
        text: "Summarize the task tool output above and continue with your task.",
        synthetic: true,
      } satisfies MessageV2.TextPart)
    })

    const shellImpl = Effect.fn("SessionPrompt.shellImpl")(function* (input: ShellInput, ready?: Latch.Latch) {
      return yield* Effect.uninterruptibleMask((restore) =>
        Effect.gen(function* () {
          const markReady = ready ? ready.open.pipe(Effect.asVoid) : Effect.void
          const { msg, part, cwd } = yield* Effect.gen(function* () {
            const ctx = yield* InstanceState.context
            const session = yield* sessions.get(input.sessionID).pipe(Effect.orDie)
            if (session.revert) {
              yield* revert.cleanup(session)
            }
            const agent = yield* agents.get(input.agent)
            if (!agent) {
              const available = (yield* agents.list()).filter((a) => !a.hidden).map((a) => a.name)
              const hint = available.length ? ` Available agents: ${available.join(", ")}` : ""
              const error = new NamedError.Unknown({ message: `Agent not found: "${input.agent}".${hint}` })
              yield* bus.publish(Session.Event.Error, { sessionID: input.sessionID, error: error.toObject() })
              throw error
            }
            const model = input.model ?? agent.model ?? (yield* currentModel(input.sessionID))
            const userMsg: MessageV2.User = {
              id: input.messageID ?? MessageID.ascending(),
              sessionID: input.sessionID,
              time: { created: Date.now() },
              role: "user",
              agent: input.agent,
              model: { providerID: model.providerID, modelID: model.modelID },
            }
            yield* sessions.updateMessage(userMsg)
            const userPart: MessageV2.Part = {
              type: "text",
              id: PartID.ascending(),
              messageID: userMsg.id,
              sessionID: input.sessionID,
              text: "The following tool was executed by the user",
              synthetic: true,
            }
            yield* sessions.updatePart(userPart)

            const msg: MessageV2.Assistant = {
              id: MessageID.ascending(),
              sessionID: input.sessionID,
              parentID: userMsg.id,
              mode: input.agent,
              agent: input.agent,
              cost: 0,
              path: { cwd: ctx.directory, root: ctx.worktree },
              time: { created: Date.now() },
              role: "assistant",
              tokens: { input: 0, output: 0, reasoning: 0, cache: { read: 0, write: 0 } },
              modelID: model.modelID,
              providerID: model.providerID,
            }
            yield* sessions.updateMessage(msg)
            const started = Date.now()
            const part: MessageV2.ToolPart = {
              type: "tool",
              id: PartID.ascending(),
              messageID: msg.id,
              sessionID: input.sessionID,
              tool: ShellID.ToolID,
              callID: ulid(),
              state: {
                status: "running",
                time: { start: started },
                input: { command: input.command },
              },
            }
            yield* sessions.updatePart(part)
            if (flags.experimentalEventSystem) {
              yield* events.publish(SessionEvent.Shell.Started, {
                sessionID: input.sessionID,
                timestamp: DateTime.makeUnsafe(started),
                callID: part.callID,
                command: input.command,
              })
            }
            return { msg, part, cwd: ctx.directory }
          }).pipe(Effect.ensuring(markReady))

          const cfg = yield* config.get()
          const sh = Shell.preferred(cfg.shell)
          const args = Shell.args(sh, input.command, cwd)
          let output = ""
          let aborted = false

          const finish = Effect.uninterruptible(
            Effect.gen(function* () {
              if (aborted) {
                output += "\n\n" + ["<metadata>", "User aborted the command", "</metadata>"].join("\n")
              }
              const completed = Date.now()
              if (flags.experimentalEventSystem) {
                yield* events.publish(SessionEvent.Shell.Ended, {
                  sessionID: input.sessionID,
                  timestamp: DateTime.makeUnsafe(completed),
                  callID: part.callID,
                  output,
                })
              }
              if (!msg.time.completed) {
                msg.time.completed = completed
                yield* sessions.updateMessage(msg)
              }
              if (part.state.status === "running") {
                part.state = {
                  status: "completed",
                  time: { ...part.state.time, end: completed },
                  input: part.state.input,
                  title: "",
                  metadata: { output, description: "" },
                  output,
                }
                yield* sessions.updatePart(part)
              }
            }),
          )

          const exit = yield* restore(
            Effect.gen(function* () {
              const shellEnv = yield* plugin.trigger(
                "shell.env",
                { cwd, sessionID: input.sessionID, callID: part.callID },
                { env: {} },
              )
              const cmd = ChildProcess.make(sh, args, {
                cwd,
                extendEnv: true,
                env: { ...shellEnv.env, TERM: "dumb" },
                stdin: "ignore",
                forceKillAfter: "3 seconds",
              })
              const handle = yield* spawner.spawn(cmd)
              yield* Stream.runForEach(Stream.decodeText(handle.all), (chunk) =>
                Effect.gen(function* () {
                  output += chunk
                  if (part.state.status === "running") {
                    part.state.metadata = { output, description: "" }
                    yield* sessions.updatePart(part)
                  }
                }),
              )
              yield* handle.exitCode
            }).pipe(Effect.scoped, Effect.orDie),
          ).pipe(Effect.exit)

          if (Exit.isFailure(exit) && Cause.hasInterrupts(exit.cause) && !Cause.hasDies(exit.cause)) {
            aborted = true
          }
          yield* finish

          if (Exit.isFailure(exit) && !aborted && !Cause.hasInterruptsOnly(exit.cause)) {
            return yield* Effect.failCause(exit.cause)
          }

          return { info: msg, parts: [part] }
        }),
      )
    })

    const getModel = Effect.fn("SessionPrompt.getModel")(function* (
      providerID: ProviderID,
      modelID: ModelID,
      sessionID: SessionID,
    ) {
      const exit = yield* provider.getModel(providerID, modelID).pipe(Effect.exit)
      if (Exit.isSuccess(exit)) return exit.value
      const err = Cause.squash(exit.cause)
      if (Provider.ModelNotFoundError.isInstance(err)) {
        const hint = err.suggestions?.length ? ` Did you mean: ${err.suggestions.join(", ")}?` : ""
        yield* bus.publish(Session.Event.Error, {
          sessionID,
          error: new NamedError.Unknown({
            message: `Model not found: ${err.providerID}/${err.modelID}.${hint}`,
          }).toObject(),
        })
      }
      return yield* Effect.die(err)
    })

    const isImageModel = (model: Provider.Model) => model.capabilities.output.image && !model.capabilities.output.text
    const isVideoModel = (model: Provider.Model) => model.capabilities.output.video && !model.capabilities.output.text
    const IMAGE_REQUEST_TIMEOUT_MS = 120_000
    const IMAGE_REQUEST_MAX_ATTEMPTS = 3
    const IMAGE_COUNT_MAX = 10
    const imageSizes = new Set(["1024x1024", "1024x1536", "1536x1024", "1792x1024", "1024x1792", "auto"])
    const imageBackgrounds = new Set(["auto", "opaque", "transparent"])
    const imageFormats = new Set(["png", "jpeg", "jpg", "webp"])
    const imageFidelities = new Set(["low", "high"])
    const videoAspectRatios = new Set(["16:9", "9:16", "1:1", "4:3", "3:4", "3:2", "2:3"])

    const imageMime = (format?: string, url?: string) => {
      if (format === "jpeg" || format === "jpg") return "image/jpeg"
      if (format === "webp") return "image/webp"
      if (format === "png") return "image/png"
      if (url) {
        const match = url.match(/\.([a-zA-Z0-9]+)(?:$|\?)/)
        if (match?.[1]?.toLowerCase() === "jpg" || match?.[1]?.toLowerCase() === "jpeg") return "image/jpeg"
        if (match?.[1]?.toLowerCase() === "webp") return "image/webp"
        if (match?.[1]?.toLowerCase() === "png") return "image/png"
      }
      return "image/png"
    }

    const imageExt = (mime: string) => {
      if (mime === "image/jpeg") return "jpg"
      if (mime === "image/webp") return "webp"
      return "png"
    }

    const imageArgs = (text: string) => {
      const opts: {
        n?: number
        size?: "1024x1024" | "1024x1536" | "1536x1024" | "1792x1024" | "1024x1792" | "auto"
        quality?: string
        background?: "auto" | "opaque" | "transparent"
        format?: "png" | "jpeg" | "webp"
        fidelity?: "low" | "high"
        aspectRatio?: string
        resolution?: "1k" | "2k" | "480p" | "720p"
        duration?: number
      } = {}
      const out = text.replace(
        /(^|\s)--(n|count|size|quality|background|format|fidelity|aspect-ratio|ratio|resolution|duration)(?:=(\S+)|\s+(\S+))(?=\s|$)/g,
        (all, lead, key, a, b) => {
          const val = (a ?? b ?? "").trim().toLowerCase()
          if (!val) return all
          if (key === "n" || key === "count") {
            const n = Number.parseInt(val, 10)
            if (Number.isFinite(n) && n > 0) opts.n = Math.min(Math.max(n, 1), IMAGE_COUNT_MAX)
            return lead
          }
          if (key === "size" && imageSizes.has(val)) {
            opts.size = val as typeof opts.size
            return lead
          }
          if (key === "quality") {
            opts.quality = val
            return lead
          }
          if (key === "background" && imageBackgrounds.has(val)) {
            opts.background = val as typeof opts.background
            return lead
          }
          if (key === "format" && imageFormats.has(val)) {
            opts.format = (val === "jpg" ? "jpeg" : val) as typeof opts.format
            return lead
          }
          if (key === "fidelity" && imageFidelities.has(val)) {
            opts.fidelity = val as typeof opts.fidelity
            return lead
          }
          if (key === "aspect-ratio" || key === "ratio") {
            opts.aspectRatio = val
            return lead
          }
          if (key === "resolution") {
            opts.resolution = val === "720p" || val === "480p" ? val : val === "2k" ? "2k" : "1k"
            return lead
          }
          if (key === "duration") {
            const seconds = Number.parseInt(val, 10)
            if (Number.isFinite(seconds) && seconds >= 1 && seconds <= 15) opts.duration = seconds
            return lead
          }
          return all
        },
      )
      return {
        text: out.replace(/\s{2,}/g, " ").trim(),
        opts,
      }
    }

    // 合并两个来源：消息上的 media 字段（新链路）优先，文本里的 --size/--n
    // 次之（兼容旧客户端）。两者都没有时参数为空，由方言取默认值。
    const mediaArgs = (turn: MessageV2.WithParts | undefined) => {
      const base = imageTextParts(turn?.parts ?? [], true).opts
      const info = turn?.info
      const media = info && info.role === "user" ? info.media : undefined
      if (!media) return base
      return {
        ...base,
        n: media.count ?? base.n,
        size: media.size ?? base.size,
        quality: media.quality ?? base.quality,
        format: media.format ?? base.format,
        background: media.background ?? base.background,
        fidelity: media.fidelity ?? base.fidelity,
        aspectRatio: media.aspectRatio ?? base.aspectRatio,
        resolution: (media.resolution as typeof base.resolution) ?? base.resolution,
        duration: media.duration ?? base.duration,
      }
    }

    const imageTextParts = (parts: MessageV2.Part[], pick = false) => {
      const opts: ReturnType<typeof imageArgs>["opts"] = {}
      const text = parts
        .filter((part): part is MessageV2.TextPart => part.type === "text" && !part.synthetic && !part.ignored)
        .map((part) => {
          const next = imageArgs(part.text.trim())
          if (pick) Object.assign(opts, next.opts)
          return next.text
        })
        .filter(Boolean)
        .join("\n\n")
      return { text, opts }
    }

    const imageFiles = (items: MessageV2.WithParts[]) =>
      Array.from(
        new Map(
          items
            .flatMap((item) => item.parts)
            .filter((part): part is MessageV2.FilePart => part.type === "file" && part.mime.startsWith("image/"))
            .map((part) => [`${part.filename ?? ""}:${part.url}`, part] as const),
        ).values(),
      )

    const describeImageEndpoint = (url: string) => {
      try {
        const parsed = new URL(url)
        return `${parsed.origin}${parsed.pathname}`
      } catch {
        return url
      }
    }

    const resolveImageBaseURL = (model: Provider.Model, info: Provider.Info) => {
      const custom =
        typeof info.options?.endpoint === "string" && info.options.endpoint.trim()
          ? info.options.endpoint
          : typeof info.options?.baseURL === "string" && info.options.baseURL.trim()
            ? info.options.baseURL
            : undefined
      const fallback =
        typeof model.api.url === "string" && model.api.url.trim() ? model.api.url : "https://api.openai.com/v1"
      return (custom ?? fallback).replace(/\/+$/, "")
    }

    const parseImageResponse = async (res: Response) => {
      const text = await res.text()
      try {
        return {
          text,
          json: JSON.parse(text) as {
            data?: Array<{ b64_json?: string; revised_prompt?: string; url?: string }>
            error?: { message?: string }
          },
        }
      } catch {
        return { text, json: undefined }
      }
    }

    const clipImageDetail = (input?: string, max = 400) => {
      if (!input) return
      const text = input.trim()
      if (!text) return
      if (text.length <= max) return text
      return `${text.slice(0, max)}...`
    }

    const imageKind = (input: { status?: number; raw?: string; error?: Error }) => {
      const text = `${input.raw ?? ""}\n${input.error?.message ?? ""}`
      if (input.status === 504 || /gateway time-?out/i.test(text)) return "gateway" as const
      if (input.status === 429 || /rate.?limit|too many requests|overloaded|temporarily unavailable/i.test(text)) {
        return "rate" as const
      }
      if (
        input.status === 408 ||
        /timed?\s*out|timeout|fetch failed|unable to connect|connection reset|network error|econnreset|econnrefused|enotfound|eai_again|etimedout/i.test(
          text,
        )
      ) {
        return "network" as const
      }
      if (input.status !== undefined && input.status >= 500) return "server" as const
      return "generic" as const
    }

    const imageText = (input: { kind: ReturnType<typeof imageKind>; status?: number; attempt: number }) => {
      if (input.kind === "gateway") {
        return `上游生图服务响应超时（504），已自动重试${input.attempt}次仍失败，请稍后重试或切换其他生图模型`
      }
      if (input.kind === "rate") {
        return `上游生图服务当前较繁忙，已自动重试${input.attempt}次仍失败，请稍后重试`
      }
      if (input.kind === "network") {
        return `生图请求网络超时，已自动重试${input.attempt}次仍失败，请稍后重试`
      }
      if (input.kind === "server") {
        return `上游生图服务暂时不可用（${input.status ?? 500}），已自动重试${input.attempt}次仍失败，请稍后重试`
      }
      if (input.status !== undefined) {
        return `生图失败（状态码 ${input.status}），已自动重试${input.attempt}次仍失败，请稍后重试`
      }
      return `生图失败，已自动重试${input.attempt}次仍失败，请稍后重试`
    }

    const imageFail = (input: {
      kind: ReturnType<typeof imageKind>
      status?: number
      attempt: number
      endpoint: string
      raw?: string
      error?: Error
    }) => {
      log.error("image generation request failed", {
        attempt: input.attempt,
        status: input.status,
        endpoint: describeImageEndpoint(input.endpoint),
        kind: input.kind,
        detail: clipImageDetail(input.raw ?? input.error?.message, 800),
      })
      return new MessageV2.APIError({
        message: imageText(input),
        ...(typeof input.status === "number" ? { statusCode: input.status } : {}),
        isRetryable: false,
        metadata: {
          source: "image",
          kind: input.kind,
          endpoint: describeImageEndpoint(input.endpoint),
          attempts: String(input.attempt),
          max_attempts: String(IMAGE_REQUEST_MAX_ATTEMPTS),
        },
      })
    }

    const generateImage = Effect.fn("SessionPrompt.image")(function* (input: {
      sessionID: SessionID
      user: MessageV2.User
      assistant: MessageV2.Assistant
      model: Provider.Model
      parts: MessageV2.WithParts[]
    }) {
      const exit = yield* Effect.gen(function* () {
        // 能不能生图由方言命中决定，不再用 npm 白名单：
        // OpenAI 走 /images/*，xAI Grok Imagine 走同名路由但换一套参数。
        const dialect = resolveDialect({
          providerID: input.model.providerID,
          npm: input.model.api.npm,
          modelID: input.model.api.id,
        })
        if (!dialect) {
          throw new Error(`Image generation is not supported for provider: ${input.model.providerID}`)
        }

        const info = yield* provider.getProvider(input.model.providerID)
        const saved = yield* auth.get(input.model.providerID)
        const key =
          info.key ??
          (saved?.type === "api" ? saved.key : undefined) ??
          (typeof info.options?.apiKey === "string" && info.options.apiKey.trim() ? info.options.apiKey : undefined)
        if (!key) {
          throw new Error(`API key is required for image generation (${input.model.providerID})`)
        }

        const base = resolveImageBaseURL(input.model, info)
        // 生图是单轮请求：只用本次用户输入（见 currentImageTurn 的说明）。
        const current = currentImageTurn(input.parts)
        // 参数优先取消息上的 media 字段（客户端不再拼进提示词）；
        // 旧客户端仍会把 --size/--n 拼在文本里，文本解析保留作兼容。
        const opts = mediaArgs(current[0])
        const text = current
          .map((item) => imageTextParts(item.parts).text)
          .filter(Boolean)
          .join("\n\n")
        const imgs = imageFiles(current)
        // 请求体由方言构建：OpenAI 是 JSON/multipart 两态、xAI 一律 JSON，
        // 各自的参数集合也不同（见 src/image/dialect.ts）。
        const request = yield* Effect.promise(() =>
          dialect.build({
            modelID: input.model.api.id,
            prompt: text || input.user.id,
            args: opts,
            images: imgs.map((part) => ({
              url: part.url,
              mime: part.mime,
              filename: part.filename,
            })),
            loadImage: async (part) => {
              const res = await fetch(part.url)
              if (!res.ok) throw new Error(`Failed to load image part: ${part.filename ?? part.url}`)
              return await res.blob()
            },
          }),
        )
        const endpoint = `${base}${request.path}`
        const isMultipart = request.transport === "multipart"
        const body = request.transport === "multipart" ? request.form : JSON.stringify(request.body)

        // 中转站（以及部分官方实现）对 `n>1` 经常只返回一张图；
        // 数量在客户端侧保证：n>1 时把请求体的 n 归一成 1，
        // 并发发 n 次独立请求并聚合结果，选几张就出几张。
        const requestedCount = opts.n && opts.n > 1 ? opts.n : 0
        const singleBody =
          requestedCount && request.transport === "json"
            ? JSON.stringify({ ...request.body, n: 1 })
            : body

        const shouldRetryImageRequest = (status?: number, error?: Error) => {
          if (status !== undefined) {
            if (status === 408 || status === 409 || status === 429) return true
            if (status >= 500) return true
          }
          if (!error) return false
          const retryable = SessionRetry.retryable(
            new NamedError.Unknown({
              message: error.message,
            }).toObject(),
            input.model.providerID,
          )
          return (
            !!retryable ||
            /timed out|timeout|unable to connect|certificate verify failed|fetch failed/i.test(error.message)
          )
        }

        let finalBody:
          | {
              data?: Array<{ b64_json?: string; revised_prompt?: string; url?: string }>
              error?: { message?: string }
            }
          | undefined

        const requestOnce = Effect.fn("SessionPrompt.image.request")(function* () {
          for (let attempt = 1; attempt <= IMAGE_REQUEST_MAX_ATTEMPTS; attempt++) {
            const result = yield* Effect.tryPromise({
              try: async () => {
                const headers = new Headers({
                  Authorization: `Bearer ${key}`,
                  ...input.model.headers,
                })
                // multipart 的 Content-Type 必须由 FormData 自己带 boundary。
                if (isMultipart) headers.delete("Content-Type")
                else headers.set("Content-Type", "application/json")
                const res = await fetch(endpoint, {
                  method: "POST",
                  headers,
                  body: requestedCount ? singleBody : body,
                  signal: AbortSignal.timeout(IMAGE_REQUEST_TIMEOUT_MS),
                })
                const parsed = await parseImageResponse(res)
                return { res, ...parsed }
              },
              catch: (err) => (err instanceof Error ? err : new Error(String(err))),
            }).pipe(Effect.exit)

            if (Exit.isSuccess(result)) {
              const { res, json, text } = result.value
              if (res.ok) return json

              const message = json?.error?.message || text || `Image generation failed with status ${res.status}`
              const retryable = shouldRetryImageRequest(res.status)
              if (retryable && attempt < IMAGE_REQUEST_MAX_ATTEMPTS) {
                const wait = SessionRetry.delay(attempt)
                log.warn("retrying image generation request", {
                  attempt,
                  nextAttempt: attempt + 1,
                  status: res.status,
                  providerID: input.model.providerID,
                  modelID: input.model.id,
                  endpoint: describeImageEndpoint(endpoint),
                  wait,
                })
                yield* Effect.sleep(wait)
                continue
              }

              const kind = imageKind({ status: res.status, raw: message })
              throw imageFail({
                kind,
                status: res.status,
                attempt,
                endpoint,
                raw: message,
              })
            }

            const error = Cause.squash(result.cause)
            const retryable = error instanceof Error && shouldRetryImageRequest(undefined, error)
            if (retryable && attempt < IMAGE_REQUEST_MAX_ATTEMPTS) {
              const wait = SessionRetry.delay(attempt)
              log.warn("retrying image generation request after network error", {
                attempt,
                nextAttempt: attempt + 1,
                providerID: input.model.providerID,
                modelID: input.model.id,
                endpoint: describeImageEndpoint(endpoint),
                wait,
                error: error instanceof Error ? error.message : String(error),
              })
              yield* Effect.sleep(wait)
              continue
            }

            if (error instanceof Error) {
              const kind = imageKind({ error })
              throw imageFail({
                kind,
                attempt,
                endpoint,
                error,
              })
            }
            throw error
          }
          throw new Error("Image generation failed after retries")
        })

        if (requestedCount) {
          // 并发 n 张单图请求；只要有一张成功就交付成功张，全部失败才报错。
          const results = yield* Effect.forEach(
            Array.from({ length: requestedCount }, (_, i) => i),
            () => Effect.exit(requestOnce()),
            { concurrency: "unbounded" },
          )
          const succeeded = results
            .filter((item) => Exit.isSuccess(item))
            .map((item) => (item as Exit.Success<NonNullable<typeof finalBody>>).value)
          if (succeeded.length === 0) {
            const firstFailure = results.find((item) => Exit.isFailure(item))
            const cause = firstFailure && Exit.isFailure(firstFailure) ? firstFailure.cause : undefined
            const error = cause ? Cause.squash(cause) : undefined
            if (error instanceof Error) throw error
            throw new Error("Image generation returned no image data")
          }
          finalBody = {
            data: succeeded.flatMap((entry) => entry.data ?? []),
          }
        } else {
          finalBody = yield* requestOnce()
        }

        const items = finalBody?.data?.filter((entry) => entry.b64_json || entry.url) ?? []
        if (items.length === 0) throw new Error("Image generation returned no image data")

        const revised = items.find((entry) => entry.revised_prompt?.trim())?.revised_prompt?.trim()
        if (revised) {
          yield* sessions.updatePart({
            id: PartID.ascending(),
            messageID: input.assistant.id,
            sessionID: input.sessionID,
            type: "text",
            text: revised,
            synthetic: true,
          } satisfies MessageV2.TextPart)
        }

        input.assistant.finish = "stop"
        input.assistant.time.completed = Date.now()
        yield* Effect.forEach(items, (item, i) => {
          // 方言决定默认 mime（xAI 出 jpeg，OpenAI 出 png）；显式 `--format` 优先。
          const mime = imageMime(opts.format, item.url ?? request.mime)
          const ext = imageExt(mime)
          return sessions.updatePart({
            id: PartID.ascending(),
            messageID: input.assistant.id,
            sessionID: input.sessionID,
            type: "file",
            mime,
            filename: `${input.model.id}-${i + 1}.${ext}`,
            url: item.b64_json ? `data:${mime};base64,${item.b64_json}` : item.url!,
          } satisfies MessageV2.FilePart)
        })
        yield* sessions.updateMessage(input.assistant)
      }).pipe(Effect.exit)

      if (Exit.isSuccess(exit)) return

      const msg = Cause.squash(exit.cause)
      input.assistant.error = MessageV2.APIError.isInstance(msg)
        ? msg.toObject()
        : new NamedError.Unknown({
            message: msg instanceof Error ? msg.message : String(msg),
          }).toObject()
      input.assistant.time.completed = Date.now()
      yield* bus.publish(Session.Event.Error, {
        sessionID: input.sessionID,
        error: input.assistant.error,
      })
      yield* sessions.updateMessage(input.assistant)
    })

    // 抽视频第一帧做封面：先把视频下载到临时文件（可能带鉴权），
    // 再用本机 ffmpeg 抽帧。没装 ffmpeg、下载失败或抽帧失败都直接跳过，
    // 封面只是锦上添花，任何环节出错都不能影响视频交付。
    // 找 ffmpeg：先看 PATH，再看 launcher 托管的运行时目录
    // （<LAUNCHER_MANAGED_RUNTIME_DIR>/ffmpeg/<version>/bin/ffmpeg）。
    const resolveFfmpeg = (() => {
      let cached: string | undefined
      return () => {
        // 只缓存「找到」的结果：找不到时不缓存，用户中途装上 FFmpeg 后
        // 下一次抽帧就能用上，不必重启 agent。
        if (cached) return cached
        cached = Bun.which("ffmpeg") ?? undefined
        if (cached) return cached
        const managed = process.env.LAUNCHER_MANAGED_RUNTIME_DIR?.trim()
        if (!managed) return undefined
        try {
          const root = path.join(managed, "ffmpeg")
          for (const version of readdirSync(root)) {
            for (const name of ["ffmpeg", "ffmpeg.exe"]) {
              const candidate = path.join(root, version, "bin", name)
              if (existsSync(candidate)) {
                cached = candidate
                return cached
              }
            }
          }
        } catch {
          return undefined
        }
        return undefined
      }
    })()

    const videoPoster = async (url: string, headers: Record<string, string>) => {
      const ffmpeg = resolveFfmpeg()
      if (!ffmpeg) return undefined
      const tmp = path.join(os.tmpdir(), `opencode-video-${crypto.randomUUID()}.mp4`)
      try {
        const res = await fetch(url, { headers, signal: AbortSignal.timeout(IMAGE_REQUEST_TIMEOUT_MS) })
        if (!res.ok) return undefined
        const bytes = Buffer.from(await res.arrayBuffer())
        if (bytes.length === 0) return undefined
        await Bun.write(tmp, bytes)
        const out = await Process.run(
          [ffmpeg, "-y", "-i", tmp, "-frames:v", "1", "-f", "image2pipe", "-vcodec", "mjpeg", "pipe:1"],
          { nothrow: true, timeout: 20_000 },
        )
        if (out.code !== 0 || out.stdout.byteLength === 0) return undefined
        return out.stdout.toString("base64")
      } catch {
        return undefined
      } finally {
        await rm(tmp, { force: true }).catch(() => {})
      }
    }

    // 上游异常时可能返回 HTML 错误页（例如 Cloudflare 502），
    // 直接 JSON.parse 会抛出难以理解的解析错误，这里统一成可读文案。
    const parseVideoJson = <T,>(text: string): T => {
      if (!text) return {} as T
      try {
        return JSON.parse(text) as T
      } catch {
        return {} as T
      }
    }
    const videoErrorDetail = (status: number, text: string, message?: string) => {
      const trimmed = message?.trim()
      if (trimmed) return trimmed
      if (text.trimStart().startsWith("<")) return `上游返回了错误页面（HTTP ${status}），通常是上游服务暂时不可用`
      return clipImageDetail(text, 200) || `HTTP ${status}`
    }

    // xAI 视频是异步的：POST /videos/generations 拿 request_id，
    // 轮询 GET /videos/{id}，完成后从 /videos/{id}/content 下载 MP4。
    const VIDEO_POLL_INTERVAL_MS = 5_000
    const VIDEO_POLL_DEADLINE_MS = 10 * 60_000
    const generateVideo = Effect.fn("SessionPrompt.video")(function* (input: {
      sessionID: SessionID
      user: MessageV2.User
      assistant: MessageV2.Assistant
      model: Provider.Model
      parts: MessageV2.WithParts[]
    }) {
      const exit = yield* Effect.gen(function* () {
        const info = yield* provider.getProvider(input.model.providerID)
        const saved = yield* auth.get(input.model.providerID)
        const key =
          info.key ??
          (saved?.type === "api" ? saved.key : undefined) ??
          (typeof info.options?.apiKey === "string" && info.options.apiKey.trim() ? info.options.apiKey : undefined)
        if (!key) throw new Error(`API key is required for video generation (${input.model.providerID})`)
        const base = resolveImageBaseURL(input.model, info)
        const current = currentImageTurn(input.parts)
        const promptText = current
          .map((item) => imageTextParts(item.parts).text)
          .filter(Boolean)
          .join("\n\n")
        const reference = imageFiles(current)[0]
        const opts = mediaArgs(current[0])
        const duration = opts.duration && opts.duration >= 1 && opts.duration <= 15 ? opts.duration : undefined
        const ratio = opts.aspectRatio && videoAspectRatios.has(opts.aspectRatio) ? opts.aspectRatio : undefined
        const resolution = opts.resolution === "720p" || opts.resolution === "480p" ? opts.resolution : undefined
        const body: Record<string, unknown> = {
          model: input.model.api.id,
          prompt: promptText || input.user.id,
          ...(duration ? { duration } : {}),
          ...(ratio ? { aspect_ratio: ratio } : {}),
          ...(resolution ? { resolution } : {}),
        }
        if (reference) {
          body.image = { url: reference.url }
        }
        const headers = {
          Authorization: `Bearer ${key}`,
          "Content-Type": "application/json",
          ...input.model.headers,
        }
        const created = yield* Effect.tryPromise({
          try: async () => {
            const res = await fetch(`${base}/videos/generations`, {
              method: "POST",
              headers,
              body: JSON.stringify(body),
              signal: AbortSignal.timeout(IMAGE_REQUEST_TIMEOUT_MS),
            })
            const text = await res.text()
            const json = parseVideoJson<{ request_id?: string; error?: { message?: string } }>(text)
            if (!res.ok) {
              throw new Error(`视频生成失败：${videoErrorDetail(res.status, text, json.error?.message)}`)
            }
            if (!json.request_id) throw new Error("视频生成未返回任务标识，请稍后重试")
            return json.request_id
          },
          catch: (err) => (err instanceof Error ? err : new Error(String(err))),
        })
        log.info("video generation submitted", {
          providerID: input.model.providerID,
          modelID: input.model.api.id,
          requestID: created,
          duration: body.duration,
          aspectRatio: body.aspect_ratio,
          resolution: body.resolution,
        })
        const started = Date.now()
        let videoUrl = ""
        // 上游任务刚创建时，查询接口可能短暂查不到（异步注册/索引延迟），
        // 这种「not found」在有限次数内不算致命，继续等即可。
        const NOT_FOUND_TOLERANCE = 6
        let notFoundStreak = 0
        while (Date.now() - started < VIDEO_POLL_DEADLINE_MS) {
          yield* Effect.sleep(`${VIDEO_POLL_INTERVAL_MS} millis`)
          const status = yield* Effect.tryPromise({
            try: async () => {
              const res = await fetch(`${base}/videos/${created}`, {
                headers,
                signal: AbortSignal.timeout(IMAGE_REQUEST_TIMEOUT_MS),
              })
              const text = await res.text()
              const json = parseVideoJson<{
                status?: string
                video?: { url?: string }
                error?: { message?: string }
              }>(text)
              if (!res.ok) {
                const detail = videoErrorDetail(res.status, text, json.error?.message)
                log.warn("video status query failed", {
                  providerID: input.model.providerID,
                  modelID: input.model.api.id,
                  requestID: created,
                  status: res.status,
                  detail,
                  elapsedMS: Date.now() - started,
                })
                if (/not\s*found/i.test(detail) && notFoundStreak < NOT_FOUND_TOLERANCE) {
                  notFoundStreak += 1
                  return { status: "pending" }
                }
                throw new Error(`视频状态查询失败：${detail}`)
              }
              return json
            },
            catch: (err) => (err instanceof Error ? err : new Error(String(err))),
          })
          notFoundStreak = 0
          if (status.status === "failed" || status.status === "expired") {
            throw new Error(status.error?.message?.trim() || `视频生成失败（${status.status}），请稍后重试`)
          }
          if (status.status === "done") {
            videoUrl = status.video?.url || `${base}/videos/${created}/content`
            break
          }
        }
        if (!videoUrl) throw new Error("视频生成超时（等待超过 10 分钟），请稍后重试")
        // 上游常常只回相对路径（/v1/videos/<id>/content）：必须补成绝对地址，
        // 否则抽帧与客户端播放都会失败，视频看起来像没生成。
        // 地址归一：已带协议（http/https/data）的原样保留；其余按 URL 规则
        // 相对 base 解析——上游常回 "/v1/videos/<id>/content"，而 base 本身
        // 已含 /v1，字符串拼接会拼出 /v1/v1/... 这种坏地址。
        const absoluteVideoUrl = (() => {
          if (/^[a-z][a-z0-9+.-]*:/i.test(videoUrl)) return videoUrl
          try {
            return new URL(videoUrl, `${base}/`).toString()
          } catch {
            return `${base}/${videoUrl.replace(/^\/+/, "")}`
          }
        })()
        // 抽帧失败不应影响视频交付：连缺陷一起吞掉。
        const posterExit = yield* Effect.exit(Effect.promise(() => videoPoster(absoluteVideoUrl, headers)))
        const poster = Exit.isSuccess(posterExit) ? posterExit.value : undefined
        input.assistant.finish = "stop"
        input.assistant.time.completed = Date.now()
        if (poster) {
          yield* sessions.updatePart({
            id: PartID.ascending(),
            messageID: input.assistant.id,
            sessionID: input.sessionID,
            type: "file",
            mime: "image/jpeg",
            filename: `${input.model.id}.poster.jpg`,
            url: `data:image/jpeg;base64,${poster}`,
          } satisfies MessageV2.FilePart)
        }
        yield* sessions.updatePart({
          id: PartID.ascending(),
          messageID: input.assistant.id,
          sessionID: input.sessionID,
          type: "file",
          mime: "video/mp4",
          filename: `${input.model.id}.mp4`,
          url: absoluteVideoUrl,
        } satisfies MessageV2.FilePart)
        yield* sessions.updateMessage(input.assistant)
      }).pipe(Effect.exit)

      if (Exit.isSuccess(exit)) return
      const msg = Cause.squash(exit.cause)
      input.assistant.error = new NamedError.Unknown({
        message: msg instanceof Error ? msg.message : String(msg),
      }).toObject()
      input.assistant.time.completed = Date.now()
      yield* bus.publish(Session.Event.Error, {
        sessionID: input.sessionID,
        error: input.assistant.error,
      })
      yield* sessions.updateMessage(input.assistant)
    })

    const currentModel = Effect.fnUntraced(function* (sessionID: SessionID) {
      const current = Database.use((db) =>
        db.select({ model: SessionTable.model }).from(SessionTable).where(eq(SessionTable.id, sessionID)).get(),
      )
      if (current?.model) {
        return {
          providerID: ProviderID.make(current.model.providerID),
          modelID: ModelID.make(current.model.id),
          ...(current.model.variant && current.model.variant !== "default" ? { variant: current.model.variant } : {}),
        }
      }
      const match = yield* sessions
        .findMessage(sessionID, (m) => m.info.role === "user" && !!m.info.model)
        .pipe(Effect.orDie)
      if (Option.isSome(match) && match.value.info.role === "user") return match.value.info.model
      return yield* provider.defaultModel()
    })

    const createUserMessage = Effect.fn("SessionPrompt.createUserMessage")(function* (input: PromptInput) {
      const agentName = input.agent
      const ag = agentName ? yield* agents.get(agentName) : yield* agents.defaultInfo()
      if (!ag) {
        const available = (yield* agents.list()).filter((a) => !a.hidden).map((a) => a.name)
        const hint = available.length ? ` Available agents: ${available.join(", ")}` : ""
        const error = new NamedError.Unknown({ message: `Agent not found: "${agentName}".${hint}` })
        yield* bus.publish(Session.Event.Error, { sessionID: input.sessionID, error: error.toObject() })
        throw error
      }

      const current = Database.use((db) =>
        db
          .select({ agent: SessionTable.agent, model: SessionTable.model })
          .from(SessionTable)
          .where(eq(SessionTable.id, input.sessionID))
          .get(),
      )
      const model = input.model ?? ag.model ?? (yield* currentModel(input.sessionID))
      const same = ag.model && model.providerID === ag.model.providerID && model.modelID === ag.model.modelID
      const full =
        !input.variant && ag.variant && same
          ? yield* provider
              .getModel(model.providerID, model.modelID)
              .pipe(Effect.catchIf(Provider.ModelNotFoundError.isInstance, () => Effect.succeed(undefined)))
          : undefined
      const variant = input.variant ?? (ag.variant && full?.variants?.[ag.variant] ? ag.variant : undefined)

      const info: MessageV2.User = {
        id: input.messageID ?? MessageID.ascending(),
        role: "user",
        sessionID: input.sessionID,
        time: { created: Date.now() },
        tools: input.tools,
        agent: ag.name,
        model: {
          providerID: model.providerID,
          modelID: model.modelID,
          variant,
        },
        system: input.system,
        format: input.format,
        media: input.media,
      }

      if (current?.agent !== info.agent) {
        yield* events.publish(SessionEvent.AgentSwitched, {
          sessionID: input.sessionID,
          timestamp: DateTime.makeUnsafe(info.time.created),
          agent: info.agent,
        })
      }
      if (
        current?.model?.providerID !== info.model.providerID ||
        current.model.id !== info.model.modelID ||
        (current.model.variant === "default" ? undefined : current.model.variant) !== info.model.variant
      ) {
        yield* events.publish(SessionEvent.ModelSwitched, {
          sessionID: input.sessionID,
          timestamp: DateTime.makeUnsafe(info.time.created),
          model: {
            id: ModelV2.ID.make(info.model.modelID),
            providerID: ProviderV2.ID.make(info.model.providerID),
            variant: ModelV2.VariantID.make(info.model.variant ?? "default"),
          },
        })
      }

      yield* Effect.addFinalizer(() => instruction.clear(info.id))

      type Draft<T> = T extends MessageV2.Part ? Omit<T, "id"> & { id?: string } : never
      const assign = (part: Draft<MessageV2.Part>): MessageV2.Part => ({
        ...part,
        id: part.id ? PartID.make(part.id) : PartID.ascending(),
      })

      const referenceContextFromFilePart = Effect.fnUntraced(function* (
        part: Extract<PromptInput["parts"][number], { type: "file" }>,
        filepath: string,
      ) {
        const name = part.filename?.replace(/#\d+(?:-\d*)?$/, "")
        if (!name) return
        const slash = name.indexOf("/")
        if (slash === -1) return

        const reference = yield* references.get(name.slice(0, slash))
        if (!reference || reference.kind === "invalid") return
        if (!AppFileSystem.contains(reference.path, filepath)) return

        const target = path.relative(reference.path, filepath).split(path.sep).join("/")
        if (!target || target.startsWith("../") || target === "..") return

        return referenceTextPart({
          reference,
          source: part.source?.text ?? { value: `@${name}`, start: 0, end: name.length + 1 },
          target,
          targetPath: filepath,
        })
      })

      const resolvePart: (part: PromptInput["parts"][number]) => Effect.Effect<Draft<MessageV2.Part>[]> = Effect.fn(
        "SessionPrompt.resolveUserPart",
      )(function* (part) {
        if (part.type === "file") {
          if (part.source?.type === "resource") {
            const { clientName, uri } = part.source
            log.info("mcp resource", { clientName, uri, mime: part.mime })
            const pieces: Draft<MessageV2.Part>[] = [
              {
                messageID: info.id,
                sessionID: input.sessionID,
                type: "text",
                synthetic: true,
                text: `Reading MCP resource: ${part.filename} (${uri})`,
              },
            ]
            const exit = yield* mcp.readResource(clientName, uri).pipe(Effect.exit)
            if (Exit.isSuccess(exit)) {
              const content = exit.value
              if (!content) throw new Error(`Resource not found: ${clientName}/${uri}`)
              const items = Array.isArray(content.contents) ? content.contents : [content.contents]
              for (const c of items) {
                if ("text" in c && c.text) {
                  pieces.push({
                    messageID: info.id,
                    sessionID: input.sessionID,
                    type: "text",
                    synthetic: true,
                    text: c.text,
                  })
                } else if ("blob" in c && c.blob) {
                  const mime = "mimeType" in c ? c.mimeType : part.mime
                  pieces.push({
                    messageID: info.id,
                    sessionID: input.sessionID,
                    type: "text",
                    synthetic: true,
                    text: `[Binary content: ${mime}]`,
                  })
                }
              }
              pieces.push({ ...part, messageID: info.id, sessionID: input.sessionID })
            } else {
              const error = Cause.squash(exit.cause)
              log.error("failed to read MCP resource", { error, clientName, uri })
              const message = error instanceof Error ? error.message : String(error)
              pieces.push({
                messageID: info.id,
                sessionID: input.sessionID,
                type: "text",
                synthetic: true,
                text: `Failed to read MCP resource ${part.filename}: ${message}`,
              })
            }
            return pieces
          }
          const url = new URL(part.url)
          switch (url.protocol) {
            case "data:":
              if (part.mime === "text/plain") {
                return [
                  {
                    messageID: info.id,
                    sessionID: input.sessionID,
                    type: "text",
                    synthetic: true,
                    text: `Called the Read tool with the following input: ${JSON.stringify({ filePath: part.filename })}`,
                  },
                  {
                    messageID: info.id,
                    sessionID: input.sessionID,
                    type: "text",
                    synthetic: true,
                    text: decodeDataUrl(part.url),
                  },
                  { ...part, messageID: info.id, sessionID: input.sessionID },
                ]
              }
              break
            case "file:": {
              log.info("file", { mime: part.mime })
              const filepath = fileURLToPath(part.url)
              const referenceContext = yield* referenceContextFromFilePart(part, filepath)
              const mime = (yield* fsys.isDir(filepath)) ? "application/x-directory" : part.mime

              const { read } = yield* registry.named()
              const execRead = (args: Parameters<typeof read.execute>[0], extra?: Tool.Context["extra"]) => {
                const controller = new AbortController()
                return read
                  .execute(args, {
                    sessionID: input.sessionID,
                    abort: controller.signal,
                    agent: input.agent!,
                    messageID: info.id,
                    extra: { bypassCwdCheck: true, ...extra },
                    messages: [],
                    metadata: () => Effect.void,
                    ask: () => Effect.void,
                  })
                  .pipe(Effect.onInterrupt(() => Effect.sync(() => controller.abort())))
              }

              if (mime === "text/plain") {
                let offset: number | undefined
                let limit: number | undefined
                const range = { start: url.searchParams.get("start"), end: url.searchParams.get("end") }
                if (range.start != null) {
                  const filePathURI = part.url.split("?")[0]
                  let start = parseInt(range.start)
                  let end = range.end ? parseInt(range.end) : undefined
                  if (start === end) {
                    const symbols = yield* lsp.documentSymbol(filePathURI).pipe(Effect.catch(() => Effect.succeed([])))
                    for (const symbol of symbols) {
                      let r: LSP.Range | undefined
                      if ("range" in symbol) r = symbol.range
                      else if ("location" in symbol) r = symbol.location.range
                      if (r?.start?.line && r?.start?.line === start) {
                        start = r.start.line
                        end = r?.end?.line ?? start
                        break
                      }
                    }
                  }
                  offset = Math.max(start, 1)
                  if (end) limit = end - (offset - 1)
                }
                const args = { filePath: filepath, offset, limit }
                const pieces: Draft<MessageV2.Part>[] = [
                  ...(referenceContext
                    ? [{ ...referenceContext, messageID: info.id, sessionID: input.sessionID }]
                    : []),
                  {
                    messageID: info.id,
                    sessionID: input.sessionID,
                    type: "text",
                    synthetic: true,
                    text: `Called the Read tool with the following input: ${JSON.stringify(args)}`,
                  },
                ]
                const exit = yield* provider.getModel(info.model.providerID, info.model.modelID).pipe(
                  Effect.flatMap((mdl) => execRead(args, { model: mdl })),
                  Effect.exit,
                )
                if (Exit.isSuccess(exit)) {
                  const result = exit.value
                  pieces.push({
                    messageID: info.id,
                    sessionID: input.sessionID,
                    type: "text",
                    synthetic: true,
                    text: result.output,
                  })
                  if (result.attachments?.length) {
                    pieces.push(
                      ...result.attachments.map((a) => ({
                        ...a,
                        synthetic: true,
                        filename: a.filename ?? part.filename,
                        messageID: info.id,
                        sessionID: input.sessionID,
                      })),
                    )
                  } else {
                    pieces.push({ ...part, mime, messageID: info.id, sessionID: input.sessionID })
                  }
                } else {
                  const error = Cause.squash(exit.cause)
                  log.error("failed to read file", { error })
                  const message = error instanceof Error ? error.message : String(error)
                  yield* bus.publish(Session.Event.Error, {
                    sessionID: input.sessionID,
                    error: new NamedError.Unknown({ message }).toObject(),
                  })
                  pieces.push({
                    messageID: info.id,
                    sessionID: input.sessionID,
                    type: "text",
                    synthetic: true,
                    text: `Read tool failed to read ${filepath} with the following error: ${message}`,
                  })
                }
                return pieces
              }

              if (mime === "application/x-directory") {
                const args = { filePath: filepath }
                const exit = yield* execRead(args).pipe(Effect.exit)
                if (Exit.isFailure(exit)) {
                  const error = Cause.squash(exit.cause)
                  log.error("failed to read directory", { error })
                  const message = error instanceof Error ? error.message : String(error)
                  yield* bus.publish(Session.Event.Error, {
                    sessionID: input.sessionID,
                    error: new NamedError.Unknown({ message }).toObject(),
                  })
                  return [
                    ...(referenceContext
                      ? [{ ...referenceContext, messageID: info.id, sessionID: input.sessionID }]
                      : []),
                    {
                      messageID: info.id,
                      sessionID: input.sessionID,
                      type: "text",
                      synthetic: true,
                      text: `Read tool failed to read ${filepath} with the following error: ${message}`,
                    },
                  ]
                }
                return [
                  ...(referenceContext
                    ? [{ ...referenceContext, messageID: info.id, sessionID: input.sessionID }]
                    : []),
                  {
                    messageID: info.id,
                    sessionID: input.sessionID,
                    type: "text",
                    synthetic: true,
                    text: `Called the Read tool with the following input: ${JSON.stringify(args)}`,
                  },
                  {
                    messageID: info.id,
                    sessionID: input.sessionID,
                    type: "text",
                    synthetic: true,
                    text: exit.value.output,
                  },
                  { ...part, mime, messageID: info.id, sessionID: input.sessionID },
                ]
              }

              return [
                ...(referenceContext ? [{ ...referenceContext, messageID: info.id, sessionID: input.sessionID }] : []),
                {
                  messageID: info.id,
                  sessionID: input.sessionID,
                  type: "text",
                  synthetic: true,
                  text: `Called the Read tool with the following input: {"filePath":"${filepath}"}`,
                },
                {
                  id: part.id,
                  messageID: info.id,
                  sessionID: input.sessionID,
                  type: "file",
                  url:
                    `data:${mime};base64,` +
                    Buffer.from(yield* fsys.readFile(filepath).pipe(Effect.catch(Effect.die))).toString("base64"),
                  mime,
                  filename: part.filename!,
                  source: part.source,
                },
              ]
            }
          }
        }

        if (part.type === "agent") {
          const perm = Permission.evaluate("task", part.name, ag.permission)
          const hint = perm.action === "deny" ? " . Invoked by user; guaranteed to exist." : ""
          return [
            { ...part, messageID: info.id, sessionID: input.sessionID },
            {
              messageID: info.id,
              sessionID: input.sessionID,
              type: "text",
              synthetic: true,
              text:
                " Use the above message and context to generate a prompt and call the task tool with subagent: " +
                part.name +
                hint,
            },
          ]
        }

        return [{ ...part, messageID: info.id, sessionID: input.sessionID }]
      })

      const resolvedParts = yield* Effect.forEach(input.parts, resolvePart, { concurrency: "unbounded" }).pipe(
        Effect.map((x) => x.flat().map(assign)),
      )

      yield* plugin.trigger(
        "chat.message",
        {
          sessionID: input.sessionID,
          agent: input.agent,
          model: input.model,
          messageID: input.messageID,
          variant: input.variant,
        },
        { message: info, parts: resolvedParts },
      )

      const parts = yield* Effect.forEach(resolvedParts, (part) =>
        part.type === "file" && part.mime.startsWith("image/")
          ? image.normalize(part).pipe(
              Effect.catchIf(
                (error) => error instanceof Image.ResizerUnavailableError,
                () => Effect.succeed(part),
              ),
            )
          : Effect.succeed(part),
      )

      const parsed = decodeMessageInfo(info, { errors: "all", propertyOrder: "original" })
      if (Exit.isFailure(parsed)) {
        log.error("invalid user message before save", {
          sessionID: input.sessionID,
          messageID: info.id,
          agent: info.agent,
          model: info.model,
          cause: Cause.pretty(parsed.cause),
        })
      }
      parts.forEach((part, index) => {
        const p = decodeMessagePart(part, { errors: "all", propertyOrder: "original" })
        if (Exit.isSuccess(p)) return
        log.error("invalid user part before save", {
          sessionID: input.sessionID,
          messageID: info.id,
          partID: part.id,
          partType: part.type,
          index,
          cause: Cause.pretty(p.cause),
          part,
        })
      })

      yield* sessions.updateMessage(info)
      for (const part of parts) yield* sessions.updatePart(part)
      const nextPrompt = parts.reduce(
        (result, part) => {
          if (part.type === "text") {
            if (part.synthetic) result.synthetic.push(part.text)
            else result.text.push(part.text)
            const reference = referencePromptMetadata(part.metadata?.reference)
            if (reference) {
              result.references.push(
                new ReferenceAttachment({
                  name: reference.name,
                  kind: reference.kind,
                  uri: reference.path ? pathToFileURL(reference.path).href : undefined,
                  repository: reference.repository,
                  branch: reference.branch,
                  target: reference.target,
                  targetUri: reference.targetPath ? pathToFileURL(reference.targetPath).href : undefined,
                  problem: reference.problem,
                  source: new Source({
                    start: reference.source.start,
                    end: reference.source.end,
                    text: reference.source.value,
                  }),
                }),
              )
            }
          }
          if (part.type === "file") {
            result.files.push(
              new FileAttachment({
                uri: part.url,
                mime: part.mime,
                name: part.filename,
                source: part.source
                  ? new Source({
                      start: part.source.text.start,
                      end: part.source.text.end,
                      text: part.source.text.value,
                    })
                  : undefined,
              }),
            )
          }
          if (part.type === "agent") {
            result.agents.push(
              new AgentAttachment({
                name: part.name,
                source: part.source
                  ? new Source({
                      start: part.source.start,
                      end: part.source.end,
                      text: part.source.value,
                    })
                  : undefined,
              }),
            )
          }
          return result
        },
        {
          text: [] as string[],
          files: [] as FileAttachment[],
          agents: [] as AgentAttachment[],
          references: [] as ReferenceAttachment[],
          synthetic: [] as string[],
        },
      )
      // TODO(v2): Temporary dual-write while migrating session messages to v2 events.
      if (flags.experimentalEventSystem) {
        yield* events.publish(SessionEvent.Prompted, {
          sessionID: input.sessionID,
          timestamp: DateTime.makeUnsafe(info.time.created),
          prompt: {
            text: nextPrompt.text.join("\n"),
            files: nextPrompt.files,
            agents: nextPrompt.agents,
            references: nextPrompt.references,
          },
        })
      }
      for (const text of nextPrompt.synthetic) {
        // TODO(v2): Temporary dual-write while migrating session messages to v2 events.
        if (flags.experimentalEventSystem) {
          yield* events.publish(SessionEvent.Synthetic, {
            sessionID: input.sessionID,
            timestamp: DateTime.makeUnsafe(info.time.created),
            text,
          })
        }
      }

      return { info, parts }
    }, Effect.scoped)

    const prompt: (input: PromptInput) => Effect.Effect<MessageV2.WithParts, Image.Error> = Effect.fn(
      "SessionPrompt.prompt",
    )(function* (input: PromptInput) {
      const session = yield* sessions.get(input.sessionID).pipe(Effect.orDie)
      yield* revert.cleanup(session)
      const message = yield* createUserMessage(input)
      yield* sessions.touch(input.sessionID)

      const permissions: Permission.Ruleset = []
      for (const [t, enabled] of Object.entries(input.tools ?? {})) {
        permissions.push({ permission: t, action: enabled ? "allow" : "deny", pattern: "*" })
      }
      if (permissions.length > 0) {
        session.permission = permissions
        yield* sessions.setPermission({ sessionID: session.id, permission: permissions })
      }

      if (input.noReply === true) return message
      return yield* loop({ sessionID: input.sessionID })
    })

    const lastAssistant = Effect.fnUntraced(function* (sessionID: SessionID) {
      const match = yield* sessions.findMessage(sessionID, (m) => m.info.role !== "user").pipe(Effect.orDie)
      if (Option.isSome(match)) return match.value
      const msgs = yield* sessions.messages({ sessionID, limit: 1 }).pipe(Effect.orDie)
      if (msgs.length > 0) return msgs[0]
      throw new Error("Impossible")
    })

    const runLoop: (sessionID: SessionID) => Effect.Effect<MessageV2.WithParts> = Effect.fn("SessionPrompt.run")(
      function* (sessionID: SessionID) {
        const ctx = yield* InstanceState.context
        const slog = elog.with({ sessionID })
        let structured: unknown
        let step = 0
        let outputLengthContinuations = 0
        let providerFinishErrorAttempts = 0
        const session = yield* sessions.get(sessionID).pipe(Effect.orDie)

        while (true) {
          if ((yield* status.get(sessionID)).type !== "retry") {
            yield* status.set(sessionID, { type: "busy" })
          }
          yield* slog.info("loop", { step })

          let msgs = yield* MessageV2.filterCompactedEffect(sessionID)

          const { user: lastUser, assistant: lastAssistant, finished: lastFinished, tasks } = MessageV2.latest(msgs)

          if (!lastUser) throw new Error("No user message found in stream. This should never happen.")

          const lastAssistantMsg = msgs.findLast(
            (msg) => msg.info.role === "assistant" && msg.info.id === lastAssistant?.id,
          )
          // Some providers return "stop" even when the assistant message contains tool calls.
          // Keep the loop running so tool results can be sent back to the model.
          // Skip provider-executed tool parts — those were fully handled within the
          // provider's stream (e.g. DWS Agent Platform) and don't need a re-loop.
          const hasToolCalls =
            lastAssistantMsg?.parts.some((part) => part.type === "tool" && !part.metadata?.providerExecuted) ?? false
          const continueFromOutputLength =
            lastAssistant?.finish === "length" && !hasToolCalls && lastUser.id < lastAssistant.id
          const continueFromProviderError =
            lastAssistant?.finish === "error" && !lastAssistant.error && lastUser.id < lastAssistant.id

          if (
            lastAssistant?.finish &&
            !["tool-calls", "length"].includes(lastAssistant.finish) &&
            !continueFromProviderError &&
            !hasToolCalls &&
            lastUser.id < lastAssistant.id
          ) {
            yield* slog.info("exiting loop", { finish: lastAssistant.finish })
            break
          }

          if (continueFromOutputLength) {
            outputLengthContinuations++
            if (outputLengthContinuations > OUTPUT_LENGTH_CONTINUATION_LIMIT) {
              lastAssistant.error = new MessageV2.OutputLengthError({}).toObject()
              lastAssistant.time.completed ??= Date.now()
              yield* sessions.updateMessage(lastAssistant)
              yield* bus.publish(Session.Event.Error, { sessionID, error: lastAssistant.error })
              yield* slog.warn("output length continuation limit reached", {
                continuationCount: outputLengthContinuations - 1,
              })
              break
            }
            yield* slog.info("continuing length-limited output", {
              continuationCount: outputLengthContinuations,
            })
          }

          step++
          if (step === 1)
            yield* title({
              session,
              modelID: lastUser.model.modelID,
              providerID: lastUser.model.providerID,
              history: msgs,
            }).pipe(Effect.ignore, Effect.forkIn(scope))

          const model = yield* getModel(lastUser.model.providerID, lastUser.model.modelID, sessionID)
          const task = tasks.pop()

          if (task?.type === "subtask") {
            yield* handleSubtask({ task, model, lastUser, sessionID, session, msgs })
            continue
          }

          if (task?.type === "compaction") {
            const result = yield* compaction.process({
              messages: msgs,
              parentID: lastUser.id,
              sessionID,
              auto: task.auto,
              overflow: task.overflow,
            })
            if (result === "stop") break
            const compactedMsgs = yield* MessageV2.filterCompactedEffect(sessionID)
            const compactedLatest = MessageV2.latest(compactedMsgs)
            const compactedUser = compactedLatest.user ?? lastUser
            const compactedAgent = yield* agents.get(compactedUser.agent)
            const compactedModel = yield* getModel(
              compactedUser.model.providerID,
              compactedUser.model.modelID,
              sessionID,
            )
            const compactedCfg = yield* config.get()
            const compactedModelMsgs = yield* MessageV2.toModelMessagesEffect(compactedMsgs, compactedModel)
            yield* emitContextUsage({
              stage: "compaction_context_ready",
              sessionID,
              messageID: compactedLatest.assistant?.id ?? compactedUser.id,
              parentMessageID: compactedUser.id,
              model: compactedModel,
              agent: compactedAgent,
              messages: compactedMsgs,
              cfg: compactedCfg,
              outputTokenMax: flags.outputTokenMax,
              transformMessages: true,
            })
            yield* emitPromptAudit({
              stage: "compaction_context_ready",
              sessionID,
              messageID: compactedLatest.assistant?.id ?? compactedUser.id,
              parentMessageID: compactedUser.id,
              model: compactedModel,
              agent: compactedAgent,
              messages: compactedMsgs,
              modelMessages: compactedModelMsgs,
              cfg: compactedCfg,
              outputTokenMax: flags.outputTokenMax,
              transformMessages: true,
            })
            continue
          }

          if (
            lastFinished &&
            lastFinished.summary !== true &&
            (yield* compaction.isOverflow({ tokens: lastFinished.tokens, model }))
          ) {
            yield* compaction.create({ sessionID, agent: lastUser.agent, model: lastUser.model, auto: true })
            continue
          }

          const agent = yield* agents.get(lastUser.agent)
          if (!agent) {
            const available = (yield* agents.list()).filter((a) => !a.hidden).map((a) => a.name)
            const hint = available.length ? ` Available agents: ${available.join(", ")}` : ""
            const error = new NamedError.Unknown({ message: `Agent not found: "${lastUser.agent}".${hint}` })
            yield* bus.publish(Session.Event.Error, { sessionID, error: error.toObject() })
            throw error
          }
          const maxSteps = agent.steps ?? Infinity
          const isLastStep = step >= maxSteps
          msgs = yield* SessionReminders.apply({ messages: msgs, agent, session }).pipe(
            Effect.provideService(RuntimeFlags.Service, flags),
            Effect.provideService(AppFileSystem.Service, fsys),
            Effect.provideService(Session.Service, sessions),
          )

          const msg: MessageV2.Assistant = {
            id: MessageID.ascending(),
            parentID: lastUser.id,
            role: "assistant",
            mode: agent.name,
            agent: agent.name,
            variant: lastUser.model.variant,
            path: { cwd: ctx.directory, root: ctx.worktree },
            cost: 0,
            tokens: { input: 0, output: 0, reasoning: 0, cache: { read: 0, write: 0 } },
            modelID: model.id,
            providerID: model.providerID,
            time: { created: Date.now() },
            sessionID,
          }
          yield* sessions.updateMessage(msg)

          if (isImageModel(model)) {
            yield* generateImage({
              sessionID,
              user: lastUser,
              assistant: msg,
              model,
              parts: msgs,
            })
            break
          }

          if (isVideoModel(model)) {
            yield* generateVideo({
              sessionID,
              user: lastUser,
              assistant: msg,
              model,
              parts: msgs,
            })
            break
          }

          const finalizeInterruptedAssistant = Effect.gen(function* () {
            if (msg.time.completed) return
            msg.error ??= MessageV2.fromError(new DOMException("Aborted", "AbortError"), {
              providerID: msg.providerID,
              aborted: true,
            })
            msg.time.completed = Date.now()
            yield* sessions.updateMessage(msg)
          })

          const handle = yield* processor
            .create({
              assistantMessage: msg,
              sessionID,
              model,
            })
            .pipe(Effect.onInterrupt(() => finalizeInterruptedAssistant))

          const outcome: "break" | "continue" = yield* Effect.gen(function* () {
            const lastUserMsg = msgs.findLast((m) => m.info.role === "user")
            const bypassAgentCheck = lastUserMsg?.parts.some((p) => p.type === "agent") ?? false
            const promptOps = yield* ops()

            const tools = yield* SessionTools.resolve({
              agent,
              session,
              model,
              processor: handle,
              bypassAgentCheck,
              messages: msgs,
              promptOps,
            }).pipe(
              Effect.provideService(Plugin.Service, plugin),
              Effect.provideService(Permission.Service, permission),
              Effect.provideService(ToolRegistry.Service, registry),
              Effect.provideService(MCP.Service, mcp),
              Effect.provideService(Truncate.Service, truncate),
            )

            if (lastUser.format?.type === "json_schema") {
              tools["StructuredOutput"] = createStructuredOutputTool({
                schema: lastUser.format.schema,
                onSuccess(output) {
                  structured = output
                },
              })
            }

            if (step === 1)
              yield* summary.summarize({ sessionID, messageID: lastUser.id }).pipe(Effect.ignore, Effect.forkIn(scope))

            if (step > 1 && lastFinished) {
              for (const m of msgs) {
                if (m.info.role !== "user" || m.info.id <= lastFinished.id) continue
                for (const p of m.parts) {
                  if (p.type !== "text" || p.ignored || p.synthetic) continue
                  if (!p.text.trim()) continue
                  p.text = [
                    "<system-reminder>",
                    "The user sent the following message:",
                    p.text,
                    "",
                    "Please address this message and continue with your tasks.",
                    "</system-reminder>",
                  ].join("\n")
                }
              }
            }

            const [skills, env, instructions, modelMsgs] = yield* Effect.all([
              sys.skills(agent),
              sys.environment(model),
              instruction.system().pipe(Effect.orDie),
              Effect.gen(function* () {
                const transformed = structuredClone(msgs)
                yield* plugin.trigger("experimental.chat.messages.transform", {}, { messages: transformed })
                msgs = transformed
                return yield* MessageV2.toModelMessagesEffect(msgs, model)
              }),
            ])
            const system = [...env, ...instructions, ...(skills ? [skills] : [])]
            const format = lastUser.format ?? { type: "text" as const }
            if (format.type === "json_schema") system.push(STRUCTURED_OUTPUT_SYSTEM_PROMPT)
            const cfg = yield* config.get()
            const userInputText = msgs
              .findLast((item) => item.info.role === "user" && item.info.id === lastUser.id)
              ?.parts.filter((part): part is MessageV2.TextPart => part.type === "text")
              .map((part) => part.text)
              .join("\n\n")
            const verifyText = userInputText ?? ""
            if (isAndroidReverseAgent(agent.name)) {
              const connected = isVerifyMcpConnected(yield* mcp.status())
              const configExists = yield* fsys.existsSafe(verifyProjectConfigPath(ctx.directory))
              const verifyIntent = hasVerifyIntent(verifyText)
              const fridaIntent = hasFridaIntent(verifyText)
              const apkTargetIntent = hasApkTargetIntent(verifyText)
              if (connected && (verifyIntent || apkTargetIntent || (configExists && fridaIntent))) {
                system.push(verifyPrompt({ configExists, fridaRequested: fridaIntent }))
              }
            }
            yield* emitPromptAudit({
              stage: "request_context_ready",
              sessionID,
              messageID: msg.id,
              parentMessageID: lastUser.id,
              model,
              agent,
              messages: msgs,
              modelMessages: modelMsgs,
              system,
              cfg,
              outputTokenMax: flags.outputTokenMax,
              userInputText,
              tools,
              transformMessages: false,
            })
            yield* emitContextUsage({
              stage: "request_context_ready",
              sessionID,
              messageID: msg.id,
              parentMessageID: lastUser.id,
              model,
              agent,
              messages: msgs,
              system,
              cfg,
              outputTokenMax: flags.outputTokenMax,
              userInputText,
              tools,
              transformMessages: false,
            })
            const result = yield* handle.process({
              user: lastUser,
              agent,
              permission: session.permission,
              sessionID,
              parentSessionID: session.parentID,
              system,
              messages: [
                ...modelMsgs,
                ...(continueFromOutputLength
                  ? [{ role: "user" as const, content: OUTPUT_LENGTH_CONTINUATION_PROMPT }]
                  : []),
                ...(continueFromProviderError
                  ? [{ role: "user" as const, content: PROVIDER_FINISH_ERROR_CONTINUATION_PROMPT }]
                  : []),
                ...(isLastStep ? [{ role: "assistant" as const, content: MAX_STEPS }] : []),
              ],
              tools,
              model,
              toolChoice: format.type === "json_schema" ? "required" : undefined,
            })

            if (structured !== undefined) {
              handle.message.structured = structured
              handle.message.finish = handle.message.finish ?? "stop"
              yield* sessions.updateMessage(handle.message)
              return "break" as const
            }

            const finished = handle.message.finish && !["tool-calls", "unknown"].includes(handle.message.finish)
            if (finished && !handle.message.error) {
              if (handle.message.finish === "error") {
                providerFinishErrorAttempts++
                const retryError = new MessageV2.APIError({
                  message: "The provider ended the response with finish reason error",
                  isRetryable: true,
                  metadata: { finish_reason: "error" },
                }).toObject()
                if (providerFinishErrorAttempts >= SessionRetry.RETRY_MAX_ATTEMPTS) {
                  handle.message.error = retryError
                  yield* sessions.updateMessage(handle.message)
                  yield* bus.publish(Session.Event.Error, { sessionID, error: handle.message.error })
                  yield* slog.warn("provider finish error retry limit reached", {
                    attempt: providerFinishErrorAttempts,
                    maxAttempts: SessionRetry.RETRY_MAX_ATTEMPTS,
                  })
                  return "break" as const
                }
                const next = Date.now() + PROVIDER_FINISH_ERROR_RETRY_DELAY
                if (flags.experimentalEventSystem) {
                  yield* events.publish(SessionEvent.Retried, {
                    sessionID,
                    attempt: providerFinishErrorAttempts,
                    error: {
                      message: "The provider ended the response unexpectedly",
                      isRetryable: true,
                      metadata: { finish_reason: "error" },
                    },
                    timestamp: DateTime.makeUnsafe(Date.now()),
                  })
                }
                yield* status.set(sessionID, {
                  type: "retry",
                  attempt: providerFinishErrorAttempts,
                  maxAttempts: SessionRetry.RETRY_MAX_ATTEMPTS,
                  message: "The provider ended the response unexpectedly",
                  next,
                })
                yield* slog.warn("retrying provider finish error in a new model round", {
                  attempt: providerFinishErrorAttempts,
                  nextAttempt: providerFinishErrorAttempts + 1,
                  maxAttempts: SessionRetry.RETRY_MAX_ATTEMPTS,
                  next,
                })
                yield* Effect.sleep(PROVIDER_FINISH_ERROR_RETRY_DELAY)
                return "continue" as const
              }
              if (handle.message.finish === "content-filter") {
                handle.message.error = new MessageV2.APIError({
                  message: "The response was blocked by the provider's content filter",
                  isRetryable: false,
                }).toObject()
                yield* sessions.updateMessage(handle.message)
                yield* bus.publish(Session.Event.Error, { sessionID, error: handle.message.error })
                return "break" as const
              }
              if (format.type === "json_schema") {
                handle.message.error = new MessageV2.StructuredOutputError({
                  message: "Model did not produce structured output",
                  retries: 0,
                }).toObject()
                yield* sessions.updateMessage(handle.message)
                return "break" as const
              }
            }

            if (result === "stop") return "break" as const
            if (result === "compact") {
              yield* compaction.create({
                sessionID,
                agent: lastUser.agent,
                model: lastUser.model,
                auto: true,
                overflow: handle.compactionReason === "provider_context_overflow",
              })
            }
            return "continue" as const
          }).pipe(
            Effect.ensuring(instruction.clear(handle.message.id)),
            Effect.onInterrupt(() => finalizeInterruptedAssistant),
          )
          if (outcome === "break") break
          continue
        }

        yield* compaction.prune({ sessionID }).pipe(Effect.ignore, Effect.forkIn(scope))
        return yield* lastAssistant(sessionID)
      },
    )

    const loop: (input: LoopInput) => Effect.Effect<MessageV2.WithParts> = Effect.fn("SessionPrompt.loop")(function* (
      input: LoopInput,
    ) {
      return yield* state.ensureRunning(input.sessionID, lastAssistant(input.sessionID), runLoop(input.sessionID))
    })

    const shell: (input: ShellInput) => Effect.Effect<MessageV2.WithParts, Session.BusyError> = Effect.fn(
      "SessionPrompt.shell",
    )(function* (input: ShellInput) {
      const ready = yield* Latch.make()
      return yield* state.startShell(input.sessionID, lastAssistant(input.sessionID), shellImpl(input, ready), ready)
    })

    const command = Effect.fn("SessionPrompt.command")(function* (input: CommandInput) {
      yield* elog.info("command", { sessionID: input.sessionID, command: input.command, agent: input.agent })
      const cmd = yield* commands.get(input.command)
      if (!cmd) {
        const available = (yield* commands.list()).map((c) => c.name)
        const hint = available.length ? ` Available commands: ${available.join(", ")}` : ""
        const error = new NamedError.Unknown({ message: `Command not found: "${input.command}".${hint}` })
        yield* bus.publish(Session.Event.Error, { sessionID: input.sessionID, error: error.toObject() })
        throw error
      }
      const agentName = cmd.agent ?? input.agent

      const raw = input.arguments.match(argsRegex) ?? []
      const args = raw.map((arg) => arg.replace(quoteTrimRegex, ""))
      const templateCommand = yield* Effect.promise(async () => cmd.template)

      const placeholders = templateCommand.match(placeholderRegex) ?? []
      let last = 0
      for (const item of placeholders) {
        const value = Number(item.slice(1))
        if (value > last) last = value
      }

      const withArgs = templateCommand.replaceAll(placeholderRegex, (_, index) => {
        const position = Number(index)
        const argIndex = position - 1
        if (argIndex >= args.length) return ""
        if (position === last) return args.slice(argIndex).join(" ")
        return args[argIndex]
      })
      const usesArgumentsPlaceholder = templateCommand.includes("$ARGUMENTS")
      let template = withArgs.replaceAll("$ARGUMENTS", input.arguments)

      if (placeholders.length === 0 && !usesArgumentsPlaceholder && input.arguments.trim()) {
        template = template + "\n\n" + input.arguments
      }

      const shellMatches = ConfigMarkdown.shell(template)
      if (shellMatches.length > 0) {
        const cfg = yield* config.get()
        const sh = Shell.preferred(cfg.shell)
        const results = yield* Effect.promise(() =>
          Promise.all(
            shellMatches.map(async ([, cmd]) => (await Process.text([cmd], { shell: sh, nothrow: true })).text),
          ),
        )
        let index = 0
        template = template.replace(bashRegex, () => results[index++])
      }
      template = template.trim()

      const taskModel = yield* Effect.gen(function* () {
        if (cmd.model) return Provider.parseModel(cmd.model)
        if (cmd.agent) {
          const cmdAgent = yield* agents.get(cmd.agent)
          if (cmdAgent?.model) return cmdAgent.model
        }
        if (input.model) return Provider.parseModel(input.model)
        return yield* currentModel(input.sessionID)
      })

      yield* getModel(taskModel.providerID, taskModel.modelID, input.sessionID)

      const agent = agentName ? yield* agents.get(agentName) : yield* agents.defaultInfo()
      if (!agent) {
        const available = (yield* agents.list()).filter((a) => !a.hidden).map((a) => a.name)
        const hint = available.length ? ` Available agents: ${available.join(", ")}` : ""
        const error = new NamedError.Unknown({ message: `Agent not found: "${agentName}".${hint}` })
        yield* bus.publish(Session.Event.Error, { sessionID: input.sessionID, error: error.toObject() })
        throw error
      }

      const templateParts = yield* resolvePromptParts(template)
      const isSubtask = (agent.mode === "subagent" && cmd.subtask !== false) || cmd.subtask === true
      const parts = isSubtask
        ? [
            {
              type: "subtask" as const,
              agent: agent.name,
              description: cmd.description ?? "",
              command: input.command,
              model: { providerID: taskModel.providerID, modelID: taskModel.modelID },
              prompt: templateParts.find((y) => y.type === "text")?.text ?? "",
            },
          ]
        : [...templateParts, ...(input.parts ?? [])]

      const userAgent = isSubtask ? (input.agent ?? (yield* agents.defaultInfo()).name) : agent.name
      const userModel = isSubtask
        ? input.model
          ? Provider.parseModel(input.model)
          : yield* currentModel(input.sessionID)
        : taskModel

      yield* plugin.trigger(
        "command.execute.before",
        { command: input.command, sessionID: input.sessionID, arguments: input.arguments },
        { parts },
      )

      const result = yield* prompt({
        sessionID: input.sessionID,
        messageID: input.messageID,
        model: userModel,
        agent: userAgent,
        parts,
        variant: input.variant,
      })
      yield* bus.publish(Command.Event.Executed, {
        name: input.command,
        sessionID: input.sessionID,
        arguments: input.arguments,
        messageID: result.info.id,
      })
      return result
    })

    return Service.of({
      cancel,
      prompt,
      loop,
      shell,
      command,
      resolvePromptParts,
    })
  }),
)

export const defaultLayer = Layer.suspend(() => {
  const promptServiceDeps = Layer.mergeAll(
    SessionRunState.defaultLayer,
    SessionStatus.defaultLayer,
    SessionCompaction.defaultLayer,
    SessionProcessor.defaultLayer,
    Command.defaultLayer,
    Permission.defaultLayer,
    MCP.defaultLayer,
    LSP.defaultLayer,
    ToolRegistry.defaultLayer,
    Truncate.defaultLayer,
    Provider.defaultLayer,
    Auth.defaultLayer,
    Config.defaultLayer,
    Instruction.defaultLayer,
    AppFileSystem.defaultLayer,
    Plugin.defaultLayer,
    Session.defaultLayer,
    SessionRevert.defaultLayer,
    SessionSummary.defaultLayer,
    Image.defaultLayer,
  )
  const promptBaseDeps = Layer.mergeAll(
    EventV2Bridge.defaultLayer,
    Agent.defaultLayer,
    SystemPrompt.defaultLayer,
    LLM.defaultLayer,
    Reference.defaultLayer,
    Bus.layer,
    CrossSpawnSpawner.defaultLayer,
    RuntimeFlags.defaultLayer,
  )
  return layer.pipe(Layer.provide(promptServiceDeps), Layer.provide(promptBaseDeps))
})
const ModelRef = Schema.Struct({
  providerID: ProviderID,
  modelID: ModelID,
})

export const PromptInput = Schema.Struct({
  sessionID: SessionID,
  messageID: Schema.optional(MessageID),
  model: Schema.optional(ModelRef),
  agent: Schema.optional(Schema.String),
  noReply: Schema.optional(Schema.Boolean),
  tools: Schema.optional(Schema.Record(Schema.String, Schema.Boolean)).annotate({
    description:
      "@deprecated tools and permissions have been merged, you can set permissions on the session itself now",
  }),
  format: Schema.optional(MessageV2.Format),
  system: Schema.optional(Schema.String),
  variant: Schema.optional(Schema.String),
  media: Schema.optional(MessageV2.MessageMedia),
  parts: Schema.Array(
    Schema.Union([
      MessageV2.TextPartInput,
      MessageV2.FilePartInput,
      MessageV2.AgentPartInput,
      MessageV2.SubtaskPartInput,
    ]).annotate({ discriminator: "type" }),
  ),
})
export type PromptInput = Schema.Schema.Type<typeof PromptInput>

/**
 * 只输出媒体（图片 / 视频）的模型：只能用生图/生视频接口，不能当对话模型用。
 * 标题生成、小模型选择这类内部调用必须避开它们，否则必然失败。
 */
export function isMediaOnlyModel(model: Provider.Model) {
  const output = model.capabilities.output
  return (output.image || output.video) && !output.text
}

/**
 * 生图只用「本次用户输入」这一条消息。
 *
 * 生图是单轮请求：把整段会话的文本拼进 prompt、把历史里的图片当输入图，会让
 * 第二次生图带上上一轮的要求、并因为历史里有图而走成图生图（edits）。
 *
 * 注意不能用 `input.user.id` 过滤：`input.parts` 里的消息对象与 `input.user`
 * 不是同一实例，按 id 比对会拿不到任何内容。这里取最后一条 user 消息。
 */
export function currentImageTurn(parts: MessageV2.WithParts[]): MessageV2.WithParts[] {
  const current = [...parts].reverse().find((item) => item.info.role === "user")
  return current ? [current] : []
}
export class LoopInput extends Schema.Class<LoopInput>("SessionPrompt.LoopInput")({
  sessionID: SessionID,
}) {}

export const ShellInput = Schema.Struct({
  sessionID: SessionID,
  messageID: Schema.optional(MessageID),
  agent: Schema.String,
  model: Schema.optional(ModelRef),
  command: Schema.String,
})
export type ShellInput = Schema.Schema.Type<typeof ShellInput>

export const CommandInput = Schema.Struct({
  messageID: Schema.optional(MessageID),
  sessionID: SessionID,
  agent: Schema.optional(Schema.String),
  model: Schema.optional(Schema.String),
  arguments: Schema.String,
  command: Schema.String,
  variant: Schema.optional(Schema.String),
  // Inlined (no identifier annotation) to keep the original SDK output — the
  // PromptInput call site below references FilePartInput by ref via the
  // Schema export in message-v2.ts.
  parts: Schema.optional(
    Schema.Array(
      Schema.Union([
        Schema.Struct({
          id: Schema.optional(PartID),
          type: Schema.Literal("file"),
          mime: Schema.String,
          filename: Schema.optional(Schema.String),
          url: Schema.String,
          source: Schema.optional(MessageV2.FilePartSource),
        }),
      ]).annotate({ discriminator: "type" }),
    ),
  ),
})
export type CommandInput = Schema.Schema.Type<typeof CommandInput>

/** @internal Exported for testing */
export function createStructuredOutputTool(input: {
  schema: Record<string, any>
  onSuccess: (output: unknown) => void
}): AITool {
  // Remove $schema property if present (not needed for tool input)
  const { $schema: _, ...toolSchema } = input.schema

  return tool({
    description: STRUCTURED_OUTPUT_DESCRIPTION,
    inputSchema: jsonSchema(toolSchema as JSONSchema7),
    async execute(args) {
      // AI SDK validates args against inputSchema before calling execute()
      input.onSuccess(args)
      return {
        output: "Structured output captured successfully.",
        title: "Structured Output",
        metadata: { valid: true },
      }
    },
    toModelOutput({ output }) {
      return {
        type: "text",
        value: output.output,
      }
    },
  })
}
const bashRegex = /!`([^`]+)`/g
// Match [Image N] as single token, quoted strings, or non-space sequences
const argsRegex = /(?:\[Image\s+\d+\]|"[^"]*"|'[^']*'|[^\s"']+)/gi
const placeholderRegex = /\$(\d+)/g
const quoteTrimRegex = /^["']|["']$/g

export * as SessionPrompt from "./prompt"
