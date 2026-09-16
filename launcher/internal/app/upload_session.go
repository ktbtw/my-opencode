package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"

	"launcher/internal/model"
)

// uploadManifestName 是与 payload.bin 同目录的分块清单文件名。
// 清单记录已成功写入的分块下标，用于断点续传时跳过重复分块。
const uploadManifestName = "chunks.json"

// uploadPayloadName 是上传临时数据文件名。
const uploadPayloadName = "payload.bin"

// uploadSessionRetention 是上传会话临时目录的保留时长。
// 中断后不再重试的会话否则会永久留在 .chat-codex-uploads 里，
// 而它是隐藏目录，在文件列表里看不到，只能占着磁盘。
const uploadSessionRetention = 24 * time.Hour

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
//
// 会话的 size/total_chunks 属于 create 阶段（resetUploadSession），这里只在
// 清单缺失或值不合理时用请求值补齐。历史实现无条件用分块级的值覆盖会话信息，
// 而分块请求并不一定携带会话大小，结果清单里的 size 被写成 0，
// 续传校验（manifest.Size == size）永远不成立，已写入的分块只能整份重传。
func recordUploadChunk(tempPath string, size int64, totalChunks int, chunkIndex int) error {
	manifest, ok := loadUploadManifest(tempPath)
	if !ok || manifest.Size <= 0 {
		manifest.Size = size
	}
	if manifest.TotalChunks <= 0 {
		manifest.TotalChunks = totalChunks
	}
	for _, item := range manifest.Received {
		if item == chunkIndex {
			return nil
		}
	}
	manifest.Received = append(manifest.Received, chunkIndex)
	return saveUploadManifest(tempPath, manifest)
}

// cleanupStaleUploadSessions 清理同一上传目录下已经过期的会话目录。
// 只处理 .chat-codex-uploads 下带 payload.bin 的会话目录，并跳过当前会话，
// 避免把正在上传的临时数据删掉。
func cleanupStaleUploadSessions(tempPath string) {
	sessionDir := filepath.Dir(tempPath)
	root := filepath.Dir(sessionDir)
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	current := filepath.Base(sessionDir)
	cutoff := time.Now().Add(-uploadSessionRetention)
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == current {
			continue
		}
		session := filepath.Join(root, entry.Name())
		payload, err := os.Stat(filepath.Join(session, uploadPayloadName))
		if err != nil {
			continue
		}
		// payload.bin 与 chunks.json 每个分块都会被写入，取它们与会话目录中
		// 最新的时间作为活跃时间，避免把正在上传的会话当成过期会话删掉。
		activity := payload.ModTime()
		if info, err := entry.Info(); err == nil && info.ModTime().After(activity) {
			activity = info.ModTime()
		}
		if manifest, err := os.Stat(filepath.Join(session, uploadManifestName)); err == nil &&
			manifest.ModTime().After(activity) {
			activity = manifest.ModTime()
		}
		if activity.After(cutoff) {
			continue
		}
		_ = os.RemoveAll(session)
	}
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
