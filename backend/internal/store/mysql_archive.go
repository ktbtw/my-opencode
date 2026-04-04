package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"relay-server/internal/model"
)

type MySQLConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	Database string
}

type MySQLArchive struct {
	db *sql.DB
}

func NewTaskArchiveFromEnv() (TaskArchive, error) {
	cfg := MySQLConfig{
		Host:     env("MYSQL_HOST", "127.0.0.1"),
		Port:     envInt("MYSQL_PORT", 3306),
		User:     env("MYSQL_USER", "root"),
		Password: env("MYSQL_PASSWORD", "ymh20040825"),
		Database: env("MYSQL_DATABASE", "chat_codex"),
	}

	archive, err := NewMySQLArchive(context.Background(), cfg)
	if err != nil {
		return nil, err
	}
	return archive, nil
}

func NewMySQLArchive(ctx context.Context, cfg MySQLConfig) (*MySQLArchive, error) {
	serverDSN := fmt.Sprintf("%s:%s@tcp(%s:%d)/?charset=utf8mb4&parseTime=true&multiStatements=true",
		cfg.User,
		cfg.Password,
		cfg.Host,
		cfg.Port,
	)
	serverDB, err := sql.Open("mysql", serverDSN)
	if err != nil {
		return nil, err
	}
	defer serverDB.Close()

	if err := serverDB.PingContext(ctx); err != nil {
		return nil, err
	}
	if _, err := serverDB.ExecContext(ctx, "CREATE DATABASE IF NOT EXISTS "+quoteIdent(cfg.Database)+" CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci"); err != nil {
		return nil, err
	}

	dbDSN := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=true&multiStatements=true",
		cfg.User,
		cfg.Password,
		cfg.Host,
		cfg.Port,
		cfg.Database,
	)
	db, err := sql.Open("mysql", dbDSN)
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	if err := ensureSchema(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	if err := ensureDefaultOperator(ctx, db); err != nil {
		db.Close()
		return nil, err
	}

	return &MySQLArchive{db: db}, nil
}

func (m *MySQLArchive) UpsertTask(task *model.Task) error {
	if task == nil {
		return nil
	}
	inputJSON, err := json.Marshal(task.Parts)
	if err != nil {
		return err
	}

	query := `
INSERT INTO tasks (
  task_id, agent_id, machine_id, project_id, project_root, session_id,
  status, result_text, error_text, input_json, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  agent_id = VALUES(agent_id),
  machine_id = VALUES(machine_id),
  project_id = VALUES(project_id),
  project_root = VALUES(project_root),
  session_id = VALUES(session_id),
  status = VALUES(status),
  result_text = VALUES(result_text),
  error_text = VALUES(error_text),
  input_json = VALUES(input_json),
  updated_at = VALUES(updated_at)
`

	_, err = m.db.Exec(
		query,
		task.ID,
		task.AgentID,
		task.MachineID,
		task.ProjectID,
		task.ProjectRoot,
		task.SessionID,
		string(task.Status),
		nullString(task.Result),
		nullString(task.Error),
		string(inputJSON),
		task.CreatedAt.UTC(),
		task.UpdatedAt.UTC(),
	)
	return err
}

func (m *MySQLArchive) AppendEvent(event model.Event) error {
	payloadJSON, err := json.Marshal(event)
	if err != nil {
		return err
	}

	_, err = m.db.Exec(`
INSERT INTO task_events (
  task_id, event_type, session_id, permission_id, payload_json,
  content_text, error_text, sent_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
`,
		event.TaskID,
		event.Type,
		event.SessionID,
		event.PermissionID,
		string(payloadJSON),
		nullString(event.Content),
		nullString(event.Error),
		event.SentAt.UTC(),
	)
	return err
}

func (m *MySQLArchive) ListTasks(filter model.TaskFilter) ([]*model.Task, error) {
	query := `
SELECT task_id, agent_id, machine_id, project_id, project_root, session_id,
       status, result_text, error_text, input_json, created_at, updated_at
FROM tasks
`
	clauses := make([]string, 0, 4)
	args := make([]any, 0, 5)

	if filter.AgentID != "" {
		clauses = append(clauses, "agent_id = ?")
		args = append(args, filter.AgentID)
	}
	if filter.MachineID != "" {
		clauses = append(clauses, "machine_id = ?")
		args = append(args, filter.MachineID)
	}
	if filter.ProjectID != "" {
		clauses = append(clauses, "project_id = ?")
		args = append(args, filter.ProjectID)
	}
	if filter.SessionID != "" {
		clauses = append(clauses, "session_id = ?")
		args = append(args, filter.SessionID)
	}
	if filter.Status != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, string(filter.Status))
	}
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	query += " ORDER BY created_at DESC"
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}

	rows, err := m.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []*model.Task
	for rows.Next() {
		var (
			task      model.Task
			status    string
			result    sql.NullString
			errText   sql.NullString
			inputJSON sql.NullString
		)
		if err := rows.Scan(
			&task.ID,
			&task.AgentID,
			&task.MachineID,
			&task.ProjectID,
			&task.ProjectRoot,
			&task.SessionID,
			&status,
			&result,
			&errText,
			&inputJSON,
			&task.CreatedAt,
			&task.UpdatedAt,
		); err != nil {
			return nil, err
		}
		task.Status = model.TaskStatus(status)
		if result.Valid {
			task.Result = result.String
		}
		if errText.Valid {
			task.Error = errText.String
		}
		if inputJSON.Valid && inputJSON.String != "" {
			if err := json.Unmarshal([]byte(inputJSON.String), &task.Parts); err != nil {
				return nil, err
			}
		}
		tasks = append(tasks, &task)
	}

	return tasks, rows.Err()
}

