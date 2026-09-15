package app

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"launcher/internal/config"
	"launcher/internal/model"
)

func discoverDirectories(cfg config.Config) ([]model.Directory, error) {
	return discoverDirectoryEntries(cfg, false)
}

func discoverDirectoryEntries(cfg config.Config, allowAll bool) ([]model.Directory, error) {
	seen := map[string]struct{}{}
	result := []model.Directory{}
	add := func(path string, kind string) {
		path = filepath.Clean(path)
		if _, ok := seen[path]; ok {
			return
		}
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			return
		}
		seen[path] = struct{}{}
		result = append(result, model.Directory{Path: path, Name: directoryName(path), Kind: kind, IsDir: true})
	}
	if allowAll {
		for _, root := range systemRootCandidates(runtime.GOOS, os.Stat) {
			add(root, "root")
		}
	}
	if cfg.Discovery.IncludeCWD {
		if cwd, err := os.Getwd(); err == nil {
			add(cwd, "cwd")
		}
	}
	maxDepth := cfg.Discovery.MaxDepth
	if maxDepth < 0 {
		maxDepth = 0
	}
	for _, root := range cfg.Discovery.AllowedRoots {
		root = os.ExpandEnv(root)
		if _, err := os.Stat(root); err != nil {
			continue
		}
		add(root, "root")
		walkDirectories(root, root, 0, maxDepth, add)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result, nil
}

