import './styles.css'
import appIconUrl from '../favicon.png'
import {
  ApplySelfUpdate,
  CheckSelfUpdate,
  ClearSavedLogin,
  CreateAgent,
  CreateSSHTunnel,
  Defaults,
  DirectoryPermission,
  Directories,
  LoadSavedLogin,
  Login,
  InstallSSHAuthorizedKey,
  MCPStatus,
  PrepareRuntimeEnvironment,
  RemoveAgent,
  RestartBackground,
  RestartAgent,
  RuntimeEnvironment,
  SaveSavedLogin,
  SetAgentEnabled,
  SetAutostart,
  SetTerminalCompatEnabled,
  SetupSSH,
  SSHStatus,
  StartLauncher,
  State,
  TerminalCompatSettings,
} from '../wailsjs/go/main/GUIApp'
import {
  EventsOn,
  ClipboardSetText,
  WindowCenter,
  WindowSetMinSize,
  WindowSetSize,
} from '../wailsjs/runtime/runtime'

const app = document.querySelector('#app')

const loginWindow = { width: 520, height: 560, minWidth: 420, minHeight: 520 }
const mainWindow = { width: 1180, height: 760, minWidth: 960, minHeight: 640 }

function detectSSHClientPlatform() {
  const platform = [
    navigator.userAgentData?.platform,
    navigator.userAgent,
    navigator.platform,
  ].filter(Boolean).join(' ')
  return /Windows/i.test(platform) ? 'windows' : 'unix'
}

const state = {
  defaults: { server_url: 'https://www.xyapi.top/codex', launcher_version: '' },
  authenticated: false,
  ready: false,
  device: null,
  logs: [],
  loading: false,
  error: '',
  tab: 'overview',
  createDialog: false,
  createName: '',
  createPath: '',
  dirPath: '',
  dirEntries: [],
  dirParent: '',
  enableAutostart: true,
  terminalCompat: true,
  rememberLogin: true,
  loginUsername: '',
  loginPassword: '',
  progress: null,
  runtimeInfo: null,
  runtimeLoading: false,
  mcpDialog: false,
  mcpAgent: null,
  mcpStatus: null,
  mcpLoading: false,
  bootstrapProgress: null,
  sshStatus: null,
  sshLoading: false,
  sshCommand: '',
  sshUnixCommand: '',
  sshWindowsCommand: '',
  sshClientPlatform: detectSSHClientPlatform(),
  sshExpiresAt: '',
  sshCopied: false,
  sshPublicKey: '',
  sshKeyResult: null,
}

let automaticUpdateChecking = false

const statusLabel = {
  running: '运行中',
  stopped: '已停止',
  failed: '异常',
  starting: '启动中',
  stopping: '停止中',
  restarting: '重启中',
  upgrading: '升级中',
}

const actionIcons = {
  start: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M8 5v14l11-7z"/></svg>',
  stop: '<svg viewBox="0 0 24 24" aria-hidden="true"><rect x="6" y="6" width="12" height="12" rx="1.5"/></svg>',
  restart: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M20 11a8 8 0 1 0-2.34 5.66"/><path d="M20 4v7h-7"/></svg>',
  remove: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M3 6h18"/><path d="M8 6V4h8v2"/><path d="M18 6l-1 14H7L6 6"/><path d="M10 11v5"/><path d="M14 11v5"/></svg>',
  power: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 2v10"/><path d="M18.4 6.6a9 9 0 1 1-12.8 0"/></svg>',
  search: '<svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="11" cy="11" r="7"/><path d="M20 20l-4.2-4.2"/></svg>',
  download: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 3v12"/><path d="M7 10l5 5 5-5"/><path d="M5 21h14"/></svg>',
  refresh: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M20 11a8 8 0 1 0-2.34 5.66"/><path d="M20 4v7h-7"/></svg>',
  shield: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 3l7 3v5c0 5-3.5 8-7 10-3.5-2-7-5-7-10V6l7-3z"/><path d="M9 12l2 2 4-5"/></svg>',
  terminal: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 17l6-6-6-6"/><path d="M12 19h8"/></svg>',
  check: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M20 6L9 17l-5-5"/></svg>',
  extension: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M9 3h6v5h2a3 3 0 0 1 0 6h-2v7H9v-7H7a3 3 0 0 1 0-6h2V3z"/></svg>',
  copy: '<svg viewBox="0 0 24 24" aria-hidden="true"><rect x="9" y="9" width="11" height="11" rx="2"/><path d="M15 9V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v7a2 2 0 0 0 2 2h3"/></svg>',
}

const actionIconMap = {
  permission: 'shield',
}

function escapeHtml(value) {
  return String(value ?? '')
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#039;')
}

function escapeJs(value) {
  return String(value ?? '')
    .replaceAll('\\', '\\\\')
    .replaceAll("'", "\\'")
    .replaceAll('\n', '\\n')
    .replaceAll('\r', '\\r')
}

function actionArg(value) {
  return escapeHtml(escapeJs(value))
}

function actionButton(action, agentID, label, tone = '') {
  const icon = actionIcons[actionIconMap[action] || action] || actionIcons.shield
  return `
    <button
      class="icon-action ${tone}"
      title="${escapeHtml(label)}"
      aria-label="${escapeHtml(label)}"
      onclick="gui.agentAction('${actionArg(action)}', '${actionArg(agentID)}')"
    >${icon}</button>
  `
}

function mcpActionButton(agentID) {
  return `
    <button
      class="icon-action"
      title="MCP 状态"
      aria-label="MCP 状态"
      onclick="gui.openMCPDialog('${actionArg(agentID)}')"
    >${actionIcons.extension}</button>
  `
}

function iconCommand(icon, label, onClick, tone = '') {
  const iconSvg = actionIcons[icon] || actionIcons.shield
  return `
    <button
      class="icon-action ${tone}"
      title="${escapeHtml(label)}"
      aria-label="${escapeHtml(label)}"
      onclick="${onClick}"
      ${state.loading ? 'disabled' : ''}
    >${iconSvg}</button>
  `
}

function setState(patch) {
  const onlyProgress = Object.keys(patch).length === 1 && Object.prototype.hasOwnProperty.call(patch, 'progress')
  Object.assign(state, patch)
  if (onlyProgress && updateProgressDialogInPlace()) {
    return
  }
  render()
}

function setError(error) {
  setState({ error: error?.message || String(error || '') })
}

function runtimeReady() {
  return typeof window !== 'undefined' && !!window.runtime
}

function bindingReady() {
  return typeof window !== 'undefined' && !!window.go?.main?.GUIApp
}

function bindRuntimeEvents() {
  if (!runtimeReady()) return
  try {
    EventsOn('launcher:bootstrap-progress', (progress) => {
      applyBootstrapProgress(progress)
    })
  } catch {
    // Plain browser preview has no Wails event bridge.
  }
}

function setWindowMode(mode) {
  if (!runtimeReady()) return
  const target = mode === 'main' ? mainWindow : loginWindow
  try {
    WindowSetMinSize(target.minWidth, target.minHeight)
    WindowSetSize(target.width, target.height)
    WindowCenter()
  } catch {
    // Wails runtime is unavailable during plain browser preview.
  }
}

