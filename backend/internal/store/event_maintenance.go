package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"relay-server/internal/model"
)

// ArchiveToolOutputLimit 是归档时单个工具输出的上限（32KB）。
// 实时通道仍然推送全文，App 在线时从 Agent 本地历史读取完整内容，
// 只有数据库副本截断，避免单条 tool_updated 动辄几 MB。
const ArchiveToolOutputLimit = 32 * 1024

// 归档时单个工具事件的整体上限（工具输出截断后仍超限时，裁剪附件内容）。
const archiveToolPayloadLimit = 256 * 1024

// 归档时单个附件文本的上限。
const archiveAttachmentTextLimit = 4 * 1024

// capArchivedToolPayload 在工具输出截断后仍超过整体上限时，裁剪附件内容
// （附件多为 base64 图片/文件，单条可达数 MB）。只保留元信息，实时事件不受影响。
func capArchivedToolPayload(event *model.Event) {
	if event == nil || event.Type != "tool_updated" || event.Tool == nil {
		return
	}
	encoded, err := json.Marshal(event.Tool)
	if err != nil || len(encoded) <= archiveToolPayloadLimit {
		return
	}
	for index := range event.Tool.Attachments {
		part := &event.Tool.Attachments[index]
		if len(part.Text) > archiveAttachmentTextLimit {
			part.Text = truncateUTF8(part.Text, archiveAttachmentTextLimit) + "（附件内容已归档）"
		}
		if part.Source != nil {
			part.Source = nil
			if strings.TrimSpace(part.Description) == "" {
				part.Description = "附件内容已归档"
			}
		}
	}
}

// truncateArchivedToolOutput 在归档前截断超长工具输出（只改副本，不影响实时事件）。
func truncateArchivedToolOutput(event *model.Event) {
	if event == nil || event.Tool == nil || event.Type != "tool_updated" {
		return
	}
	if len(event.Tool.Output) <= ArchiveToolOutputLimit {
		capArchivedToolPayload(event)
		return
	}
	event.Tool.Output = truncateUTF8(event.Tool.Output, ArchiveToolOutputLimit)
	event.Tool.OutputTruncated = true
	capArchivedToolPayload(event)
}

// truncateUTF8 按字节截断但保证不切断多字节字符。
func truncateUTF8(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut] + "\n…（输出已截断）"
}

