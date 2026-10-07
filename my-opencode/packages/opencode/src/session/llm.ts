import { Provider } from "@/provider/provider"
import * as Log from "@opencode-ai/core/util/log"
import { Context, Effect, Layer, Record } from "effect"
import * as Stream from "effect/Stream"
import {
  streamText,
  wrapLanguageModel,
  type LanguageModelMiddleware,
  type ModelMessage,
  type Tool,
  tool as aiTool,
  jsonSchema,
} from "ai"
import type { LanguageModelV3StreamPart } from "@ai-sdk/provider"
import type { LLMEvent } from "@opencode-ai/llm"
import { LLMClient, RequestExecutor } from "@opencode-ai/llm/route"
import type { LLMClientService } from "@opencode-ai/llm/route"
import { mergeDeep } from "remeda"
import { GitLabWorkflowLanguageModel } from "gitlab-ai-provider"
import { ProviderTransform } from "@/provider/transform"
import { Config } from "@/config/config"
import { InstanceState } from "@/effect/instance-state"
import type { Agent } from "@/agent/agent"
import type { MessageV2 } from "./message-v2"
import { Plugin } from "@/plugin"
import { SystemPrompt } from "./system"
import { Permission } from "@/permission"
import { PermissionID } from "@/permission/schema"
import { Bus } from "@/bus"
import { GlobalBus } from "@/bus/global"
import { Wildcard } from "@/util/wildcard"
import { SessionID } from "@/session/schema"
import { Auth } from "@/auth"
import { InstallationVersion } from "@opencode-ai/core/installation/version"
import { EffectBridge } from "@/effect/bridge"
import { RuntimeFlags } from "@/effect/runtime-flags"
import * as Option from "effect/Option"
import * as OtelTracer from "@effect/opentelemetry/Tracer"
import { LLMAISDK } from "./llm/ai-sdk"
import { LLMNativeRuntime } from "./llm/native-runtime"
import { isLazyMcpTool } from "./mcp-lazy"

const log = Log.create({ service: "llm" })
export const OUTPUT_TOKEN_MAX = ProviderTransform.OUTPUT_TOKEN_MAX

// Avoid re-instantiating remeda's deep merge types in this hot LLM path; the runtime behavior is still mergeDeep.
const mergeOptions = (target: Record<string, any>, source: Record<string, any> | undefined): Record<string, any> =>
  mergeDeep(target, source ?? {}) as Record<string, any>

const firstEventStages = new Set([
  "start",
  "start-step",
  "stream-start",
  "response-metadata",
  "text-start",
  "text-delta",
  "text-end",
  "reasoning-start",
  "reasoning-delta",
  "reasoning-end",
  "tool-input-start",
  "tool-input-delta",
  "tool-input-end",
  "tool-call",
  "tool-result",
  "tool-error",
  "finish-step",
  "finish",
  "provider-error",
  "error",
  "abort",
  "raw",
])

type AISDKEvent = Awaited<ReturnType<typeof streamText>>["fullStream"] extends AsyncIterable<infer T> ? T : never
type LatencyBase = {
  provider_id: string
  model_id: string
  session_id: string
  user_message_id: string
  agent: string
  small: boolean
}

const providerEventSeen = new WeakMap<LatencyBase, Set<string>>()
const aiSdkEventSeen = new WeakMap<LatencyBase, Set<string>>()
const llmEventSeen = new WeakMap<LatencyBase, Set<string>>()
const FIRST_EVENT_TIMEOUTS = [30_000, 60_000, 120_000, 180_000, 300_000] as const
const DEFAULT_CHUNK_TIMEOUTS = [120_000, 180_000, 300_000, 420_000, 600_000] as const
const DEFAULT_REQUEST_TIMEOUTS = [180_000, 300_000, 480_000, 720_000, 900_000] as const
const DEFAULT_TOOL_INPUT_TIMEOUT = 60_000
const MIN_TOOL_INPUT_TIMEOUT = 30_000

function attemptTimeout(attempt: number | undefined, configured: unknown, defaults: readonly number[]) {
  if (typeof configured === "number" && configured > 0) return configured
  const timeouts =
    Array.isArray(configured) &&
    configured.length > 0 &&
    configured.every((item) => typeof item === "number" && item > 0)
      ? configured
      : defaults
  const index = Math.min(Math.max((attempt ?? 1) - 1, 0), timeouts.length - 1)
  return timeouts[index]
}

