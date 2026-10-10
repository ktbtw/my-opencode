package relay

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
)

// 二进制帧格式（用于传输分块内容，避免 base64 的 33% 膨胀）：
//
//	[4B magic "CCB1"][4B headerLen (big-endian)][headerLen 字节 JSON 头][原始字节]
//
// 头部是完整的 envelope（含 type/request_id/payload），其中不含任何大字段；
// 原始字节放在帧尾，接收方按 header_len 切分即可，无需再次解析大字段。
//
// 选择自描述帧而非「纯字节 + 独立元数据消息」的原因：后者需要维护
// 请求与数据的配对状态，断线或乱序时会错配；自描述帧无状态、可独立校验。
const (
	binaryFrameMagic  = "CCB1"
	binaryFrameHeader = 8 // 4B magic + 4B headerLen
)

// binaryFrameCapability 是设备声明支持二进制分块传输的能力标识。
// 后端据此决定下发二进制帧还是 base64 文本，未声明的设备保持旧协议。
const binaryFrameCapability = "device_binary_chunk_v1"

var errInvalidBinaryFrame = errors.New("无效的二进制帧")

// encodeBinaryFrame 把 envelope 与原始字节打包成一个二进制帧。
func encodeBinaryFrame(env envelope, payload []byte) ([]byte, error) {
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

// decodeBinaryFrame 拆出 envelope 头与原始字节。
// 返回 ok=false 表示这不是二进制帧（调用方应回退到 JSON 文本解析）。
func decodeBinaryFrame(frame []byte) (envelope, []byte, bool) {
	if len(frame) < binaryFrameHeader || string(frame[0:4]) != binaryFrameMagic {
		return envelope{}, nil, false
	}
	headerLen := int(binary.BigEndian.Uint32(frame[4:8]))
	if headerLen <= 0 || binaryFrameHeader+headerLen > len(frame) {
		return envelope{}, nil, false
	}
	var env envelope
	if err := json.Unmarshal(frame[binaryFrameHeader:binaryFrameHeader+headerLen], &env); err != nil {
		return envelope{}, nil, false
	}
	return env, frame[binaryFrameHeader+headerLen:], true
}
