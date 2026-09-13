import { afterEach, describe, expect, test } from "bun:test"
import { mkdir, writeFile } from "node:fs/promises"
import path from "node:path"
import type { ServerWebSocket } from "bun"
import { Relay } from "../../src/server/relay"
import { Server } from "../../src/server/server"
import { disposeAllInstances, tmpdir } from "../fixture/fixture"
import { withTimeout } from "../../src/util/timeout"
import { resetDatabase } from "../fixture/db"
import { GlobalBus } from "../../src/bus/global"

type RelayMessage = {
  type: string
  request_id: string
  sent_at: string
  payload: Record<string, any>
}

const relayEnvKeys = [
  "OPENCODE_RELAY_URL",
  "OPENCODE_RELAY_AGENT_ID",
  "OPENCODE_RELAY_MACHINE_ID",
  "OPENCODE_RELAY_OPERATOR_KEY",
  "OPENCODE_RELAY_TOKEN",
  "OPENCODE_RELAY_PROJECT_ID",
  "OPENCODE_RELAY_PROJECT_ROOT",
  "OPENCODE_RELAY_PROJECT_SCOPE_ID",
  "OPENCODE_RELAY_PROJECT_INSTANCE_NONCE",
  "OPENCODE_RELAY_PROJECT_LINEAGE_SCOPE_ID",
  "OPENCODE_RELAY_PROJECT_VOLUME_ID",
  "OPENCODE_RELAY_PROJECT_FILE_ID",
  "OPENCODE_RELAY_PROJECT_DISPLAY_NAME",
  "OPENCODE_RELAY_HOSTNAME",
  "OPENCODE_RELAY_PERMISSION_MODE",
  "OPENCODE_PROJECT_MEMORY_ENABLED",
] as const

const originalEnv = Object.fromEntries(relayEnvKeys.map((key) => [key, process.env[key]]))

afterEach(async () => {
  for (const key of relayEnvKeys) {
    const value = originalEnv[key]
    if (value === undefined) delete process.env[key]
    else process.env[key] = value
  }
  await resetDatabase()
})

function parse(input: string) {
  return JSON.parse(input) as RelayMessage
}

test("project memory candidate parser accepts one JSON object and rejects surrounding narration", () => {
  expect(Relay.parseProjectMemoryBatch('{"schema_version":1,"candidates":[]}')).toEqual({
    schema_version: 1,
    candidates: [],
  })
  expect(Relay.parseProjectMemoryBatch('```json\n{"schema_version":1,"candidates":[]}\n```')).toEqual({
    schema_version: 1,
    candidates: [],
  })
  expect(() => Relay.parseProjectMemoryBatch('result: {"schema_version":1,"candidates":[]}')).toThrow()
  expect(() => Relay.parseProjectMemoryBatch("[]")).toThrow("curator returned invalid JSON")
})

test("project memory candidate parser rejects string artifact and source references", () => {
  const candidate = {
    subject_key: "project:memory",
    kind: "procedure",
    statement: "Keep durable project context",
    confidence: 0.9,
    scope_paths: [],
    artifact_refs: [],
    verification: { status: "unverified", evidence_refs: [] },
    source_refs: [{ task_id: "task-1", message_ids: ["message-1"] }],
    sensitive: false,
  }
  expect(Relay.parseProjectMemoryBatch(JSON.stringify({ schema_version: 1, candidates: [candidate] }))).toMatchObject({
    candidates: [candidate],
  })
  expect(() =>
    Relay.parseProjectMemoryBatch(
      JSON.stringify({ schema_version: 1, candidates: [{ ...candidate, artifact_refs: ["libgame.so"] }] }),
    ),
  ).toThrow("artifact_refs must be an array of objects")
  expect(() =>
    Relay.parseProjectMemoryBatch(
      JSON.stringify({ schema_version: 1, candidates: [{ ...candidate, source_refs: ["task-1"] }] }),
    ),
  ).toThrow("source_refs must be an array of objects")
})

test("project memory candidate parser validates evidence-backed invalidations", () => {
  const invalidation = {
    memory_id: "memory-1",
    content_hash: "a".repeat(64),
    status: "stale",
    reason: "The referenced artifact changed",
    evidence_refs: ["task:new-artifact"],
  }
  expect(
    Relay.parseProjectMemoryBatch(JSON.stringify({ schema_version: 1, candidates: [], invalidations: [invalidation] })),
  ).toMatchObject({ invalidations: [invalidation] })
  expect(() =>
    Relay.parseProjectMemoryBatch(
      JSON.stringify({ schema_version: 1, candidates: [], invalidations: [{ ...invalidation, status: "deleted" }] }),
    ),
  ).toThrow("has invalid fields")
  expect(() =>
    Relay.parseProjectMemoryBatch(
      JSON.stringify({ schema_version: 1, candidates: [], invalidations: [{ ...invalidation, evidence_refs: [] }] }),
    ),
  ).toThrow("requires evidence_refs")
})

test("project memory source recheck reports available changed and missing files", async () => {
  await using tmp = await tmpdir({ git: true })
  await Bun.write(path.join(tmp.path, "present.txt"), "current")
  const currentHash = new Bun.CryptoHasher("sha256").update("current").digest("hex")
  const updates = await Relay.projectMemorySourceAvailability(tmp.path, [
    {
      memory_id: "memory-a",
      source_refs: [
        { relative_path: "present.txt", sha256: currentHash },
        { relative_path: "missing.txt", sha256: "old" },
      ],
    },
    {
      memory_id: "memory-b",
      source_refs: [{ relative_path: "present.txt", sha256: "different" }],
    },
  ])
  expect(updates).toContainEqual({
    memory_id: "memory-a",
    relative_path: "present.txt",
    sha256: currentHash,
    status: "available",
  })
  expect(updates).toContainEqual({ memory_id: "memory-a", relative_path: "missing.txt", status: "source_missing" })
  expect(updates.find((item) => item.memory_id === "memory-b")?.status).toBe("changed")
})

test("project memory feature flag defaults on and accepts explicit disable", () => {
  expect(Relay.projectMemoryEnabled({})).toBe(true)
  expect(Relay.projectMemoryEnabled({ OPENCODE_PROJECT_MEMORY_ENABLED: "false" })).toBe(false)
  expect(Relay.projectMemoryEnabled({ OPENCODE_PROJECT_MEMORY_ENABLED: "1" })).toBe(true)
})

test("project memory cancellation during source inspection emits no post-cancel result", async () => {
  await using tmp = await tmpdir({ git: true })
  const relay = relayServer()
  envForRelay(relay.url, tmp.path)
  const listener = await Server.listen({ hostname: "127.0.0.1", port: 0 })
  try {
    await withTimeout(
      waitFor(() => relay.messages.find((msg) => msg.type === "device.hello"), "missing relay hello"),
      5_000,
      "timed out waiting for relay hello",
    )
    const socket = await withTimeout(
      waitFor(() => Array.from(relay.sockets)[0], "missing relay socket"),
      5_000,
      "timed out waiting for relay socket",
    )
    const sourceRefs = Array.from({ length: 128 }, (_, index) => ({
      relative_path: `../outside-${index}.txt`,
    }))
    const job = {
      job_id: "memory-job-cancel",
      project_scope_id: "scope-1",
      project_id: "project-1",
      binding_epoch: 1,
      fencing_token: 1,
      trigger: "source_recheck",
      current_memory: [{ memory_id: "memory-1", source_refs: sourceRefs }],
    }
    socket.send(serverEnvelope("project.memory.run", job))
    socket.send(serverEnvelope("project.memory.cancel", { job_id: job.job_id }))

    await withTimeout(
      waitFor(
        () => relay.messages.find((msg) => msg.type === "project.memory.accepted"),
        "missing project.memory.accepted",
      ),
      5_000,
      "timed out waiting for project memory acceptance",
    )
    await listener.stop(true)

    expect(
      relay.messages.filter(
        (msg) =>
          msg.payload.job_id === job.job_id &&
          ["project.memory.sources", "project.memory.completed", "project.memory.failed"].includes(msg.type),
      ),
    ).toEqual([])
  } finally {
    await listener.stop(true)
    relay.stop()
  }
})

test("project memory candidates with changed or unverified file hashes are deferred", async () => {
  await using tmp = await tmpdir({ git: true })
  await Bun.write(path.join(tmp.path, "stable.txt"), "stable")
  const stableHash = new Bun.CryptoHasher("sha256").update("stable").digest("hex")
  const result = await Relay.filterChangedProjectMemoryCandidates(tmp.path, {
    candidates: [
      { subject_key: "stable", source_refs: [{ relative_path: "stable.txt", sha256: stableHash }] },
      { subject_key: "changed", source_refs: [{ relative_path: "stable.txt", sha256: "a".repeat(64) }] },
      { subject_key: "missing-hash", source_refs: [{ relative_path: "stable.txt" }] },
      { subject_key: "task-only", source_refs: [{ task_id: "task-1" }] },
    ],
  })
  expect(result.dropped).toBe(2)
  expect((result.batch.candidates as Array<{ subject_key: string }>).map((item) => item.subject_key)).toEqual([
    "stable",
    "task-only",
  ])
})

function waitFor<T>(fn: () => T | undefined, message: string | (() => string), timeout = 20_000) {
  const started = Date.now()
  return new Promise<T>((resolve, reject) => {
    const tick = () => {
      const value = fn()
      if (value !== undefined) {
        resolve(value)
        return
      }
      if (Date.now() - started > timeout) {
        reject(new Error(typeof message === "function" ? message() : message))
        return
      }
      setTimeout(tick, 10)
    }
    tick()
  })
}

function relayServer() {
  const sockets = new Set<ServerWebSocket<unknown>>()
  const messages: RelayMessage[] = []
  const server = Bun.serve({
    port: 0,
    fetch(req, srv) {
      if (new URL(req.url).pathname !== "/codex/ws/device") {
        return new Response("not found", { status: 404 })
      }
      if (srv.upgrade(req)) return
      return new Response("upgrade failed", { status: 400 })
    },
    websocket: {
      open(ws) {
        sockets.add(ws)
      },
      message(ws, raw) {
        messages.push(parse(String(raw)))
        ws.send(
          JSON.stringify({
            type: "device.welcome",
            request_id: "welcome-1",
            payload: { heartbeat_interval_sec: 1 },
          }),
        )
      },
      close(ws) {
        sockets.delete(ws)
      },
    },
  })
  return {
    server,
    messages,
    sockets,
    url: `${server.url.origin}/codex/ws/device`,
    stop() {
      for (const socket of sockets) socket.close()
      server.stop(true)
    },
  }
}

function envForRelay(relayURL: string, root: string) {
  process.env.OPENCODE_RELAY_URL = relayURL
  process.env.OPENCODE_RELAY_OPERATOR_KEY = "operator-key"
  process.env.OPENCODE_RELAY_AGENT_ID = "agent-1"
  process.env.OPENCODE_RELAY_MACHINE_ID = "machine-1"
  process.env.OPENCODE_RELAY_PROJECT_ID = "project-1"
  process.env.OPENCODE_RELAY_PROJECT_ROOT = root
  process.env.OPENCODE_RELAY_HOSTNAME = "host-1"
}

function runEnvelope(payload: Record<string, unknown>) {
  return JSON.stringify({
    type: "task.run",
    request_id: "run-1",
    payload: {
      task_id: "task-1",
      agent_id: "agent-1",
      machine_id: "machine-1",
      project_id: "project-1",
      parts: [{ type: "text", text: "say relay done" }],
      ...payload,
    },
  })
}

function serverEnvelope(type: string, payload: Record<string, unknown>, requestID = `${type}-1`) {
  return JSON.stringify({
    type,
    request_id: requestID,
    payload,
  })
}

function textChunks(value: string, finishReason = "stop") {
  return [
    { choices: [{ delta: { role: "assistant" } }] },
    { choices: [{ delta: { content: value } }] },
    {
      choices: [{ finish_reason: finishReason }],
      usage: { prompt_tokens: 1, completion_tokens: 2, total_tokens: 3 },
    },
  ]
}

function emptyStopChunks() {
  return [
    { choices: [{ delta: { role: "assistant" } }] },
    { choices: [{ finish_reason: "stop" }], usage: { prompt_tokens: 0, completion_tokens: 0, total_tokens: 0 } },
  ]
}

function toolChunks(name: string, input: unknown) {
  return [
    { choices: [{ delta: { role: "assistant" } }] },
    {
      choices: [
        {
          delta: {
            tool_calls: [
              {
                index: 0,
                id: `call-${name}`,
                type: "function",
                function: {
                  name,
                  arguments: "",
                },
              },
            ],
          },
        },
      ],
    },
    {
      choices: [
        {
          delta: {
            tool_calls: [
              {
                index: 0,
                function: {
                  arguments: JSON.stringify(input),
                },
              },
            ],
          },
        },
      ],
    },
    { choices: [{ delta: {}, finish_reason: "tool_calls" }] },
  ]
}

function jsonText(value: unknown) {
  return textChunks(JSON.stringify(value))
}

function imageResponse(value: string) {
  return {
    data: [
      {
        b64_json: Buffer.from(value).toString("base64"),
      },
    ],
  }
}

function isTitleRequest(body: unknown) {
  return !!body && typeof body === "object" && JSON.stringify(body).includes("Generate a title for this conversation")
}

type LLMHTTPError = {
  type: "http-error"
  status: number
  body: Record<string, unknown>
}

function isLLMHTTPError(value: unknown): value is LLMHTTPError {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false
  const record = value as Record<string, unknown>
  return record.type === "http-error" && typeof record.status === "number" && !!record.body
}

function llmServer(
  script: Array<Record<string, unknown> | Array<Record<string, unknown>> | LLMHTTPError> = [textChunks("relay done")],
) {
  const inputs: Record<string, unknown>[] = []
  const queue = [...script]
  const server = Bun.serve({
    port: 0,
    async fetch(req) {
      const url = new URL(req.url)
      if (url.pathname !== "/v1/chat/completions" && !url.pathname.startsWith("/v1/images/")) {
        return new Response("not found", { status: 404 })
      }
      const body = (await req.json()) as Record<string, unknown>
      inputs.push(body)
      if (url.pathname.startsWith("/v1/images/")) {
        const next = queue.shift()
        return Response.json(Array.isArray(next) || !next ? imageResponse("relay image") : next)
      }
      const encoder = new TextEncoder()
      const next = isTitleRequest(body) ? undefined : queue.shift()
      if (isLLMHTTPError(next)) {
        return Response.json(next.body, { status: next.status })
      }
      const chunks = isTitleRequest(body) || !Array.isArray(next) ? textChunks("Relay Test") : next
      return new Response(
        new ReadableStream({
          start(controller) {
            for (const chunk of chunks) controller.enqueue(encoder.encode(`data: ${JSON.stringify(chunk)}\n\n`))
            controller.enqueue(encoder.encode("data: [DONE]\n\n"))
            controller.close()
          },
        }),
        {
          headers: { "content-type": "text/event-stream" },
        },
      )
    },
  })
  return {
    url: `${server.url.origin}/v1`,
    inputs,
    stop() {
      server.stop(true)
    },
  }
}

function delayedLLMServer(delayMS: number, chunks = textChunks("relay done")) {
  const inputs: Record<string, unknown>[] = []
  const server = Bun.serve({
    port: 0,
    async fetch(req) {
      const url = new URL(req.url)
      if (url.pathname !== "/v1/chat/completions") {
        return new Response("not found", { status: 404 })
      }
      const body = (await req.json()) as Record<string, unknown>
      inputs.push(body)
      const encoder = new TextEncoder()
      return new Response(
        new ReadableStream({
          async start(controller) {
            await Bun.sleep(delayMS)
            for (const chunk of chunks) controller.enqueue(encoder.encode(`data: ${JSON.stringify(chunk)}\n\n`))
            controller.enqueue(encoder.encode("data: [DONE]\n\n"))
            controller.close()
          },
        }),
        {
          headers: { "content-type": "text/event-stream" },
        },
      )
    },
  })
  return {
    url: `${server.url.origin}/v1`,
    inputs,
    stop() {
      server.stop(true)
    },
  }
}

function delayedScriptLLMServer(script: Array<{ delayMS: number; chunks: Array<Record<string, unknown>> }>) {
  const inputs: Record<string, unknown>[] = []
  const queue = [...script]
  const server = Bun.serve({
    port: 0,
    async fetch(req) {
      const url = new URL(req.url)
      if (url.pathname !== "/v1/chat/completions") {
        return new Response("not found", { status: 404 })
      }
      const body = (await req.json()) as Record<string, unknown>
      inputs.push(body)
      const encoder = new TextEncoder()
      const next = isTitleRequest(body)
        ? { delayMS: 0, chunks: textChunks("Relay Test") }
        : (queue.shift() ?? { delayMS: 0, chunks: textChunks("unexpected extra request") })
      return new Response(
        new ReadableStream({
          async start(controller) {
            await Bun.sleep(next.delayMS)
            for (const chunk of next.chunks) controller.enqueue(encoder.encode(`data: ${JSON.stringify(chunk)}\n\n`))
            controller.enqueue(encoder.encode("data: [DONE]\n\n"))
            controller.close()
          },
        }),
        {
          headers: { "content-type": "text/event-stream" },
        },
      )
    },
  })
  return {
    url: `${server.url.origin}/v1`,
    inputs,
    stop() {
      server.stop(true)
    },
  }
}