function firstEventTimeout(attempt: number | undefined, configured?: unknown) {
  return attemptTimeout(attempt, configured, FIRST_EVENT_TIMEOUTS)
}

function requestTimeout(attempt: number | undefined, configured?: unknown) {
  if (configured === false) return false
  return attemptTimeout(attempt, configured, DEFAULT_REQUEST_TIMEOUTS)
}

function chunkTimeout(attempt: number | undefined, configured?: unknown) {
  if (configured === false) return false
  return attemptTimeout(attempt, configured, DEFAULT_CHUNK_TIMEOUTS)
}

function toolInputTimeout(attempt: number | undefined, configured: unknown, chunk: number | false) {
  if (configured === false || chunk === false) return false
  if (configured !== undefined) return attemptTimeout(attempt, configured, DEFAULT_CHUNK_TIMEOUTS)
  return Math.min(DEFAULT_TOOL_INPUT_TIMEOUT, Math.max(MIN_TOOL_INPUT_TIMEOUT, Math.floor(chunk / 2)))
}

function timeoutError(message: string) {
  return new DOMException(message, "TimeoutError")
}

export type StreamInput = {
  user: MessageV2.User
  sessionID: string
  parentSessionID?: string
  model: Provider.Model
  agent: Agent.Info
  permission?: Permission.Ruleset
  system: string[]
  messages: ModelMessage[]
  small?: boolean
  tools: Record<string, Tool>
  retries?: number
  attempt?: number
  toolChoice?: "auto" | "required" | "none"
}

export type StreamRequest = StreamInput & {
  abort: AbortSignal
}

export interface Interface {
  readonly stream: (input: StreamInput) => Stream.Stream<LLMEvent, unknown>
}

export class Service extends Context.Service<Service, Interface>()("@opencode/LLM") {}

const live: Layer.Layer<
  Service,
  never,
  | Auth.Service
  | Config.Service
  | Provider.Service
  | Plugin.Service
  | Permission.Service
  | LLMClientService
  | RuntimeFlags.Service
