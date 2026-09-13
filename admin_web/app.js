const storageKeys = {
  baseUrl: "chat_codex_admin_base_url",
  token: "chat_codex_admin_token",
  username: "chat_codex_admin_username",
};

const state = {
  baseUrl: initialBaseUrl(),
  token: localStorage.getItem(storageKeys.token) || "",
  username: localStorage.getItem(storageKeys.username) || "admin",
  view: initialView(),
  items: [],
  semanticAgents: [],
  skills: [],
  mcpCatalog: [],
  environmentPresets: [],
  runtimeCatalog: [],
  toolCatalog: [],
  editing: null,
  confirmResolver: null,
  agentSkillSelection: [],
  agentMcpSelection: [],
  agentRuntimeSelection: [],
  runtimeDraft: null,
  agentToolPermissions: {},
  referencePicker: {
    kind: "",
    selected: [],
    query: "",
  },
};

const el = {
  loginView: document.querySelector("#loginView"),
  appView: document.querySelector("#appView"),
  loginForm: document.querySelector("#loginForm"),
  loginBaseUrl: document.querySelector("#loginBaseUrl"),
  loginUsername: document.querySelector("#loginUsername"),
  loginPassword: document.querySelector("#loginPassword"),
  loginButton: document.querySelector("#loginButton"),
  navLinks: document.querySelectorAll("[data-view]"),
  topbarEyebrow: document.querySelector("#topbarEyebrow"),
  topbarTitle: document.querySelector("#topbarTitle"),
  topbarDescription: document.querySelector("#topbarDescription"),
  currentUser: document.querySelector("#currentUser"),
  logoutButton: document.querySelector("#logoutButton"),
  refreshButton: document.querySelector("#refreshButton"),
  newButton: document.querySelector("#newButton"),
  searchInput: document.querySelector("#searchInput"),
  categoryFilter: document.querySelector("#categoryFilter"),
  statusFilter: document.querySelector("#statusFilter"),
  grid: document.querySelector("#grid"),
  emptyState: document.querySelector("#emptyState"),
  statTotal: document.querySelector("#statTotal"),
  statTotalLabel: document.querySelector("#statTotalLabel"),
  statEnabled: document.querySelector("#statEnabled"),
  statEnabledLabel: document.querySelector("#statEnabledLabel"),
  statDisabled: document.querySelector("#statDisabled"),
  statDisabledLabel: document.querySelector("#statDisabledLabel"),
  statCategories: document.querySelector("#statCategories"),
  statCategoriesLabel: document.querySelector("#statCategoriesLabel"),
  drawerOverlay: document.querySelector("#drawerOverlay"),
  editorForm: document.querySelector("#editorForm"),
  drawerTitle: document.querySelector("#drawerTitle"),
  closeDrawerButton: document.querySelector("#closeDrawerButton"),
  cancelButton: document.querySelector("#cancelButton"),
  deleteButton: document.querySelector("#deleteButton"),
  saveButton: document.querySelector("#saveButton"),
  parseImportButton: document.querySelector("#parseImportButton"),
  importJson: document.querySelector("#importJson"),
  fieldTitle: document.querySelector("#fieldTitle"),
  fieldName: document.querySelector("#fieldName"),
  fieldSource: document.querySelector("#fieldSource"),
  fieldSortOrder: document.querySelector("#fieldSortOrder"),
  fieldEnabled: document.querySelector("#fieldEnabled"),
  fieldEnabledText: document.querySelector("#fieldEnabledText"),
  catalogFieldId: document.querySelector("#catalogFieldId"),
  catalogFieldTags: document.querySelector("#catalogFieldTags"),
  catalogFieldCategory: document.querySelector("#catalogFieldCategory"),
  catalogFieldRecommended: document.querySelector("#catalogFieldRecommended"),
  fieldCredentialUrl: document.querySelector("#fieldCredentialUrl"),
  fieldSourceUrl: document.querySelector("#fieldSourceUrl"),
  fieldDescription: document.querySelector("#fieldDescription"),
  fieldType: document.querySelector("#fieldType"),
  fieldTimeout: document.querySelector("#fieldTimeout"),
  fieldCommand: document.querySelector("#fieldCommand"),
  fieldUrl: document.querySelector("#fieldUrl"),
  fieldEnvironment: document.querySelector("#fieldEnvironment"),
  fieldHeaders: document.querySelector("#fieldHeaders"),
  fieldInstallPackageManager: document.querySelector("#fieldInstallPackageManager"),
  fieldInstallCommands: document.querySelector("#fieldInstallCommands"),
  fieldBuildCommands: document.querySelector("#fieldBuildCommands"),
  fieldRunCommandTemplate: document.querySelector("#fieldRunCommandTemplate"),
  fieldConfigPathTemplate: document.querySelector("#fieldConfigPathTemplate"),
  fieldExecutableNames: document.querySelector("#fieldExecutableNames"),
  fieldInstallEnvironment: document.querySelector("#fieldInstallEnvironment"),
  fieldInstallNotes: document.querySelector("#fieldInstallNotes"),
  mcpEditorSections: document.querySelectorAll(".mcp-editor-section"),
  semanticEditorSections: document.querySelectorAll(".semantic-editor-section"),
  skillEditorSections: document.querySelectorAll(".skill-editor-section"),
  environmentEditorSections: document.querySelectorAll(".environment-editor-section"),
  runtimeEditorSections: document.querySelectorAll(".runtime-editor-section"),
  toolEditorSections: document.querySelectorAll(".tool-editor-section"),
  catalogEditorFields: document.querySelectorAll(".catalog-editor-field"),
  agentFieldId: document.querySelector("#agentFieldId"),
  agentFieldName: document.querySelector("#agentFieldName"),
  agentFieldOpencodeName: document.querySelector("#agentFieldOpencodeName"),
  agentFieldIcon: document.querySelector("#agentFieldIcon"),
  agentFieldColor: document.querySelector("#agentFieldColor"),
  agentFieldSortOrder: document.querySelector("#agentFieldSortOrder"),
  agentFieldEnabled: document.querySelector("#agentFieldEnabled"),
  agentFieldDescription: document.querySelector("#agentFieldDescription"),
  agentFieldPrompt: document.querySelector("#agentFieldPrompt"),
  agentSkillPicker: document.querySelector("#agentSkillPicker"),
  agentMcpPicker: document.querySelector("#agentMcpPicker"),
  agentRuntimePicker: document.querySelector("#agentRuntimePicker"),
  addAgentSkillButton: document.querySelector("#addAgentSkillButton"),
  addAgentMcpButton: document.querySelector("#addAgentMcpButton"),
  addAgentRuntimeButton: document.querySelector("#addAgentRuntimeButton"),
  resetToolPermissionsButton: document.querySelector("#resetToolPermissionsButton"),
  applyToolPermissionsJsonButton: document.querySelector("#applyToolPermissionsJsonButton"),
  agentToolPermissionList: document.querySelector("#agentToolPermissionList"),
  agentFieldToolPermissions: document.querySelector("#agentFieldToolPermissions"),
  skillFieldId: document.querySelector("#skillFieldId"),
  skillFieldName: document.querySelector("#skillFieldName"),
  skillFieldSource: document.querySelector("#skillFieldSource"),
  skillFieldCategory: document.querySelector("#skillFieldCategory"),
  skillFieldTags: document.querySelector("#skillFieldTags"),
  skillFieldSortOrder: document.querySelector("#skillFieldSortOrder"),
  skillFieldEnabled: document.querySelector("#skillFieldEnabled"),
  skillFieldDescription: document.querySelector("#skillFieldDescription"),
  skillFieldContent: document.querySelector("#skillFieldContent"),
  skillFieldPackageFiles: document.querySelector("#skillFieldPackageFiles"),
  envFieldId: document.querySelector("#envFieldId"),
  envFieldName: document.querySelector("#envFieldName"),
  envFieldCategory: document.querySelector("#envFieldCategory"),
  envFieldSource: document.querySelector("#envFieldSource"),
  envFieldTags: document.querySelector("#envFieldTags"),
  envFieldSortOrder: document.querySelector("#envFieldSortOrder"),
  envFieldEnabled: document.querySelector("#envFieldEnabled"),
  envFieldDescription: document.querySelector("#envFieldDescription"),
  envFieldVariables: document.querySelector("#envFieldVariables"),
  runtimeFieldId: document.querySelector("#runtimeFieldId"),
  runtimeFieldName: document.querySelector("#runtimeFieldName"),
  runtimeFieldKind: document.querySelector("#runtimeFieldKind"),
  runtimeFieldInstallStrategy: document.querySelector("#runtimeFieldInstallStrategy"),
  runtimeFieldDefaultConstraint: document.querySelector("#runtimeFieldDefaultConstraint"),
  runtimeFieldExecutables: document.querySelector("#runtimeFieldExecutables"),
  runtimeFieldTags: document.querySelector("#runtimeFieldTags"),
  runtimeFieldSortOrder: document.querySelector("#runtimeFieldSortOrder"),
  runtimeFieldEnabled: document.querySelector("#runtimeFieldEnabled"),
  runtimeFieldDescription: document.querySelector("#runtimeFieldDescription"),
  runtimeFieldEnvTemplate: document.querySelector("#runtimeFieldEnvTemplate"),
  addRuntimeVersionButton: document.querySelector("#addRuntimeVersionButton"),
  addRuntimeMirrorButton: document.querySelector("#addRuntimeMirrorButton"),
  addRuntimeArtifactButton: document.querySelector("#addRuntimeArtifactButton"),
  runtimeVersionList: document.querySelector("#runtimeVersionList"),
  runtimeMirrorList: document.querySelector("#runtimeMirrorList"),
  runtimeArtifactList: document.querySelector("#runtimeArtifactList"),
  toolFieldId: document.querySelector("#toolFieldId"),
  toolFieldName: document.querySelector("#toolFieldName"),
  toolFieldCategory: document.querySelector("#toolFieldCategory"),
  toolFieldPermissionKey: document.querySelector("#toolFieldPermissionKey"),
  toolFieldPattern: document.querySelector("#toolFieldPattern"),
  toolFieldDefaultAction: document.querySelector("#toolFieldDefaultAction"),
  toolFieldTags: document.querySelector("#toolFieldTags"),
  toolFieldSortOrder: document.querySelector("#toolFieldSortOrder"),
  toolFieldEnabled: document.querySelector("#toolFieldEnabled"),
  toolFieldDescription: document.querySelector("#toolFieldDescription"),
  commandGroup: document.querySelector("#commandGroup"),
  urlGroup: document.querySelector("#urlGroup"),
  emptyTitle: document.querySelector("#emptyTitle"),
  emptyDescription: document.querySelector("#emptyDescription"),
  confirmOverlay: document.querySelector("#confirmOverlay"),
  confirmTitle: document.querySelector("#confirmTitle"),
  confirmMessage: document.querySelector("#confirmMessage"),
  confirmCancel: document.querySelector("#confirmCancel"),
  confirmOk: document.querySelector("#confirmOk"),
  referencePickerOverlay: document.querySelector("#referencePickerOverlay"),
  referencePickerTitle: document.querySelector("#referencePickerTitle"),
  referencePickerClose: document.querySelector("#referencePickerClose"),
  referencePickerSearch: document.querySelector("#referencePickerSearch"),
  referencePickerList: document.querySelector("#referencePickerList"),
  referencePickerCancel: document.querySelector("#referencePickerCancel"),
  referencePickerApply: document.querySelector("#referencePickerApply"),
  toast: document.querySelector("#toast"),
};

function bootstrap() {
  normalizeLegacyHash();
  el.loginBaseUrl.value = state.baseUrl;
  el.loginUsername.value = state.username;

  initCustomSelects();
  el.navLinks.forEach((link) => {
    link.addEventListener("click", (event) => {
      event.preventDefault();
      switchView(link.dataset.view || "mcpCatalog");
    });
  });
  el.loginForm.addEventListener("submit", login);
  el.logoutButton.addEventListener("click", logout);
  el.refreshButton.addEventListener("click", loadCurrentView);
  el.newButton.addEventListener("click", () => openEditor());
  el.searchInput.addEventListener("input", render);
  el.categoryFilter.addEventListener("change", render);
  el.statusFilter.addEventListener("change", render);
  el.closeDrawerButton.addEventListener("click", closeEditor);
  el.cancelButton.addEventListener("click", closeEditor);
  el.deleteButton.addEventListener("click", deleteCurrent);
  el.editorForm.addEventListener("submit", saveEditor);
  el.parseImportButton.addEventListener("click", parseImport);
  el.fieldType.addEventListener("change", syncTypeFields);
  el.addAgentSkillButton.addEventListener("click", () => openReferencePicker("skill"));
  el.addAgentMcpButton.addEventListener("click", () => openReferencePicker("mcp"));
  el.addAgentRuntimeButton.addEventListener("click", () => openReferencePicker("runtime"));
  el.addRuntimeVersionButton.addEventListener("click", () => addRuntimeNestedItem("version"));
  el.addRuntimeMirrorButton.addEventListener("click", () => addRuntimeNestedItem("mirror"));
  el.addRuntimeArtifactButton.addEventListener("click", () => addRuntimeNestedItem("artifact"));
  el.resetToolPermissionsButton.addEventListener("click", resetToolPermissionsFromCatalog);
  el.applyToolPermissionsJsonButton.addEventListener("click", applyToolPermissionsJson);
  el.drawerOverlay.addEventListener("click", (event) => {
    if (event.target === el.drawerOverlay) closeEditor();
  });
  el.confirmCancel.addEventListener("click", () => resolveConfirm(false));
  el.confirmOk.addEventListener("click", () => resolveConfirm(true));
  el.referencePickerClose.addEventListener("click", closeReferencePicker);
  el.referencePickerCancel.addEventListener("click", closeReferencePicker);
  el.referencePickerApply.addEventListener("click", applyReferencePicker);
  el.referencePickerSearch.addEventListener("input", () => {
    state.referencePicker.query = el.referencePickerSearch.value;
    renderReferencePickerList();
  });
  el.referencePickerOverlay.addEventListener("click", (event) => {
    if (event.target === el.referencePickerOverlay) closeReferencePicker();
  });

  if (state.token) {
    showApp();
    loadCurrentView();
  } else {
    showLogin();
  }
}

async function login(event) {
  event.preventDefault();
  const baseUrl = normalizeBaseUrl(el.loginBaseUrl.value);
  const username = el.loginUsername.value.trim();
  const password = el.loginPassword.value;
  if (!baseUrl || !username || !password) {
    showToast("请填写服务地址、账号和密码");
    return;
  }
  setBusy(el.loginButton, true, "登录中");
  try {
    const response = await fetch(`${baseUrl}/api/auth/login`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ username, password }),
    });
    const data = await readResponse(response);
    const token = data.access_token;
    if (!token) throw new Error("登录响应缺少 access_token");
    state.baseUrl = baseUrl;
    state.token = token;
    state.username = username;
    localStorage.setItem(storageKeys.baseUrl, baseUrl);
    localStorage.setItem(storageKeys.token, token);
    localStorage.setItem(storageKeys.username, username);
    showApp();
    await loadCurrentView();
  } catch (error) {
    showToast(error.message || "登录失败");
  } finally {
    setBusy(el.loginButton, false, "登录管理端");
  }
}

function logout() {
  state.token = "";
  localStorage.removeItem(storageKeys.token);
  showLogin();
}

function showLogin() {
  el.loginView.classList.remove("hidden");
  el.appView.classList.add("hidden");
}

function showApp() {
  el.loginView.classList.add("hidden");
  el.appView.classList.remove("hidden");
  el.currentUser.textContent = state.username || "admin";
  syncViewChrome();
}

