# 逆向与安全 MCP 逐项清单

整理时间：2026-06-06  
说明：以下条目按用户粘贴清单覆盖 84 个 GitHub 仓库。每个条目都给出公开仓库链接、MCP 描述、所需环境、依赖安装、启动方式、客户端配置要点和当前确认状态。若 README 没有给出完整命令，条目标为“待确认”，不伪造可执行配置。

## 快速分类

- 可直接启动：`0x4m4/hexstrike-ai`、`0xKoda/WireMCP`、`0xhackerfren/frida-game-hacking-mcp`、`FuzzingLabs/mcp-security-hub`、`FuzzingLabs/secpipe`、`FuzzySecurity/kahlo-mcp`、`Gaffx/volatility-mcp`、`Ipiano/gdb-mcp`、`MCPPhalanx/binaryninja-mcp`、`MeroZemory/ida-multi-mcp`、`MxIris-Reverse-Engineering/ida-mcp-server`、`NoOne-hub/JSReverser-MCP`、`RocketMaDev/pwndbg-mcp`、`Sarks0/binary-mcp`、`ThreatFlux/YaraFlux`、`Wael-Rd/ultimate-mobile-mcp`、`X3r0K/BurpSuite-MCP-Server`、`ZSA233/frida-analykit`、`ajtazer/ZAP-MCP`、`blacktop/ida-mcp-rs`、`cnitlrt/headless-ida-mcp-server`、`digitalandrew/wairz`、`eversinc33/TriageMCP`、`jtsylve/re-mcp`、`kggzs/ApkMCP-Auto`、`mrexodia/ida-pro-mcp`、`mrphrazer/binary-ninja-headless-mcp`、`mrphrazer/ghidra-headless-mcp`、`pansila/mcp_server_gdb`、`president-xd/revula`、`pwno-io/pwno-mcp`、`rsprudencio/binja_mcp`、`signal-slot/mcp-gdb`、`sjkim1127/Reversecore_MCP`、`smadi0x86/MDB-MCP`、`swgee/BurpMCP`、`vichhka-git/android-reverse-engineering-mcp-server`、`vmoranv/jshookmcp`、`vwww-droid/Mira`、`wuji66dde/jshook-reverse-tool`、`zhizhuodemao/frida-mcp`、`dnakov/frida-mcp`、`zinja-coder/apktool-mcp-server`、`zinja-coder/jadx-mcp-server`。
- 需宿主插件或宿主工具绑定：`AgentSmithers/x64DbgMCPServer`、`CaptainNox/x64dbg-mcp`、`Invoke-RE/binja-lattice-mcp`、`LaurieWired/GhidraMCP`、`P4nda0s/IDA-NO-MCP`、`PortSwigger/mcp-server`、`QiuChenly/ida-pro-mcp-enhancement`、`Qtty/jadx-mcp-server`、`SetsunaYukiOvO/x64dbg-mcp`、`Wasdubya/x64dbgMCP`、`ap425q/CutterMCP`、`bbgouzi123/x64dbg-mcp`、`bethington/ghidra-mcp`、`bromoket/x64dbg_mcp`、`buzzer-re/Rikugan`、`cyberkaida/reverse-engineering-assistant`、`fdrechsler/mcp-server-idapro`、`llnl/OGhidra`、`m4rba4s/mcp_debugger`、`radareorg/r2ai`、`s4dp4nd4/frida-c2-mcp`、`starsong-consulting/GhydraMCP`、`suidpit/ghidra-mcp`、`symgraph/BinAssistMCP`、`symgraph/GhidrAssistMCP`、`symgraph/IDAssist`、`taida957789/ida-mcp-server-plugin`、`zinja-coder/jadx-ai-mcp`。
- 非 MCP Server / 索引 / Skill / 工作流：`P4nda0s/reverse-skills`、`crowdere/Awesome-RE-MCP`、`cycraft-corp/BinaryAnalysisMCPs`、`darbra/awesome-ai-reverse`、`dwmetz/MalChela`、`mrphrazer/agentic-malware-analysis`、`punkpeye/awesome-mcp-servers`、`yzfly/Awesome-MCP-ZH`、`zhaoxuya520/reverse-skill`、`zhizhuodemao/ai-reverse-toolkit`。

## 逐项清单

### 0x4m4/hexstrike-ai

- 链接：https://github.com/0x4m4/hexstrike-ai
- 状态：可直接启动，偏大型安全工具编排平台。
- MCP 描述：HexStrike AI MCP Agents v6.0，提供 150+ 安全工具和多智能体安全自动化能力。
- 环境：Python 3.8+；大量外部安全工具；浏览器能力需要 Chromium/Chrome 与 chromedriver；部分功能需要 Docker、Kubernetes 安全工具等。
- 依赖安装：`python3 -m venv hexstrike-env`，激活后执行 `pip3 install -r requirements.txt`，再按 README 安装 nmap、ffuf、sqlmap、gobuster、nuclei 等安全工具。
- 启动：`python3 hexstrike_server.py`；调试：`python3 hexstrike_server.py --debug`；自定义端口：`python3 hexstrike_server.py --port 8888`。
- 配置：MCP 客户端连接本地 HexStrike MCP server；若使用端口模式，需与客户端配置保持一致。

### 0xKoda/WireMCP

- 链接：https://github.com/0xKoda/WireMCP
- 状态：可直接启动，Node stdio MCP。
- MCP 描述：基于 Wireshark `tshark` 的实时网络流量分析 MCP。
- 环境：Wireshark/tshark 可执行文件在 PATH 中；Node.js 16+；npm。
- 依赖安装：`git clone https://github.com/0xkoda/WireMCP.git`，进入目录后执行 `npm install`。
- 启动：`node index.js`。
- 配置：客户端配置 `command: "node"`，`args: ["/ABSOLUTE_PATH_TO/WireMCP/index.js"]`。

### 0xhackerfren/frida-game-hacking-mcp

- 链接：https://github.com/0xhackerfren/frida-game-hacking-mcp
- 状态：可直接启动，Python stdio MCP。
- MCP 描述：通过 Frida 提供类似 Cheat Engine 的内存扫描、修改、pattern scan、hook 和代码注入能力。
- 环境：Python 3.10+；Frida；Windows 额外需要 `pywin32`；Android/iOS 需要设备端 frida-server。
- 依赖安装：`pip install frida-game-hacking-mcp`，或源码安装 `pip install -e .`；手动依赖为 `pip install frida frida-tools mcp pillow`，Windows 加 `pip install pywin32`。
- 启动：`python -m frida_game_hacking_mcp` 或 `frida-game-hacking-mcp`。
- 配置：Claude Desktop 示例为 `command: "python"`，`args: ["-m", "frida_game_hacking_mcp"]`。

### AgentSmithers/x64DbgMCPServer

- 链接：https://github.com/AgentSmithers/x64DbgMCPServer
- 状态：需宿主插件；Windows/x64dbg HTTP/SSE 桥接。
- MCP 描述：C# x64dbg/x32dbg 插件，在调试器内暴露 HTTP/SSE 接口供 MCP 客户端控制调试器。
- 环境：Windows；x64dbg/x32dbg；.NET Framework/C# 构建环境；需要把插件部署到 x64dbg 插件目录。
- 依赖安装：README 提供 Visual Studio 构建和 post-build 复制示例，例如复制到 `...\x96\release\x64\plugins\x64DbgMCPServer`。
- 启动：启动 x64dbg 并加载插件，插件监听 `http://localhost:50300` / `/sse`。
- 配置：支持 SSE 的客户端可配置 URL；Claude Desktop 需 `MCPProxy-STDIO-to-SSE.exe`，示例 `args: ["http://localhost:50300"]`。

### BlackSnufkin/LitterBox