> = Layer.effect(
  Service,
  Effect.gen(function* () {
    const auth = yield* Auth.Service
    const config = yield* Config.Service
    const provider = yield* Provider.Service
    const plugin = yield* Plugin.Service
    const perm = yield* Permission.Service
    const llmClient = yield* LLMClient.Service
    const flags = yield* RuntimeFlags.Service

    const run = Effect.fn("LLM.run")(function* (input: StreamRequest) {
      const l = log
        .clone()
        .tag("providerID", input.model.providerID)
        .tag("modelID", input.model.id)
        .tag("session.id", input.sessionID)
        .tag("small", (input.small ?? false).toString())
        .tag("agent", input.agent.name)
        .tag("mode", input.agent.mode)
      l.info("stream", {
        modelID: input.model.id,
        providerID: input.model.providerID,
      })
      const latencyBase = {
        provider_id: input.model.providerID,
        model_id: input.model.id,
        session_id: input.sessionID,
        user_message_id: input.user.id,
        agent: input.agent.name,
        small: input.small ?? false,
      }
      const streamStartedAt = Date.now()
      const attempt = input.attempt ?? 1
      const attemptMax = FIRST_EVENT_TIMEOUTS.length
      llmLatency("stream_requested", streamStartedAt, {
        ...latencyBase,
        attempt,
        max_attempts: attemptMax,
      })

      const [language, cfg, item, info] = yield* Effect.all(
        [
          provider.getLanguage(input.model),
          config.get(),
          provider.getProvider(input.model.providerID),
          auth.get(input.model.providerID),
        ],
        { concurrency: "unbounded" },
      )
      llmLatency("provider_config_ready", streamStartedAt, {
        ...latencyBase,
        provider_npm: input.model.api.npm,
        has_auth: !!info,
        attempt,
        max_attempts: attemptMax,
      })

      // TODO: move this to a proper hook
      const isOpenaiOauth = item.id === "openai" && info?.type === "oauth"

      const system: string[] = []
      system.push(
        [
          // Keep the shared service rules for every agent and model. The role prompt is an extension.
          ...(input.agent.prompt
            ? [input.agent.prompt, ...SystemPrompt.provider(input.model)]
            : SystemPrompt.provider(input.model)),
          // any custom prompt passed into this call
          ...input.system,
          // any custom prompt from last user message
          ...(input.user.system ? [input.user.system] : []),
          SystemPrompt.QUESTION_POLICY,
        ]
          .filter((x) => x)
          .join("\n"),
      )

      const header = system[0]
      yield* plugin.trigger(
        "experimental.chat.system.transform",
        { sessionID: input.sessionID, model: input.model },
        { system },
      )
      // rejoin to maintain 2-part structure for caching if header unchanged
      if (system.length > 2 && system[0] === header) {
        const rest = system.slice(1)
        system.length = 0
        system.push(header, rest.join("\n"))
      }

      const variant =
        !input.small && input.model.variants && input.user.model.variant
          ? input.model.variants[input.user.model.variant]
          : {}
      const base = input.small
        ? ProviderTransform.smallOptions(input.model)
        : ProviderTransform.options({
            model: input.model,
            sessionID: input.sessionID,
            providerOptions: item.options,
          })
      const options = mergeOptions(mergeOptions(mergeOptions(base, input.model.options), input.agent.options), variant)
      options.timeout = requestTimeout(attempt, options.timeout ?? item.options?.timeout)
      options.chunkTimeout = chunkTimeout(attempt, options.chunkTimeout ?? item.options?.chunkTimeout)
      const attemptToolInputTimeout = toolInputTimeout(
        attempt,
        options.toolInputTimeout ?? item.options?.toolInputTimeout,
        options.chunkTimeout,
      )
      const attemptFirstEventTimeout = firstEventTimeout(
        attempt,
        options.firstEventTimeout ?? item.options?.firstEventTimeout,
      )
      llmLatency("request_timeout_config_ready", streamStartedAt, {
        ...latencyBase,
        attempt,
        max_attempts: attemptMax,
        first_event_timeout_ms: attemptFirstEventTimeout,
        request_timeout_ms: options.timeout === false ? false : options.timeout,
        chunk_timeout_ms: options.chunkTimeout,
        tool_input_timeout_ms: attemptToolInputTimeout,
      })
      if (isOpenaiOauth) {
        options.instructions = system.join("\n")
      }

      const isWorkflow = language instanceof GitLabWorkflowLanguageModel
      const messages = isOpenaiOauth
        ? input.messages
        : isWorkflow
          ? input.messages
          : [
              ...system.map(
                (x): ModelMessage => ({
                  role: "system",
                  content: x,
                }),
              ),
              ...input.messages,
            ]

      const params = yield* plugin.trigger(
        "chat.params",
        {
          sessionID: input.sessionID,
          agent: input.agent.name,
          model: input.model,
          provider: item,
          message: input.user,
        },
        {
          temperature: input.model.capabilities.temperature
            ? (input.agent.temperature ?? ProviderTransform.temperature(input.model))
            : undefined,
          topP: input.agent.topP ?? ProviderTransform.topP(input.model),
          topK: ProviderTransform.topK(input.model),
          maxOutputTokens: ProviderTransform.maxOutputTokens(input.model, flags.outputTokenMax),
          options,
        },
      )

      const { headers } = yield* plugin.trigger(
        "chat.headers",
        {
          sessionID: input.sessionID,
          agent: input.agent.name,
          model: input.model,
          provider: item,
          message: input.user,
        },
        {
          headers: {},
        },
      )

      const tools = resolveTools(input)

      // GitHub Copilot may require the tools parameter when message history contains
      // tool calls but no tools are active (e.g. compaction). Inject a stub tool that
      // is never meant to be invoked. LiteLLM-backed providers are excluded.
      if (
        input.model.providerID.includes("github-copilot") &&
        Object.keys(tools).length === 0 &&
        hasToolCalls(input.messages)
      ) {
        tools["_noop"] = aiTool({
          description: "Do not call this tool. It exists only for API compatibility and must never be invoked.",
          inputSchema: jsonSchema({
            type: "object",
            properties: {
              reason: { type: "string", description: "Unused" },
            },
          }),
          execute: async () => ({ output: "", title: "", metadata: {} }),
        })
      }
      const sortedTools = Object.fromEntries(Object.entries(tools).toSorted(([a], [b]) => a.localeCompare(b)))

      // Wire up toolExecutor for DWS workflow models so that tool calls
      // from the workflow service are executed via opencode's tool system
      // and results sent back over the WebSocket.
      if (language instanceof GitLabWorkflowLanguageModel) {
        const workflowModel = language as GitLabWorkflowLanguageModel & {
          sessionID?: string
          sessionPreapprovedTools?: string[]
          approvalHandler?: (approvalTools: { name: string; args: string }[]) => Promise<{ approved: boolean }>
        }
        workflowModel.sessionID = input.sessionID
        workflowModel.systemPrompt = system.join("\n")
        workflowModel.toolExecutor = async (toolName, argsJson, _requestID) => {
          const t = sortedTools[toolName]
          if (!t || !t.execute) {
            return { result: "", error: `Unknown tool: ${toolName}` }
          }
          try {
            const result = await t.execute!(JSON.parse(argsJson), {
              toolCallId: _requestID,
              messages: input.messages,
              abortSignal: input.abort,
            })
            const output = typeof result === "string" ? result : (result?.output ?? JSON.stringify(result))
            return {
              result: output,
              metadata: typeof result === "object" ? result?.metadata : undefined,
              title: typeof result === "object" ? result?.title : undefined,
            }
          } catch (e: any) {
            return { result: "", error: e.message ?? String(e) }
          }
        }

        const ruleset = Permission.merge(input.agent.permission ?? [], input.permission ?? [])
        workflowModel.sessionPreapprovedTools = Object.keys(sortedTools).filter((name) => {
          const match = ruleset.findLast((rule) => Wildcard.match(name, rule.permission))
          return !match || match.action !== "ask"
        })

        const bridge = yield* EffectBridge.make()
        const approvedToolsForSession = new Set<string>()
        workflowModel.approvalHandler = bridge.bind(async (approvalTools) => {
          const uniqueNames = [...new Set(approvalTools.map((t: { name: string }) => t.name))] as string[]
          // Auto-approve tools that were already approved in this session
          // (prevents infinite approval loops for server-side MCP tools)
          if (uniqueNames.every((name) => approvedToolsForSession.has(name))) {
            return { approved: true }
          }

          const id = PermissionID.ascending()
          let unsub: (() => void) | undefined
          try {
            unsub = Bus.subscribe(Permission.Event.Replied, (evt) => {
              if (evt.properties.requestID === id) void evt.properties.reply
            })
            const toolPatterns = approvalTools.map((t: { name: string; args: string }) => {
              try {
                const parsed = JSON.parse(t.args) as Record<string, unknown>
                const title = (parsed?.title ?? parsed?.name ?? "") as string
                return title ? `${t.name}: ${title}` : t.name
              } catch {
                return t.name
              }
            })
            const uniquePatterns = [...new Set(toolPatterns)] as string[]
            await bridge.promise(
              perm.ask({
                id,
                sessionID: SessionID.make(input.sessionID),
                permission: "workflow_tool_approval",
                patterns: uniquePatterns,
                metadata: { tools: approvalTools },
                always: uniquePatterns,
                ruleset: [],
              }),
            )
            for (const name of uniqueNames) approvedToolsForSession.add(name)
            workflowModel.sessionPreapprovedTools = [...(workflowModel.sessionPreapprovedTools ?? []), ...uniqueNames]
            return { approved: true }
          } catch {
            return { approved: false }
          } finally {
            unsub?.()
          }
        })
      }

      const tracer = cfg.experimental?.openTelemetry
        ? Option.getOrUndefined(yield* Effect.serviceOption(OtelTracer.OtelTracer))
        : undefined
      const telemetryTracer = tracer
        ? new Proxy(tracer, {
            get(target, prop, receiver) {
              if (prop !== "startSpan") return Reflect.get(target, prop, receiver)
              return (...args: Parameters<typeof target.startSpan>) => {
                const span = target.startSpan(...args)
                span.setAttribute("session.id", input.sessionID)
                return span
              }
            },
          })
        : undefined

      const opencodeProjectID = input.model.providerID.startsWith("opencode")
        ? (yield* InstanceState.context).project.id
        : undefined

      const requestHeaders = {
        ...(input.model.providerID.startsWith("opencode")
          ? {
              ...(opencodeProjectID ? { "x-opencode-project": opencodeProjectID } : {}),
              "x-opencode-session": input.sessionID,
              "x-opencode-request": input.user.id,
              "x-opencode-client": flags.client,
              "User-Agent": `opencode/${InstallationVersion}`,
            }
          : {
              "x-session-affinity": input.sessionID,
              ...(input.parentSessionID ? { "x-parent-session-id": input.parentSessionID } : {}),
              "User-Agent": `opencode/${InstallationVersion}`,
            }),
        ...input.model.headers,
        ...headers,
      }

      const useNativeRuntime = flags.experimentalNativeLlm

      if (useNativeRuntime) {
        const native = LLMNativeRuntime.stream({
          model: input.model,
          provider: item,
          auth: info,
          llmClient,
          isOpenaiOauth,
          system,
          messages,
          tools: sortedTools,
          toolChoice: input.toolChoice,
          temperature: params.temperature,
          topP: params.topP,
          topK: params.topK,
          maxOutputTokens: params.maxOutputTokens,
          providerOptions: params.options,
          headers: requestHeaders,
          abort: input.abort,
        })
        if (native.type === "supported") {
          yield* Effect.logInfo("llm runtime selected").pipe(
            Effect.annotateLogs({
              "llm.runtime": "native",
              "llm.provider": input.model.providerID,
              "llm.model": input.model.id,
            }),
          )
          return {
            type: "native" as const,
            stream: native.stream.pipe(
              Stream.tap((event) => Effect.sync(() => tapLLMEvent(streamStartedAt, latencyBase, event))),
            ),
          }
        }
        yield* Effect.logInfo("llm runtime selected").pipe(
          Effect.annotateLogs({
            "llm.runtime": "ai-sdk",
            "llm.provider": input.model.providerID,
            "llm.model": input.model.id,
            "llm.native_unsupported_reason": native.reason,
            "llm.native_forced": !flags.experimentalNativeLlm,
          }),
        )
        l.info("native runtime unavailable; falling back to ai-sdk", { reason: native.reason })
      }

      yield* Effect.logInfo("llm runtime selected").pipe(
        Effect.annotateLogs({
          "llm.runtime": "ai-sdk",
          "llm.provider": input.model.providerID,
          "llm.model": input.model.id,
        }),
      )
      return {
        type: "ai-sdk" as const,
        startedAt: streamStartedAt,
        latencyBase,
        toolInputTimeout: attemptToolInputTimeout,
        attempt,
        maxAttempts: attemptMax,
        result: streamText({
          onError(error) {
            l.error("stream error", {
              error,
            })
          },
          async experimental_repairToolCall(failed) {
            const lower = failed.toolCall.toolName.toLowerCase()
            if (lower !== failed.toolCall.toolName && sortedTools[lower]) {
              l.info("repairing tool call", {
                tool: failed.toolCall.toolName,
                repaired: lower,
              })
              return {
                ...failed.toolCall,
                toolName: lower,
              }
            }
            return {
              ...failed.toolCall,
              input: JSON.stringify({
                tool: failed.toolCall.toolName,
                error: failed.error.message,
              }),
              toolName: "invalid",
            }
          },
          temperature: params.temperature,
          topP: params.topP,
          topK: params.topK,
          providerOptions: ProviderTransform.providerOptions(input.model, params.options),
          activeTools: Object.keys(sortedTools).filter((x) => x !== "invalid"),
          tools: sortedTools,
          toolChoice: input.toolChoice,
          maxOutputTokens: params.maxOutputTokens,
          abortSignal: input.abort,
          headers: requestHeaders,
          maxRetries: input.retries ?? 0,
          messages,
          model: wrapLanguageModel({
            model: language,
            middleware: [
              aiSdkFirstEventTimeoutMiddleware(streamStartedAt, latencyBase, {
                attempt,
                maxAttempts: attemptMax,
                timeout: attemptFirstEventTimeout,
              }),
              aiSdkLatencyMiddleware(streamStartedAt, latencyBase, {
                attempt,
                maxAttempts: attemptMax,
                firstEventTimeout: attemptFirstEventTimeout,
              }),
              {
                specificationVersion: "v3" as const,
                async transformParams(args) {
                  if (args.type === "stream") {
                    // @ts-expect-error
                    args.params.prompt = ProviderTransform.message(args.params.prompt, input.model, options)
                  }
                  return args.params
                },
              },
            ],
          }),
          experimental_telemetry: {
            isEnabled: cfg.experimental?.openTelemetry,
            functionId: "session.llm",
            tracer: telemetryTracer,
            metadata: {
              userId: cfg.username ?? "unknown",
              sessionId: input.sessionID,
            },
          },
        }),
      }
    })

    const stream: Interface["stream"] = (input) =>
      Stream.scoped(
        Stream.unwrap(
          Effect.gen(function* () {
            const ctrl = yield* Effect.acquireRelease(
              Effect.sync(() => new AbortController()),
              (ctrl) => Effect.sync(() => ctrl.abort()),
            )

            const result = yield* run({ ...input, abort: ctrl.signal })

            if (result.type === "native") return result.stream

            const state = LLMAISDK.adapterState()
            const source = watchToolInputStream(result.result.fullStream, result.startedAt, result.latencyBase, {
              attempt: result.attempt,
              maxAttempts: result.maxAttempts,
              timeout: result.toolInputTimeout,
            })
            const iterable = {
              [Symbol.asyncIterator]() {
                const iterator = source[Symbol.asyncIterator]()
                return {
                  next: () => iterator.next(),
                  return: async () => {
                    // Abort before AI SDK's return() waits for the hanging provider stream to close.
                    ctrl.abort()
                    return iterator.return ? iterator.return() : { done: true as const, value: undefined }
                  },
                }
              },
            }
            return Stream.fromAsyncIterable(iterable, (e) => (e instanceof Error ? e : new Error(String(e)))).pipe(
              Stream.tap((event) => Effect.sync(() => tapAISDKEvent(result.startedAt, result.latencyBase, event))),
              Stream.mapEffect((event) => LLMAISDK.toLLMEvents(state, event)),
              Stream.flatMap((events) => Stream.fromIterable(events)),
            )
          }),
        ),
      )

    return Service.of({ stream })
  }),
)

