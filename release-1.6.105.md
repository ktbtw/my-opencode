# Release 1.6.105

## Versions

- Flutter app: `1.6.105+252`
- OpenCode Agent: `1.15.104`
- Backend: 本次无独立用户版本号（按 Git commit / 发布日标识）
- Launcher / OpenCode CLI: 本次不发布

## 背景（生产问题）

用户发送 TXT 附件时任务必定失败，报英文错误：

```
'file part media type application/octet-stream' functionality not supported.
```

排查结论（均以生产数据与接口实测确认，不是静态推断）：

1. 第一次测试（旧客户端拖拽上传）把 `.txt` 标记成 `application/octet-stream` 发出，该 part 被写进会话历史。
2. Agent 每轮都会把整段历史重发给模型，历史里那条 `application/octet-stream` 附件被当作 media file part 透传给 OpenAI 兼容供应商，供应商直接抛错 → 整个任务失败。
3. 因此**该会话之后每一轮都失败**，即使后续消息发送的是正确的 `text/plain`，甚至不带任何附件也会失败（已用接口实测复现：向该会话发送纯文本消息同样报错）。
4. 客户端 1.6.104 已修正拖拽的 mime（新发送的附件都是 `text/plain`），但历史污染无法自愈。

## Changes

### OpenCode Agent（1.15.104）

- `session/message-v2.ts`：非媒体附件不再作为 file part 透传。文本内容直接内联为 text part，无法作为文本解码时降级为中文提示；图片 / PDF 的原有媒体路径不变。
- `provider/transform.ts`：无法映射成模型输入类型的 file part 不再原样放行，改为中文提示，避免供应商错误中断整个请求；模型不支持某模态时的提示文案一并中文化。
- `util/data-url.ts`：新增 `decodeDataUrlBytes` / `decodeTextDataUrl`，只有通过 UTF-8 校验且不含 NUL 的负载才按文本内联。

### 客户端（1.6.104 → 1.6.105）

- 文本类附件（txt/md/csv/json/源码等）统一按 `text/plain` 发送，拖拽与选择器判断一致；不支持的附件错误提示改为中文。
- 修复历史消息丢失附件的问题：`TaskModel` 现在解析 `parts` 中的 file part 并回填到消息，重进对话仍能看到附件预览；只发附件不打字的消息也不再被丢弃。
- 新增附件详情页：图片支持双指缩放、旋转与保存；文本 / JSON / CSV / 代码类附件在新页面打开，可编辑、复制并带语法高亮（JSON / YAML / XML / Markdown / 日志 / 通用代码）。
- 历史附件超过 4MB 时只保留文件名与类型，避免长会话把 base64 全部驻留内存，页面会提示内容未保留。

## Verification

- OpenCode：`bun run typecheck` 通过；`bun test test/session/message-v2.test.ts` 46 项通过；新增 5 条回归（octet-stream 文本内联、二进制降级提示、图片仍走媒体、transform 兜底两条）。
- Flutter：`flutter analyze` 无新增问题；`flutter test` 530 项全部通过；新增历史附件回填 2 条、附件详情页 4 条、语法高亮 6 条回归。
- 生产复现证据：agent 本地 `GET /session/{id}/message` 显示历史中存在 `mime=application/octet-stream` 的 file part；向该会话发送无附件消息同样报同一错误，删除测试消息后恢复原状。

## 遗留

- 已被污染的会话不需要特殊处理：新版本 Agent 会把历史里的 octet-stream 文本附件按文本内联，旧会话可直接继续使用。

## Rollback

- 客户端：上一版 APK / EXE / DMG 与 `version.json` 保留在发布目录。
- Agent：Launcher 保留历史版本目录，可回退到上一版本。
