// 提示词管理页：读取/保存提示词注册表的平台默认值。
// 复用管理端登录态（localStorage 的 chat_codex_admin_token）与 API base 规则。
import { safeBrowserStorage, defaultAPIBase, readJSONResponse } from "./session.mjs"

const storage = safeBrowserStorage(window)
const base = defaultAPIBase(window.location)
const token = storage.get("chat_codex_admin_token")

const statusEl = document.getElementById("status")
const listEl = document.getElementById("slotList")
const categoryNames = { chat: "主对话", mode: "模式", job: "内部任务", message: "消息片段" }

function setStatus(text, kind = "") {
  statusEl.textContent = text
  statusEl.className = `prompt-status ${kind}`
}

async function api(path, options = {}) {
  const response = await fetch(`${base}${path}`, {
    ...options,
    headers: {
      "Content-Type": "application/json",
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...(options.headers || {}),
    },
  })
  if (response.status === 401 || response.status === 403) throw new Error("登录态失效，请回到控制台重新登录")
  return readJSONResponse(response)
}

function renderSlot(slot) {
  const card = document.createElement("section")
  card.className = "prompt-card"

  const head = document.createElement("header")
  const title = document.createElement("strong")
  title.textContent = slot.id
  const meta = document.createElement("span")
  meta.className = "prompt-meta"
  const overrides = (slot.overrides || []).map((item) => item.semantic_agent_name || item.semantic_agent_id)
  meta.textContent = [
    `层级 ${(slot.scope || []).join("/")}`,
    slot.risk === "low" ? "风险低" : `风险 ${slot.risk}`,
    slot.platform_set ? `平台值已设置（rev ${slot.revision}）` : "未设置平台值（用内置默认）",
    overrides.length ? `语义 Agent 覆盖：${overrides.join("、")}` : "无语义 Agent 覆盖",
  ].join(" · ")
  head.append(title, meta)

  const textarea = document.createElement("textarea")
  textarea.rows = 12
  textarea.value = slot.platform_value || ""
  textarea.placeholder = `留空表示使用内置默认。内置默认（只读预览）：\n${(slot.default || "").slice(0, 400)}`

  const actions = document.createElement("div")
  actions.className = "prompt-actions"
  const save = document.createElement("button")
  save.type = "button"
  save.textContent = "保存平台默认"
  save.onclick = async () => {
    save.disabled = true
    try {
      await api(`/api/admin/prompt-templates/${encodeURIComponent(slot.id)}`, {
        method: "POST",
        body: JSON.stringify({ content: textarea.value }),
      })
      setStatus(`${slot.id} 已保存并推送到在线设备`, "ok")
      await load()
    } catch (error) {
      setStatus(`${slot.id} 保存失败：${error.message}`, "error")
    } finally {
      save.disabled = false
    }
  }
  const reset = document.createElement("button")
  reset.type = "button"
  reset.className = "secondary"
  reset.textContent = "恢复内置默认"
  reset.onclick = async () => {
    reset.disabled = true
    try {
      await api(`/api/admin/prompt-templates/${encodeURIComponent(slot.id)}/reset`, { method: "POST" })
      setStatus(`${slot.id} 已恢复内置默认`, "ok")
      await load()
    } catch (error) {
      setStatus(`${slot.id} 恢复失败：${error.message}`, "error")
    } finally {
      reset.disabled = false
    }
  }
  actions.append(save, reset)
  card.append(head, textarea, actions)
  return card
}

async function load() {
  setStatus("加载中…")
  const data = await api("/api/admin/prompt-registry")
  const items = Array.isArray(data.items) ? data.items : []
  listEl.replaceChildren()
  const grouped = new Map()
  for (const slot of items) {
    const key = slot.category || "chat"
    if (!grouped.has(key)) grouped.set(key, [])
    grouped.get(key).push(slot)
  }
  for (const [category, slots] of grouped) {
    const heading = document.createElement("h2")
    heading.textContent = `${categoryNames[category] || category}（${slots.length}）`
    listEl.append(heading, ...slots.map(renderSlot))
  }
  setStatus(`共 ${items.length} 个槽位；改动保存后自动推送到在线设备，下一条消息生效`, "ok")
}

if (!token) {
  setStatus("未登录：请先回到控制台登录后再打开本页", "error")
} else {
  load().catch((error) => setStatus(`加载失败：${error.message}`, "error"))
}
