package download

import (
	"archive/zip"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestMatchAssetRejectsUnknownPlatform(t *testing.T) {
	_, err := matchAsset([]downloadVariant{{Platform: "other", Filename: "x"}})
	if err == nil {
		t.Fatal("expected matchAsset to fail for unmatched platform")
	}
}

func TestUnpackZipRequiresMatchingExecutable(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "bad.zip")
	writer, err := os.Create(archive)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	zipWriter := zip.NewWriter(writer)
	file, err := zipWriter.Create("not-opencode")
	if err != nil {
		t.Fatalf("create zip entry: %v", err)
	}
	if _, err := file.Write([]byte("data")); err != nil {
		t.Fatalf("write zip entry: %v", err)
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close archive file: %v", err)
	}

	_, err = unpackZip(archive, t.TempDir())
	if err == nil || err.Error() != "压缩包中没有匹配的可执行文件" {
		t.Fatalf("expected matching executable error, got %v", err)
	}
}

func TestCurrentPlatformReturnsKnownShape(t *testing.T) {
	value := currentPlatform()
	if value == "" {
		t.Fatal("expected current platform")
	}
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" && value != "darwin-arm64" {
		t.Fatalf("unexpected platform %s", value)
	}
}

func TestResolveDownloadURLKeepsAbsoluteURL(t *testing.T) {
	raw := "https://www.xyapi.top/codex/api/app/download?artifact=opencode-windows-x64.zip"
	if got := resolveDownloadURL("https://www.xyapi.top/codex", raw); got != raw {
		t.Fatalf("expected absolute URL to stay unchanged, got %s", got)
	}
}

func TestResolveDownloadURLBuildsAbsoluteFromRelativePath(t *testing.T) {
	got := resolveDownloadURL("https://www.xyapi.top/codex", "/api/app/download?artifact=opencode-windows-x64.zip")
	want := "https://www.xyapi.top/api/app/download?artifact=opencode-windows-x64.zip"
	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

func TestResolveDownloadURLBuildsAbsoluteFromRelativeRef(t *testing.T) {
	got := resolveDownloadURL("https://www.xyapi.top/codex", "api/app/download?artifact=opencode-windows-x64.zip")
	want := "https://www.xyapi.top/codex/api/app/download?artifact=opencode-windows-x64.zip"
	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}