function switchView(view) {
  state.view = normalizeView(view);
  state.editing = null;
  window.location.hash = hashForView(state.view);
  syncViewChrome();
  loadCurrentView();
}

function syncViewChrome() {
  el.navLinks.forEach((link) => {
    link.classList.toggle("active", (link.dataset.view || "mcp") === state.view);
  });
  const chrome = viewChrome(state.view);
  el.topbarEyebrow.textContent = chrome.eyebrow;
  el.topbarTitle.textContent = chrome.title;
  el.topbarDescription.textContent = chrome.description;
  el.newButton.textContent = chrome.newText;
  el.newButton.classList.toggle("hidden", !chrome.canCreate);
  el.emptyTitle.textContent = chrome.emptyTitle;
  el.emptyDescription.textContent = chrome.emptyDescription;
}

function loadCurrentView() {
  switch (state.view) {
    case "semantic":
      return loadSemanticAgents();
    case "skills":
      return loadSkills();
    case "mcpCatalog":
      return loadMCPCatalog();
    case "runtime":
      return loadRuntimeCatalog();
    case "environment":
      return loadEnvironmentPresets();
    case "tools":
      return loadToolCatalog();
    default:
      return loadMCPCatalog();
  }
}

async function loadSemanticAgents() {
  setBusy(el.refreshButton, true, "刷新中");
  try {
    const [agentsData, skillsData, mcpData, toolData, runtimeData] = await Promise.all([
      apiGet("/api/admin/semantic-agents"),
      apiGet("/api/admin/skills"),
      apiGet("/api/admin/mcp-catalog"),
      apiGet("/api/admin/tool-catalog"),
      apiGet("/api/admin/runtime-catalog"),
    ]);
    state.skills = Array.isArray(skillsData.items) ? skillsData.items : [];
    state.mcpCatalog = Array.isArray(mcpData.items) ? mcpData.items : [];
    state.toolCatalog = Array.isArray(toolData.items) ? toolData.items : [];
    state.runtimeCatalog = Array.isArray(runtimeData.items) ? runtimeData.items : [];
    const data = agentsData;
    state.semanticAgents = Array.isArray(data.items) ? data.items : [];
    render();
  } catch (error) {
    if (error.statusCode === 401 || error.statusCode === 403) {
      logout();
    }
    showToast(error.message || "加载语义 Agent 失败");
  } finally {
    setBusy(el.refreshButton, false, "刷新");
  }
}

async function loadSkills() {
  setBusy(el.refreshButton, true, "刷新中");
  try {
    const data = await apiGet("/api/admin/skills");
    state.skills = Array.isArray(data.items) ? data.items : [];
    render();
  } catch (error) {
    if (error.statusCode === 401 || error.statusCode === 403) {
      logout();
    }
    showToast(error.message || "加载 Skill 库失败");
  } finally {
    setBusy(el.refreshButton, false, "刷新");
  }
}

async function loadMCPCatalog() {
  setBusy(el.refreshButton, true, "刷新中");
  try {
    const data = await apiGet("/api/admin/mcp-catalog");
    state.mcpCatalog = Array.isArray(data.items) ? data.items : [];
    render();
  } catch (error) {
    if (error.statusCode === 401 || error.statusCode === 403) {
      logout();
    }
    showToast(error.message || "加载 MCP 库失败");
  } finally {
    setBusy(el.refreshButton, false, "刷新");
  }
}

async function loadEnvironmentPresets() {
  setBusy(el.refreshButton, true, "刷新中");
  try {
    const data = await apiGet("/api/admin/environment-presets");
    state.environmentPresets = Array.isArray(data.items) ? data.items : [];
    render();
  } catch (error) {
    if (error.statusCode === 401 || error.statusCode === 403) {
      logout();
    }
    showToast(error.message || "加载环境预设失败");
  } finally {
    setBusy(el.refreshButton, false, "刷新");
  }
}

async function loadRuntimeCatalog() {
  setBusy(el.refreshButton, true, "刷新中");
  try {
    const data = await apiGet("/api/admin/runtime-catalog");
    state.runtimeCatalog = Array.isArray(data.items) ? data.items : [];
    render();
  } catch (error) {
    if (error.statusCode === 401 || error.statusCode === 403) {
      logout();
    }
    showToast(error.message || "加载运行时环境失败");
  } finally {
    setBusy(el.refreshButton, false, "刷新");
  }
}

async function loadToolCatalog() {
  setBusy(el.refreshButton, true, "刷新中");
  try {
    const data = await apiGet("/api/admin/tool-catalog");
    state.toolCatalog = Array.isArray(data.items) ? data.items : [];
    render();
  } catch (error) {
    if (error.statusCode === 401 || error.statusCode === 403) {
      logout();
    }
    showToast(error.message || "加载工具库失败");
  } finally {
    setBusy(el.refreshButton, false, "刷新");
  }
}

function render() {
  syncViewChrome();
  renderStats();
  renderCategoryFilter();
  const items = filteredItems();
  el.grid.innerHTML = items.map(cardForCurrentView).join("");
  el.emptyState.classList.toggle("hidden", items.length > 0);
  el.grid.querySelectorAll("[data-action]").forEach((button) => {
    button.addEventListener("click", handleCardAction);
  });
}

function renderStats() {
  const items = currentItems();
  const enabled = items.filter((item) => item.enabled).length;
  const categories = statCategorySet(items);
  const chrome = viewChrome(state.view);
  el.statTotalLabel.textContent = chrome.totalLabel;
  el.statEnabledLabel.textContent = "已启用";
  el.statDisabledLabel.textContent = "已停用";
  el.statCategoriesLabel.textContent = chrome.categoryLabel;
  el.statTotal.textContent = items.length;
  el.statEnabled.textContent = enabled;
  el.statDisabled.textContent = items.length - enabled;
  el.statCategories.textContent = categories.size;
}

function renderCategoryFilter() {
  const current = el.categoryFilter.value || "all";
  let options;
  if (state.view === "semantic") {
    options = [
      `<option value="all">全部类型</option>`,
      `<option value="built_in">内置 Agent</option>`,
      `<option value="custom">自定义 Agent</option>`,
    ].join("");
  } else if (state.view === "mcpCatalog") {
    const optionsList = [
      `<option value="all">全部 MCP</option>`,
      `<option value="recommended">仅推荐</option>`,
      `<option value="normal">普通 MCP</option>`,
    ];
    const categories = [...new Set(currentItems().map((item) => item.category || "通用"))].sort();
    options = [
      ...optionsList,
      ...categories.map((category) => `<option value="category:${escapeAttr(category)}">${escapeHtml(category)}</option>`),
    ].join("");
  } else if (state.view === "skills") {
    const categories = [...new Set(currentItems().map((item) => item.category || "通用"))].sort();
    options = [
      `<option value="all">全部分类</option>`,
      ...categories.map((category) => `<option value="${escapeAttr(category)}">${escapeHtml(category)}</option>`),
    ].join("");
  } else if (state.view === "runtime") {
    const tags = [...new Set(currentItems().flatMap((item) => item.tags || []))].sort();
    options = [
      `<option value="all">全部标签</option>`,
      ...tags.map((tag) => `<option value="${escapeAttr(tag)}">${escapeHtml(tag)}</option>`),
    ].join("");
  } else if (state.view === "environment" || state.view === "tools") {
    const categories = [...new Set(currentItems().map((item) => item.category || "通用"))].sort();
    options = [
      `<option value="all">全部分类</option>`,
      ...categories.map((category) => `<option value="${escapeAttr(category)}">${escapeHtml(category)}</option>`),
    ].join("");
  } else {
    options = `<option value="all">全部</option>`;
  }
  if (el.categoryFilter.innerHTML !== options) {
    el.categoryFilter.innerHTML = options;
    const values = Array.from(el.categoryFilter.options).map((option) => option.value);
    el.categoryFilter.value = values.includes(current) ? current : "all";
    syncCustomSelect(el.categoryFilter);
  }
}

function filteredItems() {
  const query = el.searchInput.value.trim().toLowerCase();
  const category = el.categoryFilter.value || "all";
  const status = el.statusFilter.value || "all";
  return currentItems()
    .filter((item) => {
      if (state.view === "semantic") {
        if (category === "built_in" && !item.built_in) return false;
        if (category === "custom" && item.built_in) return false;
      } else if (state.view === "mcpCatalog") {
        if (category === "recommended" && !item.recommended) return false;
        if (category === "normal" && item.recommended) return false;
        if (category.startsWith("category:") && (item.category || "通用") !== category.slice(9)) return false;
      } else if (state.view === "skills") {
        if (category !== "all" && (item.category || "通用") !== category) return false;
      } else if (state.view === "runtime") {
        if (category !== "all" && !(item.tags || []).includes(category)) return false;
      } else if (state.view === "environment" || state.view === "tools") {
        if (category !== "all" && (item.category || "通用") !== category) return false;
      }
      if (status === "enabled" && !item.enabled) return false;
      if (status === "disabled" && item.enabled) return false;
      if (!query) return true;
      return searchableTextForCurrentView(item).includes(query);
    })
    .sort((left, right) => {
      if (state.view === "mcpCatalog") {
        const recommendedDelta = Number(Boolean(right.recommended)) - Number(Boolean(left.recommended));
        if (recommendedDelta !== 0) return recommendedDelta;
      }
      const sortDelta = (left.sort_order || 100) - (right.sort_order || 100);
      if (sortDelta !== 0) return sortDelta;
      return (right.updated_at || "").localeCompare(left.updated_at || "");
    });
}

function currentItems() {
  switch (state.view) {
    case "semantic":
      return state.semanticAgents;
    case "skills":
      return state.skills;
    case "mcpCatalog":
      return state.mcpCatalog;
    case "runtime":
      return state.runtimeCatalog;
    case "environment":
      return state.environmentPresets;
    case "tools":
      return state.toolCatalog;
    default:
      return state.mcpCatalog;
  }
}

function cardForCurrentView(item) {
  switch (state.view) {
    case "semantic":
      return semanticCardTemplate(item);
    case "skills":
      return skillCardTemplate(item);
    case "mcpCatalog":
      return mcpCatalogCardTemplate(item);
    case "runtime":
      return runtimeCatalogCardTemplate(item);
    case "environment":
      return environmentPresetCardTemplate(item);
    case "tools":
      return toolCatalogCardTemplate(item);
    default:
      return mcpCatalogCardTemplate(item);
  }
}

function semanticCardTemplate(item) {
  const skills = Array.isArray(item.skills) ? item.skills : [];
  const mcps = Array.isArray(item.recommended_mcp_servers) ? item.recommended_mcp_servers : [];
  const runtimes = Array.isArray(item.runtime_requirements) ? item.runtime_requirements : [];
  const updated = formatTime(item.updated_at);
  const title = item.name || item.id || "未命名 Agent";
  const id = item.id || "";
  const description = displayDescription(item.description);
  return `
    <article class="recommendation-card">
      <div class="card-head">
        <div>
          <h2 title="${escapeAttr(title)}">${escapeHtml(title)}</h2>
          <p class="muted" title="${escapeAttr(id)}">${escapeHtml(id)}</p>
        </div>
        <span class="pill ${item.enabled ? "enabled" : "disabled"}">${item.enabled ? "已启用" : "已停用"}</span>
      </div>
      <p class="card-description" title="${escapeAttr(description)}">${escapeHtml(description)}</p>
      <div class="pill-row">
        <span class="pill">${item.built_in ? "内置" : "自定义"}</span>
        ${pill(item.opencode_name || item.id || "")}
        <span class="pill">Skill ${skills.length}</span>
        <span class="pill">MCP ${mcps.length}</span>
        <span class="pill">运行时 ${runtimes.length}</span>
      </div>
      ${item.prompt ? `<div class="card-code" title="${escapeAttr(item.prompt)}">${escapeHtml(item.prompt)}</div>` : ""}
      <div class="pill-row">
        <span class="pill">排序 ${escapeHtml(String(item.sort_order || 100))}</span>
        <span class="pill">更新 ${escapeHtml(updated)}</span>
      </div>
      <div class="card-actions">
        <button class="secondary-button" type="button" data-action="edit" data-id="${escapeAttr(item.id)}">编辑</button>
        <button class="secondary-button" type="button" data-action="toggle" data-id="${escapeAttr(item.id)}">
          ${item.enabled ? "停用" : "启用"}
        </button>
      </div>
    </article>
  `;
}

function skillCardTemplate(item) {
  const updated = formatTime(item.updated_at);
  const tags = Array.isArray(item.tags) ? item.tags : [];
  const packageCount = Array.isArray(item.package_files) ? item.package_files.length : 0;
  const title = item.name || item.id || "未命名 Skill";
  const id = item.id || "";
  const description = displayDescription(item.description);
  return `
    <article class="recommendation-card">
      <div class="card-head">
        <div>
          <h2 title="${escapeAttr(title)}">${escapeHtml(title)}</h2>
          <p class="muted" title="${escapeAttr(id)}">${escapeHtml(id)}</p>
        </div>
        <span class="pill ${item.enabled ? "enabled" : "disabled"}">${item.enabled ? "已启用" : "已停用"}</span>
      </div>
      <p class="card-description" title="${escapeAttr(description)}">${escapeHtml(description)}</p>
      <div class="pill-row">
        <span class="pill">${escapeHtml(item.category || "通用")}</span>
        <span class="pill">${item.built_in ? "内置" : "自定义"}</span>
        ${pill(item.source || "Skill")}
        ${packageCount ? `<span class="pill warning">${packageCount} 个配套文件</span>` : ""}
        ${tags.map((tag) => pill(tag)).join("")}
      </div>
      ${item.content ? `<div class="card-code multiline" title="${escapeAttr(item.content)}">${escapeHtml(item.content)}</div>` : ""}
      <div class="pill-row">
        <span class="pill">排序 ${escapeHtml(String(item.sort_order || 100))}</span>
        <span class="pill">更新 ${escapeHtml(updated)}</span>
      </div>
      <div class="card-actions">
        <button class="secondary-button" type="button" data-action="edit" data-id="${escapeAttr(item.id)}">编辑</button>
        <button class="secondary-button" type="button" data-action="toggle" data-id="${escapeAttr(item.id)}">
          ${item.enabled ? "停用" : "启用"}
        </button>
        <button class="danger-button" type="button" data-action="delete" data-id="${escapeAttr(item.id)}">删除</button>
      </div>
    </article>
  `;
}

function mcpCatalogCardTemplate(item) {
  const config = item.config || {};
  const detail = config.type === "remote" ? config.url || "" : (config.command || []).join(" ");
  const envKeys = Object.keys(config.environment || {});
  const tags = Array.isArray(item.tags) ? item.tags : [];
  const updated = formatTime(item.updated_at);
  const title = item.title || item.name || item.id || "未命名 MCP";
  const id = item.id || item.name || "";
  const description = displayDescription(item.description);
  return `
    <article class="recommendation-card">
      <div class="card-head">
        <div>
          <h2 title="${escapeAttr(title)}">${escapeHtml(title)}</h2>
          <p class="muted" title="${escapeAttr(id)}">${escapeHtml(id)}</p>
        </div>
        <span class="pill ${item.enabled ? "enabled" : "disabled"}">${item.enabled ? "已启用" : "已停用"}</span>
      </div>
      <p class="card-description" title="${escapeAttr(description)}">${escapeHtml(description)}</p>
      <div class="pill-row">
        <span class="pill">${escapeHtml(item.category || "通用")}</span>
        <span class="pill">${item.recommended ? "推荐" : "普通"}</span>
        <span class="pill">${config.type === "remote" || item.type === "remote" ? "远程 HTTP" : "本地 stdio"}</span>
        <span class="pill">${item.built_in ? "内置" : "自定义"}</span>
        <span class="pill ${item.launch_ready === false ? "warning" : "enabled"}">${item.launch_ready === false ? "需安装解析" : "可启动"}</span>
        ${pill(item.source || "MCP 库")}
        ${envKeys.length ? `<span class="pill warning">${envKeys.length} 个环境变量</span>` : ""}
        ${tags.map((tag) => pill(tag)).join("")}
      </div>
      ${item.launch_ready === false && item.launch_block_reason ? `<p class="card-warning">${escapeHtml(item.launch_block_reason)}</p>` : ""}
      ${detail ? `<div class="card-code" title="${escapeAttr(detail)}">${escapeHtml(detail)}</div>` : ""}
      <div class="pill-row">
        <span class="pill">排序 ${escapeHtml(String(item.sort_order || 100))}</span>
        <span class="pill">更新 ${escapeHtml(updated)}</span>
      </div>
      <div class="card-actions">
        <button class="secondary-button" type="button" data-action="edit" data-id="${escapeAttr(item.id)}">编辑</button>
        <button class="secondary-button" type="button" data-action="toggle" data-id="${escapeAttr(item.id)}">
          ${item.enabled ? "停用" : "启用"}
        </button>
        <button class="danger-button" type="button" data-action="delete" data-id="${escapeAttr(item.id)}">删除</button>
      </div>
    </article>
  `;
}

