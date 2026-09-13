import { Schema } from "effect"
import { PositiveInt } from "@opencode-ai/core/schema"

export const Local = Schema.Struct({
  type: Schema.Literal("local").annotate({ description: "Type of MCP server connection" }),
  command: Schema.mutable(Schema.Array(Schema.String)).annotate({
    description: "Command and arguments to run the MCP server",
  }),
  environment: Schema.optional(Schema.Record(Schema.String, Schema.String)).annotate({
    description: "Environment variables to set when running the MCP server",
  }),
  enabled: Schema.optional(Schema.Boolean).annotate({
    description: "Enable or disable the MCP server on startup",
  }),
  timeout: Schema.optional(PositiveInt).annotate({
    description: "Legacy timeout in ms used when a phase-specific timeout is not configured.",
  }),
  connect_timeout: Schema.optional(PositiveInt).annotate({
    description: "Timeout in ms for starting and connecting to the MCP server. Defaults to 30000.",
  }),
  discovery_timeout: Schema.optional(PositiveInt).annotate({
    description: "Timeout in ms for MCP tool discovery. Defaults to 30000.",
  }),
  tool_timeout: Schema.optional(PositiveInt).annotate({
    description: "Timeout in ms for MCP tool execution.",
  }),
  async_tools: Schema.optional(Schema.mutable(Schema.Array(Schema.String))).annotate({
    description: "MCP tool names that should run as asynchronous jobs.",
  }),
}).annotate({ identifier: "McpLocalConfig" })
export type Local = Schema.Schema.Type<typeof Local>

export const OAuth = Schema.Struct({
  clientId: Schema.optional(Schema.String).annotate({
    description: "OAuth client ID. If not provided, dynamic client registration (RFC 7591) will be attempted.",
  }),
  clientSecret: Schema.optional(Schema.String).annotate({
    description: "OAuth client secret (if required by the authorization server)",
  }),
  scope: Schema.optional(Schema.String).annotate({ description: "OAuth scopes to request during authorization" }),
  redirectUri: Schema.optional(Schema.String).annotate({
    description: "OAuth redirect URI (default: http://127.0.0.1:19876/mcp/oauth/callback).",
  }),
}).annotate({ identifier: "McpOAuthConfig" })
export type OAuth = Schema.Schema.Type<typeof OAuth>

export const Remote = Schema.Struct({
  type: Schema.Literal("remote").annotate({ description: "Type of MCP server connection" }),
  url: Schema.String.annotate({ description: "URL of the remote MCP server" }),
  enabled: Schema.optional(Schema.Boolean).annotate({
    description: "Enable or disable the MCP server on startup",
  }),
  headers: Schema.optional(Schema.Record(Schema.String, Schema.String)).annotate({
    description: "Headers to send with the request",
  }),
  oauth: Schema.optional(Schema.Union([OAuth, Schema.Literal(false)])).annotate({
    description: "OAuth authentication configuration for the MCP server. Set to false to disable OAuth auto-detection.",
  }),
  timeout: Schema.optional(PositiveInt).annotate({
    description: "Legacy timeout in ms used when a phase-specific timeout is not configured.",
  }),
  connect_timeout: Schema.optional(PositiveInt).annotate({
    description: "Timeout in ms for connecting to the MCP server. Defaults to 30000.",
  }),
  discovery_timeout: Schema.optional(PositiveInt).annotate({
    description: "Timeout in ms for MCP tool discovery. Defaults to 30000.",
  }),
  tool_timeout: Schema.optional(PositiveInt).annotate({
    description: "Timeout in ms for MCP tool execution.",
  }),
  async_tools: Schema.optional(Schema.mutable(Schema.Array(Schema.String))).annotate({
    description: "MCP tool names that should run as asynchronous jobs.",
  }),
}).annotate({ identifier: "McpRemoteConfig" })
export type Remote = Schema.Schema.Type<typeof Remote>

export const Info = Schema.Union([Local, Remote]).annotate({ discriminator: "type" })
export type Info = Schema.Schema.Type<typeof Info>

export * as ConfigMCP from "./mcp"
