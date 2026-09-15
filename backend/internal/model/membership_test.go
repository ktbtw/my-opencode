package model

import "testing"

func TestNormalizeMembershipTier(t *testing.T) {
	cases := map[string]string{
		"":        MembershipTierFree,
		"free":    MembershipTierFree,
		"PLUS":    MembershipTierPlus,
		" pro ":   MembershipTierPro,
		"unknown": MembershipTierFree,
		"vip":     MembershipTierFree,
	}
	for input, want := range cases {
		if got := NormalizeMembershipTier(input); got != want {
			t.Errorf("NormalizeMembershipTier(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestUploadPolicyForTier(t *testing.T) {
	free := UploadPolicyForTier("")
	if free.IsMember {
		t.Fatal("空等级不应被识别为会员")
	}
	if free.ChunkSize != FreeUploadChunkSize {
		t.Fatalf("普通用户分块大小应为 %d，实际 %d", FreeUploadChunkSize, free.ChunkSize)
	}
	if free.Concurrency != 1 {
		t.Fatalf("普通用户并发应为 1，实际 %d", free.Concurrency)
	}

	plus := UploadPolicyForTier(MembershipTierPlus)
	if !plus.IsMember {
		t.Fatal("plus 应被识别为会员")
	}
	if plus.ChunkSize != MemberUploadChunkSize {
		t.Fatalf("会员分块大小应为 %d，实际 %d", MemberUploadChunkSize, plus.ChunkSize)
	}
	if plus.Concurrency < 2 {
		t.Fatalf("会员并发应大于 1，实际 %d", plus.Concurrency)
	}
	if plus.TargetBytesPerSec != 5*1024*1024 {
		t.Fatalf("会员目标速率应为 5MB/s，实际 %d", plus.TargetBytesPerSec)
	}

	pro := UploadPolicyForTier(MembershipTierPro)
	if !pro.IsMember || pro.Concurrency <= plus.Concurrency {
		t.Fatalf("pro 并发应高于 plus：pro=%d plus=%d", pro.Concurrency, plus.Concurrency)
	}
}

func TestMaxBase64ContentLen(t *testing.T) {
	// 512KB 原始数据 base64 后长度约为 4/3 倍，且必须是 4 的倍数。
	got := MaxBase64ContentLen(FreeUploadChunkSize)
	want := int64(699052)
	if got != want {
		t.Fatalf("MaxBase64ContentLen(%d) = %d, want %d", FreeUploadChunkSize, got, want)
	}
	if got%4 != 0 {
		t.Fatalf("base64 长度必须是 4 的倍数，实际 %d", got)
	}
	if MaxBase64ContentLen(0) != 0 {
		t.Fatal("零分块应返回 0")
	}
}

func TestIsMemberTier(t *testing.T) {
	if IsMemberTier("") || IsMemberTier("free") {
		t.Fatal("普通等级不应被识别为会员")
	}
	if !IsMemberTier(MembershipTierPlus) || !IsMemberTier(MembershipTierPro) {
		t.Fatal("付费等级应被识别为会员")
	}
}
