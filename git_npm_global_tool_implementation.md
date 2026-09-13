# Git 运行时 npm_global_tool 策略实现总结

## 实现日期
2026-09-03

## 实现内容

### 1. 配置文件修改

#### Backend Catalog (`backend/internal/app/builtin/catalog_v4.json`)
- 添加 `git` runtime 配置（sort_order: 21）
- 添加 `git-latest` version 配置
- install_strategy: `npm_global_tool`
- npm_package: `dugite`
- requires_runtime: `node`

#### Launcher Catalog (`launcher/internal/app/builtin/catalog_v4.json`)
- 添加 `git` runtime 配置（sort_order: 21）
- 添加 `git-latest` version 配置
- 配置与backend保持一致

### 2. 后端代码修改

#### `backend/internal/api/runtime_preflight.go`
**位置：** 第417行
**修改：** 添加 `npm_global_tool` 策略的依赖检测
```go
if strings.EqualFold(strings.TrimSpace(item.InstallStrategy), "npm_global_tool") {
    needsNode = true
    hasNodeTask = true
}
```

### 3. Launcher代码修改

#### `launcher/internal/app/runtime_preflight.go`

**修改1：依赖检测（第468行）**
```go
if normalizeRuntimeInstallStrategy(task.item.InstallStrategy) == "npm_global_tool" {
    needsNode = true
    hasNodeTask = true
}
```

**修改2：变量声明（第546行）**
```go
var needsNode bool
var hasNodeTask bool
```

**修改3：依赖验证（第556行）**
```go
if needsNode && !hasNodeTask {
    err := errors.New("npm_global_tool 安装策略依赖 Node.js，但运行时快照未下发 node")
    // ... 错误处理
}
```

**修改4：任务分组（第594行）**
```go
var npmToolTasks []*runtimePreflightTask
for _, t := range tasks {
    strategy := normalizeRuntimeInstallStrategy(t.item.InstallStrategy)
    if strategy == "npm_global_tool" {
        npmToolTasks = append(npmToolTasks, t)
    }
}
```

**修改5：分组添加（第636行）**
```go
if len(npmToolTasks) > 0 {
    groups = append(groups, runtimePreflightTaskGroup{name: "npm_global_tool", tasks: npmToolTasks})
}
```

**修改6：安装策略判断（第771行）**
```go
if strategy == "npm_global_tool" {
    return s.installNpmGlobalTool(ctx, task, manifest, nodeExePath, targetInstallDir)
}
```

**修改7：安装函数实现（第909行之后）**
```go
func (s *runtimePreflightService) installNpmGlobalTool(
    ctx context.Context,
    task *runtimePreflightTask,
    manifest installManifest,
    nodeExePath string,
    targetInstallDir string,
) (installedRuntimeManifest, error) {
    // 完整的安装函数实现
    // 包括：Node.js检测、npm包安装、可执行文件查找、manifest生成等
}
```

### 4. 实现特性

#### npm_global_tool 安装策略支持：
- ✅ 自动检测Node.js依赖
- ✅ 使用 `npm install -g` 全局安装npm包
- ✅ 自动查找已安装的可执行文件
- ✅ 生成运行时manifest
- ✅ 支持dugite（Git的npm包装）
- ✅ 错误处理和日志记录

#### 依赖管理：
- ✅ 检测是否需要Node.js运行时
- ✅ 验证Node.js是否已安装
- ✅ 按依赖顺序分组安装（Node.js → npm tools）

#### 任务分组：
- ✅ 将npm_global_tool任务单独分组
- ✅ 在Node.js安装完成后执行

### 5. 编译验证

#### Backend编译：
```bash
cd backend && go build -o /tmp/test-backend ./cmd/backend
# ✅ 编译成功
```

#### Launcher编译：
```bash
cd launcher && go build -o /tmp/test-launcher-git2 ./cmd/launcher
# ✅ 编译成功
```

### 6. 配置验证

#### Backend catalog验证：
```bash
cat backend/internal/app/builtin/catalog_v4.json | jq '.runtime_catalog[] | select(.id == "git")'
# ✅ 配置正确

cat backend/internal/app/builtin/catalog_v4.json | jq '.runtime_versions[] | select(.runtime_id == "git")'
# ✅ 配置正确
```

#### Launcher catalog验证：
```bash
cat launcher/internal/app/builtin/catalog_v4.json | jq '.runtime_catalog[] | select(.id == "git")'
# ✅ 配置正确

cat launcher/internal/app/builtin/catalog_v4.json | jq '.runtime_versions[] | select(.runtime_id == "git")'
# ✅ 配置正确
```

