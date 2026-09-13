# 逆向与安全 MCP 调研总览

整理时间：2026-06-06  
来源：用户粘贴清单中的 84 个 GitHub 仓库引用，以及各仓库公开 README、依赖清单和启动配置片段。粘贴文本正文写“83 个仓库”，但说明文字中额外引用了 `dnakov/frida-mcp`，所以本目录按 84 个仓库覆盖。

## 文件说明

- [reverse-mcp-inventory.md](./reverse-mcp-inventory.md)：逐仓库清单，包含 MCP 描述、运行环境、依赖安装、启动方式、客户端配置和确认状态。

## 通用环境

这些 MCP 覆盖了多种生态，不能用同一个启动方式统一运行。建议本机准备以下基础环境，再按单个仓库条目补齐专用工具：

- Python：建议 3.10 至 3.12；部分项目声明 3.8+、3.11+、3.13+ 或 3.12。
- Python 包管理：`pip`、`pipx`、`uv`。
- Node.js：多数 JS MCP 需要 Node 16+，新项目如 `vmoranv/jshookmcp` 需要 Node 22.12+ 与 pnpm 10。
- Docker / Docker Compose：安全工具 Hub、恶意软件分析、Pwno、Reversecore、Revula 等项目需要容器。
- Rust / Cargo：`blacktop/ida-mcp-rs`、`pansila/mcp_server_gdb`、`dwmetz/MalChela` 等需要。
- Java / Gradle：Ghidra 插件、JADX、Burp 扩展相关项目需要。
- Windows + Visual Studio / MSVC / CMake：x64dbg 插件类项目通常需要。

## 专用工具环境

- IDA Pro：IDA MCP 和 headless IDA 项目通常需要 IDA Pro 授权，很多项目明确不支持 IDA Free。
- Ghidra：Ghidra 插件或 headless Ghidra 项目需要 Ghidra 安装目录、Java 运行时和扩展安装。
- Binary Ninja：Binary Ninja MCP 或 headless MCP 需要本地 Binary Ninja 授权和 Python API。
- x64dbg / x32dbg：x64dbg MCP 多数是 Windows 调试器插件，需要把 `.dp64` / `.dp32` 复制到插件目录。
- Frida：移动和运行时注入项目需要 `frida`、`frida-tools`，Android/iOS 还需要设备侧 `frida-server`。
- Wireshark / tshark：`WireMCP` 需要 `tshark` 在 PATH 中可用。
- Burp Suite / OWASP ZAP：相关 MCP 需要本地代理工具已启动，并开放 API 或 SSE/HTTP 服务。
- Android 逆向工具：`adb`、`apktool`、`jadx`、`frida-tools`、`objection`、Java。
- 调试器：GDB、LLDB、pwndbg、x64dbg、WinDbg/KD 按项目需要安装。

## MCP 启动形态

- stdio server：最适合直接导入 MCP 客户端，配置 `command` 和 `args` 即可，例如 `python -m ...`、`uvx ...`、`npx -y ...`。
- HTTP / SSE server：需要先启动本地服务，再在支持 HTTP/SSE 的客户端配置 URL；不支持 SSE 的客户端可使用 `mcp-proxy` 或项目自带代理。
- 工具内插件：IDA、Ghidra、Binary Ninja、x64dbg、Burp 这类通常先在宿主工具内安装插件，再由插件暴露 HTTP/SSE 或由外部 stdio bridge 连接。
- 索引 / Skill / Agent：不是独立 MCP server，不应直接作为 MCP `command` 导入；它们用于安装技能、查看项目索引或在宿主工具里运行 agent。

## 常见客户端配置模板

stdio MCP：

```json
{
  "mcpServers": {
    "server-name": {
      "command": "python",
      "args": ["-m", "module_name"]
    }
  }
}
```

Node MCP：

```json
{
  "mcpServers": {
    "server-name": {
      "command": "npx",
      "args": ["-y", "package-name"]
    }
  }
}
```

SSE/HTTP bridge：

```json
{
  "mcpServers": {
    "server-name": {
      "command": "mcp-proxy",
      "args": ["http://127.0.0.1:8888/sse"]
    }
  }
}
```

Docker stdio MCP：

```json
{
  "mcpServers": {
    "server-name": {
      "command": "docker",
      "args": ["run", "-i", "--rm", "image-name:latest"]
    }
  }
}
```

## 状态标记

- 可直接启动：README 或依赖清单中有明确 stdio/HTTP/Docker 启动命令。
- 需宿主插件：需要先安装到 IDA、Ghidra、Binary Ninja、x64dbg、Burp、JADX 等宿主工具。
- 待确认：公开 README 信息不足，或只给了项目描述，没有完整启动/配置命令。
- 非 MCP Server：awesome list、skill pack、agent、工作流仓库或近邻工具，不建议直接导入 MCP 客户端。

