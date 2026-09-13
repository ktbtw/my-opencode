import { describe, expect, test } from "bun:test"
import { mergeMCPEnvironment } from "../../src/mcp/environment"

describe("mergeMCPEnvironment", () => {
  test("placeholder credentials inherit real Agent environment values", () => {
    const result = mergeMCPEnvironment(
      {
        VERIFY_API_TOKEN: "vat_real_token",
        VERIFY_PROTECT_TOKEN: "vpt_real_token",
      },
      {
        VERIFY_API_TOKEN: "vat_xxx_replace_me",
        VERIFY_PROTECT_TOKEN: "vpt_xxx",
        VERIFY_BASE_URL: "https://verify.example.com",
      },
    )

    expect(result.VERIFY_API_TOKEN).toBe("vat_real_token")
    expect(result.VERIFY_PROTECT_TOKEN).toBe("vpt_real_token")
    expect(result.VERIFY_BASE_URL).toBe("https://verify.example.com")
  })

  test("real MCP-specific credentials still override inherited values", () => {
    const result = mergeMCPEnvironment({ VERIFY_API_TOKEN: "vat_global_token" }, { VERIFY_API_TOKEN: "vat_mcp_token" })

    expect(result.VERIFY_API_TOKEN).toBe("vat_mcp_token")
  })

  test("placeholder credentials stay absent when no inherited value exists", () => {
    const result = mergeMCPEnvironment({}, { VERIFY_API_TOKEN: "vat_xxx" })

    expect(result).not.toHaveProperty("VERIFY_API_TOKEN")
  })
})