- 链接：https://github.com/BlackSnufkin/LitterBox
- 状态：可启动，但更偏沙箱/分析平台，MCP 能力在 GrumpyCats/LitterBoxMCP 相关文档中。
- MCP 描述：自托管 payload 分析沙箱，支持静态、动态、EDR 分析和检测评分。
- 环境：Python 3.11+；Windows 管理员 shell 或 Linux Docker/KVM；可选 EDR VM。
- 依赖安装：`python -m venv venv`，激活后 `pip install -r requirements.txt`。
- 启动：Windows 下 `python litterbox.py`；Linux Docker 方式进入 `LitterBox/Docker` 后执行 `./setup.sh`。
- 配置：Web UI 默认 `http://127.0.0.1:1337`；MCP 配置需按仓库 Wiki 的 LitterBoxMCP/GrumpyCats 文档补齐。

### CaptainNox/x64dbg-mcp

- 链接：https://github.com/CaptainNox/x64dbg-mcp
- 状态：待确认/需 x64dbg 绑定；README 很短，依赖清单显示为 Python MCP。
- MCP 描述：基于 `x64dbg_automate` 的 x64dbg MCP server。
- 环境：Python 3.13+；x64dbg；`x64dbg-automate`；Windows 调试环境。
- 依赖安装：`pyproject.toml` 声明依赖 `bottle`、`fastmcp`、`requests`、`x64dbg-automate`。
- 启动：公开 README 未给出完整启动命令；需要按项目源码确认 module/entry point。
- 配置：确认入口后按 stdio 或 HTTP 方式导入 MCP 客户端；同时需保证 x64dbg 自动化接口可连接。

### FuzzingLabs/mcp-security-hub

- 链接：https://github.com/FuzzingLabs/mcp-security-hub
- 状态：可直接启动，Docker 化 MCP 集合。
- MCP 描述：生产化 offensive security MCP server 集合，覆盖 38 个 MCP、300+ 安全工具。
- 环境：Docker；Docker Compose；部分工具需要网络能力、卷挂载或额外 Linux capability。
- 依赖安装：`git clone https://github.com/FuzzingLabs/mcp-security-hub`，进入目录后 `docker-compose build`。
- 启动：例如 `docker-compose up nmap-mcp nuclei-mcp -d`；单个 MCP 可用 `docker run -i --rm ... image:latest`。
- 配置：Claude/Cursor 中常见配置为 `command: "docker"`，`args: ["run", "-i", "--rm", "--cap-add=NET_RAW", "nmap-mcp:latest"]`；源码内 examples 提供完整模板。

### FuzzingLabs/secpipe

- 链接：https://github.com/FuzzingLabs/secpipe
- 状态：可直接启动，安全研究编排 MCP。
- MCP 描述：通过 MCP 编排安全研究工具、fuzzer 和 `mcp-security-hub`。
- 环境：Python/uv；Docker 或 Podman；可选 FuzzingLabs MCP Hub。
- 依赖安装：README 示例为 `git clone https://github.com/FuzzingLabs/secpipe_ai.git`，进入目录后 `uv sync`；可拉取 hub 到 `~/.secpipe/hubs/mcp-security-hub`。
- 启动：README 描述为 stdio MCP server；具体命令需按项目 CLI/pyproject 入口运行，通常使用 `uv run ...`。
- 配置：需要配置 hub 路径、Docker/Podman 运行环境和目标工具。

### FuzzySecurity/kahlo-mcp

- 链接：https://github.com/FuzzySecurity/kahlo-mcp
- 状态：可直接启动，Node 构建后的 stdio MCP。
- MCP 描述：Frida Kahlo MCP，用于 Frida/运行时分析。
- 环境：Node.js；npm；Frida 工具链；目标设备或本机进程。
- 依赖安装：进入 `kahlo-mcp` 后 `npm install`，再 `npm run build`。
- 启动：`node /Your/Full/Path/k4hlo/kahlo-mcp/dist/index.js`。
- 配置：客户端配置示例为 `command: "node"`，`args: ["/Your/Full/Path/k4hlo/kahlo-mcp/dist/index.js"]`，并可设置 `cwd`。

### Gaffx/volatility-mcp

- 链接：https://github.com/Gaffx/volatility-mcp
- 状态：可直接启动，但 README 启动命令较简略。
- MCP 描述：Volatility 内存取证相关 MCP。
- 环境：Python；Volatility3 相关依赖；内存镜像文件和 symbol 环境。
- 依赖安装：`pip install -r requirements.txt`。
- 启动：README 未给出清晰命令；需要根据源码入口确认 server 脚本。
- 配置：配置内存镜像路径、Volatility 插件路径和 MCP 客户端 command。

### Invoke-RE/binja-lattice-mcp

- 链接：https://github.com/Invoke-RE/binja-lattice-mcp
- 状态：需 Binary Ninja / Lattice API 绑定。
- MCP 描述：BinjaLattice，面向 Binary Ninja/Lattice 的交互式分析客户端。
- 环境：Python；Binary Ninja 或 Lattice 服务；API key/用户名密码；可选 SSL。
- 依赖安装：`python -m venv .venv`，激活后 `pip install -r requirements.txt`。
- 启动：README 示例为 `python lattice_client.py --host localhost --port 9000 --username user --password YOUR_API_KEY`。
- 配置：需要配置 host、port、SSL、用户名和 API key；作为 MCP 使用前需确认是否有独立 MCP server 入口。

### Ipiano/gdb-mcp

- 链接：https://github.com/Ipiano/gdb-mcp
- 状态：可直接启动，GDB MCP。
- MCP 描述：GDB MCP Server，用于让 MCP 客户端控制 GDB。
- 环境：Python；pipx；GDB。
- 依赖安装：`python3 -m pip install --user pipx`，`python3 -m pipx ensurepath`，进入源码后 `pipx install .`。
- 启动：`gdb-mcp-server`。
- 配置：可设置 `GDB_MCP_LOG_LEVEL=DEBUG`；客户端配置 command 为 `gdb-mcp-server`。

### LaurieWired/GhidraMCP

- 链接：https://github.com/LaurieWired/GhidraMCP
- 状态：需 Ghidra 插件 + Python bridge。
- MCP 描述：Ghidra MCP 插件/桥接，暴露反编译、函数、字符串、注释等能力。
- 环境：Ghidra；Java；Python；`requirements.txt` 依赖；MCP 客户端。
- 依赖安装：安装 Ghidra 扩展后，在桥接脚本目录执行 `pip install -r requirements.txt`。
- 启动：先在 Ghidra 中启用插件，再运行 `python bridge_mcp_ghidra.py --transport sse --mcp-host 127.0.0.1 --mcp-port 8081 --ghidra-server http://127.0.0.1:8080/`。
- 配置：客户端连接 bridge 的 SSE/HTTP 地址；Ghidra 插件默认服务地址需与 `--ghidra-server` 一致。

### MCPPhalanx/binaryninja-mcp

- 链接：https://github.com/MCPPhalanx/binaryninja-mcp
- 状态：可直接启动，但需要 Binary Ninja API/授权。
- MCP 描述：Binary Ninja MCP server，支持直接分析一个或多个二进制文件。
- 环境：Python/uv；Binary Ninja；Binary Ninja Python API。
- 依赖安装：首次执行 `uvx binaryninja-mcp install-api`；源码开发用 `uv venv`、`uv sync --dev`。
- 启动：`uvx binaryninja-mcp server <filename> [filename]...`。
- 配置：客户端 command 可使用 `uvx`，args 为 `["binaryninja-mcp", "server", "/path/to/binary"]`；需要确保 Binary Ninja API 已安装。

### MeroZemory/ida-multi-mcp

- 链接：https://github.com/MeroZemory/ida-multi-mcp
- 状态：可直接启动，IDA 多实例 router。
- MCP 描述：为多个 IDA 会话提供 stdio MCP router。
- 环境：IDA Pro；Python 3.11；pipx 或 pip；MCP 客户端。
- 依赖安装：`pipx install git+https://github.com/MeroZemory/ida-multi-mcp.git`，或 `python3.11 -m pip install --user git+https://github.com/MeroZemory/ida-multi-mcp.git`。
- 启动：先执行 `ida-multi-mcp --install` 安装 IDA 插件；客户端可用 `ida-multi-mcp` 作为 command。
- 配置：Claude Code 示例 `claude mcp add ida-multi-mcp -s user -- ida-multi-mcp`。

