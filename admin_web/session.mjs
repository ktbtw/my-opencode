// Connection and session helpers are shared by the browser and Node regression tests.
export function safeBrowserStorage(browser) {
  const memory = new Map();
  let storage;
  let persistent = true;
  try { storage = browser.localStorage; } catch { /* Private mode may deny storage. */ }
  return {
    get(key) {
      if (memory.has(key)) return memory.get(key);
      try { return storage?.getItem(key) ?? memory.get(key) ?? ""; }
      catch { return memory.get(key) ?? ""; }
    },
    set(key, value) {
      memory.set(key, String(value));
      if (!persistent || !storage) return false;
      try { storage.setItem(key, String(value)); return true; }
      catch {
        // QuotaExceededError and private-mode storage failures must not break login.
        // Keep this session in memory and stop retrying a known-broken backend.
        persistent = false;
        try { storage.removeItem(key); } catch { /* Ignore a blocked cleanup. */ }
        return false;
      }
    },
    remove(key) {
      memory.set(key, "");
      try { storage?.removeItem(key); } catch { /* Keep the in-memory session usable. */ }
    },
  };
}

export function defaultAPIBase(location) {
  return location.origin + (/^\/codex(?:-admin|\/admin)?(?:\/|$)/.test(location.pathname) ? "/codex" : "");
}

export function normalizeAPIBase(value, location) {
  const input = String(value || "").trim();
  if (!input) return defaultAPIBase(location);
  const candidate = input.startsWith("/") ? new URL(input, location.origin) : new URL(/^[a-z][\w+.-]*:/i.test(input) ? input : `https://${input}`);
  if (!['http:', 'https:'].includes(candidate.protocol) || candidate.username || candidate.password) {
    throw new Error("服务地址请使用 HTTP 或 HTTPS 地址，不要包含账号密码");
  }
  let pathname = candidate.pathname.replace(/\/+$/, "");
  pathname = pathname.replace(/\/api(?:\/auth\/login)?$/, "");
  if (/\/codex-admin$/.test(pathname)) pathname = pathname.replace(/\/codex-admin$/, "/codex");
  if (/\/codex\/admin$/.test(pathname)) pathname = pathname.replace(/\/admin$/, "");
  if (pathname === "/admin") pathname = "";
  return candidate.origin + pathname;
}

export function initialAPIBase(location, stored, custom = false) {
  const fallback = defaultAPIBase(location);
  try {
    const normalized = normalizeAPIBase(stored || fallback, location);
    // Old deployments stored a bare domain, IP endpoint or the admin page URL.
    // Hosted consoles default to their own API unless a custom server was chosen.
    const hosted = fallback.endsWith("/codex");
    const mixedContent = location.protocol === 'https:' && new URL(normalized).protocol === 'http:';
    const baseUrl = mixedContent || (hosted && !custom) ? fallback : normalized;
    return { baseUrl, migrated: Boolean(stored) && stored.replace(/\/+$/, "") !== baseUrl };
  } catch {
    return { baseUrl: fallback, migrated: Boolean(stored) };
  }
}

const messages = {
  "username or password invalid": "账号或密码不正确，请重新输入",
  "admin only": "请使用管理员账号登录管理控制台",
  "unauthorized": "登录已过期，请重新登录",
  "login failed": "服务登录出现异常，请稍后重试",
  "issue token failed": "服务生成登录会话失败，请稍后重试",
};

export async function readJSONResponse(response) {
  const text = await response.text();
  const contentType = response.headers.get("content-type") || "";
  if (!contentType.includes("application/json")) {
    const error = new Error("服务返回了网页而不是接口数据，请恢复默认服务地址后重试");
    error.statusCode = response.status;
    throw error;
  }
  let data;
  try { data = text ? JSON.parse(text) : {}; }
  catch { throw new Error("服务返回的数据格式有误，请稍后重试"); }
  if (response.ok) return data;
  const message = data.error || data.message || `请求失败 (${response.status})`;
  const error = new Error(messages[message] || message);
  error.statusCode = response.status;
  throw error;
}

export async function fetchWithTimeout(url, options = {}, timeoutMs = 30000, fetcher = fetch) {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  try { return await fetcher(url, { ...options, signal: controller.signal }); }
  catch (error) {
    if (error?.name === "AbortError") throw new Error("请求超时，请检查连接后重试");
    if (error instanceof TypeError) throw new Error("服务连接失败，请检查服务地址或恢复默认地址");
    throw error;
  } finally { clearTimeout(timer); }
}

export function assertAdmin(operator) {
  if (String(operator?.username || "").trim().toLowerCase() !== "admin") {
    throw new Error("请使用管理员账号登录管理控制台");
  }
  return operator;
}
