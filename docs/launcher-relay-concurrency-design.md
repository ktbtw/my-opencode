# Launcher Relay 指令并发化设计

## 1. 背景与问题

### 1.1 现象

设备（launcher）通过 WebSocket 长连接接收后端下发的指令。当前实现中，**所有指令在读循环里同步串行执行**：

```go
// launcher/internal/relay/client.go:91-133（现状）
for {
    _, data, err := conn.Read(ctx)      // 读一条指令
    if err != nil { return err }
    ...
    result := handler(env, cfg, svc)    // ← 同步执行，无 goroutine
    if err := writeEnvelope(ctx, result); err != nil {
        return err
    }
}
```

### 1.2 后果：故障扩散

一条慢指令会堵塞整条链路，表现为**放大器效应**：

```
一条慢指令 → 读循环阻塞 → 心跳停摆 → 后端判定设备离线
                       → 后续所有指令排队 → 全线超时
```

**已发生的实例（2026-10）**：`device.ai_config.get` 内部同步抓取 `models.dev`，国内网络下卡满 10 秒。后端 `launcherModelsInitialTimeout` 为 12 秒（`backend/internal/api/api.go:105`），于是：

- 对话页模型列表 → `context deadline exceeded`
- AI 配置页 → 同样超时
- 那 10 秒内的其他所有设备指令 → 全部排队

该具体堵点已通过「目录刷新异步化」修复（Launcher 0.1.188）。但**结构性扩散风险仍在**——下一个慢 handler 会重现同样的问题。

### 1.3 目标

- 单条慢指令**不再影响**其他指令与心跳
- 保持协议、后端行为、指令语义完全不变
- 队列过载时**明确快速失败**，而非静默超时
- 未知指令可被观测到

---

## 2. 现状分析

### 2.1 三个已核实的关键事实

| 事实 | 证据 | 影响 |
|---|---|---|
| 后端靠 `RequestID` 配对，**响应可乱序** | `backend/internal/app/app.go:1958` `pushLauncherResult(env.RequestID, msg)` | 解开最大枷锁：无需保序机制 |
| 写侧**已并发安全** | `client.go:59-67` `writeMu` 保护所有 `conn.Write` | 多 worker 并发写不会串包 |
| 后端**确实会并发下发**指令 | Flutter `Future.wait` 并发上传分块（`flutter_app/lib/features/devices/data/device_repository.dart:1375`） | 不能假设「发一个等一个」 |

### 2.2 服务层锁保护现状（已逐个核实）

| 指令族 | 保护机制 | 位置 |
|---|---|---|
| `launcher.*` 变更类 | `s.opsMu`（全局串行）+ `s.mu` | `app.go:2753`(CreateAgent)、`:2824`(RenameAgent)、`:2930`(RemoveAgent)、`:2860`(RestartAgent)、`:2911`(SetAgentEnabled) 等 |
| `launcher.get` / `storage` / `diagnostics` 等只读 | `s.mu.RLock` 或纯读取 | 无共享可变状态 |
| `ai_config.*` | `s.opsMu` | `app.go:1281`(Save)、`:1294`、`:1307` |
| `mcp_config.*` / `env_config.*` / `compaction_config.*` | `s.opsMu` | `app.go:1384`、`:1439`、`:1507`、`:1519` |
| `directory_files.*` | `s.directoryFilesMu` | `discovery.go:358, 387, 439, 486, 513, 536` |
| **`project_files.*` 上传族** | **无锁** | `discovery.go:832, 867, 923` 均无加锁 |
| `launcher.self_update` | `s.updateMu.TryLock()` | `app.go:1071` |

**结论**：并发化后，绝大多数指令族因已有锁而**天然安全**。唯一缺口是 `project_files` 上传族——它的正确性目前**完全依赖读循环的串行执行**。

### 2.3 唯一真实缺口：project_files 上传

`WriteProjectUploadChunk`（`discovery.go:867`）调用 `recordUploadChunk`（`upload_session.go:75`），后者是**读-改-写**：

```go
manifest, ok := loadUploadManifest(tempPath)               // 读
manifest.Received = append(manifest.Received, chunkIndex)  // 改
return saveUploadManifest(tempPath, manifest)              // 整体写回
```