func systemRootCandidates(goos string, stat func(string) (os.FileInfo, error)) []string {
	if goos == "windows" {
		roots := []string{}
		for letter := 'A'; letter <= 'Z'; letter++ {
			root := string(letter) + `:\`
			info, err := stat(root)
			if err == nil && info.IsDir() {
				roots = append(roots, root)
			}
		}
		return roots
	}
	info, err := stat(string(os.PathSeparator))
	if err == nil && info.IsDir() {
		return []string{string(os.PathSeparator)}
	}
	return nil
}

func directoryName(path string) string {
	path = filepath.Clean(path)
	if drive := windowsDriveName(path); drive != "" {
		return drive
	}
	name := filepath.Base(path)
	if name == "." || name == string(os.PathSeparator) {
		return path
	}
	return name
}

func windowsDriveName(path string) string {
	if len(path) < 2 || path[1] != ':' {
		return ""
	}
	letter := path[0]
	if !((letter >= 'A' && letter <= 'Z') || (letter >= 'a' && letter <= 'z')) {
		return ""
	}
	rest := ""
	if len(path) > 2 {
		rest = strings.Trim(path[2:], `\/`)
	}
	if rest != "" {
		return ""
	}
	return strings.ToUpper(path[:2])
}

func walkDirectories(root string, current string, depth int, maxDepth int, add func(string, string)) {
	if depth >= maxDepth {
		return
	}
	entries, err := os.ReadDir(current)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		next := filepath.Join(current, entry.Name())
		add(next, classifyDirectory(root, next))
		walkDirectories(root, next, depth+1, maxDepth, add)
	}
}

func classifyDirectory(root string, path string) string {
	if filepath.Clean(path) == filepath.Clean(root) {
		return "root"
	}
	return "workspace"
}

func browseDirectory(cfg config.Config, currentPath string, allowAll bool) (model.DirectoryListResult, error) {
	path := strings.TrimSpace(currentPath)
	if path == "" {
		roots, err := discoverDirectoryEntries(cfg, allowAll)
		if err != nil {
			return model.DirectoryListResult{}, err
		}
		return model.DirectoryListResult{CurrentPath: "", Entries: roots}, nil
	}
	path = filepath.Clean(path)
	if !allowedPath(cfg, path, allowAll) {
		return model.DirectoryListResult{}, errors.New("目录不在允许范围内")
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return model.DirectoryListResult{}, err
	}
	items := make([]model.Directory, 0, len(entries))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		items = append(items, model.Directory{
			Path:  filepath.Join(path, entry.Name()),
			Name:  entry.Name(),
			Kind:  kind(entry.IsDir()),
			IsDir: entry.IsDir(),
			Size:  fileSize(entry),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].IsDir != items[j].IsDir {
			return items[i].IsDir
		}
		return items[i].Name < items[j].Name
	})
	parent := filepath.Dir(path)
	if parent == path || !allowedPath(cfg, parent, allowAll) {
		parent = ""
	}
	return model.DirectoryListResult{CurrentPath: path, ParentPath: parent, Entries: items}, nil
}

func allowedPath(cfg config.Config, target string, allowAll bool) bool {
	if allowAll {
		return true
	}
	target = filepath.Clean(target)
	for _, root := range cfg.Discovery.AllowedRoots {
		root = filepath.Clean(os.ExpandEnv(root))
		if target == root || strings.HasPrefix(target, root+string(os.PathSeparator)) {
			return true
		}
	}
	if cfg.Discovery.IncludeCWD {
		if cwd, err := os.Getwd(); err == nil {
			cwd = filepath.Clean(cwd)
			if target == cwd || strings.HasPrefix(target, cwd+string(os.PathSeparator)) {
				return true
			}
		}
	}
	return false
}

func directoryMutationPath(cfg config.Config, rawPath string, allowAll bool, mustExist bool) (string, error) {
	path := strings.TrimSpace(rawPath)
	if path == "" {
		return "", errors.New("文件路径不能为空")
	}
	full, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", errors.New("文件路径不合法")
	}
	if !allowedPath(cfg, full, allowAll) {
		return "", errors.New("目录不在允许范围内")
	}
	parent := filepath.Dir(full)
	resolvedParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return "", friendlyPathError(parent, err)
	}
	if !allowedResolvedPath(cfg, resolvedParent, allowAll) {
		return "", errors.New("路径不允许通过符号链接越过允许目录")
	}
	if mustExist {
		resolved, err := filepath.EvalSymlinks(full)
		if err != nil {
			return "", friendlyPathError(full, err)
		}
		if !allowedResolvedPath(cfg, resolved, allowAll) {
			return "", errors.New("路径不允许通过符号链接越过允许目录")
		}
	}
	return full, nil
}

func allowedResolvedPath(cfg config.Config, target string, allowAll bool) bool {
	if allowAll {
		return true
	}
	target = filepath.Clean(target)
	for _, rawRoot := range cfg.Discovery.AllowedRoots {
		root, err := filepath.EvalSymlinks(filepath.Clean(os.ExpandEnv(rawRoot)))
		if err == nil && pathWithin(filepath.Clean(root), target) {
			return true
		}
	}
	if cfg.Discovery.IncludeCWD {
		if cwd, err := os.Getwd(); err == nil {
			if resolved, err := filepath.EvalSymlinks(cwd); err == nil && pathWithin(filepath.Clean(resolved), target) {
				return true
			}
		}
	}
	return false
}

func pathWithin(root string, target string) bool {
	return target == root || strings.HasPrefix(target, root+string(os.PathSeparator))
}

func validateDirectoryEntryPath(path string) error {
	name := filepath.Base(path)
	if name == "." || name == ".." || strings.HasPrefix(name, ".") {
		return errors.New("名称不能为空、隐藏或使用特殊目录名")
	}
	if strings.ContainsRune(name, '\x00') {
		return errors.New("名称不合法")
	}
	if runtime.GOOS == "windows" {
		trimmed := strings.TrimRight(name, ". ")
		if trimmed != name || trimmed == "" {
			return errors.New("Windows 文件名不能以空格或句点结尾")
		}
		base := strings.ToUpper(strings.SplitN(trimmed, ".", 2)[0])
		reserved := map[string]bool{"CON": true, "PRN": true, "AUX": true, "NUL": true}
		for i := 1; i <= 9; i++ {
			reserved["COM"+strconv.Itoa(i)] = true
			reserved["LPT"+strconv.Itoa(i)] = true
		}
		if reserved[base] {
			return errors.New("名称是 Windows 保留名称")
		}
	}
	return nil
}

func isDirectoryBrowseRoot(cfg config.Config, path string, allowAll bool) bool {
	path = filepath.Clean(path)
	if filepath.Dir(path) == path {
		return true
	}
	roots, err := discoverDirectoryEntries(cfg, allowAll)
	if err != nil {
		return false
	}
	for _, root := range roots {
		if filepath.Clean(root.Path) == path {
			return true
		}
	}
	return false
}

func directoryUploadTempPath(target string, uploadID string) (string, error) {
	uploadID = strings.TrimSpace(uploadID)
	if uploadID == "" {
		return "", errors.New("upload_id 不能为空")
	}
	if len(uploadID) > 96 {
		return "", errors.New("upload_id 过长")
	}
	for _, ch := range uploadID {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_' {
			continue
		}
		return "", errors.New("upload_id 不合法")
	}
	targetHash := sha256Hex([]byte(filepath.Clean(target)))[:16]
	sessionName := uploadID + "-" + targetHash
	return filepath.Join(filepath.Dir(target), ".chat-codex-uploads", sessionName, "payload.bin"), nil
}

func requireSHA256(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len(value) != sha256.Size*2 {
		return "", errors.New("必须提供有效的 SHA-256 校验值")
	}
	if _, err := hex.DecodeString(value); err != nil {
		return "", errors.New("必须提供有效的 SHA-256 校验值")
	}
	return value, nil
}

func publishDirectoryUpload(tempPath string, target string) error {
	if err := os.Link(tempPath, target); err != nil {
		if os.IsExist(err) {
			return errors.New("目标名称已存在")
		}
		return err
	}
	// The destination is already fully published. A failed temp unlink only
	// leaves an extra hard link for a later cleanup and does not corrupt it.
	_ = os.Remove(tempPath)
	return nil
}

func directoryFileResult(path string, info os.FileInfo) model.ProjectFile {
	return model.ProjectFile{
		Path:  path,
		Name:  filepath.Base(path),
		Kind:  kind(info.IsDir()),
		IsDir: info.IsDir(),
		Size:  info.Size(),
	}
}

func (s *service) CreateDirectoryUpload(req model.DirectoryFileRequest) (model.ProjectFile, error) {
	s.directoryFilesMu.Lock()
	defer s.directoryFilesMu.Unlock()
	full, err := directoryMutationPath(s.cfg, req.Path, req.AllowAll, false)
	if err != nil {
		return model.ProjectFile{}, err
	}
	if err := validateDirectoryEntryPath(full); err != nil {
		return model.ProjectFile{}, err
	}
	if req.Size < 0 || req.TotalChunks <= 0 {
		return model.ProjectFile{}, errors.New("上传文件参数不合法")
	}
	if _, err := os.Lstat(full); err == nil {
		return model.ProjectFile{}, errors.New("目标名称已存在")
	} else if !os.IsNotExist(err) {
		return model.ProjectFile{}, friendlyPathError(full, err)
	}
	tempPath, err := directoryUploadTempPath(full, req.UploadID)
	if err != nil {
		return model.ProjectFile{}, err
	}
	if err := prepareUploadSession(tempPath, req.Size, req.TotalChunks, req.Resume); err != nil {
		return model.ProjectFile{}, err
	}
	return model.ProjectFile{Path: full, Name: filepath.Base(full), Kind: "文件", Size: req.Size}, nil
}

func (s *service) WriteDirectoryUploadChunk(req model.DirectoryFileRequest) (model.ProjectFile, error) {
	s.directoryFilesMu.Lock()
	defer s.directoryFilesMu.Unlock()
	full, err := directoryMutationPath(s.cfg, req.Path, req.AllowAll, false)
	if err != nil {
		return model.ProjectFile{}, err
	}
	if req.Offset < 0 || req.ChunkIndex < 0 {
		return model.ProjectFile{}, errors.New("分块位置不合法")
	}
	if encoding := strings.TrimSpace(req.Encoding); encoding != "" && encoding != "base64" {
		return model.ProjectFile{}, errors.New("仅支持 base64 编码")
	}
	data, err := base64.StdEncoding.DecodeString(req.Content)
	if err != nil {
		return model.ProjectFile{}, errors.New("文件分块不是有效 base64")
	}
	expectedSHA256, err := requireSHA256(req.SHA256)
	if err != nil {
		return model.ProjectFile{}, err
	}
	if !strings.EqualFold(expectedSHA256, sha256Hex(data)) {
		return model.ProjectFile{}, errors.New("文件分块校验失败")
	}
	tempPath, err := directoryUploadTempPath(full, req.UploadID)
	if err != nil {
		return model.ProjectFile{}, err
	}
	info, err := os.Stat(tempPath)
	if err != nil {
		return model.ProjectFile{}, friendlyPathError(tempPath, err)
	}
	if req.Offset+int64(len(data)) > info.Size() {
		return model.ProjectFile{}, errors.New("文件分块超过声明大小")
	}
	file, err := os.OpenFile(tempPath, os.O_WRONLY, 0o600)
	if err != nil {
		return model.ProjectFile{}, err
	}
	if _, err := file.WriteAt(data, req.Offset); err != nil {
		_ = file.Close()
		return model.ProjectFile{}, err
	}
	if err := file.Close(); err != nil {
		return model.ProjectFile{}, err
	}
	if err := recordUploadChunk(tempPath, info.Size(), req.TotalChunks, req.ChunkIndex); err != nil {
		return model.ProjectFile{}, err
	}
	return model.ProjectFile{Path: full, Name: filepath.Base(full), Kind: "文件", Size: int64(len(data))}, nil
}

func (s *service) CompleteDirectoryUpload(req model.DirectoryFileRequest) (model.ProjectFile, error) {
	s.directoryFilesMu.Lock()
	defer s.directoryFilesMu.Unlock()
	full, err := directoryMutationPath(s.cfg, req.Path, req.AllowAll, false)
	if err != nil {
		return model.ProjectFile{}, err
	}
	if _, err := os.Lstat(full); err == nil {
		return model.ProjectFile{}, errors.New("目标名称已存在")
	} else if !os.IsNotExist(err) {
		return model.ProjectFile{}, friendlyPathError(full, err)
	}
	tempPath, err := directoryUploadTempPath(full, req.UploadID)
	if err != nil {
		return model.ProjectFile{}, err
	}
	info, err := os.Stat(tempPath)
	if err != nil {
		return model.ProjectFile{}, friendlyPathError(tempPath, err)
	}
	if req.Size < 0 || info.Size() != req.Size {
		return model.ProjectFile{}, errors.New("文件大小校验失败")
	}
	expectedSHA256, err := requireSHA256(req.SHA256)
	if err != nil {
		return model.ProjectFile{}, err
	}
	sum, err := fileSHA256Hex(tempPath)
	if err != nil {
		return model.ProjectFile{}, err
	}
	if !strings.EqualFold(expectedSHA256, sum) {
		return model.ProjectFile{}, errors.New("文件整体校验失败")
	}
	if err := publishDirectoryUpload(tempPath, full); err != nil {
		return model.ProjectFile{}, err
	}
	removeUploadManifest(tempPath)
	_ = os.Remove(filepath.Dir(tempPath))
	_ = os.Remove(filepath.Dir(filepath.Dir(tempPath)))
	finalInfo, err := os.Stat(full)
	if err != nil {
		return model.ProjectFile{}, friendlyPathError(full, err)
	}
	return directoryFileResult(full, finalInfo), nil
}

func (s *service) CreateDirectoryFile(req model.DirectoryFileRequest) (model.ProjectFile, error) {
	s.directoryFilesMu.Lock()
	defer s.directoryFilesMu.Unlock()
	full, err := directoryMutationPath(s.cfg, req.Path, req.AllowAll, false)
	if err != nil {
		return model.ProjectFile{}, err
	}
	if err := validateDirectoryEntryPath(full); err != nil {
		return model.ProjectFile{}, err
	}
	file, err := os.OpenFile(full, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return model.ProjectFile{}, errors.New("目标名称已存在")
		}
		return model.ProjectFile{}, friendlyPathError(full, err)
	}
	if err := file.Close(); err != nil {
		return model.ProjectFile{}, err
	}
	info, err := os.Stat(full)
	if err != nil {
		return model.ProjectFile{}, friendlyPathError(full, err)
	}
	return directoryFileResult(full, info), nil
}

func (s *service) CreateDirectoryFolder(req model.DirectoryFileRequest) (model.ProjectFile, error) {
	s.directoryFilesMu.Lock()
	defer s.directoryFilesMu.Unlock()
	full, err := directoryMutationPath(s.cfg, req.Path, req.AllowAll, false)
	if err != nil {
		return model.ProjectFile{}, err
	}
	if err := validateDirectoryEntryPath(full); err != nil {
		return model.ProjectFile{}, err
	}
	if err := os.Mkdir(full, 0o755); err != nil {
		if os.IsExist(err) {
			return model.ProjectFile{}, errors.New("目标名称已存在")
		}
		return model.ProjectFile{}, friendlyPathError(full, err)
	}
	info, err := os.Stat(full)
	if err != nil {
		return model.ProjectFile{}, friendlyPathError(full, err)
	}
	return directoryFileResult(full, info), nil
}

func (s *service) DeleteDirectoryFile(req model.DirectoryFileRequest) (model.ProjectFile, error) {
	s.directoryFilesMu.Lock()
	defer s.directoryFilesMu.Unlock()
	full, err := directoryMutationPath(s.cfg, req.Path, req.AllowAll, true)
	if err != nil {
		return model.ProjectFile{}, err
	}
	if isDirectoryBrowseRoot(s.cfg, full, req.AllowAll) {
		return model.ProjectFile{}, errors.New("不能删除目录根节点")
	}
	info, err := os.Stat(full)
	if err != nil {
		return model.ProjectFile{}, friendlyPathError(full, err)
	}
	if req.IsDir != info.IsDir() {
		return model.ProjectFile{}, errors.New("目标类型与删除请求不一致")
	}
	result := directoryFileResult(full, info)
	if info.IsDir() {
		err = os.RemoveAll(full)
	} else {
		err = os.Remove(full)
	}
	if err != nil {
		return model.ProjectFile{}, friendlyPathError(full, err)
	}
	return result, nil
}

func kind(isDir bool) string {
	if isDir {
		return "目录"
	}
	return "文件"
}

func fileSize(entry os.DirEntry) int64 {
	if entry.IsDir() {
		return 0
	}
	info, err := entry.Info()
	if err != nil {
		return 0
	}
	return info.Size()
}

func (s *service) projectRoot(agentID string) (string, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return "", errors.New("agent_id 不能为空")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, agent := range s.agents {
		if agent.AgentID == agentID {
			root := filepath.Clean(agent.ProjectDir)
			info, err := os.Stat(root)
			if err != nil || !info.IsDir() {
				return "", errors.New("项目目录不存在")
			}
			return root, nil
		}
	}
	return "", errors.New("未找到 agent")
}

func cleanProjectRelativePath(path string) (string, error) {
	path = strings.TrimSpace(filepath.ToSlash(path))
	if path == "" || path == "." {
		return "", nil
	}
	if strings.HasPrefix(path, "/") || strings.Contains(path, "\x00") {
		return "", errors.New("路径不合法")
	}
	cleaned := filepath.ToSlash(filepath.Clean(path))
	if cleaned == "." {
		return "", nil
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", errors.New("路径不允许越过项目目录")
	}
	return cleaned, nil
}

func projectFilePath(root string, relative string) (string, string, error) {
	relative, err := cleanProjectRelativePath(relative)
	if err != nil {
		return "", "", err
	}
	if relative == ".chat-codex-uploads" || strings.HasPrefix(relative, ".chat-codex-uploads/") {
		return "", "", errors.New("路径不合法")
	}
	full := filepath.Clean(filepath.Join(root, filepath.FromSlash(relative)))
	if full != root && !strings.HasPrefix(full, root+string(os.PathSeparator)) {
		return "", "", errors.New("路径不允许越过项目目录")
	}
	return full, relative, nil
}

func (s *service) ProjectFiles(req model.ProjectFilesRequest) (model.DirectoryListResult, error) {
	root, err := s.projectRoot(req.AgentID)
	if err != nil {
		return model.DirectoryListResult{}, err
	}
	full, relative, err := projectFilePath(root, req.Path)
	if err != nil {
		return model.DirectoryListResult{}, err
	}
	info, err := os.Stat(full)
	if err != nil {
		return model.DirectoryListResult{}, friendlyPathError(full, err)
	}
	if !info.IsDir() {
		return model.DirectoryListResult{}, errors.New("路径不是目录")
	}
	entries, err := os.ReadDir(full)
	if err != nil {
		return model.DirectoryListResult{}, friendlyPathError(full, err)
	}
	items := make([]model.Directory, 0, len(entries))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		childRelative := filepath.ToSlash(filepath.Join(relative, entry.Name()))
		items = append(items, model.Directory{
			Path:  childRelative,
			Name:  entry.Name(),
			Kind:  kind(entry.IsDir()),
			IsDir: entry.IsDir(),
			Size:  fileSize(entry),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].IsDir != items[j].IsDir {
			return items[i].IsDir
		}
		return items[i].Name < items[j].Name
	})
	parent := ""
	if relative != "" {
		parent = filepath.ToSlash(filepath.Dir(relative))
		if parent == "." {
			parent = ""
		}
	}
	return model.DirectoryListResult{CurrentPath: relative, ParentPath: parent, Entries: items}, nil
}

func (s *service) DownloadProjectFile(req model.ProjectFilesRequest) (model.ProjectFile, error) {
	root, err := s.projectRoot(req.AgentID)
	if err != nil {
		return model.ProjectFile{}, err
	}
	full, relative, err := projectFilePath(root, req.Path)
	if err != nil {
		return model.ProjectFile{}, err
	}
	info, err := os.Stat(full)
	if err != nil {
		return model.ProjectFile{}, friendlyPathError(full, err)
	}
	if info.IsDir() {
		return model.ProjectFile{}, errors.New("不能下载目录")
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return model.ProjectFile{}, friendlyPathError(full, err)
	}
	return model.ProjectFile{
		Path:     relative,
		Name:     filepath.Base(full),
		Kind:     "文件",
		IsDir:    false,
		Size:     info.Size(),
		Content:  base64.StdEncoding.EncodeToString(data),
		Encoding: "base64",
	}, nil
}

func (s *service) CreateProjectDownload(req model.ProjectFilesRequest) (model.ProjectFile, error) {
	root, err := s.projectRoot(req.AgentID)
	if err != nil {
		return model.ProjectFile{}, err
	}
	full, relative, err := projectFilePath(root, req.Path)
	if err != nil {
		return model.ProjectFile{}, err
	}
	info, err := os.Stat(full)
	if err != nil {
		return model.ProjectFile{}, friendlyPathError(full, err)
	}
	if info.IsDir() {
		return model.ProjectFile{}, errors.New("不能下载目录")
	}
	sum, err := fileSHA256Hex(full)
	if err != nil {
		return model.ProjectFile{}, err
	}
	return model.ProjectFile{
		Path:     relative,
		Name:     filepath.Base(full),
		Kind:     "文件",
		IsDir:    false,
		Size:     info.Size(),
		Encoding: "base64",
		SHA256:   sum,
	}, nil
}

func (s *service) ReadProjectDownloadChunk(req model.ProjectFilesRequest) (model.ProjectFile, error) {
	root, err := s.projectRoot(req.AgentID)
	if err != nil {
		return model.ProjectFile{}, err
	}
	full, relative, err := projectFilePath(root, req.Path)
	if err != nil {
		return model.ProjectFile{}, err
	}
	info, err := os.Stat(full)
	if err != nil {
		return model.ProjectFile{}, friendlyPathError(full, err)
	}
	if info.IsDir() {
		return model.ProjectFile{}, errors.New("不能下载目录")
	}
	if req.Offset < 0 || req.Length <= 0 {
		return model.ProjectFile{}, errors.New("下载分块范围不合法")
	}
	file, err := os.Open(full)
	if err != nil {
		return model.ProjectFile{}, friendlyPathError(full, err)
	}
	defer file.Close()

	data := make([]byte, req.Length)
	n, err := file.ReadAt(data, req.Offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return model.ProjectFile{}, err
	}
	data = data[:n]
	return model.ProjectFile{
		Path:     relative,
		Name:     filepath.Base(full),
		Kind:     "文件",
		IsDir:    false,
		Size:     int64(len(data)),
		Content:  base64.StdEncoding.EncodeToString(data),
		Encoding: "base64",
	}, nil
}

func (s *service) UploadProjectFile(req model.ProjectFilesRequest) (model.ProjectFile, error) {
	root, err := s.projectRoot(req.AgentID)
	if err != nil {
		return model.ProjectFile{}, err
	}
	full, relative, err := projectFilePath(root, req.Path)
	if err != nil {
		return model.ProjectFile{}, err
	}
	if relative == "" {
		return model.ProjectFile{}, errors.New("文件路径不能为空")
	}
	encoding := strings.TrimSpace(req.Encoding)
	if encoding == "" {
		encoding = "base64"
	}
	if encoding != "base64" {
		return model.ProjectFile{}, errors.New("仅支持 base64 编码")
	}
	data, err := base64.StdEncoding.DecodeString(req.Content)
	if err != nil {
		return model.ProjectFile{}, errors.New("文件内容不是有效 base64")
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return model.ProjectFile{}, err
	}
	if err := os.WriteFile(full, data, 0o644); err != nil {
		return model.ProjectFile{}, err
	}
	info, err := os.Stat(full)
	if err != nil {
		return model.ProjectFile{}, friendlyPathError(full, err)
	}
	return model.ProjectFile{
		Path:     relative,
		Name:     filepath.Base(full),
		Kind:     "文件",
		IsDir:    false,
		Size:     info.Size(),
		Encoding: "base64",
	}, nil
}

func (s *service) CreateProjectUpload(req model.ProjectFilesRequest) (model.ProjectFile, error) {
	root, err := s.projectRoot(req.AgentID)
	if err != nil {
		return model.ProjectFile{}, err
	}
	full, relative, err := projectFilePath(root, req.Path)
	if err != nil {
		return model.ProjectFile{}, err
	}
	if relative == "" {
		return model.ProjectFile{}, errors.New("文件路径不能为空")
	}
	if req.Size < 0 {
		return model.ProjectFile{}, errors.New("文件大小不合法")
	}
	if req.TotalChunks <= 0 {
		return model.ProjectFile{}, errors.New("分块数量不合法")
	}
	tempPath, err := projectUploadTempPath(root, req.UploadID)
	if err != nil {
		return model.ProjectFile{}, err
	}
	if err := prepareUploadSession(tempPath, req.Size, req.TotalChunks, req.Resume); err != nil {
		return model.ProjectFile{}, err
	}
	return model.ProjectFile{
		Path:  relative,
		Name:  filepath.Base(full),
		Kind:  "文件",
		IsDir: false,
		Size:  req.Size,
	}, nil
}

func (s *service) WriteProjectUploadChunk(req model.ProjectFilesRequest) (model.ProjectFile, error) {
	root, err := s.projectRoot(req.AgentID)
	if err != nil {
		return model.ProjectFile{}, err
	}
	full, relative, err := projectFilePath(root, req.Path)
	if err != nil {
		return model.ProjectFile{}, err
	}
	if relative == "" {
		return model.ProjectFile{}, errors.New("文件路径不能为空")
	}
	if req.Offset < 0 || req.ChunkIndex < 0 {
		return model.ProjectFile{}, errors.New("分块位置不合法")
	}
	encoding := strings.TrimSpace(req.Encoding)
	if encoding == "" {
		encoding = "base64"
	}
	if encoding != "base64" {
		return model.ProjectFile{}, errors.New("仅支持 base64 编码")
	}
	data, err := base64.StdEncoding.DecodeString(req.Content)
	if err != nil {
		return model.ProjectFile{}, errors.New("文件分块不是有效 base64")
	}
	if req.SHA256 != "" && !strings.EqualFold(req.SHA256, sha256Hex(data)) {
		return model.ProjectFile{}, errors.New("文件分块校验失败")
	}
	tempPath, err := projectUploadTempPath(root, req.UploadID)
	if err != nil {
		return model.ProjectFile{}, err
	}
	file, err := os.OpenFile(tempPath, os.O_WRONLY, 0o600)
	if err != nil {
		return model.ProjectFile{}, err
	}
	if _, err := file.WriteAt(data, req.Offset); err != nil {
		_ = file.Close()
		return model.ProjectFile{}, err
	}
	if err := file.Close(); err != nil {
		return model.ProjectFile{}, err
	}
	if err := recordUploadChunk(tempPath, int64(req.Size), req.TotalChunks, req.ChunkIndex); err != nil {
		return model.ProjectFile{}, err
	}
	return model.ProjectFile{
		Path:  relative,
		Name:  filepath.Base(full),
		Kind:  "文件",
		IsDir: false,
		Size:  int64(len(data)),
	}, nil
}

func (s *service) CompleteProjectUpload(req model.ProjectFilesRequest) (model.ProjectFile, error) {
	root, err := s.projectRoot(req.AgentID)
	if err != nil {
		return model.ProjectFile{}, err
	}
	full, relative, err := projectFilePath(root, req.Path)
	if err != nil {
		return model.ProjectFile{}, err
	}
	if relative == "" {
		return model.ProjectFile{}, errors.New("文件路径不能为空")
	}
	tempPath, err := projectUploadTempPath(root, req.UploadID)
	if err != nil {
		return model.ProjectFile{}, err
	}
	info, err := os.Stat(tempPath)
	if err != nil {
		return model.ProjectFile{}, err
	}
	if req.Size >= 0 && info.Size() != req.Size {
		return model.ProjectFile{}, errors.New("文件大小校验失败")
	}
	if req.SHA256 != "" {
		sum, err := fileSHA256Hex(tempPath)
		if err != nil {
			return model.ProjectFile{}, err
		}
		if !strings.EqualFold(req.SHA256, sum) {
			return model.ProjectFile{}, errors.New("文件整体校验失败")
		}
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return model.ProjectFile{}, err
	}
	backupPath, restore, err := backupExistingProjectFile(full)
	if err != nil {
		return model.ProjectFile{}, err
	}
	if err := os.Rename(tempPath, full); err != nil {
		restore()
		return model.ProjectFile{}, err
	}
	if backupPath != "" {
		_ = os.Remove(backupPath)
	}
	removeUploadManifest(tempPath)
	_ = os.RemoveAll(filepath.Dir(tempPath))
	finalInfo, err := os.Stat(full)
	if err != nil {
		return model.ProjectFile{}, friendlyPathError(full, err)
	}
	return model.ProjectFile{
		Path:  relative,
		Name:  filepath.Base(full),
		Kind:  "文件",
		IsDir: false,
		Size:  finalInfo.Size(),
	}, nil
}

// ProjectUploadStatus 返回项目文件分块上传的当前进度，供断点续传使用。
func (s *service) ProjectUploadStatus(req model.ProjectFilesRequest) (model.UploadStatus, error) {
	root, err := s.projectRoot(req.AgentID)
	if err != nil {
		return model.UploadStatus{}, err
	}
	_, relative, err := projectFilePath(root, req.Path)
	if err != nil {
		return model.UploadStatus{}, err
	}
	tempPath, err := projectUploadTempPath(root, req.UploadID)
	if err != nil {
		return model.UploadStatus{}, err
	}
	return uploadStatusFor(tempPath, relative, req.UploadID, req.Size, req.TotalChunks), nil
}

// DirectoryUploadStatus 返回设备目录分块上传的当前进度，供断点续传使用。
func (s *service) DirectoryUploadStatus(req model.DirectoryFileRequest) (model.UploadStatus, error) {
	full, err := directoryMutationPath(s.cfg, req.Path, req.AllowAll, false)
	if err != nil {
		return model.UploadStatus{}, err
	}
	tempPath, err := directoryUploadTempPath(full, req.UploadID)
	if err != nil {
		return model.UploadStatus{}, err
	}
	return uploadStatusFor(tempPath, full, req.UploadID, req.Size, req.TotalChunks), nil
}

func (s *service) CreateProjectFile(req model.ProjectFilesRequest) (model.ProjectFile, error) {
	root, err := s.projectRoot(req.AgentID)
	if err != nil {
		return model.ProjectFile{}, err
	}
	full, relative, err := projectFilePath(root, req.Path)
	if err != nil {
		return model.ProjectFile{}, err
	}
	if relative == "" {
		return model.ProjectFile{}, errors.New("文件路径不能为空")
	}
	file, err := os.OpenFile(full, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return model.ProjectFile{}, errors.New("目标名称已存在")
		}
		return model.ProjectFile{}, friendlyPathError(full, err)
	}
	if err := file.Close(); err != nil {
		return model.ProjectFile{}, err
	}
	info, err := os.Stat(full)
	if err != nil {
		return model.ProjectFile{}, friendlyPathError(full, err)
	}
	if info.IsDir() {
		return model.ProjectFile{}, errors.New("创建结果不是文件")
	}
	return model.ProjectFile{
		Path:  relative,
		Name:  filepath.Base(full),
		Kind:  "文件",
		IsDir: false,
		Size:  info.Size(),
	}, nil
}

func (s *service) CreateProjectFolder(req model.ProjectFilesRequest) (model.ProjectFile, error) {
	root, err := s.projectRoot(req.AgentID)
	if err != nil {
		return model.ProjectFile{}, err
	}
	full, relative, err := projectFilePath(root, req.Path)
	if err != nil {
		return model.ProjectFile{}, err
	}
	if relative == "" {
		return model.ProjectFile{}, errors.New("文件夹路径不能为空")
	}
	if err := os.Mkdir(full, 0o755); err != nil {
		return model.ProjectFile{}, friendlyPathError(full, err)
	}
	info, err := os.Stat(full)
	if err != nil {
		return model.ProjectFile{}, friendlyPathError(full, err)
	}
	if !info.IsDir() {
		return model.ProjectFile{}, errors.New("创建结果不是文件夹")
	}
	return model.ProjectFile{
		Path:  relative,
		Name:  filepath.Base(full),
		Kind:  "目录",
		IsDir: true,
	}, nil
}

func (s *service) DeleteProjectFile(req model.ProjectFilesRequest) (model.ProjectFile, error) {
	root, err := s.projectRoot(req.AgentID)
	if err != nil {
		return model.ProjectFile{}, err
	}
	full, relative, err := projectFilePath(root, req.Path)
	if err != nil {
		return model.ProjectFile{}, err
	}
	if relative == "" {
		return model.ProjectFile{}, errors.New("不能删除项目根目录")
	}
	info, err := os.Stat(full)
	if err != nil {
		return model.ProjectFile{}, friendlyPathError(full, err)
	}
	if req.IsDir && !info.IsDir() {
		return model.ProjectFile{}, errors.New("目标不是文件夹")
	}
	if !req.IsDir && info.IsDir() {
		return model.ProjectFile{}, errors.New("目标是文件夹")
	}
	file := model.ProjectFile{
		Path:  relative,
		Name:  filepath.Base(full),
		Kind:  kind(info.IsDir()),
		IsDir: info.IsDir(),
		Size:  info.Size(),
	}
	if info.IsDir() {
		if err := os.RemoveAll(full); err != nil {
			return model.ProjectFile{}, friendlyPathError(full, err)
		}
	} else if err := os.Remove(full); err != nil {
		return model.ProjectFile{}, friendlyPathError(full, err)
	}
	return file, nil
}

func (s *service) RenameProjectFile(req model.ProjectFilesRequest) (model.ProjectFile, error) {
	root, err := s.projectRoot(req.AgentID)
	if err != nil {
		return model.ProjectFile{}, err
	}
	full, relative, err := projectFilePath(root, req.Path)
	if err != nil {
		return model.ProjectFile{}, err
	}
	if relative == "" {
		return model.ProjectFile{}, errors.New("不能重命名项目根目录")
	}
	newName, err := cleanProjectEntryName(req.Name)
	if err != nil {
		return model.ProjectFile{}, err
	}
	info, err := os.Stat(full)
	if err != nil {
		return model.ProjectFile{}, friendlyPathError(full, err)
	}
	parentRelative := filepath.ToSlash(filepath.Dir(relative))
	if parentRelative == "." {
		parentRelative = ""
	}
	targetRelative := newName
	if parentRelative != "" {
		targetRelative = filepath.ToSlash(filepath.Join(parentRelative, newName))
	}
	targetFull, targetRelative, err := projectFilePath(root, targetRelative)
	if err != nil {
		return model.ProjectFile{}, err
	}
	if targetFull == full {
		return model.ProjectFile{
			Path:  targetRelative,
			Name:  newName,
			Kind:  kind(info.IsDir()),
			IsDir: info.IsDir(),
			Size:  info.Size(),
		}, nil
	}
	if _, err := os.Stat(targetFull); err == nil {
		return model.ProjectFile{}, errors.New("目标名称已存在")
	} else if !os.IsNotExist(err) {
		return model.ProjectFile{}, err
	}
	if err := os.Rename(full, targetFull); err != nil {
		return model.ProjectFile{}, friendlyPathError(full, err)
	}
	finalInfo, err := os.Stat(targetFull)
	if err != nil {
		return model.ProjectFile{}, friendlyPathError(targetFull, err)
	}
	return model.ProjectFile{
		Path:  targetRelative,
		Name:  filepath.Base(targetFull),
		Kind:  kind(finalInfo.IsDir()),
		IsDir: finalInfo.IsDir(),
		Size:  finalInfo.Size(),
	}, nil
}

func cleanProjectEntryName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("名称不能为空")
	}
	if name == "." || name == ".." {
		return "", errors.New("名称不合法")
	}
	if strings.Contains(name, "\x00") || strings.Contains(name, "/") || strings.Contains(name, `\`) {
		return "", errors.New("名称不能包含路径分隔符")
	}
	return name, nil
}

func backupExistingProjectFile(path string) (string, func(), error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return "", func() {}, nil
	} else if err != nil {
		return "", func() {}, err
	}
	backupPath := path + ".chat-codex-upload-backup"
	for i := 1; ; i++ {
		if _, err := os.Stat(backupPath); os.IsNotExist(err) {
			break
		} else if err != nil {
			return "", func() {}, err
		}
		backupPath = path + ".chat-codex-upload-backup-" + strconv.Itoa(i)
	}
	if err := os.Rename(path, backupPath); err != nil {
		return "", func() {}, err
	}
	restore := func() {
		if _, err := os.Stat(path); err == nil {
			_ = os.Remove(path)
		}
		_ = os.Rename(backupPath, path)
	}
	return backupPath, restore, nil
}

func projectUploadTempPath(root string, uploadID string) (string, error) {
	uploadID = strings.TrimSpace(uploadID)
	if uploadID == "" {
		return "", errors.New("upload_id 不能为空")
	}
	for _, ch := range uploadID {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_' {
			continue
		}
		return "", errors.New("upload_id 不合法")
	}
	return filepath.Join(root, ".chat-codex-uploads", uploadID, "payload.bin"), nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func fileSHA256Hex(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
