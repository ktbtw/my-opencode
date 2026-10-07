import { afterEach, describe, expect, test } from "bun:test"
import { mkdtemp, rm, writeFile } from "node:fs/promises"
import os from "node:os"
import path from "node:path"
import { Server } from "../../src/server/server"
import { Relay } from "../../src/server/relay"
import { withTimeout } from "../../src/util/timeout"
import { resetDatabase } from "../fixture/db"
import { disposeAllInstances, tmpdir } from "../fixture/fixture"

type BackendReady = {
  url: string
  token: string
  operator_key: string
}

type BackendProcess = {
  ready: BackendReady
  output: () => string
  drain: Promise<void>
  stopFile: string
  stop: () => Promise<void>
}

type Agent = {
  agent_id: string
  machine_id: string
  projects?: Array<{
    project_id: string
    root: string
    project_scope_id?: string
    binding_epoch?: number
  }>
}

type ProjectMemory = {
  memory_id: string
  statement: string
  source_refs?: Array<{ task_id?: string; session_id?: string; message_ids?: string[] }>
}

type ProjectScope = {
  project_scope_id: string
  machine_id: string
  display_name: string
}

type Task = {
  task_id: string
  status: string
  result?: string
  session_id?: string
}

type TaskEvent = {
  type: string
  content?: string
}

const relayEnvKeys = [
  "OPENCODE_RELAY_URL",
  "OPENCODE_RELAY_AGENT_ID",
  "OPENCODE_RELAY_MACHINE_ID",
  "OPENCODE_RELAY_OPERATOR_KEY",
  "OPENCODE_RELAY_PROJECT_ID",
  "OPENCODE_RELAY_PROJECT_ROOT",
  "OPENCODE_RELAY_HOSTNAME",
] as const

const originalEnv = Object.fromEntries(relayEnvKeys.map((key) => [key, process.env[key]]))

afterEach(async () => {
  for (const key of relayEnvKeys) {
    const value = originalEnv[key]
    if (value === undefined) delete process.env[key]
    else process.env[key] = value
  }
  await disposeAllInstances()
  await resetDatabase()
})

async function startBackend() {
  const dir = await mkdtemp(path.join(os.tmpdir(), "chat-codex-relay-e2e-"))
  const stopFile = path.join(dir, "stop")
  const output: string[] = []
  const proc = Bun.spawn(["go", "test", "./internal/app", "-run", "TestRelayE2EServer", "-count=1", "-v"], {
    cwd: path.resolve(import.meta.dir, "../../../../../backend"),
    env: { ...process.env, CHAT_CODEX_RELAY_E2E_SERVER: "1", CHAT_CODEX_RELAY_E2E_STOP_FILE: stopFile },
    stdin: "pipe",
    stdout: "pipe",
    stderr: "pipe",
  })
  const stderr = proc.stderr.pipeTo(
    new WritableStream({
      write(chunk) {
        output.push(new TextDecoder().decode(chunk))
      },
    }),
  )
  let stdout: Promise<void> = Promise.resolve()
  const stop = async () => {
    await writeFile(stopFile, "stop").catch(() => undefined)
    await withTimeout(proc.exited, 5_000, "timed out waiting for backend E2E server to exit").catch(() => {
      proc.kill()
    })
    await stdout.catch(() => undefined)
    await stderr.catch(() => undefined)
    await rm(dir, { recursive: true, force: true }).catch(() => undefined)
  }

  try {
    const ready = await waitForReady(proc.stdout, output)
    stdout = drainReader(ready.reader, output)
    return { ready: ready.value, output: () => output.join(""), drain: stdout, stopFile, stop } satisfies BackendProcess
  } catch (error) {
    proc.kill()
    throw new Error(`${error instanceof Error ? error.message : String(error)}\nbackend output:\n${output.join("")}`)
  }
}

async function drainReader(reader: ReadableStreamDefaultReader<string>, output: string[]) {
  try {
    for (;;) {
      const result = await reader.read()
      if (result.done) return
      output.push(result.value)
    }
  } catch {
    return
  }
}

async function waitForReady(stdout: ReadableStream<Uint8Array>, output: string[]) {
  const reader = stdout.pipeThrough(new TextDecoderStream() as TransformStream<Uint8Array, string>).getReader()
  let buffer = ""
  const started = Date.now()
  const timeout = 30_000
  while (Date.now() - started <= timeout) {
    const result = await Promise.race([
      reader.read(),
      Bun.sleep(timeout - (Date.now() - started)).then(() => ({ done: true, value: undefined })),
    ])
    if (result.done) break
    if (result.value === undefined) continue
    buffer += result.value
    output.push(result.value)
    const lines = buffer.split(/\r?\n/)
    buffer = lines.pop() ?? ""
    for (const line of lines) {
      const trimmed = line.trim()
      if (!trimmed.startsWith("{")) continue
      const parsed = JSON.parse(trimmed) as BackendReady
      if (parsed.url && parsed.token && parsed.operator_key) return { value: parsed, reader }
    }
  }
  throw new Error(`backend E2E server did not print ready JSON; stdout:\n${buffer}\nall output:\n${output.join("")}`)
}

