import { Effect, Schema } from "effect"
import * as Tool from "./tool"
import { Question } from "../question"
import DESCRIPTION from "./question.txt"

export const AskReason = Schema.Literals([
  "required_user_input",
  "irreversible_user_decision",
  "material_preference_conflict",
])

export type AskReason = Schema.Schema.Type<typeof AskReason>

export const Parameters = Schema.Struct({
  reason: AskReason.annotate({
    description:
      "Why user participation is required: required_user_input, irreversible_user_decision, or material_preference_conflict",
  }),
  blocking_context: Schema.String.annotate({
    description: "Specific missing input or decision and why a reasonable default cannot resolve it",
  }),
  attempted_steps: Schema.mutable(Schema.Array(Schema.String)).annotate({
    description: "Concrete steps already attempted to resolve the issue without asking the user",
  }),
  questions: Schema.mutable(Schema.Array(Question.Prompt)).annotate({ description: "Questions to ask" }),
})

type Metadata = {
  answers: ReadonlyArray<Question.Answer>
  reason: AskReason
  blocking_context: string
  attempted_steps: ReadonlyArray<string>
}

export const QuestionTool = Tool.define<typeof Parameters, Metadata, Question.Service>(
  "question",
  Effect.gen(function* () {
    const question = yield* Question.Service

    return {
      description: DESCRIPTION,
      parameters: Parameters,
      execute: (params: Schema.Schema.Type<typeof Parameters>, ctx: Tool.Context<Metadata>) =>
        Effect.gen(function* () {
          const blockingContext = params.blocking_context.trim()
          const attemptedSteps = params.attempted_steps.map((step) => step.trim()).filter(Boolean)
          if (!blockingContext || attemptedSteps.length === 0 || params.questions.length === 0) {
            return {
              title: "Question rejected",
              output: "提问请求缺少具体阻塞原因或已尝试步骤。请先自行处理，并仅在确实需要用户参与时重新提交提问。",
              metadata: {
                answers: [],
                reason: params.reason,
                blocking_context: blockingContext,
                attempted_steps: attemptedSteps,
              },
            }
          }

          const answers = yield* question.ask({
            sessionID: ctx.sessionID,
            questions: params.questions,
            tool: ctx.callID ? { messageID: ctx.messageID, callID: ctx.callID } : undefined,
          })

          const formatted = params.questions
            .map((q, i) => `"${q.question}"="${answers[i]?.length ? answers[i].join(", ") : "Unanswered"}"`)
            .join(", ")

          return {
            title: `Asked ${params.questions.length} question${params.questions.length > 1 ? "s" : ""}`,
            output: `User has answered your questions: ${formatted}. You can now continue with the user's answers in mind.`,
            metadata: {
              answers,
              reason: params.reason,
              blocking_context: blockingContext,
              attempted_steps: attemptedSteps,
            },
          }
        }).pipe(Effect.orDie),
    }
  }),
)
