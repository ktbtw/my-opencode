import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js"
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js"
import { z } from "zod/v4"

const server = new McpServer({
  name: "opencode-test-tools",
  version: "1.0.0",
})

server.registerTool(
  "search_docs",
  {
    description: "Search docs",
    inputSchema: {
      query: z.string().describe("Search query"),
    },
  },
  async ({ query }) => ({
    content: [{ type: "text", text: `query=${query}` }],
  }),
)

await server.connect(new StdioServerTransport())