function environmentPresetCardTemplate(item) {
  const variables = item.variables || {};
  const variableKeys = Object.keys(variables);
  const tags = Array.isArray(item.tags) ? item.tags : [];
  const updated = formatTime(item.updated_at);
  const preview = variableKeys.map((key) => `${key}=${variables[key]}`).join("\n");
  return `
    <article class="recommendation-card">
      <div class="card-head">
        <div>
          <h2>${escapeHtml(item.name || item.id || "未命名环境预设")}</h2>
          <p class="muted">${escapeHtml(item.id || "")}</p>
        </div>
        <span class="pill ${item.enabled ? "enabled" : "disabled"}">${item.enabled ? "已启用" : "已停用"}</span>
      </div>
      <p class="card-description">${escapeHtml(item.description || "暂无描述")}</p>
      <div class="pill-row">
        <span class="pill">${escapeHtml(item.category || "通用")}</span>
        <span class="pill">${item.built_in ? "内置" : "自定义"}</span>
        <span class="pill">${escapeHtml(item.source || "环境预设")}</span>
        <span class="pill warning">${variableKeys.length} 个变量</span>
        ${tags.map((tag) => `<span class="pill">${escapeHtml(tag)}</span>`).join("")}
      </div>
      ${preview ? `<div class="card-code multiline" title="${escapeAttr(preview)}">${escapeHtml(preview)}</div>` : ""}
      <div class="pill-row">
        <span class="pill">排序 ${escapeHtml(String(item.sort_order || 100))}</span>
        <span class="pill">更新 ${escapeHtml(updated)}</span>
      </div>
      <div class="card-actions">
        <button class="secondary-button" type="button" data-action="edit" data-id="${escapeAttr(item.id)}">编辑</button>
        <button class="secondary-button" type="button" data-action="toggle" data-id="${escapeAttr(item.id)}">
          ${item.enabled ? "停用" : "启用"}
        </button>
        <button class="danger-button" type="button" data-action="delete" data-id="${escapeAttr(item.id)}">删除</button>
      </div>
    </article>
  `;
}

function runtimeCatalogCardTemplate(item) {
  const versions = Array.isArray(item.versions) ? item.versions : [];
  const artifacts = Array.isArray(item.artifacts) ? item.artifacts : [];
  const mirrors = Array.isArray(item.mirrors) ? item.mirrors : [];
  const tags = Array.isArray(item.tags) ? item.tags : [];
  const updated = formatTime(item.updated_at);
  const defaultVersion = versions.find((version) => version.default_selected) || versions[0];
  const mirrorNames = mirrors.slice(0, 3).map((mirror) => mirror.name || mirror.id).join(" / ");
  return `
    <article class="recommendation-card">
      <div class="card-head">
        <div>
          <h2>${escapeHtml(item.name || item.id || "未命名运行时")}</h2>
          <p class="muted">${escapeHtml(item.id || "")}</p>
        </div>
        <span class="pill ${item.enabled ? "enabled" : "disabled"}">${item.enabled ? "已启用" : "已停用"}</span>
      </div>
      <p class="card-description">${escapeHtml(item.description || "暂无描述")}</p>
      <div class="pill-row">
        <span class="pill">${escapeHtml(runtimeKindLabel(item.runtime_kind))}</span>
        <span class="pill">${escapeHtml(installStrategyLabel(item.install_strategy))}</span>
        <span class="pill">版本 ${versions.length}</span>
        <span class="pill">镜像 ${mirrors.length}</span>
        <span class="pill">包 ${artifacts.length}</span>
        ${tags.map((tag) => `<span class="pill">${escapeHtml(tag)}</span>`).join("")}
      </div>
      <div class="runtime-summary">
        <span>默认：${escapeHtml(defaultVersion?.version || item.default_version_constraint || "-")}</span>
        <span>约束：${escapeHtml(item.default_version_constraint || "-")}</span>
        <span>镜像：${escapeHtml(mirrorNames || "-")}</span>
      </div>
      <div class="pill-row">
        <span class="pill">排序 ${escapeHtml(String(item.sort_order || 100))}</span>
        <span class="pill">更新 ${escapeHtml(updated)}</span>
      </div>
      <div class="card-actions">
        <button class="secondary-button" type="button" data-action="edit" data-id="${escapeAttr(item.id)}">编辑</button>
        <button class="secondary-button" type="button" data-action="toggle" data-id="${escapeAttr(item.id)}">
          ${item.enabled ? "停用" : "启用"}
        </button>
        <button class="danger-button" type="button" data-action="delete" data-id="${escapeAttr(item.id)}">删除</button>
      </div>
    </article>
  `;
}

function toolCatalogCardTemplate(item) {
  const tags = Array.isArray(item.tags) ? item.tags : [];
  const updated = formatTime(item.updated_at);
  const pattern = item.pattern || "*";
  const permissionSummary = `${item.permission_key || ""}:${pattern}`;
  return `
    <article class="recommendation-card">
      <div class="card-head">
        <div>
          <h2>${escapeHtml(item.name || item.id || "未命名工具")}</h2>
          <p class="muted">${escapeHtml(item.id || "")}</p>
        </div>
        <span class="pill ${item.enabled ? "enabled" : "disabled"}">${item.enabled ? "已启用" : "已停用"}</span>
      </div>
      <p class="card-description">${escapeHtml(item.description || "暂无描述")}</p>
      <div class="pill-row">
        <span class="pill">${escapeHtml(item.category || "通用")}</span>
        <span class="pill">${item.built_in ? "内置" : "自定义"}</span>
        <span class="pill">${escapeHtml(actionLabel(item.default_action))}</span>
        ${tags.map((tag) => `<span class="pill">${escapeHtml(tag)}</span>`).join("")}
      </div>
      <div class="card-code" title="${escapeAttr(permissionSummary)}">${escapeHtml(permissionSummary)}</div>
      <div class="pill-row">
        <span class="pill">排序 ${escapeHtml(String(item.sort_order || 100))}</span>
        <span class="pill">更新 ${escapeHtml(updated)}</span>
      </div>
      <div class="card-actions">
        <button class="secondary-button" type="button" data-action="edit" data-id="${escapeAttr(item.id)}">编辑</button>
        <button class="secondary-button" type="button" data-action="toggle" data-id="${escapeAttr(item.id)}">
          ${item.enabled ? "停用" : "启用"}
        </button>
        <button class="danger-button" type="button" data-action="delete" data-id="${escapeAttr(item.id)}">删除</button>
      </div>
    </article>
  `;
}

async function handleCardAction(event) {
  const button = event.currentTarget;
  if (button.disabled) return;
  const action = event.currentTarget.dataset.action;
  const id = event.currentTarget.dataset.id;
  const item = findItemByID(id);
  if (!item) return;
  if (action === "edit") {
    openEditor(item);
    return;
  }
  if (action === "toggle") {
    await setEnabled(item, !item.enabled, button);
    return;
  }
  if (action === "delete") {
    await deleteItem(item, false, button);
  }
}

function openEditor(item = null) {
  if (state.view === "semantic") {
    openSemanticEditor(item);
    return;
  }
  if (state.view === "skills") {
    openSkillEditor(item);
    return;
  }
  if (state.view === "environment") {
    openEnvironmentEditor(item);
    return;
  }
  if (state.view === "runtime") {
    openRuntimeEditor(item);
    return;
  }
  if (state.view === "tools") {
    openToolEditor(item);
    return;
  }
  state.editing = item;
  el.drawerTitle.textContent = item ? "编辑 MCP" : "新增 MCP";
  el.deleteButton.classList.toggle("hidden", !item);
  el.importJson.value = "";
  fillForm(item || defaultCatalogItem(), true);
  el.drawerOverlay.classList.remove("hidden");
  el.drawerOverlay.setAttribute("aria-hidden", "false");
  el.catalogFieldId.focus();
}

async function openSemanticEditor(item) {
  if (!item) return;
  state.editing = item;
  el.drawerTitle.textContent = "编辑语义 Agent";
  el.deleteButton.classList.add("hidden");
  el.importJson.value = "";
  showEditorSections("semantic");
  await ensureAgentReferenceLibraries();
  fillSemanticForm(item);
  el.drawerOverlay.classList.remove("hidden");
  el.drawerOverlay.setAttribute("aria-hidden", "false");
  el.agentFieldName.focus();
}

function openSkillEditor(item = null) {
  state.editing = item;
  el.drawerTitle.textContent = item ? "编辑 Skill" : "新增 Skill";
  el.deleteButton.classList.toggle("hidden", !item);
  el.importJson.value = "";
  showEditorSections("skills");
  fillSkillForm(item || defaultSkill());
  el.drawerOverlay.classList.remove("hidden");
  el.drawerOverlay.setAttribute("aria-hidden", "false");
  el.skillFieldId.focus();
}

function openEnvironmentEditor(item = null) {
  state.editing = item;
  el.drawerTitle.textContent = item ? "编辑环境预设" : "新增环境预设";
  el.deleteButton.classList.toggle("hidden", !item);
  el.importJson.value = "";
  showEditorSections("environment");
  fillEnvironmentForm(item || defaultEnvironmentPreset());
  el.drawerOverlay.classList.remove("hidden");
  el.drawerOverlay.setAttribute("aria-hidden", "false");
  el.envFieldId.focus();
}

function openRuntimeEditor(item = null) {
  state.editing = item;
  state.runtimeDraft = cloneRuntimeDraft(item || defaultRuntimeCatalogItem());
  el.drawerTitle.textContent = item ? "编辑运行时环境" : "新增运行时环境";
  el.deleteButton.classList.toggle("hidden", !item);
  el.importJson.value = "";
  showEditorSections("runtime");
  fillRuntimeForm(item || defaultRuntimeCatalogItem());
  el.drawerOverlay.classList.remove("hidden");
  el.drawerOverlay.setAttribute("aria-hidden", "false");
  el.runtimeFieldId.focus();
}

function openToolEditor(item = null) {
  state.editing = item;
  el.drawerTitle.textContent = item ? "编辑工具" : "新增工具";
  el.deleteButton.classList.toggle("hidden", !item);
  el.importJson.value = "";
  showEditorSections("tools");
  fillToolForm(item || defaultToolCatalogItem());
  el.drawerOverlay.classList.remove("hidden");
  el.drawerOverlay.setAttribute("aria-hidden", "false");
  el.toolFieldId.focus();
}

function closeEditor() {
  el.drawerOverlay.classList.add("hidden");
  el.drawerOverlay.setAttribute("aria-hidden", "true");
  state.editing = null;
  state.runtimeDraft = null;
  showEditorSections("mcpCatalog");
}

function defaultCatalogItem() {
  return {
    id: "",
    name: "",
    title: "",
    description: "",
    type: "local",
    source: "自定义",
    source_url: "",
    credential_url: "",
    category: "通用",
    recommended: false,
    enabled: true,
    tags: [],
    sort_order: 100,
    config: {
      name: "",
      type: "local",
      enabled: true,
      command: [],
      environment: {},
      headers: {},
    },
    install: {
      package_manager: "",
      source_url: "",
      install_commands: [],
      build_commands: [],
      run_command_template: [],
      config_path_template: "",
      executable_names: [],
      environment: {},
      install_notes: "",
    },
  };
}

function defaultSkill() {
  return {
    id: "",
    name: "",
    description: "",
    content: "",
    category: "通用",
    source: "自定义",
    enabled: true,
    tags: [],
    sort_order: 100,
  };
}

function defaultEnvironmentPreset() {
  return {
    id: "",
    name: "",
    description: "",
    category: "通用",
    source: "自定义",
    enabled: true,
    tags: [],
    variables: {},
    sort_order: 100,
  };
}

function defaultRuntimeCatalogItem() {
  return {
    id: "",
    name: "",
    description: "",
    runtime_kind: "language",
    default_version_constraint: "",
    executable_names: [],
    env_template: {},
    install_strategy: "archive",
    enabled: true,
    tags: [],
    sort_order: 100,
    versions: [],
    mirrors: [],
    artifacts: [],
  };
}

function cloneRuntimeDraft(item) {
  const base = defaultRuntimeCatalogItem();
  const source = item || base;
  return {
    ...base,
    ...source,
    executable_names: [...(source.executable_names || [])],
    env_template: { ...(source.env_template || {}) },
    tags: [...(source.tags || [])],
    versions: (source.versions || []).map((version) => ({
      ...version,
      min_os_version: { ...(version.min_os_version || {}) },
      install_manifest: { ...(version.install_manifest || {}) },
    })),
    mirrors: (source.mirrors || []).map((mirror) => ({
      ...mirror,
      headers: { ...(mirror.headers || {}) },
      tags: [...(mirror.tags || [])],
    })),
    artifacts: (source.artifacts || []).map((artifact) => ({
      ...artifact,
      bin_paths: [...(artifact.bin_paths || [])],
      env_patch: { ...(artifact.env_patch || {}) },
    })),
  };
}

function defaultToolCatalogItem() {
  return {
    id: "",
    name: "",
    description: "",
    category: "通用",
    permission_key: "",
    pattern: "*",
    default_action: "ask",
    enabled: true,
    tags: [],
    sort_order: 100,
  };
}

function fillForm(item) {
  showEditorSections("mcpCatalog");
  const source = normalizeCatalogForForm(item);
  const server = source.config || {};
  el.catalogFieldId.value = source.id || "";
  el.catalogFieldId.disabled = Boolean(state.editing);
  el.catalogFieldTags.value = (source.tags || []).join(", ");
  el.catalogFieldCategory.value = source.category || "通用";
  el.catalogFieldRecommended.checked = source.recommended !== false;
  el.fieldEnabledText.textContent = "启用 MCP";
  el.fieldTitle.value = source.title || "";
  el.fieldName.value = source.name || server.name || "";
  el.fieldSource.value = source.source || "自定义";
  el.fieldSortOrder.value = source.sort_order || 100;
  el.fieldEnabled.checked = source.enabled !== false;
  el.fieldCredentialUrl.value = source.credential_url || "";
  el.fieldSourceUrl.value = source.source_url || source.install?.source_url || "";
  el.fieldDescription.value = source.description || "";
  el.fieldType.value = server.type === "remote" ? "remote" : "local";
  el.fieldTimeout.value = server.timeout || "";
  el.fieldCommand.value = Array.isArray(server.command) ? server.command.join("\n") : "";
  el.fieldUrl.value = server.url || "";
  el.fieldEnvironment.value = mapToJson(server.environment || {});
  el.fieldHeaders.value = mapToJson(server.headers || {});
  el.fieldInstallPackageManager.value = source.install?.package_manager || "";
  el.fieldInstallCommands.value = Array.isArray(source.install?.install_commands) ? source.install.install_commands.join("\n") : "";
  el.fieldBuildCommands.value = Array.isArray(source.install?.build_commands) ? source.install.build_commands.join("\n") : "";
  el.fieldRunCommandTemplate.value = Array.isArray(source.install?.run_command_template) ? source.install.run_command_template.join("\n") : "";
  el.fieldConfigPathTemplate.value = source.install?.config_path_template || "";
  el.fieldExecutableNames.value = Array.isArray(source.install?.executable_names) ? source.install.executable_names.join(", ") : "";
  el.fieldInstallEnvironment.value = mapToJson(source.install?.environment || {});
  el.fieldInstallNotes.value = source.install?.install_notes || "";
  syncCustomSelect(el.fieldType);
  syncTypeFields();
}

