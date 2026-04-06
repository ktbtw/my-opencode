import os from "node:os"
import path from "node:path"
import { bootstrap } from "@/cli/bootstrap"
import { GlobalBus } from "@/bus/global"
import { Flag } from "@/flag/flag"
import { Session } from "@/session"
import { SessionPrompt } from "@/session/prompt"
import { SessionID } from "@/session/schema"
import { Permission } from "@/permission"
import { PermissionID } from "@/permission/schema"
import { InstanceBootstrap } from "@/project/bootstrap"
import { Instance } from "@/project/instance"
import { ModelID, ProviderID } from "@/provider/schema"
import { Log } from "@/util/log"

type Task = {
  id: string
  session?: string
  cancelled?: boolean
  permissionMode?: PermissionMode
}

type Env = {
  type: string
  request_id?: string
  payload?: any
}

type PermissionMode = "deny" | "ask" | "auto-approve"

type Run = {
  task_id: string
  agent_id?: string
  machine_id?: string
  project_id: string
  session_id?: string
  parts?: Array<{
    type: string
    text?: string
    mime?: string
    filename?: string
    url?: string
    source?: Record<string, unknown>
    name?: string
    prompt?: string
    description?: string
    agent?: string
    model?: {
      providerID: string
      modelID: string
    }
    command?: string
  }>
  metadata?: Record<string, string>
}

type ApprovalResponse = {
  task_id: string
  permission_id: string
  reply: "once" | "always" | "reject"
  message?: string
}

type Cfg = {
  url: string
  agent: string
  machine: string
  host: string
  operatorKey: string
  project: {
    id: string
    root: string
  }
}

type State = {
  stop: boolean
  beat?: ReturnType<typeof setInterval>
  task?: Task
  ws?: WebSocket
  wait?: Promise<void>
}

export namespace Relay {
  const log = Log.create({ service: "relay" })
  type InputPart = Parameters<typeof SessionPrompt.prompt>[0]["parts"][number]

  function permissionMode(task?: Task): PermissionMode {
    // task 级别优先（来自 metadata.permission_mode）
    if (task?.permissionMode) return task.permissionMode
    const value = Flag.OPENCODE_RELAY_PERMISSION_MODE?.trim().toLowerCase()
    if (value === "ask" || value === "auto-approve" || value === "deny") return value
    return "ask" // 默认 ask，让用户能看到审批弹窗
  }

  function cfg(): Cfg | undefined {
    if (!Flag.OPENCODE_RELAY_URL) return
    if (!Flag.OPENCODE_RELAY_OPERATOR_KEY?.trim()) {
      log.warn("relay disabled: missing OPENCODE_RELAY_OPERATOR_KEY")
      return
    }
    const root = path.resolve(Flag.OPENCODE_RELAY_PROJECT_ROOT ?? process.cwd())
    const host = Flag.OPENCODE_RELAY_HOSTNAME ?? os.hostname()
    return {
      url: addr(Flag.OPENCODE_RELAY_URL),
      agent: Flag.OPENCODE_RELAY_AGENT_ID ?? `${host}:${path.basename(root)}`,
      machine: Flag.OPENCODE_RELAY_MACHINE_ID ?? host,
      host,
      operatorKey: Flag.OPENCODE_RELAY_OPERATOR_KEY.trim(),
      project: {
        id: Flag.OPENCODE_RELAY_PROJECT_ID ?? path.basename(root),
        root,
      },
    }
  }

  function addr(input: string) {
    const url = new URL(input)
    if (url.protocol === "http:") url.protocol = "ws:"
    if (url.protocol === "https:") url.protocol = "wss:"
    if (url.pathname === "/" || url.pathname === "") {
      url.pathname = "/ws/device"
    }
    if (Flag.OPENCODE_RELAY_TOKEN) {
      url.searchParams.set("token", Flag.OPENCODE_RELAY_TOKEN)
    }
    return url.toString()
  }

  function id(type: string) {
    return `relay_${type}_${crypto.randomUUID()}`
  }

