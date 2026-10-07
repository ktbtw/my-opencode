import { describe, expect, test } from "bun:test"
import { openaiImageUrl } from "@opencode-ai/core/github-copilot/openai-media-url"
import { convertToOpenAIResponsesInput } from "@opencode-ai/core/github-copilot/responses/convert-to-openai-responses-input"
import { convertToOpenAICompatibleChatMessages } from "@opencode-ai/core/github-copilot/chat/convert-to-openai-compatible-chat-messages"

const PNG_BASE64 = "AAECAw=="
const PNG_DATA_URL = `data:image/png;base64,${PNG_BASE64}`

describe("openaiImageUrl", () => {
  test("keeps data URLs as-is", () => {
    expect(openaiImageUrl(PNG_DATA_URL, "image/png")).toBe(PNG_DATA_URL)
  })

  test("keeps http URLs as-is", () => {
    expect(openaiImageUrl("https://example.com/a.png", "image/png")).toBe("https://example.com/a.png")
  })

  test("prefixes raw base64", () => {
    expect(openaiImageUrl(PNG_BASE64, "image/png")).toBe(PNG_DATA_URL)
  })

  test("encodes Uint8Array", () => {
    expect(openaiImageUrl(new Uint8Array([0, 1, 2, 3]), "image/png")).toBe(PNG_DATA_URL)
  })

  test("stringifies URL objects", () => {
    expect(openaiImageUrl(new URL("https://example.com/a.png"), "image/png")).toBe("https://example.com/a.png")
  })
})

describe("convertToOpenAIResponsesInput images", () => {
  test("does not double-prefix data URL file parts from convertToModelMessages", async () => {
    const { input } = await convertToOpenAIResponsesInput({
      prompt: [
        {
          role: "user",
          content: [
            { type: "text", text: "Describe this image" },
            { type: "file", mediaType: "image/png", filename: "image.png", data: PNG_DATA_URL },
          ],
        },
      ],
      systemMessageMode: "system",
      store: false,
    })

    const user = input[0] as { role: string; content: Array<{ type: string; text?: string; image_url?: string }> }
    expect(user.role).toBe("user")
    expect(user.content[0]).toEqual({ type: "input_text", text: "Describe this image" })
    expect(user.content[1]?.type).toBe("input_image")
    expect(user.content[1]?.image_url).toBe(PNG_DATA_URL)
    expect(user.content[1]?.image_url?.startsWith("data:image/png;base64,data:")).toBe(false)
  })
})

describe("convertToOpenAICompatibleChatMessages images", () => {
  test("does not double-prefix data URL file parts", () => {
    const result = convertToOpenAICompatibleChatMessages([
      {
        role: "user",
        content: [
          { type: "text", text: "Hello" },
          { type: "file", mediaType: "image/png", filename: "image.png", data: PNG_DATA_URL },
        ],
      },
    ])

    expect(result).toEqual([
      {
        role: "user",
        content: [
          { type: "text", text: "Hello" },
          { type: "image_url", image_url: { url: PNG_DATA_URL } },
        ],
      },
    ])
  })
})