### MxIris-Reverse-Engineering/ida-mcp-server

- 链接：https://github.com/MxIris-Reverse-Engineering/ida-mcp-server
- 状态：可直接启动，但需要 IDA 宿主配合。
- MCP 描述：IDA MCP Server，用于 IDA Pro 逆向辅助。
- 环境：Python；IDA Pro；MCP client。
- 依赖安装：`pip install mcp-server-ida`。
- 启动：`python -m mcp_server_ida`；开发调试可用 `npx @modelcontextprotocol/inspector uvx mcp-server-ida`。
- 配置：客户端 command 可为 `python`，args 为 `["-m", "mcp_server_ida"]`；IDA 端按项目说明安装/连接。

### NoOne-hub/JSReverser-MCP

- 链接：https://github.com/NoOne-hub/JSReverser-MCP
- 状态：可直接启动，Node/TypeScript JS 逆向 MCP。
- MCP 描述：JS 逆向任务、参数 workflow、知识库和任务管理 MCP。
- 环境：Node.js；npm；构建后的 `build/src/index.js`。
- 依赖安装：`npm install` 后构建项目。
- 启动：README 示例多为 `node build/src/index.js --doctor`、`node build/src/index.js --manageReverseTask ...`；MCP server 入口应为 `node build/src/index.js`。
- 配置：客户端 command 为 `node`，args 指向 `build/src/index.js`；可按需要传入工作流或任务管理参数。

### P4nda0s/IDA-NO-MCP

- 链接：https://github.com/P4nda0s/IDA-NO-MCP
- 状态：需 IDA 插件绑定/待确认。
- MCP 描述：IDA NO MCP，面向 IDA 的交互优化或插件项目。
- 环境：IDA Pro；Python/IDA Python。
- 依赖安装：README 未给出清晰独立依赖命令。
- 启动：公开 README 未确认可作为独立 MCP server 启动。
- 配置：应先按 IDA 插件方式安装，再确认是否暴露 MCP/HTTP/stdio 入口。

### P4nda0s/reverse-skills

- 链接：https://github.com/P4nda0s/reverse-skills
- 状态：非 MCP Server，Skill 包。
- MCP 描述：逆向工程技能集，不是独立 MCP 进程。
- 环境：支持 skills 的客户端/运行时；Node/npm。
- 依赖安装：README 示例 `npx skills add P4nda0s/reverse-skills`。
- 启动：无独立 MCP server 启动命令。
- 配置：用于安装/更新/移除技能，例如 `npx skills check`、`npx skills update`。

### PortSwigger/mcp-server

- 链接：https://github.com/PortSwigger/mcp-server
- 状态：需 Burp Suite 扩展和代理桥接。
- MCP 描述：Burp Suite MCP Server Extension。
- 环境：Burp Suite；Java；PortSwigger 扩展；可选 MCP proxy。
- 依赖安装：`git clone https://github.com/PortSwigger/mcp-server.git` 后按扩展构建/安装。
- 启动：Burp 扩展启动后使用 proxy，例如 `/path/to/packaged/burp/java -jar /path/to/proxy/jar/mcp-proxy-all.jar --sse-url http://127.0.0.1:9876`。
- 配置：客户端连接 proxy 或 SSE URL；端口需与 Burp 扩展一致。

### QiuChenly/ida-pro-mcp-enhancement

- 链接：https://github.com/QiuChenly/ida-pro-mcp-enhancement
- 状态：需 IDA 插件，Python stdio MCP。
- MCP 描述：`mrexodia/ida-pro-mcp` 增强版，支持 IDA 插件与 MCP 进程协同。
- 环境：IDA Pro；Python；uv 或 pip；MCP 客户端。
- 依赖安装：`pip install https://github.com/QiuChenly/ida-pro-mcp-enhancement/archive/refs/heads/main.zip`，或源码 `uv venv && uv pip install -e .`。
- 启动：执行 `ida-pro-mcp --install` 安装插件；客户端 command 使用 `ida-pro-mcp`。
- 配置：需打开 IDA 并加载插件；多个客户端会各自启动 stdio MCP 进程连接 IDA 插件。

### Qtty/jadx-mcp-server

- 链接：https://github.com/Qtty/jadx-mcp-server
- 状态：需 JADX/Java 项目构建，启动命令待确认。
- MCP 描述：基于 Spring Boot/JADX 的 APK 分析 MCP server。
- 环境：Java；JADX；Maven/Gradle 或 Spring Boot 运行环境。
- 依赖安装：README 展示 `src/main/java/...` 结构，但未给出明确构建命令。
- 启动：需要根据项目构建文件确认 `java -jar` 或 Gradle/Maven 启动方式。
- 配置：需要配置 APK 输入路径、JADX 环境和 MCP 端口/transport。

### RocketMaDev/pwndbg-mcp

- 链接：https://github.com/RocketMaDev/pwndbg-mcp
- 状态：可直接启动，pwndbg/GDB MCP。
- MCP 描述：让 AI agent 具备调试 ELF 的 pwndbg 能力。
- 环境：uv；GDB；pwndbg；Linux 调试环境。
- 依赖安装：`uv tool install pwndbg-mcp`。
- 启动：`pwndbg-mcp`。
- 配置：客户端 command 为 `pwndbg-mcp`；目标 ELF 和调试参数按工具调用传入。

### Sarks0/binary-mcp

- 链接：https://github.com/Sarks0/binary-mcp
- 状态：可直接启动，跨工具二进制分析 MCP。
- MCP 描述：整合 Ghidra、Python、x64dbg、WinDbg/KD、ILSpyCmd 等的 FastMCP server。
- 环境：Python/uv；可选 Ghidra、x64dbg、WinDbg、ILSpyCmd；Windows/Linux/macOS 依工具而定。
- 依赖安装：Windows 可 `irm https://raw.githubusercontent.com/Sarks0/binary-mcp/main/install.ps1 | iex`；Linux/macOS 可 `curl -sSL https://raw.githubusercontent.com/Sarks0/binary-mcp/main/install.py | python3 -`；源码为 `uv sync`。
- 启动：Claude 示例 `claude mcp add binary-analysis -- uv --directory /path/to/binary-mcp run python -m src.server`。
- 配置：按启用的后端工具配置路径和权限。

### SetsunaYukiOvO/x64dbg-mcp