async function writeConfig(dir: string, baseURL: string) {
  await Bun.write(
    path.join(dir, "opencode.json"),
    JSON.stringify({
      $schema: "https://opencode.ai/config.json",
      enabled_providers: ["test"],
      provider: {
        test: {
          name: "Test",
          id: "test",
          env: [],
          npm: "@ai-sdk/openai-compatible",
          models: {
            "test-model": {
              id: "test-model",
              name: "Test Model",
              attachment: false,
              reasoning: false,
              temperature: false,
              tool_call: false,
              release_date: "2025-01-01",
              limit: { context: 100000, output: 10000 },
              cost: { input: 0, output: 0 },
              options: {},
            },
          },
          options: {
            apiKey: "test-key",
            baseURL,
          },
        },
      },
      agent: {
        build: {
          model: "test/test-model",
        },
      },
    }),
  )
}

function llmServer() {
  const inputs: Record<string, unknown>[] = []
  const allStrings = (value: unknown): string[] => {
    if (typeof value === "string") return [value]
    if (Array.isArray(value)) return value.flatMap(allStrings)
    if (value && typeof value === "object") return Object.values(value).flatMap(allStrings)
    return []
  }
  const server = Bun.serve({
    port: 0,
    async fetch(req) {
      const url = new URL(req.url)
      if (url.pathname !== "/v1/chat/completions") return new Response("not found", { status: 404 })
      const body = (await req.json()) as Record<string, unknown>
      inputs.push(body)
      const promptText = allStrings(body).join("\n")
      let content = "backend relay e2e completed"
      if (promptText.includes("Project Memory Curator")) {
        const scopeID = promptText.match(/project_scope_id:\s*([A-Za-z0-9_-]+)/)?.[1] ?? ""
        const jobID = promptText.match(/job_id:\s*([A-Za-z0-9_-]+)/)?.[1] ?? ""
        const cursor = promptText.match(/source_cursor:\s*([A-Za-z0-9_-]+)/)?.[1] ?? ""
        const sourceTaskID = promptText.match(/"task_id":"([^"]+)"/)?.[1] ?? cursor
        content = JSON.stringify({
          schema_version: 1,
          project_scope_id: scopeID,
          job_id: jobID,
          source_cursor: cursor,
          candidates: [
            {
              subject_key: "relay:e2e:accepted",
              kind: "procedure",
              statement: "RELAY_ACCEPTED_MEMORY is available to the next relevant task",
              confidence: 0.95,
              scope_paths: [],
              artifact_refs: [],
              verification: { status: "unverified" },
              source_refs: [{ task_id: sourceTaskID }],
              sensitive: false,
            },
          ],
        })
      } else if (promptText.includes("Generate a title for this conversation")) {
        content = "Relay Backend E2E"
      }
      return new Response(
        new ReadableStream({
          start(controller) {
            const encoder = new TextEncoder()
            for (const chunk of [
              { choices: [{ delta: { role: "assistant" } }] },
              { choices: [{ delta: { content } }] },
              {
                choices: [{ finish_reason: "stop" }],
                usage: { prompt_tokens: 1, completion_tokens: 2, total_tokens: 3 },
              },
            ]) {
              controller.enqueue(encoder.encode(`data: ${JSON.stringify(chunk)}\n\n`))
            }
            controller.enqueue(encoder.encode("data: [DONE]\n\n"))
            controller.close()
          },
        }),
        { headers: { "content-type": "text/event-stream" } },
      )
    },
  })
  return {
    inputs,
    url: `${server.url.origin}/v1`,
    stop: () => server.stop(true),
  }
}

function authHeaders(backend: BackendReady) {
  return { authorization: `Bearer ${backend.token}` }
}

async function probeBackendWebSocket(backend: BackendReady) {
  const url = new URL("/ws/device", backend.url)
  url.protocol = "ws:"
  const ws = new WebSocket(url)
  await withTimeout(
    new Promise<void>((resolve, reject) => {
      ws.addEventListener("open", () => resolve(), { once: true })
      ws.addEventListener("error", () => reject(new Error("backend websocket probe failed")), { once: true })
    }),
    5_000,
    "timed out probing backend websocket",
  )
  ws.close()
}