export const layer = live.pipe(Layer.provide(Permission.defaultLayer))

export const defaultLayer = Layer.suspend(() =>
  layer.pipe(
    Layer.provide(Auth.defaultLayer),
    Layer.provide(Config.defaultLayer),
    Layer.provide(Provider.defaultLayer),
    Layer.provide(Plugin.defaultLayer),
    Layer.provide(LLMClient.layer.pipe(Layer.provide(RequestExecutor.defaultLayer))),
    Layer.provide(RuntimeFlags.defaultLayer),
  ),
)

function resolveTools(input: Pick<StreamInput, "tools" | "agent" | "permission" | "user">) {
  const disabled = Permission.disabled(
    Object.keys(input.tools),
    Permission.merge(input.agent.permission, input.permission ?? []),
  )
  return Record.filter(input.tools, (_, k) => input.user.tools?.[k] !== false && (isLazyMcpTool(k) || !disabled.has(k)))
}

// Check if messages contain any tool-call content
// Used to determine if a dummy tool should be added (GitHub Copilot only; see stream()).
export function hasToolCalls(messages: ModelMessage[]): boolean {
  for (const msg of messages) {
    if (!Array.isArray(msg.content)) continue
    for (const part of msg.content) {
      if (part.type === "tool-call" || part.type === "tool-result") return true
    }
  }
  return false
}