  function send(ws: WebSocket, type: string, payload: Record<string, unknown>) {
    if (ws.readyState !== WebSocket.OPEN) return
    log.info("send relay message", {
      type,
      taskID: payload["task_id"],
      sessionID: payload["session_id"],
    })
    ws.send(
      JSON.stringify({
        type,
        request_id: id(type),
        sent_at: new Date().toISOString(),
        payload,
      }),
    )
  }

  async function pushModels(cfg: Cfg, ws: WebSocket) {
    try {
      const resp = await fetch("http://127.0.0.1:4096/provider", {
        headers: { "x-opencode-directory": cfg.project.root },
      })
      if (!resp.ok) return
      const data = await resp.json()
      send(ws, "device.models", { agent_id: cfg.agent, models: data })
    } catch (err) {
      log.error("failed to push models", { error: err })
    }
  }

  function text(data: unknown) {
    if (typeof data === "string") return data
    if (data instanceof ArrayBuffer) return Buffer.from(data).toString("utf8")
    if (ArrayBuffer.isView(data)) return Buffer.from(data.buffer, data.byteOffset, data.byteLength).toString("utf8")
    return String(data ?? "")
  }

  function parse(data: unknown): Env | undefined {
    try {
      return JSON.parse(text(data))
    } catch (err) {
      log.error("invalid relay message", { error: err })
      return
    }
  }

  function parts(run: Run): InputPart[] {
    const out: InputPart[] = []
    for (const part of run.parts ?? []) {
      if (part.type === "text") {
        if (!part.text?.trim()) continue
        out.push({ type: "text", text: part.text })
        continue
      }
      if (part.type === "file") {
        if (!part.url?.trim()) continue
        out.push({
          type: "file",
          mime: part.mime || "application/octet-stream",
          filename: part.filename,
          url: part.url,
          source: part.source as InputPart extends { type: "file"; source?: infer T } ? T : never,
        })
        continue
      }
      if (part.type === "agent") {
        if (!part.name?.trim()) continue
        out.push({
          type: "agent",
          name: part.name,
          source: part.source as InputPart extends { type: "agent"; source?: infer T } ? T : never,
        })
        continue
      }
      if (part.type === "subtask") {
        if (!part.prompt?.trim() || !part.description?.trim() || !part.agent?.trim()) continue
        out.push({
          type: "subtask",
          prompt: part.prompt,
          description: part.description,
          agent: part.agent,
          model: part.model
            ? {
                providerID: ProviderID.make(part.model.providerID),
                modelID: ModelID.make(part.model.modelID),
              }
            : undefined,
          command: part.command,
        })
      }
    }
    return out
  }

  async function session(run: Run) {
    if (!run.session_id) return Session.create({})
    return Session.get(SessionID.make(run.session_id))
  }

  function sub(root: string, task: Task, ws: WebSocket) {
    const on = (msg: { directory?: string; payload: any }) => {
      if (msg.directory !== root) return
      const evt = msg.payload
      if (evt.type === "message.part.delta") {
        if (evt.properties.sessionID !== task.session) return
        const field: string = evt.properties.field
        if (field !== "text") return
        if (!evt.properties.delta) return
        // partType 由 processor.ts 传入：reasoning part 的 delta 标记为 "reasoning"，text part 为 undefined
        const outField = evt.properties.partType === "reasoning" ? "reasoning" : "text"
        send(ws, "task.delta", {
          task_id: task.id,
          content: evt.properties.delta,
          field: outField,
        })
        return
      }
      if (evt.type === "permission.asked") {
        if (evt.properties.sessionID !== task.session) return
        const mode = permissionMode(task)
        const permissionID = String(evt.properties.id)
        if (mode === "auto-approve") {
          void Permission.reply({
            requestID: PermissionID.make(permissionID),
            reply: "once",
          })
            .then(() => {
              send(ws, "task.approval_auto_approved", {
                task_id: task.id,
                session_id: task.session,
                permission_id: permissionID,
                permission: evt.properties.permission,
                patterns: evt.properties.patterns,
              })
            })
            .catch((error) => {
              log.error("failed to auto approve permission", { error, permissionID, taskID: task.id })
              task.cancelled = true
              if (task.session) {
                SessionPrompt.cancel(SessionID.make(task.session))
              }
              send(ws, "task.failed", {
                task_id: task.id,
                session_id: task.session,
                error: `permission auto approve failed: ${evt.properties.permission}`,
              })
            })
          return
        }
        if (mode === "ask") {
          send(ws, "task.waiting_approval", {
            task_id: task.id,
            session_id: task.session,
            permission_id: permissionID,
            permission: evt.properties.permission,
            patterns: evt.properties.patterns,
            metadata: evt.properties.metadata,
          })
          return
        }
        task.cancelled = true
        if (task.session) {
          SessionPrompt.cancel(SessionID.make(task.session))
        }
        send(ws, "task.failed", {
          task_id: task.id,
          session_id: task.session,
          error: `permission required: ${evt.properties.permission}`,
        })
      }
    }
    GlobalBus.on("event", on)
    return () => GlobalBus.off("event", on)
  }