- 链接：https://github.com/SetsunaYukiOvO/x64dbg-mcp
- 状态：需 x64dbg 插件。
- MCP 描述：x64dbg MCP Server Plugin，支持 x64dbg/x32dbg。
- 环境：Windows；x64dbg/x32dbg；C++ 构建工具；vcpkg；或直接使用 release 插件。
- 依赖安装：源码构建需 `git clone https://github.com/Microsoft/vcpkg.git C:\vcpkg` 和项目依赖；也可复制 `dist\x64dbg_mcp.dp64` / `dist\x32dbg_mcp.dp32`。
- 启动：复制插件到 `<x64dbg-path>\x64\plugins\` 或 `<x64dbg-path>\x32\plugins\` 后启动调试器。
- 配置：复制 `config.json` 到插件配置目录；客户端连接插件暴露的接口。

### ThreatFlux/YaraFlux

- 链接：https://github.com/ThreatFlux/YaraFlux
- 状态：可直接启动，Docker/Python YARA MCP。
- MCP 描述：YaraFlux MCP Server，提供 YARA 规则管理、扫描和恶意软件分析工具。
- 环境：Docker 或 Python；YARA；规则目录；MCP 客户端。
- 依赖安装：Docker 方式 `docker pull threatflux/yaraflux-mcp-server:latest`；源码可用 `requirements.txt` / `pyproject.toml`。
- 启动：`docker run -p 8000:8000 threatflux/yaraflux-mcp-server:latest`。
- 配置：Claude Desktop 可配置 Docker stdio 或 HTTP server；需挂载规则目录和样本目录。

### Wael-Rd/ultimate-mobile-mcp

- 链接：https://github.com/Wael-Rd/ultimate-mobile-mcp
- 状态：可直接启动，Node 移动安全 MCP。
- MCP 描述：Ultimate Mobile Pentest MCP，覆盖 Android/iOS 移动安全、静态分析、Frida、MobSF、ADB 等。
- 环境：Node/npm；`adb`、`apktool`、`jadx`、Python、Frida tools、objection；可选 MobSF Docker。
- 依赖安装：README 示例 `sudo apt install -y adb apktool jadx python3-pip npm`，`pip3 install frida-tools objection`，MobSF 可 `docker run -it --rm -p 8000:8000 opensecurity/mobsf:latest`；项目内 `npm install`。
- 启动：克隆项目后按 package.json 入口启动，通常为 Node MCP stdio。
- 配置：配置目标设备、MobSF 地址、Frida/ADB 路径和 MCP 客户端 command。

### Wasdubya/x64dbgMCP

- 链接：https://github.com/Wasdubya/x64dbgMCP
- 状态：需 x64dbg 插件 + Python MCP agent。
- MCP 描述：x64dbg/x32dbg 插件和 Python MCP server，提供 40+ x64dbg SDK 调试工具。
- 环境：Windows；x64dbg/x32dbg；Python；`mcp`、`requests`；构建源码需 CMake 和 MSVC。
- 依赖安装：下载 `.dp64` 或 `.dp32` 放到 `[x64dbg_dir]/release/x64/plugins/`；复制 `src/x64dbgmcp.py`，并执行 `pip install mcp requests`。
- 启动：启动 x64dbg 后启动 MCP 客户端，客户端通过 Python 脚本连接插件。
- 配置：Claude 示例 `command: "Path\\To\\Python"`，`args: ["Path\\to\\x64dbg.py"]`。

### X3r0K/BurpSuite-MCP-Server

- 链接：https://github.com/X3r0K/BurpSuite-MCP-Server
- 状态：可直接启动，Python Burp MCP。
- MCP 描述：BurpSuite MCP Server，通过 Burp API/代理能力暴露安全测试工具。
- 环境：Python；Burp Suite；Burp API/代理配置。
- 依赖安装：`git clone https://github.com/X3r0K/BurpSuite-MCP-Server.git`，进入目录后 `pip install -r requirements.txt`。
- 启动：设置 `MCP_SERVER_HOST=0.0.0.0`、`MCP_SERVER_PORT=8000` 后 `python main.py`。
- 配置：客户端连接本地 server；Burp 地址/API 配置需与项目设置一致。

### ZSA233/frida-analykit

- 链接：https://github.com/ZSA233/frida-analykit
- 状态：可直接启动，Frida MCP/CLI。
- MCP 描述：Frida-Analykit，Python CLI 与 MCP server，用于 Frida runtime 分析。
- 环境：Python/uv；Node/npm；Frida；目标设备或进程。
- 依赖安装：`uv tool install "git+https://github.com/ZSA233/frida-analykit@stable"`；项目内前端/辅助依赖可 `npm install`。
- 启动：`frida-analykit-mcp --config ./mcp.toml`。
- 配置：通过 `mcp.toml` 配置 Frida 目标、连接方式和工具参数。

### ajtazer/ZAP-MCP

- 链接：https://github.com/ajtazer/ZAP-MCP
- 状态：可直接启动，OWASP ZAP MCP。
- MCP 描述：ZAP-MCP，让 MCP 客户端操作 OWASP ZAP 进行扫描和测试。
- 环境：Python；OWASP ZAP；ZAP API key/端口；可选模型目录。
- 依赖安装：README 示例 `git clone https://github.com/tazer/ZAP-MCP.git`，进入目录后 `pip install -r requirements.txt`，再 `./setup_mcp.sh`。
- 启动：`mcp-server --config claude_desktop_config.json --model-dir ./models`。
- 配置：需配置 ZAP API 地址、API key、代理端口和 MCP 客户端。

### ap425q/CutterMCP

- 链接：https://github.com/ap425q/CutterMCP
- 状态：需 Cutter/Rizin 宿主绑定。
- MCP 描述：Cutter MCP，面向 Cutter/Rizin 的逆向辅助 MCP。
- 环境：Python；Cutter/Rizin；`mcp==1.5.0`、`requests==2.32.3`。
- 依赖安装：`pip install -r requirements.txt`。
- 启动：README 未给出完整启动命令；需确认 Cutter 插件或 bridge 脚本入口。
- 配置：需要配置 Cutter/Rizin 连接地址和 MCP 客户端 command。

### bbgouzi123/x64dbg-mcp

- 链接：https://github.com/bbgouzi123/x64dbg-mcp
- 状态：需 x64dbg 绑定，Python 依赖明确。
- MCP 描述：X64Dbg MCP Server。
- 环境：Windows；x64dbg；Python/uv。
- 依赖安装：`pip install -r requirements.txt` 或 `uv pip install -r requirements.txt`。
- 启动：README 未在抓取片段中给出完整启动命令；需确认 server 脚本。
- 配置：配置 x64dbg 插件/自动化接口和 MCP 客户端 command。

### bethington/ghidra-mcp

- 链接：https://github.com/bethington/ghidra-mcp
- 状态：需 Ghidra 插件部署。
- MCP 描述：Ghidra MCP Server，带 setup 工具链。
- 环境：Ghidra；Python；Java/Gradle；`requirements.txt`。
- 依赖安装：`python -m tools.setup preflight --ghidra-path "F:\ghidra_12.1_PUBLIC"`，`python -m tools.setup ensure-prereqs --ghidra-path ...`，再 `python -m tools.setup build`。
- 启动：`python -m tools.setup deploy --ghidra-path "F:\ghidra_12.1_PUBLIC"` 部署插件后，在 Ghidra 内启动。
- 配置：使用 setup 命令生成/部署扩展，再配置 MCP 客户端连接插件暴露的服务。

### blacktop/ida-mcp-rs

- 链接：https://github.com/blacktop/ida-mcp-rs
- 状态：可直接启动，Rust/IDA MCP。
- MCP 描述：面向 IDA 的 Rust MCP server。
- 环境：IDA Pro；Rust/Cargo 或包管理器；IDA 路径环境变量。
- 依赖安装：macOS 可 `brew install blacktop/tap/ida-mcp`；Windows 可 `scoop install blacktop/ida-mcp`；也支持 Nix/Snap。
- 启动：`ida-mcp`。
- 配置：Claude 示例 `claude mcp add ida -- ida-mcp`；必要时设置 `DYLD_LIBRARY_PATH` 或 `IDADIR` 指向 IDA 安装目录。

### bromoket/x64dbg_mcp

- 链接：https://github.com/bromoket/x64dbg_mcp
- 状态：需 x64dbg 插件 + npm bridge。
- MCP 描述：x64dbg MCP Server，插件启动后在本地端口暴露能力。
- 环境：Windows；x64dbg/x32dbg；Node.js/npx。
- 依赖安装：把 `x64dbg_mcp.dp64` 放入 `x64/plugins/`，`x64dbg_mcp.dp32` 放入 `x32/plugins/`。
- 启动：x64dbg 日志出现 `[MCP] x64dbg MCP Server started on 127.0.0.1:27042` 后，客户端可用 `npx -y x64dbg-mcp-server`。
- 配置：MCP 客户端配置 Node bridge；端口需匹配插件默认 `27042`。

### buzzer-re/Rikugan

