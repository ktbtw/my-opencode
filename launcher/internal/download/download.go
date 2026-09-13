package download

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"launcher/internal/binarymeta"
	"launcher/internal/defaults"
	"launcher/internal/release"
	"launcher/internal/resumabledownload"
)

type versionBody struct {
	Version   string            `json:"version"`
	Channel   string            `json:"channel"`
	Downloads []downloadVariant `json:"downloads"`
}

type downloadVariant struct {
	Platform string `json:"platform"`
	Filename string `json:"filename"`
	URL      string `json:"url"`
	SHA256   string `json:"sha256"`
}

type ProgressFunc func(received int64, total int64)

func Fetch(root string, target string) error {
	return fetch(root, target, nil)
}

func FetchWithProgress(root string, target string, progress ProgressFunc) error {
	return fetch(root, target, progress)
}

func Latest() (string, error) {
	body, err := fetchVersionBody()
	if err != nil {
		return "", err
	}
	return body.Version, nil
}

func FetchLatest(root string, progress ProgressFunc) (string, error) {
	body, err := fetchVersionBody()
	if err != nil {
		return "", err
	}
	if err := install(root, body, progress); err != nil {
		return "", err
	}
	return body.Version, nil
}

func fetch(root string, target string, progress ProgressFunc) error {
	body, err := fetchVersionBody()
	if err != nil {
		return err
	}
	if body.Version != target {
		return fmt.Errorf("版本源当前为 %s，不是目标版本 %s", body.Version, target)
	}
	return install(root, body, progress)
}

func fetchVersionBody() (versionBody, error) {
	base := versionBaseURL()
	resp, err := http.Get(base + "/api/cli/version")
	if err != nil {
		return versionBody{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return versionBody{}, fmt.Errorf("获取版本信息失败: %s", resp.Status)
	}
	var body versionBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return versionBody{}, err
	}
	normalizeDownloadURLs(base, &body)
	return body, nil
}

func versionBaseURL() string {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("LAUNCHER_UPDATE_BASE_URL")), "/")
	if base != "" {
		return base
	}
	base = strings.TrimRight(strings.TrimSpace(os.Getenv("LAUNCHER_PUBLIC_BASE")), "/")
	if base != "" {
		return base
	}
	return defaults.PublicBase
}

func install(root string, body versionBody, progress ProgressFunc) error {
	asset, err := matchAsset(body.Downloads)
	if err != nil {
		return err
	}
	versionDir := filepath.Join(root, "versions", body.Version)
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		return err
	}
	archivePath := filepath.Join(root, "downloads", asset.Filename)
	if err := fetchFile(asset.URL, archivePath, asset.SHA256, progress); err != nil {
		return err
	}
	executable, err := unpack(archivePath, versionDir)
	if err != nil {
		return err
	}
	if err := release.WriteManifest(root, body.Version, release.Manifest{
		Version:    body.Version,
		Channel:    body.Channel,
		Executable: executable,
	}); err != nil {
		return err
	}
	return nil
}

func matchAsset(list []downloadVariant) (downloadVariant, error) {
	platform := currentPlatform()
	for _, item := range list {
		if item.Platform == platform {
			return item, nil
		}
	}
	return downloadVariant{}, fmt.Errorf("未找到当前平台 %s 的安装包", platform)
}

func currentPlatform() string {
	switch runtime.GOOS {
	case "darwin":
		switch runtime.GOARCH {
		case "arm64":
			return "darwin-arm64"
		case "amd64":
			return "darwin-x64"
		}
	case "linux":
		switch runtime.GOARCH {
		case "amd64":
			return "linux-x64"
		case "arm64":
			return "linux-arm64"
		}
	case "windows":
		switch runtime.GOARCH {
		case "amd64":
			return "windows-x64"
		case "arm64":
			return "windows-arm64"
		}
	}
	return runtime.GOOS + "-" + runtime.GOARCH
}

func normalizeDownloadURLs(base string, body *versionBody) {
	if body == nil {
		return
	}
	for i := range body.Downloads {
		body.Downloads[i].URL = resolveDownloadURL(base, body.Downloads[i].URL)
	}
}

func resolveDownloadURL(base string, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err == nil && parsed.IsAbs() {
		return parsed.String()
	}
	base = strings.TrimSpace(base)
	if base == "" {
		return raw
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return raw
	}
	ref, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if strings.HasPrefix(raw, "/") {
		return (&url.URL{
			Scheme:   baseURL.Scheme,
			Host:     baseURL.Host,
			Path:     ref.Path,
			RawQuery: ref.RawQuery,
			Fragment: ref.Fragment,
		}).String()
	}
	if !strings.HasSuffix(baseURL.Path, "/") {
		baseURL.Path = strings.TrimRight(baseURL.Path, "/") + "/"
	}
	return baseURL.ResolveReference(ref).String()
}

func fetchFile(url string, target string, expectedSHA256 string, progress ProgressFunc) error {
	_, err := resumabledownload.Fetch(context.Background(), resumabledownload.Options{
		URL:            url,
		Target:         target,
		ExpectedSHA256: expectedSHA256,
		Progress: func(received int64, total int64) error {
			if progress != nil {
				progress(received, total)
			}
			return nil
		},
	})
	return err
}

func unpack(archivePath string, target string) (string, error) {
	if strings.HasSuffix(archivePath, ".zip") {
		return unpackZip(archivePath, target)
	}
	if strings.HasSuffix(archivePath, ".tar.gz") {
		return unpackTarGz(archivePath, target)
	}
	return "", errors.New("不支持的压缩包格式")
}

func unpackZip(archivePath string, target string) (string, error) {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return "", err
	}
	defer reader.Close()
	for _, file := range reader.File {
		name := filepath.Base(file.Name)
		if name == "" || strings.HasPrefix(name, ".") {
			continue
		}
		if !binarymeta.MatchesExecutableName(name) {
			continue
		}
		path := filepath.Join(target, name)
		if err := writeZipFile(file, path); err != nil {
			return "", err
		}
		return name, nil
	}
	return "", errors.New("压缩包中没有匹配的可执行文件")
}

func writeZipFile(file *zip.File, path string) error {
	reader, err := file.Open()
	if err != nil {
		return err
	}
	defer reader.Close()
	writer, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	defer writer.Close()
	_, err = io.Copy(writer, reader)
	return err
}

func unpackTarGz(archivePath string, target string) (string, error) {
	file, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return "", err
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
		name := filepath.Base(header.Name)
		if name == "" || strings.HasPrefix(name, ".") {
			continue
		}
		if !binarymeta.MatchesExecutableName(name) {
			continue
		}
		path := filepath.Join(target, name)
		writer, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			return "", err
		}
		if _, err := io.Copy(writer, reader); err != nil {
			writer.Close()
			return "", err
		}
		writer.Close()
		return name, nil
	}
	return "", errors.New("压缩包中没有匹配的可执行文件")
}