  async function applyApproval(state: State, cfg: Cfg, ws: WebSocket, env: Env) {
    const msg = env.payload as ApprovalResponse | undefined
    if (!msg?.task_id || !msg.permission_id || !msg.reply) return
    try {
      await Instance.provide({
        directory: cfg.project.root,
        init: InstanceBootstrap,
        fn: async () =>
          Permission.reply({
            requestID: PermissionID.make(msg.permission_id),
            reply: msg.reply,
            message: msg.message,
          }),
      })
      if (msg.reply !== "reject") {
        send(ws, "task.approval_applied", {
          task_id: msg.task_id,
          session_id: state.task?.session,
          permission_id: msg.permission_id,
          reply: msg.reply,
        })
      }
    } catch (error) {
      log.error("failed to apply approval reply", {
        error,
        taskID: msg.task_id,
        permissionID: msg.permission_id,
      })
      send(ws, "task.failed", {
        task_id: msg.task_id,
        session_id: state.task?.session,
        error: `apply approval failed: ${msg.permission_id}`,
      })
    }
  }

  async function exec(state: State, cfg: Cfg, ws: WebSocket, env: Env) {
    const job = env.payload as Run | undefined
    if (!job?.task_id) return
    log.info("received task.run", {
      taskID: job.task_id,
      projectID: job.project_id,
      sessionID: job.session_id,
      parts: job.parts?.map((part) => part.type),
    })
    if (job.project_id !== cfg.project.id) {
      send(ws, "task.failed", {
        task_id: job.task_id,
        error: `unknown project: ${job.project_id}`,
      })
      return
    }
    if (job.agent_id && job.agent_id !== cfg.agent) {
      send(ws, "task.failed", {
        task_id: job.task_id,
        error: `unknown agent: ${job.agent_id}`,
      })
      return
    }
    if (state.task) {
      send(ws, "task.failed", {
        task_id: job.task_id,
        error: `agent busy: ${state.task.id}`,
      })
      return
    }
    const list = parts(job)
    if (list.length === 0) {
      send(ws, "task.failed", {
        task_id: job.task_id,
        error: "missing parts",
      })
      return
    }

    const metaPermMode = job.metadata?.permission_mode?.trim().toLowerCase()
    const task: Task = {
      id: job.task_id,
      permissionMode:
        metaPermMode === "ask" || metaPermMode === "auto-approve" || metaPermMode === "deny"
          ? (metaPermMode as PermissionMode)
          : undefined,
    }
    state.task = task

    await bootstrap(cfg.project.root, async () => {
      let off = () => {}
      let sid = ""
      try {
        log.info("relay bootstrap ready", {
          taskID: task.id,
          root: cfg.project.root,
          mode: permissionMode(task),
        })
        const sess = await session(job)
        task.session = sess.id
        sid = sess.id
        log.info("relay session prepared", {
          taskID: task.id,
          sessionID: sess.id,
        })
        off = sub(cfg.project.root, task, ws)
        send(ws, "task.started", {
          task_id: task.id,
          session_id: sess.id,
        })
        // 从 metadata.model 解析任务级别模型（格式："providerID/modelID"）
        let taskModel: { providerID: ProviderID; modelID: ModelID } | undefined
        const metaModel = job.metadata?.model
        if (metaModel) {
          const slashIdx = metaModel.indexOf("/")
          if (slashIdx > 0) {
            taskModel = {
              providerID: ProviderID.make(metaModel.substring(0, slashIdx)),
              modelID: ModelID.make(metaModel.substring(slashIdx + 1)),
            }
          }
        }
        const msg = await SessionPrompt.prompt({
          sessionID: sess.id,
          parts: list,
          model: taskModel,
        })
        log.info("relay prompt completed", {
          taskID: task.id,
          sessionID: sess.id,
          resultParts: msg.parts.map((part) => part.type),
        })
        if (task.cancelled) return
        send(ws, "task.completed", {
          task_id: task.id,
          session_id: sess.id,
          result: msg.parts.findLast((part) => part.type === "text")?.text ?? "",
        })
      } catch (err) {
        log.error("relay exec failed", {
          taskID: task.id,
          sessionID: sid || task.session,
          error: err,
        })
        if (task.cancelled) return
        send(ws, "task.failed", {
          task_id: task.id,
          session_id: sid || task.session,
          error: err instanceof Error ? err.message : String(err),
        })
      } finally {
        off()
      }
    }).finally(() => {
      if (state.task?.id === task.id) {
        state.task = undefined
      }
    })
  }

