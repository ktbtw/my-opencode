// 设备 Agent 级提示词覆盖：写入 device_settings（key = prompt_template.<slot>）。
// 独立模块，复用同一登录态；保存后后端会在下一次解析/推送时使用该覆盖。
import { safeBrowserStorage, defaultAPIBase, readJSONResponse } from "./session.mjs"

const storage = safeBrowserStorage(window)
const base = defaultAPIBase(window.location)
const token = storage.get("chat_codex_admin_token")
const statusEl = document.getElementById("status")
const selectEl = document.getElementById("deviceAgentSelect")
const listEl = document.getElementById("deviceOverrideList")

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

let agents = []
let slots = []

function agentLabel(machine, agent) {
  const device = machine.name || machine.hostname || machine.machine_id || "未知设备"
  return `${device} / ${agent.name || agent.agent_id}`
}

async function load() {
  if (!selectEl || !listEl) return
  const [devices, registry] = await Promise.all([api("/api/devices"), api("/api/admin/prompt-registry")])
  slots = (Array.isArray(registry.items) ? registry.items : []).filter((slot) => (slot.scope || []).includes("device"))
  agents = []
  for (const machine of Array.isArray(devices.items) ? devices.items : []) {
    for (const agent of Array.isArray(machine.agents) ? machine.agents : []) {
      agents.push({ id: agent.agent_id, label: agentLabel(machine, agent), semantic: agent.semantic_agent_name || agent.semantic_agent_id || "" })
    }
  }
  const selected = selectEl.value
  selectEl.replaceChildren(
    ...agents.map((item) => {
      const option = document.createElement("option")
      option.value = item.id
      option.textContent = item.label
      return option
    }),
  )
  if (selected && agents.some((item) => item.id === selected)) selectEl.value = selected
  render()
}

function render() {
  if (!listEl) return
  const agent = agents.find((item) => item.id === selectEl.value)
  listEl.replaceChildren()
  if (!agent) {
    const empty = document.createElement("p")
    empty.className = "prompt-meta"
    empty.textContent = "没有可编辑的设备 Agent。"
    listEl.append(empty)
    return
  }
  const hint = document.createElement("p")
  hint.className = "prompt-meta"
  hint.textContent = `目标：${agent.label}${agent.semantic ? `（当前语义 Agent：${agent.semantic}）` : ""}。留空保存 = 清除该设备 Agent 的覆盖并回到继承。`
  listEl.append(hint)
  for (const slot of slots) {
    const card = document.createElement("section")
    card.className = "prompt-card"
    const head = document.createElement("header")
    const title = document.createElement("strong")
    title.textContent = slot.id
    const meta = document.createElement("span")
    meta.className = "prompt-meta"
    meta.textContent = "设备 Agent 覆盖（优先级最高）"
    head.append(title, meta)
    const textarea = document.createElement("textarea")
    textarea.rows = 8
    textarea.placeholder = "留空 = 继承语义 Agent / 平台默认"
    const actions = document.createElement("div")
    actions.className = "prompt-actions"
    const save = document.createElement("button")
    save.type = "button"
    save.textContent = "保存到该设备 Agent"
    save.onclick = async () => {
      save.disabled = true
      try {
        await api(`/api/agents/${encodeURIComponent(agent.id)}/settings`, {
          method: "POST",
          body: JSON.stringify({ [`prompt_template.${slot.id}`]: textarea.value }),
        })
        setStatus(`${agent.label} 的 ${slot.id} 覆盖已保存（下次推送/重启生效）`, "ok")
      } catch (error) {
        setStatus(`保存失败：${error.message}`, "error")
      } finally {
        save.disabled = false
      }
    }
    actions.append(save)
    card.append(head, textarea, actions)
    listEl.append(card)
  }
}

if (selectEl) selectEl.onchange = () => render()
if (!token) {
  if (statusEl) setStatus("未登录：请先回到控制台登录后再打开本页", "error")
} else {
  load().catch((error) => setStatus(`设备 Agent 覆盖加载失败：${error.message}`, "error"))
}