function fillSemanticForm(item) {
  el.agentFieldId.value = item.id || "";
  el.agentFieldName.value = item.name || "";
  el.agentFieldOpencodeName.value = item.opencode_name || item.id || "";
  el.agentFieldIcon.value = item.icon || "";
  el.agentFieldColor.value = item.color || "";
  el.agentFieldSortOrder.value = item.sort_order || 100;
  el.agentFieldEnabled.checked = item.enabled !== false;
  el.agentFieldDescription.value = item.description || "";
  el.agentFieldPrompt.value = item.prompt || "";
  state.agentSkillSelection = selectedIDs(item.skill_ids, item.skills);
  state.agentMcpSelection = selectedIDs(item.mcp_ids, item.recommended_mcp_servers);
  state.agentRuntimeSelection = normalizeAgentRuntimeSelection(item.runtime_requirements || []);
  state.agentToolPermissions = normalizeToolPermissions(item.tool_permissions || {});
  renderSelectedReferences("skill");
  renderSelectedReferences("mcp");
  renderSelectedReferences("runtime");
  renderToolPermissionList();
  syncToolPermissionsJson();
}

function fillSkillForm(item) {
  el.skillFieldId.value = item.id || "";
  el.skillFieldId.disabled = Boolean(state.editing);
  el.skillFieldName.value = item.name || "";
  el.skillFieldSource.value = item.source || "自定义";
  el.skillFieldCategory.value = item.category || "通用";
  el.skillFieldTags.value = (item.tags || []).join(", ");
  el.skillFieldSortOrder.value = item.sort_order || 100;
  el.skillFieldEnabled.checked = item.enabled !== false;
  el.skillFieldDescription.value = item.description || "";
  el.skillFieldContent.value = item.content || "";
  el.skillFieldPackageFiles.value = jsonArrayText(item.package_files || []);
}

function fillEnvironmentForm(item) {
  el.envFieldId.value = item.id || "";
  el.envFieldId.disabled = Boolean(state.editing);
  el.envFieldName.value = item.name || "";
  el.envFieldCategory.value = item.category || "通用";
  el.envFieldSource.value = item.source || "自定义";
  el.envFieldTags.value = (item.tags || []).join(", ");
  el.envFieldSortOrder.value = item.sort_order || 100;
  el.envFieldEnabled.checked = item.enabled !== false;
  el.envFieldDescription.value = item.description || "";
  el.envFieldVariables.value = mapToJson(item.variables || {});
}

function fillRuntimeForm(item) {
  el.runtimeFieldId.value = item.id || "";
  el.runtimeFieldId.disabled = Boolean(state.editing);
  el.runtimeFieldName.value = item.name || "";
  el.runtimeFieldKind.value = item.runtime_kind || "language";
  el.runtimeFieldInstallStrategy.value = item.install_strategy || "archive";
  el.runtimeFieldDefaultConstraint.value = item.default_version_constraint || "";
  el.runtimeFieldExecutables.value = (item.executable_names || []).join(", ");
  el.runtimeFieldTags.value = (item.tags || []).join(", ");
  el.runtimeFieldSortOrder.value = item.sort_order || 100;
  el.runtimeFieldEnabled.checked = item.enabled !== false;
  el.runtimeFieldDescription.value = item.description || "";
  el.runtimeFieldEnvTemplate.value = mapToJson(item.env_template || {});
  renderRuntimeNestedLists();
  syncCustomSelect(el.runtimeFieldKind);
  syncCustomSelect(el.runtimeFieldInstallStrategy);
}

function fillToolForm(item) {
  el.toolFieldId.value = item.id || "";
  el.toolFieldId.disabled = Boolean(state.editing);
  el.toolFieldName.value = item.name || "";
  el.toolFieldCategory.value = item.category || "通用";
  el.toolFieldPermissionKey.value = item.permission_key || "";
  el.toolFieldPattern.value = item.pattern || "*";
  el.toolFieldDefaultAction.value = normalizePermissionAction(item.default_action) || "ask";
  el.toolFieldTags.value = (item.tags || []).join(", ");
  el.toolFieldSortOrder.value = item.sort_order || 100;
  el.toolFieldEnabled.checked = item.enabled !== false;
  el.toolFieldDescription.value = item.description || "";
  syncCustomSelect(el.toolFieldDefaultAction);
}

function syncTypeFields() {
  const remote = el.fieldType.value === "remote";
  el.commandGroup.classList.toggle("hidden", remote);
  el.urlGroup.classList.toggle("hidden", !remote);
}

async function saveEditor(event) {
  event.preventDefault();
  if (el.saveButton.disabled) return;
  if (state.view === "semantic") {
    await saveSemanticEditor();
    return;
  }
  if (state.view === "skills") {
    await saveSkillEditor();
    return;
  }
  if (state.view === "environment") {
    await saveEnvironmentEditor();
    return;
  }
  if (state.view === "runtime") {
    await saveRuntimeEditor();
    return;
  }
  if (state.view === "tools") {
    await saveToolEditor();
    return;
  }
  let payload;
  try {
    payload = catalogFormPayload();
  } catch (error) {
    showToast(error.message);
    return;
  }
  await runAdminAction({
    button: el.saveButton,
    busyText: "保存中",
    pendingText: state.editing ? "正在保存 MCP..." : "正在新增 MCP...",
    successText: state.editing ? "MCP 已保存" : "MCP 已新增",
    failureText: "MCP 保存失败",
    action: async () => {
      const id = state.editing ? state.editing.id : payload.id;
      await apiPost(state.editing ? `/api/admin/mcp-catalog/${encodeURIComponent(id)}` : "/api/admin/mcp-catalog", payload);
      closeEditor();
      await loadMCPCatalog();
    },
  });
}

async function saveSemanticEditor() {
  if (!state.editing) return;
  let payload;
  try {
    payload = semanticFormPayload();
  } catch (error) {
    showToast(error.message);
    return;
  }
  await runAdminAction({
    button: el.saveButton,
    busyText: "保存中",
    pendingText: "正在保存语义 Agent...",
    successText: "语义 Agent 已保存",
    failureText: "语义 Agent 保存失败",
    action: async () => {
      await apiPost(`/api/admin/semantic-agents/${encodeURIComponent(state.editing.id)}`, payload);
      closeEditor();
      await loadSemanticAgents();
    },
  });
}

async function saveSkillEditor() {
  let payload;
  try {
    payload = skillFormPayload();
  } catch (error) {
    showToast(error.message);
    return;
  }
  await runAdminAction({
    button: el.saveButton,
    busyText: "保存中",
    pendingText: state.editing ? "正在保存 Skill..." : "正在新增 Skill...",
    successText: state.editing ? "Skill 已保存" : "Skill 已新增",
    failureText: "Skill 保存失败",
    action: async () => {
      const id = state.editing ? state.editing.id : payload.id;
      await apiPost(state.editing ? `/api/admin/skills/${encodeURIComponent(id)}` : "/api/admin/skills", payload);
      closeEditor();
      await loadSkills();
    },
  });
}

async function saveEnvironmentEditor() {
  let payload;
  try {
    payload = environmentFormPayload();
  } catch (error) {
    showToast(error.message);
    return;
  }
  await runAdminAction({
    button: el.saveButton,
    busyText: "保存中",
    pendingText: state.editing ? "正在保存环境预设..." : "正在新增环境预设...",
    successText: state.editing ? "环境预设已保存" : "环境预设已新增",
    failureText: "环境预设保存失败",
    action: async () => {
      const id = state.editing ? state.editing.id : payload.id;
      await apiPost(state.editing ? `/api/admin/environment-presets/${encodeURIComponent(id)}` : "/api/admin/environment-presets", payload);
      closeEditor();
      await loadEnvironmentPresets();
    },
  });
}

async function saveRuntimeEditor() {
  let payload;
  try {
    payload = runtimeFormPayload();
  } catch (error) {
    showToast(error.message);
    return;
  }
  await runAdminAction({
    button: el.saveButton,
    busyText: "保存中",
    pendingText: state.editing ? "正在保存运行时环境..." : "正在新增运行时环境...",
    successText: state.editing ? "运行时环境已保存" : "运行时环境已新增",
    failureText: "运行时环境保存失败",
    action: async () => {
      const id = state.editing ? state.editing.id : payload.id;
      const stored = await apiPost(state.editing ? `/api/admin/runtime-catalog/${encodeURIComponent(id)}` : "/api/admin/runtime-catalog", payload);
      const runtimeId = stored.id || payload.id;
      await syncRuntimeNestedItems(runtimeId, payload);
      closeEditor();
      await loadRuntimeCatalog();
    },
  });
}

async function saveToolEditor() {
  let payload;
  try {
    payload = toolFormPayload();
  } catch (error) {
    showToast(error.message);
    return;
  }
  await runAdminAction({
    button: el.saveButton,
    busyText: "保存中",
    pendingText: state.editing ? "正在保存工具..." : "正在新增工具...",
    successText: state.editing ? "工具已保存" : "工具已新增",
    failureText: "工具保存失败",
    action: async () => {
      const id = state.editing ? state.editing.id : payload.id;
      await apiPost(state.editing ? `/api/admin/tool-catalog/${encodeURIComponent(id)}` : "/api/admin/tool-catalog", payload);
      closeEditor();
      await loadToolCatalog();
    },
  });
}

function formPayload() {
  const name = el.fieldName.value.trim();
  const type = el.fieldType.value;
  const command = lines(el.fieldCommand.value);
  const url = el.fieldUrl.value.trim();
  if (!name) throw new Error("服务名不能为空");
  if (type === "local" && command.length === 0) throw new Error("本地 MCP 需要填写启动命令");
  if (type === "remote" && !url) throw new Error("远程 MCP 需要填写 URL");
  const environment = parseMapJson(el.fieldEnvironment.value, "环境变量 JSON");
  const headers = parseMapJson(el.fieldHeaders.value, "请求头 JSON");
  const installEnvironment = parseMapJson(el.fieldInstallEnvironment.value, "安装环境变量 JSON");
  const enabled = el.fieldEnabled.checked;
  const server = {
    name,
    type,
    enabled,
    timeout: Number(el.fieldTimeout.value || 0),
    command: type === "local" ? command : [],
    url: type === "remote" ? url : "",
    environment,
    headers,
  };
  return {
    title: el.fieldTitle.value.trim() || name,
    name,
    description: el.fieldDescription.value.trim(),
    source: el.fieldSource.value.trim() || "自定义",
    source_url: el.fieldSourceUrl.value.trim(),
    credential_url: el.fieldCredentialUrl.value.trim(),
    enabled,
    sort_order: Number(el.fieldSortOrder.value || 100),
    server,
    install: {
      package_manager: el.fieldInstallPackageManager.value.trim(),
      source_url: el.fieldSourceUrl.value.trim(),
      install_commands: lines(el.fieldInstallCommands.value),
      build_commands: lines(el.fieldBuildCommands.value),
      run_command_template: lines(el.fieldRunCommandTemplate.value),
      config_path_template: el.fieldConfigPathTemplate.value.trim(),
      executable_names: commaList(el.fieldExecutableNames.value),
      environment: installEnvironment,
      install_notes: el.fieldInstallNotes.value.trim(),
    },
  };
}

function catalogFormPayload() {
  const base = formPayload();
  const id = el.catalogFieldId.value.trim() || base.name;
  if (!id) throw new Error("MCP ID 不能为空");
  return {
    id,
    name: base.name,
    title: base.title,
    description: base.description,
    type: base.server.type,
    source: base.source || "自定义",
    source_url: base.source_url,
    credential_url: base.credential_url,
    category: el.catalogFieldCategory.value.trim() || "通用",
    recommended: el.catalogFieldRecommended.checked,
    enabled: base.enabled,
    tags: commaList(el.catalogFieldTags.value),
    sort_order: base.sort_order,
    built_in: state.editing?.built_in === true,
    config: {
      name: base.name,
      type: base.server.type,
      enabled: base.enabled,
      timeout: base.server.timeout,
      url: base.server.url,
      headers: base.server.headers,
      command: base.server.command,
      environment: base.server.environment,
    },
    install: base.install,
  };
}

function skillFormPayload() {
  const id = el.skillFieldId.value.trim();
  const name = el.skillFieldName.value.trim();
  if (!id) throw new Error("Skill ID 不能为空");
  if (!name) throw new Error("名称不能为空");
  return {
    id,
    name,
    description: el.skillFieldDescription.value.trim(),
    content: el.skillFieldContent.value.trim(),
    package_files: parsePackageFilesJson(el.skillFieldPackageFiles.value),
    category: el.skillFieldCategory.value.trim() || "通用",
    source: el.skillFieldSource.value.trim() || "自定义",
    enabled: el.skillFieldEnabled.checked,
    tags: commaList(el.skillFieldTags.value),
    sort_order: Number(el.skillFieldSortOrder.value || 100),
    built_in: state.editing?.built_in === true,
  };
}

function environmentFormPayload() {
  const id = el.envFieldId.value.trim();
  const name = el.envFieldName.value.trim();
  if (!id) throw new Error("预设 ID 不能为空");
  if (!name) throw new Error("名称不能为空");
  const variables = parseMapJson(el.envFieldVariables.value, "环境变量 JSON");
  if (Object.keys(variables).length === 0) throw new Error("环境变量不能为空");
  return {
    id,
    name,
    description: el.envFieldDescription.value.trim(),
    category: el.envFieldCategory.value.trim() || "通用",
    source: el.envFieldSource.value.trim() || "自定义",
    enabled: el.envFieldEnabled.checked,
    tags: commaList(el.envFieldTags.value),
    variables,
    sort_order: Number(el.envFieldSortOrder.value || 100),
    built_in: state.editing?.built_in === true,
  };
}

function runtimeFormPayload() {
  const id = el.runtimeFieldId.value.trim();
  const name = el.runtimeFieldName.value.trim();
  if (!id) throw new Error("运行时 ID 不能为空");
  if (!name) throw new Error("名称不能为空");
  return {
    id,
    name,
    description: el.runtimeFieldDescription.value.trim(),
    runtime_kind: el.runtimeFieldKind.value || "language",
    default_version_constraint: el.runtimeFieldDefaultConstraint.value.trim(),
    executable_names: commaList(el.runtimeFieldExecutables.value),
    env_template: parseMapJson(el.runtimeFieldEnvTemplate.value, "环境模板 JSON"),
    install_strategy: el.runtimeFieldInstallStrategy.value || "archive",
    enabled: el.runtimeFieldEnabled.checked,
    tags: commaList(el.runtimeFieldTags.value),
    sort_order: Number(el.runtimeFieldSortOrder.value || 100),
    built_in: state.editing?.built_in === true,
    versions: collectRuntimeNestedItems("version"),
    mirrors: collectRuntimeNestedItems("mirror"),
    artifacts: collectRuntimeNestedItems("artifact"),
  };
}

