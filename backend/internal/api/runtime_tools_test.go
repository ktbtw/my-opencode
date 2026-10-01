package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"relay-server/internal/auth"
	"relay-server/internal/broker"
	"relay-server/internal/model"
	"relay-server/internal/store"
)

func TestGetDeviceRuntimeStatusReturnsLauncherState(t *testing.T) {
	mem := store.NewMemory(nil)
	authManager := auth.NewManager(time.Hour)
	b := broker.New()
	operator := model.Operator{ID: 1, Username: "tester"}
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatal(err)
	}
	b.Add(nil, model.HelloPayload{
		MachineID:    "m_runtime",
		DeviceID:     "launcher:m_runtime",
		Kind:         "launcher",
		Capabilities: []string{launcherRuntimeCapability},
	}, operator.ID)

	var seenType, seenRuntimeID string
	api := newTestAPI(mem, b, authManager, testAPIOptions{
		launcherRequest: func(_ context.Context, _ int64, _ string, _, envType string, payload any) (model.DeviceLauncherResultPayload, error) {
			seenType = envType
			blob, _ := json.Marshal(payload)
			var decoded struct {
				RuntimeID string `json:"runtime_id"`
			}
			_ = json.Unmarshal(blob, &decoded)
			seenRuntimeID = decoded.RuntimeID
			return model.DeviceLauncherResultPayload{
				Success: true,
				Runtime: &model.DeviceRuntimeStatus{
					RuntimeID: "ffmpeg",
					Installed: true,
					Version:   "6.0",
				},
			}, nil
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/devices/m_runtime/runtimes/ffmpeg/status", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("machineID", "m_runtime")
	rctx.URLParams.Add("runtimeID", "ffmpeg")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()
	api.GetDeviceRuntimeStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if seenType != "device.launcher.runtime_status" {
		t.Fatalf("unexpected dispatch type: %s", seenType)
	}
	if seenRuntimeID != "ffmpeg" {
		t.Fatalf("unexpected runtime_id in payload: %q", seenRuntimeID)
	}
	if !strings.Contains(rr.Body.String(), `"installed":true`) {
		t.Fatalf("runtime status not returned: %s", rr.Body.String())
	}
}

func TestRuntimeCatalogSnapshotIncludesFFmpegAssets(t *testing.T) {
	mem := store.NewMemory(nil)
	api := newTestAPI(mem, broker.New(), auth.NewManager(time.Hour), testAPIOptions{})
	if err := api.ensureDefaultRuntimeCatalog(); err != nil {
		t.Fatal(err)
	}

	runtimes, err := api.runtimeCatalogSnapshotByIDs([]string{"ffmpeg"})
	if err != nil {
		t.Fatal(err)
	}
	if len(runtimes) != 1 || runtimes[0].ID != "ffmpeg" {
		t.Fatalf("expected only ffmpeg, got %+v", runtimes)
	}
	if runtimes[0].InstallStrategy != "archive" {
		t.Fatalf("ffmpeg 应使用 archive 安装策略，实际 %q", runtimes[0].InstallStrategy)
	}

	// 安装包、版本与镜像来自内嵌目录（线上由 MySQL 聚合，内存存储不 join）。
	platforms := map[string]bool{}
	for _, artifact := range builtinRuntimeArtifacts() {
		if artifact.RuntimeID != "ffmpeg" || !artifact.Enabled {
			continue
		}
		if strings.TrimSpace(artifact.SHA256) == "" || artifact.SizeBytes <= 0 {
			t.Fatalf("ffmpeg 安装包缺少校验信息: %+v", artifact)
		}
		platforms[artifact.Platform+"/"+artifact.Arch] = true
	}
	for _, want := range []string{"windows/amd64", "darwin/arm64", "darwin/amd64", "linux/amd64"} {
		if !platforms[want] {
			t.Fatalf("缺少平台安装包 %s（现有 %v）", want, platforms)
		}
	}

	var hasVersion, hasMirror bool
	for _, version := range builtinRuntimeVersions() {
		if version.RuntimeID == "ffmpeg" && version.ID == "ffmpeg-6.0" {
			hasVersion = true
		}
	}
	for _, mirror := range builtinRuntimeMirrors() {
		if mirror.RuntimeID == "ffmpeg" && strings.TrimSpace(mirror.BaseURL) != "" {
			hasMirror = true
		}
	}
	if !hasVersion || !hasMirror {
		t.Fatalf("ffmpeg 缺少版本或镜像配置: version=%v mirror=%v", hasVersion, hasMirror)
	}
}
