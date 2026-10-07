import { describe, expect, test } from "bun:test"
import { asSchema, dynamicTool, jsonSchema, type Tool, type ToolExecutionOptions } from "ai"
import { createLazyMcpTools, MCP_CALL_TOOL, MCP_SEARCH_TOOL } from "../../src/session/mcp-lazy"

const options = {
  toolCallId: "call_test",
  messages: [],
  abortSignal: new AbortController().signal,
} as ToolExecutionOptions

describe("lazy MCP tools", () => {
  test("search returns exact names and input schemas", async () => {
    const tools = createLazyMcpTools({
      verify_compile_config: fakeTool("Compile a Verify configuration", {
        type: "object",
        properties: { configId: { type: "integer", description: "Verify configuration ID" } },
        required: ["configId"],
        additionalProperties: false,
      }),
      verify_list_apps: fakeTool("List Verify applications", {
        type: "object",
        properties: {},
        additionalProperties: false,
      }),
    })

    const result = await execute(tools[MCP_SEARCH_TOOL], { query: "compile config" })
    const body = JSON.parse(output(result))

    expect(body.matches).toBeGreaterThan(0)
    expect(body.tools[0].name).toBe("verify_compile_config")
    expect(body.tools[0].input_schema.properties.configId.type).toBe("integer")
    expect(body.next).toContain(MCP_CALL_TOOL)
  })

  test("call delegates to the hidden MCP execution object", async () => {
    let received: unknown
    let callID = ""
    const tools = createLazyMcpTools({
      verify_get_config: dynamicTool({
        description: "Get one Verify configuration",
        inputSchema: jsonSchema({
          type: "object",
          properties: { configId: { type: "integer" } },
          required: ["configId"],
          additionalProperties: false,
        }),
        execute(args, execution) {
          received = args
          callID = execution.toolCallId
          return Promise.resolve({ title: "config", metadata: {}, output: "ok" })
        },
      }),
    })

    const result = await execute(tools[MCP_CALL_TOOL], {
      name: "verify_get_config",
      arguments_json: '{"configId":42}',
    })

    expect(received).toEqual({ configId: 42 })
    expect(callID).toBe("call_test")
    expect(output(result)).toBe("ok")
  })

  test("call rejects malformed arguments and suggests nearby names", async () => {
    const tools = createLazyMcpTools({
      verify_compile_config: fakeTool("Compile configuration", { type: "object", properties: {} }),
    })

    expect(() =>
      execute(tools[MCP_CALL_TOOL], {
        name: "verify_compile_confg",
        arguments_json: "{}",
      }),
    ).toThrow("verify_compile_config")

    expect(() =>
      execute(tools[MCP_CALL_TOOL], {
        name: "verify_compile_config",
        arguments_json: "[]",
      }),
    ).toThrow("must decode to a JSON object")
  })

  test("advertised proxy schemas stay compact for a large MCP catalog", async () => {
    const largeDescription = "Detailed MCP operation. ".repeat(100)
    const largeSchema = {
      type: "object",
      properties: Object.fromEntries(
        Array.from({ length: 20 }, (_, index) => [
          `field_${index}`,
          { type: "string", description: `Detailed field ${index}. `.repeat(20) },
        ]),
      ),
      additionalProperties: false,
    }
    const source = Object.fromEntries(
      Array.from({ length: 75 }, (_, index) => [`server_tool_${index}`, fakeTool(largeDescription, largeSchema)]),
    )
    const tools = createLazyMcpTools(source)
    const advertised = await Promise.all(
      Object.entries(tools).map(async ([name, item]) => ({
        name,
        description: item.description,
        schema: await Promise.resolve(asSchema(item.inputSchema).jsonSchema),
      })),
    )

    expect(Object.keys(tools)).toEqual([MCP_SEARCH_TOOL, MCP_CALL_TOOL])
    expect(Buffer.byteLength(JSON.stringify(advertised))).toBeLessThan(2_000)
  })
})

function fakeTool(description: string, schema: Record<string, unknown>) {
  return dynamicTool({
    description,
    inputSchema: jsonSchema(schema),
    execute: () => Promise.resolve({ title: "", metadata: {}, output: "ok" }),
  })
}

function execute(tool: Tool, args: Record<string, unknown>) {
  if (!tool.execute) throw new Error("expected executable tool")
  return tool.execute(args, options)
}

function output(result: unknown) {
  if (typeof result !== "object" || result === null || !("output" in result)) {
    throw new Error("expected tool output")
  }
  const value = result.output
  if (typeof value !== "string") throw new Error("expected string tool output")
  return value
}