并发执行会导致**丢失更新**（后写覆盖先写），表现为偶发的「分块丢失 → complete 校验失败」。而客户端是 `Future.wait` 并发发块的，所以这个场景真实存在。

> 对比：`directory_files` 上传族有 `directoryFilesMu` 保护同一段逻辑，所以安全。

---

## 3. 设计方案

### 3.1 架构

采用**有界 worker 池 + 按 key 串行化**：

```text
                    ┌──────────────────────────────────┐
   conn.Read ──────►│  读循环（永不阻塞）              │
                    │  解析 → 分类 → 投递 or 拒绝       │
                    └───────────┬──────────────────────┘
                                │ jobs (有界队列, cap=32)
                ┌───────────────┼───────────────┐
                ▼               ▼               ▼
            worker-1        worker-2   ...   worker-8
                │               │               │
                └───────┬───────┴───────┬───────┘
                        │               │
                   keyMutex         keyMutex
              ("proj-upload:A")  ("proj-upload:B")
                        │
                 writeEnvelope（writeMu 已保证并发安全）
```

**读循环**只做三件事：读、分类、投递。唯一同步特例是 `device.welcome`（仅启动心跳 goroutine，微秒级）。

**Worker** 取任务后先按 key 取互斥，再执行 handler。

### 3.2 核心数据结构

```go
type dispatcher struct {
    ctx     context.Context   // 跟随连接生命周期
    jobs    chan job          // 有界队列
    write   func(context.Context, envelope) error
    keys    sync.Map          // map[string]*keyEntry
    wg      sync.WaitGroup
}

type job struct {
    env     envelope
    handler handlerFunc
    cfg     config.Config
    svc     Service
}

// keyEntry 是 per-key 串行锁，带引用计数以便回收。
type keyEntry struct {
    mu   sync.Mutex
    refs atomic.Int32
}
```

### 3.3 读循环改造

```go
for {
    _, data, err := conn.Read(ctx)
    if err != nil { return err }
    var env envelope
    if json.Unmarshal(data, &env) != nil { continue }

    // 唯一同步特例：只启动心跳，微秒级
    if env.Type == "device.welcome" {
        startHeartbeatOnce(...)
        continue
    }

    handler, ok := handlers[env.Type]
    if !ok {
        // 现状是静默 continue，后端只能等到超时
        log.Printf("[relay] 未知指令 type=%s request_id=%s", env.Type, env.RequestID)
        continue
    }

    select {
    case d.jobs <- job{env, handler, cfg, svc}:
        // 入队成功
    default:
        // 队列满 → 立即拒绝，绝不阻塞读循环
        _ = d.write(ctx, busyEnvelope(env))
    }
}
```

**为什么队列满时拒绝而非阻塞**：阻塞读循环会导致**读不到后续消息**，包括用户点的「取消」。拒绝把「等 12 秒超时」变成「立刻明确失败」，且客户端 `_postChunkWithRetry` 已有重试逻辑。

```go
func busyEnvelope(env envelope) envelope {
    return envelope{
        Type:      resultTypeFor(env.Type),   // 按指令族映射
        RequestID: env.RequestID,
        SentAt:    time.Now().UTC().Format(time.RFC3339),
        Payload: map[string]any{
            "success": false,
            "error":   "设备繁忙，请稍后重试",
        },
    }
}
```

### 3.4 Worker 实现

```go
func (d *dispatcher) worker() {
    defer d.wg.Done()
    for {
        select {
        case <-d.ctx.Done():
            return                     // ① 连接断开 → 立即退出
        case j := <-d.jobs:
            key := jobKey(j.env)
            if key != "" {
                entry := d.acquireKey(key)
                entry.mu.Lock()
                result := j.handler(j.env, j.cfg, j.svc)
                entry.mu.Unlock()
                d.releaseKey(key, entry)
                if err := d.write(d.ctx, result); err != nil {
                    return
                }
                continue
            }
            result := j.handler(j.env, j.cfg, j.svc)
            if err := d.write(d.ctx, result); err != nil {
                return             // ② 写失败（连接已坏）→ 退出
            }
        }
    }
}
```

