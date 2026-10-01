package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"relay-server/internal/model"
)

// GetDeviceRuntimeStatus 查询设备上单个受管运行时组件的安装状态。
// 用于「切到视频模型时检查设备有没有 FFmpeg」这类按需检测。
func (a *API) GetDeviceRuntimeStatus(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	runtimeID := strings.TrimSpace(chi.URLParam(r, "runtimeID"))
	if machineID == "" || runtimeID == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "缺少设备或运行时标识"})
		return
	}
	launcherID, err := a.deviceLauncherAgentWithCapability(operator.ID, machineID, launcherRuntimeCapability)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": launcherCapabilityMessage(err, "设备")})
		return
	}
	requestCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	result, err := a.requestDeviceLauncher(requestCtx, operator.ID, launcherID,
		fmt.Sprintf("req_runtime_status_%d", time.Now().UnixNano()),
		"device.launcher.runtime_status", model.DeviceRuntimeRequestPayload{RuntimeID: runtimeID})
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	status := model.DeviceRuntimeStatus{RuntimeID: runtimeID}
	if result.Runtime != nil {
		status = *result.Runtime
	}
	write(w, http.StatusOK, map[string]any{"runtime": status})
}

// InstallDeviceRuntime 请求设备安装单个运行时组件（例如 FFmpeg）。
// 返回可轮询的安装任务，进度事件与语义 Agent 预检共用同一套接口。
func (a *API) InstallDeviceRuntime(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	runtimeID := strings.TrimSpace(chi.URLParam(r, "runtimeID"))
	if machineID == "" || runtimeID == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "缺少设备或运行时标识"})
		return
	}
	launcherID, err := a.deviceLauncherAgentWithCapability(operator.ID, machineID, launcherRuntimeCapability)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": launcherCapabilityMessage(err, "设备")})
		return
	}
	runtimes, err := a.runtimeCatalogSnapshotByIDs([]string{runtimeID})
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if len(runtimes) == 0 {
		write(w, http.StatusBadRequest, map[string]string{"error": "未找到该运行时的安装包配置"})
		return
	}

	now := time.Now().UTC()
	job := model.RuntimeInstallJob{
		ID:              fmt.Sprintf("runtime_install_%d", time.Now().UnixNano()),
		OperatorID:      operator.ID,
		MachineID:       machineID,
		LauncherAgentID: launcherID,
		Status:          "pending",
		CurrentStep:     "runtime",
		ProgressPercent: 0,
		RequestedBy:     operator.Username,
		StartedAt:       now,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	stored, err := a.store.UpsertRuntimeInstallJob(job)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := a.store.ReplaceRuntimeInstallJobItems(stored.ID, runtimeOnlyInstallItems(stored.ID, runtimes)); err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	_, _ = a.store.AppendRuntimeInstallEvent(model.RuntimeInstallEvent{
		JobID:           stored.ID,
		ItemType:        "job",
		Phase:           "dispatch",
		Status:          "info",
		Message:         fmt.Sprintf("已创建 %s 安装任务", runtimes[0].Name),
		ProgressPercent: 1,
	})
	_, _ = a.store.AppendRuntimeInstallEvent(model.RuntimeInstallEvent{
		JobID:           stored.ID,
		ItemType:        "job",
		Phase:           "dispatch",
		Status:          "running",
		Message:         "安装任务正在下发，等待 launcher 开始执行",
		ProgressPercent: 3,
	})

	payload := model.RuntimePreflightStartPayload{
		JobID:           stored.ID,
		MachineID:       machineID,
		LauncherAgentID: launcherID,
		RuntimeOnly:     true,
		Runtimes:        runtimes,
	}
	if err := a.broker.DispatchForOperator(r.Context(), operator.ID, launcherID, model.Envelope{
		Type:      "device.launcher.runtime_install",
		RequestID: stored.ID,
		SentAt:    time.Now().UTC().Format(time.RFC3339),
		Payload:   payload,
	}); err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusAccepted, map[string]any{"job": stored, "job_id": stored.ID})
}

// runtimeCatalogSnapshotByIDs 按运行时 ID 取目录快照（含各自依赖关系），
// 供「单独安装某个运行时」使用。
func (a *API) runtimeCatalogSnapshotByIDs(ids []string) ([]model.RuntimeCatalogItem, error) {
	// 与 catalog 列表接口一致：先确保内置目录已写入，否则新实例上
	// 查不到 ffmpeg 这类后加的运行时，会直接报「未找到安装包配置」。
	if err := a.ensureDefaultRuntimeCatalog(); err != nil {
		return nil, err
	}
	all, err := a.store.ListRuntimeCatalog(false)
	if err != nil {
		return nil, err
	}
	byID := map[string]model.RuntimeCatalogItem{}
	for _, item := range all {
		byID[item.ID] = item
	}
	out := make([]model.RuntimeCatalogItem, 0, len(ids))
	seen := map[string]bool{}
	appendByID := func(id string) {
		item, ok := byID[id]
		if !ok || seen[id] {
			return
		}
		out = append(out, item)
		seen[id] = true
	}
	for _, id := range ids {
		appendByID(strings.TrimSpace(id))
	}
	// 依赖：npm_global_tool 需要 node，uv_python 需要 uv。
	for _, id := range ids {
		item, ok := byID[strings.TrimSpace(id)]
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(item.InstallStrategy)) {
		case "uv_python":
			appendByID("uv")
		case "npm_global_tool":
			appendByID("node")
		}
	}
	return out, nil
}

func runtimeOnlyInstallItems(jobID string, runtimes []model.RuntimeCatalogItem) []model.RuntimeInstallJobItem {
	now := time.Now().UTC()
	items := make([]model.RuntimeInstallJobItem, 0, len(runtimes))
	for _, runtime := range runtimes {
		id := strings.TrimSpace(runtime.ID)
		if id == "" {
			continue
		}
		items = append(items, model.RuntimeInstallJobItem{
			JobID:     jobID,
			ItemType:  "runtime",
			ItemID:    id,
			Name:      strings.TrimSpace(runtime.Name),
			Required:  true,
			Status:    "pending",
			UpdatedAt: now,
		})
	}
	return items
}
