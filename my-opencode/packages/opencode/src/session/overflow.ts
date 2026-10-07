import type { Config } from "@/config/config"
import type { Provider } from "@/provider/provider"
import { ProviderTransform } from "@/provider/transform"
import type { MessageV2 } from "./message-v2"

const COMPACTION_BUFFER = 20_000
const DEFAULT_COMPACTION_THRESHOLD_PERCENT = 80

export function thresholdPercent(cfg: Config.Info) {
  const configured = cfg.compaction?.threshold_percent
  if (configured === undefined) return DEFAULT_COMPACTION_THRESHOLD_PERCENT
  return Math.min(100, Math.max(1, configured))
}

export function usable(input: { cfg: Config.Info; model: Provider.Model; outputTokenMax?: number }) {
  const context = input.model.limit.context
  if (context === 0) return 0

  const reserved =
    input.cfg.compaction?.reserved ??
    Math.min(COMPACTION_BUFFER, ProviderTransform.maxOutputTokens(input.model, input.outputTokenMax))
  const safeLimit = input.model.limit.input
    ? Math.max(0, input.model.limit.input - reserved)
    : Math.max(0, context - ProviderTransform.maxOutputTokens(input.model, input.outputTokenMax))
  const thresholdLimit = Math.floor(context * (thresholdPercent(input.cfg) / 100))
  return Math.max(0, Math.min(safeLimit, thresholdLimit))
}

export function count(tokens: MessageV2.Assistant["tokens"]) {
  return tokens.total || tokens.input + tokens.output + tokens.reasoning + tokens.cache.read + tokens.cache.write
}

export function isOverflow(input: {
  cfg: Config.Info
  tokens: MessageV2.Assistant["tokens"]
  model: Provider.Model
  outputTokenMax?: number
}) {
  if (input.cfg.compaction?.auto === false) return false
  if (input.model.limit.context === 0) return false

  return count(input.tokens) >= usable(input)
}
