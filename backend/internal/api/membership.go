package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"relay-server/internal/model"
)

// membershipTierOption 是管理后台可选的会员等级及其上传权益说明。
type membershipTierOption struct {
	Tier        string `json:"tier"`
	Label       string `json:"label"`
	ChunkSize   int64  `json:"chunk_size"`
	Concurrency int    `json:"concurrency"`
	Description string `json:"description"`
}

// membershipTierOptions 供管理后台渲染等级下拉框，与后端策略保持一致。
func membershipTierOptions() []membershipTierOption {
	tiers := []struct {
		tier  string
		label string
		desc  string
	}{
		{model.MembershipTierFree, "普通用户", "512 KB 分块、单线程上传"},
		{model.MembershipTierPlus, "Plus 会员", "5 MB 分块、3 并发，约 5 MB/s"},
		{model.MembershipTierPro, "Pro 会员", "5 MB 分块、5 并发，约 5 MB/s"},
	}
	options := make([]membershipTierOption, 0, len(tiers))
	for _, item := range tiers {
		policy := model.UploadPolicyForTier(item.tier)
		options = append(options, membershipTierOption{
			Tier:        policy.Tier,
			Label:       item.label,
			ChunkSize:   policy.ChunkSize,
			Concurrency: policy.Concurrency,
			Description: item.desc,
		})
	}
	return options
}

// ListAdminOperators 返回全部用户及其会员等级。
func (a *API) ListAdminOperators(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	operators, err := a.store.ListOperators()
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	items := make([]map[string]any, 0, len(operators))
	for _, operator := range operators {
		tier := model.NormalizeMembershipTier(operator.MembershipTier)
		policy := model.UploadPolicyForTier(tier)
		items = append(items, map[string]any{
			"id":              operator.ID,
			"operator_uid":    operator.OperatorUID,
			"username":        operator.Username,
			"name":            operator.Name,
			"email":           operator.Email,
			"membership_tier": tier,
			"upload_policy":   policy,
		})
	}
	write(w, http.StatusOK, map[string]any{
		"items":   items,
		"options": membershipTierOptions(),
	})
}

// SetOperatorMembership 更新指定用户的会员等级。
func (a *API) SetOperatorMembership(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	rawID := strings.TrimSpace(chi.URLParam(r, "id"))
	operatorID, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || operatorID <= 0 {
		write(w, http.StatusBadRequest, map[string]string{"error": "用户 ID 不合法"})
		return
	}
	var req struct {
		Tier string `json:"membership_tier"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	tier := strings.ToLower(strings.TrimSpace(req.Tier))
	if tier != model.MembershipTierFree && tier != model.MembershipTierPlus && tier != model.MembershipTierPro {
		write(w, http.StatusBadRequest, map[string]string{"error": "会员等级不合法"})
		return
	}
	if err := a.store.SetOperatorMembershipTier(operatorID, tier); err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	policy := model.UploadPolicyForTier(tier)
	write(w, http.StatusOK, map[string]any{
		"operator_id":     operatorID,
		"membership_tier": policy.Tier,
		"upload_policy":   policy,
	})
}