- 链接：https://github.com/buzzer-re/Rikugan
- 状态：非传统 MCP server；宿主内 agent，支持接入 MCP server。
- MCP 描述：IDA Pro / Binary Ninja 内部逆向 agent，带 60+ 工具、技能和 MCP server integration。
- 环境：IDA Pro 9.0+ 或 Binary Ninja UI；Python 3.10+；LLM provider；Windows 需 VC++ Redistributable。
- 依赖安装：Linux/macOS `curl -fsSL https://raw.githubusercontent.com/buzzer-re/Rikugan/main/install.sh | bash`；Windows `irm https://raw.githubusercontent.com/buzzer-re/Rikugan/main/install.ps1 | iex`。
- 启动：安装后在 IDA/Binary Ninja 中使用快捷键/插件 UI 启动 agent。
- 配置：配置 LLM provider、profile、skills 和可接入的 MCP server；不是单独导入 MCP 客户端的 server。

### cnitlrt/headless-ida-mcp-server

- 链接：https://github.com/cnitlrt/headless-ida-mcp-server
- 状态：可直接启动，headless IDA MCP。
- MCP 描述：无头 IDA MCP server。
- 环境：IDA Pro；Python 3.12；uv；IDA 授权和可访问路径。
- 依赖安装：`uv python install 3.12`，`uv venv --python 3.12`，`uv pip install -e .`。
- 启动：`uv run headless_ida_mcp_server`。
- 配置：可用 `npx -y @modelcontextprotocol/inspector` 调试；客户端 command 可指向 `uv run headless_ida_mcp_server`。

### crowdere/Awesome-RE-MCP

- 链接：https://github.com/crowdere/Awesome-RE-MCP
- 状态：非 MCP Server，awesome list。
- MCP 描述：逆向 MCP 资源索引。
- 环境：无运行环境。
- 依赖安装：无。
- 启动：无独立启动命令。
- 配置：用作选型参考，不导入 MCP 客户端。

### cyberkaida/reverse-engineering-assistant

- 链接：https://github.com/cyberkaida/reverse-engineering-assistant
- 状态：可作为 Ghidra/ReVa MCP 使用，也提供插件/marketplace。
- MCP 描述：ReVa，Ghidra MCP Server for AI-Powered Reverse Engineering。
- 环境：Ghidra；Gradle；Python/uv；Claude/Codex 等 MCP 客户端。
- 依赖安装：Ghidra 插件可 `gradle install`；Python MCP 可 `uv tool install reverse-engineering-assistant`。
- 启动：HTTP 方式 `claude mcp add --scope user --transport http ReVa -- http://localhost:8080/mcp/message`；stdio 方式 `claude mcp add --scope user ReVa -- mcp-reva`。
- 配置：也支持 `claude plugin marketplace add cyberkaida/reverse-engineering-assistant`。

### cycraft-corp/BinaryAnalysisMCPs

- 链接：https://github.com/cycraft-corp/BinaryAnalysisMCPs
- 状态：非单一 MCP Server，集合/索引。
- MCP 描述：Binary Analysis MCPs 集合。
- 环境：取决于具体子项目。
- 依赖安装：需选择具体 MCP 后按其说明安装。
- 启动：无统一启动命令。
- 配置：作为二进制分析 MCP 选型索引。

### cyproxio/mcp-for-security

- 链接：https://github.com/cyproxio/mcp-for-security
- 状态：可容器化启动的安全 MCP 集合/工具。
- MCP 描述：MCP for Security，偏安全测试工具集合。
- 环境：Docker；Node/Python 取决于具体 server；安全测试工具链。
- 依赖安装：仓库包含 Dockerfile；按 README 构建镜像。
- 启动：使用 Docker 镜像或仓库脚本启动具体 security MCP。
- 配置：需选择目标工具并配置对应 MCP 客户端条目。

### darbra/awesome-ai-reverse

- 链接：https://github.com/darbra/awesome-ai-reverse
- 状态：非 MCP Server，awesome list。
- MCP 描述：AI reverse engineering 资源索引。
- 环境：无运行环境。
- 依赖安装：无。
- 启动：无。
- 配置：作为调研索引。

### digitalandrew/wairz

- 链接：https://github.com/digitalandrew/wairz
- 状态：可启动，固件/串口/后端 + MCP 工作台。
- MCP 描述：通过 `wairz-mcp` 连接 FastAPI 后端、UART bridge 和分析工作流。
- 环境：Docker Compose；Python/uv；Postgres；Redis；串口/设备环境。
- 依赖安装：`docker compose up --build`；后端基础服务可 `docker compose up -d postgres redis`；Python 环境 `uv sync`。
- 启动：README 架构中 MCP server 为 `wairz-mcp`，后端可 `uv run alembic upgrade head` 后启动。
- 配置：配置 UART bridge `TCP:9999`、后端地址和 MCP 客户端 command。

### dwmetz/MalChela

- 链接：https://github.com/dwmetz/MalChela
- 状态：非明确 MCP Server，Rust/Claude plugin 近邻项目。
- MCP 描述：恶意软件分析工具/插件项目，抓取到 Cargo 和 `.claude-plugin` 安装信息。
- 环境：Rust/Cargo；Node/npm；Claude plugin 运行环境。
- 依赖安装：`git clone https://github.com/dwmetz/MalChela.git`，进入 `.claude-plugin/` 后 `npm install`。
- 启动：README 未确认独立 MCP server 命令。
- 配置：按 Claude plugin 而非 MCP server 方式接入。

### eversinc33/TriageMCP

- 链接：https://github.com/eversinc33/TriageMCP
- 状态：可直接启动，PE triage MCP。
- MCP 描述：用于恶意软件/PE 初筛分析的 MCP。
- 环境：Python；`pefile`、`yara-python`、`die-python`、`mcp[cli]`。
- 依赖安装：`pip install pefile yara-python die-python mcp[cli]`。
- 启动：`mcp install .\triage.py`。
- 配置：安装后在 MCP client 中调用 triage 工具，对样本路径生成分析报告。

### fdrechsler/mcp-server-idapro

- 链接：https://github.com/fdrechsler/mcp-server-idapro
- 状态：需 IDA/Node MCP 绑定。
- MCP 描述：IDA Pro MCP Server，Node/TypeScript 项目。
- 环境：Node.js；npm；IDA Pro。
- 依赖安装：`git clone <repository-url>`，`npm install`，`npm run build`。
- 启动：构建后按项目 server 入口运行，通常为 `node build/...`。
- 配置：README 展示 MCP tool 调用格式，需配置 IDA 连接参数。

### jtsylve/re-mcp

- 链接：https://github.com/jtsylve/re-mcp
- 状态：可直接启动，IDA/Ghidra unified MCP。
- MCP 描述：RE-MCP，统一 CLI，可安装 IDA 或 Ghidra 后端。
- 环境：Python/uv；IDA Pro 或 Ghidra；对应后端依赖。
- 依赖安装：`uv tool install re-mcp-ida`、`uv tool install re-mcp-ghidra`，或 `uv tool install re-mcp --with re-mcp-ida --with re-mcp-ghidra`；pip 也可安装。
- 启动：使用统一 CLI 或对应后端 MCP command；源码开发可 `uv sync`。
- 配置：按启用后端配置 IDA/Ghidra 路径和 MCP 客户端 command。

### kggzs/ApkMCP-Auto

- 链接：https://github.com/kggzs/ApkMCP-Auto
- 状态：可直接启动，Android 逆向 MCP 工具套件。
- MCP 描述：自动安装、配置和启动 apktool/jadx/adb 等 Android 逆向 MCP。
- 环境：Python；Java；Android tools；apktool；jadx；adb。
- 依赖安装：`python apkmcp.py install`，也可 `python apkmcp.py install apktool`。
- 启动：单个 server `python apkmcp.py start apktool`；全量 `python start_all_servers.py`；指定服务 `python start_all_servers.py --servers jadx,apktool,adb`。
- 配置：`python apkmcp.py config`，可 `-p` 打印或 `-o my-config.json` 输出配置。

### llnl/OGhidra

