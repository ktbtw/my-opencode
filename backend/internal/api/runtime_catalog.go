package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"relay-server/internal/model"
)

func (a *API) ListRuntimeCatalog(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.currentOperator(r); !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if err := a.ensureDefaultRuntimeCatalog(); err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	items, err := a.store.ListRuntimeCatalog(false)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, map[string]any{"items": items})
}

func (a *API) ListAdminRuntimeCatalog(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	if err := a.ensureDefaultRuntimeCatalog(); err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	items, err := a.store.ListRuntimeCatalog(true)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, map[string]any{"items": items})
}

func (a *API) UpsertRuntimeCatalogItem(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	var req model.RuntimeCatalogItem
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if id := strings.TrimSpace(chi.URLParam(r, "id")); id != "" {
		req.ID = id
	}
	item, err := a.upsertRuntimeCatalogItem(req)
	if err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, item)
}

func (a *API) DeleteRuntimeCatalogItem(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "runtime id 不能为空"})
		return
	}
	if err := a.store.DeleteRuntimeCatalogItem(id); err != nil {
		write(w, statusForStoreError(err), map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, map[string]any{"success": true})
}

func (a *API) UpsertRuntimeVersion(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	var req model.RuntimeVersion
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if id := strings.TrimSpace(chi.URLParam(r, "id")); id != "" {
		req.ID = id
	}
	item, err := a.upsertRuntimeVersion(req)
	if err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, item)
}

func (a *API) DeleteRuntimeVersion(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "version id 不能为空"})
		return
	}
	if err := a.store.DeleteRuntimeVersion(id); err != nil {
		write(w, statusForStoreError(err), map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, map[string]any{"success": true})
}

func (a *API) UpsertRuntimeArtifact(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	var req model.RuntimeArtifact
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if id := strings.TrimSpace(chi.URLParam(r, "id")); id != "" {
		req.ID = id
	}
	item, err := a.upsertRuntimeArtifact(req)
	if err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, item)
}

func (a *API) DeleteRuntimeArtifact(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "artifact id 不能为空"})
		return
	}
	if err := a.store.DeleteRuntimeArtifact(id); err != nil {
		write(w, statusForStoreError(err), map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, map[string]any{"success": true})
}

func (a *API) UpsertRuntimeMirror(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	var req model.RuntimeMirror
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if id := strings.TrimSpace(chi.URLParam(r, "id")); id != "" {
		req.ID = id
	}
	item, err := a.upsertRuntimeMirror(req)
	if err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, item)
}

func (a *API) DeleteRuntimeMirror(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "mirror id 不能为空"})
		return
	}
	if err := a.store.DeleteRuntimeMirror(id); err != nil {
		write(w, statusForStoreError(err), map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, map[string]any{"success": true})
}

func (a *API) upsertRuntimeCatalogItem(item model.RuntimeCatalogItem) (*model.RuntimeCatalogItem, error) {
	item.ID = strings.TrimSpace(item.ID)
	item.Name = strings.TrimSpace(item.Name)
	item.Description = strings.TrimSpace(item.Description)
	item.RuntimeKind = strings.TrimSpace(item.RuntimeKind)
	item.DefaultVersionConstraint = strings.TrimSpace(item.DefaultVersionConstraint)
	item.ExecutableNames = cleanStringSlice(item.ExecutableNames)
	item.EnvTemplate = cleanStringMapForAPI(item.EnvTemplate)
	item.InstallStrategy = strings.TrimSpace(item.InstallStrategy)
	item.Tags = cleanStringSlice(item.Tags)
	if item.ID == "" {
		item.ID = item.Name
	}
	if item.ID == "" {
		return nil, errors.New("运行时 ID 不能为空")
	}
	if item.Name == "" {
		item.Name = item.ID
	}
	if item.RuntimeKind == "" {
		item.RuntimeKind = "language"
	}
	if item.InstallStrategy == "" {
		item.InstallStrategy = "archive"
	}
	if item.SortOrder == 0 {
		item.SortOrder = 100
	}
	return a.store.UpsertRuntimeCatalogItem(item)
}

