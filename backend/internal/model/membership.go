package model

import "strings"

// 会员等级常量。数据库存空值时按 free 处理，保证老数据兼容。
const (
	MembershipTierFree = "free"
	MembershipTierPlus = "plus"
	MembershipTierPro  = "pro"
)

const (
	// FreeUploadChunkSize 普通用户分块大小（与历史行为一致）。
	FreeUploadChunkSize int64 = 512 * 1024
	// MemberUploadChunkSize 会员分块大小。配合并发减少往返次数：
	// 分块越大，单次请求携带的有效数据越多，固定开销占比越低。
	MemberUploadChunkSize int64 = 16 * 1024 * 1024
	// UploadChunkHardLimit 绝对上限，防止异常请求撑爆内存。
	UploadChunkHardLimit int64 = 32 * 1024 * 1024
)

// UploadPolicy 描述某个会员等级可用的上传参数，由服务端下发，客户端不自行猜测。
type UploadPolicy struct {
	Tier          string `json:"tier"`
	IsMember      bool   `json:"is_member"`
	ChunkSize     int64  `json:"chunk_size"`
	MaxChunkSize  int64  `json:"max_chunk_size"`
	Concurrency   int    `json:"concurrency"`
	ResumeEnabled bool   `json:"resume_enabled"`
}

// NormalizeMembershipTier 把任意输入收敛到已知等级，未知值一律按 free 处理。
func NormalizeMembershipTier(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case MembershipTierPlus:
		return MembershipTierPlus
	case MembershipTierPro:
		return MembershipTierPro
	default:
		return MembershipTierFree
	}
}

// IsMemberTier 判断是否属于付费会员等级。
func IsMemberTier(raw string) bool {
	tier := NormalizeMembershipTier(raw)
	return tier == MembershipTierPlus || tier == MembershipTierPro
}

// UploadPolicyForTier 返回等级对应的上传策略。
//
// 注意：这里没有限速字段。历史字段 TargetBytesPerSec 已移除——它从未被任何
// 代码读取用于限速，只是管理后台的展示值，留着会让人误以为存在限速。
// 实际约束只有两项：单块上限（MaxChunkSize，超限直接拒绝）与并发数。
//
// free：512KB 分块、单并发，保持原有行为。
// plus：16MB 分块、3 并发。
// pro：16MB 分块、5 并发。
func UploadPolicyForTier(raw string) UploadPolicy {
	switch NormalizeMembershipTier(raw) {
	case MembershipTierPro:
		return UploadPolicy{
			Tier:          MembershipTierPro,
			IsMember:      true,
			ChunkSize:     MemberUploadChunkSize,
			MaxChunkSize:  MemberUploadChunkSize,
			Concurrency:   5,
			ResumeEnabled: true,
		}
	case MembershipTierPlus:
		return UploadPolicy{
			Tier:          MembershipTierPlus,
			IsMember:      true,
			ChunkSize:     MemberUploadChunkSize,
			MaxChunkSize:  MemberUploadChunkSize,
			Concurrency:   3,
			ResumeEnabled: true,
		}
	default:
		return UploadPolicy{
			Tier:          MembershipTierFree,
			IsMember:      false,
			ChunkSize:     FreeUploadChunkSize,
			MaxChunkSize:  FreeUploadChunkSize,
			Concurrency:   1,
			ResumeEnabled: true,
		}
	}
}

// MaxBase64ContentLen 返回指定原始分块大小对应的 base64 内容最大长度。
// 用于在不解码的情况下校验请求体是否超限。
func MaxBase64ContentLen(chunkSize int64) int64 {
	if chunkSize <= 0 {
		return 0
	}
	return ((chunkSize + 2) / 3) * 4
}