**两个退出条件都必然可达**，因此不存在 worker 泄漏：
- `d.ctx` 跟随 `connect()` 的 ctx，连接断开时 `Done()` 一定关闭
- `d.write` 失败说明连接已坏，继续留着无意义

**配套三件事封死泄漏**：
1. `defer d.wg.Done()` — 退出时计数递减
2. 读循环退出时 `close(d.jobs)` — 防止 worker 卡在接收
3. `connect()` 的 `defer d.wg.Wait()` — 连接结束前 worker 全部回收

> 唯一残留风险是 handler 自身不返回（如死循环）。但这不是 dispatcher 引入的——现状同样会卡死，且**更糟**（卡住整条链路）。并发化后至少只损失一个 worker。
>
> 注：`handler` 签名无 `ctx`（`func(env, cfg, svc) envelope`），无法强制取消。这与现状一致，非新增问题。

### 3.5 Key 提取规则

```go
func jobKey(env envelope) string {
    switch env.Type {
    case "device.directory_files.upload_create",
         "device.directory_files.upload_chunk",
         "device.directory_files.upload_complete":
        return "dir-upload:" + payloadUploadID(env)
    case "device.project_files.upload_create",
         "device.project_files.upload_chunk",
         "device.project_files.upload_complete":
        return "proj-upload:" + payloadUploadID(env)
    case "device.project_files.download_create",
         "device.project_files.download_chunk":
        return "proj-download:" + payloadUploadID(env)
    default:
        return ""   // 空 key = 完全并发
    }
}
```

`payloadUploadID` 从 `DirectoryFileRequest.UploadID` / `ProjectFilesRequest.UploadID` 提取（`model/model.go:725, 765`）。

**为什么 `directory_files` 也要串行**：虽然它已有 `directoryFilesMu` 保证不损坏，但该锁只保护单次调用，不保证 `complete` 不会越过 `chunk`。加上 per-key 串行可保证业务顺序。

### 3.6 参数

| 参数 | 建议值 | 理由 |
|---|---|---|
| worker 数 | 8 | handler 混合 I/O（文件系统、HTTP）与 CPU（分块 SHA-256） |
| 队列深度 | 32 | 容纳一次上传窗口的突发（默认并发约 4-8） |
| 两者均可配 | — | 不写死，按实测调 |

### 3.7 分期

| 阶段 | 内容 | 风险 |
|---|---|---|
| **一** | 有界池 + per-key 串行 + 拒绝 + 未知指令日志 | 中（并发语义） |
| 二 | 快/慢 lane 分离（查询走快道，安装/升级走慢道） | 低 |
| 三 | 未知指令回错误响应（需后端配合） | 低 |

**阶段一是核心**。阶段二解决「即使有池，一个 10 秒的 `runtime_install` 仍会占住一个 worker」的问题，属优化，待阶段一跑稳后再评估。

---

## 4. 逐指令行为变化

下表列出**每个指令**在改造前后的行为差异。

**列说明**：
- **锁**：该指令依赖的既有保护（`—` 表示只读或无共享可变状态）
- **Key**：新增的 per-key 串行键（`—` 表示不串行）
- **阻塞特征**：该 handler 的最坏耗时量级

### 4.1 连接管理类（读循环内特判）

| 指令 | 锁 | Key | 阻塞特征 | 改造前 | 改造后 |
|---|---|---|---|---|---|
| `device.welcome` | — | — | 微秒 | 读循环内同步启动心跳 | **不变**（仍同步，但只启动 goroutine） |
| `device.launcher.agent_preflight.start` | `semanticPreflightMu` | — | 长（分钟级） | 读循环内同步，内部已 `go`（`client.go:321`） | 走 worker；**读循环立即返回** |
| `device.launcher.runtime_install` | `runtimeMu` | — | 长（分钟级） | 读循环内同步，内部已 `go`（`:417`） | 走 worker；**读循环立即返回** |
| `device.launcher.tunnel.open` | — | — | 长 | 已是 `go`（`:122`） | **不变** |

### 4.2 `handleLauncher` — 只读/查询类