function invalidKeyLLMServer() {
  const inputs: Record<string, unknown>[] = []
  const server = Bun.serve({
    port: 0,
    async fetch(req) {
      const url = new URL(req.url)
      if (url.pathname !== "/v1/chat/completions") {
        return new Response("not found", { status: 404 })
      }
      inputs.push((await req.json()) as Record<string, unknown>)
      return Response.json({ code: "INVALID_API_KEY", message: "Invalid API key" }, { status: 401 })
    },
  })
  return {
    url: `${server.url.origin}/v1`,
    inputs,
    stop() {
      server.stop(true)
    },
  }
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

async function writeRelayConfig(dir: string, baseURL: string, extra: Record<string, unknown>) {
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
              tool_call: true,
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
      ...extra,
    }),
  )
}

async function writeImageConfig(dir: string, baseURL: string) {
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
            "test-image": {
              id: "test-image",
              name: "Test Image",
              attachment: true,
              reasoning: false,
              temperature: false,
              tool_call: false,
              release_date: "2025-01-01",
              limit: { context: 100000, output: 10000 },
              cost: { input: 0, output: 0 },
              modalities: {
                input: ["text", "image"],
                output: ["image"],
              },
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
          model: "test/test-image",
        },
      },
    }),
  )
}

describe.serial("Relay", () => {
  test("splits think tags across streamed task deltas", () => {
    const task = {}
    expect(Relay.splitTaskDelta(task, "hello <thi")).toEqual([{ field: "text", content: "hello " }])
    expect(Relay.splitTaskDelta(task, "nk>reason")).toEqual([{ field: "reasoning", content: "reason" }])
    expect(Relay.splitTaskDelta(task, "ing</thi")).toEqual([{ field: "reasoning", content: "ing" }])
    expect(Relay.splitTaskDelta(task, "nk> world")).toEqual([{ field: "text", content: " world" }])
    expect(Relay.flushTaskDelta(task, true)).toEqual([])
  })

  test("splits thinking tags across streamed task deltas", () => {
    const task = {}
    expect(Relay.splitTaskDelta(task, "hello <think")).toEqual([{ field: "text", content: "hello " }])
    expect(Relay.splitTaskDelta(task, "ing>reason")).toEqual([{ field: "reasoning", content: "reason" }])
    expect(Relay.splitTaskDelta(task, "ing</think")).toEqual([{ field: "reasoning", content: "ing" }])
    expect(Relay.splitTaskDelta(task, "ing> world")).toEqual([{ field: "text", content: " world" }])
    expect(Relay.flushTaskDelta(task, true)).toEqual([])
  })

  test("flushes unfinished think blocks as reasoning", () => {
    const task = {}
    expect(Relay.splitTaskDelta(task, "<think>partial reasoning")).toEqual([
      { field: "reasoning", content: "partial reasoning" },
    ])
    expect(Relay.flushTaskDelta(task, true)).toEqual([])
  })

  test("flushes unfinished thinking blocks as reasoning", () => {
    const task = {}
    expect(Relay.splitTaskDelta(task, "<thinking>partial reasoning")).toEqual([
      { field: "reasoning", content: "partial reasoning" },
    ])
    expect(Relay.flushTaskDelta(task, true)).toEqual([])
  })

  test("does not carry unfinished thinking tags across text parts", () => {
    const task = {}
    expect(Relay.splitTaskDelta(task, "<thinking>draft answer", "part_a")).toEqual([
      { field: "reasoning", content: "draft answer" },
    ])
    expect(Relay.splitTaskDelta(task, "**结论**\n正文", "part_b")).toEqual([
      { field: "text", content: "**结论**\n正文" },
    ])
    expect(Relay.flushTaskDelta(task, true)).toEqual([])
  })

  test("keeps thinking blocks open until their matching close tag", () => {
    const task = {}
    expect(Relay.splitTaskDelta(task, "<thinking>reason</think> still reasoning")).toEqual([
      { field: "reasoning", content: "reason</think> still reasoning" },
    ])
    expect(Relay.splitTaskDelta(task, "</thinking> visible")).toEqual([{ field: "text", content: " visible" }])
    expect(Relay.flushTaskDelta(task, true)).toEqual([])
  })

  test("flushes a partial think-tag prefix when the model starts a new text part", () => {
    const task = {}
    expect(Relay.splitTaskDelta(task, "answer<thi", "part_a")).toEqual([{ field: "text", content: "answer" }])
    expect(Relay.splitTaskDelta(task, "next", "part_b")).toEqual([
      { field: "text", content: "<thi" },
      { field: "text", content: "next" },
    ])
  })

  test("matches think tags case-insensitively while streaming", () => {
    const task = {}
    expect(Relay.splitTaskDelta(task, "<Thinking>reason</Thinking> visible")).toEqual([
      { field: "reasoning", content: "reason" },
      { field: "text", content: " visible" },
    ])
  })

  test("cleans think and thinking tags from final results", () => {
    expect(Relay.testInternals.clean("before <think>hidden</think> after")).toBe("before after")
    expect(Relay.testInternals.clean("before <thinking>hidden</thinking> after")).toBe("before after")
    expect(Relay.testInternals.clean("before <thinking>hidden")).toBe("before ")
  })

  test("appends dynamic artifact instruction after user parts", () => {
    const out = Relay.testInternals.parts(
      {
        metadata: { delivery_required: "true" },
        parts: [
          { type: "text", text: "say relay done" },
          { type: "file", url: "data:text/plain,hello", filename: "hello.txt" },
        ],
      } as any,
      ".chatcodex-artifacts/task_dynamic",
    )

    expect(out[0]).toEqual({ type: "text", text: "say relay done" })
    expect(out.at(-1)).toEqual({
      type: "text",
      text: expect.stringContaining(".chatcodex-artifacts/task_dynamic/"),
      synthetic: true,
    })

    const userText = JSON.stringify(out)
    expect(userText.indexOf("系统附加说明")).toBeGreaterThan(userText.indexOf("say relay done"))
  })

  test("scopes ordinary requests without forcing a delivery workflow", () => {
    const out = Relay.testInternals.parts(
      {
        parts: [{ type: "text", text: "请解释这个函数的作用。" }],
      } as any,
      ".chatcodex-artifacts/task_ordinary",
    )

    expect(out[0]).toEqual({ type: "text", text: "请解释这个函数的作用。" })
    expect(out).toHaveLength(2)
    expect(out[1]).toMatchObject({ type: "text", synthetic: true })
    expect((out[1] as any).text).toContain(".chatcodex-artifacts/task_ordinary/")
    expect((out[1] as any).text).toContain("普通代码修改无需复制")
  })

  test("detects delivery requests and appends a mandatory artifact workflow", () => {
    const run = {
      parts: [{ type: "text", text: "请生成一份 PDF 报告并交付给我。" }],
    } as any
    expect(Relay.testInternals.deliveryRequired(run)).toBe(true)

    const out = Relay.testInternals.parts(run, ".chatcodex-artifacts/task_delivery")
    const instruction = (out.at(-1) as any)?.text ?? ""
    expect(instruction).toContain("这是完成条件")
    expect(instruction).toContain(".chatcodex-artifacts/task_delivery/")
    expect(instruction).toContain("重新列出")
  })

  test("does not treat 给我看 as a file delivery request", () => {
    expect(
      Relay.testInternals.deliveryRequired({
        parts: [
          {
            type: "text",
            text: "那让速通max也接入这个流程吧 写完之后，你先不要推送，先模拟一遍对应的流程给我看一下",
          },
        ],
      } as any),
    ).toBe(false)
  })

  test("honors an explicit delivery metadata override", () => {
    expect(
      Relay.testInternals.deliveryRequired({
        metadata: { delivery_required: "true" },
        parts: [{ type: "text", text: "请分析这段代码。" }],
      } as any),
    ).toBe(true)
    expect(
      Relay.testInternals.deliveryRequired({
        metadata: { delivery_required: "false" },
        parts: [{ type: "text", text: "请生成一份报告。" }],
      } as any),
    ).toBe(false)
  })

  test("maps official part updated events to reasoning deltas by part id", () => {
    const task = {}
    Relay.rememberTaskPart(task, "part_reasoning", "reasoning")
    Relay.rememberTaskPart(task, "part_text", "text")

    expect(
      Relay.taskDeltaField(task, {
        properties: { partID: "part_reasoning", field: "text", delta: "thinking" },
      }),
    ).toBe("reasoning")
    expect(
      Relay.taskDeltaField(task, {
        properties: { partID: "part_text", field: "text", delta: "answer" },
      }),
    ).toBe("text")
  })

  test("marks compaction summary message deltas as internal", () => {
    const task = {}
    Relay.rememberInternalMessage(task, "msg_summary", {
      id: "msg_summary",
      role: "assistant",
      summary: true,
      mode: "compaction",
    })
    Relay.rememberInternalPart(task, "part_summary", "msg_summary")
    Relay.rememberTaskPart(task, "part_answer", "text")

    expect(
      Relay.taskDeltaField(task, {
        properties: { partID: "part_summary", messageID: "msg_summary", field: "text", delta: "## Goal" },
      }),
    ).toBe("internal")
    expect(
      Relay.taskDeltaField(task, {
        properties: { partID: "part_answer", messageID: "msg_answer", field: "text", delta: "answer" },
      }),
    ).toBe("text")
  })

  test("derives relay compaction status from compaction parts", () => {
    expect(
      Relay.compactionStateFromPart({
        id: "part_compaction",
        type: "compaction",
        auto: true,
      }),
    ).toEqual({ type: "started", reason: "auto" })

    expect(
      Relay.compactionStateFromPart({
        id: "part_compaction",
        type: "compaction",
        auto: false,
        tail_start_id: "msg_tail",
      }),
    ).toEqual({ type: "completed", reason: "manual" })

    expect(Relay.compactionStateFromPart({ type: "text" })).toBeUndefined()
  })

  test("closes compaction when a full summary has no tail marker", () => {
    expect(
      Relay.compactionCompletedFromMessage({
        id: "msg_summary",
        role: "assistant",
        summary: true,
        mode: "compaction",
        time: { created: 1, completed: 2 },
      }),
    ).toBe(true)
    expect(
      Relay.compactionCompletedFromMessage({
        id: "msg_summary",
        role: "assistant",
        summary: true,
        mode: "compaction",
        time: { created: 1 },
      }),
    ).toBe(false)
    expect(
      Relay.compactionCompletedFromMessage({
        id: "msg_summary",
        role: "assistant",
        summary: true,
        mode: "compaction",
        error: { name: "APIError", message: "Unauthorized" },
        time: { created: 1, completed: 2 },
      }),
    ).toBe(false)
    expect(
      Relay.compactionCompletedFromMessage({
        id: "msg_answer",
        role: "assistant",
        time: { created: 1, completed: 2 },
      }),
    ).toBe(false)
  })

  test("maps todowrite tool parts to relay plan updates", () => {
    const plan = Relay.todoPlanFromToolPart({ id: "task-1", session: "ses_123" }, {
      id: "part_todo",
      callID: "call_todo",
      tool: "todowrite",
      type: "tool",
      state: {
        status: "completed",
        input: {
          todos: [
            {
              id: "todo_a",
              planID: "plan_custom",
              content: "分析现有计划显示链路",
              status: "completed",
              priority: "high",
            },
            {
              id: "todo_b",
              planID: "plan_custom",
              content: "补齐 todowrite 到计划事件转换",
              status: "in_progress",
              priority: "medium",
            },
          ],
        },
        output: "[]",
        title: "1 todos",
        time: { start: Date.now(), end: Date.now() },
      },
    } as any)

    expect(plan).toEqual({
      id: "plan_custom",
      title: "执行计划",
      mode: "build",
      status: "in_progress",
      session_id: "ses_123",
      items: [
        {
          id: "todo_a",
          planID: "plan_custom",
          text: "分析现有计划显示链路",
          status: "completed",
          priority: "high",
        },
        {
          id: "todo_b",
          planID: "plan_custom",
          text: "补齐 todowrite 到计划事件转换",
          status: "in_progress",
          priority: "medium",
        },
      ],
    })
  })

  test("does not build relay plan updates from todowrite parts without a real plan id", () => {
    const plan = Relay.todoPlanFromToolPart({ id: "task-1", session: "ses_123" }, {
      id: "part_todo",
      callID: "call_todo",
      tool: "todowrite",
      type: "tool",
      state: {
        status: "completed",
        input: {
          todos: [
            {
              id: "todo_a",
              content: "分析现有计划显示链路",
              status: "completed",
              priority: "high",
            },
          ],
        },
        output: "[]",
        title: "1 todos",
        time: { start: Date.now(), end: Date.now() },
      },
    } as any)

    expect(plan).toBeUndefined()
  })

  test("suppresses todowrite tool updates because plan updates own the UI", async () => {
    await using tmp = await tmpdir({ git: true })
    const llm = llmServer([
      toolChunks("todowrite", {
        todos: [{ content: "更新计划", status: "completed", priority: "high" }],
      }),
      textChunks("todo done"),
    ])
    const relay = relayServer()
    await writeRelayConfig(tmp.path, llm.url, {
      permission: {
        todowrite: "allow",
      },
    })
    envForRelay(relay.url, tmp.path)

    const listener = await Server.listen({ hostname: "127.0.0.1", port: 0 })
    try {
      await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "device.hello"), "missing relay hello"),
        5_000,
        "timed out waiting for relay hello",
      )

      const socket = await withTimeout(
        waitFor(() => Array.from(relay.sockets)[0], "missing relay socket"),
        5_000,
        "timed out waiting for relay socket",
      )
      socket.send(runEnvelope({ task_id: "todo-task", parts: [{ type: "text", text: "update todo" }] }))

      const plan = await withTimeout(
        waitFor(
          () => relay.messages.find((msg) => msg.type === "task.plan_updated"),
          () =>
            `missing plan update; relay messages: ${JSON.stringify(relay.messages)}; llm inputs: ${JSON.stringify(llm.inputs)}`,
          15_000,
        ),
        20_000,
        "timed out waiting for plan update",
      )
      expect(plan.payload.plan.items).toHaveLength(1)
      expect(plan.payload.plan.items[0].text).toBe("更新计划")

      await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "task.completed"), "missing task.completed", 15_000),
        20_000,
        "timed out waiting for task.completed",
      )
      expect(
        relay.messages.filter((msg) => msg.type === "task.tool_updated" && msg.payload.tool?.tool === "todowrite"),
      ).toHaveLength(0)
    } finally {
      await listener.stop(true)
      relay.stop()
      llm.stop()
    }
  })

  test("detects empty relay output snapshots", () => {
    expect(
      Relay.taskOutputState({
        parts: [
          {
            id: "step-start",
            messageID: "msg",
            sessionID: "ses",
            type: "step-start",
          },
          {
            id: "step-finish",
            messageID: "msg",
            sessionID: "ses",
            type: "step-finish",
            reason: "stop",
            tokens: { input: 0, output: 0, reasoning: 0, cache: { read: 0, write: 0 } },
            cost: 0,
          },
        ] as any,
      }).empty,
    ).toBe(true)

    expect(
      Relay.taskOutputState({
        parts: [
          {
            id: "text",
            messageID: "msg",
            sessionID: "ses",
            type: "text",
            text: "done",
          },
        ] as any,
      }).empty,
    ).toBe(false)
  })

  test("evaluates build completion from structured action state", () => {
    const state = Relay.testInternals.createCompletionGuardState()
    Relay.testInternals.beginCompletionRound(state)
    const first = Relay.testInternals.evaluateCompletion({
      state,
      mode: "build",
      text: "接下来我会读取文件并完成修改。",
    })
    expect(first.type).toBe("continue")
    expect(first.reason).toBe("promise_without_action")

    expect(
      Relay.testInternals.evaluateCompletion({
        state,
        mode: "plan",
        text: "接下来我会整理方案。",
      }).type,
    ).toBe("allow")

    Relay.testInternals.rememberCompletionPart(state, {
      id: "tool-1",
      type: "tool",
      tool: "bash",
      state: { status: "completed" },
    })

    expect(Relay.testInternals.completionCounts(state).completedToolCount).toBe(1)
    expect(
      Relay.testInternals.evaluateCompletion({
        state,
        mode: "build",
        text: "接下来我会总结结果。",
      }).type,
    ).toBe("continue")

    expect(
      Relay.testInternals.evaluateCompletion({
        state,
        mode: "build",
        text: "命令执行完成，结果为 guard-ok。",
      }).type,
    ).toBe("allow")

    const artifactState = Relay.testInternals.createCompletionGuardState()
    Relay.testInternals.beginCompletionRound(artifactState)
    Relay.testInternals.rememberCompletionArtifacts(artifactState, [{ path: "out/report.txt" }], [])
    expect(
      Relay.testInternals.evaluateCompletion({
        state: artifactState,
        mode: "build",
        text: "接下来我会整理生成的产物。",
      }).type,
    ).toBe("continue")
  })

  test("accepts direct text conversation without requiring a build action", () => {
    const state = Relay.testInternals.createCompletionGuardState()
    Relay.testInternals.beginCompletionRound(state)
    Relay.testInternals.rememberCompletionModel(state, { finishReason: "stop", finalText: "你好，有什么可以帮你？" })

    const decision = Relay.testInternals.evaluateCompletion({
      state,
      mode: "build",
      text: "你好，有什么可以帮你？",
    })

    expect(decision.type).toBe("allow")
    expect(decision.reason).toBe("direct_text_answer")
  })

  test("waits for terminal task cleanup before accepting the next queued task", async () => {
    let settle = () => {}
    const settled = new Promise<void>((resolve) => {
      settle = resolve
    })
    let handoffFinished = false
    const handoff = Relay.testInternals
      .waitForTerminalHandoff({
        stop: false,
        task: {
          id: "previous-task",
          finalizationCommitted: true,
          settled,
        },
      } as any)
      .then(() => {
        handoffFinished = true
      })

    await Bun.sleep(20)
    expect(handoffFinished).toBe(false)
    settle()
    await handoff
    expect(handoffFinished).toBe(true)
  })

  test("completes a normal build-mode greeting without a false action warning", async () => {
    await using tmp = await tmpdir({ git: true })
    const llm = llmServer([textChunks("你好，有什么可以帮你？")])
    const relay = relayServer()
    await writeConfig(tmp.path, llm.url)
    envForRelay(relay.url, tmp.path)

    const listener = await Server.listen({ hostname: "127.0.0.1", port: 0 })
    try {
      await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "device.hello"), "missing relay hello"),
        5_000,
        "timed out waiting for relay hello",
      )
      const socket = await withTimeout(
        waitFor(() => Array.from(relay.sockets)[0], "missing relay socket"),
        5_000,
        "timed out waiting for relay socket",
      )
      socket.send(runEnvelope({ task_id: "greeting-task", parts: [{ type: "text", text: "你好" }] }))

      const completed = await withTimeout(
        waitFor(
          () => relay.messages.find((msg) => msg.type === "task.completed" && msg.payload.task_id === "greeting-task"),
          "missing greeting task.completed",
        ),
        20_000,
        "timed out waiting for greeting completion",
      )
      expect(completed.payload.result).toBe("你好，有什么可以帮你？")
      expect(relay.messages.find((msg) => msg.type === "task.failed")).toBeUndefined()
    } finally {
      await listener.stop(true)
      relay.stop()
      llm.stop()
    }
  })

  test("keeps an active goal on the current websocket after relay reconnect", async () => {
    await using tmp = await tmpdir({ git: true })
    const llm = delayedScriptLLMServer([{ delayMS: 4_500, chunks: textChunks("目标本轮处理完成") }])
    const relay = relayServer()
    await writeConfig(tmp.path, llm.url)
    envForRelay(relay.url, tmp.path)

    const listener = await Server.listen({ hostname: "127.0.0.1", port: 0 })
    try {
      await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "device.hello"), "missing first relay hello"),
        5_000,
        "timed out waiting for first relay hello",
      )
      const firstSocket = await withTimeout(
        waitFor(() => Array.from(relay.sockets)[0], "missing first relay socket"),
        5_000,
        "timed out waiting for first relay socket",
      )
      firstSocket.send(
        runEnvelope({
          task_id: "goal-reconnect-task",
          metadata: {
            model: "test/test-model",
            agent: "build",
            goal: "完成重连验证",
            goal_id: "goal-reconnect",
            goal_max_iterations: "1",
          },
        }),
      )
      await withTimeout(
        waitFor(
          () =>
            relay.messages.find((msg) => msg.type === "task.started" && msg.payload.task_id === "goal-reconnect-task"),
          "missing goal task start",
        ),
        10_000,
        "timed out waiting for goal task start",
      )

      firstSocket.close()
      await withTimeout(
        waitFor(() => (relay.sockets.size === 0 ? true : undefined), "first relay socket stayed open"),
        5_000,
        "timed out waiting for first relay socket close",
      )
      await withTimeout(
        waitFor(
          () => (relay.messages.filter((msg) => msg.type === "device.hello").length >= 2 ? true : undefined),
          "missing reconnect relay hello",
          8_000,
        ),
        10_000,
        "timed out waiting for relay reconnect",
      )

      const reconnectHello = relay.messages.filter((msg) => msg.type === "device.hello").at(-1)
      expect(reconnectHello?.payload.running_task_id).toBe("goal-reconnect-task")
      expect(reconnectHello?.payload.capabilities).toContain("task_resume_v1")
      const reconnectSocket = Array.from(relay.sockets)[0]
      reconnectSocket?.send(
        runEnvelope({
          task_id: "goal-reconnect-task",
          resume: true,
          metadata: {
            model: "test/test-model",
            agent: "build",
            goal: "完成重连验证",
            goal_id: "goal-reconnect",
            goal_iteration: "1",
            goal_max_iterations: "1",
          },
        }),
      )

      const resumed = relay.messages.find(
        (msg) =>
          msg.type === "task.progress" &&
          msg.payload.task_id === "goal-reconnect-task" &&
          msg.payload.metadata?.source === "relay_reconnect",
      )
      expect(resumed?.payload.metadata?.reconnected).toBe(true)
      const heartbeat = relay.messages.find(
        (msg) =>
          msg.type === "task.goal_heartbeat" &&
          msg.payload.task_id === "goal-reconnect-task" &&
          msg.payload.metadata?.reconnected === true,
      )
      expect(heartbeat?.payload.goal_id).toBe("goal-reconnect")

      const completed = await withTimeout(
        waitFor(
          () =>
            relay.messages.find(
              (msg) => msg.type === "task.completed" && msg.payload.task_id === "goal-reconnect-task",
            ),
          "missing goal completion after reconnect",
          12_000,
        ),
        15_000,
        "timed out waiting for goal completion after reconnect",
      )
      expect(completed.payload.result).toContain("目标本轮处理完成")
      expect(
        relay.messages.find(
          (msg) =>
            msg.type === "task.failed" &&
            msg.payload.task_id === "goal-reconnect-task" &&
            String(msg.payload.error).includes("agent busy"),
        ),
      ).toBeUndefined()
      expect(
        relay.messages.filter((msg) => msg.type === "task.started" && msg.payload.task_id === "goal-reconnect-task")
          .length,
      ).toBeGreaterThanOrEqual(2)
    } finally {
      await listener.stop(true)
      relay.stop()
      llm.stop()
    }
  }, 20_000)

  test("flushes a terminal task event that was produced while relay was disconnected", async () => {
    await using tmp = await tmpdir({ git: true })
    const llm = delayedScriptLLMServer([{ delayMS: 800, chunks: textChunks("断线期间任务已完成") }])
    const relay = relayServer()
    await writeConfig(tmp.path, llm.url)
    envForRelay(relay.url, tmp.path)

    const listener = await Server.listen({ hostname: "127.0.0.1", port: 0 })
    try {
      await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "device.hello"), "missing first relay hello"),
        5_000,
        "timed out waiting for first relay hello",
      )
      const firstSocket = await withTimeout(
        waitFor(() => Array.from(relay.sockets)[0], "missing first relay socket"),
        5_000,
        "timed out waiting for first relay socket",
      )
      firstSocket.send(runEnvelope({ task_id: "disconnected-terminal-task" }))
      await withTimeout(
        waitFor(
          () =>
            relay.messages.find(
              (msg) => msg.type === "task.started" && msg.payload.task_id === "disconnected-terminal-task",
            ),
          "missing disconnected terminal task start",
        ),
        10_000,
        "timed out waiting for disconnected terminal task start",
      )

      firstSocket.close()
      await withTimeout(
        waitFor(() => (relay.sockets.size === 0 ? true : undefined), "first relay socket stayed open"),
        5_000,
        "timed out waiting for first relay socket close",
      )
      await Bun.sleep(1_500)
      expect(
        relay.messages.find(
          (msg) => msg.type === "task.completed" && msg.payload.task_id === "disconnected-terminal-task",
        ),
      ).toBeUndefined()

      const completed = await withTimeout(
        waitFor(
          () =>
            relay.messages.find(
              (msg) => msg.type === "task.completed" && msg.payload.task_id === "disconnected-terminal-task",
            ),
          "missing queued terminal event after reconnect",
          8_000,
        ),
        10_000,
        "timed out waiting for queued terminal event after reconnect",
      )
      expect(completed.payload.result).toContain("断线期间任务已完成")
      expect(relay.messages.filter((msg) => msg.type === "device.hello").length).toBeGreaterThanOrEqual(2)
    } finally {
      await listener.stop(true)
      relay.stop()
      llm.stop()
    }
  }, 18_000)

  test("retries a provider finish error in a new model round and completes", async () => {
    await using tmp = await tmpdir({ git: true })
    const llm = llmServer([
      textChunks("当前先处理文件，接下来继续分析。", "error"),
      textChunks("分析完成，截图已经生成。"),
    ])
    const relay = relayServer()
    await writeConfig(tmp.path, llm.url)
    envForRelay(relay.url, tmp.path)

    const listener = await Server.listen({ hostname: "127.0.0.1", port: 0 })
    try {
      await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "device.hello"), "missing relay hello"),
        5_000,
        "timed out waiting for relay hello",
      )
      const socket = await withTimeout(
        waitFor(() => Array.from(relay.sockets)[0], "missing relay socket"),
        5_000,
        "timed out waiting for relay socket",
      )
      socket.send(runEnvelope({ task_id: "provider-error-task" }))

      const completed = await withTimeout(
        waitFor(
          () =>
            relay.messages.find(
              (msg) => msg.type === "task.completed" && msg.payload.task_id === "provider-error-task",
            ),
          "missing provider-error task.completed",
        ),
        20_000,
        "timed out waiting for provider-error recovery",
      )
      expect(completed.payload.result).toContain("分析完成")
      expect(
        relay.messages.find((msg) => msg.type === "task.retrying" && msg.payload.task_id === "provider-error-task")
          ?.payload,
      ).toMatchObject({ attempt: 1, max_attempts: 5 })
      expect(
        relay.messages.find((msg) => msg.type === "task.failed" && msg.payload.task_id === "provider-error-task"),
      ).toBeUndefined()
      const buildInputs = llm.inputs.filter((input) => !isTitleRequest(input))
      expect(buildInputs).toHaveLength(2)
      expect(JSON.stringify(buildInputs[1])).toContain("previous provider response ended unexpectedly")
    } finally {
      await listener.stop(true)
      relay.stop()
      llm.stop()
    }
  })

  test("keeps an active goal alive after the provider retry budget is exhausted", async () => {
    await using tmp = await tmpdir({ git: true })
    const llm = llmServer([
      ...Array.from({ length: 5 }, (_, index) => textChunks(`goal provider error ${index + 1}`, "error")),
      textChunks("目标连接恢复后继续完成"),
    ])
    const relay = relayServer()
    await writeConfig(tmp.path, llm.url)
    envForRelay(relay.url, tmp.path)

    const listener = await Server.listen({ hostname: "127.0.0.1", port: 0 })
    try {
      await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "device.hello"), "missing relay hello"),
        5_000,
        "timed out waiting for relay hello",
      )
      const socket = await withTimeout(
        waitFor(() => Array.from(relay.sockets)[0], "missing relay socket"),
        5_000,
        "timed out waiting for relay socket",
      )
      socket.send(
        runEnvelope({
          task_id: "goal-provider-recovery-task",
          metadata: {
            model: "test/test-model",
            agent: "build",
            goal: "验证模型连接恢复",
            goal_id: "goal-provider-recovery",
            goal_max_iterations: "1",
          },
        }),
      )

      const paused = await withTimeout(
        waitFor(
          () =>
            relay.messages.find(
              (msg) =>
                msg.type === "task.goal_paused" &&
                msg.payload.task_id === "goal-provider-recovery-task" &&
                msg.payload.reason === "provider_connection_retry",
            ),
          "missing provider recovery checkpoint",
          15_000,
        ),
        18_000,
        "timed out waiting for provider recovery checkpoint",
      )
      expect(paused.payload.status).toBe("paused")

      const completed = await withTimeout(
        waitFor(
          () =>
            relay.messages.find(
              (msg) => msg.type === "task.completed" && msg.payload.task_id === "goal-provider-recovery-task",
            ),
          "missing recovered goal completion",
          15_000,
        ),
        18_000,
        "timed out waiting for recovered goal completion",
      )
      expect(completed.payload.result).toContain("目标连接恢复后继续完成")
      expect(
        relay.messages.find(
          (msg) => msg.type === "task.failed" && msg.payload.task_id === "goal-provider-recovery-task",
        ),
      ).toBeUndefined()
    } finally {
      await listener.stop(true)
      relay.stop()
      llm.stop()
    }
  }, 25_000)

  test("fails after five consecutive provider finish errors", async () => {
    await using tmp = await tmpdir({ git: true })
    const llm = llmServer(Array.from({ length: 5 }, (_, index) => textChunks(`provider error ${index + 1}`, "error")))
    const relay = relayServer()
    await writeConfig(tmp.path, llm.url)
    envForRelay(relay.url, tmp.path)

    const listener = await Server.listen({ hostname: "127.0.0.1", port: 0 })
    try {
      await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "device.hello"), "missing relay hello"),
        5_000,
        "timed out waiting for relay hello",
      )
      const socket = await withTimeout(
        waitFor(() => Array.from(relay.sockets)[0], "missing relay socket"),
        5_000,
        "timed out waiting for relay socket",
      )
      socket.send(runEnvelope({ task_id: "provider-error-exhausted-task" }))

      const failed = await withTimeout(
        waitFor(
          () =>
            relay.messages.find(
              (msg) => msg.type === "task.failed" && msg.payload.task_id === "provider-error-exhausted-task",
            ),
          "missing exhausted provider-error task.failed",
          15_000,
        ),
        20_000,
        "timed out waiting for provider-error exhaustion",
      )
      expect(failed.payload.error).toContain("finish reason error")
      expect(llm.inputs.filter((input) => !isTitleRequest(input))).toHaveLength(5)
      expect(
        relay.messages
          .filter((msg) => msg.type === "task.retrying" && msg.payload.task_id === "provider-error-exhausted-task")
          .map((msg) => msg.payload.attempt),
      ).toEqual([1, 2, 3, 4])
      expect(
        relay.messages.find(
          (msg) => msg.type === "task.completed" && msg.payload.task_id === "provider-error-exhausted-task",
        ),
      ).toBeUndefined()
    } finally {
      await listener.stop(true)
      relay.stop()
      llm.stop()
    }
  }, 20_000)

  test("preserves completed tool results when recovering a provider finish error", async () => {
    await using tmp = await tmpdir({ git: true })
    const outputPath = path.join(tmp.path, "retry-tool.txt")
    const llm = llmServer([
      toolChunks("write", { filePath: outputPath, content: "tool result persisted" }),
      textChunks("", "error"),
      textChunks("tool context preserved"),
    ])
    const relay = relayServer()
    await writeRelayConfig(tmp.path, llm.url, { permission: { edit: "allow" } })
    envForRelay(relay.url, tmp.path)

    const listener = await Server.listen({ hostname: "127.0.0.1", port: 0 })
    try {
      await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "device.hello"), "missing relay hello"),
        5_000,
        "timed out waiting for relay hello",
      )
      const socket = await withTimeout(
        waitFor(() => Array.from(relay.sockets)[0], "missing relay socket"),
        5_000,
        "timed out waiting for relay socket",
      )
      socket.send(runEnvelope({ task_id: "provider-error-tool-task" }))

      const completed = await withTimeout(
        waitFor(
          () =>
            relay.messages.find(
              (msg) => msg.type === "task.completed" && msg.payload.task_id === "provider-error-tool-task",
            ),
          "missing tool recovery task.completed",
        ),
        25_000,
        "timed out waiting for tool recovery",
      )
      expect(completed.payload.result).toContain("tool context preserved")
      expect(await Bun.file(outputPath).text()).toBe("tool result persisted")
      const buildInputs = llm.inputs.filter((input) => !isTitleRequest(input))
      expect(buildInputs).toHaveLength(3)
      expect(JSON.stringify(buildInputs[2])).toContain("call-write")
      expect(JSON.stringify(buildInputs[2])).toContain("tool result persisted")
      expect(JSON.stringify(buildInputs[2])).toContain("do not repeat completed tool calls")
      expect(
        new Set(
          relay.messages
            .filter(
              (msg) =>
                msg.type === "task.tool_updated" &&
                msg.payload.task_id === "provider-error-tool-task" &&
                msg.payload.tool?.tool === "write",
            )
            .map((msg) => msg.payload.tool.call_id),
        ).size,
      ).toBe(1)
    } finally {
      await listener.stop(true)
      relay.stop()
      llm.stop()
    }
  })

  test("continues a length-limited response before completing", async () => {
    await using tmp = await tmpdir({ git: true })
    const llm = llmServer([textChunks("第一段未完", "length"), textChunks("，第二段完成。")])
    const relay = relayServer()
    await writeConfig(tmp.path, llm.url)
    envForRelay(relay.url, tmp.path)

    const listener = await Server.listen({ hostname: "127.0.0.1", port: 0 })
    try {
      await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "device.hello"), "missing relay hello"),
        5_000,
        "timed out waiting for relay hello",
      )
      const socket = await withTimeout(
        waitFor(() => Array.from(relay.sockets)[0], "missing relay socket"),
        5_000,
        "timed out waiting for relay socket",
      )
      socket.send(runEnvelope({ task_id: "length-task" }))

      const completed = await withTimeout(
        waitFor(
          () => relay.messages.find((msg) => msg.type === "task.completed" && msg.payload.task_id === "length-task"),
          "missing length task.completed",
        ),
        20_000,
        "timed out waiting for length continuation",
      )
      expect(completed.payload.result).toContain("第一段未完")
      expect(completed.payload.result).toContain("第二段完成")
      const buildInputs = llm.inputs.filter((input) => !isTitleRequest(input))
      expect(buildInputs).toHaveLength(2)
      expect(JSON.stringify(buildInputs[1])).toContain("Continue exactly where")
    } finally {
      await listener.stop(true)
      relay.stop()
      llm.stop()
    }
  })

  test("never completes provider errors and continues length-limited output", () => {
    const errorState = Relay.testInternals.createCompletionGuardState()
    Relay.testInternals.beginCompletionRound(errorState)
    Relay.testInternals.rememberCompletionPart(errorState, {
      id: "tool-before-error",
      type: "tool",
      tool: "bash",
      state: { status: "completed" },
    })
    Relay.testInternals.rememberCompletionModel(errorState, {
      finishReason: "error",
      finalText: "当前先处理文件，接下来继续分析。",
    })
    const errorDecision = Relay.testInternals.evaluateCompletion({
      state: errorState,
      mode: "build",
      text: "当前先处理文件，接下来继续分析。",
    })
    expect(errorDecision.type).toBe("continue")
    expect(errorDecision.reason).toBe("provider_error")

    const lengthState = Relay.testInternals.createCompletionGuardState()
    Relay.testInternals.beginCompletionRound(lengthState)
    Relay.testInternals.rememberCompletionModel(lengthState, {
      finishReason: "length",
      finalText: "部分输出",
    })
    const lengthDecision = Relay.testInternals.evaluateCompletion({
      state: lengthState,
      mode: "build",
      text: "部分输出",
    })
    expect(lengthDecision.type).toBe("continue")
    expect(lengthDecision.reason).toBe("output_length")
    expect(lengthDecision.counts.hasRoundProgress).toBe(true)
  })

  test("tracks unresolved todos and fails after two stagnant continuation rounds", () => {
    const state = Relay.testInternals.createCompletionGuardState()
    Relay.testInternals.beginCompletionRound(state)
    Relay.testInternals.rememberCompletionPart(state, {
      id: "todo-call",
      type: "tool",
      tool: "todowrite",
      state: {
        status: "completed",
        metadata: { todos: [{ id: "todo-1", content: "继续分析", status: "in_progress" }] },
      },
    })
    const first = Relay.testInternals.evaluateCompletion({ state, mode: "build", text: "正在分析。" })
    expect(first.type).toBe("continue")
    expect(first.reason).toBe("unresolved_todo")
    Relay.testInternals.incrementCompletionRetry(state, first.reason, first.counts)

    Relay.testInternals.beginCompletionRound(state)
    const second = Relay.testInternals.evaluateCompletion({ state, mode: "build", text: "仍在分析。" })
    expect(second.type).toBe("fail")
    expect(second.reason).toBe("stalled")
  })

  test("keeps a task active while an asynchronous MCP job is running", () => {
    const state = Relay.testInternals.createCompletionGuardState()
    Relay.testInternals.beginCompletionRound(state)
    Relay.testInternals.rememberCompletionPart(state, {
      id: "mcp-call",
      type: "tool",
      tool: "mcp_call",
      state: {
        status: "completed",
        metadata: {
          structuredContent: { mcp_job: { id: "mcpjob-1", status: "running" } },
        },
      },
    })
    const waiting = Relay.testInternals.evaluateCompletion({ state, mode: "build", text: "作业已启动。" })
    expect(waiting.type).toBe("continue")
    expect(waiting.reason).toBe("pending_mcp_job")

    Relay.testInternals.rememberCompletionPart(state, {
      id: "mcp-job-status",
      type: "tool",
      tool: "mcp_call",
      state: {
        status: "completed",
        metadata: {
          structuredContent: { mcp_job: { id: "mcpjob-1", status: "completed" } },
        },
      },
    })
    const completed = Relay.testInternals.evaluateCompletion({ state, mode: "build", text: "分析完成。" })
    expect(completed.type).toBe("allow")
  })

  test("uses the domain relay endpoint when no explicit relay url is provided", async () => {
    await using tmp = await tmpdir()

    const cfg = Relay.config(
      {
        OPENCODE_RELAY_OPERATOR_KEY: "operator-key",
        OPENCODE_RELAY_PROJECT_ROOT: tmp.path,
        OPENCODE_RELAY_HOSTNAME: "host-a",
      },
      { cwd: "/ignored", hostname: "ignored-host" },
    )

    expect(cfg?.url).toBe("wss://www.xyapi.top/codex/ws/device")
  })

  test("builds relay websocket config from domain env and launcher machine id", async () => {
    await using tmp = await tmpdir({
      init: async (dir) => {
        const home = path.join(dir, "home")
        await Bun.write(path.join(home, ".my-opencode-launcher", "machine_id"), "machine-from-launcher\n")
        return { home }
      },
    })

    const cfg = Relay.config(
      {
        OPENCODE_RELAY_URL: "https://www.xyapi.top",
        OPENCODE_RELAY_OPERATOR_KEY: "operator-key",
        OPENCODE_RELAY_PROJECT_ROOT: tmp.path,
        OPENCODE_RELAY_HOSTNAME: "host-a",
        OPENCODE_RELAY_TOKEN: "relay-token",
      },
      { home: tmp.extra.home, cwd: "/ignored", hostname: "ignored-host" },
    )

    expect(cfg?.url).toBe("wss://www.xyapi.top/codex/ws/device?token=relay-token")
    expect(cfg?.agent).toBe(`host-a:${path.basename(tmp.path)}`)
    expect(cfg?.machine).toBe("machine-from-launcher")
    expect(cfg?.project).toEqual({ id: path.basename(tmp.path), root: tmp.path })
  })

  test("normalizes public domain base url to relay websocket endpoint", () => {
    expect(Relay.address("https://www.xyapi.top/codex")).toBe("wss://www.xyapi.top/codex/ws/device")
    expect(Relay.address("https://www.xyapi.top/codex/")).toBe("wss://www.xyapi.top/codex/ws/device")
    expect(Relay.address("wss://www.xyapi.top/codex/ws/device")).toBe("wss://www.xyapi.top/codex/ws/device")
  })

  test("connects through Server.listen, sends device hello, heartbeats, and stops", async () => {
    await using tmp = await tmpdir()
    const relay = relayServer()
    envForRelay(relay.url, tmp.path)

    const listener = await Server.listen({ hostname: "127.0.0.1", port: 0 })
    try {
      const hello = await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "device.hello"), "missing relay hello"),
        5_000,
        "timed out waiting for relay hello",
      )
      expect(hello.payload).toMatchObject({
        operator_key: "operator-key",
        agent_id: "agent-1",
        machine_id: "machine-1",
        hostname: "host-1",
      })
      expect(hello.payload.projects).toEqual([{ project_id: "project-1", root: tmp.path }])

      const heartbeat = await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "device.heartbeat"), "missing relay heartbeat"),
        3_000,
        "timed out waiting for relay heartbeat",
      )
      expect(heartbeat.payload.agent_id).toBe("agent-1")
    } finally {
      await listener.stop(true)
      await withTimeout(
        waitFor(() => relay.sockets.size === 0 || undefined, "relay socket stayed open"),
        5_000,
        "timed out waiting for relay socket close",
      )
      relay.stop()
    }
  })

  test("runs a relay task through SessionPrompt and returns delta and completed", async () => {
    await using tmp = await tmpdir({ git: true })
    const llm = llmServer()
    const relay = relayServer()
    await writeConfig(tmp.path, llm.url)
    envForRelay(relay.url, tmp.path)

    const listener = await Server.listen({ hostname: "127.0.0.1", port: 0 })
    try {
      await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "device.hello"), "missing relay hello"),
        5_000,
        "timed out waiting for relay hello",
      )

      const socket = await withTimeout(
        waitFor(() => Array.from(relay.sockets)[0], "missing relay socket"),
        5_000,
        "timed out waiting for relay socket",
      )
      socket.send(runEnvelope({ system: "global rule\n\nproject rule" }))

      const started = await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "task.started"), "missing task.started"),
        10_000,
        "timed out waiting for task.started",
      )
      expect(started.payload.task_id).toBe("task-1")
      expect(started.payload.session_id).toBeString()

      const delta = await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "task.delta"), "missing task.delta"),
        10_000,
        "timed out waiting for task.delta",
      )
      expect(delta.payload).toMatchObject({
        task_id: "task-1",
        session_id: started.payload.session_id,
        field: "text",
      })
      expect(String(delta.payload.content)).toContain("relay")

      const completed = await withTimeout(
        waitFor(
          () => relay.messages.find((msg) => msg.type === "task.completed"),
          () =>
            `missing task.completed; relay messages: ${JSON.stringify(relay.messages)}; llm inputs: ${JSON.stringify(llm.inputs)}`,
        ),
        10_000,
        "timed out waiting for task.completed",
      )
      expect(completed.payload).toMatchObject({
        task_id: "task-1",
        session_id: started.payload.session_id,
        result: "relay done",
      })
      expect(completed.payload.usage).toMatchObject({
        stage: "request_usage_ready",
        context_tokens: expect.any(Number),
        input_tokens: expect.any(Number),
        output_tokens: expect.any(Number),
        total_tokens: expect.any(Number),
      })
      const usageProgress = relay.messages.find(
        (msg) =>
          msg.type === "task.progress" &&
          msg.payload.task_id === "task-1" &&
          msg.payload.metadata?.source === "llm_usage" &&
          typeof msg.payload.metadata?.context_tokens === "number",
      )
      expect(usageProgress?.payload.metadata).toMatchObject({
        source: "llm_usage",
        context_limit: 100000,
        context_tokens: expect.any(Number),
      })
      expect(llm.inputs.length).toBeGreaterThanOrEqual(1)
      const promptInput = llm.inputs.find((input) => !isTitleRequest(input))
      const messages = (promptInput?.messages ?? []) as Array<{ role?: string; content?: unknown }>
      const systemText = JSON.stringify(messages.filter((message) => message.role === "system"))
      const userText = JSON.stringify(messages.filter((message) => message.role === "user"))
      expect(systemText).toContain("global rule")
      expect(systemText).toContain("project rule")
      expect(userText).not.toContain("global rule")
      expect(userText).not.toContain("project rule")
      expect(userText).toContain("say relay done")
    } finally {
      await listener.stop(true)
      relay.stop()
      llm.stop()
    }
  })

  test("persists inserted task input and processes it in the next model loop", async () => {
    await using tmp = await tmpdir({ git: true })
    const llm = delayedScriptLLMServer([
      { delayMS: 350, chunks: emptyStopChunks() },
      { delayMS: 0, chunks: textChunks("injected round answer") },
    ])
    const relay = relayServer()
    await writeConfig(tmp.path, llm.url)
    envForRelay(relay.url, tmp.path)

    const listener = await Server.listen({ hostname: "127.0.0.1", port: 0 })
    try {
      await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "device.hello"), "missing relay hello"),
        5_000,
        "timed out waiting for relay hello",
      )
      const socket = await withTimeout(
        waitFor(() => Array.from(relay.sockets)[0], "missing relay socket"),
        5_000,
        "timed out waiting for relay socket",
      )
      socket.send(runEnvelope({ task_id: "insert-task" }))

      const started = await withTimeout(
        waitFor(
          () => relay.messages.find((msg) => msg.type === "task.started" && msg.payload.task_id === "insert-task"),
          "missing inserted task start",
        ),
        10_000,
        "timed out waiting for inserted task start",
      )
      await withTimeout(
        waitFor(() => llm.inputs.find((input) => !isTitleRequest(input)), "missing initial model request"),
        10_000,
        "timed out waiting for initial model request",
      )

      socket.send(
        serverEnvelope(
          "task.input",
          {
            task_id: "insert-task",
            queue_item_id: "queue-1",
            injection_version: 42,
            parts: [{ type: "text", text: "insert this while running" }],
            metadata: { model: "test/test-model", agent: "build", variant: "high" },
          },
          "input-request-1",
        ),
      )

      const acknowledged = await withTimeout(
        waitFor(
          () =>
            relay.messages.find(
              (msg) =>
                msg.type === "task.input_ack" &&
                msg.payload.task_id === "insert-task" &&
                msg.payload.queue_item_id === "queue-1",
            ),
          () => `missing input acknowledgement; relay messages: ${JSON.stringify(relay.messages)}`,
        ),
        10_000,
        "timed out waiting for input acknowledgement",
      )
      expect(acknowledged.request_id).toBe("input-request-1")
      expect(acknowledged.payload).toMatchObject({
        task_id: "insert-task",
        queue_item_id: "queue-1",
        injection_version: 42,
        accepted: true,
      })
      expect(
        relay.messages.find((msg) => msg.type === "task.completed" && msg.payload.task_id === "insert-task"),
      ).toBeUndefined()

      const completed = await withTimeout(
        waitFor(
          () => relay.messages.find((msg) => msg.type === "task.completed" && msg.payload.task_id === "insert-task"),
          () =>
            `missing inserted task completion; relay messages: ${JSON.stringify(relay.messages)}; llm inputs: ${JSON.stringify(llm.inputs)}`,
          20_000,
        ),
        25_000,
        "timed out waiting for inserted task completion",
      )
      expect(completed.payload).toMatchObject({
        task_id: "insert-task",
        session_id: started.payload.session_id,
        result: "injected round answer",
        round_result: "injected round answer",
      })

      const taskInputs = llm.inputs.filter((input) => !isTitleRequest(input))
      expect(taskInputs.length).toBe(2)
      expect(JSON.stringify(taskInputs[0])).not.toContain("insert this while running")
      expect(JSON.stringify(taskInputs[1])).toContain("insert this while running")
      expect(
        relay.messages.find((msg) => msg.type === "task.failed" && msg.payload.task_id === "insert-task"),
      ).toBeUndefined()
    } finally {
      await listener.stop(true)
      relay.stop()
      llm.stop()
    }
  })

  test("allows plan agent text-only promise completion", async () => {
    await using tmp = await tmpdir({ git: true })
    const llm = llmServer([textChunks("接下来我会整理方案。")])
    const relay = relayServer()
    await writeConfig(tmp.path, llm.url)
    envForRelay(relay.url, tmp.path)

    const listener = await Server.listen({ hostname: "127.0.0.1", port: 0 })
    try {
      await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "device.hello"), "missing relay hello"),
        5_000,
        "timed out waiting for relay hello",
      )

      const socket = await withTimeout(
        waitFor(() => Array.from(relay.sockets)[0], "missing relay socket"),
        5_000,
        "timed out waiting for relay socket",
      )
      socket.send(
        runEnvelope({
          task_id: "plan-text-task",
          metadata: { agent: "plan", model: "test/test-model" },
        }),
      )

      const completed = await withTimeout(
        waitFor(
          () => relay.messages.find((msg) => msg.type === "task.completed" && msg.payload.task_id === "plan-text-task"),
          () => `missing plan task.completed; relay messages: ${JSON.stringify(relay.messages)}`,
          20_000,
        ),
        25_000,
        "timed out waiting for plan task.completed",
      )
      expect(completed.payload.result).toBe("接下来我会整理方案。")
      expect(
        relay.messages.find((msg) => msg.type === "task.failed" && msg.payload.task_id === "plan-text-task"),
      ).toBeUndefined()
    } finally {
      await listener.stop(true)
      relay.stop()
      llm.stop()
    }
  })

  test("build promise without action auto-continues and completes after a real tool action", async () => {
    await using tmp = await tmpdir({ git: true })
    const guardPath = path.join(tmp.path, "guard-ok.txt")
    const llm = llmServer([
      textChunks("接下来我会运行命令验证。"),
      toolChunks("write", { filePath: guardPath, content: "guard-ok" }),
      textChunks("guard-ok done"),
    ])
    const relay = relayServer()
    await writeRelayConfig(tmp.path, llm.url, {
      permission: { edit: "allow" },
    })
    envForRelay(relay.url, tmp.path)

    const listener = await Server.listen({ hostname: "127.0.0.1", port: 0 })
    try {
      await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "device.hello"), "missing relay hello"),
        5_000,
        "timed out waiting for relay hello",
      )

      const socket = await withTimeout(
        waitFor(() => Array.from(relay.sockets)[0], "missing relay socket"),
        5_000,
        "timed out waiting for relay socket",
      )
      socket.send(runEnvelope({ task_id: "build-guard-retry-task" }))

      const guardProgress = await withTimeout(
        waitFor(
          () =>
            relay.messages.find(
              (msg) =>
                msg.type === "task.progress" &&
                msg.payload.task_id === "build-guard-retry-task" &&
                msg.payload.metadata?.source === "completion_guard",
            ),
          () => `missing completion guard progress; relay messages: ${JSON.stringify(relay.messages)}`,
          20_000,
        ),
        25_000,
        "timed out waiting for completion guard progress",
      )
      expect(guardProgress.payload.metadata.reason).toBe("promise_without_action")
      expect(guardProgress.payload.metadata).toMatchObject({
        source: "completion_guard",
        decision: "continue",
        mode: "build",
        retry_count: 0,
      })
      expect(guardProgress.payload.metadata.action_state).toEqual(
        expect.objectContaining({
          toolCount: expect.any(Number),
          hasRealAction: expect.any(Boolean),
          hasRoundProgress: expect.any(Boolean),
        }),
      )
      expect(guardProgress.payload.metadata.final_text_preview).toBeUndefined()
      expect(guardProgress.payload.metadata.text_signal).toBeUndefined()
      expect(guardProgress.payload.metadata.usage).toBeUndefined()

      const completed = await withTimeout(
        waitFor(
          () =>
            relay.messages.find(
              (msg) => msg.type === "task.completed" && msg.payload.task_id === "build-guard-retry-task",
            ),
          () => `missing guarded task.completed; relay messages: ${JSON.stringify(relay.messages)}`,
          30_000,
        ),
        35_000,
        "timed out waiting for guarded task.completed",
      )
      expect(completed.payload.result).toContain("guard-ok")
      expect(
        relay.messages.some(
          (msg) => msg.type === "task.tool_updated" && msg.payload.task_id === "build-guard-retry-task",
        ),
      ).toBe(true)
      const buildInputs = llm.inputs.filter((input) => !isTitleRequest(input))
      expect(buildInputs.length).toBeGreaterThanOrEqual(2)
    } finally {
      await listener.stop(true)
      relay.stop()
      llm.stop()
    }
  })

  test("build fails after auto-continue still produces no real action", async () => {
    await using tmp = await tmpdir({ git: true })
    const llm = llmServer([textChunks("接下来我会运行命令验证。"), textChunks("已经完成了。")])
    const relay = relayServer()
    await writeConfig(tmp.path, llm.url)
    envForRelay(relay.url, tmp.path)

    const listener = await Server.listen({ hostname: "127.0.0.1", port: 0 })
    try {
      await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "device.hello"), "missing relay hello"),
        5_000,
        "timed out waiting for relay hello",
      )

      const socket = await withTimeout(
        waitFor(() => Array.from(relay.sockets)[0], "missing relay socket"),
        5_000,
        "timed out waiting for relay socket",
      )
      socket.send(runEnvelope({ task_id: "build-guard-failed-task" }))

      const terminal = await withTimeout(
        waitFor(
          () =>
            relay.messages.find(
              (msg) =>
                (msg.type === "task.failed" || msg.type === "task.completed") &&
                msg.payload.task_id === "build-guard-failed-task",
            ),
          () => `missing guarded terminal event; relay messages: ${JSON.stringify(relay.messages)}`,
          30_000,
        ),
        35_000,
        "timed out waiting for guarded terminal event",
      )
      if (terminal.type === "task.failed") {
        expect(terminal.payload.error).toBe("Build 模式连续两轮没有产生新进度")
        expect(terminal.payload.error_detail).toContain("stalled")
        expect(terminal.payload.error_detail).toContain("retry_count")
        expect(terminal.payload.error_detail).toContain("final_text_length")
        expect(terminal.payload.error_detail).toContain("usage")
      } else {
        expect(terminal.payload.result).toBeTruthy()
      }
    } finally {
      await listener.stop(true)
      relay.stop()
      llm.stop()
    }
  })

  test("build with a real tool action can complete without guard retry", async () => {
    await using tmp = await tmpdir({ git: true })
    const directPath = path.join(tmp.path, "direct-ok.txt")
    const llm = llmServer([
      toolChunks("write", { filePath: directPath, content: "direct-ok" }),
      textChunks("direct-ok done"),
    ])
    const relay = relayServer()
    await writeRelayConfig(tmp.path, llm.url, {
      permission: { edit: "allow" },
    })
    envForRelay(relay.url, tmp.path)

    const listener = await Server.listen({ hostname: "127.0.0.1", port: 0 })
    try {
      await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "device.hello"), "missing relay hello"),
        5_000,
        "timed out waiting for relay hello",
      )

      const socket = await withTimeout(
        waitFor(() => Array.from(relay.sockets)[0], "missing relay socket"),
        5_000,
        "timed out waiting for relay socket",
      )
      socket.send(runEnvelope({ task_id: "build-tool-completed-task" }))

      const completed = await withTimeout(
        waitFor(
          () =>
            relay.messages.find(
              (msg) => msg.type === "task.completed" && msg.payload.task_id === "build-tool-completed-task",
            ),
          () => `missing tool task.completed; relay messages: ${JSON.stringify(relay.messages)}`,
          30_000,
        ),
        35_000,
        "timed out waiting for tool task.completed",
      )
      expect(completed.payload.result).toContain("direct-ok")
      expect(
        relay.messages.find(
          (msg) =>
            msg.type === "task.progress" &&
            msg.payload.task_id === "build-tool-completed-task" &&
            msg.payload.metadata?.source === "completion_guard",
        ),
      ).toBeUndefined()
    } finally {
      await listener.stop(true)
      relay.stop()
      llm.stop()
    }
  })

  test("relays LLM latency stages as task progress", async () => {
    await using tmp = await tmpdir({ git: true })
    const llm = delayedLLMServer(250)
    const relay = relayServer()
    await writeConfig(tmp.path, llm.url)
    envForRelay(relay.url, tmp.path)

    const listener = await Server.listen({ hostname: "127.0.0.1", port: 0 })
    try {
      await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "device.hello"), "missing relay hello"),
        5_000,
        "timed out waiting for relay hello",
      )

      const socket = await withTimeout(
        waitFor(() => Array.from(relay.sockets)[0], "missing relay socket"),
        5_000,
        "timed out waiting for relay socket",
      )
      socket.send(runEnvelope({ task_id: "llm-progress-task" }))

      const started = await withTimeout(
        waitFor(
          () =>
            relay.messages.find((msg) => msg.type === "task.started" && msg.payload.task_id === "llm-progress-task"),
          "missing llm progress task.started",
          25_000,
        ),
        25_000,
        "timed out waiting for llm progress task.started",
      )

      GlobalBus.emit("event", {
        payload: {
          type: "opencode.llm.latency",
          component: "llm",
          stage: "request_timeout_config_ready",
          session_id: started.payload.session_id,
          provider_id: "test",
          model_id: "test-model",
          attempt: 2,
          max_attempts: 5,
          elapsed_ms: 12,
          first_event_timeout_ms: 60_000,
          request_timeout_ms: 300_000,
          chunk_timeout_ms: 180_000,
        },
      })

      const progress = await withTimeout(
        waitFor(
          () =>
            relay.messages.find(
              (msg) =>
                msg.type === "task.progress" &&
                msg.payload.task_id === "llm-progress-task" &&
                msg.payload.metadata?.source === "llm_latency",
            ),
          () => `missing llm progress; relay messages: ${JSON.stringify(relay.messages)}`,
        ),
        10_000,
        "timed out waiting for llm progress",
      )
      expect(progress.payload).toMatchObject({
        task_id: "llm-progress-task",
        session_id: started.payload.session_id,
        message: "正在请求模型，第 2/5 次，等待首包中",
        metadata: {
          source: "llm_latency",
          stage: "request_timeout_config_ready",
          provider_id: "test",
          model_id: "test-model",
          attempt: 2,
          max_attempts: 5,
          first_event_timeout_ms: 60_000,
          request_timeout_ms: 300_000,
          chunk_timeout_ms: 180_000,
        },
      })

      expect(llm.inputs.length).toBeGreaterThanOrEqual(0)
    } finally {
      await listener.stop(true)
      relay.stop()
      llm.stop()
    }
  }, 45_000)

  test("persists one subagent completion and injects it into the active parent task", async () => {
    await using tmp = await tmpdir({ git: true })
    const llm = delayedLLMServer(800)
    const relay = relayServer()
    await writeConfig(tmp.path, llm.url)
    envForRelay(relay.url, tmp.path)

    const listener = await Server.listen({ hostname: "127.0.0.1", port: 0 })
    try {
      await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "device.hello"), "missing relay hello"),
        5_000,
        "timed out waiting for relay hello",
      )
      const socket = await withTimeout(
        waitFor(() => Array.from(relay.sockets)[0], "missing relay socket"),
        5_000,
        "timed out waiting for relay socket",
      )
      socket.send(runEnvelope({ task_id: "subagent-context-task" }))
      const started = await withTimeout(
        waitFor(
          () =>
            relay.messages.find(
              (msg) => msg.type === "task.started" && msg.payload.task_id === "subagent-context-task",
            ),
          "missing task start",
        ),
        25_000,
        "timed out waiting for task start",
      )
      const properties = {
        parentSessionID: started.payload.session_id,
        sessionID: "ses_child_subagent",
        subagentType: "repo-explorer",
        resolvedAgent: "explore",
        title: "Inspect repository",
        background: true,
        handledByRelay: false,
      }
      GlobalBus.emit("event", { directory: tmp.path, payload: { type: "opencode.subagent.started", properties } })
      await withTimeout(
        waitFor(
          () =>
            relay.messages.find(
              (msg) => msg.type === "task.subagent_started" && msg.payload.node_id === properties.sessionID,
            ),
          "missing subagent started",
        ),
        5_000,
        "timed out waiting for subagent started",
      )
      const completion = {
        ...properties,
        status: "completed" as const,
        output: "located the call graph",
        completedAt: 1_700_000_000_000,
      }
      GlobalBus.emit("event", {
        directory: tmp.path,
        payload: { type: "opencode.subagent.completed", properties: completion },
      })
      GlobalBus.emit("event", {
        directory: tmp.path,
        payload: { type: "opencode.subagent.completed", properties: completion },
      })
      const foreground = {
        ...properties,
        sessionID: "ses_child_foreground",
        background: false,
        title: "Review evidence",
      }
      GlobalBus.emit("event", {
        directory: tmp.path,
        payload: { type: "opencode.subagent.started", properties: foreground },
      })
      GlobalBus.emit("event", {
        directory: tmp.path,
        payload: {
          type: "opencode.subagent.completed",
          properties: { ...foreground, status: "completed", output: "reviewed", completedAt: 1_700_000_000_001 },
        },
      })
      await withTimeout(
        waitFor(
          () =>
            relay.messages.find(
              (msg) => msg.type === "task.subagent_result" && msg.payload.node_id === properties.sessionID,
            ),
          "missing subagent result",
        ),
        10_000,
        "timed out waiting for subagent result",
      )
      const applied = relay.messages.filter(
        (msg) => msg.type === "task.input_applied" && msg.payload.metadata?.context_type === "subagentResult",
      )
      expect(applied).toHaveLength(1)
      expect(applied[0]?.payload.content).toContain("<subagent_result>")
      expect(relay.messages.filter((msg) => msg.type === "task.subagent_result")).toHaveLength(2)
    } finally {
      await listener.stop(true)
      relay.stop()
      llm.stop()
    }
  }, 45_000)

  test("runs an orchestration plan through Relay with stable node state and pause control", async () => {
    await using tmp = await tmpdir({ git: true })
    const llm = delayedLLMServer(1_500)
    const relay = relayServer()
    await writeConfig(tmp.path, llm.url)
    envForRelay(relay.url, tmp.path)
    const listener = await Server.listen({ hostname: "127.0.0.1", port: 0 })
    try {
      await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "device.hello"), "missing relay hello"),
        5_000,
        "timed out waiting for relay hello",
      )
      const socket = await withTimeout(
        waitFor(() => Array.from(relay.sockets)[0], "missing relay socket"),
        5_000,
        "timed out waiting for relay socket",
      )
      socket.send(runEnvelope({ task_id: "orchestration-relay-task" }))
      const started = await withTimeout(
        waitFor(
          () =>
            relay.messages.find(
              (msg) => msg.type === "task.started" && msg.payload.task_id === "orchestration-relay-task",
            ),
          "missing parent start",
        ),
        30_000,
        "timed out waiting for parent start",
      )
      GlobalBus.emit("event", {
        directory: tmp.path,
        payload: {
          type: "opencode.orchestration.plan",
          properties: {
            parentSessionID: started.payload.session_id,
            plan: {
              id: "relay-plan",
              semanticAgentID: "coding-assistant",
              nodes: [{ id: "inspect", role: "repo-explorer", prompt: "Inspect the repository", timeoutMS: 10_000 }],
            },
          },
        },
      })
      await withTimeout(
        waitFor(
          () => relay.messages.find((msg) => msg.type === "task.subagent_started" && msg.payload.node_id === "inspect"),
          "missing orchestration start",
        ),
        10_000,
        "timed out waiting for orchestration start",
      )
      await withTimeout(
        waitFor(
          () =>
            relay.messages.find(
              (msg) =>
                msg.type === "task.subagent_state" &&
                msg.payload.node_id === "inspect" &&
                msg.payload.state === "running",
            ),
          "missing running node state",
        ),
        10_000,
        "timed out waiting for running node state",
      )
      socket.send(
        serverEnvelope("task.subagent_control", {
          task_id: "orchestration-relay-task",
          node_id: "inspect",
          action: "pause",
        }),
      )
      await withTimeout(
        waitFor(
          () =>
            relay.messages.find(
              (msg) =>
                msg.type === "task.subagent_control_applied" &&
                msg.payload.node_id === "inspect" &&
                msg.payload.applied === true,
            ),
          "missing pause acknowledgement",
        ),
        10_000,
        "timed out waiting for pause acknowledgement",
      )
      expect(
        relay.messages.some(
          (msg) =>
            msg.type === "task.subagent_state" && msg.payload.node_id === "inspect" && msg.payload.state === "paused",
        ),
      ).toBe(true)
    } finally {
      relay.stop()
      llm.stop()
      await listener.stop(true)
    }
  }, 45_000)

  test("collects artifacts produced in an orchestrated child directory", async () => {
    await using tmp = await tmpdir({ git: true })
    const llm = delayedLLMServer(100)
    const relay = relayServer()
    await writeConfig(tmp.path, llm.url)
    envForRelay(relay.url, tmp.path)
    const listener = await Server.listen({ hostname: "127.0.0.1", port: 0 })
    try {
      await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "device.hello"), "missing relay hello"),
        5_000,
        "timed out waiting for relay hello",
      )
      const socket = await withTimeout(
        waitFor(() => Array.from(relay.sockets)[0], "missing relay socket"),
        5_000,
        "timed out waiting for relay socket",
      )
      socket.send(runEnvelope({ task_id: "orchestration-artifact-task" }))
      const started = await withTimeout(
        waitFor(
          () =>
            relay.messages.find(
              (msg) => msg.type === "task.started" && msg.payload.task_id === "orchestration-artifact-task",
            ),
          "missing parent start",
        ),
        30_000,
        "timed out waiting for parent start",
      )
      GlobalBus.emit("event", {
        directory: tmp.path,
        payload: {
          type: "opencode.orchestration.plan",
          properties: {
            parentSessionID: started.payload.session_id,
            plan: {
              id: "artifact-plan",
              semanticAgentID: "coding-assistant",
              nodes: [{ id: "inspect", role: "repo-explorer", prompt: "Inspect the repository", timeoutMS: 10_000 }],
            },
          },
        },
      })
      await withTimeout(
        waitFor(
          () => relay.messages.find((msg) => msg.type === "task.subagent_started" && msg.payload.node_id === "inspect"),
          "missing child start",
        ),
        10_000,
        "timed out waiting for child start",
      )
      const artifactPath = path.join(
        tmp.path,
        ".chatcodex-artifacts",
        "orchestration-artifact-task",
        "subagents",
        "inspect",
        "inspection.txt",
      )
      await mkdir(path.dirname(artifactPath), { recursive: true })
      await writeFile(artifactPath, "child evidence")

      const result = await withTimeout(
        waitFor(
          () => relay.messages.find((msg) => msg.type === "task.subagent_result" && msg.payload.node_id === "inspect"),
          () => `missing child artifact result: ${JSON.stringify(relay.messages)}`,
          20_000,
        ),
        25_000,
        "timed out waiting for child artifact result",
      )
      expect(result.payload.artifacts).toEqual([
        expect.objectContaining({
          filename: "inspection.txt",
          relative_path: ".chatcodex-artifacts/orchestration-artifact-task/subagents/inspect/inspection.txt",
          size_bytes: "child evidence".length,
        }),
      ])
    } finally {
      relay.stop()
      llm.stop()
      await listener.stop(true)
    }
  }, 45_000)

  test("fails cleanly when relay task stops with empty output", async () => {
    await using tmp = await tmpdir({ git: true })
    const llm = llmServer([emptyStopChunks(), textChunks("retry final answer")])
    const relay = relayServer()
    await writeConfig(tmp.path, llm.url)
    envForRelay(relay.url, tmp.path)

    const listener = await Server.listen({ hostname: "127.0.0.1", port: 0 })
    try {
      await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "device.hello"), "missing relay hello"),
        5_000,
        "timed out waiting for relay hello",
      )

      const socket = await withTimeout(
        waitFor(() => Array.from(relay.sockets)[0], "missing relay socket"),
        5_000,
        "timed out waiting for relay socket",
      )
      socket.send(runEnvelope({ task_id: "empty-retry-task" }))

      await withTimeout(
        waitFor(
          () => relay.messages.find((msg) => msg.type === "task.started" && msg.payload.task_id === "empty-retry-task"),
          "missing empty retry task.started",
          30_000,
        ),
        30_000,
        "timed out waiting for empty retry task.started",
      )

      const failed = await withTimeout(
        waitFor(
          () => relay.messages.find((msg) => msg.type === "task.failed" && msg.payload.task_id === "empty-retry-task"),
          () =>
            `missing task.failed; relay messages: ${JSON.stringify(relay.messages)}; llm inputs: ${JSON.stringify(llm.inputs)}`,
          30_000,
        ),
        30_000,
        "timed out waiting for empty output task.failed",
      )
      expect(failed.payload.error).toBe("模型未返回正文，本轮未产生可展示输出，请重试或切换供应商")
      expect(
        relay.messages.some((msg) => msg.type === "task.completed" && msg.payload.task_id === "empty-retry-task"),
      ).toBe(false)
      expect(JSON.stringify(llm.inputs)).not.toContain("上一轮模型响应正常结束")
    } finally {
      await listener.stop(true)
      relay.stop()
      llm.stop()
    }
  })

  test("fails when relay task has empty output", async () => {
    await using tmp = await tmpdir({ git: true })
    const llm = llmServer([emptyStopChunks(), emptyStopChunks()])
    const relay = relayServer()
    await writeConfig(tmp.path, llm.url)
    envForRelay(relay.url, tmp.path)

    const listener = await Server.listen({ hostname: "127.0.0.1", port: 0 })
    try {
      await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "device.hello"), "missing relay hello"),
        5_000,
        "timed out waiting for relay hello",
      )

      const socket = await withTimeout(
        waitFor(() => Array.from(relay.sockets)[0], "missing relay socket"),
        5_000,
        "timed out waiting for relay socket",
      )
      socket.send(runEnvelope({ task_id: "empty-failed-task" }))

      await withTimeout(
        waitFor(
          () =>
            relay.messages.find((msg) => msg.type === "task.started" && msg.payload.task_id === "empty-failed-task"),
          "missing empty failed task.started",
          30_000,
        ),
        30_000,
        "timed out waiting for empty failed task.started",
      )

      const failed = await withTimeout(
        waitFor(
          () => relay.messages.find((msg) => msg.type === "task.failed" && msg.payload.task_id === "empty-failed-task"),
          () =>
            `missing task.failed; relay messages: ${JSON.stringify(relay.messages)}; llm inputs: ${JSON.stringify(llm.inputs)}`,
          30_000,
        ),
        30_000,
        "timed out waiting for empty retry task.failed",
      )
      expect(failed.payload.error).toBe("模型未返回正文，本轮未产生可展示输出，请重试或切换供应商")
      expect(
        relay.messages.some((msg) => msg.type === "task.completed" && msg.payload.task_id === "empty-failed-task"),
      ).toBe(false)
    } finally {
      await listener.stop(true)
      relay.stop()
      llm.stop()
    }
  })

  test("optimizes goal prompts through a temporary relay session", async () => {
    await using tmp = await tmpdir({ git: true })
    const llm = llmServer([
      textChunks("```markdown\n优化后的目标：完成 Goal 保存流程，并验证后端接口和前端交互。\n```"),
    ])
    const relay = relayServer()
    await writeConfig(tmp.path, llm.url)
    envForRelay(relay.url, tmp.path)

    const listener = await Server.listen({ hostname: "127.0.0.1", port: 0 })
    try {
      await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "device.hello"), "missing relay hello"),
        5_000,
        "timed out waiting for relay hello",
      )

      const socket = await withTimeout(
        waitFor(() => Array.from(relay.sockets)[0], "missing relay socket"),
        5_000,
        "timed out waiting for relay socket",
      )
      socket.send(
        serverEnvelope(
          "goal.optimize",
          {
            agent_id: "agent-1",
            machine_id: "machine-1",
            project_id: "project-1",
            goal: "修一下 Goal 保存",
            provider_id: "test",
            model_id: "test-model",
            max_iterations: 7,
          },
          "goal-optimize-1",
        ),
      )

      const result = await withTimeout(
        waitFor(
          () =>
            relay.messages.find((msg) => msg.type === "goal.optimize.result" && msg.request_id === "goal-optimize-1"),
          () => `missing goal.optimize.result; relay messages: ${JSON.stringify(relay.messages)}`,
          30_000,
        ),
        30_000,
        "timed out waiting for goal.optimize.result",
      )
      expect(result.payload).toMatchObject({
        success: true,
        original_goal: "修一下 Goal 保存",
        optimized_goal: "完成 Goal 保存流程，并验证后端接口和前端交互。",
      })
      expect(relay.messages.some((msg) => msg.type === "task.started")).toBe(false)
      expect(JSON.stringify(llm.inputs)).toContain("最大轮次：7")
      expect(JSON.stringify(llm.inputs)).toContain("修一下 Goal 保存")
    } finally {
      await listener.stop(true)
      relay.stop()
      llm.stop()
    }
  })

  test("continues relay goal tasks until goal_complete is called", async () => {
    await using tmp = await tmpdir({ git: true })
    const llm = llmServer([
      textChunks("first step done"),
      toolChunks("goal_complete", { summary: "goal done" }),
      textChunks("goal done"),
      jsonText({ completed: true, reason: "目标已完成", missing_items: [], next_instruction: "" }),
    ])
    const relay = relayServer()
    await writeRelayConfig(tmp.path, llm.url, {})
    envForRelay(relay.url, tmp.path)

    const listener = await Server.listen({ hostname: "127.0.0.1", port: 0 })
    try {
      await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "device.hello"), "missing relay hello"),
        5_000,
        "timed out waiting for relay hello",
      )

      const socket = await withTimeout(
        waitFor(() => Array.from(relay.sockets)[0], "missing relay socket"),
        5_000,
        "timed out waiting for relay socket",
      )
      socket.send(
        runEnvelope({
          task_id: "goal-task",
          parts: [{ type: "text", text: "start goal" }],
          metadata: {
            goal: "完成 goal relay 测试",
            goal_id: "goal-test",
            goal_max_iterations: "3",
          },
        }),
      )

      const created = await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "task.goal_created"), "missing goal created"),
        10_000,
        "timed out waiting for goal created",
      )
      expect(created.payload).toMatchObject({
        task_id: "goal-task",
        goal_id: "goal-test",
        objective: "完成 goal relay 测试",
        status: "active",
        iteration: 1,
        max: 3,
      })
      const progressEvents = relay.messages.filter(
        (msg) => msg.type === "task.progress" && msg.payload.task_id === "goal-task",
      )
      expect(progressEvents.length).toBe(1)
      expect(progressEvents[0]?.payload).toMatchObject({
        task_id: "goal-task",
        session_id: created.payload.session_id,
        message: "目标状态更新：created",
        metadata: {
          goal_id: "goal-test",
          goal_status: "active",
          goal_iteration: 1,
        },
      })
      expect(
        relay.messages.some((msg) => msg.type === "task.progress" && msg.payload.message === "任务仍在执行中"),
      ).toBe(false)

      const continued = await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "task.goal_continued"), "missing goal continued"),
        10_000,
        "timed out waiting for goal continued",
      )
      expect(continued.payload).toMatchObject({
        task_id: "goal-task",
        goal_id: "goal-test",
        iteration: 2,
      })

      const checkpoint = await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "task.goal_checkpoint"), "missing goal checkpoint"),
        10_000,
        "timed out waiting for goal checkpoint",
      )
      expect(checkpoint.payload).toMatchObject({
        task_id: "goal-task",
        goal_id: "goal-test",
        status: "active",
      })
      expect(String(checkpoint.payload.metadata?.checkpoint)).toContain("first step done")

      const completedGoal = await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "task.goal_completed"), "missing goal completed"),
        10_000,
        "timed out waiting for goal completed",
      )
      expect(completedGoal.payload).toMatchObject({
        task_id: "goal-task",
        goal_id: "goal-test",
        status: "completed",
      })
      expect(String(completedGoal.payload.metadata?.summary)).toContain("goal done")

      const completed = await withTimeout(
        waitFor(
          () => relay.messages.find((msg) => msg.type === "task.completed"),
          () =>
            `missing task.completed; relay messages: ${JSON.stringify(relay.messages)}; llm inputs: ${JSON.stringify(llm.inputs)}`,
        ),
        10_000,
        "timed out waiting for task.completed",
      )
      expect(completed.payload).toMatchObject({
        task_id: "goal-task",
      })
      const visibleDeltas = relay.messages
        .filter((msg) => msg.type === "task.delta" && msg.payload.task_id === "goal-task")
        .map((msg) => String(msg.payload.content))
        .join("")
      expect(visibleDeltas).toContain("first step done")
      expect(visibleDeltas).toContain("goal done")
      expect(visibleDeltas.match(/first step done/g)?.length ?? 0).toBe(1)
      expect(visibleDeltas.match(/goal done/g)?.length ?? 0).toBe(1)
      expect(JSON.stringify(llm.inputs)).toContain('iteration=\\"1\\"')
      expect(JSON.stringify(llm.inputs)).toContain('iteration=\\"2\\"')
      expect(JSON.stringify(llm.inputs)).toContain("隐藏的 Goal 完成审查器")
      expect(llm.inputs.length).toBeGreaterThanOrEqual(2)
    } finally {
      await listener.stop(true)
      relay.stop()
      llm.stop()
    }
  })

  test("keeps relay goal active when hidden completion review rejects goal_complete", async () => {
    await using tmp = await tmpdir({ git: true })
    const llm = llmServer([
      toolChunks("goal_complete", { summary: "还有接口未完全测试，下一步继续补齐" }),
      textChunks("还有接口未完全测试，下一步继续补齐"),
      jsonText({
        completed: false,
        reason: "仍有接口未完成真实测试",
        missing_items: ["评论接口真实测试", "点赞接口真实测试"],
        next_instruction: "继续补齐评论和点赞接口的参数来源、实现和真实 API 测试。",
      }),
      toolChunks("goal_complete", { summary: "所有接口均已实现并完成真实测试" }),
      textChunks("所有接口均已实现并完成真实测试"),
      jsonText({
        completed: true,
        reason: "目标已完成",
        missing_items: [],
        next_instruction: "",
      }),
    ])
    const relay = relayServer()
    await writeRelayConfig(tmp.path, llm.url, {})
    envForRelay(relay.url, tmp.path)

    const listener = await Server.listen({ hostname: "127.0.0.1", port: 0 })
    try {
      await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "device.hello"), "missing relay hello"),
        5_000,
        "timed out waiting for relay hello",
      )

      const socket = await withTimeout(
        waitFor(() => Array.from(relay.sockets)[0], "missing relay socket"),
        5_000,
        "timed out waiting for relay socket",
      )
      socket.send(
        runEnvelope({
          task_id: "goal-review-task",
          parts: [{ type: "text", text: "start reviewed goal" }],
          metadata: {
            goal: "实现并测试评论和点赞接口",
            goal_id: "goal-review-test",
            goal_max_iterations: "4",
          },
        }),
      )

      await withTimeout(
        waitFor(
          () =>
            relay.messages.find((msg) => msg.type === "task.completed" && msg.payload.task_id === "goal-review-task"),
          () =>
            `missing task.completed; relay messages: ${JSON.stringify(relay.messages)}; llm inputs: ${JSON.stringify(llm.inputs)}`,
          35_000,
        ),
        35_000,
        "timed out waiting for reviewed goal task.completed",
      )

      const continued = relay.messages.find(
        (msg) => msg.type === "task.goal_continued" && msg.payload.task_id === "goal-review-task",
      )
      expect(continued?.payload).toMatchObject({
        task_id: "goal-review-task",
        iteration: 2,
      })
      const completed = relay.messages.filter(
        (msg) => msg.type === "task.goal_completed" && msg.payload.task_id === "goal-review-task",
      )
      expect(completed.length).toBe(1)
      expect(String(completed[0]?.payload.metadata?.summary)).toContain("所有接口均已实现")
      const inputs = JSON.stringify(llm.inputs)
      expect(inputs).toContain("隐藏的 Goal 完成审查器")
      expect(inputs).toContain("完成审查未通过")
      expect(inputs).toContain("继续补齐评论和点赞接口")
      expect(inputs).not.toContain("task.goal_completed")
    } finally {
      await listener.stop(true)
      relay.stop()
      llm.stop()
    }
  })

  test("goal max iterations defaults to 30 and caps user value", () => {
    expect(Relay.testInternals.parseGoalMaxIterations()).toBe(30)
    expect(Relay.testInternals.parseGoalMaxIterations("")).toBe(30)
    expect(Relay.testInternals.parseGoalMaxIterations("0")).toBe(30)
    expect(Relay.testInternals.parseGoalMaxIterations("12")).toBe(12)
    expect(Relay.testInternals.parseGoalMaxIterations("150")).toBe(100)
    expect(
      Relay.testInternals.relayGoal({
        task_id: "resumed-goal-task",
        project_id: "project-1",
        resume: true,
        metadata: {
          goal: "继续恢复目标",
          goal_iteration: "7",
          goal_max_iterations: "12",
        },
      })?.iteration,
    ).toBe(7)
  })

  test("relays think-tag reasoning while running goal tasks", async () => {
    await using tmp = await tmpdir({ git: true })
    const llm = llmServer([
      textChunks("<think>goal reasoning</think>goal text"),
      toolChunks("goal_complete", { summary: "goal done" }),
      textChunks("goal done"),
      jsonText({ completed: true, reason: "目标已完成", missing_items: [], next_instruction: "" }),
    ])
    const relay = relayServer()
    await writeRelayConfig(tmp.path, llm.url, {})
    envForRelay(relay.url, tmp.path)

    const listener = await Server.listen({ hostname: "127.0.0.1", port: 0 })
    try {
      await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "device.hello"), "missing relay hello"),
        5_000,
        "timed out waiting for relay hello",
      )

      const socket = await withTimeout(
        waitFor(() => Array.from(relay.sockets)[0], "missing relay socket"),
        5_000,
        "timed out waiting for relay socket",
      )
      socket.send(
        runEnvelope({
          task_id: "goal-reasoning-task",
          parts: [{ type: "text", text: "start reasoning goal" }],
          metadata: {
            goal: "完成 goal reasoning relay 测试",
            goal_id: "goal-reasoning-test",
            goal_max_iterations: "3",
          },
        }),
      )

      await withTimeout(
        waitFor(
          () =>
            relay.messages.find(
              (msg) => msg.type === "task.completed" && msg.payload.task_id === "goal-reasoning-task",
            ),
          "missing task.completed",
          35_000,
        ),
        35_000,
        "timed out waiting for task.completed",
      )

      const reasoningDeltas = relay.messages
        .filter(
          (msg) =>
            msg.type === "task.delta" &&
            msg.payload.task_id === "goal-reasoning-task" &&
            msg.payload.field === "reasoning",
        )
        .map((msg) => String(msg.payload.content))
        .join("")
      const textDeltas = relay.messages
        .filter(
          (msg) =>
            msg.type === "task.delta" && msg.payload.task_id === "goal-reasoning-task" && msg.payload.field === "text",
        )
        .map((msg) => String(msg.payload.content))
        .join("")
      expect(reasoningDeltas).toContain("goal reasoning")
      expect(textDeltas).toContain("goal text")
      expect(textDeltas.match(/goal text/g)?.length ?? 0).toBe(1)
    } finally {
      await listener.stop(true)
      relay.stop()
      llm.stop()
    }
  })

  test("reports invalid model key for relay API auth failures", async () => {
    await using tmp = await tmpdir({ git: true })
    const llm = invalidKeyLLMServer()
    const relay = relayServer()
    await writeConfig(tmp.path, llm.url)
    envForRelay(relay.url, tmp.path)

    const listener = await Server.listen({ hostname: "127.0.0.1", port: 0 })
    try {
      await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "device.hello"), "missing relay hello"),
        5_000,
        "timed out waiting for relay hello",
      )

      const socket = await withTimeout(
        waitFor(() => Array.from(relay.sockets)[0], "missing relay socket"),
        5_000,
        "timed out waiting for relay socket",
      )
      socket.send(runEnvelope({ task_id: "bad-key" }))

      const failed = await withTimeout(
        waitFor(
          () => relay.messages.find((msg) => msg.type === "task.failed" && msg.payload.task_id === "bad-key"),
          () =>
            `missing task.failed; relay messages: ${JSON.stringify(relay.messages)}; llm inputs: ${JSON.stringify(llm.inputs)}`,
        ),
        10_000,
        "timed out waiting for invalid key task.failed",
      )
      expect(failed.payload.error).toBe("模型 key 无效")
      expect(llm.inputs.length).toBeGreaterThanOrEqual(1)
    } finally {
      await listener.stop(true)
      relay.stop()
      llm.stop()
    }
  })

  test("relays permission approval response into the running task instance", async () => {
    await using tmp = await tmpdir({ git: true })
    const llm = llmServer([
      toolChunks("todowrite", {
        todos: [{ content: "relay approval", status: "completed", priority: "high" }],
      }),
      textChunks("approved done"),
    ])
    const relay = relayServer()
    await writeRelayConfig(tmp.path, llm.url, {
      permission: {
        todowrite: "ask",
      },
    })
    envForRelay(relay.url, tmp.path)

    const listener = await Server.listen({ hostname: "127.0.0.1", port: 0 })
    try {
      await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "device.hello"), "missing relay hello"),
        5_000,
        "timed out waiting for relay hello",
      )

      const socket = await withTimeout(
        waitFor(() => Array.from(relay.sockets)[0], "missing relay socket"),
        5_000,
        "timed out waiting for relay socket",
      )
      socket.send(runEnvelope({ task_id: "approval-task", parts: [{ type: "text", text: "update todo" }] }))

      const waiting = await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "task.waiting_approval"), "missing approval wait"),
        10_000,
        "timed out waiting for approval wait",
      )
      expect(waiting.payload).toMatchObject({
        task_id: "approval-task",
        permission: "todowrite",
      })
      expect(waiting.payload.permission_id).toBeString()

      socket.send(
        serverEnvelope(
          "task.approval_response",
          {
            task_id: "approval-task",
            permission_id: waiting.payload.permission_id,
            reply: "once",
          },
          "approval-response-1",
        ),
      )

      const applied = await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "task.approval_applied"), "missing approval applied"),
        10_000,
        "timed out waiting for approval applied",
      )
      expect(applied.payload).toMatchObject({
        task_id: "approval-task",
        permission_id: waiting.payload.permission_id,
        reply: "once",
      })

      const completed = await withTimeout(
        waitFor(
          () => relay.messages.find((msg) => msg.type === "task.completed"),
          () =>
            `missing task.completed; relay messages: ${JSON.stringify(relay.messages)}; llm inputs: ${JSON.stringify(
              llm.inputs,
            )}`,
        ),
        10_000,
        "timed out waiting for task.completed",
      )
      expect(completed.payload).toMatchObject({
        task_id: "approval-task",
        result: "approved done",
      })
      expect(llm.inputs.length).toBeGreaterThanOrEqual(2)
    } finally {
      await listener.stop(true)
      relay.stop()
      llm.stop()
    }
  })

  test("preserves waiting approval state across reconnect instead of restarting the task", async () => {
    await using tmp = await tmpdir({ git: true })
    const llm = llmServer([
      toolChunks("todowrite", {
        todos: [{ content: "reconnect approval", status: "completed", priority: "high" }],
      }),
      textChunks("reconnect approval done"),
    ])
    const relay = relayServer()
    await writeRelayConfig(tmp.path, llm.url, { permission: { todowrite: "ask" } })
    envForRelay(relay.url, tmp.path)

    const listener = await Server.listen({ hostname: "127.0.0.1", port: 0 })
    try {
      await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "device.hello"), "missing relay hello"),
        5_000,
        "timed out waiting for relay hello",
      )
      const firstSocket = await withTimeout(
        waitFor(() => Array.from(relay.sockets)[0], "missing relay socket"),
        5_000,
        "timed out waiting for relay socket",
      )
      firstSocket.send(
        runEnvelope({ task_id: "approval-reconnect-task", parts: [{ type: "text", text: "update todo" }] }),
      )

      const waiting = await withTimeout(
        waitFor(
          () =>
            relay.messages.find(
              (msg) => msg.type === "task.waiting_approval" && msg.payload.task_id === "approval-reconnect-task",
            ),
          "missing approval wait",
        ),
        10_000,
        "timed out waiting for approval wait",
      )
      firstSocket.close()
      await withTimeout(
        waitFor(
          () => (relay.messages.filter((msg) => msg.type === "device.hello").length >= 2 ? true : undefined),
          "missing reconnect relay hello",
        ),
        10_000,
        "timed out waiting for relay reconnect",
      )
      await withTimeout(
        waitFor(
          () =>
            relay.messages.filter(
              (msg) => msg.type === "task.waiting_approval" && msg.payload.task_id === "approval-reconnect-task",
            ).length >= 2
              ? true
              : undefined,
          "missing reannounced approval wait",
        ),
        5_000,
        "timed out waiting for reannounced approval wait",
      )
      expect(
        relay.messages.filter(
          (msg) => msg.type === "task.started" && msg.payload.task_id === "approval-reconnect-task",
        ),
      ).toHaveLength(1)

      const reconnectSocket = await withTimeout(
        waitFor(() => Array.from(relay.sockets)[0], "missing reconnect socket"),
        5_000,
        "timed out waiting for reconnect socket",
      )
      reconnectSocket.send(
        serverEnvelope("task.approval_response", {
          task_id: "approval-reconnect-task",
          permission_id: waiting.payload.permission_id,
          reply: "once",
        }),
      )
      await withTimeout(
        waitFor(
          () =>
            relay.messages.find(
              (msg) => msg.type === "task.completed" && msg.payload.task_id === "approval-reconnect-task",
            ),
          "missing completed task",
        ),
        10_000,
        "timed out waiting for reconnect approval completion",
      )
    } finally {
      await listener.stop(true)
      relay.stop()
      llm.stop()
    }
  })

  test("releases the relay slot when cancelling a task waiting for approval", async () => {
    await using tmp = await tmpdir({ git: true })
    const llm = llmServer([
      toolChunks("todowrite", {
        todos: [{ content: "cancel approval", status: "completed", priority: "high" }],
      }),
      textChunks("next task done"),
    ])
    const relay = relayServer()
    await writeRelayConfig(tmp.path, llm.url, { permission: { todowrite: "ask" } })
    envForRelay(relay.url, tmp.path)

    const listener = await Server.listen({ hostname: "127.0.0.1", port: 0 })
    try {
      await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "device.hello"), "missing relay hello"),
        5_000,
        "timed out waiting for relay hello",
      )
      const socket = await withTimeout(
        waitFor(() => Array.from(relay.sockets)[0], "missing relay socket"),
        5_000,
        "timed out waiting for relay socket",
      )
      socket.send(runEnvelope({ task_id: "cancel-approval-task", parts: [{ type: "text", text: "update todo" }] }))
      await withTimeout(
        waitFor(
          () =>
            relay.messages.find(
              (msg) => msg.type === "task.waiting_approval" && msg.payload.task_id === "cancel-approval-task",
            ),
          "missing approval wait",
        ),
        10_000,
        "timed out waiting for approval wait",
      )
      socket.send(serverEnvelope("task.cancel", { task_id: "cancel-approval-task" }))
      await withTimeout(
        waitFor(
          () =>
            relay.messages.find(
              (msg) => msg.type === "task.cancelled" && msg.payload.task_id === "cancel-approval-task",
            ),
          "missing cancellation",
        ),
        10_000,
        "timed out waiting for task cancellation",
      )

      socket.send(runEnvelope({ task_id: "after-cancel-task", parts: [{ type: "text", text: "continue" }] }))
      await withTimeout(
        waitFor(
          () =>
            relay.messages.find((msg) => msg.type === "task.started" && msg.payload.task_id === "after-cancel-task"),
          "next task did not start",
        ),
        10_000,
        "timed out waiting for next task start",
      )
      expect(
        relay.messages.find(
          (msg) =>
            msg.type === "task.failed" &&
            msg.payload.task_id === "after-cancel-task" &&
            String(msg.payload.error).includes("agent busy"),
        ),
      ).toBeUndefined()
    } finally {
      await listener.stop(true)
      relay.stop()
      llm.stop()
    }
  })

  test("relays question response into the running task instance", async () => {
    await using tmp = await tmpdir({ git: true })
    const llm = llmServer([
      toolChunks("question", {
        reason: "required_user_input",
        blocking_context: "The deploy target must be selected by the user before continuing.",
        attempted_steps: ["Checked the task input and found no deploy target."],
        questions: [
          {
            question: "Choose deploy target",
            header: "target",
            options: [{ label: "prod", description: "production" }],
          },
        ],
      }),
      textChunks("question done"),
    ])
    const relay = relayServer()
    await writeRelayConfig(tmp.path, llm.url, {})
    envForRelay(relay.url, tmp.path)

    const listener = await Server.listen({ hostname: "127.0.0.1", port: 0 })
    try {
      await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "device.hello"), "missing relay hello"),
        5_000,
        "timed out waiting for relay hello",
      )

      const socket = await withTimeout(
        waitFor(() => Array.from(relay.sockets)[0], "missing relay socket"),
        5_000,
        "timed out waiting for relay socket",
      )
      socket.send(runEnvelope({ task_id: "question-task", parts: [{ type: "text", text: "ask user" }] }))

      const asked = await withTimeout(
        waitFor(
          () => relay.messages.find((msg) => msg.type === "task.question_asked"),
          "missing question asked",
          25_000,
        ),
        30_000,
        "timed out waiting for question asked",
      )
      expect(asked.payload).toMatchObject({
        task_id: "question-task",
      })
      expect(asked.payload.request_id).toBeString()
      expect(asked.payload.questions).toEqual([
        expect.objectContaining({
          question: "Choose deploy target",
          header: "target",
        }),
      ])

      socket.send(
        serverEnvelope(
          "task.question_response",
          {
            task_id: "question-task",
            request_id: asked.payload.request_id,
            answers: [["prod"]],
          },
          "question-response-1",
        ),
      )

      const replied = await withTimeout(
        waitFor(
          () => relay.messages.find((msg) => msg.type === "task.question_replied"),
          "missing question replied",
          25_000,
        ),
        30_000,
        "timed out waiting for question replied",
      )
      expect(replied.payload).toMatchObject({
        task_id: "question-task",
        request_id: asked.payload.request_id,
        answers: [["prod"]],
      })

      const completed = await withTimeout(
        waitFor(
          () => relay.messages.find((msg) => msg.type === "task.completed"),
          () =>
            `missing task.completed; relay messages: ${JSON.stringify(relay.messages)}; llm inputs: ${JSON.stringify(
              llm.inputs,
            )}`,
          35_000,
        ),
        35_000,
        "timed out waiting for task.completed",
      )
      expect(completed.payload).toMatchObject({
        task_id: "question-task",
        result: "question done",
      })
      expect(llm.inputs.length).toBeGreaterThanOrEqual(2)
    } finally {
      await listener.stop(true)
      relay.stop()
      llm.stop()
    }
  })

  test("collects relay artifact directory files and streams artifact.fetch chunks", async () => {
    await using tmp = await tmpdir({ git: true })
    const artifactPath = path.join(tmp.path, ".chatcodex-artifacts", "artifact-task", "report.txt")
    const llm = llmServer([
      toolChunks("write", {
        filePath: artifactPath,
        content: "relay artifact body",
      }),
      textChunks("artifact ready"),
    ])
    const relay = relayServer()
    await writeRelayConfig(tmp.path, llm.url, {
      permission: {
        edit: "allow",
      },
    })
    envForRelay(relay.url, tmp.path)

    const listener = await Server.listen({ hostname: "127.0.0.1", port: 0 })
    try {
      await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "device.hello"), "missing relay hello"),
        5_000,
        "timed out waiting for relay hello",
      )
      const socket = await withTimeout(
        waitFor(() => Array.from(relay.sockets)[0], "missing relay socket"),
        5_000,
        "timed out waiting for relay socket",
      )
      socket.send(
        runEnvelope({
          task_id: "artifact-task",
          parts: [{ type: "text", text: "write artifact" }],
          metadata: { delivery_required: "true" },
        }),
      )

      const completed = await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "task.completed"), "missing task.completed", 35_000),
        35_000,
        "timed out waiting for task.completed",
      )
      expect(completed.payload).toMatchObject({
        task_id: "artifact-task",
        result: "artifact ready",
      })
      expect(completed.payload.artifacts).toEqual([
        expect.objectContaining({
          filename: "report.txt",
          relative_path: ".chatcodex-artifacts/artifact-task/report.txt",
          size_bytes: "relay artifact body".length,
        }),
      ])
      expect(
        relay.messages.find(
          (msg) =>
            msg.type === "task.progress" &&
            msg.payload.task_id === "artifact-task" &&
            msg.payload.metadata?.reason === "missing_artifact",
        ),
      ).toBeUndefined()

      const artifact = completed.payload.artifacts[0]
      socket.send(
        serverEnvelope(
          "artifact.fetch",
          {
            task_id: "artifact-task",
            artifact_id: artifact.id,
            relative_path: artifact.relative_path,
          },
          "artifact-fetch-1",
        ),
      )

      const chunk = await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "artifact.chunk"), "missing artifact chunk"),
        5_000,
        "timed out waiting for artifact chunk",
      )
      const done = await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "artifact.done"), "missing artifact done"),
        5_000,
        "timed out waiting for artifact done",
      )
      expect(chunk.request_id).toBe("artifact-fetch-1")
      expect(chunk.payload).toMatchObject({
        task_id: "artifact-task",
        artifact_id: artifact.id,
        seq: 0,
      })
      expect(Buffer.from(chunk.payload.data, "base64").toString("utf8")).toBe("relay artifact body")
      expect(done.payload).toMatchObject({
        task_id: "artifact-task",
        artifact_id: artifact.id,
      })
    } finally {
      await listener.stop(true)
      relay.stop()
      llm.stop()
    }
  })

  test("streams delivery process text and still commits the corrected final response", async () => {
    await using tmp = await tmpdir({ git: true })
    const artifactPath = path.join(tmp.path, ".chatcodex-artifacts", "buffered-delivery-task", "report.txt")
    const llm = llmServer([
      textChunks("<think>不应显示的内部修正</think>错误草稿：文件已经交付。"),
      toolChunks("write", { filePath: artifactPath, content: "final report" }),
      textChunks("<think>最终可见思考</think>最终答复：报告已生成，可下载 report.txt。"),
    ])
    const relay = relayServer()
    await writeRelayConfig(tmp.path, llm.url, { permission: { edit: "allow" } })
    envForRelay(relay.url, tmp.path)

    const listener = await Server.listen({ hostname: "127.0.0.1", port: 0 })
    try {
      await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "device.hello"), "missing relay hello"),
        5_000,
        "timed out waiting for relay hello",
      )
      const socket = await withTimeout(
        waitFor(() => Array.from(relay.sockets)[0], "missing relay socket"),
        5_000,
        "timed out waiting for relay socket",
      )
      socket.send(
        runEnvelope({
          task_id: "buffered-delivery-task",
          metadata: { delivery_required: "true" },
        }),
      )

      const checking = await withTimeout(
        waitFor(
          () =>
            relay.messages.find(
              (msg) =>
                msg.type === "task.progress" &&
                msg.payload.task_id === "buffered-delivery-task" &&
                msg.payload.metadata?.reason === "missing_artifact",
            ),
          "missing delivery validation progress",
          20_000,
        ),
        25_000,
        "timed out waiting for delivery validation",
      )
      expect(checking.payload.message).toBe("正在校验交付文件")

      const completed = await withTimeout(
        waitFor(
          () =>
            relay.messages.find(
              (msg) => msg.type === "task.completed" && msg.payload.task_id === "buffered-delivery-task",
            ),
          "missing corrected delivery completion",
          30_000,
        ),
        35_000,
        "timed out waiting for corrected delivery completion",
      )
      const textDeltas = relay.messages.filter(
        (msg) =>
          msg.type === "task.delta" && msg.payload.task_id === "buffered-delivery-task" && msg.payload.field === "text",
      )
      expect(textDeltas.map((msg) => msg.payload.content).join("")).toContain(
        "最终答复：报告已生成，可下载 report.txt。",
      )
      expect(
        relay.messages
          .filter(
            (msg) =>
              msg.type === "task.delta" &&
              msg.payload.task_id === "buffered-delivery-task" &&
              msg.payload.field === "reasoning",
          )
          .map((msg) => msg.payload.content)
          .join(""),
      ).toContain("最终可见思考")
      expect(completed.payload.result).toContain("最终答复：报告已生成，可下载 report.txt。")
      expect(completed.payload.artifacts).toHaveLength(1)
      expect(completed.payload.artifacts[0].filename).toBe("report.txt")
      expect(JSON.stringify(llm.inputs)).toContain("完整、自洽、可独立阅读")
    } finally {
      await listener.stop(true)
      relay.stop()
      llm.stop()
    }
  })

  test("retains delivery text across output-length continuation without duplicating chunks", async () => {
    await using tmp = await tmpdir({ git: true })
    const artifactDir = path.join(tmp.path, ".chatcodex-artifacts", "delivery-length-task")
    await mkdir(artifactDir, { recursive: true })
    await writeFile(path.join(artifactDir, "long-report.txt"), "report")
    const llm = llmServer([textChunks("第一段，", "length"), textChunks("第二段完成。")])
    const relay = relayServer()
    await writeConfig(tmp.path, llm.url)
    envForRelay(relay.url, tmp.path)

    const listener = await Server.listen({ hostname: "127.0.0.1", port: 0 })
    try {
      await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "device.hello"), "missing relay hello"),
        5_000,
        "timed out waiting for relay hello",
      )
      const socket = await withTimeout(
        waitFor(() => Array.from(relay.sockets)[0], "missing relay socket"),
        5_000,
        "timed out waiting for relay socket",
      )
      socket.send(
        runEnvelope({ task_id: "delivery-length-task", metadata: { delivery_required: "true" } }),
      )

      const completed = await withTimeout(
        waitFor(
          () => relay.messages.find((msg) => msg.type === "task.completed" && msg.payload.task_id === "delivery-length-task"),
          "missing length continuation completion",
          30_000,
        ),
        35_000,
        "timed out waiting for length continuation completion",
      )
      const textDeltas = relay.messages.filter(
        (msg) => msg.type === "task.delta" && msg.payload.task_id === "delivery-length-task" && msg.payload.field === "text",
      )
      expect(textDeltas.map((msg) => msg.payload.content).join("")).toBe("第一段，第二段完成。")
      expect(completed.payload.result).toBe("第一段，第二段完成。")
      expect(
        relay.messages.filter(
          (msg) => msg.type === "task.completed" && msg.payload.task_id === "delivery-length-task",
        ),
      ).toHaveLength(1)
    } finally {
      await listener.stop(true)
      relay.stop()
      llm.stop()
    }
  })

  test("streams goal delivery process text and still commits the corrected goal response", async () => {
    await using tmp = await tmpdir({ git: true })
    const artifactPath = path.join(tmp.path, ".chatcodex-artifacts", "goal-delivery-task", "goal-report.txt")
    const llm = llmServer([
      toolChunks("goal_complete", { summary: "目标已完成但尚未生成文件" }),
      textChunks("目标草稿：交付已经完成。"),
      jsonText({ completed: true, reason: "目标步骤完成", missing_items: [], next_instruction: "" }),
      toolChunks("write", { filePath: artifactPath, content: "goal report" }),
      textChunks("目标最终答复：交付文件 goal-report.txt 已生成。"),
    ])
    const relay = relayServer()
    await writeRelayConfig(tmp.path, llm.url, { permission: { edit: "allow" } })
    envForRelay(relay.url, tmp.path)

    const listener = await Server.listen({ hostname: "127.0.0.1", port: 0 })
    try {
      await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "device.hello"), "missing relay hello"),
        5_000,
        "timed out waiting for relay hello",
      )
      const socket = await withTimeout(
        waitFor(() => Array.from(relay.sockets)[0], "missing relay socket"),
        5_000,
        "timed out waiting for relay socket",
      )
      socket.send(
        runEnvelope({
          task_id: "goal-delivery-task",
          metadata: {
            goal: "生成并交付目标报告",
            goal_id: "goal-delivery",
            goal_max_iterations: "2",
            delivery_required: "true",
          },
        }),
      )

      const completed = await withTimeout(
        waitFor(
          () => relay.messages.find((msg) => msg.type === "task.completed" && msg.payload.task_id === "goal-delivery-task"),
          () => `missing goal delivery completion; messages: ${JSON.stringify(relay.messages)}`,
          35_000,
        ),
        40_000,
        "timed out waiting for goal delivery completion",
      )
      const textDeltas = relay.messages.filter(
        (msg) => msg.type === "task.delta" && msg.payload.task_id === "goal-delivery-task" && msg.payload.field === "text",
      )
      expect(textDeltas.map((msg) => msg.payload.content).join("")).toContain(
        "目标最终答复：交付文件 goal-report.txt 已生成。",
      )
      expect(completed.payload.result).toContain("目标最终答复：交付文件 goal-report.txt 已生成。")
      expect(completed.payload.artifacts).toHaveLength(1)
      expect(completed.payload.artifacts[0].filename).toBe("goal-report.txt")
      expect(
        relay.messages.filter(
          (msg) => msg.type === "task.progress" && msg.payload.metadata?.reason === "missing_artifact",
        ),
      ).toHaveLength(1)
    } finally {
      await listener.stop(true)
      relay.stop()
      llm.stop()
    }
  }, 45_000)

  test("drops buffered delivery text when the task is cancelled before validation", async () => {
    await using tmp = await tmpdir({ git: true })
    const llm = delayedScriptLLMServer([{ delayMS: 4_000, chunks: textChunks("取消后不可见的交付正文") }])
    const relay = relayServer()
    await writeConfig(tmp.path, llm.url)
    envForRelay(relay.url, tmp.path)

    const listener = await Server.listen({ hostname: "127.0.0.1", port: 0 })
    try {
      await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "device.hello"), "missing relay hello"),
        5_000,
        "timed out waiting for relay hello",
      )
      const socket = await withTimeout(
        waitFor(() => Array.from(relay.sockets)[0], "missing relay socket"),
        5_000,
        "timed out waiting for relay socket",
      )
      socket.send(
        runEnvelope({ task_id: "cancel-buffered-delivery", metadata: { delivery_required: "true" } }),
      )
      await withTimeout(
        waitFor(
          () => relay.messages.find((msg) => msg.type === "task.started" && msg.payload.task_id === "cancel-buffered-delivery"),
          "missing task start",
        ),
        10_000,
        "timed out waiting for task start",
      )
      socket.send(serverEnvelope("task.cancel", { task_id: "cancel-buffered-delivery" }))
      await withTimeout(
        waitFor(
          () => relay.messages.find((msg) => msg.type === "task.cancelled" && msg.payload.task_id === "cancel-buffered-delivery"),
          "missing cancellation",
        ),
        10_000,
        "timed out waiting for cancellation",
      )
      await Bun.sleep(4_200)
      expect(
        relay.messages.filter(
          (msg) =>
            msg.payload.task_id === "cancel-buffered-delivery" && msg.type === "task.completed",
        ),
      ).toEqual([])
      expect(
        relay.messages
          .filter(
            (msg) =>
              msg.payload.task_id === "cancel-buffered-delivery" &&
              msg.type === "task.delta" &&
              msg.payload.field === "text",
          )
          .map((msg) => msg.payload.content)
          .join(""),
      ).not.toContain("取消后不可见的交付正文")
    } finally {
      await listener.stop(true)
      relay.stop()
      llm.stop()
    }
  }, 20_000)

  test("fails a stalled delivery without leaking rejected drafts or emitting completion", async () => {
    await using tmp = await tmpdir({ git: true })
    const llm = llmServer([textChunks("第一份无文件草稿"), textChunks("第二份无文件草稿")])
    const relay = relayServer()
    await writeConfig(tmp.path, llm.url)
    envForRelay(relay.url, tmp.path)

    const listener = await Server.listen({ hostname: "127.0.0.1", port: 0 })
    try {
      await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "device.hello"), "missing relay hello"),
        5_000,
        "timed out waiting for relay hello",
      )
      const socket = await withTimeout(
        waitFor(() => Array.from(relay.sockets)[0], "missing relay socket"),
        5_000,
        "timed out waiting for relay socket",
      )
      socket.send(
        runEnvelope({ task_id: "failed-buffered-delivery", metadata: { delivery_required: "true" } }),
      )

      await withTimeout(
        waitFor(
          () => relay.messages.find((msg) => msg.type === "task.failed" && msg.payload.task_id === "failed-buffered-delivery"),
          "missing failed delivery event",
          30_000,
        ),
        35_000,
        "timed out waiting for failed delivery event",
      )
      expect(
        relay.messages.filter(
          (msg) =>
            msg.payload.task_id === "failed-buffered-delivery" && msg.type === "task.completed",
        ),
      ).toEqual([])
      expect(
        relay.messages
          .filter(
            (msg) =>
              msg.payload.task_id === "failed-buffered-delivery" &&
              msg.type === "task.delta" &&
              msg.payload.field === "text",
          )
          .map((msg) => msg.payload.content)
          .join(""),
      ).toContain("第一份无文件草稿")
    } finally {
      await listener.stop(true)
      relay.stop()
      llm.stop()
    }
  })

  test("persists image file parts as downloadable relay artifacts", async () => {
    await using tmp = await tmpdir({ git: true })
    const llm = llmServer([imageResponse("relay image bytes")])
    const relay = relayServer()
    await writeImageConfig(tmp.path, llm.url)
    envForRelay(relay.url, tmp.path)

    const listener = await Server.listen({ hostname: "127.0.0.1", port: 0 })
    try {
      await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "device.hello"), "missing relay hello"),
        5_000,
        "timed out waiting for relay hello",
      )
      const socket = await withTimeout(
        waitFor(() => Array.from(relay.sockets)[0], "missing relay socket"),
        5_000,
        "timed out waiting for relay socket",
      )
      socket.send(runEnvelope({ task_id: "image-task", parts: [{ type: "text", text: "generate image" }] }))

      const completed = await withTimeout(
        waitFor(
          () => relay.messages.find((msg) => msg.type === "task.completed"),
          () =>
            `missing image task.completed; relay messages: ${JSON.stringify(relay.messages)}; llm inputs: ${JSON.stringify(
              llm.inputs,
            )}`,
          35_000,
        ),
        35_000,
        "timed out waiting for task.completed",
      )
      expect(completed.payload.task_id).toBe("image-task")
      expect(completed.payload.files).toEqual([
        expect.objectContaining({
          type: "file",
          mime: "image/png",
          filename: "test-image-1.png",
        }),
      ])
      expect(completed.payload.files[0].url).toStartWith("data:image/png;base64,")
      expect(completed.payload.artifacts).toEqual([
        expect.objectContaining({
          mime: "image/png",
          size_bytes: "relay image bytes".length,
        }),
      ])
      expect(completed.payload.artifacts[0].filename).toEndWith("test-image-1.png")
      expect(completed.payload.artifacts[0].relative_path).toStartWith(".chatcodex-artifacts/image-task/")
      expect(completed.payload.artifacts[0].relative_path).toEndWith("test-image-1.png")

      const artifact = completed.payload.artifacts[0]
      socket.send(
        serverEnvelope(
          "artifact.fetch",
          {
            task_id: "image-task",
            artifact_id: artifact.id,
            relative_path: artifact.relative_path,
          },
          "image-fetch-1",
        ),
      )

      const chunk = await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "artifact.chunk"), "missing image artifact chunk"),
        5_000,
        "timed out waiting for image artifact chunk",
      )
      expect(Buffer.from(chunk.payload.data, "base64").toString("utf8")).toBe("relay image bytes")
    } finally {
      await listener.stop(true)
      relay.stop()
      llm.stop()
    }
  })

  test("runs manual compaction as a dedicated relay task", async () => {
    await using tmp = await tmpdir({ git: true })
    const llm = llmServer([textChunks("initial response"), textChunks("compaction summary")])
    const relay = relayServer()
    await writeConfig(tmp.path, llm.url)
    envForRelay(relay.url, tmp.path)

    const listener = await Server.listen({ hostname: "127.0.0.1", port: 0 })
    try {
      await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "device.hello"), "missing relay hello"),
        5_000,
        "timed out waiting for relay hello",
      )
      const socket = await withTimeout(
        waitFor(() => Array.from(relay.sockets)[0], "missing relay socket"),
        5_000,
        "timed out waiting for relay socket",
      )
      socket.send(
        runEnvelope({ task_id: "before-compact-task", parts: [{ type: "text", text: "retain this context" }] }),
      )

      const initial = await withTimeout(
        waitFor(
          () =>
            relay.messages.find(
              (msg) => msg.type === "task.completed" && msg.payload.task_id === "before-compact-task",
            ),
          "missing initial task.completed",
        ),
        20_000,
        "timed out waiting for initial task completion",
      )
      expect(initial.payload.session_id).toBeString()

      socket.send(
        serverEnvelope("task.run", {
          task_id: "manual-compact-task",
          agent_id: "agent-1",
          machine_id: "machine-1",
          project_id: "project-1",
          session_id: initial.payload.session_id,
          parts: [],
          metadata: { task_command: "compact" },
        }),
      )

      await withTimeout(
        waitFor(
          () =>
            relay.messages.find(
              (msg) => msg.type === "task.compaction_started" && msg.payload.task_id === "manual-compact-task",
            ),
          "missing manual compaction start",
        ),
        20_000,
        "timed out waiting for manual compaction start",
      )
      const completed = await withTimeout(
        waitFor(
          () =>
            relay.messages.find(
              (msg) => msg.type === "task.completed" && msg.payload.task_id === "manual-compact-task",
            ),
          "missing manual compaction completion",
        ),
        20_000,
        "timed out waiting for manual compaction completion",
      )
      expect(completed.payload.result).toBe("压缩上下文完成")
      expect(completed.payload.usage?.stage).toBe("compaction_context_ready")
      expect(completed.payload.usage?.compaction_count_tokens).toBeGreaterThan(0)
      expect(completed.payload.usage?.context_usage_percent).toBeNumber()
      expect(completed.payload.usage?.context_usage_percent).toBeGreaterThanOrEqual(0)
      expect(
        relay.messages.find(
          (msg) => msg.type === "task.compaction_completed" && msg.payload.task_id === "manual-compact-task",
        ),
      ).toBeDefined()
      expect(
        relay.messages.find((msg) => msg.type === "task.failed" && msg.payload.task_id === "manual-compact-task"),
      ).toBeUndefined()
      expect(llm.inputs.length).toBeGreaterThanOrEqual(2)
    } finally {
      await listener.stop(true)
      relay.stop()
      llm.stop()
    }
  })

  test("reports manual compaction model failures instead of false completion", async () => {
    await using tmp = await tmpdir({ git: true })
    const llm = llmServer([
      textChunks("initial response"),
      { type: "http-error", status: 401, body: { code: "INVALID_API_KEY", message: "Invalid API key" } },
    ])
    const relay = relayServer()
    await writeConfig(tmp.path, llm.url)
    envForRelay(relay.url, tmp.path)

    const listener = await Server.listen({ hostname: "127.0.0.1", port: 0 })
    try {
      await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "device.hello"), "missing relay hello"),
        5_000,
        "timed out waiting for relay hello",
      )
      const socket = await withTimeout(
        waitFor(() => Array.from(relay.sockets)[0], "missing relay socket"),
        5_000,
        "timed out waiting for relay socket",
      )
      socket.send(
        runEnvelope({ task_id: "before-failed-compact-task", parts: [{ type: "text", text: "retain context" }] }),
      )
      const initial = await withTimeout(
        waitFor(
          () =>
            relay.messages.find(
              (msg) => msg.type === "task.completed" && msg.payload.task_id === "before-failed-compact-task",
            ),
          "missing initial task.completed",
        ),
        20_000,
        "timed out waiting for initial task completion",
      )

      socket.send(
        serverEnvelope("task.run", {
          task_id: "failed-manual-compact-task",
          agent_id: "agent-1",
          machine_id: "machine-1",
          project_id: "project-1",
          session_id: initial.payload.session_id,
          parts: [],
          metadata: { task_command: "compact" },
        }),
      )

      const failed = await withTimeout(
        waitFor(
          () =>
            relay.messages.find(
              (msg) => msg.type === "task.failed" && msg.payload.task_id === "failed-manual-compact-task",
            ),
          "missing failed manual compaction",
        ),
        20_000,
        "timed out waiting for failed manual compaction",
      )
      expect(failed.payload.error).toContain("模型 key 无效")
      expect(
        relay.messages.find(
          (msg) => msg.type === "task.compaction_completed" && msg.payload.task_id === "failed-manual-compact-task",
        ),
      ).toBeUndefined()
      expect(
        relay.messages.find(
          (msg) => msg.type === "task.completed" && msg.payload.task_id === "failed-manual-compact-task",
        ),
      ).toBeUndefined()
    } finally {
      await listener.stop(true)
      relay.stop()
      llm.stop()
    }
  })

  test("rejects relay task.run with mismatched routing or empty input before model call", async () => {
    await using tmp = await tmpdir({ git: true })
    const llm = llmServer()
    const relay = relayServer()
    await writeConfig(tmp.path, llm.url)
    envForRelay(relay.url, tmp.path)

    const listener = await Server.listen({ hostname: "127.0.0.1", port: 0 })
    try {
      await withTimeout(
        waitFor(() => relay.messages.find((msg) => msg.type === "device.hello"), "missing relay hello"),
        5_000,
        "timed out waiting for relay hello",
      )
      const socket = await withTimeout(
        waitFor(() => Array.from(relay.sockets)[0], "missing relay socket"),
        5_000,
        "timed out waiting for relay socket",
      )

      socket.send(runEnvelope({ task_id: "bad-project", project_id: "other-project" }))
      socket.send(runEnvelope({ task_id: "bad-agent", agent_id: "other-agent" }))
      socket.send(runEnvelope({ task_id: "bad-machine", machine_id: "other-machine" }))
      socket.send(runEnvelope({ task_id: "bad-parts", parts: [] }))

      await withTimeout(
        waitFor(
          () => (relay.messages.filter((msg) => msg.type === "task.failed").length >= 4 ? true : undefined),
          "missing task.failed messages",
        ),
        5_000,
        "timed out waiting for task.failed messages",
      )
      const failed = relay.messages.filter((msg) => msg.type === "task.failed")
      expect(failed.map((msg) => msg.payload.task_id)).toEqual(["bad-project", "bad-agent", "bad-machine", "bad-parts"])
      expect(failed.map((msg) => msg.payload.error)).toEqual([
        "unknown project: other-project",
        "unknown agent: other-agent",
        "unknown machine: other-machine",
        "missing parts",
      ])
      expect(llm.inputs).toHaveLength(0)
    } finally {
      await listener.stop(true)
      relay.stop()
      llm.stop()
    }
  })

  test("stop closes a pending websocket before it opens", async () => {
    class PendingWebSocket extends EventTarget {
      static readonly CONNECTING = 0
      static readonly OPEN = 1
      static readonly CLOSING = 2
      static readonly CLOSED = 3

      readonly url: string
      readyState = PendingWebSocket.CONNECTING

      constructor(url: string) {
        super()
        this.url = url
        sockets.push(this)
      }

      send() {}

      close() {
        this.readyState = PendingWebSocket.CLOSED
        this.dispatchEvent(new Event("close"))
      }
    }

    const originalWebSocket = globalThis.WebSocket
    const sockets: PendingWebSocket[] = []
    globalThis.WebSocket = PendingWebSocket as unknown as typeof WebSocket
    process.env.OPENCODE_RELAY_URL = "wss://www.xyapi.top/codex/ws/device"
    process.env.OPENCODE_RELAY_OPERATOR_KEY = "operator-key"

    try {
      const relay = Relay.start()
      expect(sockets).toHaveLength(1)
      await relay?.stop()
      expect(sockets[0]?.readyState).toBe(PendingWebSocket.CLOSED)
    } finally {
      globalThis.WebSocket = originalWebSocket
    }
  })
})