function llmLatency(stage: string, startedAt: number, fields: Record<string, unknown> = {}) {
  const payload = {
    component: "llm",
    stage,
    elapsed_ms: Date.now() - startedAt,
    ...fields,
  }
  console.log(`[latency][opencode] ${JSON.stringify(payload)}`)
  GlobalBus.emit("event", {
    payload: {
      type: "opencode.llm.latency",
      ...payload,
    },
  })
}

function aiSdkFirstEventTimeoutMiddleware(
  startedAt: number,
  base: LatencyBase,
  retry: { attempt: number; maxAttempts: number; timeout: number },
): LanguageModelMiddleware {
  let ctl: AbortController | undefined
  let timer: ReturnType<typeof setTimeout> | undefined
  let streamController: ReadableStreamDefaultController<LanguageModelV3StreamPart> | undefined
  let streamReader: ReadableStreamDefaultReader<LanguageModelV3StreamPart> | undefined
  let startReject: ((error: DOMException) => void) | undefined
  let timeout: DOMException | undefined
  const clear = () => {
    if (timer) clearTimeout(timer)
    timer = undefined
  }
  const abort = () => {
    if (timeout) return
    const error = timeoutError(
      `LLM first stream event timed out after ${retry.timeout}ms (attempt ${retry.attempt}/${retry.maxAttempts})`,
    )
    timeout = error
    clear()
    llmLatency("ai_sdk_first_event_timeout", startedAt, {
      ...base,
      attempt: retry.attempt,
      max_attempts: retry.maxAttempts,
      timeout_ms: retry.timeout,
      error: error.message,
    })
    ctl?.abort(error)
    startReject?.(error)
    streamController?.error(error)
    streamReader?.cancel(error).catch(() => {})
  }

  return {
    specificationVersion: "v3",
    async transformParams(args) {
      if (args.type !== "stream" || retry.timeout <= 0) return args.params
      ctl = new AbortController()
      const signals = [args.params.abortSignal, ctl.signal].filter((item): item is AbortSignal => Boolean(item))
      return {
        ...args.params,
        abortSignal: signals.length === 1 ? signals[0] : AbortSignal.any(signals),
      }
    },
    async wrapStream(args) {
      if (retry.timeout <= 0) return args.doStream()
      timer = setTimeout(abort, retry.timeout)
      try {
        const result = await Promise.race([
          args.doStream(),
          new Promise<never>((_, reject) => {
            startReject = reject
          }),
        ])
        return {
          ...result,
          stream: new ReadableStream<LanguageModelV3StreamPart>({
            start(controller) {
              streamController = controller
              if (timeout) {
                controller.error(timeout)
                return
              }

              const reader = result.stream.getReader()
              streamReader = reader
              const pump = async () => {
                try {
                  while (true) {
                    if (timeout) return
                    const { done, value } = await reader.read()
                    if (timeout) return
                    if (done) {
                      clear()
                      controller.close()
                      return
                    }
                    clear()
                    startReject = undefined
                    controller.enqueue(value)
                  }
                } catch (error) {
                  clear()
                  if (!timeout) controller.error(error)
                } finally {
                  streamController = undefined
                  if (streamReader === reader) streamReader = undefined
                }
              }
              pump().catch((error) => {
                clear()
                if (!timeout) controller.error(error)
              })
            },
            cancel(reason) {
              clear()
              streamController = undefined
              const reader = streamReader
              streamReader = undefined
              return reader?.cancel(reason)
            },
          }),
        }
      } catch (error) {
        clear()
        throw error
      }
    },
  }
}