| 指令 | 锁 | Key | 阻塞特征 | 变化 |
|---|---|---|---|---|
| `device.launcher.get` | `mu.RLock` | — | 微秒 | 走 worker，**可与其他指令并行** |
| `device.launcher.mcp_status` | `mu.RLock` | — | 毫秒 | 同上 |
| `device.launcher.semantic_agent_get` | `mu.RLock` | — | 毫秒 | 同上 |
| `device.launcher.skills_get` | 读 | — | 毫秒 | 同上 |
| `device.launcher.skills_global_get` | 读 | — | 毫秒 | 同上 |
| `device.launcher.autostart_status` | 读 | — | 毫秒 | 同上 |
| `device.launcher.runtime_status` | 读 | — | 毫秒 | 同上 |
| `device.launcher.storage` | — | — | **秒级**（目录遍历，`storage.go:10` 注释明示「开销较大」） | 走 worker；**不再阻塞心跳与其他指令** |
| `device.launcher.diagnostics` | — | — | 秒级 | 同上 |
| `device.launcher.ssh_status` | 读 | — | 毫秒 | 同上 |

### 4.3 `handleLauncher` — 变更类

| 指令 | 锁 | Key | 阻塞特征 | 变化 |
|---|---|---|---|---|
| `device.launcher.create_agent` | `opsMu`+`mu` | — | 秒级（建目录、起进程） | 走 worker；与其他变更类**仍由 `opsMu` 串行**，但**不阻塞只读类** |
| `device.launcher.rename_agent` | `opsMu` | — | 毫秒 | 同上 |
| `device.launcher.remove_agent` | `opsMu`+`mu` | — | 秒级 | 同上 |
| `device.launcher.restart_agent` | `opsMu` | — | **秒级~十秒**（停+起进程） | 同上 |
| `device.launcher.set_agent_enabled` | `opsMu` | — | 秒级 | 同上 |
| `device.launcher.project_identity_correct` | `opsMu` | — | 毫秒 | 同上 |
| `device.launcher.semantic_agent_save` | `opsMu` | — | 毫秒 | 同上 |
| `device.launcher.skills_save` / `skills_import` | `opsMu` | — | 毫秒~秒 | 同上 |
| `device.launcher.skills_global_save` / `_import` | `opsMu` | — | 毫秒~秒 | 同上 |
| `device.launcher.autostart_enable` / `_disable` | `opsMu` | — | 毫秒 | 同上 |
| `device.launcher.directory_permission` | `opsMu` | — | 毫秒 | 同上 |
| `device.launcher.storage_clear` | — | — | **秒级~分钟**（删大量文件） | 走 worker；**不再阻塞心跳** |
| `device.launcher.ssh_setup` | — | — | 秒级 | 同上 |
| `device.launcher.ssh_authorized_key` | — | — | 毫秒 | 同上 |
| `device.launcher.upgrade` | `opsMu` | — | 长（分钟级，`StartUpgrade` 异步） | 走 worker；返回快 |
| `device.launcher.rollback` | `opsMu` | — | 长（分钟级） | 同上 |
| `device.launcher.self_update` | `updateMu.TryLock` | — | **最长 5 分钟**（`app.go:1094-1098`） | 走 worker；**不再阻塞整机** |

### 4.4 `handleAIConfig`

| 指令 | 锁 | Key | 阻塞特征 | 变化 |
|---|---|---|---|---|
| `device.ai_config.get` | `opsMu`(部分) | — | 已异步化目录，现为毫秒 | 走 worker |
| `device.ai_config.list_models` | `opsMu` | — | 保留同步抓目录（用户显式刷新，≤10 秒） | 走 worker；**期间其他指令不受影响** |
| `device.ai_config.save` | `opsMu` | — | 秒级 | 走 worker |
| `device.ai_config.save_text` | `opsMu` | — | 秒级 | 同上 |
| `device.ai_config.clear_provider` | `opsMu` | — | 秒级 | 同上 |

### 4.5 `handleMCPConfig` / `handleEnvConfig` / `handleCompactionConfig`

| 指令 | 锁 | Key | 阻塞特征 | 变化 |
|---|---|---|---|---|
| `device.mcp_config.get` / `agent_get` | 读 | — | 毫秒 | 走 worker |
| `device.mcp_config.agent_save` / `save` / `remove` | `opsMu` | — | 毫秒~秒 | 走 worker |
| `device.env_config.get` | 读 | — | 毫秒 | 走 worker |
| `device.env_config.save` | `opsMu` | — | 毫秒 | 走 worker |
| `device.compaction_config.get` | 读 | — | 毫秒 | 走 worker |
| `device.compaction_config.save` | `opsMu` | — | 毫秒 | 走 worker |