  function beat(state: State, cfg: Cfg, ws: WebSocket, sec: number) {
    if (state.beat) clearInterval(state.beat)
    state.beat = setInterval(() => {
      send(ws, "device.heartbeat", {
        agent_id: cfg.agent,
        running_task_id: state.task?.id,
      })
    }, sec * 1000)
  }

  async function open(state: State, cfg: Cfg) {
    log.info("connecting relay websocket", {
      url: cfg.url,
      agentID: cfg.agent,
      machineID: cfg.machine,
      projectID: cfg.project.id,
      root: cfg.project.root,
      permissionMode: permissionMode(state.task),
    })
    const ws = await new Promise<WebSocket>((resolve, reject) => {
      const ws = new WebSocket(cfg.url)
      ws.onopen = () => {
        resolve(ws)
      }
      ws.onerror = (event) => {
        reject(new Error("relay websocket connect failed"))
      }
      ws.onclose = (event) => {
        reject(new Error("relay websocket closed"))
      }
    })
    state.ws = ws
    beat(state, cfg, ws, 15)
    send(ws, "device.hello", {
      operator_key: cfg.operatorKey,
      agent_id: cfg.agent,
      machine_id: cfg.machine,
      hostname: cfg.host,
      version: "relay-0.1.0",
      projects: [
        {
          project_id: cfg.project.id,
          root: cfg.project.root,
        },
      ],
    })

    ws.onmessage = (evt) => {
      const msg = parse(evt.data)
      if (!msg) return
      log.info("received relay envelope", {
        type: msg.type,
        requestID: msg.request_id,
      })
      if (msg.type === "device.welcome") {
        const sec = Number(msg.payload?.heartbeat_interval_sec)
        if (Number.isFinite(sec) && sec > 0) {
          beat(state, cfg, ws, sec)
        }
        // 连接成功后，把模型列表发给后端缓存
        void pushModels(cfg, ws)
        return
      }
      if (msg.type === "task.cancel") {
        if (!state.task) return
        if (msg.payload?.task_id !== state.task.id) return
        state.task.cancelled = true
        if (state.task.session) {
          SessionPrompt.cancel(SessionID.make(state.task.session))
        }
        // 强制清除占用，避免 AI 挂起时 agent 一直显示 busy
        state.task = undefined
        return
      }
      if (msg.type === "task.run") {
        void exec(state, cfg, ws, msg)
        return
      }
      if (msg.type === "task.approval_response") {
        void applyApproval(state, cfg, ws, msg)
      }
    }

    state.wait = new Promise<void>((resolve) => {
      ws.onclose = () => resolve()
      ws.onerror = () => resolve()
    })
    await state.wait
  }

  export function start() {
    const value = cfg()
    if (!value) return
    const state: State = { stop: false }

    void (async () => {
      while (!state.stop) {
        await open(state, value).catch((err) => {
          if (!state.stop) {
            log.error("relay disconnected", { error: err, url: value.url })
          }
        })
        if (state.stop) return
        await Bun.sleep(3000)
      }
    })()

    return {
      async stop() {
        state.stop = true
        if (state.beat) clearInterval(state.beat)
        state.ws?.close()
        await state.wait
      },
    }
  }
}
