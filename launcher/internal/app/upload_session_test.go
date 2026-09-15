package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestUploadSessionResumeKeepsExistingData(t *testing.T) {
	dir := t.TempDir()
	tempPath := filepath.Join(dir, "payload.bin")

	// 首次创建：声明 10 字节，分 2 块。
	if err := prepareUploadSession(tempPath, 10, 2, false); err != nil {
		t.Fatalf("创建上传会话失败: %v", err)
	}

	// 写入第 0 块并记录。
	file, err := os.OpenFile(tempPath, os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteAt([]byte("hello"), 0); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := recordUploadChunk(tempPath, 10, 2, 0); err != nil {
		t.Fatalf("记录分块失败: %v", err)
	}

	// 续传：不应截断已写入的数据。
	if err := prepareUploadSession(tempPath, 10, 2, true); err != nil {
		t.Fatalf("续传打开失败: %v", err)
	}
	data, err := os.ReadFile(tempPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data[:5]) != "hello" {
		t.Fatalf("续传后已写入的分块被清空，实际前 5 字节为 %q", string(data[:5]))
	}
	if len(data) != 10 {
		t.Fatalf("续传后文件大小应为 10，实际 %d", len(data))
	}

	status := uploadStatusFor(tempPath, "a.txt", "up_1", 10, 2)
	if len(status.ReceivedChunks) != 1 || status.ReceivedChunks[0] != 0 {
		t.Fatalf("已收分块应为 [0]，实际 %v", status.ReceivedChunks)
	}
	if status.Completed {
		t.Fatal("只收到 1/2 分块时不应标记为完成")
	}
	if !status.Resumable {
		t.Fatal("存在清单时应标记为可续传")
	}

	// 补齐第 1 块后应视为完成。
	file, err = os.OpenFile(tempPath, os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteAt([]byte("world"), 5); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := recordUploadChunk(tempPath, 10, 2, 1); err != nil {
		t.Fatal(err)
	}
	status = uploadStatusFor(tempPath, "a.txt", "up_1", 10, 2)
	if !status.Completed {
		t.Fatalf("收齐分块后应标记为完成，实际 %+v", status)
	}
	if status.ReceivedBytes != 10 {
		t.Fatalf("已收字节应为 10，实际 %d", status.ReceivedBytes)
	}
}

func TestUploadSessionWithoutResumeRejectsDuplicate(t *testing.T) {
	dir := t.TempDir()
	tempPath := filepath.Join(dir, "payload.bin")

	if err := prepareUploadSession(tempPath, 10, 2, false); err != nil {
		t.Fatalf("首次创建失败: %v", err)
	}
	// 未声明续传时，重复创建必须被拒绝，避免两个上传互相覆盖。
	if err := prepareUploadSession(tempPath, 10, 2, false); !errors.Is(err, errUploadSessionExists) {
		t.Fatalf("重复创建应返回上传会话已存在，实际 %v", err)
	}
}

func TestUploadSessionResumeMismatchRebuilds(t *testing.T) {
	dir := t.TempDir()
	tempPath := filepath.Join(dir, "payload.bin")

	if err := prepareUploadSession(tempPath, 10, 2, false); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(tempPath, os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteAt([]byte("hello"), 0); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := recordUploadChunk(tempPath, 10, 2, 0); err != nil {
		t.Fatal(err)
	}

	// 声明大小与既有会话不一致时，即使要求续传也必须重建，防止拼接出错文件。
	if err := prepareUploadSession(tempPath, 20, 2, true); err != nil {
		t.Fatalf("大小不一致时应重建会话，实际报错 %v", err)
	}
	data, err := os.ReadFile(tempPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 20 {
		t.Fatalf("重建后大小应为 20，实际 %d", len(data))
	}
	for i, b := range data {
		if b != 0 {
			t.Fatalf("重建后应清空旧数据，偏移 %d 仍为 %d", i, b)
		}
	}
	status := uploadStatusFor(tempPath, "a.txt", "up_1", 20, 2)
	if len(status.ReceivedChunks) != 0 {
		t.Fatalf("重建后不应保留已收分块，实际 %v", status.ReceivedChunks)
	}
}

func TestUploadSessionCreateWithoutResumeTruncates(t *testing.T) {
	dir := t.TempDir()
	tempPath := filepath.Join(dir, "payload.bin")

	if err := prepareUploadSession(tempPath, 10, 2, false); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(tempPath, os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteAt([]byte("hello"), 0); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	// 全新上传（不同 upload_id 目录）应清空旧数据。
	otherPath := filepath.Join(dir, "other", "payload.bin")
	if err := prepareUploadSession(otherPath, 10, 2, false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(otherPath)
	if err != nil {
		t.Fatal(err)
	}
	for i, b := range data {
		if b != 0 {
			t.Fatalf("全新上传应清空旧数据，偏移 %d 仍为 %d", i, b)
		}
	}
	status := uploadStatusFor(otherPath, "a.txt", "up_2", 10, 2)
	if len(status.ReceivedChunks) != 0 {
		t.Fatalf("全新上传不应有已收分块，实际 %v", status.ReceivedChunks)
	}
}

func TestRecordUploadChunkIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	tempPath := filepath.Join(dir, "payload.bin")
	if err := prepareUploadSession(tempPath, 6, 3, false); err != nil {
		t.Fatal(err)
	}
	// 同一分块重试多次不应产生重复记录。
	for i := 0; i < 3; i++ {
		if err := recordUploadChunk(tempPath, 6, 3, 1); err != nil {
			t.Fatal(err)
		}
	}
	status := uploadStatusFor(tempPath, "a.txt", "up_3", 6, 3)
	if len(status.ReceivedChunks) != 1 {
		t.Fatalf("重复记录同一分块应只保留一条，实际 %v", status.ReceivedChunks)
	}
}

func TestUploadStatusWithoutSessionIsEmpty(t *testing.T) {
	dir := t.TempDir()
	tempPath := filepath.Join(dir, "missing", "payload.bin")
	status := uploadStatusFor(tempPath, "a.txt", "up_x", 100, 5)
	if len(status.ReceivedChunks) != 0 {
		t.Fatalf("无会话时不应返回分块，实际 %v", status.ReceivedChunks)
	}
	if status.Completed || status.Resumable {
		t.Fatalf("无会话时不应标记完成或可续传，实际 %+v", status)
	}
}