## 测试建议

### 集成测试步骤：

1. **启动后端服务**
   ```bash
   cd backend && ./backend
   ```

2. **查询Git运行时配置**
   ```bash
   curl http://localhost:8080/api/runtime-catalog | jq '.runtimes[] | select(.id == "git")'
   ```

3. **创建任务请求Git运行时**
   ```bash
   curl -X POST http://localhost:8080/api/tasks \
     -H "Content-Type: application/json" \
     -d '{
       "prompt": "测试Git运行时",
       "runtimes": ["git-latest"]
     }'
   ```

4. **验证安装过程**
   - 检查Node.js是否已安装
   - 检查是否执行 `npm install -g dugite`
   - 检查git命令是否可用

## 实现优势

1. **复用npm生态**：利用npm包管理器安装工具
2. **依赖自动化**：自动管理Node.js依赖
3. **扩展性强**：可轻松添加其他npm全局工具
4. **跨平台**：npm全局安装跨平台兼容
5. **版本管理**：通过npm管理工具版本

## 可扩展的npm工具示例

基于此实现，可以轻松添加其他npm全局工具：

- **TypeScript**: `npm install -g typescript` → `tsc`
- **ESLint**: `npm install -g eslint` → `eslint`
- **Prettier**: `npm install -g prettier` → `prettier`
- **Yarn**: `npm install -g yarn` → `yarn`
- **pnpm**: `npm install -g pnpm` → `pnpm`

只需在catalog中添加配置：
```json
{
  "id": "typescript-latest",
  "runtime_id": "typescript",
  "install_manifest": {
    "npm_package": "typescript",
    "requires_runtime": "node"
  }
}
```

## 注意事项

1. **Node.js版本要求**：确保Node.js版本支持目标npm包
2. **全局权限**：某些系统可能需要sudo权限进行全局安装
3. **PATH配置**：npm全局bin目录需在系统PATH中
4. **网络依赖**：npm安装需要网络连接

## 网络配置

### 代理支持

npm_global_tool 策略会自动继承系统环境变量，支持以下代理配置：

```bash
# 方式1：系统代理（自动继承）
export https_proxy=http://127.0.0.1:7897
export http_proxy=http://127.0.0.1:7897
export all_proxy=socks5://127.0.0.1:7897

# 启动launcher，代理设置会自动生效
./launcher
```

### npm镜像支持（推荐）

优先使用国内镜像，无需代理，速度更快：

```bash
# 方式1：通过环境变量（优先级最高）
export LAUNCHER_NPM_REGISTRY=https://registry.npmmirror.com
./launcher

# 方式2：通过npm全局配置
npm config set registry https://registry.npmmirror.com
./launcher

# 方式3：在代码中自动读取NPM_CONFIG_REGISTRY环境变量
export NPM_CONFIG_REGISTRY=https://registry.npmmirror.com
./launcher
```

### 镜像优先级

1. `LAUNCHER_NPM_REGISTRY` 环境变量（最高优先级）
2. `NPM_CONFIG_REGISTRY` 环境变量
3. npm 全局配置（~/.npmrc）
4. npm 默认源（registry.npmjs.org）

### 常用npm镜像

- **淘宝镜像**：`https://registry.npmmirror.com`（推荐）
- **腾讯云镜像**：`https://mirrors.cloud.tencent.com/npm/`
- **华为云镜像**：`https://mirrors.huaweicloud.com/repository/npm/`
- **npm官方源**：`https://registry.npmjs.org`（需代理或国外网络）

### 网络故障排查

如果npm install失败，按以下顺序排查：

1. **检查网络连接**
   ```bash
   curl -I https://registry.npmmirror.com
   ```

2. **测试npm配置**
   ```bash
   npm config get registry
   ```

3. **验证代理设置**
   ```bash
   echo $https_proxy
   echo $LAUNCHER_NPM_REGISTRY
   ```

4. **查看launcher日志**
   - 日志会显示实际执行的npm命令
   - 检查是否正确传递了 `--registry` 参数

## 下一步

建议进行完整的端到端测试，验证：
- [ ] Git运行时能否正确安装
- [ ] dugite是否正常工作
- [ ] git命令是否可用
- [ ] 依赖顺序是否正确（先Node.js后Git）
- [ ] 错误处理是否完善
