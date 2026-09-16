# Workflow Preferences

- Wants every code change to end with a version number bump plus version notes (更新版本号/版本说明) before building/deploying. Confidence: 0.9
- Prefers deploying/building only the components that actually changed, and objects to blanket "deploy all" (e.g. running `deploy.sh all` for a Flutter-only change). Confidence: 0.85
- Prefers full git commits as backups (全量提交), but scoped deployment of just the changed parts. Confidence: 0.8
- Before a full release, wants a client build (e.g. APK/EXE) compiled and installed on their own device so they can test it personally. Confidence: 0.85
- Expects real, production-like end-to-end testing (including edge cases) before shipping; repeatedly asks "都测试完成了吗". Confidence: 0.85
- Prefers parallel/async execution for installs, downloads, uploads, and reporting; also wants chunked parallel transfer for large files. Confidence: 0.8
- Strongly prefers domestic (China) mirrors over GitHub/official sources when downloading runtimes, packages, and dependencies ("镜像下载优先","不要走 github"). Confidence: 0.9
- Diagnoses problems via server/cloud logs and expects detailed, per-module timing and step logs to be added to help future comparison. Confidence: 0.8
- Wants plans, checklists, unfinished-item lists, and design docs recorded into local files (markdown) rather than only kept in chat. Confidence: 0.8
- Does not want unnecessary rebuilds/redeploys of artifacts (e.g. re-uploading the runtime env or rebuilding when nothing changed). Confidence: 0.75
