package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

// PromptTemplate 是控制台保存的平台默认提示词槽位内容。
type PromptTemplate struct {
	SlotID    string    `json:"slot_id"`
	Content   string    `json:"content"`
	Revision  int64     `json:"revision"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ListPromptTemplates 返回全部平台默认槽位（slot_id → 内容）。
func (m *MySQLArchive) ListPromptTemplates() (map[string]PromptTemplate, error) {
	rows, err := m.db.Query(`SELECT slot_id, content, revision, updated_at FROM prompt_templates`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]PromptTemplate{}
	for rows.Next() {
		var item PromptTemplate
		if err := rows.Scan(&item.SlotID, &item.Content, &item.Revision, &item.UpdatedAt); err != nil {
			return nil, err
		}
		out[item.SlotID] = item
	}
	return out, rows.Err()
}

// UpsertPromptTemplate 保存平台默认值，revision 每次自增。
func (m *MySQLArchive) UpsertPromptTemplate(slotID, content string) (PromptTemplate, error) {
	if _, err := m.db.Exec(`
INSERT INTO prompt_templates (slot_id, content, revision) VALUES (?, ?, 1)
ON DUPLICATE KEY UPDATE content = VALUES(content), revision = revision + 1
`, slotID, content); err != nil {
		return PromptTemplate{}, err
	}
	var item PromptTemplate
	err := m.db.QueryRow(`SELECT slot_id, content, revision, updated_at FROM prompt_templates WHERE slot_id = ?`, slotID).
		Scan(&item.SlotID, &item.Content, &item.Revision, &item.UpdatedAt)
	if err != nil {
		return PromptTemplate{}, err
	}
	return item, nil
}

// DeletePromptTemplate 删除平台默认值，槽位回落到内置默认。
func (m *MySQLArchive) DeletePromptTemplate(slotID string) error {
	_, err := m.db.Exec(`DELETE FROM prompt_templates WHERE slot_id = ?`, slotID)
	return err
}

// PromptTemplateAppliedRevision 返回某设备 Agent 最近一次成功下发的提示词 revision。
func (m *MySQLArchive) PromptTemplateAppliedRevision(operatorID int64, agentID string) (string, error) {
	var revision string
	err := m.db.QueryRow(
		`SELECT revision FROM device_prompt_revisions WHERE operator_id = ? AND agent_id = ?`,
		operatorID, agentID,
	).Scan(&revision)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return revision, nil
}

// SetPromptTemplateAppliedRevision 记录某设备 Agent 最近一次成功下发的提示词 revision。
func (m *MySQLArchive) SetPromptTemplateAppliedRevision(operatorID int64, agentID, revision string) error {
	_, err := m.db.Exec(`
INSERT INTO device_prompt_revisions (operator_id, agent_id, revision) VALUES (?, ?, ?)
ON DUPLICATE KEY UPDATE revision = VALUES(revision)
`, operatorID, agentID, revision)
	return err
}

// DeleteDeviceSetting 删除单个设备设置项（用于清除设备 Agent 级提示词覆盖）。
func (m *MySQLArchive) DeleteDeviceSetting(operatorID int64, agentID, key string) error {
	_, err := m.db.Exec(
		`DELETE FROM device_settings WHERE operator_id = ? AND agent_id = ? AND setting_key = ?`,
		operatorID, agentID, key,
	)
	return err
}

// PromptTemplateDeviceOverrideCounts 统计各槽位在设备 Agent 层被覆盖的数量，供控制台展示。
func (m *MySQLArchive) PromptTemplateDeviceOverrideCounts() (map[string]int, error) {
	rows, err := m.db.Query(`
SELECT setting_key, COUNT(*) FROM device_settings
WHERE setting_key LIKE 'prompt_template.%' AND TRIM(setting_value) <> ''
GROUP BY setting_key
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var key string
		var count int
		if err := rows.Scan(&key, &count); err != nil {
			return nil, err
		}
		out[strings.TrimPrefix(key, "prompt_template.")] = count
	}
	return out, rows.Err()
}

// PromptTemplatesRevision 返回所有平台默认槽位的 revision 之和，用于判断设备是否需要更新。
func (m *MySQLArchive) PromptTemplatesRevision() (int64, error) {
	var total sql.NullInt64
	if err := m.db.QueryRow(`SELECT SUM(revision) FROM prompt_templates`).Scan(&total); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}
	return total.Int64, nil
}
