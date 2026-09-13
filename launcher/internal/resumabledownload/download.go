package resumabledownload

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type ProgressFunc func(received int64, total int64) error

type Options struct {
	Client         *http.Client
	URL            string
	Target         string
	ExpectedSHA256 string
	Headers        map[string]string
	Progress       ProgressFunc
}

type Result struct {
	ReceivedBytes int64
	TotalBytes    int64
	ResumedFrom   int64
	Reused        bool
}

type partialMetadata struct {
	URL          string `json:"url"`
	ETag         string `json:"etag,omitempty"`
	LastModified string `json:"last_modified,omitempty"`
	TotalBytes   int64  `json:"total_bytes,omitempty"`
}

func Fetch(ctx context.Context, options Options) (Result, error) {
	options.URL = strings.TrimSpace(options.URL)
	options.Target = strings.TrimSpace(options.Target)
	options.ExpectedSHA256 = strings.TrimSpace(options.ExpectedSHA256)
	if options.URL == "" {
		return Result{}, errors.New("下载地址不能为空")
	}
	if options.Target == "" {
		return Result{}, errors.New("下载目标不能为空")
	}
	if options.Client == nil {
		options.Client = http.DefaultClient
	}
	if err := os.MkdirAll(filepath.Dir(options.Target), 0o755); err != nil {
		return Result{}, err
	}

	if options.ExpectedSHA256 != "" {
		if size, valid := validExistingFile(options.Target, options.ExpectedSHA256); valid {
			removePartial(options.Target+".part", options.Target+".part.json")
			if options.Progress != nil {
				if err := options.Progress(size, size); err != nil {
					return Result{}, err
				}
			}
			return Result{ReceivedBytes: size, TotalBytes: size, ResumedFrom: size, Reused: true}, nil
		}
	}

	partialPath := options.Target + ".part"
	metadataPath := partialPath + ".json"
	if err := migrateIncompleteTarget(options.Target, partialPath); err != nil {
		return Result{}, err
	}
	metadata := readPartialMetadata(metadataPath)
	if metadata.URL != "" && metadata.URL != options.URL {
		removePartial(partialPath, metadataPath)
		metadata = partialMetadata{}
	}

	resetUsed := false
	for {
		result, complete, restart, nextMetadata, err := fetchPartial(ctx, options, partialPath, metadata)
		if err != nil {
			return Result{}, err
		}
		metadata = nextMetadata
		if restart {
			if resetUsed {
				removePartial(partialPath, metadataPath)
				return Result{}, errors.New("服务端返回的续传范围无效")
			}
			removePartial(partialPath, metadataPath)
			metadata = partialMetadata{}
			resetUsed = true
			continue
		}
		if !complete {
			return Result{}, errors.New("下载未完成")
		}
		if options.ExpectedSHA256 != "" {
			if err := verifySHA256(partialPath, options.ExpectedSHA256); err != nil {
				removePartial(partialPath, metadataPath)
				if !resetUsed {
					metadata = partialMetadata{}
					resetUsed = true
					continue
				}
				return Result{}, err
			}
		}
		if err := os.Remove(options.Target); err != nil && !errors.Is(err, os.ErrNotExist) {
			return Result{}, err
		}
		if err := os.Rename(partialPath, options.Target); err != nil {
			return Result{}, err
		}
		_ = os.Remove(metadataPath)
		return result, nil
	}
}