function toolFormPayload() {
  const id = el.toolFieldId.value.trim();
  const name = el.toolFieldName.value.trim();
  const permissionKey = el.toolFieldPermissionKey.value.trim();
  if (!id) throw new Error("工具 ID 不能为空");
  if (!name) throw new Error("名称不能为空");
  if (!permissionKey) throw new Error("权限键不能为空");
  return {
    id,
    name,
    description: el.toolFieldDescription.value.trim(),
    category: el.toolFieldCategory.value.trim() || "通用",
    permission_key: permissionKey,
    pattern: el.toolFieldPattern.value.trim() || "*",
    default_action: normalizePermissionAction(el.toolFieldDefaultAction.value) || "ask",
    enabled: el.toolFieldEnabled.checked,
    tags: commaList(el.toolFieldTags.value),
    sort_order: Number(el.toolFieldSortOrder.value || 100),
    built_in: state.editing?.built_in === true,
  };
}

function semanticFormPayload() {
  const id = el.agentFieldId.value.trim();
  const name = el.agentFieldName.value.trim();
  if (!id) throw new Error("Agent ID 不能为空");
  if (!name) throw new Error("名称不能为空");
  const permissions = normalizeToolPermissions(state.agentToolPermissions);
  return {
    id,
    name,
    description: el.agentFieldDescription.value.trim(),
    icon: el.agentFieldIcon.value.trim(),
    color: el.agentFieldColor.value.trim(),
    opencode_name: el.agentFieldOpencodeName.value.trim() || id,
    prompt: el.agentFieldPrompt.value.trim(),
    skill_ids: state.agentSkillSelection.slice(),
    mcp_ids: state.agentMcpSelection.slice(),
    runtime_requirements: state.agentRuntimeSelection.map((item, index) => ({
      runtime_id: item.runtime_id,
      version_constraint: item.version_constraint || "",
      required: item.required !== false,
      purpose: item.purpose || "",
      sort_order: item.sort_order || (index + 1) * 10,
    })),
    tool_permissions: permissions,
    enabled: el.agentFieldEnabled.checked,
    sort_order: Number(el.agentFieldSortOrder.value || 100),
    built_in: state.editing?.built_in !== false,
  };
}

function parseImport() {
  try {
    const parsed = parseMcpJson(el.importJson.value);
    const item = defaultCatalogItem();
    item.title = parsed.name;
    item.name = parsed.name;
    item.description = state.editing?.description || "";
    item.source = state.editing?.source || "自定义";
    item.credential_url = state.editing?.credential_url || "";
    item.enabled = parsed.enabled;
    item.sort_order = state.editing?.sort_order || 100;
    item.id = state.editing?.id || parsed.name;
    item.recommended = state.editing?.recommended === true;
    item.tags = Array.isArray(state.editing?.tags) ? state.editing.tags.slice() : [];
    item.config = parsed;
    fillForm(item);
    showToast("已识别 MCP 配置并填入表单");
  } catch (error) {
    showToast(error.message || "识别失败");
  }
}

async function syncRuntimeNestedItems(runtimeId, payload) {
  await deleteRemovedRuntimeNestedItems(runtimeId, payload);
  for (const version of payload.versions || []) {
    await apiPost(version.id ? `/api/admin/runtime-versions/${encodeURIComponent(version.id)}` : "/api/admin/runtime-versions", {
      ...version,
      runtime_id: runtimeId,
    });
  }
  for (const mirror of payload.mirrors || []) {
    await apiPost(mirror.id ? `/api/admin/runtime-mirrors/${encodeURIComponent(mirror.id)}` : "/api/admin/runtime-mirrors", {
      ...mirror,
      runtime_id: mirror.runtime_id || runtimeId,
    });
  }
  for (const artifact of payload.artifacts || []) {
    await apiPost(artifact.id ? `/api/admin/runtime-artifacts/${encodeURIComponent(artifact.id)}` : "/api/admin/runtime-artifacts", {
      ...artifact,
      runtime_id: runtimeId,
    });
  }
}

async function deleteRemovedRuntimeNestedItems(runtimeId, payload) {
  if (!state.editing || state.view !== "runtime") return;
  const deleteMissing = async (kind, endpoint, nextItems) => {
    const key = kind === "version" ? "versions" : kind === "mirror" ? "mirrors" : "artifacts";
    const previousIDs = new Set((state.editing[key] || []).map((item) => item.id).filter(Boolean));
    const nextIDs = new Set((nextItems || []).map((item) => item.id).filter(Boolean));
    for (const id of previousIDs) {
      if (!nextIDs.has(id)) {
        await apiPost(`/api/admin/${endpoint}/${encodeURIComponent(id)}/delete`, { runtime_id: runtimeId });
      }
    }
  };
  await deleteMissing("version", "runtime-versions", payload.versions);
  await deleteMissing("mirror", "runtime-mirrors", payload.mirrors);
  await deleteMissing("artifact", "runtime-artifacts", payload.artifacts);
}

function renderRuntimeNestedLists() {
  renderRuntimeVersionList();
  renderRuntimeMirrorList();
  renderRuntimeArtifactList();
}

function runtimeEditingItem() {
  if (state.view !== "runtime") return defaultRuntimeCatalogItem();
  if (!state.runtimeDraft) {
    state.runtimeDraft = cloneRuntimeDraft(state.editing || defaultRuntimeCatalogItem());
  }
  return state.runtimeDraft;
}

function addRuntimeNestedItem(kind) {
  const draft = runtimeEditingItem();
  draft.id = el.runtimeFieldId.value.trim();
  if (kind === "version") {
    draft.versions = [...(draft.versions || []), {
      id: "",
      runtime_id: el.runtimeFieldId.value.trim(),
      version: "",
      channel: "stable",
      status: "available",
      version_order: 100,
      default_selected: false,
      enabled: true,
      sort_order: 100,
    }];
  } else if (kind === "mirror") {
    draft.mirrors = [...(draft.mirrors || []), {
      id: "",
      runtime_id: el.runtimeFieldId.value.trim(),
      name: "",
      base_url: "",
      url_template: "{base}/{filename}",
      priority: 100,
      timeout_seconds: 30,
      enabled: true,
      tags: [],
    }];
  } else if (kind === "artifact") {
    draft.artifacts = [...(draft.artifacts || []), {
      id: "",
      runtime_id: el.runtimeFieldId.value.trim(),
      version_id: "",
      platform: "darwin",
      arch: "arm64",
      package_kind: "archive",
      filename: "",
      sha256: "",
      bin_paths: [],
      env_patch: {},
      priority: 100,
      enabled: true,
    }];
  }
  renderRuntimeNestedLists();
  showToast(`已添加${runtimeNestedLabel(kind)}`);
}

function renderRuntimeVersionList() {
  const versions = runtimeEditingItem().versions || [];
  el.runtimeVersionList.innerHTML = versions.length
    ? versions.map((item, index) => runtimeVersionRowTemplate(item, index)).join("")
    : `<div class="choice-empty">暂无版本</div>`;
  bindRuntimeNestedInputs("version");
}

function renderRuntimeMirrorList() {
  const mirrors = runtimeEditingItem().mirrors || [];
  el.runtimeMirrorList.innerHTML = mirrors.length
    ? mirrors.map((item, index) => runtimeMirrorRowTemplate(item, index)).join("")
    : `<div class="choice-empty">暂无镜像源</div>`;
  bindRuntimeNestedInputs("mirror");
}

function renderRuntimeArtifactList() {
  const artifacts = runtimeEditingItem().artifacts || [];
  el.runtimeArtifactList.innerHTML = artifacts.length
    ? artifacts.map((item, index) => runtimeArtifactRowTemplate(item, index)).join("")
    : `<div class="choice-empty">暂无平台包</div>`;
  bindRuntimeNestedInputs("artifact");
}

function runtimeVersionRowTemplate(item, index) {
  return `
    <article class="runtime-nested-item" data-runtime-kind="version" data-runtime-index="${index}">
      <div class="form-grid three">
        <label><span>版本 ID</span><input data-runtime-field="id" value="${escapeAttr(item.id || "")}" placeholder="python-3.14" /></label>
        <label><span>版本</span><input data-runtime-field="version" value="${escapeAttr(item.version || "")}" placeholder="3.14" /></label>
        <label><span>通道</span><input data-runtime-field="channel" value="${escapeAttr(item.channel || "stable")}" /></label>
        <label><span>状态</span><input data-runtime-field="status" value="${escapeAttr(item.status || "available")}" /></label>
        <label><span>排序值</span><input data-runtime-field="version_order" type="number" value="${escapeAttr(item.version_order || 100)}" /></label>
        <label><span>显示排序</span><input data-runtime-field="sort_order" type="number" value="${escapeAttr(item.sort_order || 100)}" /></label>
        <label class="switch-row"><span>默认</span><input data-runtime-field="default_selected" type="checkbox" ${item.default_selected ? "checked" : ""} /></label>
        <label class="switch-row"><span>启用</span><input data-runtime-field="enabled" type="checkbox" ${item.enabled !== false ? "checked" : ""} /></label>
      </div>
      <button class="ghost-button compact-button" type="button" data-remove-runtime-nested="version" data-index="${index}">移除版本</button>
    </article>
  `;
}

function runtimeMirrorRowTemplate(item, index) {
  return `
    <article class="runtime-nested-item" data-runtime-kind="mirror" data-runtime-index="${index}">
      <div class="form-grid two">
        <label><span>镜像 ID</span><input data-runtime-field="id" value="${escapeAttr(item.id || "")}" placeholder="node-ustc" /></label>
        <label><span>名称</span><input data-runtime-field="name" value="${escapeAttr(item.name || "")}" /></label>
        <label><span>版本 ID</span><input data-runtime-field="version_id" value="${escapeAttr(item.version_id || "")}" placeholder="可选" /></label>
        <label><span>优先级</span><input data-runtime-field="priority" type="number" value="${escapeAttr(item.priority || 100)}" /></label>
        <label><span>平台</span><input data-runtime-field="platform" value="${escapeAttr(item.platform || "")}" placeholder="可选 darwin/windows" /></label>
        <label><span>架构</span><input data-runtime-field="arch" value="${escapeAttr(item.arch || "")}" placeholder="可选 arm64/amd64" /></label>
        <label><span>Base URL</span><input data-runtime-field="base_url" value="${escapeAttr(item.base_url || "")}" /></label>
        <label><span>超时秒</span><input data-runtime-field="timeout_seconds" type="number" value="${escapeAttr(item.timeout_seconds || 30)}" /></label>
      </div>
      <label><span>URL 模板</span><input data-runtime-field="url_template" value="${escapeAttr(item.url_template || "")}" placeholder="{base}/{version}/{filename}" /></label>
      <label><span>标签，逗号分隔</span><input data-runtime-field="tags" value="${escapeAttr((item.tags || []).join(", "))}" /></label>
      <label class="switch-row"><span>启用</span><input data-runtime-field="enabled" type="checkbox" ${item.enabled !== false ? "checked" : ""} /></label>
      <button class="ghost-button compact-button" type="button" data-remove-runtime-nested="mirror" data-index="${index}">移除镜像</button>
    </article>
  `;
}

function runtimeArtifactRowTemplate(item, index) {
  return `
    <article class="runtime-nested-item" data-runtime-kind="artifact" data-runtime-index="${index}">
      <div class="form-grid three">
        <label><span>包 ID</span><input data-runtime-field="id" value="${escapeAttr(item.id || "")}" /></label>
        <label><span>版本 ID</span><input data-runtime-field="version_id" value="${escapeAttr(item.version_id || "")}" /></label>
        <label><span>平台</span><input data-runtime-field="platform" value="${escapeAttr(item.platform || "darwin")}" /></label>
        <label><span>架构</span><input data-runtime-field="arch" value="${escapeAttr(item.arch || "arm64")}" /></label>
        <label><span>包类型</span><input data-runtime-field="package_kind" value="${escapeAttr(item.package_kind || "archive")}" /></label>
        <label><span>优先级</span><input data-runtime-field="priority" type="number" value="${escapeAttr(item.priority || 100)}" /></label>
      </div>
      <label><span>文件名</span><input data-runtime-field="filename" value="${escapeAttr(item.filename || "")}" /></label>
      <label><span>下载路径</span><input data-runtime-field="download_path" value="${escapeAttr(item.download_path || "")}" /></label>
      <label><span>SHA256</span><input data-runtime-field="sha256" value="${escapeAttr(item.sha256 || "")}" /></label>
      <label><span>bin 路径，逗号分隔</span><input data-runtime-field="bin_paths" value="${escapeAttr((item.bin_paths || []).join(", "))}" /></label>
      <label class="switch-row"><span>启用</span><input data-runtime-field="enabled" type="checkbox" ${item.enabled !== false ? "checked" : ""} /></label>
      <button class="ghost-button compact-button" type="button" data-remove-runtime-nested="artifact" data-index="${index}">移除包</button>
    </article>
  `;
}

function bindRuntimeNestedInputs(kind) {
  const selector = `[data-runtime-kind="${kind}"]`;
  document.querySelectorAll(selector).forEach((row) => {
    row.querySelectorAll("[data-runtime-field]").forEach((input) => {
      input.addEventListener("input", () => updateRuntimeNestedItem(kind, Number(row.dataset.runtimeIndex), input));
      input.addEventListener("change", () => updateRuntimeNestedItem(kind, Number(row.dataset.runtimeIndex), input));
    });
  });
  document.querySelectorAll(`[data-remove-runtime-nested="${kind}"]`).forEach((button) => {
    button.addEventListener("click", () => removeRuntimeNestedItem(kind, Number(button.dataset.index)));
  });
}

function updateRuntimeNestedItem(kind, index, input) {
  const item = runtimeNestedArray(kind)[index];
  if (!item) return;
  const field = input.dataset.runtimeField;
  if (input.type === "checkbox") {
    item[field] = input.checked;
  } else if (["sort_order", "version_order", "priority", "timeout_seconds", "size_bytes"].includes(field)) {
    item[field] = Number(input.value || 0);
  } else if (["tags", "bin_paths"].includes(field)) {
    item[field] = commaList(input.value);
  } else {
    item[field] = input.value.trim();
  }
}

function removeRuntimeNestedItem(kind, index) {
  const list = runtimeNestedArray(kind);
  if (!list[index]) return;
  list.splice(index, 1);
  renderRuntimeNestedLists();
  showToast(`已移除${runtimeNestedLabel(kind)}，保存后生效`);
}

function runtimeNestedLabel(kind) {
  if (kind === "version") return "版本";
  if (kind === "mirror") return "镜像源";
  if (kind === "artifact") return "平台包";
  return "条目";
}

function runtimeNestedArray(kind) {
  const draft = runtimeEditingItem();
  const key = kind === "version" ? "versions" : kind === "mirror" ? "mirrors" : "artifacts";
  if (!Array.isArray(draft[key])) draft[key] = [];
  return draft[key];
}

function collectRuntimeNestedItems(kind) {
  const list = runtimeNestedArray(kind);
  return list.map((item) => ({ ...item })).filter((item) => {
    if (kind === "version") return item.version || item.id;
    if (kind === "mirror") return item.name || item.id || item.base_url || item.url_template;
    return item.filename || item.id;
  });
}

