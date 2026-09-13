import fuzzysort from "fuzzysort"
import { asSchema, dynamicTool, jsonSchema, type Tool, type ToolExecutionOptions } from "ai"

export const MCP_SEARCH_TOOL = "mcp_search"
export const MCP_CALL_TOOL = "mcp_call"

const DEFAULT_SEARCH_LIMIT = 5
const MAX_SEARCH_LIMIT = 8
const DESCRIPTION_LIMIT = 320

type CatalogEntry = {
  name: string
  description: string
  schema: unknown
  search: string
  execute: NonNullable<Tool["execute"]>
}

export function createLazyMcpTools(source: Record<string, Tool>) {
  const catalog = Object.entries(source).flatMap(([name, item]): CatalogEntry[] => {
    if (!item.execute) return []
    const description = clip(item.description ?? "", DESCRIPTION_LIMIT)
    return [
      {
        name,
        description,
        schema: asSchema(item.inputSchema).jsonSchema,
        search: `${name} ${description}`,
        execute: item.execute,
      },
    ]
  })
  const byName = new Map(catalog.map((item) => [item.name, item]))

  return {
    [MCP_SEARCH_TOOL]: dynamicTool({
      description:
        "Search connected MCP tools before calling one. Returns exact tool names, descriptions, and input schemas for the best matches.",
      inputSchema: jsonSchema({
        type: "object",
        properties: {
          query: {
            type: "string",
            description: "Capability, operation, or tool name to search for.",
          },
          limit: {
            type: "integer",
            minimum: 1,
            maximum: MAX_SEARCH_LIMIT,
            description: `Maximum results. Defaults to ${DEFAULT_SEARCH_LIMIT}.`,
          },
        },
        required: ["query"],
        additionalProperties: false,
      }),
      execute(args) {
        const input = record(args, MCP_SEARCH_TOOL)
        const query = text(input.query, "query", MCP_SEARCH_TOOL)
        const limit = integer(input.limit, DEFAULT_SEARCH_LIMIT, 1, MAX_SEARCH_LIMIT)
        const matches = searchCatalog(catalog, query, limit)
        return Promise.resolve({
          title: "MCP tool search",
          metadata: { query, matches: matches.length, available: catalog.length },
          output: JSON.stringify(
            {
              query,
              matches: matches.length,
              available: catalog.length,
              tools: matches.map((item) => ({
                name: item.name,
                description: item.description,
                input_schema: item.schema,
              })),
              next: matches.length
                ? `Call ${MCP_CALL_TOOL} with one exact tool name and arguments_json matching its input_schema.`
                : "Try a broader capability or operation name.",
            },
            null,
            2,
          ),
          attachments: [],
        })
      },
    }),
    [MCP_CALL_TOOL]: dynamicTool({
      description:
        "Execute one MCP tool discovered with mcp_search. Use the exact returned name and encode arguments as a JSON object string.",
      inputSchema: jsonSchema({
        type: "object",
        properties: {
          name: {
            type: "string",
            description: "Exact MCP tool name returned by mcp_search.",
          },
          arguments_json: {
            type: "string",
            description: "JSON object string that satisfies the discovered input_schema.",
          },
        },
        required: ["name", "arguments_json"],
        additionalProperties: false,
      }),
      execute(args, options) {
        const input = record(args, MCP_CALL_TOOL)
        const name = text(input.name, "name", MCP_CALL_TOOL)
        const item = byName.get(name)
        if (!item) {
          const suggestions = searchCatalog(catalog, name, 3).map((match) => match.name)
          throw new Error(
            suggestions.length
              ? `Unknown MCP tool "${name}". Did you mean: ${suggestions.join(", ")}?`
              : `Unknown MCP tool "${name}". Call ${MCP_SEARCH_TOOL} first.`,
          )
        }
        const payload = jsonObject(input.arguments_json, name)
        return item.execute(payload, options as ToolExecutionOptions)
      },
    }),
  } satisfies Record<string, Tool>
}

export function isLazyMcpTool(name: string) {
  return name === MCP_SEARCH_TOOL || name === MCP_CALL_TOOL
}

function searchCatalog(catalog: CatalogEntry[], query: string, limit: number) {
  const normalized = query.trim().toLowerCase()
  if (!normalized) return catalog.slice(0, limit)

  const exact = catalog.filter((item) => item.name.toLowerCase() === normalized)
  const contains = catalog.filter((item) => !exact.includes(item) && item.search.toLowerCase().includes(normalized))
  const used = new Set([...exact, ...contains])
  const fuzzy = fuzzysort
    .go(
      query,
      catalog.filter((item) => !used.has(item)),
      { key: "search", limit, threshold: -10_000 },
    )
    .map((result) => result.obj)
  return [...exact, ...contains, ...fuzzy].slice(0, limit)
}

function record(value: unknown, tool: string): Record<string, unknown> {
  if (typeof value === "object" && value !== null && !Array.isArray(value)) return value as Record<string, unknown>
  throw new Error(`${tool} expects an object input.`)
}

function text(value: unknown, field: string, tool: string) {
  if (typeof value === "string" && value.trim()) return value.trim()
  throw new Error(`${tool}.${field} must be a non-empty string.`)
}

function integer(value: unknown, fallback: number, min: number, max: number) {
  if (value === undefined) return fallback
  if (typeof value !== "number" || !Number.isInteger(value)) return fallback
  return Math.min(max, Math.max(min, value))
}

function jsonObject(value: unknown, name: string): Record<string, unknown> {
  if (typeof value !== "string") throw new Error(`${MCP_CALL_TOOL}.arguments_json must be a JSON object string.`)
  const parsed: unknown = JSON.parse(value)
  if (typeof parsed === "object" && parsed !== null && !Array.isArray(parsed)) return parsed as Record<string, unknown>
  throw new Error(`Arguments for MCP tool "${name}" must decode to a JSON object.`)
}

function clip(value: string, limit: number) {
  if (value.length <= limit) return value
  return `${value.slice(0, limit - 1)}…`
}