- 链接：https://github.com/llnl/OGhidra
- 状态：需 Ghidra/Ollama/插件，MCP 近邻。
- MCP 描述：OGhidra 3，Ghidra AI 逆向平台，包含 OGhidraMCP 插件。
- 环境：Ghidra；Python/uv 或 pip；Gradle；Ollama/local model 可选。
- 依赖安装：`uv sync` 或 `pip install -r requirements.txt`；进入 `OGhidraMCP` 后 `gradle buildExtension`。
- 启动：`uv run main.py --ui` 或 `uv run main.py --interactive`。
- 配置：需要安装 GhidraMCP Plugin 并配置本地模型/服务。

### m4rba4s/mcp_debugger

- 链接：https://github.com/m4rba4s/mcp_debugger
- 状态：需 x64dbg 插件。
- MCP 描述：MCP Debugger，x64dbg 插件/调试器 MCP。
- 环境：Windows；x64dbg；C++ 构建环境；Python 测试脚本。
- 依赖安装：源码构建后复制 `build\Release\mcp_debugger.dp64` 到 `C:\x64dbg\release\x64\plugins\`。
- 启动：启动 x64dbg 并加载插件。
- 配置：README 提到 JSON config 和 Claude/credential 配置，需按本地插件端口填写。

### mrexodia/ida-pro-mcp

- 链接：https://github.com/mrexodia/ida-pro-mcp
- 状态：可直接启动，主流 IDA Pro MCP。
- MCP 描述：把 IDA Pro 与 MCP 客户端桥接的逆向助手。
- 环境：Python 3.11+；IDA Pro 8.3+，推荐 9.x；IDA Free 不支持；uv/pip。
- 依赖安装：Claude plugin 可 `claude plugin marketplace add mrexodia/claude-marketplace` 后 `claude plugin install ida-pro-mcp@mrexodia`；手动可 `pip install https://github.com/mrexodia/ida-pro-mcp/archive/refs/heads/main.zip`。
- 启动：执行 `ida-pro-mcp --install` 安装 IDA 插件；客户端使用 `ida-pro-mcp` command。
- 配置：若 IDA 9.3 idalib 需先运行 `uv run ".../idalib/python/py-activate-idalib.py"`；必要时配置 IDA 路径环境变量。

### mrphrazer/agentic-malware-analysis

- 链接：https://github.com/mrphrazer/agentic-malware-analysis
- 状态：非单一 MCP Server，Docker 化恶意软件分析工作流。
- MCP 描述：集成 Claude/Codex、Binary Ninja MCP、Ghidra MCP 的 agentic malware analysis 环境。
- 环境：Docker；可选 Binary Ninja zip/user dir；Claude/Codex 配置目录；Ghidra/Binja MCP 仓库。
- 依赖安装：`git clone https://github.com/mrphrazer/agentic-malware-analysis.git`。
- 启动：`./run_docker.sh`；可传 `BINARY_NINJA_ZIP=...`、`CLAUDE_USER_DIR=...`、`CODEX_USER_DIR=...`。
- 配置：通过环境变量指定 Binja/Ghidra MCP repo URL 和本地配置目录；不单独作为 MCP command。

### mrphrazer/binary-ninja-headless-mcp

- 链接：https://github.com/mrphrazer/binary-ninja-headless-mcp
- 状态：可直接启动，Binary Ninja headless MCP。
- MCP 描述：无需 GUI 的 Binary Ninja MCP。
- 环境：Python；Binary Ninja headless/API；授权。
- 依赖安装：源码 `pip install .`，或 `pip install git+https://github.com/mrphrazer/binary-ninja-headless-mcp.git`。
- 启动：`python3 binary_ninja_headless_mcp.py`；TCP 模式 `python3 binary_ninja_headless_mcp.py --transport tcp --host 127.0.0.1 --port 8765`；测试可 `--fake-backend`。
- 配置：Claude/Codex 示例为 `claude mcp add binary_ninja_headless_mcp -- python3 /path/to/.../binary_ninja_headless_mcp.py`。

### mrphrazer/ghidra-headless-mcp

- 链接：https://github.com/mrphrazer/ghidra-headless-mcp
- 状态：可直接启动，Ghidra headless MCP。
- MCP 描述：无头 Ghidra MCP。
- 环境：Python；Ghidra 安装目录；Java。
- 依赖安装：`python3 -m venv .venv` 后 `pip install .`；开发用 `pip install -e ".[dev]"`。
- 启动：`GHIDRA_INSTALL_DIR=/ABSOLUTE/PATH/TO/ghidra python3 ghidra_headless_mcp.py`；TCP 模式加 `--transport tcp --host 127.0.0.1 --port 8765`。
- 配置：Claude 示例 `claude mcp add ghidra_headless_mcp -- python3 /path/to/ghidra-headless-mcp/ghidra_headless_mcp.py --ghidra-install-dir /ABSOLUTE/PATH/TO/ghidra`。

### pansila/mcp_server_gdb

- 链接：https://github.com/pansila/mcp_server_gdb
- 状态：可直接启动，Rust GDB MCP。
- MCP 描述：MCP Server GDB。
- 环境：Rust/Cargo；GDB；可选 Nix。
- 依赖安装：`cargo build --release`。
- 启动：`cargo run`，或 `nix run "git+https://github.com/pansila/mcp_server_gdb.git" -- --help`。
- 配置：客户端 command 可指向 release binary 或 `cargo run` 包装脚本。

### president-xd/revula

- 链接：https://github.com/president-xd/revula
- 状态：可直接启动，Python/Docker 逆向 MCP。
- MCP 描述：revula，逆向分析 MCP/toolkit。
- 环境：Python；可选 Docker；按 full extra 安装完整工具链。
- 依赖安装：`pip install -e .`；完整依赖 `pip install -e ".[full]"`。
- 启动：Docker 可 `docker build -t revula:latest .` 后 `docker run -i --rm -v $(pwd)/workspace:/workspace -v revula-data:/root/.revula revula:latest`。
- 配置：可用 `python scripts/test/validate_install.py` 和 availability report 检查工具链。

### punkpeye/awesome-mcp-servers

- 链接：https://github.com/punkpeye/awesome-mcp-servers
- 状态：非 MCP Server，awesome list。
- MCP 描述：通用 MCP server 索引。
- 环境：无。
- 依赖安装：无。
- 启动：无。
- 配置：仅用于选型参考。

### pwno-io/pwno-mcp

- 链接：https://github.com/pwno-io/pwno-mcp
- 状态：可直接启动，Docker HTTP MCP。
- MCP 描述：Pwn/security MCP，提供 HTTP MCP endpoint。
- 环境：Docker；网络端口 5500。
- 依赖安装：拉取 `ghcr.io/pwno-io/pwno-mcp:latest`。
- 启动：`docker run --rm -p 5500:5500 ghcr.io/pwno-io/pwno-mcp:latest`。
- 配置：客户端 HTTP MCP 地址 `http://127.0.0.1:5500/mcp`。

### radareorg/r2ai

- 链接：https://github.com/radareorg/r2ai
- 状态：非标准 MCP Server，radare2 + LLM 工具。
- MCP 描述：R2AI，radare2 的 LLM 增强逆向工具。
- 环境：radare2；LLM provider；r2ai。
- 依赖安装：按 r2ai 项目说明安装。
- 启动：示例 `r2ai -e model=claude-3-7-sonnet-20250219`。
- 配置：配置模型、provider 和 radare2 环境；若需 MCP 需另查是否有 server 模式。

### rsprudencio/binja_mcp

- 链接：https://github.com/rsprudencio/binja_mcp
- 状态：可直接启动，但需要 Binary Ninja。
- MCP 描述：Binary Ninja MCP Server。
- 环境：Python；Binary Ninja API/授权。
- 依赖安装：`pip install binja-mcp`。
- 启动：`python -m binja_mcp`。
- 配置：可用 `npx @modelcontextprotocol/inspector uvx binja_mcp` 调试；客户端 command 为 `python -m binja_mcp`。