async function prepareRelayEnv(tmp: Awaited<ReturnType<typeof tmpdir>>, llm: ReturnType<typeof llmServer>) {
  const backend = await startBackend()
  process.env.OPENCODE_RELAY_URL = new URL("/ws/device", backend.ready.url).toString()
  process.env.OPENCODE_RELAY_OPERATOR_KEY = backend.ready.operator_key
  process.env.OPENCODE_RELAY_AGENT_ID = "agent-cross-e2e"
  process.env.OPENCODE_RELAY_MACHINE_ID = "machine-cross-e2e"
  process.env.OPENCODE_RELAY_PROJECT_ID = "project-cross-e2e"
  process.env.OPENCODE_RELAY_PROJECT_ROOT = tmp.path
  process.env.OPENCODE_RELAY_HOSTNAME = "host-cross-e2e"
  await writeConfig(tmp.path, llm.url)
  expect(Relay.config()?.url).toBe(new URL("/ws/device", backend.ready.url).toString().replace(/^http:/, "ws:"))
  await probeBackendWebSocket(backend.ready)
  return backend
}

async function getJSON<T>(backend: BackendReady, pathname: string) {
  const response = await fetch(new URL(pathname, backend.url), {
    headers: authHeaders(backend),
  })
  expect(response.status).toBe(200)
  return (await response.json()) as T
}

async function getText(backend: BackendReady, pathname: string) {
  const response = await fetch(new URL(pathname, backend.url), {
    headers: authHeaders(backend),
  })
  return await response.text()
}

async function postJSON<T>(backend: BackendReady, pathname: string, body: unknown, status: number) {
  const response = await fetch(new URL(pathname, backend.url), {
    method: "POST",
    headers: { ...authHeaders(backend), "content-type": "application/json" },
    body: JSON.stringify(body),
  })
  expect(response.status).toBe(status)
  return (await response.json()) as T
}

async function waitFor<T>(fn: () => Promise<T | undefined>, message: string, timeout = 15_000) {
  const started = Date.now()
  while (Date.now() - started <= timeout) {
    const value = await fn()
    if (value !== undefined) return value
    await Bun.sleep(50)
  }
  throw new Error(message)
}

async function events(backend: BackendReady, taskID: string) {
  const response = await fetch(new URL(`/api/tasks/${taskID}/events`, backend.url), {
    headers: authHeaders(backend),
  })
  expect(response.status).toBe(200)
  const text = await response.text()
  return text
    .split("\n")
    .filter((line) => line.startsWith("data: "))
    .map((line) => JSON.parse(line.slice("data: ".length)) as TaskEvent)
}

