// 提示词管理页：平台默认槽位 + 语义 Agent 覆盖。
// 复用管理端登录态（localStorage 的 chat_codex_admin_token）与 API base 规则。
import { safeBrowserStorage, defaultAPIBase, readJSONResponse } from "./session.mjs"

const storage = safeBrowserStorage(window)
const base = defaultAPIBase(window.location)
const token = storage.get("chat_codex_admin_token")

const statusEl = document.getElementById("status")
const listEl = document.getElementById("slotList")
const overrideEl = document.getElementById("overrideList")
const agentSelect = document.getElementById("agentSelect")
const categoryNames = { chat: "主对话", mode: "模式", job: "内部任务", message: "消息片段" }

let slots = []
let agents = []

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

function slotCard({ id, meta, value, placeholder, rows = 10, buttonText, onSave }) {
  const card = document.createElement("section")
  card.className = "prompt-card"
  const head = document.createElement("header")
  const title = document.createElement("strong")
  title.textContent = id
  const metaEl = document.createElement("span")
  metaEl.className = "prompt-meta"
  metaEl.textContent = meta
  head.append(title, metaEl)

  const textarea = document.createElement("textarea")
  textarea.rows = rows
  textarea.value = value || ""
  textarea.placeholder = placeholder

  const actions = document.createElement("div")
  actions.className = "prompt-actions"
  const save = document.createElement("button")
  save.type = "button"
  save.textContent = buttonText
  save.onclick = async () => {
    save.disabled = true
    try {
      await onSave(textarea.value)
    } catch (error) {
      setStatus(`${id} 保存失败：${error.message}`, "error")
    } finally {
      save.disabled = false
    }
  }
  actions.append(save)
  card.append(head, textarea, actions)
  return card
}

function renderPlatformSlots() {
  listEl.replaceChildren()
  const grouped = new Map()
  for (const slot of slots) {
    const key = slot.category || "chat"
    if (!grouped.has(key)) grouped.set(key, [])
    grouped.get(key).push(slot)
  }
  for (const [category, items] of grouped) {
    const heading = document.createElement("h2")
    heading.textContent = `${categoryNames[category] || category}（${items.length}）`
    listEl.append(heading)
    for (const slot of items) {
      const overrides = (slot.overrides || []).map((item) => item.semantic_agent_name || item.semantic_agent_id)
      listEl.append(
        slotCard({
          id: slot.id,
          meta: [
            `层级 ${(slot.scope || []).join("/")}`,
            slot.risk === "low" ? "风险低" : `风险 ${slot.risk}`,
            slot.platform_set ? `平台值已设置（rev ${slot.revision}）` : "未设置平台值（用内置默认）",
            overrides.length ? `语义 Agent 覆盖：${overrides.join("、")}` : "无语义 Agent 覆盖",
          ].join(" · "),
          value: slot.platform_value,
          placeholder: `留空表示使用内置默认。内置默认预览：\n${(slot.default || "").slice(0, 300)}`,
          buttonText: "保存平台默认",
          onSave: async (value) => {
            await api(`/api/admin/prompt-templates/${encodeURIComponent(slot.id)}`, {
              method: "POST",
              body: JSON.stringify({ content: value }),
            })
            setStatus(`${slot.id} 已保存并推送到在线设备`, "ok")
            await load()
          },
        }),
      )
    }
  }
  const resetRow = document.createElement("div")
  resetRow.className = "prompt-actions"
  for (const slot of slots) {
    const reset = document.createElement("button")
    reset.type = "button"
    reset.className = "secondary"
    reset.textContent = `恢复 ${slot.id} 内置默认`
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
    resetRow.append(reset)
  }
  listEl.append(resetRow)
}

function currentAgent() {
  return agents.find((item) => item.id === agentSelect.value)
}

function renderOverrides() {
  if (!overrideEl || !agentSelect) return
  const agent = currentAgent()
  overrideEl.replaceChildren()
  if (!agent) {
    const empty = document.createElement("p")
    empty.className = "prompt-meta"
    empty.textContent = "暂无可编辑的语义 Agent。"
    overrideEl.append(empty)
    return
  }
  for (const slot of slots.filter((item) => (item.scope || []).includes("semantic"))) {
    const overridden = (agent.prompt_templates || {})[slot.id]
    overrideEl.append(
      slotCard({
        id: slot.id,
        meta: overridden ? "已覆盖该语义 Agent（留空保存即恢复继承）" : "继承平台默认",
        value: overridden || "",
        placeholder: "留空 = 继承平台默认",
        rows: 8,
        buttonText: `保存到「${agent.name || agent.id}」`,
        onSave: async (value) => {
          const next = { ...(agent.prompt_templates || {}) }
          if (value.trim()) next[slot.id] = value
          else delete next[slot.id]
          await api(`/api/admin/semantic-agents/${encodeURIComponent(agent.id)}`, {
            method: "POST",
            body: JSON.stringify({ ...agent, prompt_templates: next }),
          })
          setStatus(`${agent.name || agent.id} 的 ${slot.id} 覆盖已保存并推送`, "ok")
          await load()
        },
      }),
    )
  }
}

async function load() {
  setStatus("加载中…")
  const [registry, semantic] = await Promise.all([api("/api/admin/prompt-registry"), api("/api/admin/semantic-agents")])
  slots = Array.isArray(registry.items) ? registry.items : []
  agents = Array.isArray(semantic.items) ? semantic.items : []
  const selected = agentSelect ? agentSelect.value : ""
  if (agentSelect) {
    agentSelect.replaceChildren(
      ...agents.map((item) => {
        const option = document.createElement("option")
        option.value = item.id
        option.textContent = item.name || item.id
        return option
      }),
    )
    if (selected && agents.some((item) => item.id === selected)) agentSelect.value = selected
  }
  renderPlatformSlots()
  renderOverrides()
  setStatus(`共 ${slots.length} 个槽位、${agents.length} 个语义 Agent；保存后自动推送到在线设备，下一条消息生效`, "ok")
}

if (agentSelect) agentSelect.onchange = () => renderOverrides()

if (!token) {
  setStatus("未登录：请先回到控制台登录后再打开本页", "error")
} else {
  load().catch((error) => setStatus(`加载失败：${error.message}`, "error"))
}
