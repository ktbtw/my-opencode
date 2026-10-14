// 控制台「提示词管理」视图回归：用 CDP 驱动本机无头 Edge/Chrome，全程只连本地夹具服务。
//
// 运行：node admin_web/tests/prompt-registry-smoke.mjs
// 可选环境变量：CHAT_CODEX_BROWSER（浏览器可执行文件路径）、PORT（夹具端口，默认 18773）。
import { spawn } from 'node:child_process';
import assert from 'node:assert/strict';
import { existsSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(here, '..');
const port = Number(process.env.PORT || 18773);
const base = `http://127.0.0.1:${port}`;
const debugPort = Number(process.env.CDP_PORT || 9334);

const browserCandidates = [
  process.env.CHAT_CODEX_BROWSER,
  '/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge',
  '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
  '/usr/bin/microsoft-edge',
  '/usr/bin/google-chrome',
].filter(Boolean);
const browserPath = browserCandidates.find((candidate) => existsSync(candidate));
if (!browserPath) {
  console.error('未找到可用的 Edge/Chrome，可通过 CHAT_CODEX_BROWSER 指定路径。');
  process.exit(2);
}

let passes = 0;
const children = [];
function cleanup() {
  for (const child of children) {
    try { child.kill('SIGKILL'); } catch {}
  }
}
process.on('exit', cleanup);

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

async function waitForHTTP(url, timeoutMs = 15000) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(url);
      if (response.ok) return await response.json();
    } catch {}
    await sleep(200);
  }
  throw new Error(`等待 ${url} 超时`);
}

// 极简 CDP 客户端：够用即可（打开页面、执行表达式、收集控制台异常）。
class CDP {
  constructor(ws) {
    this.ws = ws;
    this.id = 0;
    this.pending = new Map();
    this.errors = [];
    ws.addEventListener('message', (event) => {
      const message = JSON.parse(event.data);
      if (message.id && this.pending.has(message.id)) {
        const { resolve, reject } = this.pending.get(message.id);
        this.pending.delete(message.id);
        if (message.error) reject(new Error(message.error.message));
        else resolve(message.result);
        return;
      }
      if (message.method === 'Runtime.exceptionThrown') {
        this.errors.push(message.params?.exceptionDetails?.exception?.description || 'unknown exception');
      }
      if (message.method === 'Runtime.consoleAPICalled' && message.params?.type === 'error') {
        this.errors.push((message.params.args || []).map((arg) => arg.value ?? arg.description).join(' '));
      }
    });
  }

  send(method, params = {}) {
    const id = ++this.id;
    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject });
      this.ws.send(JSON.stringify({ id, method, params }));
    });
  }

  async evaluate(expression) {
    const result = await this.send('Runtime.evaluate', {
      expression,
      returnByValue: true,
      awaitPromise: true,
    });
    if (result.exceptionDetails) {
      throw new Error(result.exceptionDetails.exception?.description || 'evaluate failed');
    }
    return result.result.value;
  }

  async waitFor(expression, timeoutMs = 10000) {
    const deadline = Date.now() + timeoutMs;
    let last;
    while (Date.now() < deadline) {
      last = await this.evaluate(expression);
      if (last) return last;
      await sleep(120);
    }
    throw new Error(`等待表达式超时: ${expression}（最后结果 ${JSON.stringify(last)}）`);
  }
}

function check(condition, message) {
  assert.ok(condition, message);
  passes += 1;
  console.log(`PASS ${message}`);
}

const fixture = spawn(process.execPath, [path.join(here, 'browser-fixture.mjs')], {
  env: { ...process.env, PORT: String(port) },
  stdio: 'ignore',
});
children.push(fixture);
await waitForHTTP(`${base}/_fixture/pending`);

const browser = spawn(browserPath, [
  '--headless=new',
  '--disable-gpu',
  '--no-first-run',
  '--no-default-browser-check',
  `--user-data-dir=${path.join('/tmp', `chat-codex-console-${process.pid}`)}`,
  `--remote-debugging-port=${debugPort}`,
  'about:blank',
], { stdio: 'ignore' });
children.push(browser);

const version = await waitForHTTP(`http://127.0.0.1:${debugPort}/json/version`);
const target = await fetch(`http://127.0.0.1:${debugPort}/json/new?about:blank`, { method: 'PUT' }).then((r) => r.json());
const ws = new WebSocket(target.webSocketDebuggerUrl);
await new Promise((resolve, reject) => {
  ws.addEventListener('open', resolve);
  ws.addEventListener('error', reject);
});
const cdp = new CDP(ws);
await cdp.send('Runtime.enable');
await cdp.send('Page.enable');

