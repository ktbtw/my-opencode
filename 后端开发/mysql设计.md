# MySQL 表设计

## 目标

为多机多 `agent` 控制台提供可持久化的数据结构，支持：

- 多操作端登录
- 多机器在线
- 多 `agent` 调度
- 任务与事件流追踪
- 会话复用
- 审批记录

当前数据库：

- 类型：`MySQL 9.x`
- 账号：`root`

## 设计原则

- 调度最小单位是 `agent`
- `machine` 只是宿主，不直接接任务
- `task` 与 `session` 绑定 `agent_id`
- `task_event` 全量保留中间状态
- 审批记录单独建表，避免塞进任务主表

## 表清单

### 1. `operators`

用途：

- 记录登录控制台的用户端身份

字段建议：

- `id` bigint pk
- `operator_uid` varchar(64) unique
- `name` varchar(128)
- `client_type` varchar(32)
- `created_at` datetime
- `updated_at` datetime

### 2. `machines`

用途：

- 记录物理设备或服务器

字段建议：

- `id` bigint pk
- `machine_id` varchar(128) unique
- `hostname` varchar(255)
- `os_name` varchar(64)
- `status` varchar(32)
- `last_seen_at` datetime
- `created_at` datetime
- `updated_at` datetime

索引建议：

- unique index `uk_machine_id(machine_id)`
- index `idx_machines_status(status)`

### 3. `agents`

用途：

- 记录每个项目执行器

字段建议：

- `id` bigint pk
- `agent_id` varchar(191) unique
- `machine_id` varchar(128)
- `project_id` varchar(128)
- `project_root` varchar(1024)
- `hostname` varchar(255)
- `version` varchar(64)
- `status` varchar(32)
- `current_task_id` varchar(64)
- `last_seen_at` datetime
- `created_at` datetime
- `updated_at` datetime

索引建议：

- unique index `uk_agent_id(agent_id)`
- index `idx_agents_machine_id(machine_id)`
- index `idx_agents_project_id(project_id)`
- index `idx_agents_status(status)`

### 4. `sessions`

用途：

- 记录对话上下文

字段建议：

- `id` bigint pk
- `session_id` varchar(64) unique
- `agent_id` varchar(191)
- `machine_id` varchar(128)
- `project_id` varchar(128)
- `status` varchar(32)
- `last_task_id` varchar(64)
- `summary` text
- `created_at` datetime
- `updated_at` datetime

索引建议：

- unique index `uk_session_id(session_id)`
- index `idx_sessions_agent_id(agent_id)`
- index `idx_sessions_project_id(project_id)`

### 5. `tasks`

用途：

- 记录任务主信息

字段建议：

- `id` bigint pk
- `task_id` varchar(64) unique
- `agent_id` varchar(191)
- `machine_id` varchar(128)
- `project_id` varchar(128)
- `project_root` varchar(1024)
- `session_id` varchar(64)
- `status` varchar(32)
- `result_text` longtext
- `error_text` longtext
- `input_json` json
- `created_at` datetime
- `updated_at` datetime

索引建议：

- unique index `uk_task_id(task_id)`
- index `idx_tasks_agent_id(agent_id)`
- index `idx_tasks_machine_id(machine_id)`
- index `idx_tasks_project_id(project_id)`
- index `idx_tasks_status(status)`
- index `idx_tasks_session_id(session_id)`
- index `idx_tasks_created_at(created_at)`

### 6. `task_events`

用途：

- 保存任务事件流

字段建议：

- `id` bigint pk
- `task_id` varchar(64)
- `event_type` varchar(64)
- `session_id` varchar(64)
- `permission_id` varchar(64)
- `payload_json` json
- `content_text` longtext
- `error_text` longtext
- `sent_at` datetime
- `created_at` datetime

索引建议：

- index `idx_task_events_task_id(task_id)`
- index `idx_task_events_sent_at(sent_at)`
- index `idx_task_events_event_type(event_type)`

### 7. `task_approvals`

用途：

- 记录审批状态与审批动作

字段建议：

- `id` bigint pk
- `task_id` varchar(64)
- `session_id` varchar(64)
- `agent_id` varchar(191)
- `permission_id` varchar(64)
- `permission_name` varchar(64)
- `reply` varchar(32)
- `patterns_json` json
- `metadata_json` json
- `message_text` text
- `status` varchar(32)
- `created_at` datetime
- `updated_at` datetime

索引建议：

- unique index `uk_task_permission(task_id, permission_id)`
- index `idx_task_approvals_agent_id(agent_id)`
- index `idx_task_approvals_status(status)`

## 建表 SQL 草案

```sql
CREATE TABLE machines (
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
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE agents (
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
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE sessions (
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
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE tasks (
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
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE task_events (
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
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE task_approvals (
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
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
```

## 第一阶段落地建议

1. 先让 `agents`、`tasks`、`task_events` 三张表落地
2. 再接 `sessions`
3. 最后接 `task_approvals`

当前第一模块前端只依赖：

- `GET /api/agents`

所以这一步先记录表设计，不强制一次性切完所有持久化实现。