function parseMcpJson(input) {
  const text = input.trim();
  if (!text) throw new Error("请先粘贴 MCP JSON");
  const decoded = JSON.parse(text);
  const root = decoded.mcpServers || decoded.servers || decoded.mcp || decoded;
  if (!root || typeof root !== "object" || Array.isArray(root)) {
    throw new Error("MCP 配置顶层必须是对象");
  }
  const entries = Object.entries(root);
  if (entries.length === 0) throw new Error("没有识别到 MCP 节点");
  const [fallbackName, raw] = entries[0];
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) {
    throw new Error("MCP 节点格式不正确");
  }
  const command = extractCommand(raw);
  const url = stringValue(raw.url);
  const headers = stringMap(raw.headers);
  const environment = Object.keys(stringMap(raw.environment)).length
    ? stringMap(raw.environment)
    : stringMap(raw.env);
  const type = normalizeType(raw.type) || (url || Object.keys(headers).length ? "remote" : "local");
  const name = stringValue(raw.name) || fallbackName;
  return {
    name,
    type,
    enabled: raw.enabled !== false,
    timeout: Number(raw.timeout || raw.timeout_ms || raw.timeoutMs || 0),
    url,
    headers,
    command,
    environment,
  };
}

async function setEnabled(item, enabled, button = null) {
  const title = item.title || item.name || item.id;
  const actionText = enabled ? "启用" : "停用";
  if (state.view === "semantic") {
    await runAdminAction({
      button,
      busyText: `${actionText}中`,
      pendingText: `正在${actionText}「${title}」...`,
      successText: enabled ? "已启用 Agent" : "已停用 Agent",
      failureText: "状态更新失败",
      action: async () => {
        await apiPost(`/api/admin/semantic-agents/${encodeURIComponent(item.id)}/enabled`, { enabled });
        await loadSemanticAgents();
      },
    });
    return;
  }
  if (state.view === "skills") {
    await runAdminAction({
      button,
      busyText: `${actionText}中`,
      pendingText: `正在${actionText}「${title}」...`,
      successText: enabled ? "已启用 Skill" : "已停用 Skill",
      failureText: "状态更新失败",
      action: async () => {
        await apiPost(`/api/admin/skills/${encodeURIComponent(item.id)}`, { ...item, enabled });
        await loadSkills();
      },
    });
    return;
  }
  if (state.view === "mcpCatalog") {
    await runAdminAction({
      button,
      busyText: `${actionText}中`,
      pendingText: `正在${actionText}「${title}」...`,
      successText: enabled ? "已启用 MCP" : "已停用 MCP",
      failureText: "状态更新失败",
      action: async () => {
        await apiPost(`/api/admin/mcp-catalog/${encodeURIComponent(item.id)}`, { ...item, enabled });
        await loadMCPCatalog();
      },
    });
    return;
  }
  if (state.view === "runtime") {
    await runAdminAction({
      button,
      busyText: `${actionText}中`,
      pendingText: `正在${actionText}「${title}」...`,
      successText: enabled ? "已启用运行时环境" : "已停用运行时环境",
      failureText: "状态更新失败",
      action: async () => {
        await apiPost(`/api/admin/runtime-catalog/${encodeURIComponent(item.id)}`, { ...item, enabled });
        await loadRuntimeCatalog();
      },
    });
    return;
  }
  if (state.view === "environment") {
    await runAdminAction({
      button,
      busyText: `${actionText}中`,
      pendingText: `正在${actionText}「${title}」...`,
      successText: enabled ? "已启用环境预设" : "已停用环境预设",
      failureText: "状态更新失败",
      action: async () => {
        await apiPost(`/api/admin/environment-presets/${encodeURIComponent(item.id)}/enabled`, { enabled });
        await loadEnvironmentPresets();
      },
    });
    return;
  }
  if (state.view === "tools") {
    await runAdminAction({
      button,
      busyText: `${actionText}中`,
      pendingText: `正在${actionText}「${title}」...`,
      successText: enabled ? "已启用工具" : "已停用工具",
      failureText: "状态更新失败",
      action: async () => {
        await apiPost(`/api/admin/tool-catalog/${encodeURIComponent(item.id)}`, { ...item, enabled });
        await loadToolCatalog();
      },
    });
    return;
  }
}

async function deleteCurrent() {
  if (!state.editing) return;
  await deleteItem(state.editing, true, el.deleteButton);
}

async function deleteItem(item, closeAfterDelete = false, button = null) {
  const chrome = viewChrome(state.view);
  const ok = await confirmAction(chrome.deleteTitle, `确认删除「${item.title || item.name || item.id}」吗？`);
  if (!ok) return;
  await runAdminAction({
    button,
    busyText: "删除中",
    pendingText: `正在删除「${item.title || item.name || item.id}」...`,
    successText: `${deleteEntityLabel()}已删除`,
    failureText: "删除失败",
    action: async () => {
      if (state.view === "semantic") {
        await apiPost(`/api/admin/semantic-agents/${encodeURIComponent(item.id)}/delete`, {});
      } else if (state.view === "skills") {
        await apiPost(`/api/admin/skills/${encodeURIComponent(item.id)}/delete`, {});
      } else if (state.view === "mcpCatalog") {
        await apiPost(`/api/admin/mcp-catalog/${encodeURIComponent(item.id)}/delete`, {});
      } else if (state.view === "environment") {
        await apiPost(`/api/admin/environment-presets/${encodeURIComponent(item.id)}/delete`, {});
      } else if (state.view === "runtime") {
        await apiPost(`/api/admin/runtime-catalog/${encodeURIComponent(item.id)}/delete`, {});
      } else if (state.view === "tools") {
        await apiPost(`/api/admin/tool-catalog/${encodeURIComponent(item.id)}/delete`, {});
      }
      if (closeAfterDelete) closeEditor();
      await loadCurrentView();
    },
  });
}

async function apiGet(path, options = {}) {
  const response = await requestWithTimeout(`${state.baseUrl}${path}`, {
    headers: { Authorization: `Bearer ${state.token}` },
  }, options.timeoutMs);
  return readResponse(response);
}