function delay(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

function progressState(title, detail, steps) {
  return {
    title,
    detail,
    percent: 6,
    active: 0,
    complete: false,
    error: '',
    runtime: null,
    steps: steps.map((label, index) => ({
      label,
      status: index === 0 ? 'active' : 'pending',
    })),
  }
}

function updateProgress(patch) {
  if (!state.progress) return
  setState({ progress: { ...state.progress, ...patch } })
}

function updateProgressStep(index, detail = '') {
  if (!state.progress) return
  const steps = state.progress.steps.map((step, stepIndex) => {
    if (stepIndex < index) return { ...step, status: 'done' }
    if (stepIndex === index) return { ...step, status: 'active' }
    return { ...step, status: 'pending' }
  })
  const percent = Math.max(8, Math.round(((index + 0.35) / Math.max(steps.length, 1)) * 100))
  setState({
    progress: {
      ...state.progress,
      active: index,
      detail: detail || state.progress.detail,
      percent,
      steps,
    },
  })
}

const bootstrapPhaseStep = {
  loading_config: 0,
  configuring_autostart: 0,
  checking_existing: 1,
  starting_autostart: 1,
  installing_launcher: 2,
  starting_background: 3,
  waiting_control: 4,
  checking_update: 5,
  self_update_checking: 5,
  self_update_downloading: 6,
  self_update_applying: 7,
  self_update_restarting: 8,
  ready: 9,
  failed: 9,
}

function applyBootstrapProgress(progress) {
  if (!progress) return
  setState({ bootstrapProgress: progress })
  if (!state.progress) return
  const phase = String(progress.phase || '').trim()
  const active = bootstrapPhaseStep[phase]
  if (typeof active !== 'number') return
  const failed = phase === 'failed'
  const steps = state.progress.steps.map((step, index) => {
    if (failed && index === active) return { ...step, status: 'active' }
    if (index < active) return { ...step, status: 'done' }
    if (index === active) return { ...step, status: 'active' }
    return { ...step, status: 'pending' }
  })
  const percent = Number(progress.percent || 0)
  updateProgress({
    active,
    steps,
    detail: progress.error || progress.message || state.progress.detail,
    percent: Math.max(state.progress.percent || 0, Math.min(100, percent || state.progress.percent || 0)),
    error: failed ? progress.error || progress.message || '本机服务启动失败' : '',
  })
}

async function finishProgress(detail = '完成') {
  if (!state.progress) return
  const steps = state.progress.steps.map((step) => ({ ...step, status: 'done' }))
  setState({
    progress: {
      ...state.progress,
      detail,
      percent: 100,
      complete: true,
      steps,
    },
  })
  await delay(520)
  setState({ progress: null })
}

function failProgress(error) {
  const message = error?.message || String(error || '操作失败')
  if (!state.progress) {
    setState({ error: message, loading: false })
    return
  }
  setState({
    loading: false,
    error: message,
    progress: {
      ...state.progress,
      detail: message,
      error: message,
    },
  })
}

async function runProgress({ title, detail, steps, done }, task) {
  setState({
    loading: true,
    error: '',
    progress: progressState(title, detail, steps),
  })
  try {
    await task({
      step: updateProgressStep,
      detail: (value) => updateProgress({ detail: value }),
    })
    setState({ loading: false })
    await finishProgress(done || '完成')
  } catch (error) {
    failProgress(error)
    throw error
  }
}

async function loadDefaults() {
  if (!bindingReady()) return
  try {
    const defaults = await Defaults()
    let saved = null
    let guiSettings = null
    try {
      saved = await LoadSavedLogin()
    } catch {
      saved = null
    }
    try {
      guiSettings = await TerminalCompatSettings()
    } catch {
      guiSettings = null
    }
    const patch = { defaults }
    if (guiSettings) {
      patch.terminalCompat = guiSettings.terminal_compat !== false
    }
    if (saved) {
      patch.loginUsername = saved.username || ''
      patch.loginPassword = saved.password || ''
      patch.rememberLogin = !!saved.has_saved && saved.auto_login !== false
    }
    setState(patch)
    if (saved?.has_saved && saved.auto_login !== false && saved.username && saved.password) {
      await loginWithCredentials({
        serverURL: saved.server_url || defaults.server_url || '',
        username: saved.username,
        password: saved.password,
        remember: true,
        auto: true,
      })
    }
  } catch (error) {
    setError(error)
  }
}

async function refreshState() {
  try {
    const result = await State()
    setState({
      ready: !!result.ready,
      device: result.state || null,
      logs: result.logs || [],
      error: result.ready ? '' : state.error,
    })
  } catch (error) {
    setError(error)
  }
}

async function login(event) {
  event.preventDefault()
  const form = new FormData(event.currentTarget)
  await loginWithCredentials({
    serverURL: state.defaults.server_url || '',
    username: String(form.get('username') || ''),
    password: String(form.get('password') || ''),
    remember: state.rememberLogin,
    auto: false,
  })
}

async function loginWithCredentials({ serverURL, username, password, remember, auto }) {
  setState({ loading: true, error: '' })
  try {
    await Login({
      server_url: serverURL || state.defaults.server_url || '',
      username,
      password,
    })
    try {
      if (remember) {
        await SaveSavedLogin({
          server_url: serverURL || state.defaults.server_url || '',
          username,
          password,
          auto_login: true,
        })
      } else {
        await ClearSavedLogin()
      }
    } catch (saveError) {
      console.warn('保存自动登录配置失败', saveError)
    }
    setWindowMode('main')
    setState({
      authenticated: true,
      ready: false,
      device: null,
      logs: [],
      loading: false,
      error: '',
      tab: 'overview',
    })
    bootstrapLauncher()
  } catch (error) {
    setState({ loading: false })
    if (auto) {
      setError(new Error(`自动登录失败，请手动登录：${error?.message || error}`))
      return
    }
    setError(error)
  }
}

async function bootstrapLauncher() {
  try {
    await runProgress({
      title: '正在准备本机服务',
      detail: '正在启动并接管本机 launcher',
      steps: [
        '配置自启动',
        '检查后台',
        '安装组件',
        '启动服务',
        '等待连接',
        '检查更新',
        '下载更新',
        '应用更新',
        '重启服务',
        '同步状态',
        '检查权限',
      ],
      done: '本机服务已就绪',
    }, async (progress) => {
      progress.step(0, state.enableAutostart ? '正在配置后台服务自启动' : '正在接管已有后台服务')
      if (state.enableAutostart) {
        await SetAutostart(true)
      }

      progress.step(1, '正在检查已有后台 launcher')
      const boot = await StartLauncher()
      applyBootstrapProgress(boot.bootstrap)
      let current = boot
      setState({
        ready: !!current.ready,
        device: current.state || null,
        logs: current.logs || [],
      })

      progress.step(9, '正在同步本机 Agent 状态')
      current = await State()
      applyBootstrapProgress(current.bootstrap)
      setState({
        ready: !!current.ready,
        device: current.state || null,
        logs: current.logs || [],
      })

      progress.step(10, '正在检查 Agent 目录访问权限')
      const permission = await requestStartupDirectoryPermission(current.state)
      if (permission && !permission.accessible) {
        const message = permission.message || permission.error || '系统阻止访问 Agent 项目目录，请授权后重启对应 Agent。'
        setState({ error: message })
      }
    })
    checkAutomaticGUIUpdate()
  } catch {
    // 错误已经在进度弹窗和主界面提示中展示。
  }
}

async function checkAutomaticGUIUpdate() {
  if (!state.authenticated || !state.ready || automaticUpdateChecking) return
  automaticUpdateChecking = true
  try {
    const check = await CheckSelfUpdate()
    if (check?.update_available) {
      await ApplySelfUpdate()
    }
  } catch (error) {
    console.warn('自动检查 Launcher GUI 更新失败', error)
  } finally {
    automaticUpdateChecking = false
  }
}

async function requestStartupDirectoryPermission(device) {
  const agents = (device?.agents || []).filter((agent) => agent.agent_id && agent.enabled && agent.status !== 'disabled')
  for (const agent of agents) {
    const result = await DirectoryPermission({ agent_id: agent.agent_id, request: false })
    if (!result.accessible) {
      return result
    }
  }
  return null
}

async function chooseDirectory(path = '') {
  setState({ error: '' })
  try {
    const result = await Directories({ path, allow_all: true })
    setState({
      dirPath: result.current_path || '',
      dirParent: result.parent_path || '',
      dirEntries: result.entries || [],
      createPath: result.current_path || state.createPath,
    })
  } catch (error) {
    setError(error)
  }
}

async function submitCreateAgent(event) {
  event.preventDefault()
  try {
    await runProgress({
      title: '正在创建 Agent',
      detail: '正在写入项目目录并启动本机 Agent',
      steps: ['校验目录', '创建 Agent', '同步状态'],
      done: 'Agent 已创建',
    }, async (progress) => {
      progress.step(0, '正在校验项目目录')
      await delay(160)
      progress.step(1, '正在创建并启动 Agent')
      await CreateAgent({ name: state.createName, project_dir: state.createPath })
      setState({ createDialog: false, createName: '', createPath: '' })
      progress.step(2, '正在同步最新 Agent 状态')
      await refreshState()
    })
  } catch {
    // 错误已经在进度弹窗和主界面提示中展示。
  }
}

async function agentAction(action, agentID) {
  const agent = ((state.device || {}).agents || []).find((item) => item.agent_id === agentID) || { agent_id: agentID }
  const actionText = {
    restart: '重启',
    start: '启动',
    stop: '停止',
    remove: '删除',
    permission: '请求目录权限',
  }[action] || '处理'
  try {
    await runProgress({
      title: `正在${actionText} Agent`,
      detail: `正在处理 ${agent.name || agent.agent_id}`,
      steps: ['发送指令', '等待执行', '同步状态'],
      done: `Agent 已${actionText}`,
    }, async (progress) => {
      progress.step(0, '正在发送控制指令')
      if (action === 'restart') await RestartAgent(agent.agent_id)
      if (action === 'start') await SetAgentEnabled({ agent_id: agent.agent_id, enabled: true })
      if (action === 'stop') await SetAgentEnabled({ agent_id: agent.agent_id, enabled: false })
      if (action === 'remove') await RemoveAgent(agent.agent_id)
      if (action === 'permission') {
        const result = await DirectoryPermission({ agent_id: agent.agent_id, request: true })
        if (!result.accessible && result.message) {
          throw new Error(result.message)
        }
        progress.step(1, '正在重启后台服务让权限生效')
        await RestartBackground()
        await delay(800)
        const verify = await DirectoryPermission({ agent_id: agent.agent_id, request: false })
        if (!verify.accessible && verify.message) {
          throw new Error(verify.message)
        }
      }
      progress.step(1, '正在等待本机服务确认')
      await delay(180)
      progress.step(2, '正在同步最新状态')
      await refreshState()
    })
  } catch {
    // 错误已经在进度弹窗和主界面提示中展示。
  }
}

async function toggleAutostart(enabled) {
  try {
    await runProgress({
      title: enabled ? '正在开启自启动' : '正在关闭自启动',
      detail: '正在写入系统启动项',
      steps: ['读取配置', '写入启动项', '同步状态'],
      done: enabled ? '自启动已开启' : '自启动已关闭',
    }, async (progress) => {
      progress.step(0, '正在读取本机配置')
      await delay(120)
      progress.step(1, enabled ? '正在写入后台服务启动项' : '正在移除启动项')
      await SetAutostart(enabled)
      progress.step(2, '正在同步自启动状态')
      await refreshState()
    })
  } catch {
    // 错误已经在进度弹窗和主界面提示中展示。
  }
}

async function toggleTerminalCompat(enabled) {
  try {
    await runProgress({
      title: enabled ? '正在开启终端兼容模式' : '正在关闭终端兼容模式',
      detail: enabled ? '正在切换后台服务到 Terminal 权限链路' : '正在切换后台服务为直接启动',
      steps: ['保存设置', '重写启动项', '重启后台服务'],
      done: enabled ? '终端兼容模式已开启' : '终端兼容模式已关闭',
    }, async (progress) => {
      progress.step(0, '正在保存 GUI 设置')
      const settings = await SetTerminalCompatEnabled(enabled)
      setState({ terminalCompat: settings.terminal_compat !== false })
      progress.step(1, '正在重写后台服务启动项')
      await delay(250)
      progress.step(2, '正在同步最新后台状态')
      await refreshState()
    })
  } catch {
    // 错误已经在进度弹窗和主界面提示中展示。
  }
}

async function checkUpdate(apply = false) {
  try {
    let message = ''
    await runProgress({
      title: apply ? '正在升级 Launcher' : '正在检查更新',
      detail: apply ? '正在下载并调度码控更新，完成后会自动重启' : '正在连接版本源',
      steps: apply ? ['检查版本', '下载更新包', '重启码控'] : ['连接版本源', '对比版本', '同步状态'],
      done: apply ? '码控更新已调度' : '检查完成',
    }, async (progress) => {
      progress.step(0, apply ? '正在检查远端版本' : '正在连接版本源')
      const check = await CheckSelfUpdate()
      if (!apply) {
        message = check.update_available
          ? `发现新版本 ${check.latest_version || check.target_version}`
          : `当前已是最新版 ${check.current_version || ''}`
        progress.step(1, '正在对比本地版本')
        await delay(180)
        progress.step(2, '正在同步本机状态')
        await refreshState()
        return
      }
      message = check.update_available
        ? `发现新版本 ${check.latest_version || check.target_version}`
        : `当前已是最新版 ${check.current_version || ''}`
      if (!check.update_available) {
        progress.step(1, '当前已是最新版')
        await delay(180)
        progress.step(2, '正在同步本机状态')
        await refreshState()
        return
      }
      progress.step(1, '正在下载更新包并调度后台重启')
      const result = await withLauncherProgressPolling(ApplySelfUpdate(), progress)
      message = result.update_available
        ? `发现新版本 ${result.latest_version || result.target_version}`
        : `当前已是最新版 ${result.current_version || ''}`
      progress.step(2, '正在等待后台服务重启')
      await waitLauncherVersion(result.target_version || result.latest_version || check.target_version || check.latest_version)
      await refreshState()
    })
    setState({ error: message })
  } catch {
    // 错误已经在进度弹窗和主界面提示中展示。
  }
}

async function withLauncherProgressPolling(task, progress) {
  let done = false
  const polling = (async () => {
    while (!done) {
      await delay(900)
      try {
        const result = await State()
        const device = result.state || null
        setState({
          ready: !!result.ready,
          device: device || state.device,
          logs: result.logs || state.logs,
        })
        syncLauncherProgressFromState(device, progress)
      } catch {
        // 更新调度期间本地服务可能短暂断开，等待主流程处理重连。
      }
    }
  })()
  try {
    return await task
  } finally {
    done = true
    await polling.catch(() => {})
  }
}

function syncLauncherProgressFromState(device, progress) {
  if (!device || !state.progress) return
  const percent = Number(device.upgrade_progress || 0)
  const message = String(device.upgrade_message || '').trim()
  const stage = String(device.upgrade_stage || '').trim()
  if (!percent && !message) return
  const active = stage === 'checking_version' ? 0 : stage === 'switching_version' || stage === 'starting_agents' || stage === 'completed' ? 2 : 1
  const steps = state.progress.steps.map((item, index) => {
    if (index < active) return { ...item, status: 'done' }
    if (index === active) return { ...item, status: 'active' }
    return { ...item, status: 'pending' }
  })
  setState({
    progress: {
      ...state.progress,
      active,
      steps,
      detail: message || state.progress.detail,
      percent: Math.max(state.progress.percent || 0, Math.min(99, percent || state.progress.percent || 0)),
    },
  })
  if (progress && typeof progress.detail === 'function' && message) {
    progress.detail(message)
  }
}

async function waitLauncherVersion(version) {
  const target = String(version || '').trim()
  const startedAt = Date.now()
  let restartAttempts = 0
  let restarted = false
  while (Date.now() - startedAt < 180 * 1000) {
    await delay(1000)
    try {
      const result = await State()
      setState({
        ready: !!result.ready,
        device: result.state || state.device,
        logs: result.logs || state.logs,
      })
      if (!target || result.state?.launcher_version === target) {
        return result
      }
      updateProgress({
        detail: `后台已恢复，当前版本 ${result.state?.launcher_version || '未知'}，正在等待切换到 ${target}`,
        percent: Math.max(state.progress?.percent || 0, 82),
      })
    } catch {
      // 更新期间本地服务会短暂断开。终端兼容模式下由 GUI 拉起新后台。
      const elapsed = Date.now() - startedAt
      if (!restarted || (elapsed > 8000 && restartAttempts < 4)) {
        restarted = true
        restartAttempts += 1
        updateProgress({
          detail: restartAttempts === 1 ? '正在拉起更新后的后台服务' : `后台服务仍未响应，正在第 ${restartAttempts} 次重试拉起`,
          percent: Math.max(state.progress?.percent || 0, 80),
        })
        try {
          await RestartBackground()
        } catch {
          // 继续轮询，等待系统完成替换。
        }
      } else {
        updateProgress({
          detail: '正在等待后台服务端口恢复',
          percent: Math.max(state.progress?.percent || 0, 80),
        })
      }
    }
  }
  throw new Error('launcher 已调度更新，但后台服务重连超时')
}

async function loadRuntimeEnvironment() {
  if (!state.ready) return
  setState({ runtimeLoading: true, error: '' })
  try {
    const info = await RuntimeEnvironment()
    setState({ runtimeInfo: info || null, runtimeLoading: false })
  } catch (error) {
    setState({ runtimeLoading: false })
    setError(error)
  }
}

async function prepareRuntimeEnvironment() {
  try {
    await runProgress({
      title: '正在准备运行时环境',
      detail: '正在检测 Node、uv、Python 和 Java，必要时会使用镜像或托管源下载',
      steps: ['检测本机环境', '准备托管运行时', '刷新 Agent 环境'],
      done: '运行时环境检测完成',
    }, async (progress) => {
      progress.step(0, '正在检测现有运行时')
      await delay(120)
      progress.step(1, '正在准备缺失的托管运行时')
      let info = await PrepareRuntimeEnvironment()
      setState({ runtimeInfo: info || null })
      updateProgress({ runtime: runtimeProgressInfo(info), detail: runtimeProgressDetail(info) })
      info = await waitRuntimeEnvironmentReady(progress, info)
      progress.step(2, '正在同步 Agent 环境')
      await refreshState()
      if (info?.last_error) {
        setState({ error: info.last_error })
      }
    })
  } catch {
    // 错误已经在进度弹窗和主界面提示中展示。
  }
}

async function waitRuntimeEnvironmentReady(progress, initialInfo) {
  let info = initialInfo || null
  const startedAt = Date.now()
  const timeoutMs = 15 * 60 * 1000
  while (info?.preparing) {
    updateProgress({ runtime: runtimeProgressInfo(info), detail: runtimeProgressDetail(info) })
    if (Date.now() - startedAt > timeoutMs) {
      throw new Error('运行时仍在后台准备中，请稍后刷新环境状态')
    }
    await delay(1500)
    info = await RuntimeEnvironment()
    setState({ runtimeInfo: info || null })
  }
  updateProgress({ runtime: runtimeProgressInfo(info), detail: runtimeProgressDetail(info) })
  return info || {}
}

function runtimeProgressDetail(info) {
  const logs = info?.logs || []
  return logs.slice(-1)[0] || (info?.preparing ? '托管运行时仍在准备中' : '运行时检测完成')
}

function runtimeProgressInfo(info) {
  if (!info) return null
  const tools = (info.tools || []).filter((tool) => tool?.name)
  const env = info.environment || {}
  const logs = info.logs || []
  return {
    runtimeDir: info.runtime_dir || '',
    preparing: !!info.preparing,
    tools,
    envCount: Object.keys(env).length,
    logs: logs.slice(-5),
    lastError: info.last_error || '',
  }
}

async function openMCPDialog(agentID) {
  const agent = ((state.device || {}).agents || []).find((item) => item.agent_id === agentID) || { agent_id: agentID }
  setState({ mcpDialog: true, mcpAgent: agent, mcpStatus: null, mcpLoading: true, error: '' })
  await loadMCPStatus(agentID)
}

async function loadMCPStatus(agentID = '') {
  const target = agentID || state.mcpAgent?.agent_id || ''
  if (!target) return
  setState({ mcpLoading: true })
  try {
    const status = await MCPStatus(target)
    setState({ mcpStatus: status || null, mcpLoading: false })
  } catch (error) {
    setState({ mcpLoading: false, mcpStatus: null })
    setError(error)
  }
}

function closeMCPDialog() {
  setState({ mcpDialog: false, mcpAgent: null, mcpStatus: null, mcpLoading: false })
}

async function restartMCPAgent() {
  if (!state.mcpAgent?.agent_id) return
  await agentAction('restart', state.mcpAgent.agent_id)
  await loadMCPStatus(state.mcpAgent.agent_id)
}

async function loadSSHStatus() {
  if (!state.ready) return
  setState({ sshLoading: true, error: '' })
  try {
    const status = await SSHStatus()
    setState({ sshStatus: status || null, sshLoading: false })
  } catch (error) {
    setState({ sshLoading: false })
    setError(error)
  }
}

async function setupSSH() {
  setState({ sshLoading: true, sshCommand: '', sshUnixCommand: '', sshWindowsCommand: '', sshExpiresAt: '', sshCopied: false, error: '' })
  try {
    const result = await SetupSSH()
    const status = result?.status || null
    const errorText = result?.error || ''
    setState({ sshStatus: status, sshLoading: false, error: errorText })
    if (!errorText && status?.listening) {
      setState({ error: 'OpenSSH Server 已初始化并监听本机回环地址。' })
    }
  } catch (error) {
    setState({ sshLoading: false })
    setError(error)
  }
}

async function createSSHTunnel() {
  setState({ sshLoading: true, sshCommand: '', sshUnixCommand: '', sshWindowsCommand: '', sshExpiresAt: '', sshCopied: false, error: '' })
  try {
    const result = await CreateSSHTunnel()
    const unixCommand = result?.unix_command || result?.command || ''
    const windowsCommand = result?.windows_command || ''
    setState({
      sshLoading: false,
      sshUnixCommand: unixCommand,
      sshWindowsCommand: windowsCommand,
      sshCommand: state.sshClientPlatform === 'windows' ? windowsCommand : unixCommand,
      sshExpiresAt: result?.expires_at || '',
    })
  } catch (error) {
    setState({ sshLoading: false })
    setError(error)
  }
}

async function copySSHCommand() {
  if (!state.sshCommand) return
  try {
    const copied = await ClipboardSetText(state.sshCommand)
    setState({ sshCopied: copied !== false })
  } catch (error) {
    setError(error)
  }
}

function setSSHClientPlatform(platform) {
  const next = platform === 'windows' ? 'windows' : 'unix'
  setState({
    sshClientPlatform: next,
    sshCommand: next === 'windows' ? state.sshWindowsCommand : state.sshUnixCommand,
    sshCopied: false,
  })
}

async function installSSHAuthorizedKey() {
  const publicKey = String(state.sshPublicKey || '').trim()
  if (!publicKey) {
    setState({ error: '请粘贴客户端 SSH 公钥。' })
    return
  }
  setState({ sshLoading: true, sshKeyResult: null, error: '' })
  try {
    const result = await InstallSSHAuthorizedKey(publicKey)
    setState({ sshLoading: false, sshKeyResult: result || null, sshPublicKey: '' })
  } catch (error) {
    setState({ sshLoading: false })
    setError(error)
  }
}

window.gui = {
  login,
  refreshState,
  chooseDirectory,
  submitCreateAgent,
  agentAction,
  toggleAutostart,
  toggleTerminalCompat,
  checkUpdate,
  loadRuntimeEnvironment,
  prepareRuntimeEnvironment,
  openMCPDialog,
  loadMCPStatus,
  closeMCPDialog,
  restartMCPAgent,
  loadSSHStatus,
  setupSSH,
  createSSHTunnel,
  copySSHCommand,
  setSSHClientPlatform,
  installSSHAuthorizedKey,
  setTab(tab) {
    setState({ tab })
    if (tab === 'environment') {
      loadRuntimeEnvironment()
    }
    if (tab === 'ssh') {
      loadSSHStatus()
    }
  },
  openCreateDialog() {
    setState({ createDialog: true, error: '' })
    chooseDirectory(state.createPath)
  },
  closeCreateDialog() {
    setState({ createDialog: false })
  },
  setCreateName(value) {
    state.createName = value
  },
  setCreatePath(value) {
    state.createPath = value
  },
  setEnableAutostart(value) {
    state.enableAutostart = !!value
  },
  setRememberLogin(value) {
    state.rememberLogin = !!value
  },
  setLoginUsername(value) {
    state.loginUsername = value
  },
  setLoginPassword(value) {
    state.loginPassword = value
  },
  setSSHPublicKey(value) {
    state.sshPublicKey = value
  },
  closeProgress() {
    setState({ progress: null })
  },
}

function render() {
  const device = state.device || {}
  const agents = device.agents || []
  if (!state.authenticated) {
    app.innerHTML = renderLoginScreen()
    return
  }
  app.innerHTML = `
    <div class="shell">
      <aside class="sidebar">
        <div class="brand">
          <div class="brand-icon">C</div>
          <div>
            <div class="brand-title">码控</div>
            <div class="brand-subtitle">本机控制台</div>
          </div>
        </div>
        <button class="nav ${state.tab === 'overview' ? 'active' : ''}" onclick="gui.setTab('overview')">总览</button>
        <button class="nav ${state.tab === 'agents' ? 'active' : ''}" onclick="gui.setTab('agents')">Agent</button>
        <button class="nav ${state.tab === 'environment' ? 'active' : ''}" onclick="gui.setTab('environment')">环境</button>
        <button class="nav ${state.tab === 'ssh' ? 'active' : ''}" onclick="gui.setTab('ssh')">远程 SSH</button>
        <button class="nav ${state.tab === 'settings' ? 'active' : ''}" onclick="gui.setTab('settings')">设置</button>
        <button class="nav ${state.tab === 'logs' ? 'active' : ''}" onclick="gui.setTab('logs')">诊断</button>
        <div class="sidebar-footer">
          <div class="muted">Launcher ${escapeHtml(state.defaults.launcher_version || device.launcher_version || '')}</div>
          <div class="pill ${state.ready ? 'online' : 'offline'}">${state.ready ? '本地已连接' : '本地未启动'}</div>
        </div>
      </aside>
      <main class="main">
        <header class="topbar">
          <div>
            <h1>${titleForTab()}</h1>
            <p>${subtitleForTab()}</p>
          </div>
          <div class="top-actions">
            <button class="icon-btn" onclick="gui.refreshState()">刷新</button>
            <button class="primary" onclick="gui.openCreateDialog()" ${state.ready ? '' : 'disabled'}>新建 Agent</button>
          </div>
        </header>
        ${state.error ? `<div class="notice">${escapeHtml(state.error)}</div>` : ''}
        ${renderContent(device, agents)}
      </main>
    </div>
    ${state.createDialog ? renderCreateDialog() : ''}
    ${state.mcpDialog ? renderMCPDialog() : ''}
    ${state.progress ? renderProgressDialog() : ''}
  `
}

function titleForTab() {
  if (state.tab === 'agents') return 'Agent 管理'
  if (state.tab === 'environment') return '运行时环境'
  if (state.tab === 'ssh') return '远程 SSH'
  if (state.tab === 'settings') return '设置'
  if (state.tab === 'logs') return '诊断'
  return '设备总览'
}

function subtitleForTab() {
  if (state.tab === 'agents') return '创建、启动、停止和重启当前设备上的 agent'
  if (state.tab === 'environment') return '管理 MCP 常用的 Node、uv、Python 和 Java 运行时'
  if (state.tab === 'ssh') return '检查本机 SSH 服务并生成 Agent 可复用的远程会话命令'
  if (state.tab === 'settings') return '管理自启动、更新和服务器连接'
  if (state.tab === 'logs') return '查看 launcher 最近操作和启动日志'
  return '查看 launcher、opencode 和设备运行状态'
}

function renderLoginScreen() {
  return `
    <main class="login-screen">
      <div class="panel login-panel">
        <div class="login-mark" aria-hidden="true">
          <span class="login-ring"></span>
          <span class="login-core">C</span>
        </div>
        <div class="login-title">码控</div>
        ${state.error ? `<div class="notice login-notice">${escapeHtml(state.error)}</div>` : ''}
        <form onsubmit="gui.login(event)" class="form">
          <label>账号</label>
          <input name="username" autocomplete="username" value="${escapeHtml(state.loginUsername)}" oninput="gui.setLoginUsername(this.value)" />
          <label>密码</label>
          <input name="password" type="password" autocomplete="current-password" value="${escapeHtml(state.loginPassword)}" oninput="gui.setLoginPassword(this.value)" />
          <label class="check-row">
            <input type="checkbox" ${state.rememberLogin ? 'checked' : ''} onchange="gui.setRememberLogin(this.checked)" />
            <span>记住并自动登录</span>
          </label>
          <label class="check-row">
            <input type="checkbox" ${state.enableAutostart ? 'checked' : ''} onchange="gui.setEnableAutostart(this.checked)" />
            <span>登录成功后开启后台服务自启动</span>
          </label>
          <button class="primary wide" type="submit" ${state.loading ? 'disabled' : ''}>${state.loading ? '正在登录' : '登录'}</button>
        </form>
      </div>
    </main>
  `
}

function renderContent(device, agents) {
  if (state.tab === 'agents') return renderAgents(agents)
  if (state.tab === 'environment') return renderEnvironment()
  if (state.tab === 'ssh') return renderSSH()
  if (state.tab === 'settings') return renderSettings(device)
  if (state.tab === 'logs') return renderLogs()
  return renderOverview(device, agents)
}

function renderSSH() {
  const ssh = state.sshStatus || {}
  const ready = !!ssh.listening
  const platform = { windows: 'Windows', darwin: 'macOS', linux: 'Linux' }[ssh.platform] || ssh.platform || '-'
  return `
    <section class="ssh-layout">
      <div class="panel ssh-status-panel">
        <div class="section-head">
          <div>
            <h2>OpenSSH Server</h2>
            <p class="muted">仅监听本机回环地址，由远程隧道转发连接</p>
          </div>
          <div class="row-actions">
            ${iconCommand('refresh', '刷新 SSH 状态', 'gui.loadSSHStatus()')}
            <span class="pill ${ready ? 'online' : state.sshLoading ? 'processing' : 'offline'}">${state.sshLoading ? '检测中' : ready ? '已就绪' : '未就绪'}</span>
          </div>
        </div>
        <div class="ssh-status-grid">
          ${sshMetric('平台', platform, !!ssh.platform)}
          ${sshMetric('已安装', ssh.installed ? '是' : '否', !!ssh.installed)}
          ${sshMetric('服务运行', ssh.running ? '是' : '否', !!ssh.running)}
          ${sshMetric('本机监听', ssh.listening ? (ssh.listen_address || '127.0.0.1:22') : '未监听', !!ssh.listening)}
        </div>
        <div class="ssh-detail">
          <span>Host key 指纹</span>
          <code>${escapeHtml(ssh.host_key_fingerprint || '等待 SSH 服务生成')}</code>
        </div>
        ${ssh.message ? `<div class="ssh-message">${escapeHtml(ssh.message)}</div>` : ''}
        ${ssh.error ? `<div class="notice runtime-notice">${escapeHtml(ssh.error)}</div>` : ''}
        <button class="primary" onclick="gui.setupSSH()" ${state.sshLoading || ready ? 'disabled' : ''}>${state.sshLoading ? '正在处理' : ready ? 'SSH 已初始化' : '初始化 SSH'}</button>
      </div>

      <div class="panel ssh-connect-panel">
        <div class="section-head">
          <div>
            <h2>Agent SSH 会话</h2>
            <p class="muted">首次命令建立后台会话并生成本地 wrapper，后续命令复用同一连接</p>
          </div>
          <span class="pill ${state.sshCommand ? 'processing' : 'offline'}">${state.sshCommand ? '待启动' : '未生成'}</span>
        </div>
        <div class="ssh-platform-switch" role="group" aria-label="连接端系统">
          <button class="${state.sshClientPlatform === 'unix' ? 'active' : ''}" onclick="gui.setSSHClientPlatform('unix')">macOS / Linux</button>
          <button class="${state.sshClientPlatform === 'windows' ? 'active' : ''}" onclick="gui.setSSHClientPlatform('windows')">Windows</button>
        </div>
        <div class="ssh-command-box ${state.sshCommand ? '' : 'empty-command'}">
          <code>${escapeHtml(state.sshCommand || 'SSH 服务就绪后，在需要连接时生成命令')}</code>
          ${state.sshCommand ? `<button class="icon-action" title="复制 SSH 命令" aria-label="复制 SSH 命令" onclick="gui.copySSHCommand()">${state.sshCopied ? actionIcons.check : actionIcons.copy}</button>` : ''}
        </div>
        <div class="ssh-command-meta">
          <span>${state.sshExpiresAt ? `启动链接有效期至 ${escapeHtml(state.sshExpiresAt)}；会话启动后由 SSH keepalive 保持` : '首次命令只消费一次 bootstrap；执行后使用终端输出的 Agent wrapper'}</span>
          ${state.sshCopied ? '<span class="pill online">已复制</span>' : ''}
        </div>
        <button class="primary" onclick="gui.createSSHTunnel()" ${state.sshLoading || !ready ? 'disabled' : ''}>${state.sshLoading ? '正在生成' : '生成 Agent SSH 命令'}</button>
      </div>

      <div class="panel ssh-key-panel">
        <div class="section-head">
          <div>
            <h2>客户端公钥</h2>
            <p class="muted">安装发起连接电脑的 SSH 公钥，用于目标账号登录认证</p>
          </div>
          <span class="pill ${state.sshKeyResult?.installed ? 'online' : 'offline'}">${state.sshKeyResult?.installed ? '已安装' : '待配置'}</span>
        </div>
        <textarea class="ssh-key-input" rows="3" spellcheck="false" value="${escapeHtml(state.sshPublicKey)}" oninput="gui.setSSHPublicKey(this.value)" placeholder="ssh-ed25519 AAAA... client-name">${escapeHtml(state.sshPublicKey)}</textarea>
        ${state.sshKeyResult ? `
          <div class="ssh-key-result">
            <span>${escapeHtml(state.sshKeyResult.message || '客户端 SSH 公钥已安装')}</span>
            <code>${escapeHtml(state.sshKeyResult.fingerprint || '')}</code>
          </div>
        ` : ''}
        <button class="primary" onclick="gui.installSSHAuthorizedKey()" ${state.sshLoading || !ready ? 'disabled' : ''}>安装客户端公钥</button>
      </div>
    </section>
  `
}

function sshMetric(label, value, ok) {
  return `
    <div class="ssh-metric">
      <span>${escapeHtml(label)}</span>
      <strong class="${ok ? 'ok' : ''}">${escapeHtml(value)}</strong>
    </div>
  `
}

function renderOverview(device, agents) {
  const autostart = device.autostart || {}
  return `
    <section class="grid cards">
      ${metric('设备状态', statusLabel[device.status] || device.status || '未知', device.status === 'running' ? 'online' : 'processing')}
      ${metric('Agent 数量', String(agents.length), 'processing')}
      ${metric('launcher 版本', device.launcher_version || state.defaults.launcher_version || '-', 'processing')}
      ${metric('自启动', autostart.enabled ? '已开启' : '未开启', autostart.enabled ? 'online' : 'offline')}
    </section>
    <section class="panel">
      <div class="section-head">
        <h2>Agent 概览</h2>
        <button class="ghost" onclick="gui.setTab('agents')">查看全部</button>
      </div>
      ${agents.length ? renderAgentTable(agents.slice(0, 5)) : empty('还没有 agent，点击右上角新建。')}
    </section>
  `
}

function renderEnvironment() {
  const info = state.runtimeInfo || {}
  const tools = (info.tools || []).filter((tool) => tool?.name)
  const env = info.environment || {}
  return `
    <section class="grid environment-grid">
      <div class="panel env-summary">
        <div class="section-head">
          <div>
            <h2>托管运行时</h2>
            <p class="muted">用于 MCP 的 npx、uvx、python、java 等命令</p>
          </div>
          <div class="row-actions">
            ${iconCommand('refresh', '刷新环境状态', 'gui.loadRuntimeEnvironment()')}
            ${iconCommand('terminal', '准备运行时', 'gui.prepareRuntimeEnvironment()')}
          </div>
        </div>
        <div class="runtime-path path" title="${escapeHtml(info.runtime_dir || '')}">${escapeHtml(info.runtime_dir || '等待检测')}</div>
        ${info.last_error ? `<div class="notice runtime-notice">${escapeHtml(info.last_error)}</div>` : ''}
      </div>
      <div class="panel env-tools">
        <div class="section-head">
          <h2>工具状态</h2>
          <span class="pill ${info.preparing ? 'processing' : 'online'}">${info.preparing ? '检测中' : `${tools.length} 项`}</span>
        </div>
        ${state.runtimeLoading ? '<div class="empty">正在读取运行时状态</div>' : renderRuntimeTools(tools)}
      </div>
      <div class="panel env-vars">
        <div class="section-head">
          <h2>注入变量</h2>
          <span class="pill processing">${Object.keys(env).length} 项</span>
        </div>
        ${renderRuntimeEnv(env)}
      </div>
      <div class="panel env-logs">
        <div class="section-head">
          <h2>检测日志</h2>
        </div>
        <div class="logs compact">${(info.logs || []).map((line) => `<div>${escapeHtml(line)}</div>`).join('') || '<div class="muted">暂无检测日志</div>'}</div>
      </div>
    </section>
  `
}

function renderRuntimeTools(tools) {
  if (!tools.length) return empty('还没有运行时检测结果，点击准备运行时。')
  return `
    <div class="runtime-tools">
      ${tools.map((tool) => {
        const ok = tool.status === 'available'
        const source = tool.source === 'managed' ? '托管' : '系统'
        return `
          <div class="runtime-tool">
            <div class="runtime-tool-head">
              <strong>${escapeHtml(tool.name)}</strong>
              <span class="pill ${ok ? 'online' : 'error'}">${ok ? source : '不可用'}</span>
            </div>
            <div class="muted">${escapeHtml(tool.version || (ok ? '已检测到版本' : tool.error || '不可用'))}</div>
            <div class="path runtime-tool-path" title="${escapeHtml(tool.path || '')}">${escapeHtml(tool.path || '-')}</div>
            ${tool.error ? `<div class="runtime-error">${escapeHtml(tool.error)}</div>` : ''}
          </div>
        `
      }).join('')}
    </div>
  `
}

function renderRuntimeEnv(env) {
  const entries = Object.entries(env)
  if (!entries.length) return empty('暂无注入变量。')
  return `
    <div class="env-kv-list">
      ${entries.map(([key, value]) => `
        <div class="env-kv">
          <span class="mono">${escapeHtml(key)}</span>
          <span class="path" title="${escapeHtml(value)}">${escapeHtml(value)}</span>
        </div>
      `).join('')}
    </div>
  `
}

function metric(label, value, type) {
  return `
    <div class="panel metric">
      <div class="muted">${escapeHtml(label)}</div>
      <strong>${escapeHtml(value)}</strong>
      <div class="pill ${type}">${escapeHtml(value)}</div>
    </div>
  `
}

function renderAgents(agents) {
  return `
    <section class="panel">
      <div class="section-head">
        <h2>本机 Agent</h2>
        <button class="primary" onclick="gui.openCreateDialog()">新建 Agent</button>
      </div>
      ${agents.length ? renderAgentTable(agents) : empty('当前设备还没有 agent。')}
    </section>
  `
}

function renderAgentTable(agents) {
  return `
    <div class="table">
      <div class="tr th">
        <div>名称</div><div>目录</div><div>状态</div><div>端口</div><div>操作</div>
      </div>
      ${agents.map((agent) => `
        <div class="tr">
          <div>
            <strong>${escapeHtml(agent.name || agent.agent_id)}</strong>
            <div class="muted mono">${escapeHtml(agent.agent_id)}</div>
          </div>
          <div class="path">${escapeHtml(agent.project_dir || '-')}</div>
          <div><span class="pill ${agent.status === 'running' ? 'online' : agent.status === 'failed' ? 'error' : 'offline'}">${escapeHtml(statusLabel[agent.status] || agent.status || '-')}</span></div>
          <div>${escapeHtml(agent.port || '-')}</div>
          <div class="row-actions">
            ${agent.enabled ? actionButton('stop', agent.agent_id, '停止') : actionButton('start', agent.agent_id, '启动')}
            ${mcpActionButton(agent.agent_id)}
            ${actionButton('restart', agent.agent_id, '重启')}
            ${actionButton('permission', agent.agent_id, '请求目录权限')}
            ${actionButton('remove', agent.agent_id, '删除', 'danger-icon')}
          </div>
        </div>
      `).join('')}
    </div>
  `
}

function renderSettings(device) {
  const autostart = device.autostart || {}
  const launcherVersion = device.launcher_version || state.defaults.launcher_version || '-'
  const servicePath = autostart.path || autostart.command || '-'
  const terminalCompatActive = String(autostart.command || '').includes('start-terminal-compat.command')
  return `
    <section class="settings-grid">
      <div class="panel setting-panel">
        <div class="setting-main">
          <div class="setting-title">
            <h2>后台服务</h2>
            <span class="pill ${autostart.enabled ? 'online' : 'offline'}">${autostart.enabled ? '自启动' : '手动'}</span>
          </div>
          <div class="setting-meta">
            <span>${escapeHtml(autostart.method || '-')}</span>
            <span class="dot"></span>
            <span>GUI 关闭后服务保持运行</span>
          </div>
          <div class="path setting-path" title="${escapeHtml(servicePath)}">${escapeHtml(servicePath)}</div>
        </div>
        <div class="setting-actions">
          ${iconCommand('power', autostart.enabled ? '关闭自启动' : '开启自启动', `gui.toggleAutostart(${!autostart.enabled})`, autostart.enabled ? 'danger-icon' : '')}
        </div>
      </div>
      <div class="panel setting-panel">
        <div class="setting-main">
          <div class="setting-title">
            <h2>终端兼容模式</h2>
            <span class="pill ${terminalCompatActive ? 'online' : state.terminalCompat ? 'processing' : 'offline'}">${terminalCompatActive ? '已接管' : state.terminalCompat ? '待接管' : '关闭'}</span>
          </div>
          <div class="setting-meta">
            <span>使用 Terminal 权限链路启动 macOS 后台服务</span>
          </div>
          <div class="path setting-path" title="${escapeHtml(autostart.command || '')}">${escapeHtml(autostart.command || '-')}</div>
        </div>
        <div class="setting-actions">
          <label class="switch" title="${state.terminalCompat ? '关闭终端兼容模式' : '开启终端兼容模式'}">
            <input type="checkbox" ${state.terminalCompat ? 'checked' : ''} onchange="gui.toggleTerminalCompat(this.checked)" />
            <span></span>
          </label>
        </div>
      </div>
      <div class="panel setting-panel">
        <div class="setting-main">
          <div class="setting-title">
            <h2>版本更新</h2>
            <span class="pill processing">${escapeHtml(launcherVersion)}</span>
          </div>
          <div class="setting-meta">
            <span>检查并升级 launcher 后台服务</span>
          </div>
          <div class="path setting-path" title="${escapeHtml(state.defaults.server_url || '')}">${escapeHtml(state.defaults.server_url || '-')}</div>
        </div>
        <div class="setting-actions">
          ${iconCommand('search', '检查更新', 'gui.checkUpdate(false)')}
          ${iconCommand('download', '立即升级', 'gui.checkUpdate(true)')}
        </div>
      </div>
    </section>
  `
}

function renderLogs() {
  return `
    <section class="panel">
      <div class="section-head">
        <h2>最近日志</h2>
        <button class="ghost" onclick="gui.refreshState()">刷新</button>
      </div>
      <div class="logs">${(state.logs || []).map((line) => `<div>${escapeHtml(line)}</div>`).join('') || '<div class="muted">暂无日志</div>'}</div>
    </section>
  `
}

function renderCreateDialog() {
  return `
    <div class="modal-backdrop">
      <div class="modal">
        <div class="section-head">
          <h2>新建 Agent</h2>
          <button class="icon-btn" onclick="gui.closeCreateDialog()">关闭</button>
        </div>
        <form onsubmit="gui.submitCreateAgent(event)" class="form">
          <label>Agent 名称</label>
          <input value="${escapeHtml(state.createName)}" oninput="gui.setCreateName(this.value)" placeholder="例如 chat-codex" />
          <label>项目目录</label>
          <div class="inline">
            <input value="${escapeHtml(state.createPath)}" oninput="gui.setCreatePath(this.value)" placeholder="/Users/..." />
            <button class="ghost" type="button" onclick="gui.chooseDirectory(document.querySelector('.inline input').value)">定位</button>
          </div>
          <div class="dir-box">
            <div class="dir-head">
              <button class="ghost" type="button" onclick="gui.chooseDirectory('${escapeHtml(state.dirParent)}')" ${state.dirParent ? '' : 'disabled'}>上级</button>
              <span class="path">${escapeHtml(state.dirPath || '请选择目录')}</span>
            </div>
            <div class="dir-list">
              ${(state.dirEntries || []).filter((entry) => entry.is_dir).map((entry) => `
                <button class="dir-item" type="button" onclick="gui.chooseDirectory('${escapeHtml(entry.path)}')">${escapeHtml(entry.name)}</button>
              `).join('') || '<div class="muted">暂无可选目录</div>'}
            </div>
          </div>
          <button class="primary wide" type="submit" ${state.loading ? 'disabled' : ''}>创建并启动</button>
        </form>
      </div>
    </div>
  `
}

function renderMCPDialog() {
  const agent = state.mcpAgent || {}
  const status = state.mcpStatus || {}
  const servers = status.servers || []
  return `
    <div class="modal-backdrop">
      <div class="modal mcp-modal">
        <div class="section-head">
          <div>
            <h2>MCP 状态</h2>
            <p class="muted">${escapeHtml(agent.name || agent.agent_id || 'Agent')}</p>
          </div>
          <div class="row-actions">
            ${iconCommand('refresh', '刷新 MCP 状态', 'gui.loadMCPStatus()')}
            ${iconCommand('restart', '重启 Agent', 'gui.restartMCPAgent()')}
            ${iconCommand('terminal', '准备运行时', 'gui.prepareRuntimeEnvironment()')}
            <button class="icon-action" title="关闭" aria-label="关闭" onclick="gui.closeMCPDialog()">x</button>
          </div>
        </div>
        ${state.mcpLoading ? '<div class="empty">正在读取 MCP 状态</div>' : renderMCPServers(servers)}
      </div>
    </div>
  `
}

function renderMCPServers(servers) {
  if (!servers.length) return empty('当前 Agent 没有返回 MCP 状态。')
  return `
    <div class="mcp-server-list">
      ${servers.map((server) => {
        const tools = server.tools || []
        const ok = server.status === 'connected'
        return `
          <details class="mcp-server" ${server.error ? 'open' : ''}>
            <summary>
              <span class="mcp-server-name">${escapeHtml(server.name)}</span>
              <span class="pill ${ok ? 'online' : server.status === 'failed' ? 'error' : 'processing'}">${escapeHtml(mcpStatusText(server.status))}</span>
              <span class="muted">工具 ${tools.length} 个</span>
            </summary>
            ${server.error ? `<div class="runtime-error mcp-error">${escapeHtml(server.error)}</div>` : ''}
            ${tools.length ? `
              <div class="mcp-tool-list">
                ${tools.map((tool) => `
                  <div class="mcp-tool">
                    <strong>${escapeHtml(tool.id || '')}</strong>
                    ${tool.description ? `<p>${escapeHtml(tool.description)}</p>` : ''}
                  </div>
                `).join('')}
              </div>
            ` : '<div class="muted mcp-empty-tools">当前 MCP 没有返回工具。</div>'}
          </details>
        `
      }).join('')}
    </div>
  `
}

function mcpStatusText(status) {
  if (status === 'connected') return '已连接'
  if (status === 'failed') return '失败'
  if (status === 'disabled') return '已停用'
  if (status === 'needs_auth') return '需授权'
  if (status === 'needs_client_registration') return '需注册'
  return status || '未知'
}

function renderProgressDialog() {
  const progress = state.progress
  if (!progress) return ''
  return `
    <div class="progress-backdrop">
      <div class="progress-dialog ${progress.error ? 'has-error' : ''} ${progress.complete ? 'is-complete' : ''}">
        <div class="progress-visual" aria-hidden="true">
          <div class="progress-ring"></div>
          <div class="progress-scan">
            <img class="progress-logo" src="${escapeHtml(appIconUrl)}" alt="" />
          </div>
        </div>
        <div class="progress-body">
          <div class="progress-head">
            <div>
              <h2 data-progress-title>${escapeHtml(progress.title)}</h2>
              <p data-progress-detail>${escapeHtml(progress.detail)}</p>
            </div>
            <span class="progress-percent" data-progress-percent>${Math.round(progress.percent)}%</span>
          </div>
          <div class="progress-track">
            <div class="progress-fill" data-progress-fill style="width: ${Math.max(0, Math.min(100, progress.percent))}%"></div>
          </div>
          <div class="progress-steps">
            ${progress.steps.map((step, index) => `
              <div class="progress-step ${escapeHtml(step.status)}" data-progress-step>
                <span data-progress-marker>${step.status === 'done' ? '✓' : index + 1}</span>
                <div data-progress-label>${escapeHtml(step.label)}</div>
              </div>
            `).join('')}
          </div>
          <div data-progress-runtime>${renderProgressRuntime(progress.runtime)}</div>
          ${progress.error ? `<button class="ghost progress-close" data-progress-close onclick="gui.closeProgress()">关闭</button>` : ''}
        </div>
      </div>
    </div>
  `
}

function updateProgressDialogInPlace() {
  const progress = state.progress
  const dialog = document.querySelector('.progress-dialog')
  if (!dialog) return false
  if (!progress) {
    const backdrop = document.querySelector('.progress-backdrop')
    if (backdrop) backdrop.remove()
    return true
  }
  dialog.classList.toggle('has-error', !!progress.error)
  dialog.classList.toggle('is-complete', !!progress.complete)
  const title = dialog.querySelector('[data-progress-title]')
  const detail = dialog.querySelector('[data-progress-detail]')
  const percent = dialog.querySelector('[data-progress-percent]')
  const fill = dialog.querySelector('[data-progress-fill]')
  const steps = dialog.querySelectorAll('[data-progress-step]')
  if (title) title.textContent = progress.title || ''
  if (detail) detail.textContent = progress.detail || ''
  if (percent) percent.textContent = `${Math.round(progress.percent || 0)}%`
  if (fill) fill.style.width = `${Math.max(0, Math.min(100, progress.percent || 0))}%`
  steps.forEach((node, index) => {
    const step = progress.steps[index]
    if (!step) return
    node.className = `progress-step ${step.status || ''}`
    const marker = node.querySelector('[data-progress-marker]')
    const label = node.querySelector('[data-progress-label]')
    if (marker) marker.textContent = step.status === 'done' ? '✓' : String(index + 1)
    if (label) label.textContent = step.label || ''
  })
  const runtime = dialog.querySelector('[data-progress-runtime]')
  if (runtime) runtime.innerHTML = renderProgressRuntime(progress.runtime)
  const close = dialog.querySelector('[data-progress-close]')
  if (progress.error && !close) {
    const body = dialog.querySelector('.progress-body')
    if (body) body.insertAdjacentHTML('beforeend', '<button class="ghost progress-close" data-progress-close onclick="gui.closeProgress()">关闭</button>')
  } else if (!progress.error && close) {
    close.remove()
  }
  return true
}

function renderProgressRuntime(runtime) {
  if (!runtime) return ''
  const tools = runtime.tools || []
  const available = tools.filter((tool) => tool.status === 'available').length
  return `
    <div class="progress-runtime">
      <div class="progress-runtime-grid">
        <div>
          <span>托管目录</span>
          <strong title="${escapeHtml(runtime.runtimeDir || '')}">${escapeHtml(runtime.runtimeDir || '等待检测')}</strong>
        </div>
        <div>
          <span>工具</span>
          <strong>${available}/${tools.length || 0}</strong>
        </div>
        <div>
          <span>注入变量</span>
          <strong>${runtime.envCount || 0} 项</strong>
        </div>
      </div>
      ${tools.length ? `
        <div class="progress-tool-row">
          ${tools.slice(0, 8).map((tool) => `
            <span class="${tool.status === 'available' ? 'ok' : 'bad'}" title="${escapeHtml(tool.path || tool.error || '')}">
              ${escapeHtml(tool.name)} · ${tool.source === 'managed' ? '托管' : '系统'}
            </span>
          `).join('')}
        </div>
      ` : ''}
      ${runtime.logs?.length ? `
        <div class="progress-log-list">
          ${runtime.logs.map((line) => `<div>${escapeHtml(line)}</div>`).join('')}
        </div>
      ` : ''}
      ${runtime.lastError ? `<div class="progress-runtime-error">${escapeHtml(runtime.lastError)}</div>` : ''}
    </div>
  `
}

function empty(text) {
  return `<div class="empty">${escapeHtml(text)}</div>`
}

setWindowMode('login')
bindRuntimeEvents()
loadDefaults()
setInterval(() => {
  if (state.authenticated) refreshState()
}, 5000)
setInterval(checkAutomaticGUIUpdate, 5 * 60 * 1000)
render()