### 4.6 `handleDirectories` / `handleDirectoryFiles`

| 指令 | 锁 | Key | 阻塞特征 | 变化 |
|---|---|---|---|---|
| `device.directories.get` | — | — | 毫秒~秒 | 走 worker |
| `device.directory_files.upload_create` | `directoryFilesMu` | `dir-upload:{upload_id}` | 毫秒 | **新增串行**：同 upload_id 保序 |
| `device.directory_files.upload_chunk` | `directoryFilesMu` | `dir-upload:{upload_id}` | **秒级**（base64 解码 + SHA-256 + 写盘） | 同上；**不同 upload_id 可并行** |
| `device.directory_files.upload_complete` | `directoryFilesMu` | `dir-upload:{upload_id}` | **秒级**（整体 SHA-256 校验） | 同上；**保证不越过 chunk** |
| `device.directory_files.upload_status` | `directoryFilesMu` | — | 毫秒 | 走 worker |
| `device.directory_files.create_file` / `mkdir` / `delete` | `directoryFilesMu` | — | 毫秒 | 走 worker |

### 4.7 `handleProjectFiles`

| 指令 | 锁 | Key | 阻塞特征 | 变化 |
|---|---|---|---|---|
| `device.project_files.list` | — | — | 毫秒~秒 | 走 worker |
| `device.project_files.download` | — | — | 秒级 | 走 worker |
| `device.project_files.download_create` | — | `proj-download:{upload_id}` | 毫秒 | **新增串行** |
| `device.project_files.download_chunk` | — | `proj-download:{upload_id}` | 秒级 | **新增串行**；不同 id 可并行 |
| `device.project_files.upload` | — | — | 秒级 | 走 worker |
| `device.project_files.upload_create` | **无锁** | `proj-upload:{upload_id}` | 毫秒 | **新增串行（关键修复）** |
| `device.project_files.upload_chunk` | **无锁** | `proj-upload:{upload_id}` | 秒级 | **新增串行（关键修复）**：消除清单丢失更新 |
| `device.project_files.upload_complete` | **无锁** | `proj-upload:{upload_id}` | 秒级 | **新增串行（关键修复）**：保证不越过 chunk |
| `device.project_files.upload_status` | **无锁** | — | 毫秒 | 走 worker |
| `device.project_files.create_file` / `mkdir` / `delete` / `rename` | **无锁** | — | 毫秒 | 走 worker |

> `project_files` 上传族是**唯一需要新增正确性保护**的地方。`directory_files` 有 `directoryFilesMu` 兜底，`project_files` 没有。

### 4.8 写指令（设备 → 后端）

写侧**行为完全不变**，仅并发度提高：

| 消息类型 | 变化 |
|---|---|
| `device.hello` | 不变 |
| `device.heartbeat` | **改善**：不再被读循环阻塞 |
| `device.launcher.result` | 不变（`writeMu` 已保证并发安全） |
| `device.launcher.agent_preflight.status` | 不变（内部已有 goroutine + `writeMu`） |
| `device.ai_config.result` | 不变 |
| `device.mcp_config.result` | 不变 |
| `device.env_config.result` | 不变 |
| `device.directories.result` | 不变 |

**关键点**：后端按 `RequestID` 配对（`backend/internal/app/app.go:1958`），因此响应乱序**不会**影响正确性。

---

## 5. 优化效果

### 5.1 直接收益

| 场景 | 改造前 | 改造后 |
|---|---|---|
| 慢指令期间的心跳 | **停摆** | 持续正常 |
| 慢指令期间的其他指令 | 全部排队 | 并行处理 |
| 设备「假死」 | 一条慢指令 → 后端全线超时 | 只有该指令慢，其余正常 |
| 不同文件并发上传 | 串行处理 | 真正并行，且保持正确性 |
| 队列过载 | 无感知，静默堆积 | 明确快速失败 + 日志 |
| 未知指令 | 静默丢弃，后端干等超时 | 有日志可查 |