function aiSdkLatencyMiddleware(
  startedAt: number,
  base: LatencyBase,
  retry: { attempt: number; maxAttempts: number; firstEventTimeout: number },
): LanguageModelMiddleware {
  return {
    specificationVersion: "v3",
    async wrapStream(args) {
      const providerStartedAt = Date.now()
      llmLatency("ai_sdk_provider_do_stream_started", startedAt, {
        ...base,
        ai_provider: args.model.provider,
        ai_model: args.model.modelId,
        max_output_tokens: args.params.maxOutputTokens,
        tool_count: args.params.tools?.length ?? 0,
        has_provider_options: !!args.params.providerOptions,
        has_headers: !!args.params.headers,
        attempt: retry.attempt,
        max_attempts: retry.maxAttempts,
        first_event_timeout_ms: retry.firstEventTimeout,
      })
      try {
        const result = await args.doStream()
        llmLatency("ai_sdk_provider_do_stream_resolved", startedAt, {
          ...base,
          step_ms: Date.now() - providerStartedAt,
          ai_provider: args.model.provider,
          ai_model: args.model.modelId,
          has_response_headers: !!result.response?.headers,
          has_request_body: !!result.request?.body,
          attempt: retry.attempt,
          max_attempts: retry.maxAttempts,
        })
        return {
          ...result,
          stream: tapProviderReadableStream(result.stream, startedAt, base),
        }
      } catch (error) {
        llmLatency("ai_sdk_provider_do_stream_error", startedAt, {
          ...base,
          step_ms: Date.now() - providerStartedAt,
          error: error instanceof Error ? error.message : String(error),
          error_name: error instanceof Error ? error.name : undefined,
          error_constructor: error instanceof Error ? error.constructor.name : undefined,
          attempt: retry.attempt,
          max_attempts: retry.maxAttempts,
        })
        throw error
      }
    },
  }
}

