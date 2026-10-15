/**
 * 思考（reasoning）阶段的护栏。
 *
 * 放开单次输出上限后，独跑思考的步不再被 32k 输出上限截断，代价是失去那道"隐式刹车"：
 * 模型如果陷在思考里出不来了，会一路烧到输出上限或请求超时。
 *
 * 判据来自实测（9 个真实样本，含 Windows 上 327,495 字符的极端案例）：
 * - "打转"的文字形态是"反复重来"（Let me reconsider / recheck / look / check…），
 *   但**正常收敛的长思考密度一样高**（Let me 每万字符 12.9–20.4 vs 13.4–23.5），
 *   逐字重复也**一次都没有**（400 字符窗口在真实数据里 0 命中）；
 * - 唯一能把两者分开的客观标志：**思考烧满预算，而这一步正文 0 字符**。
 *
 * 所以主判据是"预算 + 无产出"，文本层面的重复只作为退化输出的兜底：
 *
 * 1. `isEcho`  —— 供应商把整段思考又当正文重发（截断后出现过），按思考处理，不进正文；
 * 2. `budgetExceeded` / `stalled` —— 超过预算（或已用一半以上且正文为空）→ 中止 + 可执行建议；
 * 3. `isLooping` —— 退化兜底：同一段文字被逐字重复输出。注意它**不是**长任务"打转"的判据。
 *
 * 判定全部是纯函数，便于单测；阈值都可由调用方传入。
 */

/**
 * 思考预算占单次输出上限的比例。
 *
 * 实测数据定这个数：
 * - 合法长任务：Mac 上正常收敛的几步思考 30–45k token，Windows 上最长一步 82k token；
 * - deepseek-v4.1-flash 声明的输出上限是 384k（opencode 官方档），免费档/部分路由 32k。
 * 取输出上限的 75%：不会误杀上面的合法长思考，又能在"几乎烧完整个输出预算、
 * 却一个正文字符都没产出"时收手（这正是实测里"反复重来、始终不收尾"的那种形态）。
 */
export const REASONING_BUDGET_RATIO = 0.75

/** 兜底预算：拿不到输出上限时使用（对应默认 128k 上限的 75%）。 */
export const DEFAULT_REASONING_TOKEN_BUDGET = 96_000

/** 按单次输出上限推思考预算。 */
export function defaultBudget(outputTokenMax: number) {
  if (!Number.isFinite(outputTokenMax) || outputTokenMax <= 0) return DEFAULT_REASONING_TOKEN_BUDGET
  return Math.max(1, Math.floor(outputTokenMax * REASONING_BUDGET_RATIO))
}

/** 循环判定窗口：尾部多少字符作为指纹。 */
const LOOP_WINDOW = 400
/** 循环判定回看范围：在最近多少字符里数指纹出现次数。 */
const LOOP_LOOKBACK = 40_000
/** 同一指纹出现多少次判为循环。 */
const LOOP_REPEATS = 3

/** 字符与 token 的换算沿用 Token.estimate 的口径（4 字符 ≈ 1 token）。 */
export function charsForTokens(tokens: number) {
  return Math.max(0, Math.round(tokens * 4))
}

/**
 * 正文其实是思考的重发。两种形态都见过：
 * - 整段相同（Mac 上 15 次样本都是这个）；
 * - **正文是思考里的一大段**（Windows gg同行：5 次里 4 次是这种，正文 82k–88k 字符、
 *   原样出现在该步 179k–478k 字符的思考里）。
 * 所以判定放宽到"包含"，但要求正文足够长（正常的回答不会整段从思考里搬）。
 */
export const ECHO_MIN_CHARS = 2_000

export function isEcho(text: string, reasoning: string) {
  const body = text.trim()
  const think = reasoning.trim()
  if (!body.length || !think.length) return false
  if (body === think) return true
  return body.length >= ECHO_MIN_CHARS && think.includes(body)
}

/** 思考是否已超出预算。 */
export function budgetExceeded(reasoning: string, budgetTokens: number) {
  if (budgetTokens <= 0) return false
  return reasoning.length >= charsForTokens(budgetTokens)
}

/** 思考是否已经用了预算的告警线（默认 60%）。 */
export function budgetWarning(reasoning: string, budgetTokens: number, ratio = 0.6) {
  if (budgetTokens <= 0) return false
  return reasoning.length >= charsForTokens(Math.floor(budgetTokens * ratio))
}

/**
 * 退化兜底：思考尾部指纹在最近一段里被逐字重复输出。
 * 实测真实"打转"数据 0 命中——它抓的是"卡在同一段文字上刷屏"这种退化，不是长任务打转。
 */
export function isLooping(reasoning: string) {
  if (reasoning.length < LOOP_WINDOW * LOOP_REPEATS) return false
  const tail = reasoning.slice(-LOOP_WINDOW)
  const from = Math.max(0, reasoning.length - LOOP_LOOKBACK)
  const scope = reasoning.slice(from)
  let count = 0
  let index = scope.indexOf(tail)
  while (index >= 0 && count < LOOP_REPEATS) {
    count += 1
    // 按窗口长度前进：同一串重复字符（分隔线之类）不会被反复计成多次命中。
    index = scope.indexOf(tail, index + LOOP_WINDOW)
  }
  return count >= LOOP_REPEATS
}

/** 一步思考的护栏结论：主判据是预算，重复只做退化兜底。 */
export function inspect(input: { reasoning: string; budgetTokens: number }) {
  if (isLooping(input.reasoning)) return { abandon: true as const, reason: "repeat" as const }
  if (budgetExceeded(input.reasoning, input.budgetTokens)) return { abandon: true as const, reason: "budget" as const }
  return { abandon: false as const, reason: undefined }
}

/**
 * 该告警的形态：思考已经用掉预算的一半以上，而这一步还没有产出任何正文。
 * 实测的"打转"就是这种：反复决定、反复重来，始终不收敛。
 */
export function stalled(input: { reasoning: string; budgetTokens: number; hasContent: boolean }) {
  return !input.hasContent && budgetWarning(input.reasoning, input.budgetTokens)
}

export function abandonMessage(reason: "repeat" | "budget", budgetTokens: number) {
  const head =
    reason === "repeat"
      ? "本步思考出现大段逐字重复（退化输出），已中止。"
      : `本步思考已用掉 ${budgetTokens} token 预算且没有产出正文，已中止。`
  return `${head}建议：把思考强度调低（max → high/low）、把任务拆小，或在指令里给出明确的收敛条件。`
}

export * as ReasoningGuard from "./reasoning-guard"