### s4dp4nd4/frida-c2-mcp

- 链接：https://github.com/s4dp4nd4/frida-c2-mcp
- 状态：需设备端 HTTP MCP，Frida C2/instrumentation。
- MCP 描述：FridaC2MCP，通过 HTTP transport 连接设备端服务。
- 环境：Frida；目标设备；HTTP 网络访问。
- 依赖安装：README 抓取片段未给出完整安装命令。
- 启动：需在设备或控制端启动服务，使其监听 `http://<DEVICE_IP>:6767/mcp`。
- 配置：Gemini 示例 `gemini mcp add --transport http frida-c2-mcp http://<DEVICE_IP>:6767/mcp`。

### signal-slot/mcp-gdb

- 链接：https://github.com/signal-slot/mcp-gdb
- 状态：可直接启动，Node GDB MCP。
- MCP 描述：MCP GDB Server。
- 环境：Node.js；npm/npx；GDB。
- 依赖安装：直接可 `npx -y mcp-gdb`；源码开发 `npm install`、`npm run build`。
- 启动：`npx -y mcp-gdb`。
- 配置：Claude 示例 `claude mcp add gdb -- npx -y mcp-gdb`。

### sjkim1127/Reversecore_MCP

- 链接：https://github.com/sjkim1127/Reversecore_MCP
- 状态：可直接启动，Docker/Python 逆向 MCP。
- MCP 描述：Reversecore_MCP，集成多架构逆向工具。
- 环境：Docker Compose；Python；x86/arm64 profile；逆向工具链。
- 依赖安装：按 Dockerfile/requirements 构建。
- 启动：`./scripts/run-docker.sh`，或 `docker compose --profile x86 up -d` / `docker compose --profile arm64 up -d`。
- 配置：可 `docker build -t reversecore-mcp:latest .` 后使用脚本运行；按 profile 选择架构。

### smadi0x86/MDB-MCP

- 链接：https://github.com/smadi0x86/MDB-MCP
- 状态：可直接启动，多调试器 MCP。
- MCP 描述：Multi-Debugger MCP Server，支持 LLDB 和 GDB。
- 环境：Python/uv；GDB；LLDB/LLVM；macOS 可 `brew install llvm python3`。
- 依赖安装：`uv sync`、`uv venv`；或 `pip3 install mcp pygdbmi --break-system-packages`。
- 启动：`uv run server.py`。
- 配置：用 `uv run python run-tests.py --check-deps` 检查依赖；按目标调试器配置路径。

### starsong-consulting/GhydraMCP

- 链接：https://github.com/starsong-consulting/GhydraMCP
- 状态：需 Ghidra 插件 + 可选 MCP bridge/CLI。
- MCP 描述：GhydraMCP v2.2.0，Ghidra REST API、CLI 和 MCP bridge。
- 环境：Ghidra；Python3；MCP Python SDK；Java/Gradle。
- 依赖安装：下载 release 的 Complete artifact，安装 Ghidra 插件；CLI 可 `pip install -e .`。
- 启动：Ghidra 插件启动后默认端口范围 8192-8447；CLI 可 `ghydra instances list`、`ghydra functions decompile --name main`。
- 配置：MCP bridge 已被 README 标注为 deprecated，但仍可 stdio 连接；推荐根据当前版本确认 `bridge_mcp_hydra.py` 或 `ghydra` 使用方式。

### suidpit/ghidra-mcp

- 链接：https://github.com/suidpit/ghidra-mcp
- 状态：需 Ghidra 插件，SSE transport。
- MCP 描述：Ghidra MCP Server，Java/Spring Boot 嵌入 Ghidra 插件。
- 环境：Ghidra；Java/Gradle；支持 SSE 或 stdio-to-SSE proxy 的 MCP 客户端。
- 依赖安装：下载 release extension ZIP，或在项目根目录运行 `gradle` 构建 dist ZIP。
- 启动：Ghidra 中 `File -> Install Extension` 安装，Code Browser 里启用 `GhidraMCPPlugin` 后自动在端口 8888 启动。
- 配置：SSE 客户端直接连 `http://localhost:8888/sse`；stdio 客户端可安装 `uv tool install mcp-proxy`，配置 `command: "mcp-proxy"`，`args: ["http://localhost:8888/sse"]`。

### swgee/BurpMCP

- 链接：https://github.com/swgee/BurpMCP
- 状态：可直接启动/需 Burp 代理配合。
- MCP 描述：BurpMCP，连接 Burp Suite 的 MCP 工具。
- 环境：Python；Burp Suite；`typer`、`mcp`。
- 依赖安装：`pip3 install typer mcp`，`git clone https://github.com/swgee/burpmcp.git`。
- 启动：进入 `burpmcp` 后按 CLI/server 脚本启动；README 抓取片段未显示完整命令。
- 配置：需配置 Burp 地址和端口。

### symgraph/BinAssistMCP

- 链接：https://github.com/symgraph/BinAssistMCP
- 状态：需 Binary Ninja，HTTP/SSE/streamable HTTP MCP。
- MCP 描述：BinAssistMCP，Binary Ninja API wrapper，提供 44 个 MCP tools 和 8 个 resources。
- 环境：Python；Binary Ninja；`requirements.txt`；网络端口。
- 依赖安装：`git clone https://github.com/jtang613/BinAssistMCP.git`，进入目录后 `pip install -r requirements.txt`。
- 启动：设置 `BINASSISTMCP_SERVER__HOST=localhost`、`BINASSISTMCP_SERVER__PORT=9090`、`BINASSISTMCP_SERVER__TRANSPORT=streamablehttp` 后运行 server。
- 配置：客户端连接 `localhost:9090` 的 streamable HTTP/SSE MCP；需确保 Binary Ninja API 可用。

### symgraph/GhidrAssistMCP

- 链接：https://github.com/symgraph/GhidrAssistMCP
- 状态：需 Ghidra 插件。
- MCP 描述：GhidrAssistMCP，Ghidra 插件/脚本式 MCP server。
- 环境：Ghidra；Gradle；Java；Ghidra headless/analyzeHeadless。
- 依赖安装：`gradle installExtension`。
- 启动：可通过 Ghidra headless `-scriptPath "$GHIDRASSISTMCP_EXT/ghidra_scripts" -preScript GAMCPStartServerScript.java "host=127.0.0.1" "port=8080"` 启动服务。
- 配置：设置 `GHIDRASSISTMCP_EXT` 指向扩展目录；客户端连接 host/port。

### symgraph/IDAssist

- 链接：https://github.com/symgraph/IDAssist
- 状态：需 IDA 插件。
- MCP 描述：IDAssist，IDA AI/MCP 辅助插件。
- 环境：IDA Pro；IDA 自带 Python；`requirements.txt`。
- 依赖安装：`<IDA_INSTALL_DIR>/python3/bin/pip3 install -r ~/.idapro/plugins/IDAssist/requirements.txt`。
- 启动：在 IDA 中加载插件。
- 配置：按插件说明配置模型/MCP 连接；不是普通独立 stdio server。

### taida957789/ida-mcp-server-plugin

- 链接：https://github.com/taida957789/ida-mcp-server-plugin
- 状态：需 IDA 插件。
- MCP 描述：IDA Pro MCP Server 插件。
- 环境：IDA Pro；Python；`requirements.txt`。
- 依赖安装：`pip install -r requirements.txt`。
- 启动：安装到 IDA 插件目录后在 IDA 内加载。
- 配置：配置插件端口/bridge 和 MCP 客户端。

### vichhka-git/android-reverse-engineering-mcp-server

- 链接：https://github.com/vichhka-git/android-reverse-engineering-mcp-server
- 状态：可直接启动，Android 逆向 MCP。
- MCP 描述：Android Reverse Engineering MCP Server。
- 环境：Python；Android SDK/adb；apktool/jadx 等 Android RE 工具。
- 依赖安装：`python3 -m venv .venv`，激活后 `pip install -r requirements.txt`。
- 启动：`python server.py`。
- 配置：配置 Android 工具路径、目标 APK/设备和 MCP 客户端 command。

