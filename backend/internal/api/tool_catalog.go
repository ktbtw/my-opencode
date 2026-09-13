package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"relay-server/internal/model"
)

func defaultToolCatalogItems() []model.ToolCatalogItem {
	return []model.ToolCatalogItem{
		defaultTool("read", "读取文件", "读取工作区文件内容。", "基础", "read", "*", "allow", 10, "file", "basic"),
		defaultTool("glob", "文件匹配", "按 glob 模式查找文件。", "基础", "glob", "*", "allow", 20, "file", "basic"),
		defaultTool("grep", "文本搜索", "在文件中搜索文本或正则。", "基础", "grep", "*", "allow", 30, "search", "basic"),
		defaultTool("list", "目录列表", "列出目录和文件结构。", "基础", "list", "*", "allow", 40, "file", "basic"),
		defaultTool("edit", "编辑文件", "写入、修改或补丁编辑文件。", "写入", "edit", "*", "ask", 50, "write", "basic"),
		defaultTool("bash", "Shell 命令", "执行命令行操作，默认建议保持询问。", "命令", "bash", "*", "ask", 60, "shell", "basic"),
		defaultTool("task", "子任务 Agent", "调用子任务或专门 Agent 执行独立工作。", "协作", "task", "*", "ask", 65, "agent", "plan"),
		defaultTool("todowrite", "计划待办", "维护模型内部的任务清单。", "协作", "todowrite", "*", "allow", 70, "plan", "basic"),
		defaultTool("question", "用户确认", "向用户发起确认或选择问题。", "协作", "question", "*", "allow", 80, "question", "basic"),
		defaultTool("skill", "Skill 加载", "加载 Agent 可用 Skill 指令。", "扩展", "skill", "*", "allow", 90, "skill", "basic"),
		defaultTool("webfetch", "网页读取", "读取指定 URL 内容。", "网络", "webfetch", "*", "allow", 100, "web", "network"),
		defaultTool("websearch", "网络搜索", "执行网络搜索或检索。", "网络", "websearch", "*", "allow", 110, "web", "network"),
		defaultTool("repo-clone", "仓库克隆", "克隆远程仓库到工作区。", "仓库", "repo_clone", "*", "ask", 120, "repo", "git"),
		defaultTool("repo-overview", "仓库概览", "读取仓库结构和摘要信息。", "仓库", "repo_overview", "*", "allow", 130, "repo", "git"),
		defaultTool("lsp", "LSP 工具", "调用语言服务相关能力。", "开发", "lsp", "*", "allow", 140, "lsp", "dev"),
		defaultTool("external-directory", "外部目录访问", "访问工作区外部目录，建议执行前确认边界。", "安全", "external_directory", "*", "ask", 145, "filesystem", "safety"),
		defaultTool("doom-loop", "循环保护", "检测和限制异常循环行为。", "安全", "doom_loop", "*", "allow", 150, "safety"),
		defaultTool("bash-file", "file 命令", "识别二进制、APK、DEX、SO 等文件类型。", "逆向命令", "bash", "file *", "allow", 200, "reverse", "shell"),
		defaultTool("bash-strings", "strings 命令", "提取二进制或样本中的可见字符串。", "逆向命令", "bash", "strings *", "allow", 210, "reverse", "shell"),
		defaultTool("bash-readelf", "readelf 命令", "读取 ELF 头、节区、符号和动态链接信息。", "逆向命令", "bash", "readelf *", "allow", 220, "reverse", "shell"),
		defaultTool("bash-objdump", "objdump 命令", "反汇编或查看目标文件信息。", "逆向命令", "bash", "objdump *", "allow", 230, "reverse", "shell"),
		defaultTool("bash-otool", "otool 命令", "分析 macOS/iOS Mach-O 文件。", "逆向命令", "bash", "otool *", "allow", 240, "reverse", "shell"),
		defaultTool("bash-nm", "nm 命令", "查看符号表和导出符号。", "逆向命令", "bash", "nm *", "allow", 250, "reverse", "shell"),
		defaultTool("bash-jadx", "JADX", "反编译 APK/DEX，建议执行前确认目标授权。", "逆向命令", "bash", "jadx *", "ask", 260, "reverse", "android"),
		defaultTool("bash-apktool", "apktool", "解包和重建 APK 资源或 smali。", "逆向命令", "bash", "apktool *", "ask", 270, "reverse", "android"),
		defaultTool("bash-frida", "Frida", "动态插桩和运行时观察，存在动态分析风险。", "逆向命令", "bash", "frida *", "ask", 280, "reverse", "dynamic"),
		defaultTool("bash-adb", "ADB", "操作 Android 设备或模拟器。", "逆向命令", "bash", "adb *", "ask", 290, "reverse", "android"),
		defaultTool("bash-ghidra", "Ghidra", "启动或调用 Ghidra 分析。", "逆向命令", "bash", "ghidra *", "ask", 300, "reverse", "binary"),
		defaultTool("bash-r2", "radare2/r2", "使用 radare2 分析二进制。", "逆向命令", "bash", "r2 *", "ask", 310, "reverse", "binary"),
		defaultTool("bash-radare2", "radare2", "使用 radare2 命令分析二进制。", "逆向命令", "bash", "radare2 *", "ask", 320, "reverse", "binary"),
		defaultTool("bash-python", "python", "执行 Python 脚本或补环境实验。", "脚本", "bash", "python *", "ask", 330, "script", "python"),
		defaultTool("bash-python3", "python3", "执行 Python3 脚本或补环境实验。", "脚本", "bash", "python3 *", "ask", 340, "script", "python"),
	}
}

