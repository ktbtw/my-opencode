package app

import (
	"encoding/json"
	"log"
	"math"
	"os"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/process"
)

// uploadDiagEnabled 控制上传耗时诊断日志。上传路径可能每个分块产生一行，
// 默认关闭避免刷屏；排查上传性能时设置 CHAT_CODEX_UPLOAD_DIAG=1 打开。
func uploadDiagEnabled() bool {
	value := strings.TrimSpace(os.Getenv("CHAT_CODEX_UPLOAD_DIAG"))
	return value != "" && value != "0" && !strings.EqualFold(value, "false")
}

// processCPUSeconds 返回当前进程累计占用的 CPU 秒数（用户态 + 内核态）。
// 两次采样之差即该区间内进程实际消耗的 CPU 时间，用于判断上传是否 CPU 受限。
// 平台不支持时返回 false，调用方跳过该字段。
func processCPUSeconds() (float64, bool) {
	proc, err := process.NewProcess(int32(os.Getpid()))
	if err != nil {
		return 0, false
	}
	times, err := proc.Times()
	if err != nil {
		return 0, false
	}
	return times.User + times.System, true
}

// uploadChunkDiag 记录单个上传分块在设备侧的耗时分解。
//
// 关键指标：
//   - lock_wait_ms：等待 directoryFilesMu 的时间。接近 total_ms 说明处理被完全串行化，
//     客户端并发发块没有收益。
//   - cpu_ms：本次处理消耗的进程 CPU。占比高说明瓶颈在 base64 解码或哈希计算。
//   - start_ms/end_ms：绝对时间戳，用来看多个分块的执行区间是否重叠（是否真并行）。
type uploadChunkDiag struct {
	uploadID      string
	chunkIndex    int
	offset        int64
	payloadBytes  int // base64 长度，即实际经过两条链路的字节数
	decodedBytes  int
	startAt       time.Time
	startCPU      float64
	hasCPU        bool
	lockWait      time.Duration
	decodeTime    time.Duration
	sha256Time    time.Duration
	writeTime     time.Duration
	manifestTime  time.Duration
	dirResolveTim time.Duration
}

func newUploadChunkDiag(reqDirectoryUploadID string, chunkIndex int, offset int64, payloadBytes int) *uploadChunkDiag {
	if !uploadDiagEnabled() {
		return nil
	}
	cpu, ok := processCPUSeconds()
	return &uploadChunkDiag{
		uploadID:     reqDirectoryUploadID,
		chunkIndex:   chunkIndex,
		offset:       offset,
		payloadBytes: payloadBytes,
		startAt:      time.Now(),
		startCPU:     cpu,
		hasCPU:       ok,
	}
}

// finish 汇总并输出一行结构化日志。errText 非空表示该分块处理失败。
func (d *uploadChunkDiag) finish(errText string) {
	if d == nil {
		return
	}
	endAt := time.Now()
	payload := map[string]any{
		"component":     "launcher",
		"stage":         "upload_chunk",
		"upload_id":     d.uploadID,
		"chunk_index":   d.chunkIndex,
		"offset":        d.offset,
		"payload_bytes": d.payloadBytes,
		"decoded_bytes": d.decodedBytes,
		"total_ms":      endAt.Sub(d.startAt).Milliseconds(),
		"lock_wait_ms":  d.lockWait.Milliseconds(),
		"dir_path_ms":   d.dirResolveTim.Milliseconds(),
		"decode_ms":     d.decodeTime.Milliseconds(),
		"sha256_ms":     d.sha256Time.Milliseconds(),
		"write_ms":      d.writeTime.Milliseconds(),
		"manifest_ms":   d.manifestTime.Milliseconds(),
		"start_ms":      d.startAt.UnixMilli(),
		"end_ms":        endAt.UnixMilli(),
	}
	if d.hasCPU {
		if cpu, ok := processCPUSeconds(); ok {
			payload["cpu_ms"] = int64((cpu - d.startCPU) * 1000)
		}
	}
	if errText != "" {
		payload["error"] = errText
	}
	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[latency][launcher] stage=upload_chunk upload_id=%s chunk=%d", d.uploadID, d.chunkIndex)
		return
	}
	log.Printf("[latency][launcher] %s", string(data))
}

// uploadSummaryDiag 记录一次上传的整体完成情况（设备侧视角）。
type uploadSummaryDiag struct {
	uploadID     string
	totalBytes   int64
	totalChunks  int
	startAt      time.Time
	sha256Time   time.Duration
	publishTime  time.Duration
	receivedSeen int
}

func newUploadSummaryDiag(uploadID string, totalBytes int64, totalChunks int, receivedSeen int) *uploadSummaryDiag {
	if !uploadDiagEnabled() {
		return nil
	}
	return &uploadSummaryDiag{
		uploadID:     uploadID,
		totalBytes:   totalBytes,
		totalChunks:  totalChunks,
		startAt:      time.Now(),
		receivedSeen: receivedSeen,
	}
}

func (d *uploadSummaryDiag) finish(errText string) {
	if d == nil {
		return
	}
	wall := time.Since(d.startAt)
	payload := map[string]any{
		"component":     "launcher",
		"stage":         "upload_complete",
		"upload_id":     d.uploadID,
		"total_bytes":   d.totalBytes,
		"total_chunks":  d.totalChunks,
		"received_seen": d.receivedSeen,
		"wall_ms":       wall.Milliseconds(),
		"sha256_ms":     d.sha256Time.Milliseconds(),
		"publish_ms":    d.publishTime.Milliseconds(),
	}
	if wall > 0 {
		payload["verify_mbps"] = math.Round(float64(d.totalBytes)/1024/1024/wall.Seconds()*100) / 100
	}
	if errText != "" {
		payload["error"] = errText
	}
	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[latency][launcher] stage=upload_complete upload_id=%s", d.uploadID)
		return
	}
	log.Printf("[latency][launcher] %s", string(data))
}