### vmoranv/jshookmcp

- 链接：https://github.com/vmoranv/jshookmcp
- 状态：可直接启动，npm stdio MCP。
- MCP 描述：`@jshookmcp/jshook`，为 JS 分析和安全研究提供 400+ 工具，覆盖浏览器自动化、CDP、网络拦截、JS hook、WASM、AST 等。
- 环境：Node.js 22.12+；pnpm 10.x 用于源码开发；npx 可直接运行。
- 依赖安装：无需全局安装；源码开发用 pnpm/npm。
- 启动：`npx -y @jshookmcp/jshook@latest`。
- 配置：Claude/Cursor 示例 `command: "npx"`，`args: ["-y", "@jshookmcp/jshook@latest"]`，可设置 `env: { "JSHOOK_BASE_PROFILE": "search" }`。

### vwww-droid/Mira

- 链接：https://github.com/vwww-droid/Mira
- 状态：可直接启动，Relay + MCP 移动运行时工作台。
- MCP 描述：Mira，Android/iOS mobile runtime detection workbench，提供 Relay、Web UI 和 `mira-mcp`。
- 环境：Python 3.10+；Android/iOS 设备；Frida 可选；局域网访问。
- 依赖安装：源码安装 Python package；`pyproject.toml` 提供脚本 `mira-cli`、`mira-relay`、`mira-mcp`、`mira-build`。
- 启动：Relay：`PYTHONPATH=. python3 -m mira.relay.server --host 0.0.0.0 --port 8765 --advertise-url http://<your-lan-ip>:8765`；MCP：`PYTHONPATH=. python3 -m mira.mcp.server --relay http://127.0.0.1:8765`。
- 配置：浏览器打开 `http://127.0.0.1:8765`；Android app 填入 LAN Relay 地址；MCP 配置见仓库 `docs/MCP.md`。

### wuji66dde/jshook-reverse-tool

- 链接：https://github.com/wuji66dde/jshook-reverse-tool
- 状态：可直接启动，npm JS 逆向 MCP/工具。
- MCP 描述：JSHook Reverse Tool，JS hook/逆向分析工具。
- 环境：Node.js；npm/npx。
- 依赖安装：直接 `npx jshook-reverse-tool`，或 `npm install -g jshook-reverse-tool`；源码为 `npm install`、`npm run build`。
- 启动：`npx jshook-reverse-tool`。
- 配置：客户端 command 可为 `npx`，args 为 `["jshook-reverse-tool"]` 或全局 binary。

### yzfly/Awesome-MCP-ZH

- 链接：https://github.com/yzfly/Awesome-MCP-ZH
- 状态：非 MCP Server，中文 MCP 索引。
- MCP 描述：Awesome-MCP-ZH，中文 MCP 资源列表。
- 环境：无。
- 依赖安装：无。
- 启动：无。
- 配置：用作索引，不导入 MCP 客户端。

### zhaoxuya520/reverse-skill

- 链接：https://github.com/zhaoxuya520/reverse-skill
- 状态：非 MCP Server，技能路由包/平台文档。
- MCP 描述：Cybersecurity Skills Router / Reverse-Engineering Skill Routing Pack。
- 环境：Node、Python、pip、pnpm 等，按目标平台不同。
- 依赖安装：开发示例 `pnpm install`，`pnpm dev`。
- 启动：文档提到 `http://localhost:23816/mcp`，但仓库定位更偏 skill/router，需要按平台部署文档确认。
- 配置：按平台文档检查 toolchain、script entry point、MCP configuration 和路径约定。

### zhizhuodemao/ai-reverse-toolkit

- 链接：https://github.com/zhizhuodemao/ai-reverse-toolkit
- 状态：非 MCP Server，逆向 toolkit/skills/rules。
- MCP 描述：AI reverse toolkit，提供 skills、rules、prompts 和逆向知识组织。
- 环境：支持 `.claude/skills` 和 `.claude/rules` 的工作区。
- 依赖安装：`cp -r skills/* your-project/.claude/skills/`，`cp -r rules/* your-project/.claude/rules/`。
- 启动：无独立 MCP server。
- 配置：作为项目技能/规则包使用。

### zhizhuodemao/frida-mcp

- 链接：https://github.com/zhizhuodemao/frida-mcp
- 状态：可直接启动，Python Frida MCP。
- MCP 描述：Frida MCP Server，提供 Frida runtime instrumentation MCP 工具。
- 环境：Python；Frida；`requirements.txt`；目标设备/进程。
- 依赖安装：README 示例为 `pip install -r requirements.txt`，`pip install -e .`。
- 启动：安装后按 entry point 或 `frida_mcp.py` 启动；README 结构显示核心实现为 `frida_mcp.py`。
- 配置：配置 Frida 连接目标；注意不要和 `dnakov/frida-mcp` 使用同名 `frida-mcp` binary 时冲突。

### dnakov/frida-mcp

- 链接：https://github.com/dnakov/frida-mcp
- 状态：可直接启动，Python Frida MCP。
- MCP 描述：Model Context Protocol implementation for Frida。
- 环境：Python 3.8+；Frida 16+；`mcp>=1.5.0`。
- 依赖安装：`pip install frida-mcp`；开发安装 `pip install -e ".[dev]"`。
- 启动：`frida-mcp`。
- 配置：客户端 command 为 `frida-mcp`；若与其他 frida-mcp fork 并存，建议使用虚拟环境或绝对路径区分。

### zinja-coder/apktool-mcp-server

- 链接：https://github.com/zinja-coder/apktool-mcp-server
- 状态：可直接启动，apktool MCP。
- MCP 描述：apktool-mcp-server，Zin's Reverse Engineering MCP Suite 的一部分。
- 环境：Python/uv；apktool；Java；`httpx`、`fastmcp`。
- 依赖安装：下载 release zip 或源码；进入 `apktool-mcp-server` 后 `uv venv`，`uv pip install httpx fastmcp`。
- 启动：`apktool_mcp_server.py`。
- 配置：编辑 `~/.config/Claude/claude_desktop_config.json`，command 指向 Python/uv，args 指向 `apktool_mcp_server.py`。

### zinja-coder/jadx-ai-mcp

- 链接：https://github.com/zinja-coder/jadx-ai-mcp
- 状态：需 JADX GUI 插件，与 `jadx-mcp-server` 配套。
- MCP 描述：JADX-AI，JADX GUI 插件/AI MCP 配套组件。
- 环境：JADX GUI；Java；Python/uv；`httpx`、`fastmcp`。
- 依赖安装：README 示例进入 `jadx_mcp_server` 后 `uv venv`，`uv pip install httpx fastmcp`。
- 启动：作为 JADX 插件配合 server 使用，不是单独 MCP server。
- 配置：与 `zinja-coder/jadx-mcp-server` 联动，MCP server 调 JADX plugin 的 HTTP handler。

### zinja-coder/jadx-mcp-server

- 链接：https://github.com/zinja-coder/jadx-mcp-server
- 状态：可直接启动，但需要 JADX AI plugin 配合。
- MCP 描述：JADX-MCP-SERVER，Zin's Reverse Engineering MCP Suite 的一部分，通过 MCP server 调用 JADX GUI Plugin。
- 环境：Python/uv；JADX GUI；`jadx-ai-mcp` 插件；`requirements.txt` / `pyproject.toml`。
- 依赖安装：进入项目后按 `requirements.txt` 或 `uv` 安装依赖。
- 启动：HTTP 模式 `uv run jadx_mcp_server.py --http`；对外监听 `uv run jadx_mcp_server.py --http --host 0.0.0.0`。
- 配置：先启动 JADX GUI plugin，再启动 MCP server；客户端连接 MCP server，server 通过 HTTP 请求调用 JADX plugin。