func fetchPartial(ctx context.Context, options Options, partialPath string, metadata partialMetadata) (Result, bool, bool, partialMetadata, error) {
	offset := fileSize(partialPath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, options.URL, nil)
	if err != nil {
		return Result{}, false, false, metadata, err
	}
	for key, value := range options.Headers {
		if strings.TrimSpace(key) != "" && strings.TrimSpace(value) != "" {
			req.Header.Set(key, value)
		}
	}
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
		if validator := firstNonEmpty(metadata.ETag, metadata.LastModified); validator != "" {
			req.Header.Set("If-Range", validator)
		}
	}
	resp, err := options.Client.Do(req)
	if err != nil {
		return Result{}, false, false, metadata, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable && offset > 0 {
		total := unsatisfiedRangeTotal(resp.Header.Get("Content-Range"))
		if total > 0 && total == offset {
			return Result{ReceivedBytes: offset, TotalBytes: total, ResumedFrom: offset}, true, false, metadata, nil
		}
		return Result{}, false, true, metadata, nil
	}

	appendFile := false
	total := resp.ContentLength
	resumedFrom := int64(0)
	switch resp.StatusCode {
	case http.StatusOK:
		offset = 0
	case http.StatusPartialContent:
		start, rangeTotal, ok := parseContentRange(resp.Header.Get("Content-Range"))
		if !ok || start != offset {
			return Result{}, false, true, metadata, nil
		}
		appendFile = offset > 0
		resumedFrom = offset
		if rangeTotal > 0 {
			total = rangeTotal
		} else if resp.ContentLength >= 0 {
			total = offset + resp.ContentLength
		}
	default:
		return Result{}, false, false, metadata, fmt.Errorf("下载失败: %s", resp.Status)
	}
	if total > 0 && offset > total {
		return Result{}, false, true, metadata, nil
	}

	flags := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	if appendFile {
		flags = os.O_CREATE | os.O_WRONLY | os.O_APPEND
	}
	file, err := os.OpenFile(partialPath, flags, 0o644)
	if err != nil {
		return Result{}, false, false, metadata, err
	}
	metadata = partialMetadata{
		URL:          options.URL,
		ETag:         strings.TrimSpace(resp.Header.Get("ETag")),
		LastModified: strings.TrimSpace(resp.Header.Get("Last-Modified")),
		TotalBytes:   total,
	}
	_ = writePartialMetadata(partialPath+".json", metadata)

	received := offset
	if options.Progress != nil {
		if err := options.Progress(received, total); err != nil {
			file.Close()
			return Result{}, false, false, metadata, err
		}
	}
	buffer := make([]byte, 64*1024)
	for {
		n, readErr := resp.Body.Read(buffer)
		if n > 0 {
			if _, err := file.Write(buffer[:n]); err != nil {
				file.Close()
				return Result{}, false, false, metadata, err
			}
			received += int64(n)
			if options.Progress != nil {
				if err := options.Progress(received, total); err != nil {
					file.Close()
					return Result{}, false, false, metadata, err
				}
			}
		}
		if errors.Is(readErr, io.EOF) {
			if err := file.Close(); err != nil {
				return Result{}, false, false, metadata, err
			}
			if total > 0 && received != total {
				return Result{}, false, false, metadata, fmt.Errorf("下载提前结束: received=%d total=%d", received, total)
			}
			return Result{ReceivedBytes: received, TotalBytes: total, ResumedFrom: resumedFrom}, true, false, metadata, nil
		}
		if readErr != nil {
			file.Close()
			return Result{}, false, false, metadata, readErr
		}
	}
}

func validExistingFile(path string, expected string) (int64, bool) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return 0, false
	}
	return info.Size(), verifySHA256(path, expected) == nil
}

func migrateIncompleteTarget(target string, partial string) error {
	if _, err := os.Stat(target); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if _, err := os.Stat(partial); err == nil {
		return os.Remove(target)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(target, partial)
}

func verifySHA256(path string, expected string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if !strings.EqualFold(actual, strings.TrimSpace(expected)) {
		return fmt.Errorf("sha256 校验失败: expected=%s actual=%s", expected, actual)
	}
	return nil
}

func parseContentRange(value string) (int64, int64, bool) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(strings.ToLower(value), "bytes ") {
		return 0, 0, false
	}
	parts := strings.SplitN(strings.TrimSpace(value[6:]), "/", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	rangeParts := strings.SplitN(parts[0], "-", 2)
	if len(rangeParts) != 2 {
		return 0, 0, false
	}
	start, err := strconv.ParseInt(rangeParts[0], 10, 64)
	if err != nil || start < 0 {
		return 0, 0, false
	}
	total, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || total <= 0 {
		return 0, 0, false
	}
	return start, total, true
}

func unsatisfiedRangeTotal(value string) int64 {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(strings.ToLower(value), "bytes */") {
		return 0
	}
	total, _ := strconv.ParseInt(strings.TrimSpace(value[len("bytes */"):]), 10, 64)
	return total
}

func readPartialMetadata(path string) partialMetadata {
	data, err := os.ReadFile(path)
	if err != nil {
		return partialMetadata{}
	}
	var metadata partialMetadata
	if json.Unmarshal(data, &metadata) != nil {
		return partialMetadata{}
	}
	return metadata
}

func writePartialMetadata(path string, metadata partialMetadata) error {
	data, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func removePartial(partialPath string, metadataPath string) {
	_ = os.Remove(partialPath)
	_ = os.Remove(metadataPath)
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return 0
	}
	return info.Size()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