// CompactDeltaEvents 合并同一次回复里连续的 delta（同一任务、同一 field，中间没有其它事件）：
// 内容按 id 顺序拼接并沿用第一条的 id。App 侧本来就是"相邻文本自动合并成一个文本块"，
// 所以重建结果完全等价，但行数能从千万级降到几十万级。
// 只处理"最近一条事件也早于 olderThan"的任务，不动正在流式输出的会话。
func (m *MySQLArchive) CompactDeltaEvents(ctx context.Context, olderThan time.Time, taskLimit int) (int, error) {
	if taskLimit <= 0 {
		taskLimit = 200
	}
	rows, err := m.db.QueryContext(ctx, `
SELECT task_id
FROM task_events
WHERE sent_at < ?
GROUP BY task_id
HAVING SUM(event_type = 'delta') > 50
ORDER BY MAX(sent_at) ASC
LIMIT ?`, olderThan, taskLimit)
	if err != nil {
		return 0, err
	}
	var taskIDs []string
	for rows.Next() {
		var taskID string
		if err := rows.Scan(&taskID); err != nil {
			rows.Close()
			return 0, err
		}
		taskIDs = append(taskIDs, taskID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	merged := 0
	for _, taskID := range taskIDs {
		count, err := m.compactTaskDeltas(ctx, taskID, olderThan)
		if err != nil {
			return merged, err
		}
		merged += count
	}
	return merged, nil
}

func (m *MySQLArchive) compactTaskDeltas(ctx context.Context, taskID string, olderThan time.Time) (int, error) {
	rows, err := m.db.QueryContext(ctx,
		`SELECT id, event_type, sent_at, payload_json FROM task_events WHERE task_id = ? ORDER BY sent_at ASC, id ASC`,
		taskID,
	)
	if err != nil {
		return 0, err
	}
	type row struct {
		id      int64
		typ     string
		sentAt  time.Time
		payload string
	}
	var items []row
	for rows.Next() {
		var item row
		var payload sql.NullString
		if err := rows.Scan(&item.id, &item.typ, &item.sentAt, &payload); err != nil {
			rows.Close()
			return 0, err
		}
		item.payload = payload.String
		items = append(items, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	type run struct {
		firstID int64
		payload string
		field   string
		content strings.Builder
		ids     []int64
	}
	var current *run
	merged := 0
	flush := func() error {
		if current == nil {
			return nil
		}
		if len(current.ids) <= 1 {
			current = nil
			return nil
		}
		// 用第一条的 payload 做底，替换 content
		var event model.Event
		if err := json.Unmarshal([]byte(current.payload), &event); err != nil {
			current = nil
			return nil
		}
		event.Content = current.content.String()
		encoded, err := json.Marshal(event)
		if err != nil {
			current = nil
			return err
		}
		if _, err := m.db.ExecContext(ctx,
			`UPDATE task_events SET payload_json = ?, content_text = ? WHERE id = ?`,
			string(encoded), nullString(event.Content), current.firstID,
		); err != nil {
			current = nil
			return err
		}
		// 删掉被合并的其余行（分批 IN）
		remove := current.ids[1:]
		for start := 0; start < len(remove); start += 500 {
			end := start + 500
			if end > len(remove) {
				end = len(remove)
			}
			batch := remove[start:end]
			placeholders := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
			args := make([]any, 0, len(batch))
			for _, id := range batch {
				args = append(args, id)
			}
			if _, err := m.db.ExecContext(ctx,
				`DELETE FROM task_events WHERE id IN (`+placeholders+`)`, args...,
			); err != nil {
				return err
			}
		}
		merged += len(remove)
		current = nil
		return nil
	}

	for _, item := range items {
		if item.typ != "delta" || !item.sentAt.Before(olderThan) {
			if err := flush(); err != nil {
				return merged, err
			}
			continue
		}
		var event model.Event
		if err := json.Unmarshal([]byte(item.payload), &event); err != nil {
			if err := flush(); err != nil {
				return merged, err
			}
			continue
		}
		field := strings.TrimSpace(event.Field)
		if current != nil && current.field == field {
			current.content.WriteString(event.Content)
			current.ids = append(current.ids, item.id)
			continue
		}
		if err := flush(); err != nil {
			return merged, err
		}
		current = &run{firstID: item.id, payload: item.payload, field: field}
		current.content.WriteString(event.Content)
		current.ids = []int64{item.id}
	}
	if err := flush(); err != nil {
		return merged, err
	}
	return merged, nil
}

// PruneEvents 清理保留期之外的历史事件。completed 永久保留，
// 保证再老的对话也还能看到最终答复。
func (m *MySQLArchive) PruneEvents(ctx context.Context, before time.Time, batch int) (int, error) {
	if batch <= 0 {
		batch = 20000
	}
	result, err := m.db.ExecContext(ctx,
		`DELETE FROM task_events WHERE sent_at < ? AND event_type <> 'completed' LIMIT ?`,
		before, batch,
	)
	if err != nil {
		return 0, err
	}
	affected, _ := result.RowsAffected()
	return int(affected), nil
}

// TruncateLargeToolEvents 回填历史里超大的 tool_updated（工具输出超过 32KB 的行）。
func (m *MySQLArchive) TruncateLargeToolEvents(ctx context.Context, batch int) (int, error) {
	if batch <= 0 {
		batch = 200
	}
	rows, err := m.db.QueryContext(ctx,
		`SELECT id, payload_json FROM task_events
		  WHERE event_type = 'tool_updated' AND CHAR_LENGTH(payload_json) > ?
		  ORDER BY id ASC LIMIT ?`,
		ArchiveToolOutputLimit+4096, batch,
	)
	if err != nil {
		return 0, err
	}
	type item struct {
		id      int64
		payload string
	}
	var items []item
	for rows.Next() {
		var current item
		var payload sql.NullString
		if err := rows.Scan(&current.id, &payload); err != nil {
			rows.Close()
			return 0, err
		}
		current.payload = payload.String
		items = append(items, current)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	updated := 0
	for _, current := range items {
		var event model.Event
		if err := json.Unmarshal([]byte(current.payload), &event); err != nil {
			continue
		}
		if event.Tool == nil {
			continue
		}
		truncateArchivedToolOutput(&event)
		capArchivedToolPayload(&event)
		encoded, err := json.Marshal(event)
		if err != nil {
			continue
		}
		if _, err := m.db.ExecContext(ctx,
			`UPDATE task_events SET payload_json = ? WHERE id = ?`,
			string(encoded), current.id,
		); err != nil {
			return updated, err
		}
		updated++
	}
	return updated, nil
}