func (m *MySQLArchive) UpsertSession(session *model.Session) error {
	if session == nil {
		return nil
	}

	query := `
INSERT INTO sessions (
  session_id, agent_id, machine_id, project_id, status, last_task_id, summary, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  agent_id = VALUES(agent_id),
  machine_id = VALUES(machine_id),
  project_id = VALUES(project_id),
  status = VALUES(status),
  last_task_id = VALUES(last_task_id),
  summary = VALUES(summary),
  updated_at = VALUES(updated_at)
`
	_, err := m.db.Exec(
		query,
		session.ID,
		session.AgentID,
		session.MachineID,
		session.ProjectID,
		session.Status,
		nullString(session.LastTaskID),
		nullString(session.Summary),
		session.CreatedAt.UTC(),
		session.UpdatedAt.UTC(),
	)
	return err
}

func (m *MySQLArchive) ListSessions(filter model.SessionFilter) ([]*model.Session, error) {
	query := `
SELECT session_id, agent_id, machine_id, project_id, status, last_task_id, summary, created_at, updated_at
FROM sessions
`
	clauses := make([]string, 0, 4)
	args := make([]any, 0, 5)
	if filter.AgentID != "" {
		clauses = append(clauses, "agent_id = ?")
		args = append(args, filter.AgentID)
	}
	if filter.MachineID != "" {
		clauses = append(clauses, "machine_id = ?")
		args = append(args, filter.MachineID)
	}
	if filter.ProjectID != "" {
		clauses = append(clauses, "project_id = ?")
		args = append(args, filter.ProjectID)
	}
	if filter.Status != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, filter.Status)
	}
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	query += " ORDER BY updated_at DESC"
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}

	rows, err := m.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*model.Session
	for rows.Next() {
		var (
			session    model.Session
			lastTaskID sql.NullString
			summary    sql.NullString
		)
		if err := rows.Scan(
			&session.ID,
			&session.AgentID,
			&session.MachineID,
			&session.ProjectID,
			&session.Status,
			&lastTaskID,
			&summary,
			&session.CreatedAt,
			&session.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if lastTaskID.Valid {
			session.LastTaskID = lastTaskID.String
		}
		if summary.Valid {
			session.Summary = summary.String
		}
		sessions = append(sessions, &session)
	}

	return sessions, rows.Err()
}