async function apiPost(path, body, options = {}) {
  const response = await requestWithTimeout(`${state.baseUrl}${path}`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${state.token}`,
    },
    body: JSON.stringify(body),
  }, options.timeoutMs);
  return readResponse(response);
}

async function requestWithTimeout(url, options, timeoutMs = 30000) {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  try {
    return await fetch(url, { ...options, signal: controller.signal });
  } catch (error) {
    if (error?.name === "AbortError") {
      throw new Error("请求超时，请检查服务状态或网络连接");
    }
    throw error;
  } finally {
    clearTimeout(timer);
  }
}

async function readResponse(response) {
  const text = await response.text();
  const contentType = response.headers.get("content-type") || "";
  if (!contentType.includes("application/json")) {
    const error = new Error(response.ok ? "服务返回格式不正确" : `服务地址不正确或接口不可用 (${response.status})`);
    error.statusCode = response.status;
    throw error;
  }
  let data;
  try {
    data = text ? JSON.parse(text) : {};
  } catch {
    const error = new Error("服务返回格式不正确");
    error.statusCode = response.status;
    throw error;
  }
  if (response.ok) return data;
  const error = new Error(data.error || data.message || `请求失败 (${response.status})`);
  error.statusCode = response.status;
  throw error;
}

function confirmAction(title, message) {
  el.confirmTitle.textContent = title;
  el.confirmMessage.textContent = message;
  el.confirmOverlay.classList.remove("hidden");
  el.confirmOverlay.setAttribute("aria-hidden", "false");
  return new Promise((resolve) => {
    state.confirmResolver = resolve;
  });
}

function resolveConfirm(value) {
  el.confirmOverlay.classList.add("hidden");
  el.confirmOverlay.setAttribute("aria-hidden", "true");
  if (state.confirmResolver) state.confirmResolver(value);
  state.confirmResolver = null;
}

async function runAdminAction({ button, busyText, pendingText, successText, failureText, action }) {
  const originalText = button ? button.textContent : "";
  if (button) setBusy(button, true, busyText || originalText);
  showToast(pendingText, { persist: true });
  try {
    await action();
    showToast(successText);
  } catch (error) {
    showToast(`${failureText || "操作失败"}：${error.message || "未知错误"}`);
  } finally {
    if (button) setBusy(button, false, originalText);
  }
}

function deleteEntityLabel() {
  switch (state.view) {
    case "skills":
      return "Skill ";
    case "mcpCatalog":
      return "MCP ";
    case "environment":
      return "环境预设";
    case "runtime":
      return "运行时环境";
    case "tools":
      return "工具";
    case "semantic":
      return "语义 Agent ";
    default:
      return "条目";
  }
}

function showToast(message, options = {}) {
  el.toast.textContent = message;
  el.toast.classList.remove("hidden");
  clearTimeout(showToast.timer);
  el.toast.classList.toggle("pending", Boolean(options.persist));
  if (!options.persist) {
    showToast.timer = setTimeout(() => el.toast.classList.add("hidden"), 3200);
  }
}

function setBusy(button, busy, text) {
  button.disabled = busy;
  button.textContent = text;
}

function initialView() {
  const hash = window.location.hash.replace(/^#/, "");
  if (hash === "semantic-agents") return "semantic";
  if (hash === "skills") return "skills";
  if (hash === "mcp-catalog" || hash === "recommendations") return "mcpCatalog";
  if (hash === "runtime-catalog") return "runtime";
  if (hash === "environment-presets") return "environment";
  if (hash === "tool-catalog") return "tools";
  return "mcpCatalog";
}

function normalizeLegacyHash() {
  if (window.location.hash.replace(/^#/, "") === "recommendations") {
    window.history.replaceState(null, "", "#mcp-catalog");
  }
}

function normalizeView(view) {
  if (view === "semantic" || view === "skills" || view === "mcpCatalog" || view === "runtime" || view === "environment" || view === "tools") {
    return view;
  }
  return "mcpCatalog";
}

function hashForView(view) {
  switch (view) {
    case "semantic":
      return "semantic-agents";
    case "skills":
      return "skills";
    case "mcpCatalog":
      return "mcp-catalog";
    case "runtime":
      return "runtime-catalog";
    case "environment":
      return "environment-presets";
    case "tools":
      return "tool-catalog";
    default:
      return "mcp-catalog";
  }
}

function viewChrome(view) {
  const views = {
    mcpCatalog: {
      eyebrow: "MCP Catalog",
      title: "MCP 库",
      description: "维护可被语义 Agent 引用的 MCP 启动配置；开启推荐后会显示在前面。",
      newText: "新增 MCP",
      canCreate: true,
      emptyTitle: "没有匹配的 MCP",
      emptyDescription: "调整筛选条件，或点击右上角新增 MCP。",
      totalLabel: "MCP 总数",
      categoryLabel: "分类数量",
      deleteTitle: "删除 MCP",
    },
    skills: {
      eyebrow: "Skill Library",
      title: "Skill 库",
      description: "维护可被语义 Agent 引用的 Skill 内容。",
      newText: "新增 Skill",
      canCreate: true,
      emptyTitle: "没有匹配的 Skill",
      emptyDescription: "调整筛选条件，或点击右上角新增 Skill。",
      totalLabel: "Skill 总数",
      categoryLabel: "分类数量",
      deleteTitle: "删除 Skill",
    },
    semantic: {
      eyebrow: "Semantic Agents",
      title: "语义 Agent",
      description: "维护内置语义 Agent 的提示词、Skill 引用、MCP 引用和工具权限。",
      newText: "新增 Agent",
      canCreate: false,
      emptyTitle: "没有匹配的语义 Agent",
      emptyDescription: "调整筛选条件，或刷新内置 Agent 配置。",
      totalLabel: "Agent 总数",
      categoryLabel: "Skill 数量",
      deleteTitle: "删除语义 Agent",
    },
    environment: {
      eyebrow: "Environment Presets",
      title: "环境变量预设",
      description: "维护设备和 Agent 环境变量页面可使用的动态预设。",
      newText: "新增预设",
      canCreate: true,
      emptyTitle: "没有匹配的环境预设",
      emptyDescription: "调整筛选条件，或点击右上角新增预设。",
      totalLabel: "预设总数",
      categoryLabel: "分类数量",
      deleteTitle: "删除环境预设",
    },
    runtime: {
      eyebrow: "Managed Runtime",
      title: "运行时环境",
      description: "维护语义 Agent 需要的 Python、Node、Java、Go、uv 运行时、版本、镜像源和平台包。",
      newText: "新增运行时",
      canCreate: true,
      emptyTitle: "没有匹配的运行时环境",
      emptyDescription: "调整筛选条件，或点击右上角新增运行时。",
      totalLabel: "运行时总数",
      categoryLabel: "标签数量",
      deleteTitle: "删除运行时环境",
    },
    tools: {
      eyebrow: "Tool Catalog",
      title: "工具库",
      description: "维护语义 Agent 可选择的工具权限、匹配模式和工具说明。",
      newText: "新增工具",
      canCreate: true,
      emptyTitle: "没有匹配的工具",
      emptyDescription: "调整筛选条件，或点击右上角新增工具。",
      totalLabel: "工具总数",
      categoryLabel: "分类数量",
      deleteTitle: "删除工具",
    },
  };
  return views[view] || views.mcpCatalog;
}

function statCategorySet(items) {
  if (state.view === "semantic") return new Set(items.flatMap((item) => item.skills || item.skill_ids || []));
  if (state.view === "mcpCatalog") return new Set(items.map((item) => item.category || "通用"));
  if (state.view === "skills") return new Set(items.map((item) => item.category || "通用"));
  if (state.view === "runtime") return new Set(items.flatMap((item) => item.tags || []));
  if (state.view === "environment" || state.view === "tools") return new Set(items.map((item) => item.category || "通用"));
  return new Set();
}

function findItemByID(id) {
  return currentItems().find((entry) => entry.id === id);
}

function showEditorSections(mode) {
  const semantic = mode === "semantic";
  const skill = mode === "skills";
  const environment = mode === "environment";
  const runtime = mode === "runtime";
  const tool = mode === "tools";
  const mcp = mode === "mcpCatalog";
  const catalog = mode === "mcpCatalog";
  el.mcpEditorSections.forEach((section) => section.classList.toggle("hidden", !mcp));
  el.semanticEditorSections.forEach((section) => section.classList.toggle("hidden", !semantic));
  el.skillEditorSections.forEach((section) => section.classList.toggle("hidden", !skill));
  el.environmentEditorSections.forEach((section) => section.classList.toggle("hidden", !environment));
  el.runtimeEditorSections.forEach((section) => section.classList.toggle("hidden", !runtime));
  el.toolEditorSections.forEach((section) => section.classList.toggle("hidden", !tool));
  el.catalogEditorFields.forEach((field) => field.classList.toggle("hidden", !catalog));
  el.parseImportButton.classList.toggle("hidden", !mcp);
  el.importJson.closest(".form-section")?.classList.toggle("hidden", !mcp);
}

async function ensureAgentReferenceLibraries() {
  const needsSkills = state.skills.length === 0;
  const needsMCPS = state.mcpCatalog.length === 0;
  const needsTools = state.toolCatalog.length === 0;
  const needsRuntime = state.runtimeCatalog.length === 0;
  if (!needsSkills && !needsMCPS && !needsTools && !needsRuntime) return;
  const [skillsData, mcpData, toolData, runtimeData] = await Promise.all([
    needsSkills ? apiGet("/api/admin/skills") : Promise.resolve({ items: state.skills }),
    needsMCPS ? apiGet("/api/admin/mcp-catalog") : Promise.resolve({ items: state.mcpCatalog }),
    needsTools ? apiGet("/api/admin/tool-catalog") : Promise.resolve({ items: state.toolCatalog }),
    needsRuntime ? apiGet("/api/admin/runtime-catalog") : Promise.resolve({ items: state.runtimeCatalog }),
  ]);
  state.skills = Array.isArray(skillsData.items) ? skillsData.items : [];
  state.mcpCatalog = Array.isArray(mcpData.items) ? mcpData.items : [];
  state.toolCatalog = Array.isArray(toolData.items) ? toolData.items : [];
  state.runtimeCatalog = Array.isArray(runtimeData.items) ? runtimeData.items : [];
}

function selectedIDs(primary, fallback) {
  const values = Array.isArray(primary) && primary.length ? primary : fallback;
  return (Array.isArray(values) ? values : []).map(String).filter(Boolean);
}

function renderSelectedReferences(kind) {
  const container = kind === "skill" ? el.agentSkillPicker : kind === "mcp" ? el.agentMcpPicker : el.agentRuntimePicker;
  const selected = selectedReferenceIDs(kind);
  const library = referenceLibrary(kind);
  if (!selected.length) {
    container.innerHTML = `<div class="choice-empty">暂无已添加项</div>`;
    return;
  }
  container.innerHTML = selected
    .map((id) => selectedReferenceTemplate(resolveReferenceItem(id, library), id, kind))
    .join("");
  container.querySelectorAll("[data-remove-reference]").forEach((button) => {
    button.addEventListener("click", () => removeSelectedReference(kind, button.dataset.removeReference));
  });
  if (kind === "runtime") bindRuntimeRequirementControls();
}

function selectedReferenceTemplate(item, id, kind) {
  const tags = Array.isArray(item?.tags) ? item.tags : [];
  const runtimeRequirement = kind === "runtime" ? runtimeRequirementByID(id) : null;
  return `
    <article class="selected-reference-item ${item?.enabled === false ? "disabled-choice" : ""}">
      <div class="choice-body">
        <strong>${escapeHtml(referenceTitle(item, id))}</strong>
        <span class="choice-meta">${escapeHtml(id)} · ${escapeHtml(referenceDetail(item, kind))}</span>
        ${runtimeRequirement ? runtimeRequirementControlsTemplate(runtimeRequirement) : ""}
        ${item?.description ? `<span class="choice-description">${escapeHtml(item.description)}</span>` : ""}
        ${tags.length ? `<span class="choice-tags">${tags.map((tag) => `<em>${escapeHtml(tag)}</em>`).join("")}</span>` : ""}
      </div>
      <button class="ghost-button compact-button" type="button" data-remove-reference="${escapeAttr(id)}">移除</button>
    </article>
  `;
}

function openReferencePicker(kind) {
  state.referencePicker = {
    kind,
    selected: selectedReferenceIDs(kind),
    query: "",
  };
  el.referencePickerTitle.textContent = kind === "skill" ? "选择 Skill" : kind === "mcp" ? "选择 MCP" : "选择运行时环境";
  el.referencePickerSearch.value = "";
  renderReferencePickerList();
  el.referencePickerOverlay.classList.remove("hidden");
  el.referencePickerOverlay.setAttribute("aria-hidden", "false");
  el.referencePickerSearch.focus();
}

function closeReferencePicker() {
  el.referencePickerOverlay.classList.add("hidden");
  el.referencePickerOverlay.setAttribute("aria-hidden", "true");
}

function applyReferencePicker() {
  const selected = state.referencePicker.selected.slice();
  if (state.referencePicker.kind === "skill") {
    state.agentSkillSelection = selected;
    renderSelectedReferences("skill");
  } else if (state.referencePicker.kind === "mcp") {
    state.agentMcpSelection = selected;
    renderSelectedReferences("mcp");
  } else if (state.referencePicker.kind === "runtime") {
    applyRuntimeReferenceSelection(selected);
    renderSelectedReferences("runtime");
  }
  closeReferencePicker();
}

function renderReferencePickerList() {
  const kind = state.referencePicker.kind;
  const query = (state.referencePicker.query || "").trim().toLowerCase();
  const library = referenceLibrary(kind);
  const selectedSet = new Set(state.referencePicker.selected);
  const filtered = library.filter((item) => !query || referenceSearchText(item, kind).includes(query));
  if (!filtered.length) {
    el.referencePickerList.innerHTML = `<div class="choice-empty">没有匹配项</div>`;
    return;
  }
  el.referencePickerList.innerHTML = filtered
    .map((item) => referencePickerItemTemplate(item, selectedSet, kind))
    .join("");
  el.referencePickerList.querySelectorAll("[data-reference-id]").forEach((input) => {
    input.addEventListener("change", () => toggleReferenceSelection(input.value, input.checked));
  });
}

function referencePickerItemTemplate(item, selectedSet, kind) {
  const id = item.id || item.name;
  const tags = Array.isArray(item.tags) ? item.tags : [];
  return `
    <label class="choice-item ${item.enabled === false ? "disabled-choice" : ""}">
      <input
        type="checkbox"
        value="${escapeAttr(id)}"
        data-reference-id="${escapeAttr(id)}"
        ${selectedSet.has(id) || selectedSet.has(item.name) ? "checked" : ""}
      />
      <span class="choice-body">
        <strong>${escapeHtml(referenceTitle(item, id))}</strong>
        <span class="choice-meta">${escapeHtml(id)} · ${escapeHtml(referenceDetail(item, kind))}</span>
        ${item.description ? `<span class="choice-description">${escapeHtml(item.description)}</span>` : ""}
        ${tags.length ? `<span class="choice-tags">${tags.map((tag) => `<em>${escapeHtml(tag)}</em>`).join("")}</span>` : ""}
      </span>
    </label>
  `;
}

function toggleReferenceSelection(id, checked) {
  const selected = state.referencePicker.selected.filter((item) => item !== id);
  if (checked) selected.push(id);
  state.referencePicker.selected = selected;
}

function removeSelectedReference(kind, id) {
  if (kind === "skill") {
    state.agentSkillSelection = state.agentSkillSelection.filter((item) => item !== id);
  } else if (kind === "mcp") {
    state.agentMcpSelection = state.agentMcpSelection.filter((item) => item !== id);
  } else if (kind === "runtime") {
    state.agentRuntimeSelection = state.agentRuntimeSelection.filter((item) => item.runtime_id !== id);
  }
  renderSelectedReferences(kind);
}

function renderToolPermissionList() {
  if (!state.toolCatalog.length) {
    el.agentToolPermissionList.innerHTML = `<div class="choice-empty">工具库为空，请先在工具库维护工具权限</div>`;
    return;
  }
  const items = state.toolCatalog
    .slice()
    .sort((left, right) => {
      const sortDelta = (left.sort_order || 100) - (right.sort_order || 100);
      if (sortDelta !== 0) return sortDelta;
      return (left.category || "").localeCompare(right.category || "") || (left.name || "").localeCompare(right.name || "");
    });
  el.agentToolPermissionList.innerHTML = items.map((item) => toolPermissionRowTemplate(item)).join("");
  el.agentToolPermissionList.querySelectorAll("[data-tool-permission-id]").forEach((select) => {
    select.addEventListener("change", () => updateToolPermission(select.dataset.toolPermissionId, select.value));
  });
}

function selectedReferenceIDs(kind) {
  if (kind === "skill") return state.agentSkillSelection.slice();
  if (kind === "mcp") return state.agentMcpSelection.slice();
  return state.agentRuntimeSelection.map((item) => item.runtime_id).filter(Boolean);
}

function referenceLibrary(kind) {
  if (kind === "skill") return state.skills;
  if (kind === "mcp") return state.mcpCatalog;
  return state.runtimeCatalog;
}

function normalizeAgentRuntimeSelection(input) {
  if (!Array.isArray(input)) return [];
  return input
    .map((item, index) => ({
      runtime_id: String(item.runtime_id || "").trim(),
      version_constraint: String(item.version_constraint || "").trim(),
      required: item.required !== false,
      purpose: String(item.purpose || "").trim(),
      sort_order: Number(item.sort_order || (index + 1) * 10),
    }))
    .filter((item) => item.runtime_id);
}

function applyRuntimeReferenceSelection(ids) {
  const existing = new Map(state.agentRuntimeSelection.map((item) => [item.runtime_id, item]));
  state.agentRuntimeSelection = ids.map((id, index) => {
    const found = existing.get(id) || {};
    const runtime = state.runtimeCatalog.find((item) => item.id === id) || {};
    return {
      runtime_id: id,
      version_constraint: found.version_constraint || runtime.default_version_constraint || "",
      required: found.required !== false,
      purpose: found.purpose || "",
      sort_order: found.sort_order || (index + 1) * 10,
    };
  });
}

function runtimeRequirementByID(id) {
  return state.agentRuntimeSelection.find((item) => item.runtime_id === id) || null;
}

function runtimeRequirementControlsTemplate(item) {
  return `
    <div class="runtime-requirement-controls" data-runtime-requirement="${escapeAttr(item.runtime_id)}">
      <label><span>版本约束</span><input data-runtime-requirement-field="version_constraint" value="${escapeAttr(item.version_constraint || "")}" placeholder=">=3.12 <3.15" /></label>
      <label><span>用途</span><input data-runtime-requirement-field="purpose" value="${escapeAttr(item.purpose || "")}" placeholder="运行 MCP 或工具链" /></label>
      <label class="switch-row compact-switch"><span>必需</span><input data-runtime-requirement-field="required" type="checkbox" ${item.required !== false ? "checked" : ""} /></label>
    </div>
  `;
}

function bindRuntimeRequirementControls() {
  el.agentRuntimePicker.querySelectorAll("[data-runtime-requirement]").forEach((root) => {
    const runtimeID = root.dataset.runtimeRequirement;
    root.querySelectorAll("[data-runtime-requirement-field]").forEach((input) => {
      input.addEventListener("input", () => updateRuntimeRequirement(runtimeID, input));
      input.addEventListener("change", () => updateRuntimeRequirement(runtimeID, input));
    });
  });
}

function updateRuntimeRequirement(runtimeID, input) {
  const item = runtimeRequirementByID(runtimeID);
  if (!item) return;
  const field = input.dataset.runtimeRequirementField;
  item[field] = input.type === "checkbox" ? input.checked : input.value.trim();
}

function toolPermissionRowTemplate(item) {
  const action = permissionActionForTool(item, state.agentToolPermissions);
  const tags = Array.isArray(item.tags) ? item.tags : [];
  const pattern = item.pattern || "*";
  const permissionSummary = `${item.permission_key || ""}:${pattern}`;
  return `
    <article class="tool-permission-row ${item.enabled === false ? "disabled-choice" : ""}">
      <div class="tool-permission-main">
        <strong>${escapeHtml(item.name || item.id || "未命名工具")}</strong>
        <span class="tool-permission-description">${escapeHtml(item.description || "暂无描述")}</span>
        <span class="tool-permission-meta">${escapeHtml(permissionSummary)} · 默认 ${escapeHtml(actionLabel(item.default_action))}</span>
        ${tags.length ? `<span class="choice-tags">${tags.map((tag) => `<em>${escapeHtml(tag)}</em>`).join("")}</span>` : ""}
      </div>
      <label class="tool-permission-action">
        <span>权限</span>
        <select data-tool-permission-id="${escapeAttr(item.id)}">
          <option value="" ${action ? "" : "selected"}>未配置</option>
          <option value="ask" ${action === "ask" ? "selected" : ""}>询问</option>
          <option value="allow" ${action === "allow" ? "selected" : ""}>允许</option>
          <option value="deny" ${action === "deny" ? "selected" : ""}>拒绝</option>
        </select>
      </label>
    </article>
  `;
}

function updateToolPermission(id, action) {
  const item = state.toolCatalog.find((entry) => entry.id === id);
  if (!item) return;
  const next = normalizeToolPermissions(state.agentToolPermissions);
  setPermissionActionForTool(next, item, action);
  state.agentToolPermissions = next;
  syncToolPermissionsJson();
}

function resetToolPermissionsFromCatalog() {
  state.agentToolPermissions = permissionsFromToolCatalog(true);
  renderToolPermissionList();
  syncToolPermissionsJson();
  showToast("已按工具库默认权限重置");
}

function applyToolPermissionsJson() {
  try {
    state.agentToolPermissions = normalizeToolPermissions(parseObjectJson(el.agentFieldToolPermissions.value, "工具权限 JSON"));
  } catch (error) {
    showToast(error.message);
    return;
  }
  renderToolPermissionList();
  syncToolPermissionsJson();
  showToast("已从 JSON 更新权限列表");
}

function syncToolPermissionsJson() {
  el.agentFieldToolPermissions.value = prettyJson(normalizeToolPermissions(state.agentToolPermissions));
}

function permissionsFromToolCatalog(defaultOnly) {
  const permissions = {};
  state.toolCatalog.forEach((item) => {
    if (item.enabled === false) return;
    const action = defaultOnly ? normalizePermissionAction(item.default_action) : permissionActionForTool(item, state.agentToolPermissions);
    setPermissionActionForTool(permissions, item, action);
  });
  return normalizeToolPermissions(permissions);
}

function permissionActionForTool(item, permissions) {
  const key = item.permission_key || "";
  if (!key) return "";
  const pattern = item.pattern || "*";
  const value = permissions?.[key];
  if (typeof value === "string") {
    return pattern === "*" ? normalizePermissionAction(value) : "";
  }
  if (value && typeof value === "object" && !Array.isArray(value)) {
    return normalizePermissionAction(value[pattern]);
  }
  return "";
}

function setPermissionActionForTool(permissions, item, action) {
  const key = item.permission_key || "";
  if (!key) return;
  const pattern = item.pattern || "*";
  const normalizedAction = normalizePermissionAction(action);
  if (!normalizedAction) {
    removePermissionActionForTool(permissions, key, pattern);
    return;
  }
  const useNestedRule = shouldUseNestedPermissionRule(key, permissions);
  if (pattern === "*" && !useNestedRule) {
    permissions[key] = normalizedAction;
    return;
  }
  const current = permissions[key];
  const next = current && typeof current === "object" && !Array.isArray(current) ? { ...current } : {};
  if (typeof current === "string") {
    const currentAction = normalizePermissionAction(current);
    if (currentAction) next["*"] = currentAction;
  }
  next[pattern] = normalizedAction;
  permissions[key] = next;
}

function removePermissionActionForTool(permissions, key, pattern) {
  const current = permissions[key];
  if (pattern === "*" && (typeof current === "string" || !shouldUseNestedPermissionRule(key, permissions))) {
    delete permissions[key];
    return;
  }
  if (!current || typeof current !== "object" || Array.isArray(current)) return;
  const next = { ...current };
  delete next[pattern];
  if (Object.keys(next).length) {
    permissions[key] = next;
  } else {
    delete permissions[key];
  }
}

function shouldUseNestedPermissionRule(key, permissions) {
  const current = permissions?.[key];
  if (current && typeof current === "object" && !Array.isArray(current)) return true;
  return state.toolCatalog.some((item) => item.permission_key === key && (item.pattern || "*") !== "*");
}

function normalizeToolPermissions(input) {
  if (!input || typeof input !== "object" || Array.isArray(input)) return {};
  const out = {};
  Object.entries(input).forEach(([rawKey, rawValue]) => {
    const key = String(rawKey).trim();
    if (!key) return;
    if (typeof rawValue === "string") {
      const action = normalizePermissionAction(rawValue);
      if (action) out[key] = action;
      return;
    }
    if (!rawValue || typeof rawValue !== "object" || Array.isArray(rawValue)) return;
    const nested = {};
    Object.entries(rawValue).forEach(([rawPattern, rawAction]) => {
      const pattern = String(rawPattern).trim() || "*";
      const action = normalizePermissionAction(rawAction);
      if (action) nested[pattern] = action;
    });
    if (Object.keys(nested).length) out[key] = nested;
  });
  return out;
}

function resolveReferenceItem(id, library) {
  return library.find((item) => item.id === id || item.name === id) || { id, name: id };
}

function referenceTitle(item, fallback) {
  return item?.title || item?.name || item?.id || fallback;
}

function referenceDetail(item, kind) {
  if (kind === "mcp") {
    const type = item?.config?.type || item?.type;
    return `${type === "remote" ? "远程 HTTP" : "本地 stdio"} · ${item?.source || "MCP 库"}`;
  }
  if (kind === "runtime") {
    return `${runtimeKindLabel(item?.runtime_kind)} · ${installStrategyLabel(item?.install_strategy)}`;
  }
  return `${item?.source || "Skill"} · ${item?.content ? "有内容" : "空内容"}`;
}

function referenceSearchText(item, kind) {
  if (kind === "runtime") return runtimeCatalogSearchableText(item);
  const config = item.config || {};
  return [
    item.id,
    item.name,
    item.title,
    item.description,
    item.source,
    ...(item.tags || []),
    kind === "skill" ? item.content : "",
    config.name,
    config.type,
    config.url,
    ...(config.command || []),
    ...Object.keys(config.environment || {}),
    ...Object.values(config.environment || {}),
  ]
    .filter(Boolean)
    .join(" ")
    .toLowerCase();
}

function runtimeKindLabel(value) {
  switch (String(value || "").trim()) {
    case "package_manager":
      return "包管理器";
    case "tool":
      return "工具";
    case "language":
    default:
      return "语言";
  }
}

function installStrategyLabel(value) {
  switch (String(value || "").trim()) {
    case "uv_python":
      return "uv Python";
    case "custom_command":
      return "自定义命令";
    case "archive":
    default:
      return "归档包";
  }
}

function normalizeCatalogForForm(item) {
  if (!item) return defaultCatalogItem();
  return {
    ...defaultCatalogItem(),
    ...item,
    config: {
      ...defaultCatalogItem().config,
      ...(item.config || {}),
      name: item.config?.name || item.name || "",
      type: item.config?.type || item.type || "local",
    },
    install: {
      ...defaultCatalogItem().install,
      ...(item.install || {}),
      source_url: item.install?.source_url || item.source_url || "",
    },
  };
}

const customSelects = new Map();

function initCustomSelects() {
  [
    el.categoryFilter,
    el.statusFilter,
    el.fieldType,
    el.runtimeFieldKind,
    el.runtimeFieldInstallStrategy,
    el.toolFieldDefaultAction,
  ].forEach(enhanceSelect);
  document.addEventListener("click", closeCustomSelects);
  document.addEventListener("keydown", (event) => {
    if (event.key === "Escape") closeCustomSelects();
  });
}

function enhanceSelect(select) {
  if (!select || customSelects.has(select)) return;

  select.classList.add("native-select");
  const root = document.createElement("div");
  root.className = "custom-select";

  const trigger = document.createElement("button");
  trigger.type = "button";
  trigger.className = "custom-select-trigger";
  trigger.setAttribute("aria-haspopup", "listbox");
  trigger.setAttribute("aria-expanded", "false");

  const value = document.createElement("span");
  value.className = "custom-select-value";

  const arrow = document.createElement("span");
  arrow.className = "custom-select-arrow";
  arrow.setAttribute("aria-hidden", "true");

  const menu = document.createElement("div");
  menu.className = "custom-select-menu hidden";
  menu.setAttribute("role", "listbox");

  trigger.append(value, arrow);
  root.append(trigger, menu);
  select.insertAdjacentElement("afterend", root);

  const custom = { root, trigger, value, menu, signature: "" };
  customSelects.set(select, custom);

  trigger.addEventListener("click", (event) => {
    event.stopPropagation();
    toggleCustomSelect(select);
  });
  trigger.addEventListener("keydown", (event) => handleCustomTriggerKeydown(event, select));
  menu.addEventListener("click", (event) => {
    const option = event.target.closest("[data-select-value]");
    if (!option) return;
    setCustomSelectValue(select, option.dataset.selectValue);
  });
  menu.addEventListener("keydown", (event) => handleCustomMenuKeydown(event, select));

  syncCustomSelect(select);
}

function syncCustomSelect(select) {
  const custom = customSelects.get(select);
  if (!custom) return;

  const options = Array.from(select.options);
  const signature = options.map((option) => `${option.value}:${option.textContent}`).join("|");
  if (signature !== custom.signature) {
    custom.menu.innerHTML = options
      .map((option) => customSelectOptionTemplate(option, option.value === select.value))
      .join("");
    custom.signature = signature;
  } else {
    custom.menu.querySelectorAll("[data-select-value]").forEach((option) => {
      const selected = option.dataset.selectValue === select.value;
      option.classList.toggle("selected", selected);
      option.setAttribute("aria-selected", selected ? "true" : "false");
    });
  }

  const selected = select.selectedOptions[0] || options[0];
  custom.value.textContent = selected ? selected.textContent : "请选择";
}

function customSelectOptionTemplate(option, selected) {
  return `
    <button
      class="custom-select-option ${selected ? "selected" : ""}"
      type="button"
      role="option"
      aria-selected="${selected ? "true" : "false"}"
      data-select-value="${escapeAttr(option.value)}"
    >${escapeHtml(option.textContent)}</button>
  `;
}

function toggleCustomSelect(select) {
  const custom = customSelects.get(select);
  if (!custom) return;
  const willOpen = custom.menu.classList.contains("hidden");
  closeCustomSelects(select);
  if (willOpen) openCustomSelect(select);
}

function openCustomSelect(select) {
  const custom = customSelects.get(select);
  if (!custom) return;
  syncCustomSelect(select);
  custom.root.classList.add("open");
  custom.menu.classList.remove("hidden");
  custom.trigger.setAttribute("aria-expanded", "true");
}

function closeCustomSelects(exceptSelect = null) {
  customSelects.forEach((custom, select) => {
    if (select === exceptSelect) return;
    custom.root.classList.remove("open");
    custom.menu.classList.add("hidden");
    custom.trigger.setAttribute("aria-expanded", "false");
  });
}

function setCustomSelectValue(select, value) {
  if (select.value !== value) {
    select.value = value;
    select.dispatchEvent(new Event("change", { bubbles: true }));
  }
  syncCustomSelect(select);
  closeCustomSelects();
}

function handleCustomTriggerKeydown(event, select) {
  if (!["ArrowDown", "ArrowUp", "Enter", " "].includes(event.key)) return;
  event.preventDefault();
  openCustomSelect(select);
  focusCustomOption(select, event.key === "ArrowUp" ? "last" : "selected");
}

function handleCustomMenuKeydown(event, select) {
  if (event.key === "Enter" || event.key === " ") {
    event.preventDefault();
    event.target.click();
    return;
  }
  if (event.key === "ArrowDown" || event.key === "ArrowUp") {
    event.preventDefault();
    focusSiblingOption(select, event.key === "ArrowDown" ? 1 : -1);
  }
}

function focusCustomOption(select, mode) {
  const custom = customSelects.get(select);
  if (!custom) return;
  const options = Array.from(custom.menu.querySelectorAll("[data-select-value]"));
  if (!options.length) return;
  const selectedIndex = options.findIndex((option) => option.dataset.selectValue === select.value);
  const targetIndex = mode === "last" ? options.length - 1 : Math.max(selectedIndex, 0);
  options[targetIndex].focus();
}

function focusSiblingOption(select, direction) {
  const custom = customSelects.get(select);
  if (!custom) return;
  const options = Array.from(custom.menu.querySelectorAll("[data-select-value]"));
  const current = options.indexOf(document.activeElement);
  const next = Math.min(options.length - 1, Math.max(0, current + direction));
  options[next]?.focus();
}

function normalizeBaseUrl(value) {
  return value.trim().replace(/\/+$/, "");
}

function defaultBaseUrl() {
  const origin = window.location.origin;
  if (window.location.pathname.startsWith("/codex-admin/")) return `${origin}/codex`;
  return origin;
}

function initialBaseUrl() {
  const stored = normalizeBaseUrl(localStorage.getItem(storageKeys.baseUrl) || "");
  const fallback = defaultBaseUrl();
  if (window.location.pathname.startsWith("/codex-admin/") && stored === window.location.origin) {
    return fallback;
  }
  return stored || fallback;
}

function searchableText(item) {
  const server = item.server || {};
  return [
    item.title,
    item.name,
    item.category,
    item.description,
    item.source,
    item.credential_url,
    server.name,
    server.type,
    server.url,
    ...(server.command || []),
    ...Object.keys(server.environment || {}),
    ...Object.values(server.environment || {}),
    ...Object.keys(server.headers || {}),
  ]
    .filter(Boolean)
    .join(" ")
    .toLowerCase();
}

function searchableTextForCurrentView(item) {
  switch (state.view) {
    case "semantic":
      return semanticSearchableText(item);
    case "skills":
      return skillSearchableText(item);
    case "mcpCatalog":
      return mcpCatalogSearchableText(item);
    case "environment":
      return environmentPresetSearchableText(item);
    case "runtime":
      return runtimeCatalogSearchableText(item);
    case "tools":
      return toolCatalogSearchableText(item);
    default:
      return searchableText(item);
  }
}

function semanticSearchableText(item) {
  return [
    item.id,
    item.name,
    item.description,
    item.icon,
    item.color,
    item.opencode_name,
    item.prompt,
    ...(item.skill_ids || []),
    ...(item.mcp_ids || []),
    ...(item.skills || []),
    ...(item.recommended_mcp_servers || []),
    ...(item.runtime_requirements || []).flatMap((runtime) => [runtime.runtime_id, runtime.version_constraint, runtime.purpose]),
    ...(item.skill_definitions || []).flatMap((skill) => [skill.name, skill.description, skill.content]),
  ]
    .filter(Boolean)
    .join(" ")
    .toLowerCase();
}

function skillSearchableText(item) {
  return [
    item.id,
    item.name,
    item.description,
    item.content,
    item.category,
    item.source,
    ...(item.tags || []),
  ]
    .filter(Boolean)
    .join(" ")
    .toLowerCase();
}

function mcpCatalogSearchableText(item) {
  const config = item.config || {};
  return [
    item.id,
    item.name,
    item.title,
    item.description,
    item.type,
    item.category,
    item.source,
    item.credential_url,
    ...(item.tags || []),
    config.name,
    config.type,
    config.url,
    ...(config.command || []),
    ...Object.keys(config.environment || {}),
    ...Object.values(config.environment || {}),
    ...Object.keys(config.headers || {}),
  ]
    .filter(Boolean)
    .join(" ")
    .toLowerCase();
}

function environmentPresetSearchableText(item) {
  const variables = item.variables || {};
  return [
    item.id,
    item.name,
    item.description,
    item.category,
    item.source,
    ...(item.tags || []),
    ...Object.keys(variables),
    ...Object.values(variables),
  ]
    .filter(Boolean)
    .join(" ")
    .toLowerCase();
}

function runtimeCatalogSearchableText(item) {
  return [
    item.id,
    item.name,
    item.description,
    item.runtime_kind,
    item.install_strategy,
    item.default_version_constraint,
    ...(item.executable_names || []),
    ...(item.tags || []),
    ...(item.versions || []).flatMap((version) => [version.id, version.version, version.channel, version.status]),
    ...(item.mirrors || []).flatMap((mirror) => [mirror.id, mirror.name, mirror.base_url, mirror.url_template, ...(mirror.tags || [])]),
    ...(item.artifacts || []).flatMap((artifact) => [artifact.id, artifact.version_id, artifact.platform, artifact.arch, artifact.filename, artifact.sha256]),
  ]
    .filter(Boolean)
    .join(" ")
    .toLowerCase();
}

function toolCatalogSearchableText(item) {
  return [
    item.id,
    item.name,
    item.description,
    item.category,
    item.permission_key,
    item.pattern,
    item.default_action,
    ...(item.tags || []),
  ]
    .filter(Boolean)
    .join(" ")
    .toLowerCase();
}

function commaList(value) {
  return value
    .split(/[,，]/)
    .map((item) => item.trim())
    .filter(Boolean);
}

function parseMapJson(value, label) {
  const text = value.trim();
  if (!text) return {};
  let decoded;
  try {
    decoded = JSON.parse(text);
  } catch {
    throw new Error(`${label} 格式不正确`);
  }
  if (!decoded || typeof decoded !== "object" || Array.isArray(decoded)) {
    throw new Error(`${label} 必须是 JSON 对象`);
  }
  return stringMap(decoded);
}

function parseObjectJson(value, label) {
  const text = value.trim();
  if (!text) return {};
  let decoded;
  try {
    decoded = JSON.parse(text);
  } catch {
    throw new Error(`${label} 格式不正确`);
  }
  if (!decoded || typeof decoded !== "object" || Array.isArray(decoded)) {
    throw new Error(`${label} 必须是 JSON 对象`);
  }
  return decoded;
}

function parseArrayJson(value, label) {
  const text = value.trim();
  if (!text) return [];
  let decoded;
  try {
    decoded = JSON.parse(text);
  } catch {
    throw new Error(`${label} 格式不正确`);
  }
  if (!Array.isArray(decoded)) {
    throw new Error(`${label} 必须是数组`);
  }
  return decoded;
}

function parseStringArrayJson(value, label) {
  return parseArrayJson(value, label).map((item) => String(item).trim()).filter(Boolean);
}

function parsePackageFilesJson(value) {
  return parseArrayJson(value, "配套文件 JSON")
    .map((item) => {
      if (!item || typeof item !== "object" || Array.isArray(item)) {
        throw new Error("配套文件 JSON 条目必须是对象");
      }
      return {
        path: stringValue(item.path),
        content: stringValue(item.content),
        executable: Boolean(item.executable),
      };
    })
    .filter((item) => item.path);
}

function normalizePermissionAction(value) {
  const action = String(value ?? "").trim().toLowerCase();
  return ["ask", "allow", "deny"].includes(action) ? action : "";
}

function actionLabel(value) {
  switch (normalizePermissionAction(value)) {
    case "allow":
      return "允许";
    case "deny":
      return "拒绝";
    case "ask":
      return "询问";
    default:
      return "未配置";
  }
}

function prettyJson(value) {
  return JSON.stringify(value ?? {}, null, 2);
}

function mapToJson(value) {
  const keys = Object.keys(value || {});
  if (!keys.length) return "";
  return JSON.stringify(value, null, 2);
}

function jsonArrayText(value) {
  if (!Array.isArray(value) || value.length === 0) return "";
  return JSON.stringify(value, null, 2);
}

function lines(value) {
  return value
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean);
}

function extractCommand(raw) {
  const args = Array.isArray(raw.args) ? raw.args.map(String).filter(Boolean) : [];
  if (Array.isArray(raw.command)) return [...raw.command.map(String).filter(Boolean), ...args];
  if (typeof raw.command === "string" && raw.command.trim()) return [raw.command.trim(), ...args];
  return [];
}

function normalizeType(value) {
  const type = stringValue(value).toLowerCase();
  if (["remote", "http", "https"].includes(type)) return "remote";
  if (["local", "stdio", "command"].includes(type)) return "local";
  return "";
}

function stringMap(value) {
  if (!value || typeof value !== "object" || Array.isArray(value)) return {};
  return Object.fromEntries(
    Object.entries(value)
      .map(([key, item]) => [String(key).trim(), item == null ? "" : String(item).trim()])
      .filter(([key, item]) => key && item),
  );
}

function stringValue(value) {
  return value == null ? "" : String(value).trim();
}

function formatTime(value) {
  if (!value) return "未知";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "未知";
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

function pad(value) {
  return String(value).padStart(2, "0");
}

function displayDescription(value) {
  const text = String(value || "").replace(/\s+/g, " ").trim();
  if (!text || text === ">" || text === ">-" || text === "|" || text === "|-") return "暂无描述";
  return text;
}

function pill(value, className = "") {
  const text = String(value || "").trim();
  if (!text) return "";
  const classes = ["pill", className].filter(Boolean).join(" ");
  return `<span class="${classes}" title="${escapeAttr(text)}">${escapeHtml(text)}</span>`;
}

function escapeHtml(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#039;");
}

function escapeAttr(value) {
  return escapeHtml(value).replaceAll("\n", " ");
}

bootstrap();
