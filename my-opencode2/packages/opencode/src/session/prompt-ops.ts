import type { MessageV2 } from "./message-v2"
import type { SessionPrompt } from "./prompt"
import type { SessionID } from "./schema"
import type { Effect } from "effect"

export interface PromptOps {
  cancel(sessionID: SessionID): Effect.Effect<void>
  resolvePromptParts(template: string): Effect.Effect<SessionPrompt.PromptInput["parts"]>
  prompt(input: SessionPrompt.PromptInput): Effect.Effect<MessageV2.WithParts>
  loop(input: SessionPrompt.LoopInput): Effect.Effect<MessageV2.WithParts>
}

export * as SessionPromptOps from "./prompt-ops"