function tapProviderReadableStream(
  stream: ReadableStream<LanguageModelV3StreamPart>,
  startedAt: number,
  base: LatencyBase,
) {
  return stream.pipeThrough(
    new TransformStream<LanguageModelV3StreamPart, LanguageModelV3StreamPart>({
      transform(chunk, controller) {
        tapFirstEvent(providerEventSeen, base, "provider_stream_event_first", startedAt, chunk.type, eventFields(chunk))
        controller.enqueue(chunk)
      },
    }),
  )
}

function tapAISDKEvent(startedAt: number, base: LatencyBase, event: AISDKEvent) {
  tapFirstEvent(aiSdkEventSeen, base, "full_stream_event_first", startedAt, event.type, eventFields(event))
}

type PendingToolInput = {
  id: string
  name: string
  lastActivity: number
}

export async function* watchToolInputStream(
  stream: AsyncIterable<AISDKEvent>,
  startedAt: number,
  base: LatencyBase,
  retry: { attempt: number; maxAttempts: number; timeout: number | false },
): AsyncIterable<AISDKEvent> {
  if (retry.timeout === false || retry.timeout <= 0) {
    yield* stream
    return
  }

  const iterator = stream[Symbol.asyncIterator]()
  const timeoutMs = retry.timeout
  const pending = new Map<string, PendingToolInput>()

  const clearPending = (id: string | undefined) => {
    if (!id) pending.clear()
    else pending.delete(id)
  }

  const updatePending = (event: AISDKEvent) => {
    const now = Date.now()
    switch (event.type) {
      case "tool-input-start":
        pending.set(event.id, { id: event.id, name: event.toolName, lastActivity: now })
        llmLatency("tool_input_started", startedAt, {
          ...base,
          attempt: retry.attempt,
          max_attempts: retry.maxAttempts,
          tool_input_timeout_ms: retry.timeout,
          tool_call_id: event.id,
          tool_name: event.toolName,
        })
        return
      case "tool-input-delta":
        {
          const current = pending.get(event.id)
          if (current) pending.set(event.id, { ...current, lastActivity: now })
        }
        return
      case "tool-input-end":
        clearPending(event.id)
        return
      case "tool-call":
      case "tool-result":
      case "tool-error":
        clearPending(event.toolCallId)
        return
      case "finish":
      case "finish-step":
      case "abort":
      case "error":
        pending.clear()
        return
      default:
        return
    }
  }

  const earliestPending = () => {
    let earliest: PendingToolInput | undefined
    for (const item of pending.values()) {
      if (!earliest || item.lastActivity < earliest.lastActivity) earliest = item
    }
    return earliest
  }

  try {
    while (true) {
      let timer: ReturnType<typeof setTimeout> | undefined
      const snapshot = earliestPending()
      const timeout = snapshot
        ? new Promise<IteratorResult<AISDKEvent>>((_, reject) => {
            const remaining = Math.max(0, snapshot.lastActivity + timeoutMs - Date.now())
            timer = setTimeout(() => {
              const current = pending.get(snapshot.id)
              if (!current || current.lastActivity !== snapshot.lastActivity) return
              const elapsed = Date.now() - current.lastActivity
              const error = timeoutError(
                `LLM tool input stalled for ${elapsed}ms after ${current.name} input started (attempt ${retry.attempt}/${retry.maxAttempts})`,
              )
              llmLatency("tool_input_stalled", startedAt, {
                ...base,
                attempt: retry.attempt,
                max_attempts: retry.maxAttempts,
                timeout_ms: timeoutMs,
                elapsed_ms_since_last_delta: elapsed,
                tool_call_id: current.id,
                tool_name: current.name,
                error: error.message,
              })
              reject(error)
            }, remaining)
          })
        : undefined

      const next = timeout
        ? await Promise.race([iterator.next(), timeout]).finally(() => {
            if (timer) clearTimeout(timer)
          })
        : await iterator.next()
      if (next.done) return
      updatePending(next.value)
      yield next.value
    }
  } finally {
    if (iterator.return) await iterator.return()
  }
}

