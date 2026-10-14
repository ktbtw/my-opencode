package store

import (
	"database/sql"
	"errors"
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
