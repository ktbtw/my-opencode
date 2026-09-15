package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"

	"launcher/internal/model"
)

// uploadManifestName 是与 payload.bin 同目录的分块清单文件名。
// 清单记录已成功写入的分块下标，用于断点续传时跳过重复分块。
const uploadManifestName = "chunks.json"

type uploadManifest struct {
	Size        int64 `json:"size"`
	TotalChunks int   `json:"total_chunks"`
	Received    []int `json:"received"`
}

func uploadManifestPath(tempPath string) string {
	return filepath.Join(filepath.Dir(tempPath), uploadManifestName)
}

func loadUploadManifest(tempPath string) (uploadManifest, bool) {
	data, err := os.ReadFile(uploadManifestPath(tempPath))
	if err != nil {
		return uploadManifest{}, false
	}
	var manifest uploadManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return uploadManifest{}, false
	}
	return manifest, true
}

func saveUploadManifest(tempPath string, manifest uploadManifest) error {
	sort.Ints(manifest.Received)
	data, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	return os.WriteFile(uploadManifestPath(tempPath), data, 0o600)
}

func removeUploadManifest(tempPath string) {
	_ = os.Remove(uploadManifestPath(tempPath))
}

// resetUploadSession 清空分块清单，用于全新上传。
func resetUploadSession(tempPath string, size int64, totalChunks int) error {
	return saveUploadManifest(tempPath, uploadManifest{
		Size:        size,
		TotalChunks: totalChunks,
		Received:    []int{},
	})
}

// recordUploadChunk 记录某个分块已写入，重复写入同一下标不会产生重复项。
func recordUploadChunk(tempPath string, size int64, totalChunks int, chunkIndex int) error {
	manifest, _ := loadUploadManifest(tempPath)
	manifest.Size = size
	manifest.TotalChunks = totalChunks
	for _, item := range manifest.Received {
		if item == chunkIndex {
			return nil
		}
	}
	manifest.Received = append(manifest.Received, chunkIndex)
	return saveUploadManifest(tempPath, manifest)
}

// uploadStatusFor 汇总当前上传进度，供客户端断点续传判断。
func uploadStatusFor(tempPath, path, uploadID string, size int64, totalChunks int) model.UploadStatus {
	status := model.UploadStatus{
		Path:           path,
		UploadID:       uploadID,
		Size:           size,
		TotalChunks:    totalChunks,
		ReceivedChunks: []int{},
	}
	manifest, ok := loadUploadManifest(tempPath)
	if !ok {
		return status
	}
	status.ReceivedChunks = append([]int{}, manifest.Received...)
	sort.Ints(status.ReceivedChunks)
	status.Size = manifest.Size
	status.TotalChunks = manifest.TotalChunks
	status.Resumable = true
	if info, err := os.Stat(tempPath); err == nil {
		status.ReceivedBytes = info.Size()
	}
	if status.TotalChunks > 0 && len(status.ReceivedChunks) >= status.TotalChunks {
		status.Completed = true
	}
	return status
}

// openUploadTempFile 打开或创建上传临时文件。
// resume 为 true 且已存在大小一致的文件时保留原有内容，否则重新截断。
func openUploadTempFile(tempPath string, size int64, resume bool) error {
	if resume {
		if info, err := os.Stat(tempPath); err == nil && info.Size() == size {
			return nil
		}
	}
	return recreateUploadTempFile(tempPath, size)
}

// recreateUploadTempFile 强制重建上传临时文件，按声明大小预分配。
func recreateUploadTempFile(tempPath string, size int64) error {
	if err := os.MkdirAll(filepath.Dir(tempPath), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(tempPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if size > 0 {
		if err := file.Truncate(size); err != nil {
			_ = file.Close()
			return err
		}
	}
	return file.Close()
}

// errUploadSessionExists 表示同一 upload_id 的会话已存在且未声明续传。
var errUploadSessionExists = errors.New("上传会话已存在")

// prepareUploadSession 准备上传临时文件与分块清单。
// 既有会话必须与本次请求（大小、分块数）完全一致且显式声明续传才保留，
// 否则一律按全新上传重建，避免不同文件误用同一会话。
func prepareUploadSession(tempPath string, size int64, totalChunks int, resume bool) error {
	info, statErr := os.Stat(tempPath)
	exists := statErr == nil
	if statErr != nil && !os.IsNotExist(statErr) {
		return statErr
	}

	if exists {
		if !resume {
			return errUploadSessionExists
		}
		// 只有清单和文件都与本次请求一致时才允许续传。
		if manifest, ok := loadUploadManifest(tempPath); ok &&
			manifest.Size == size &&
			manifest.TotalChunks == totalChunks &&
			info.Size() == size {
			return nil
		}
		// 不一致：丢弃旧会话，按全新上传重建。
	}

	if err := recreateUploadTempFile(tempPath, size); err != nil {
		return err
	}
	return resetUploadSession(tempPath, size, totalChunks)
}