func (m *MySQLArchive) AuthenticateOperator(username, password string) (*model.Operator, error) {
	row := m.db.QueryRow(`
SELECT id, operator_uid, username, name, operator_key, password_hash
FROM operators
WHERE username = ?
`, username)

	var (
		operator     model.Operator
		passwordHash string
	)
	if err := row.Scan(
		&operator.ID,
		&operator.OperatorUID,
		&operator.Username,
		&operator.Name,
		&operator.OperatorKey,
		&passwordHash,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if passwordHash != hashPassword(password) {
		return nil, nil
	}
	return &operator, nil
}

func (m *MySQLArchive) GetOperatorByKey(operatorKey string) (*model.Operator, error) {
	row := m.db.QueryRow(`
SELECT id, operator_uid, username, name, operator_key
FROM operators
WHERE operator_key = ?
`, operatorKey)

	var operator model.Operator
	if err := row.Scan(
		&operator.ID,
		&operator.OperatorUID,
		&operator.Username,
		&operator.Name,
		&operator.OperatorKey,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &operator, nil
}

func (m *MySQLArchive) Close() error {
	if m == nil || m.db == nil {
		return nil
	}
	return m.db.Close()
}

func ensureSchema(ctx context.Context, db *sql.DB) error {
	for _, stmt := range schemaStatements {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func ensureDefaultOperator(ctx context.Context, db *sql.DB) error {
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM operators WHERE username = ?", "admin").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	key, err := randomHex(24)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `
INSERT INTO operators (operator_uid, name, client_type, username, password_hash, operator_key)
VALUES (?, ?, ?, ?, ?, ?)
`, "op_admin", "管理员", "web", "admin", hashPassword("admin123456"), "opk_"+key)
	return err
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func env(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func quoteIdent(value string) string {
	return "`" + strings.ReplaceAll(value, "`", "``") + "`"
}

func randomHex(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func hashPassword(password string) string {
	sum := sha256.Sum256([]byte(password))
	return hex.EncodeToString(sum[:])
}

var schemaStatements = []string{
	`CREATE TABLE IF NOT EXISTS operators (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  operator_uid VARCHAR(64) NOT NULL,
  name VARCHAR(128) NOT NULL DEFAULT '',
  client_type VARCHAR(32) NOT NULL DEFAULT '',
  username VARCHAR(64) NOT NULL DEFAULT '',
  password_hash VARCHAR(255) NOT NULL DEFAULT '',
  operator_key VARCHAR(128) NOT NULL DEFAULT '',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_operator_uid (operator_uid)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
	`ALTER TABLE operators ADD COLUMN IF NOT EXISTS username VARCHAR(64) NOT NULL DEFAULT ''`,
	`ALTER TABLE operators ADD COLUMN IF NOT EXISTS password_hash VARCHAR(255) NOT NULL DEFAULT ''`,
	`ALTER TABLE operators ADD COLUMN IF NOT EXISTS operator_key VARCHAR(128) NOT NULL DEFAULT ''`,
	`CREATE TABLE IF NOT EXISTS machines (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  machine_id VARCHAR(128) NOT NULL,
  hostname VARCHAR(255) NOT NULL DEFAULT '',
  os_name VARCHAR(64) NOT NULL DEFAULT '',
  status VARCHAR(32) NOT NULL DEFAULT 'offline',
  last_seen_at DATETIME NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_machine_id (machine_id),
  KEY idx_machines_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
	`CREATE TABLE IF NOT EXISTS agents (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  agent_id VARCHAR(191) NOT NULL,
  machine_id VARCHAR(128) NOT NULL,
  project_id VARCHAR(128) NOT NULL,
  project_root VARCHAR(1024) NOT NULL,
  hostname VARCHAR(255) NOT NULL DEFAULT '',
  version VARCHAR(64) NOT NULL DEFAULT '',
  status VARCHAR(32) NOT NULL DEFAULT 'offline',
  current_task_id VARCHAR(64) NOT NULL DEFAULT '',
  last_seen_at DATETIME NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_agent_id (agent_id),
  KEY idx_agents_machine_id (machine_id),
  KEY idx_agents_project_id (project_id),
  KEY idx_agents_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
	`CREATE TABLE IF NOT EXISTS sessions (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  session_id VARCHAR(64) NOT NULL,
  agent_id VARCHAR(191) NOT NULL,
  machine_id VARCHAR(128) NOT NULL,
  project_id VARCHAR(128) NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'active',
  last_task_id VARCHAR(64) NOT NULL DEFAULT '',
  summary TEXT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_session_id (session_id),
  KEY idx_sessions_agent_id (agent_id),
  KEY idx_sessions_project_id (project_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
	`CREATE TABLE IF NOT EXISTS tasks (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  task_id VARCHAR(64) NOT NULL,
  agent_id VARCHAR(191) NOT NULL,
  machine_id VARCHAR(128) NOT NULL,
  project_id VARCHAR(128) NOT NULL,
  project_root VARCHAR(1024) NOT NULL,
  session_id VARCHAR(64) NOT NULL DEFAULT '',
  status VARCHAR(32) NOT NULL,
  result_text LONGTEXT NULL,
  error_text LONGTEXT NULL,
  input_json JSON NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_task_id (task_id),
  KEY idx_tasks_agent_id (agent_id),
  KEY idx_tasks_machine_id (machine_id),
  KEY idx_tasks_project_id (project_id),
  KEY idx_tasks_status (status),
  KEY idx_tasks_session_id (session_id),
  KEY idx_tasks_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
	`CREATE TABLE IF NOT EXISTS task_events (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  task_id VARCHAR(64) NOT NULL,
  event_type VARCHAR(64) NOT NULL,
  session_id VARCHAR(64) NOT NULL DEFAULT '',
  permission_id VARCHAR(64) NOT NULL DEFAULT '',
  payload_json JSON NULL,
  content_text LONGTEXT NULL,
  error_text LONGTEXT NULL,
  sent_at DATETIME NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  KEY idx_task_events_task_id (task_id),
  KEY idx_task_events_sent_at (sent_at),
  KEY idx_task_events_event_type (event_type)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
	`CREATE TABLE IF NOT EXISTS task_approvals (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  task_id VARCHAR(64) NOT NULL,
  session_id VARCHAR(64) NOT NULL DEFAULT '',
  agent_id VARCHAR(191) NOT NULL,
  permission_id VARCHAR(64) NOT NULL,
  permission_name VARCHAR(64) NOT NULL DEFAULT '',
  reply VARCHAR(32) NOT NULL DEFAULT '',
  patterns_json JSON NULL,
  metadata_json JSON NULL,
  message_text TEXT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'pending',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_task_permission (task_id, permission_id),
  KEY idx_task_approvals_agent_id (agent_id),
  KEY idx_task_approvals_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
}
