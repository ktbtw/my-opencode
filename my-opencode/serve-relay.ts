// Minimal serve script for opencode relay mode
import { Server } from "./packages/opencode/src/server/server"

const server = Server.listen({
  port: 4096,
  hostname: "127.0.0.1",
  mdns: false,
  cors: [],
})

console.log(`opencode server listening on http://${server.hostname}:${server.port}`)
await new Promise(() => {})