function tapLLMEvent(startedAt: number, base: LatencyBase, event: LLMEvent) {
  tapFirstEvent(llmEventSeen, base, "native_stream_event_first", startedAt, event.type, eventFields(event))
}

function tapFirstEvent(
  seenMap: WeakMap<LatencyBase, Set<string>>,
  base: LatencyBase,
  stage: string,
  startedAt: number,
  eventType: string,
  fields: Record<string, unknown> = {},
) {
  if (!firstEventStages.has(eventType)) return
  const seen = seenMap.get(base) ?? new Set<string>()
  if (seen.has(eventType)) return
  seen.add(eventType)
  seenMap.set(base, seen)
  llmLatency(stage, startedAt, {
    ...base,
    event_type: eventType,
    ...fields,
  })
}

function eventFields(event: { type: string }) {
  const item = event as {
    delta?: unknown
    text?: unknown
    toolName?: unknown
    finishReason?: unknown
    error?: unknown
    providerMetadata?: unknown
  }
  return {
    content_length:
      typeof item.delta === "string" ? item.delta.length : typeof item.text === "string" ? item.text.length : undefined,
    tool_name: typeof item.toolName === "string" ? item.toolName : undefined,
    finish_reason: typeof item.finishReason === "string" ? item.finishReason : undefined,
    has_error: item.error !== undefined,
    has_provider_metadata: !!item.providerMetadata,
  }
}

export * as LLM from "./llm"
