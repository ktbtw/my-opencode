package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"relay-server/internal/model"
)

// operator_gateway_keys：平台侧保存每个用户的「期望会员状态 + 网关虚拟钥匙」。
// 见 docs/反代会员分级联动方案.md（v2：零人工，兑换码为唯一入口）。
var gatewayKeySchemaStatements = []string{
	`CREATE TABLE IF NOT EXISTS operator_gateway_keys (
  operator_id BIGINT NOT NULL,
  desired_tier VARCHAR(16) NOT NULL,
  state VARCHAR(16) NOT NULL,
  key_id VARCHAR(64) NULL,
  key_last4 CHAR(4) NULL,
  key_cipher TEXT NULL,
  models_json JSON NULL,
  limits_json JSON NULL,
  provider VARCHAR(64) NULL,
  device_base_url VARCHAR(255) NULL,
  machines_json JSON NULL,
  last_error TEXT NULL,
  updated_at DATETIME(6) NOT NULL,
  PRIMARY KEY (operator_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`,
}

func (m *MySQLArchive) UpsertOperatorGatewayKey(ctx context.Context, record model.OperatorGatewayKey) error {
	if m == nil || m.db == nil || record.OperatorID <= 0 {
		return errors.New("invalid operator gateway key")
	}
	models, err := json.Marshal(record.Models)
	if err != nil {
		return err
	}
	limits, err := json.Marshal(record.Limits)
	if err != nil {
		return err
	}
	machines, err := json.Marshal(record.Machines)
	if err != nil {
		return err
	}
	updatedAt := time.Now().UTC()
	_, err = m.db.ExecContext(ctx, `INSERT INTO operator_gateway_keys
  (operator_id, desired_tier, state, key_id, key_last4, key_cipher, models_json, limits_json, provider, device_base_url, machines_json, last_error, updated_at)
  VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
  ON DUPLICATE KEY UPDATE
    desired_tier = VALUES(desired_tier), state = VALUES(state), key_id = VALUES(key_id),
    key_last4 = VALUES(key_last4), key_cipher = VALUES(key_cipher), models_json = VALUES(models_json),
    limits_json = VALUES(limits_json), provider = VALUES(provider), device_base_url = VALUES(device_base_url), machines_json = VALUES(machines_json),
    last_error = VALUES(last_error), updated_at = VALUES(updated_at)`,
		record.OperatorID, record.DesiredTier, record.State, nullIfEmpty(record.KeyID), nullIfEmpty(record.KeyLast4),
		nullIfEmpty(record.KeyCipher), models, limits, nullIfEmpty(record.Provider), nullIfEmpty(record.DeviceBaseURL), machines, nullIfEmpty(record.LastError), updatedAt)
	return err
}

func (m *MySQLArchive) GetOperatorGatewayKey(ctx context.Context, operatorID int64) (*model.OperatorGatewayKey, error) {
	if m == nil || m.db == nil || operatorID <= 0 {
		return nil, nil
	}
	row := m.db.QueryRowContext(ctx, `SELECT operator_id, desired_tier, state, key_id, key_last4, key_cipher,
  models_json, limits_json, provider, device_base_url, machines_json, last_error, updated_at
  FROM operator_gateway_keys WHERE operator_id = ?`, operatorID)
	record, err := scanOperatorGatewayKey(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (m *MySQLArchive) ListOperatorGatewayKeys(ctx context.Context, limit int) ([]model.OperatorGatewayKey, error) {
	if m == nil || m.db == nil {
		return nil, nil
	}
	if limit <= 0 || limit > 2000 {
		limit = 200
	}
	rows, err := m.db.QueryContext(ctx, `SELECT operator_id, desired_tier, state, key_id, key_last4, key_cipher,
  models_json, limits_json, provider, device_base_url, machines_json, last_error, updated_at
  FROM operator_gateway_keys ORDER BY updated_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.OperatorGatewayKey
	for rows.Next() {
		record, err := scanOperatorGatewayKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanOperatorGatewayKey(row rowScanner) (model.OperatorGatewayKey, error) {
	var (
		record    model.OperatorGatewayKey
		keyID     sql.NullString
		keyLast4  sql.NullString
		keyCipher sql.NullString
		modelsRaw []byte
		limitsRaw []byte
		provider  sql.NullString
		deviceURL sql.NullString
		machines  []byte
		lastError sql.NullString
		updatedAt time.Time
	)
	if err := row.Scan(&record.OperatorID, &record.DesiredTier, &record.State, &keyID, &keyLast4, &keyCipher,
		&modelsRaw, &limitsRaw, &provider, &deviceURL, &machines, &lastError, &updatedAt); err != nil {
		return model.OperatorGatewayKey{}, err
	}
	record.KeyID = keyID.String
	record.KeyLast4 = keyLast4.String
	record.KeyCipher = keyCipher.String
	record.Provider = provider.String
	record.DeviceBaseURL = deviceURL.String
	record.LastError = lastError.String
	record.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	if len(modelsRaw) > 0 {
		_ = json.Unmarshal(modelsRaw, &record.Models)
	}
	if len(limitsRaw) > 0 {
		_ = json.Unmarshal(limitsRaw, &record.Limits)
	}
	if len(machines) > 0 {
		_ = json.Unmarshal(machines, &record.Machines)
	}
	return record, nil
}

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}
