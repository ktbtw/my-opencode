package resumabledownload

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestFetchResumesInterruptedDownloadAndVerifiesHash(t *testing.T) {
	data := []byte(strings.Repeat("resumable-download-fixture-", 8192))
	split := len(data) / 3
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request := requests.Add(1)
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Last-Modified", "Tue, 28 Jul 2026 05:00:00 GMT")
		if request == 1 {
			w.Header().Set("Content-Length", fmt.Sprint(len(data)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(data[:split])
			return
		}
		wantRange := fmt.Sprintf("bytes=%d-", split)
		if r.Header.Get("Range") != wantRange {
			t.Errorf("Range header: got %q want %q", r.Header.Get("Range"), wantRange)
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", split, len(data)-1, len(data)))
		w.Header().Set("Content-Length", fmt.Sprint(len(data)-split))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(data[split:])
	}))
	t.Cleanup(server.Close)

	target := filepath.Join(t.TempDir(), "archive.zip")
	options := Options{URL: server.URL, Target: target, ExpectedSHA256: sha256Hex(data)}
	if _, err := Fetch(context.Background(), options); err == nil {
		t.Fatal("expected interrupted first download")
	}
	if got := fileSize(target + ".part"); got != int64(split) {
		t.Fatalf("partial size: got %d want %d", got, split)
	}
	result, err := Fetch(context.Background(), options)
	if err != nil {
		t.Fatalf("resume download: %v", err)
	}
	if result.ResumedFrom != int64(split) {
		t.Fatalf("resumed from: got %d want %d", result.ResumedFrom, split)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(data) {
		t.Fatal("resumed file differs from source")
	}
}

func TestFetchRestartsWhenServerIgnoresRange(t *testing.T) {
	data := []byte(strings.Repeat("full-response-", 2048))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	}))
	t.Cleanup(server.Close)

	target := filepath.Join(t.TempDir(), "archive.zip")
	if err := os.WriteFile(target+".part", []byte("stale partial"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Fetch(context.Background(), Options{URL: server.URL, Target: target, ExpectedSHA256: sha256Hex(data)})
	if err != nil {
		t.Fatalf("restart download: %v", err)
	}
	if result.ResumedFrom != 0 {
		t.Fatalf("expected full restart, got resumed_from=%d", result.ResumedFrom)
	}
	got, _ := os.ReadFile(target)
	if string(got) != string(data) {
		t.Fatal("full response did not replace partial data")
	}
}

func TestFetchReusesVerifiedTargetWithoutRequest(t *testing.T) {
	data := []byte("verified existing archive")
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	t.Cleanup(server.Close)

	target := filepath.Join(t.TempDir(), "archive.zip")
	if err := os.WriteFile(target, data, 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Fetch(context.Background(), Options{URL: server.URL, Target: target, ExpectedSHA256: sha256Hex(data)})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Reused || requests.Load() != 0 {
		t.Fatalf("expected verified target reuse, result=%+v requests=%d", result, requests.Load())
	}
}

func sha256Hex(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