### 5.2 最实质的改变：故障不再扩散

**改造前**（放大器效应）：
```text
一条慢指令 → 读循环阻塞 → 心跳停 → 后端判离线 → 所有请求超时
```

**改造后**（隔离效应）：
```text
一条慢指令 → 占一个 worker → 其余 7 个 worker 继续服务
```

这正是 models.dev 事件的教训：那次卡 10 秒，代价是**整台设备失联**，而非「模型列表慢」。

### 5.3 量化示例

假设 8 worker、`self_update` 耗时 5 分钟，期间收到 10 条 `launcher.get`：

| | 改造前 | 改造后 |
|---|---|---|
| 10 条查询的响应时间 | 全部排队，最坏 5 分钟后 | 毫秒级 |
| 心跳 | 5 分钟不发送 → 后端判离线 | 正常 |
| 队列占用 | 无界堆积 | 10 条，远未达上限 32 |

### 5.4 可观测性收益

- 队列深度、拒绝次数、各 key 等待时长 → 可暴露为指标
- 能回答「设备到底在忙什么」
- 未知指令有日志，协议不一致可被发现

---

## 6. 测试策略

```go
// 1. 慢 handler 不阻塞读循环（核心回归）
//    注入 sleep 2s 的 handler，同时发快指令
//    断言：快指令响应先到

// 2. 同一 upload_id 的分块严格串行（关键正确性）
//    并发投递 N 个 chunk
//    断言：handler 内部观察到的执行区间无重叠

// 3. 不同 upload_id 可并行
//    断言：两个上传的总耗时 ≈ 单个，而非累加

// 4. 队列满时快速拒绝
//    灌满队列
//    断言：收到 success=false + "设备繁忙"，且不超时

// 5. 连接断开时 worker 退出、在途任务取消
//    关闭连接
//    断言：wg.Wait() 在 1s 内返回

// 6. 竞态检测
//    go test -race ./internal/relay/...
```

现有 `client_test.go` 的 mock server 框架（`:80`、`:119`、`:198`、`:292`）可直接复用。

---

## 7. 风险与缓解

| 风险 | 严重度 | 缓解 |
|---|---|---|
| `project_files` 上传需 per-key 串行 | **高**（数据正确性） | 阶段一必须实现，已定位 |
| handler 无 ctx 无法强制取消 | 低 | 与现状一致，非新增 |
| 队列满时行为选择 | 低 | 快速拒绝，已设计 |
| 参数需实测调优 | 低 | 可配 + 观测指标 |
| 行为变化难本地验证 | 中 | 复用 mock server；灰度发布 |

**明确不做**（避免过度设计）：

- **不引入优先级调度** — 当前协议无明确优先级需求
- **不做任务持久化** — 连接断开即在途任务作废，与现状一致
- **不重构服务层锁** — `opsMu`/`mu`/`directoryFilesMu` 继续用，per-key 锁是**补充**而非替代

---

## 8. 实施清单

| # | 文件 | 改动 |
|---|---|---|
| 1 | `launcher/internal/relay/dispatcher.go`（新增） | `dispatcher`、`job`、`keyEntry`、`jobKey`、`busyEnvelope` |
| 2 | `launcher/internal/relay/client.go` | 读循环改造；`connect()` 加 `wg.Wait()`；未知指令日志 |
| 3 | `launcher/internal/relay/dispatcher_test.go`（新增） | 上述 5 个测试用例 |
| 4 | `launcher/internal/relay/client_test.go` | 复用现有 mock 框架补充集成测试 |

**预计改动规模**：新增约 200 行，修改约 40 行。

---

## 9. 结论

本方案解决的是**结构性扩散风险**，而非某个具体 bug。

- 上次的 models.dev 超时是**症状**，已通过异步化修复（0.1.188）
- 本方案处理的是**病因**：串行读循环让任何慢指令都能拖垮整机
- 唯一需要新增保护的是 `project_files` 上传族（已定位、已设计）
- 其余指令因服务层已有锁，并发化后天然安全

**建议时机**：根因已修复，此项属防御性改造。建议在下一个迭代窗口实施，并充分跑 `-race` 测试，不宜与业务改动混做。
