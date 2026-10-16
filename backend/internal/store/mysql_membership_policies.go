package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"relay-server/internal/model"
)

// membership_model_policies：控制台里按等级配置的「模型包 + 别名 + 额度」。
// 保存后平台会把别名同步到网关，并对该等级的会员自动重新下发。
var membershipPolicySchemaStatements = []string{
	`CREATE TABLE IF NOT EXISTS membership_model_policies (
  tier VARCHAR(16) NOT NULL,
  provider_id VARCHAR(64) NOT NULL,
  display_name VARCHAR(64) NULL,
  entries_json JSON NOT NULL,
  limits_json JSON NOT NULL,
  quota_mode VARCHAR(8) NULL,
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  updated_at DATETIME(6) NOT NULL,
  PRIMARY KEY (tier)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`,
}

func (m *MySQLArchive) UpsertMembershipTierPolicy(ctx context.Context, policy model.MembershipTierPolicy) error {
	if m == nil || m.db == nil {
		return errors.New("invalid membership policy store")
	}
	policy = policy.Normalize()
	if policy.Tier == "" || policy.Tier == model.MembershipTierFree && len(policy.Entries) == 0 {
		// 允许把 free 配成空（等于不发放），但不允许空等级
		if policy.Tier == "" {
			return errors.New("会员等级不能为空")
		}
	}
	entries, err := json.Marshal(policy.Entries)
	if err != nil {
		return err
	}
	limits, err := json.Marshal(policy.Limits)
	if err != nil {
		return err
	}
	enabled := 0
	if policy.Enabled {
		enabled = 1
	}
	_, err = m.db.ExecContext(ctx, `INSERT INTO membership_model_policies
  (tier, provider_id, display_name, entries_json, limits_json, quota_mode, enabled, updated_at)
  VALUES (?, ?, ?, ?, ?, ?, ?, ?)
  ON DUPLICATE KEY UPDATE
    provider_id = VALUES(provider_id), display_name = VALUES(display_name),
    entries_json = VALUES(entries_json), limits_json = VALUES(limits_json),
    quota_mode = VALUES(quota_mode), enabled = VALUES(enabled), updated_at = VALUES(updated_at)`,
		policy.Tier, policy.ProviderID, nullIfEmpty(policy.DisplayName), entries, limits,
		nullIfEmpty(policy.QuotaMode), enabled, time.Now().UTC())
	return err
}

func (m *MySQLArchive) ListMembershipTierPolicies(ctx context.Context) ([]model.MembershipTierPolicy, error) {
	if m == nil || m.db == nil {
		return nil, nil
	}
	rows, err := m.db.QueryContext(ctx, `SELECT tier, provider_id, display_name, entries_json, limits_json, quota_mode, enabled, updated_at
  FROM membership_model_policies ORDER BY tier`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.MembershipTierPolicy
	for rows.Next() {
		var (
			policy      model.MembershipTierPolicy
			displayName sql.NullString
			entriesRaw  []byte
			limitsRaw   []byte
			quotaMode   sql.NullString
			enabled     int
			updatedAt   time.Time
		)
		if err := rows.Scan(&policy.Tier, &policy.ProviderID, &displayName, &entriesRaw, &limitsRaw, &quotaMode, &enabled, &updatedAt); err != nil {
			return nil, err
		}
		policy.DisplayName = displayName.String
		policy.QuotaMode = quotaMode.String
		policy.Enabled = enabled == 1
		policy.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
		if len(entriesRaw) > 0 {
			_ = json.Unmarshal(entriesRaw, &policy.Entries)
		}
		if len(limitsRaw) > 0 {
			_ = json.Unmarshal(limitsRaw, &policy.Limits)
		}
		out = append(out, policy)
	}
	return out, rows.Err()
}