func defaultTool(id, name, description, category, permissionKey, pattern, action string, sortOrder int, tags ...string) model.ToolCatalogItem {
	return model.ToolCatalogItem{
		ID:            id,
		Name:          name,
		Description:   description,
		Category:      category,
		PermissionKey: permissionKey,
		Pattern:       pattern,
		DefaultAction: action,
		Enabled:       true,
		Tags:          tags,
		SortOrder:     sortOrder,
		BuiltIn:       true,
	}
}

func (a *API) ListToolCatalog(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.currentOperator(r); !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if err := a.ensureDefaultToolCatalog(); err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	items, err := a.store.ListToolCatalog(false)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, map[string]any{"items": items})
}

func (a *API) ListAdminToolCatalog(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	if err := a.ensureDefaultToolCatalog(); err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	items, err := a.store.ListToolCatalog(true)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, map[string]any{"items": items})
}

func (a *API) UpsertToolCatalogItem(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	var req model.ToolCatalogItem
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if id := strings.TrimSpace(chi.URLParam(r, "id")); id != "" {
		req.ID = id
	}
	item, err := a.upsertToolCatalogItem(req)
	if err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, item)
}

func (a *API) DeleteToolCatalogItem(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "工具 ID 不能为空"})
		return
	}
	if err := a.store.DeleteToolCatalogItem(id); err != nil {
		write(w, statusForStoreError(err), map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, map[string]any{"success": true})
}

func (a *API) ensureDefaultToolCatalog() error {
	initialized, err := a.defaultSeedInitialized(defaultSeedToolCatalogKey)
	if err != nil {
		return err
	}
	if initialized {
		return nil
	}
	items, err := a.store.ListToolCatalog(true)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		for _, item := range defaultToolCatalogItems() {
			if _, err := a.upsertToolCatalogItem(item); err != nil {
				return err
			}
		}
	}
	return a.markDefaultSeedInitialized(defaultSeedToolCatalogKey)
}

func (a *API) upsertToolCatalogItem(item model.ToolCatalogItem) (*model.ToolCatalogItem, error) {
	item.ID = strings.TrimSpace(item.ID)
	if item.ID == "" {
		return nil, errors.New("工具 ID 不能为空")
	}
	item.Name = strings.TrimSpace(item.Name)
	if item.Name == "" {
		item.Name = item.ID
	}
	item.Category = strings.TrimSpace(item.Category)
	if item.Category == "" {
		item.Category = "通用"
	}
	item.PermissionKey = strings.TrimSpace(item.PermissionKey)
	if item.PermissionKey == "" {
		return nil, errors.New("权限键不能为空")
	}
	item.Pattern = strings.TrimSpace(item.Pattern)
	if item.Pattern == "" {
		item.Pattern = "*"
	}
	item.DefaultAction = normalizeToolPermissionAction(item.DefaultAction)
	item.Description = strings.TrimSpace(item.Description)
	item.Tags = cleanStringSlice(item.Tags)
	if item.SortOrder == 0 {
		item.SortOrder = 100
	}
	return a.store.UpsertToolCatalogItem(item)
}

func normalizeToolPermissionAction(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "allow", "ask", "deny":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "ask"
	}
}
