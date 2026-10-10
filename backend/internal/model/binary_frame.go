package model

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
)

// 二进制帧格式（与 launcher/internal/relay/binary_frame.go 保持一致）：
//
//	[4B magic "CCB1"][4B headerLen (big-endian)][headerLen 字节 JSON 头][原始字节]
//
// 头部是完整的 Envelope，其中不含大字段；原始字节放在帧尾，
// 接收方按 header_len 切分即可。自描述帧无状态，断线/乱序不会错配。
const (
	binaryFrameMagic  = "CCB1"
	binaryFrameHeader = 8 // 4B magic + 4B headerLen
)

// BinaryChunkCapability 是设备声明支持二进制分块传输的能力标识。
const BinaryChunkCapability = "device_binary_chunk_v1"

var errInvalidBinaryFrame = errors.New("无效的二进制帧")

// EncodeBinaryFrame 把 Envelope 与原始字节打包成二进制帧。
func EncodeBinaryFrame(env Envelope, payload []byte) ([]byte, error) {
	header, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("序列化二进制帧头失败: %w", err)
	}
	frame := make([]byte, binaryFrameHeader+len(header)+len(payload))
	copy(frame[0:4], binaryFrameMagic)
	binary.BigEndian.PutUint32(frame[4:8], uint32(len(header)))
	copy(frame[binaryFrameHeader:], header)
	copy(frame[binaryFrameHeader+len(header):], payload)
	return frame, nil
}

// DecodeBinaryFrame 拆出 Envelope 头与原始字节。
// 返回 ok=false 表示这不是二进制帧（调用方应回退到 JSON 文本解析）。
func DecodeBinaryFrame(frame []byte) (Envelope, []byte, bool) {
	if len(frame) < binaryFrameHeader || string(frame[0:4]) != binaryFrameMagic {
		return Envelope{}, nil, false
	}
	headerLen := int(binary.BigEndian.Uint32(frame[4:8]))
	if headerLen <= 0 || binaryFrameHeader+headerLen > len(frame) {
		return Envelope{}, nil, false
	}
	var env Envelope
	if err := json.Unmarshal(frame[binaryFrameHeader:binaryFrameHeader+headerLen], &env); err != nil {
		return Envelope{}, nil, false
	}
	return env, frame[binaryFrameHeader+headerLen:], true
}