func (a *API) upsertRuntimeVersion(item model.RuntimeVersion) (*model.RuntimeVersion, error) {
	item.ID = strings.TrimSpace(item.ID)
	item.RuntimeID = strings.TrimSpace(item.RuntimeID)
	item.Version = strings.TrimSpace(item.Version)
	item.Channel = strings.TrimSpace(item.Channel)
	item.Status = strings.TrimSpace(item.Status)
	if item.RuntimeID == "" {
		return nil, errors.New("运行时不能为空")
	}
	if item.Version == "" {
		return nil, errors.New("版本不能为空")
	}
	if item.ID == "" {
		item.ID = item.RuntimeID + "-" + sanitizeRuntimeID(item.Version)
	}
	if item.Status == "" {
		item.Status = "available"
	}
	if item.VersionOrder == 0 {
		item.VersionOrder = 100
	}
	if item.SortOrder == 0 {
		item.SortOrder = 100
	}
	return a.store.UpsertRuntimeVersion(item)
}

func (a *API) upsertRuntimeArtifact(item model.RuntimeArtifact) (*model.RuntimeArtifact, error) {
	item.ID = strings.TrimSpace(item.ID)
	item.RuntimeID = strings.TrimSpace(item.RuntimeID)
	item.VersionID = strings.TrimSpace(item.VersionID)
	item.Platform = strings.ToLower(strings.TrimSpace(item.Platform))
	item.Arch = strings.ToLower(strings.TrimSpace(item.Arch))
	item.PackageKind = strings.TrimSpace(item.PackageKind)
	item.Filename = strings.TrimSpace(item.Filename)
	item.SHA256 = strings.ToLower(strings.TrimSpace(item.SHA256))
	item.StorageKey = strings.TrimSpace(item.StorageKey)
	item.DownloadPath = strings.TrimSpace(item.DownloadPath)
	item.ExtractRoot = strings.TrimSpace(item.ExtractRoot)
	item.BinPaths = cleanStringSlice(item.BinPaths)
	item.EnvPatch = cleanStringMapForAPI(item.EnvPatch)
	if item.RuntimeID == "" || item.VersionID == "" {
		return nil, errors.New("运行时和版本不能为空")
	}
	if item.Platform == "" || item.Arch == "" {
		return nil, errors.New("平台和架构不能为空")
	}
	if item.Filename == "" {
		return nil, errors.New("文件名不能为空")
	}
	if item.ID == "" {
		item.ID = strings.Join([]string{item.RuntimeID, item.VersionID, item.Platform, item.Arch, sanitizeRuntimeID(item.Filename)}, "-")
	}
	if item.PackageKind == "" {
		item.PackageKind = "archive"
	}
	if item.Priority == 0 {
		item.Priority = 100
	}
	return a.store.UpsertRuntimeArtifact(item)
}

func (a *API) upsertRuntimeMirror(item model.RuntimeMirror) (*model.RuntimeMirror, error) {
	item.ID = strings.TrimSpace(item.ID)
	item.RuntimeID = strings.TrimSpace(item.RuntimeID)
	item.VersionID = strings.TrimSpace(item.VersionID)
	item.Name = strings.TrimSpace(item.Name)
	item.Description = strings.TrimSpace(item.Description)
	item.BaseURL = strings.TrimSpace(item.BaseURL)
	item.URLTemplate = strings.TrimSpace(item.URLTemplate)
	item.ChecksumURLTemplate = strings.TrimSpace(item.ChecksumURLTemplate)
	item.Platform = strings.ToLower(strings.TrimSpace(item.Platform))
	item.Arch = strings.ToLower(strings.TrimSpace(item.Arch))
	item.Headers = cleanStringMapForAPI(item.Headers)
	item.Tags = cleanStringSlice(item.Tags)
	if item.ID == "" {
		item.ID = sanitizeRuntimeID(firstNonEmpty(item.RuntimeID, "runtime")) + "-" + sanitizeRuntimeID(item.Name)
	}
	if item.ID == "" || item.Name == "" {
		return nil, errors.New("镜像 ID 和名称不能为空")
	}
	if item.BaseURL == "" && item.URLTemplate == "" {
		return nil, errors.New("镜像源地址不能为空")
	}
	if item.Priority == 0 {
		item.Priority = 100
	}
	if item.TimeoutSeconds == 0 {
		item.TimeoutSeconds = 30
	}
	return a.store.UpsertRuntimeMirror(item)
}

func sanitizeRuntimeID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	var builder strings.Builder
	for _, ch := range value {
		switch {
		case ch >= 'a' && ch <= 'z':
			builder.WriteRune(ch)
		case ch >= '0' && ch <= '9':
			builder.WriteRune(ch)
		case ch == '.', ch == '-', ch == '_':
			builder.WriteRune('-')
		default:
			builder.WriteRune('-')
		}
	}
	return strings.Trim(builder.String(), "-")
}

func cleanStringMapForAPI(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = strings.TrimSpace(value)
	}
	return out
}