try {
  // 预置登录态：夹具服务只认 Bearer fixture-session。
  // 先按目标 URL（含 hash）加载一次，再写入会话并 reload，避免哈希跳转不重载文档。
  await cdp.send('Page.navigate', { url: `${base}/#prompt-registry` });
  await cdp.waitFor('document.readyState === "complete" && location.hash === "#prompt-registry"');
  await cdp.evaluate(`
    localStorage.setItem('chat_codex_admin_token', 'fixture-session');
    localStorage.setItem('chat_codex_admin_base_url', ${JSON.stringify(base)});
    localStorage.removeItem('chat_codex_admin_custom_server');
    true;
  `);
  await cdp.send('Page.reload', { ignoreCache: true });
  await cdp.waitFor('!document.querySelector("#appView").classList.contains("hidden")');
  await cdp.waitFor('document.querySelectorAll("#grid article").length === 3');
  check(true, `浏览器可用：${version.Browser}`);

  check(await cdp.evaluate('document.querySelector("#topbarTitle").textContent') === '提示词管理', '提示词管理是控制台内视图（侧栏切换，不跳页）');
  check(await cdp.evaluate('document.querySelector("a[data-view=prompts]").classList.contains("active")'), '侧栏「提示词管理」保持选中态');
  check(await cdp.evaluate('document.querySelector("#statTotal").textContent') === '3', '槽位统计按注册表返回渲染');
  check(await cdp.evaluate('document.querySelector("#statEnabledLabel").textContent') === '平台已覆盖', '统计卡片按提示词语义改写');
  check(await cdp.evaluate('[...document.querySelectorAll("#grid article")].some(card => card.textContent.includes("平台已覆盖"))'), '已设置平台值的槽位在卡片上标记');

  // 平台层编辑：抽屉内三段切换 + 保存 + 恢复默认。
  await cdp.evaluate('[...document.querySelectorAll("#grid [data-action=edit]")].find(b => b.dataset.id === "chat.question_policy").click()');
  await cdp.waitFor('!document.querySelector("#drawerOverlay").classList.contains("hidden")');
  check(await cdp.evaluate('[...document.querySelectorAll(".form-section")].filter(s => !s.classList.contains("hidden")).length === 1'), '抽屉只显示提示词编辑区');
  check(await cdp.evaluate('document.querySelector("#promptPlatformValue").value') === '平台版提问策略', '平台默认值回填到编辑器');
  check(await cdp.evaluate('document.querySelectorAll("#promptLevelTabs [data-prompt-level]").length') === 3, '提供平台 / 语义 / 设备三级切换');

  await cdp.evaluate('document.querySelector("[data-prompt-level=semantic]").click()');
  await cdp.waitFor('!document.querySelector("#promptSemanticBlock").classList.contains("hidden")');
  check(await cdp.evaluate('document.querySelector("#promptSemanticValue").value') === '语义版提问策略', '语义 Agent 层显示该 Agent 的覆盖值');
  check(await cdp.evaluate('document.querySelector("#promptSemanticClearButton").disabled === false'), '已有语义覆盖时清除按钮可用');

  await cdp.evaluate('document.querySelector("[data-prompt-level=device]").click()');
  await cdp.waitFor('document.querySelector("#promptDeviceAgentSelect").options.length > 1 && !document.querySelector("#promptDeviceBlock").classList.contains("hidden")');
  await cdp.waitFor('document.querySelector("#promptDeviceValue").value === "设备版提问策略"');
  check(await cdp.evaluate('document.querySelector("#promptDeviceHint").textContent.includes("设备 Agent")'), '设备层展示当前生效来源');
  check(await cdp.evaluate('document.querySelector("#promptDeviceAgentSelect").options[0].textContent.includes("fixture-host")'), '设备下拉列出在线设备 Agent');
  check(await cdp.evaluate('document.querySelector("#promptDeviceClearButton").disabled === false'), '已有设备覆盖时清除按钮可用');

  // 下拉框必须是控制台自绘 UI：原生 select 隐藏，自定义触发器可用。
  check(await cdp.evaluate(`
    getComputedStyle(document.querySelector("#promptDeviceAgentSelect")).display === "none" &&
    !!document.querySelector("#promptDeviceBlock .custom-select") &&
    !!document.querySelector("#promptDeviceBlock .custom-select-trigger")
  `), '设备下拉使用控制台自绘 UI（非原生 select）');
  check(await cdp.evaluate(`
    getComputedStyle(document.querySelector("#promptSemanticAgentSelect")).display === "none" &&
    !!document.querySelector("#promptSemanticBlock .custom-select")
  `), '语义 Agent 下拉使用控制台自绘 UI（非原生 select）');
  await cdp.evaluate('document.querySelector("#promptDeviceBlock .custom-select-trigger").click()');
  await cdp.waitFor('!document.querySelector("#promptDeviceBlock .custom-select-menu").classList.contains("hidden")');
  check(await cdp.evaluate('document.querySelectorAll("#promptDeviceBlock .custom-select-menu [data-select-value]").length === 2'), '自绘下拉展开后列出全部设备选项');
  await cdp.evaluate('document.querySelectorAll("#promptDeviceBlock .custom-select-menu [data-select-value]")[1].click()');
  await cdp.waitFor('document.querySelector("#promptDeviceAgentSelect").value === "1"');
  await cdp.waitFor('document.querySelector("#promptDeviceBlock .custom-select-value").textContent.includes("fixture-host-2")');
  check(true, '自绘下拉选择后同步原生值与触发器文案');

  // 没有在线设备时：自绘下拉同步禁用，避免可点却无选项的假下拉。
  await fetch(`${base}/_fixture/control`, { method: 'POST', body: JSON.stringify({ emptyDevices: true }) });
  await cdp.evaluate('document.querySelector("#promptDeviceRefreshButton").click()');
  await cdp.waitFor('document.querySelector("#promptDeviceAgentSelect").options.length === 1');
  check(await cdp.evaluate(`
    document.querySelector("#promptDeviceAgentSelect").disabled === true &&
    document.querySelector("#promptDeviceBlock .custom-select-trigger").disabled === true
  `), '无在线设备时自绘下拉同步禁用');
  await fetch(`${base}/_fixture/control`, { method: 'POST', body: JSON.stringify({ emptyDevices: false }) });

  await cdp.evaluate('document.querySelector("[data-prompt-level=platform]").click()');
  await cdp.evaluate(`(() => {
    const input = document.querySelector("#promptPlatformValue");
    input.value = "控制台改写的平台提示词";
    input.dispatchEvent(new Event("input", { bubbles: true }));
  })()`);
  await cdp.evaluate('document.querySelector("#saveButton").click()');
  await cdp.waitFor('document.querySelector("#toast").textContent.includes("平台默认已保存")');
  await cdp.waitFor('document.querySelector("#promptPlatformHint").textContent.includes("revision 3")');
  check(await cdp.evaluate('document.querySelector("#promptPlatformValue").value') === '控制台改写的平台提示词', '平台默认保存后写回编辑器');

  await cdp.evaluate('document.querySelector("#promptResetButton").click()');
  await cdp.waitFor('!document.querySelector("#confirmOverlay").classList.contains("hidden")');
  await cdp.evaluate('document.querySelector("#confirmOk").click()');
  await cdp.waitFor('document.querySelector("#toast").textContent.includes("已恢复内置默认")');
  await cdp.waitFor('document.querySelector("#promptPlatformHint").textContent.includes("当前未设置平台值")');
  check(true, '恢复内置默认走确认弹窗并生效');
  check(await cdp.evaluate('document.querySelector("#promptPlatformValue").value') === '需要用户确认时先提问。', '平台未覆盖时编辑框直接展示内置默认（无需手动点击）');
  await cdp.evaluate('document.querySelector("#saveButton").click()');
  await cdp.waitFor('document.querySelector("#toast").textContent.includes("未创建覆盖")');
  check(await cdp.evaluate('document.querySelector("#promptPlatformHint").textContent.includes("当前未设置平台值")'), '保存默认原文不会创建覆盖');

  // 编辑器有未保存改动时，关闭会先弹确认。
  await cdp.evaluate('document.querySelector("#closeDrawerButton").click()');
  await cdp.waitFor('!document.querySelector("#confirmOverlay").classList.contains("hidden")');
  await cdp.evaluate('document.querySelector("#confirmOk").click()');
  await cdp.waitFor('document.querySelector("#drawerOverlay").classList.contains("hidden")');
  check(await cdp.evaluate('!document.querySelector("#grid article .pill.enabled")'), '恢复默认后卡片回到「内置默认」状态');

  // 未配置的槽位：三级编辑框直接展示默认/继承值；保存默认原文不产生覆盖；改回默认自动恢复。
  await cdp.evaluate('[...document.querySelectorAll("#grid [data-action=edit]")].find(b => b.dataset.id === "chat.base_prompt").click()');
  await cdp.waitFor('!document.querySelector("#drawerOverlay").classList.contains("hidden")');
  check(await cdp.evaluate('document.querySelector("#promptPlatformValue").value') === '你是当前服务中的主工作 Agent。', '未配置槽位的平台编辑框直接展示内置默认');
  check(await cdp.evaluate('document.querySelector("#promptResetButton").disabled === true && document.querySelector("#promptSemanticClearButton").disabled === true'), '无覆盖时恢复/清除按钮禁用');

  await cdp.evaluate('document.querySelector("[data-prompt-level=semantic]").click()');
  await cdp.waitFor('!document.querySelector("#promptSemanticBlock").classList.contains("hidden")');
  check(await cdp.evaluate('document.querySelector("#promptSemanticValue").value') === '你是当前服务中的主工作 Agent。', '语义层未覆盖时编辑框展示继承值');

  await cdp.evaluate('document.querySelector("[data-prompt-level=device]").click()');
  await cdp.waitFor('document.querySelector("#promptDeviceValue").value === "你是当前服务中的主工作 Agent。"');
  check(await cdp.evaluate('document.querySelector("#promptDeviceClearButton").disabled === true'), '设备层未覆盖时清除按钮禁用');

  await cdp.evaluate('document.querySelector("[data-prompt-level=platform]").click()');
  await cdp.evaluate('document.querySelector("#saveButton").click()');
  await cdp.waitFor('document.querySelector("#toast").textContent.includes("未创建覆盖")');
  await cdp.evaluate(`(() => {
    const input = document.querySelector("#promptPlatformValue");
    input.value = "临时平台覆盖";
    input.dispatchEvent(new Event("input", { bubbles: true }));
  })()`);
  await cdp.evaluate('document.querySelector("#saveButton").click()');
  await cdp.waitFor('document.querySelector("#toast").textContent.includes("平台默认已保存")');
  await cdp.waitFor('document.querySelector("#promptPlatformHint").textContent.includes("revision 1")');
  await cdp.evaluate(`(() => {
    const input = document.querySelector("#promptPlatformValue");
    input.value = "你是当前服务中的主工作 Agent。";
    input.dispatchEvent(new Event("input", { bubbles: true }));
  })()`);
  await cdp.evaluate('document.querySelector("#saveButton").click()');
  await cdp.waitFor('document.querySelector("#toast").textContent.includes("已恢复内置默认")');
  await cdp.waitFor('document.querySelector("#promptPlatformHint").textContent.includes("当前未设置平台值")');
  check(await cdp.evaluate('document.querySelector("#promptPlatformValue").value') === '你是当前服务中的主工作 Agent。', '平台改回默认文本保存后自动恢复默认并回填默认文本');

  await cdp.evaluate('document.querySelector("#closeDrawerButton").click()');
  await cdp.waitFor('!document.querySelector("#confirmOverlay").classList.contains("hidden")');
  await cdp.evaluate('document.querySelector("#confirmOk").click()');
  await cdp.waitFor('document.querySelector("#drawerOverlay").classList.contains("hidden")');

  // 设备层清除覆盖：确认后恢复继承（回落到语义 Agent 值）。
  await cdp.evaluate('[...document.querySelectorAll("#grid [data-action=edit]")].find(b => b.dataset.id === "chat.question_policy").click()');
  await cdp.waitFor('!document.querySelector("#drawerOverlay").classList.contains("hidden")');
  await cdp.evaluate('document.querySelector("[data-prompt-level=device]").click()');
  await cdp.waitFor('document.querySelector("#promptDeviceValue").value === "设备版提问策略"');
  await cdp.evaluate('document.querySelector("#promptDeviceClearButton").click()');
  await cdp.waitFor('!document.querySelector("#confirmOverlay").classList.contains("hidden")');
  await cdp.evaluate('document.querySelector("#confirmOk").click()');
  await cdp.waitFor('document.querySelector("#toast").textContent.includes("已清除该层覆盖")');
  await cdp.waitFor('document.querySelector("#promptDeviceValue").value === "语义版提问策略"');
  check(await cdp.evaluate('document.querySelector("#promptDeviceHint").textContent.includes("语义 Agent")'), '设备层清除覆盖后恢复继承（来源变为语义 Agent）');
  await cdp.evaluate('document.querySelector("#closeDrawerButton").click()');
  await cdp.waitFor('document.querySelector("#drawerOverlay").classList.contains("hidden")');

  const errors = cdp.errors.filter((message) => message && !message.includes('favicon'));
  check(errors.length === 0, `控制台无运行时异常${errors.length ? `：${errors.join(' | ')}` : ''}`);
  console.log(`\n提示词管理视图回归通过：${passes} 项`);
} finally {
  try { ws.close(); } catch {}
  cleanup();
}