describe("Relay backend E2E", () => {
  test("opencode2 Server.listen starts relay against the real backend router", async () => {
    await using tmp = await tmpdir({ git: true })
    const llm = llmServer()
    const backend = await prepareRelayEnv(tmp, llm)
    const listener = await Server.listen({ hostname: "127.0.0.1", port: 0 })
    try {
      const agent = await waitFor(
        async () => {
          const agents = await getJSON<Agent[] | null>(backend.ready, "/api/agents")
          return (agents ?? []).find((item) => item.agent_id === "agent-cross-e2e")
        },
        `backend did not observe Server.listen relay agent online; output:\n${backend.output()}`,
        20_000,
      )
      expect(agent.machine_id).toBe("machine-cross-e2e")
      expect(agent.projects?.[0]).toMatchObject({ project_id: "project-cross-e2e", root: tmp.path })
    } finally {
      await listener.stop(true)
      llm.stop()
      await backend.stop()
    }
  }, 60_000)

  test("opencode2 relay connects to real backend router, executes a task, and records completed events", async () => {
    await using tmp = await tmpdir({ git: true })
    const llm = llmServer()
    const backend = await prepareRelayEnv(tmp, llm)

    let stage = "start-relay"
    const relay = Relay.start()
    if (!relay) throw new Error("relay did not start")
    try {
      stage = "wait-agent"
      const agent = await waitFor(async () => {
        const agents = await getJSON<Agent[] | null>(backend.ready, "/api/agents")
        return (agents ?? []).find((item) => item.agent_id === "agent-cross-e2e")
      }, `backend did not observe opencode2 relay agent online; output:\n${backend.output()}`)
      expect(agent.machine_id).toBe("machine-cross-e2e")
      expect(agent.projects?.[0]).toMatchObject({ project_id: "project-cross-e2e", root: tmp.path })
      const scopes = await getJSON<ProjectScope[]>(backend.ready, "/api/devices/machine-cross-e2e/projects")
      const scopeID = scopes[0]?.project_scope_id
      expect(scopeID).toBeString()

      stage = "create-task"
      const created = await postJSON<Task>(
        backend.ready,
        "/api/tasks",
        {
          agent_id: "agent-cross-e2e",
          project_id: "project-cross-e2e",
          parts: [{ type: "text", text: "run backend relay cross e2e" }],
          metadata: { source: "opencode2-cross-e2e" },
        },
        202,
      )

      stage = "wait-completed"
      const completed = await waitFor(
        async () => {
          const task = await getJSON<Task>(backend.ready, `/api/tasks/${created.task_id}`)
          return task.status === "completed" ? task : undefined
        },
        `backend task did not complete through opencode2 relay; task=${await getText(
          backend.ready,
          `/api/tasks/${created.task_id}`,
        )}; output:\n${backend.output()}`,
      )
      expect(completed.result).toBe("backend relay e2e completed")
      expect(completed.session_id).toBeString()
      expect(JSON.stringify(llm.inputs)).toContain("run backend relay cross e2e")

      stage = "events"
      const recorded = await events(backend.ready, created.task_id)
      expect(recorded.map((event) => event.type)).toContain("started")
      expect(recorded.map((event) => event.type)).toContain("delta")
      expect(recorded.map((event) => event.type)).toContain("completed")
      expect(recorded.some((event) => event.content?.includes("backend relay e2e completed"))).toBe(true)

      stage = "wait-project-memory"
      const acceptedMemory = await waitFor(
        async () => {
          const memories = await getJSON<ProjectMemory[]>(
            backend.ready,
            `/api/devices/machine-cross-e2e/projects/${scopeID}/memories?query=RELAY_ACCEPTED_MEMORY`,
          )
          return memories.find((item) => item.statement.includes("RELAY_ACCEPTED_MEMORY"))
        },
        `backend did not accept relay curator memory; output:\n${backend.output()}`,
        30_000,
      )
      expect(acceptedMemory.source_refs?.some((source) => source.task_id === created.task_id)).toBe(true)

      stage = "create-next-task"
      const inputCount = llm.inputs.length
      const next = await postJSON<Task>(
        backend.ready,
        "/api/tasks",
        {
          agent_id: "agent-cross-e2e",
          project_id: "project-cross-e2e",
          parts: [{ type: "text", text: "use RELAY_ACCEPTED_MEMORY in the next task" }],
        },
        202,
      )
      stage = "wait-next-completed"
      await waitFor(
        async () => {
          const task = await getJSON<Task>(backend.ready, `/api/tasks/${next.task_id}`)
          return task.status === "completed" ? task : undefined
        },
        `next task did not complete; output:\n${backend.output()}`,
        30_000,
      )
      const nextInputs = llm.inputs.slice(inputCount)
      expect(
        nextInputs.some((input) => {
          const encoded = JSON.stringify(input)
          return encoded.includes("<project_memory>") && encoded.includes("RELAY_ACCEPTED_MEMORY is available")
        }),
      ).toBe(true)
      stage = "events"
    } finally {
      if (stage !== "events") {
        console.error(`relay backend E2E cleanup after stage: ${stage}\nbackend output:\n${backend.output()}`)
      }
      await relay.stop()
      llm.stop()
      await backend.stop()
    }
  }, 90_000)

  test("backend goal optimize API calls the relay model path", async () => {
    await using tmp = await tmpdir({ git: true })
    const llm = llmServer()
    const backend = await prepareRelayEnv(tmp, llm)

    const relay = Relay.start()
    if (!relay) throw new Error("relay did not start")
    try {
      await waitFor(async () => {
        const agents = await getJSON<Agent[] | null>(backend.ready, "/api/agents")
        return (agents ?? []).find((item) => item.agent_id === "agent-cross-e2e")
      }, `backend did not observe opencode2 relay agent online; output:\n${backend.output()}`)

      const optimized = await postJSON<{ goal: string; original_goal: string; model: string; max_iterations: number }>(
        backend.ready,
        "/api/goal/optimize",
        {
          agent_id: "agent-cross-e2e",
          project_id: "project-cross-e2e",
          goal: "把 Goal 优化功能做完",
          model: "test/test-model",
          max_iterations: 7,
        },
        200,
      )
      expect(optimized.goal).toBe("backend relay e2e completed")
      expect(optimized.original_goal).toBe("把 Goal 优化功能做完")
      expect(optimized.model).toBe("test/test-model")
      expect(optimized.max_iterations).toBe(7)
      expect(JSON.stringify(llm.inputs)).toContain("最大轮次：7")
      expect(JSON.stringify(llm.inputs)).toContain("把 Goal 优化功能做完")
    } finally {
      await relay.stop()
      llm.stop()
      await backend.stop()
    }
  }, 90_000)
})
