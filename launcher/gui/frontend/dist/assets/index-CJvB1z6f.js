(function(){const t=document.createElement("link").relList;if(t&&t.supports&&t.supports("modulepreload"))return;for(const i of document.querySelectorAll('link[rel="modulepreload"]'))a(i);new MutationObserver(i=>{for(const l of i)if(l.type==="childList")for(const c of l.addedNodes)c.tagName==="LINK"&&c.rel==="modulepreload"&&a(c)}).observe(document,{childList:!0,subtree:!0});function s(i){const l={};return i.integrity&&(l.integrity=i.integrity),i.referrerPolicy&&(l.referrerPolicy=i.referrerPolicy),i.crossOrigin==="use-credentials"?l.credentials="include":i.crossOrigin==="anonymous"?l.credentials="omit":l.credentials="same-origin",l}function a(i){if(i.ep)return;i.ep=!0;const l=s(i);fetch(i.href,l)}})();const te="/assets/favicon-DnelM9ID.png";function O(){return window.go.main.GUIApp.ApplySelfUpdate()}function W(){return window.go.main.GUIApp.CheckSelfUpdate()}function ne(){return window.go.main.GUIApp.ClearSavedLogin()}function se(e){return window.go.main.GUIApp.CreateAgent(e)}function ae(){return window.go.main.GUIApp.CreateSSHTunnel()}function ie(){return window.go.main.GUIApp.Defaults()}function re(e){return window.go.main.GUIApp.Directories(e)}function k(e){return window.go.main.GUIApp.DirectoryPermission(e)}function oe(e){return window.go.main.GUIApp.InstallSSHAuthorizedKey(e)}function le(){return window.go.main.GUIApp.LoadSavedLogin()}function ce(e){return window.go.main.GUIApp.Login(e)}function de(e){return window.go.main.GUIApp.MCPStatus(e)}function ue(){return window.go.main.GUIApp.PrepareRuntimeEnvironment()}function pe(e){return window.go.main.GUIApp.RemoveAgent(e)}function ge(e){return window.go.main.GUIApp.RestartAgent(e)}function q(){return window.go.main.GUIApp.RestartBackground()}function B(){return window.go.main.GUIApp.RuntimeEnvironment()}function me(){return window.go.main.GUIApp.SSHStatus()}function ve(e){return window.go.main.GUIApp.SaveSavedLogin(e)}function T(e){return window.go.main.GUIApp.SetAgentEnabled(e)}function N(e){return window.go.main.GUIApp.SetAutostart(e)}function he(e){return window.go.main.GUIApp.SetTerminalCompatEnabled(e)}function fe(){return window.go.main.GUIApp.SetupSSH()}function we(){return window.go.main.GUIApp.StartLauncher()}function P(){return window.go.main.GUIApp.State()}function be(){return window.go.main.GUIApp.TerminalCompatSettings()}function $e(e,t,s){return window.runtime.EventsOnMultiple(e,t,s)}function ye(e,t){return $e(e,t,-1)}function Se(){window.runtime.WindowCenter()}function Ae(e,t){window.runtime.WindowSetSize(e,t)}function _e(e,t){window.runtime.WindowSetMinSize(e,t)}function Ce(e){return window.runtime.ClipboardSetText(e)}const R=document.querySelector("#app"),Pe={width:520,height:560,minWidth:420,minHeight:520},Le={width:1180,height:760,minWidth:960,minHeight:640};function ke(){var t;const e=[(t=navigator.userAgentData)==null?void 0:t.platform,navigator.userAgent,navigator.platform].filter(Boolean).join(" ");return/Windows/i.test(e)?"windows":"unix"}const n={defaults:{server_url:"https://www.xyapi.top/codex",launcher_version:""},authenticated:!1,ready:!1,device:null,logs:[],loading:!1,error:"",tab:"overview",createDialog:!1,createName:"",createPath:"",dirPath:"",dirEntries:[],dirParent:"",enableAutostart:!0,terminalCompat:!0,rememberLogin:!0,loginUsername:"",loginPassword:"",progress:null,runtimeInfo:null,runtimeLoading:!1,mcpDialog:!1,mcpAgent:null,mcpStatus:null,mcpLoading:!1,bootstrapProgress:null,sshStatus:null,sshLoading:!1,sshCommand:"",sshUnixCommand:"",sshWindowsCommand:"",sshClientPlatform:ke(),sshExpiresAt:"",sshCopied:!1,sshPublicKey:"",sshKeyResult:null};let L=!1;const z={running:"运行中",stopped:"已停止",failed:"异常",starting:"启动中",stopping:"停止中",restarting:"重启中",upgrading:"升级中"},b={start:'<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M8 5v14l11-7z"/></svg>',stop:'<svg viewBox="0 0 24 24" aria-hidden="true"><rect x="6" y="6" width="12" height="12" rx="1.5"/></svg>',restart:'<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M20 11a8 8 0 1 0-2.34 5.66"/><path d="M20 4v7h-7"/></svg>',remove:'<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M3 6h18"/><path d="M8 6V4h8v2"/><path d="M18 6l-1 14H7L6 6"/><path d="M10 11v5"/><path d="M14 11v5"/></svg>',power:'<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 2v10"/><path d="M18.4 6.6a9 9 0 1 1-12.8 0"/></svg>',search:'<svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="11" cy="11" r="7"/><path d="M20 20l-4.2-4.2"/></svg>',download:'<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 3v12"/><path d="M7 10l5 5 5-5"/><path d="M5 21h14"/></svg>',refresh:'<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M20 11a8 8 0 1 0-2.34 5.66"/><path d="M20 4v7h-7"/></svg>',shield:'<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 3l7 3v5c0 5-3.5 8-7 10-3.5-2-7-5-7-10V6l7-3z"/><path d="M9 12l2 2 4-5"/></svg>',terminal:'<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 17l6-6-6-6"/><path d="M12 19h8"/></svg>',check:'<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M20 6L9 17l-5-5"/></svg>',extension:'<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M9 3h6v5h2a3 3 0 0 1 0 6h-2v7H9v-7H7a3 3 0 0 1 0-6h2V3z"/></svg>',copy:'<svg viewBox="0 0 24 24" aria-hidden="true"><rect x="9" y="9" width="11" height="11" rx="2"/><path d="M15 9V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v7a2 2 0 0 0 2 2h3"/></svg>'},Me={permission:"shield"};function r(e){return String(e??"").replaceAll("&","&amp;").replaceAll("<","&lt;").replaceAll(">","&gt;").replaceAll('"',"&quot;").replaceAll("'","&#039;")}function xe(e){return String(e??"").replaceAll("\\","\\\\").replaceAll("'","\\'").replaceAll(`
`,"\\n").replaceAll("\r","\\r")}function M(e){return r(xe(e))}function y(e,t,s,a=""){const i=b[Me[e]||e]||b.shield;return`
    <button
      class="icon-action ${a}"
      title="${r(s)}"
      aria-label="${r(s)}"
      onclick="gui.agentAction('${M(e)}', '${M(t)}')"
    >${i}</button>
  `}function He(e){return`
    <button
      class="icon-action"
      title="MCP 状态"
      aria-label="MCP 状态"
      onclick="gui.openMCPDialog('${M(e)}')"
    >${b.extension}</button>
  `}function f(e,t,s,a=""){const i=b[e]||b.shield;return`
    <button
      class="icon-action ${a}"
      title="${r(t)}"
      aria-label="${r(t)}"
      onclick="${s}"
      ${n.loading?"disabled":""}
    >${i}</button>
  `}function o(e){const t=Object.keys(e).length===1&&Object.prototype.hasOwnProperty.call(e,"progress");Object.assign(n,e),!(t&&At())&&Y()}function p(e){o({error:(e==null?void 0:e.message)||String(e||"")})}function V(){return typeof window<"u"&&!!window.runtime}function Ue(){var e,t;return typeof window<"u"&&!!((t=(e=window.go)==null?void 0:e.main)!=null&&t.GUIApp)}function De(){if(V())try{ye("launcher:bootstrap-progress",e=>{x(e)})}catch{}}function F(e){if(!V())return;const t=e==="main"?Le:Pe;try{_e(t.minWidth,t.minHeight),Ae(t.width,t.height),Se()}catch{}}function g(e){return new Promise(t=>setTimeout(t,e))}function Ee(e,t,s){return{title:e,detail:t,percent:6,active:0,complete:!1,error:"",runtime:null,steps:s.map((a,i)=>({label:a,status:i===0?"active":"pending"}))}}function w(e){n.progress&&o({progress:{...n.progress,...e}})}function Ie(e,t=""){if(!n.progress)return;const s=n.progress.steps.map((i,l)=>l<e?{...i,status:"done"}:l===e?{...i,status:"active"}:{...i,status:"pending"}),a=Math.max(8,Math.round((e+.35)/Math.max(s.length,1)*100));o({progress:{...n.progress,active:e,detail:t||n.progress.detail,percent:a,steps:s}})}const Te={loading_config:0,configuring_autostart:0,checking_existing:1,starting_autostart:1,installing_launcher:2,starting_background:3,waiting_control:4,checking_update:5,self_update_checking:5,self_update_downloading:6,self_update_applying:7,self_update_restarting:8,ready:9,failed:9};function x(e){if(!e||(o({bootstrapProgress:e}),!n.progress))return;const t=String(e.phase||"").trim(),s=Te[t];if(typeof s!="number")return;const a=t==="failed",i=n.progress.steps.map((c,d)=>a&&d===s?{...c,status:"active"}:d<s?{...c,status:"done"}:d===s?{...c,status:"active"}:{...c,status:"pending"}),l=Number(e.percent||0);w({active:s,steps:i,detail:e.error||e.message||n.progress.detail,percent:Math.max(n.progress.percent||0,Math.min(100,l||n.progress.percent||0)),error:a?e.error||e.message||"本机服务启动失败":""})}async function Re(e="完成"){if(!n.progress)return;const t=n.progress.steps.map(s=>({...s,status:"done"}));o({progress:{...n.progress,detail:e,percent:100,complete:!0,steps:t}}),await g(520),o({progress:null})}function Ge(e){const t=(e==null?void 0:e.message)||String(e||"操作失败");if(!n.progress){o({error:t,loading:!1});return}o({loading:!1,error:t,progress:{...n.progress,detail:t,error:t}})}async function $({title:e,detail:t,steps:s,done:a},i){o({loading:!0,error:"",progress:Ee(e,t,s)});try{await i({step:Ie,detail:l=>w({detail:l})}),o({loading:!1}),await Re(a||"完成")}catch(l){throw Ge(l),l}}async function je(){if(Ue())try{const e=await ie();let t=null,s=null;try{t=await le()}catch{t=null}try{s=await be()}catch{s=null}const a={defaults:e};s&&(a.terminalCompat=s.terminal_compat!==!1),t&&(a.loginUsername=t.username||"",a.loginPassword=t.password||"",a.rememberLogin=!!t.has_saved&&t.auto_login!==!1),o(a),t!=null&&t.has_saved&&t.auto_login!==!1&&t.username&&t.password&&await J({serverURL:t.server_url||e.server_url||"",username:t.username,password:t.password,remember:!0,auto:!0})}catch(e){p(e)}}async function v(){try{const e=await P();o({ready:!!e.ready,device:e.state||null,logs:e.logs||[],error:e.ready?"":n.error})}catch(e){p(e)}}async function Ke(e){e.preventDefault();const t=new FormData(e.currentTarget);await J({serverURL:n.defaults.server_url||"",username:String(t.get("username")||""),password:String(t.get("password")||""),remember:n.rememberLogin,auto:!1})}async function J({serverURL:e,username:t,password:s,remember:a,auto:i}){o({loading:!0,error:""});try{await ce({server_url:e||n.defaults.server_url||"",username:t,password:s});try{a?await ve({server_url:e||n.defaults.server_url||"",username:t,password:s,auto_login:!0}):await ne()}catch(l){console.warn("保存自动登录配置失败",l)}F("main"),o({authenticated:!0,ready:!1,device:null,logs:[],loading:!1,error:"",tab:"overview"}),Oe()}catch(l){if(o({loading:!1}),i){p(new Error(`自动登录失败，请手动登录：${(l==null?void 0:l.message)||l}`));return}p(l)}}async function Oe(){try{await $({title:"正在准备本机服务",detail:"正在启动并接管本机 launcher",steps:["配置自启动","检查后台","安装组件","启动服务","等待连接","检查更新","下载更新","应用更新","重启服务","同步状态","检查权限"],done:"本机服务已就绪"},async e=>{e.step(0,n.enableAutostart?"正在配置后台服务自启动":"正在接管已有后台服务"),n.enableAutostart&&await N(!0),e.step(1,"正在检查已有后台 launcher");const t=await we();x(t.bootstrap);let s=t;o({ready:!!s.ready,device:s.state||null,logs:s.logs||[]}),e.step(9,"正在同步本机 Agent 状态"),s=await P(),x(s.bootstrap),o({ready:!!s.ready,device:s.state||null,logs:s.logs||[]}),e.step(10,"正在检查 Agent 目录访问权限");const a=await We(s.state);if(a&&!a.accessible){const i=a.message||a.error||"系统阻止访问 Agent 项目目录，请授权后重启对应 Agent。";o({error:i})}}),Q()}catch{}}async function Q(){if(!(!n.authenticated||!n.ready||L)){L=!0;try{const e=await W();e!=null&&e.update_available&&await O()}catch(e){console.warn("自动检查 Launcher GUI 更新失败",e)}finally{L=!1}}}async function We(e){const t=((e==null?void 0:e.agents)||[]).filter(s=>s.agent_id&&s.enabled&&s.status!=="disabled");for(const s of t){const a=await k({agent_id:s.agent_id,request:!1});if(!a.accessible)return a}return null}async function G(e=""){o({error:""});try{const t=await re({path:e,allow_all:!0});o({dirPath:t.current_path||"",dirParent:t.parent_path||"",dirEntries:t.entries||[],createPath:t.current_path||n.createPath})}catch(t){p(t)}}async function qe(e){e.preventDefault();try{await $({title:"正在创建 Agent",detail:"正在写入项目目录并启动本机 Agent",steps:["校验目录","创建 Agent","同步状态"],done:"Agent 已创建"},async t=>{t.step(0,"正在校验项目目录"),await g(160),t.step(1,"正在创建并启动 Agent"),await se({name:n.createName,project_dir:n.createPath}),o({createDialog:!1,createName:"",createPath:""}),t.step(2,"正在同步最新 Agent 状态"),await v()})}catch{}}async function X(e,t){const s=((n.device||{}).agents||[]).find(i=>i.agent_id===t)||{agent_id:t},a={restart:"重启",start:"启动",stop:"停止",remove:"删除",permission:"请求目录权限"}[e]||"处理";try{await $({title:`正在${a} Agent`,detail:`正在处理 ${s.name||s.agent_id}`,steps:["发送指令","等待执行","同步状态"],done:`Agent 已${a}`},async i=>{if(i.step(0,"正在发送控制指令"),e==="restart"&&await ge(s.agent_id),e==="start"&&await T({agent_id:s.agent_id,enabled:!0}),e==="stop"&&await T({agent_id:s.agent_id,enabled:!1}),e==="remove"&&await pe(s.agent_id),e==="permission"){const l=await k({agent_id:s.agent_id,request:!0});if(!l.accessible&&l.message)throw new Error(l.message);i.step(1,"正在重启后台服务让权限生效"),await q(),await g(800);const c=await k({agent_id:s.agent_id,request:!1});if(!c.accessible&&c.message)throw new Error(c.message)}i.step(1,"正在等待本机服务确认"),await g(180),i.step(2,"正在同步最新状态"),await v()})}catch{}}async function Be(e){try{await $({title:e?"正在开启自启动":"正在关闭自启动",detail:"正在写入系统启动项",steps:["读取配置","写入启动项","同步状态"],done:e?"自启动已开启":"自启动已关闭"},async t=>{t.step(0,"正在读取本机配置"),await g(120),t.step(1,e?"正在写入后台服务启动项":"正在移除启动项"),await N(e),t.step(2,"正在同步自启动状态"),await v()})}catch{}}async function Ne(e){try{await $({title:e?"正在开启终端兼容模式":"正在关闭终端兼容模式",detail:e?"正在切换后台服务到 Terminal 权限链路":"正在切换后台服务为直接启动",steps:["保存设置","重写启动项","重启后台服务"],done:e?"终端兼容模式已开启":"终端兼容模式已关闭"},async t=>{t.step(0,"正在保存 GUI 设置");const s=await he(e);o({terminalCompat:s.terminal_compat!==!1}),t.step(1,"正在重写后台服务启动项"),await g(250),t.step(2,"正在同步最新后台状态"),await v()})}catch{}}async function ze(e=!1){try{let t="";await $({title:e?"正在升级 Launcher":"正在检查更新",detail:e?"正在下载并调度码控更新，完成后会自动重启":"正在连接版本源",steps:e?["检查版本","下载更新包","重启码控"]:["连接版本源","对比版本","同步状态"],done:e?"码控更新已调度":"检查完成"},async s=>{s.step(0,e?"正在检查远端版本":"正在连接版本源");const a=await W();if(!e){t=a.update_available?`发现新版本 ${a.latest_version||a.target_version}`:`当前已是最新版 ${a.current_version||""}`,s.step(1,"正在对比本地版本"),await g(180),s.step(2,"正在同步本机状态"),await v();return}if(t=a.update_available?`发现新版本 ${a.latest_version||a.target_version}`:`当前已是最新版 ${a.current_version||""}`,!a.update_available){s.step(1,"当前已是最新版"),await g(180),s.step(2,"正在同步本机状态"),await v();return}s.step(1,"正在下载更新包并调度后台重启");const i=await Ve(O(),s);t=i.update_available?`发现新版本 ${i.latest_version||i.target_version}`:`当前已是最新版 ${i.current_version||""}`,s.step(2,"正在等待后台服务重启"),await Je(i.target_version||i.latest_version||a.target_version||a.latest_version),await v()}),o({error:t})}catch{}}async function Ve(e,t){let s=!1;const a=(async()=>{for(;!s;){await g(900);try{const i=await P(),l=i.state||null;o({ready:!!i.ready,device:l||n.device,logs:i.logs||n.logs}),Fe(l,t)}catch{}}})();try{return await e}finally{s=!0,await a.catch(()=>{})}}function Fe(e,t){if(!e||!n.progress)return;const s=Number(e.upgrade_progress||0),a=String(e.upgrade_message||"").trim(),i=String(e.upgrade_stage||"").trim();if(!s&&!a)return;const l=i==="checking_version"?0:i==="switching_version"||i==="starting_agents"||i==="completed"?2:1,c=n.progress.steps.map((d,h)=>h<l?{...d,status:"done"}:h===l?{...d,status:"active"}:{...d,status:"pending"});o({progress:{...n.progress,active:l,steps:c,detail:a||n.progress.detail,percent:Math.max(n.progress.percent||0,Math.min(99,s||n.progress.percent||0))}}),t&&typeof t.detail=="function"&&a&&t.detail(a)}async function Je(e){var l,c,d,h,u;const t=String(e||"").trim(),s=Date.now();let a=0,i=!1;for(;Date.now()-s<180*1e3;){await g(1e3);try{const m=await P();if(o({ready:!!m.ready,device:m.state||n.device,logs:m.logs||n.logs}),!t||((l=m.state)==null?void 0:l.launcher_version)===t)return m;w({detail:`后台已恢复，当前版本 ${((c=m.state)==null?void 0:c.launcher_version)||"未知"}，正在等待切换到 ${t}`,percent:Math.max(((d=n.progress)==null?void 0:d.percent)||0,82)})}catch{const m=Date.now()-s;if(!i||m>8e3&&a<4){i=!0,a+=1,w({detail:a===1?"正在拉起更新后的后台服务":`后台服务仍未响应，正在第 ${a} 次重试拉起`,percent:Math.max(((h=n.progress)==null?void 0:h.percent)||0,80)});try{await q()}catch{}}else w({detail:"正在等待后台服务端口恢复",percent:Math.max(((u=n.progress)==null?void 0:u.percent)||0,80)})}}throw new Error("launcher 已调度更新，但后台服务重连超时")}async function j(){if(n.ready){o({runtimeLoading:!0,error:""});try{const e=await B();o({runtimeInfo:e||null,runtimeLoading:!1})}catch(e){o({runtimeLoading:!1}),p(e)}}}async function Qe(){try{await $({title:"正在准备运行时环境",detail:"正在检测 Node、uv、Python 和 Java，必要时会使用镜像或托管源下载",steps:["检测本机环境","准备托管运行时","刷新 Agent 环境"],done:"运行时环境检测完成"},async e=>{e.step(0,"正在检测现有运行时"),await g(120),e.step(1,"正在准备缺失的托管运行时");let t=await ue();o({runtimeInfo:t||null}),w({runtime:U(t),detail:H(t)}),t=await Xe(e,t),e.step(2,"正在同步 Agent 环境"),await v(),t!=null&&t.last_error&&o({error:t.last_error})})}catch{}}async function Xe(e,t){let s=t||null;const a=Date.now(),i=15*60*1e3;for(;s!=null&&s.preparing;){if(w({runtime:U(s),detail:H(s)}),Date.now()-a>i)throw new Error("运行时仍在后台准备中，请稍后刷新环境状态");await g(1500),s=await B(),o({runtimeInfo:s||null})}return w({runtime:U(s),detail:H(s)}),s||{}}function H(e){return((e==null?void 0:e.logs)||[]).slice(-1)[0]||(e!=null&&e.preparing?"托管运行时仍在准备中":"运行时检测完成")}function U(e){if(!e)return null;const t=(e.tools||[]).filter(i=>i==null?void 0:i.name),s=e.environment||{},a=e.logs||[];return{runtimeDir:e.runtime_dir||"",preparing:!!e.preparing,tools:t,envCount:Object.keys(s).length,logs:a.slice(-5),lastError:e.last_error||""}}async function Ye(e){const t=((n.device||{}).agents||[]).find(s=>s.agent_id===e)||{agent_id:e};o({mcpDialog:!0,mcpAgent:t,mcpStatus:null,mcpLoading:!0,error:""}),await D(e)}async function D(e=""){var s;const t=e||((s=n.mcpAgent)==null?void 0:s.agent_id)||"";if(t){o({mcpLoading:!0});try{const a=await de(t);o({mcpStatus:a||null,mcpLoading:!1})}catch(a){o({mcpLoading:!1,mcpStatus:null}),p(a)}}}function Ze(){o({mcpDialog:!1,mcpAgent:null,mcpStatus:null,mcpLoading:!1})}async function et(){var e;(e=n.mcpAgent)!=null&&e.agent_id&&(await X("restart",n.mcpAgent.agent_id),await D(n.mcpAgent.agent_id))}async function K(){if(n.ready){o({sshLoading:!0,error:""});try{const e=await me();o({sshStatus:e||null,sshLoading:!1})}catch(e){o({sshLoading:!1}),p(e)}}}async function tt(){o({sshLoading:!0,sshCommand:"",sshUnixCommand:"",sshWindowsCommand:"",sshExpiresAt:"",sshCopied:!1,error:""});try{const e=await fe(),t=(e==null?void 0:e.status)||null,s=(e==null?void 0:e.error)||"";o({sshStatus:t,sshLoading:!1,error:s}),!s&&(t!=null&&t.listening)&&o({error:"OpenSSH Server 已初始化并监听本机回环地址。"})}catch(e){o({sshLoading:!1}),p(e)}}async function nt(){o({sshLoading:!0,sshCommand:"",sshUnixCommand:"",sshWindowsCommand:"",sshExpiresAt:"",sshCopied:!1,error:""});try{const e=await ae(),t=(e==null?void 0:e.unix_command)||(e==null?void 0:e.command)||"",s=(e==null?void 0:e.windows_command)||"";o({sshLoading:!1,sshUnixCommand:t,sshWindowsCommand:s,sshCommand:n.sshClientPlatform==="windows"?s:t,sshExpiresAt:(e==null?void 0:e.expires_at)||""})}catch(e){o({sshLoading:!1}),p(e)}}async function st(){if(n.sshCommand)try{const e=await Ce(n.sshCommand);o({sshCopied:e!==!1})}catch(e){p(e)}}function at(e){const t=e==="windows"?"windows":"unix";o({sshClientPlatform:t,sshCommand:t==="windows"?n.sshWindowsCommand:n.sshUnixCommand,sshCopied:!1})}async function it(){const e=String(n.sshPublicKey||"").trim();if(!e){o({error:"请粘贴客户端 SSH 公钥。"});return}o({sshLoading:!0,sshKeyResult:null,error:""});try{const t=await oe(e);o({sshLoading:!1,sshKeyResult:t||null,sshPublicKey:""})}catch(t){o({sshLoading:!1}),p(t)}}window.gui={login:Ke,refreshState:v,chooseDirectory:G,submitCreateAgent:qe,agentAction:X,toggleAutostart:Be,toggleTerminalCompat:Ne,checkUpdate:ze,loadRuntimeEnvironment:j,prepareRuntimeEnvironment:Qe,openMCPDialog:Ye,loadMCPStatus:D,closeMCPDialog:Ze,restartMCPAgent:et,loadSSHStatus:K,setupSSH:tt,createSSHTunnel:nt,copySSHCommand:st,setSSHClientPlatform:at,installSSHAuthorizedKey:it,setTab(e){o({tab:e}),e==="environment"&&j(),e==="ssh"&&K()},openCreateDialog(){o({createDialog:!0,error:""}),G(n.createPath)},closeCreateDialog(){o({createDialog:!1})},setCreateName(e){n.createName=e},setCreatePath(e){n.createPath=e},setEnableAutostart(e){n.enableAutostart=!!e},setRememberLogin(e){n.rememberLogin=!!e},setLoginUsername(e){n.loginUsername=e},setLoginPassword(e){n.loginPassword=e},setSSHPublicKey(e){n.sshPublicKey=e},closeProgress(){o({progress:null})}};function Y(){const e=n.device||{},t=e.agents||[];if(!n.authenticated){R.innerHTML=lt();return}R.innerHTML=`
    <div class="shell">
      <aside class="sidebar">
        <div class="brand">
          <div class="brand-icon">C</div>
          <div>
            <div class="brand-title">码控</div>
            <div class="brand-subtitle">本机控制台</div>
          </div>
        </div>
        <button class="nav ${n.tab==="overview"?"active":""}" onclick="gui.setTab('overview')">总览</button>
        <button class="nav ${n.tab==="agents"?"active":""}" onclick="gui.setTab('agents')">Agent</button>
        <button class="nav ${n.tab==="environment"?"active":""}" onclick="gui.setTab('environment')">环境</button>
        <button class="nav ${n.tab==="ssh"?"active":""}" onclick="gui.setTab('ssh')">远程 SSH</button>
        <button class="nav ${n.tab==="settings"?"active":""}" onclick="gui.setTab('settings')">设置</button>
        <button class="nav ${n.tab==="logs"?"active":""}" onclick="gui.setTab('logs')">诊断</button>
        <div class="sidebar-footer">
          <div class="muted">Launcher ${r(n.defaults.launcher_version||e.launcher_version||"")}</div>
          <div class="pill ${n.ready?"online":"offline"}">${n.ready?"本地已连接":"本地未启动"}</div>
        </div>
      </aside>
      <main class="main">
        <header class="topbar">
          <div>
            <h1>${rt()}</h1>
            <p>${ot()}</p>
          </div>
          <div class="top-actions">
            <button class="icon-btn" onclick="gui.refreshState()">刷新</button>
            <button class="primary" onclick="gui.openCreateDialog()" ${n.ready?"":"disabled"}>新建 Agent</button>
          </div>
        </header>
        ${n.error?`<div class="notice">${r(n.error)}</div>`:""}
        ${ct(e,t)}
      </main>
    </div>
    ${n.createDialog?wt():""}
    ${n.mcpDialog?bt():""}
    ${n.progress?St():""}
  `}function rt(){return n.tab==="agents"?"Agent 管理":n.tab==="environment"?"运行时环境":n.tab==="ssh"?"远程 SSH":n.tab==="settings"?"设置":n.tab==="logs"?"诊断":"设备总览"}function ot(){return n.tab==="agents"?"创建、启动、停止和重启当前设备上的 agent":n.tab==="environment"?"管理 MCP 常用的 Node、uv、Python 和 Java 运行时":n.tab==="ssh"?"检查本机 SSH 服务并生成 Agent 可复用的远程会话命令":n.tab==="settings"?"管理自启动、更新和服务器连接":n.tab==="logs"?"查看 launcher 最近操作和启动日志":"查看 launcher、opencode 和设备运行状态"}function lt(){return`
    <main class="login-screen">
      <div class="panel login-panel">
        <div class="login-mark" aria-hidden="true">
          <span class="login-ring"></span>
          <span class="login-core">C</span>
        </div>
        <div class="login-title">码控</div>
        ${n.error?`<div class="notice login-notice">${r(n.error)}</div>`:""}
        <form onsubmit="gui.login(event)" class="form">
          <label>账号</label>
          <input name="username" autocomplete="username" value="${r(n.loginUsername)}" oninput="gui.setLoginUsername(this.value)" />
          <label>密码</label>
          <input name="password" type="password" autocomplete="current-password" value="${r(n.loginPassword)}" oninput="gui.setLoginPassword(this.value)" />
          <label class="check-row">
            <input type="checkbox" ${n.rememberLogin?"checked":""} onchange="gui.setRememberLogin(this.checked)" />
            <span>记住并自动登录</span>
          </label>
          <label class="check-row">
            <input type="checkbox" ${n.enableAutostart?"checked":""} onchange="gui.setEnableAutostart(this.checked)" />
            <span>登录成功后开启后台服务自启动</span>
          </label>
          <button class="primary wide" type="submit" ${n.loading?"disabled":""}>${n.loading?"正在登录":"登录"}</button>
        </form>
      </div>
    </main>
  `}function ct(e,t){return n.tab==="agents"?vt(t):n.tab==="environment"?pt():n.tab==="ssh"?dt():n.tab==="settings"?ht(e):n.tab==="logs"?ft():ut(e,t)}function dt(){var a,i;const e=n.sshStatus||{},t=!!e.listening,s={windows:"Windows",darwin:"macOS",linux:"Linux"}[e.platform]||e.platform||"-";return`
    <section class="ssh-layout">
      <div class="panel ssh-status-panel">
        <div class="section-head">
          <div>
            <h2>OpenSSH Server</h2>
            <p class="muted">仅监听本机回环地址，由远程隧道转发连接</p>
          </div>
          <div class="row-actions">
            ${f("refresh","刷新 SSH 状态","gui.loadSSHStatus()")}
            <span class="pill ${t?"online":n.sshLoading?"processing":"offline"}">${n.sshLoading?"检测中":t?"已就绪":"未就绪"}</span>
          </div>
        </div>
        <div class="ssh-status-grid">
          ${_("平台",s,!!e.platform)}
          ${_("已安装",e.installed?"是":"否",!!e.installed)}
          ${_("服务运行",e.running?"是":"否",!!e.running)}
          ${_("本机监听",e.listening?e.listen_address||"127.0.0.1:22":"未监听",!!e.listening)}
        </div>
        <div class="ssh-detail">
          <span>Host key 指纹</span>
          <code>${r(e.host_key_fingerprint||"等待 SSH 服务生成")}</code>
        </div>
        ${e.message?`<div class="ssh-message">${r(e.message)}</div>`:""}
        ${e.error?`<div class="notice runtime-notice">${r(e.error)}</div>`:""}
        <button class="primary" onclick="gui.setupSSH()" ${n.sshLoading||t?"disabled":""}>${n.sshLoading?"正在处理":t?"SSH 已初始化":"初始化 SSH"}</button>
      </div>

      <div class="panel ssh-connect-panel">
        <div class="section-head">
          <div>
            <h2>Agent SSH 会话</h2>
            <p class="muted">首次命令建立后台会话并生成本地 wrapper，后续命令复用同一连接</p>
          </div>
          <span class="pill ${n.sshCommand?"processing":"offline"}">${n.sshCommand?"待启动":"未生成"}</span>
        </div>
        <div class="ssh-platform-switch" role="group" aria-label="连接端系统">
          <button class="${n.sshClientPlatform==="unix"?"active":""}" onclick="gui.setSSHClientPlatform('unix')">macOS / Linux</button>
          <button class="${n.sshClientPlatform==="windows"?"active":""}" onclick="gui.setSSHClientPlatform('windows')">Windows</button>
        </div>
        <div class="ssh-command-box ${n.sshCommand?"":"empty-command"}">
          <code>${r(n.sshCommand||"SSH 服务就绪后，在需要连接时生成命令")}</code>
          ${n.sshCommand?`<button class="icon-action" title="复制 SSH 命令" aria-label="复制 SSH 命令" onclick="gui.copySSHCommand()">${n.sshCopied?b.check:b.copy}</button>`:""}
        </div>
        <div class="ssh-command-meta">
          <span>${n.sshExpiresAt?`启动链接有效期至 ${r(n.sshExpiresAt)}；会话启动后由 SSH keepalive 保持`:"首次命令只消费一次 bootstrap；执行后使用终端输出的 Agent wrapper"}</span>
          ${n.sshCopied?'<span class="pill online">已复制</span>':""}
        </div>
        <button class="primary" onclick="gui.createSSHTunnel()" ${n.sshLoading||!t?"disabled":""}>${n.sshLoading?"正在生成":"生成 Agent SSH 命令"}</button>
      </div>

      <div class="panel ssh-key-panel">
        <div class="section-head">
          <div>
            <h2>客户端公钥</h2>
            <p class="muted">安装发起连接电脑的 SSH 公钥，用于目标账号登录认证</p>
          </div>
          <span class="pill ${(a=n.sshKeyResult)!=null&&a.installed?"online":"offline"}">${(i=n.sshKeyResult)!=null&&i.installed?"已安装":"待配置"}</span>
        </div>
        <textarea class="ssh-key-input" rows="3" spellcheck="false" value="${r(n.sshPublicKey)}" oninput="gui.setSSHPublicKey(this.value)" placeholder="ssh-ed25519 AAAA... client-name">${r(n.sshPublicKey)}</textarea>
        ${n.sshKeyResult?`
          <div class="ssh-key-result">
            <span>${r(n.sshKeyResult.message||"客户端 SSH 公钥已安装")}</span>
            <code>${r(n.sshKeyResult.fingerprint||"")}</code>
          </div>
        `:""}
        <button class="primary" onclick="gui.installSSHAuthorizedKey()" ${n.sshLoading||!t?"disabled":""}>安装客户端公钥</button>
      </div>
    </section>
  `}function _(e,t,s){return`
    <div class="ssh-metric">
      <span>${r(e)}</span>
      <strong class="${s?"ok":""}">${r(t)}</strong>
    </div>
  `}function ut(e,t){const s=e.autostart||{};return`
    <section class="grid cards">
      ${C("设备状态",z[e.status]||e.status||"未知",e.status==="running"?"online":"processing")}
      ${C("Agent 数量",String(t.length),"processing")}
      ${C("launcher 版本",e.launcher_version||n.defaults.launcher_version||"-","processing")}
      ${C("自启动",s.enabled?"已开启":"未开启",s.enabled?"online":"offline")}
    </section>
    <section class="panel">
      <div class="section-head">
        <h2>Agent 概览</h2>
        <button class="ghost" onclick="gui.setTab('agents')">查看全部</button>
      </div>
      ${t.length?Z(t.slice(0,5)):S("还没有 agent，点击右上角新建。")}
    </section>
  `}function pt(){const e=n.runtimeInfo||{},t=(e.tools||[]).filter(a=>a==null?void 0:a.name),s=e.environment||{};return`
    <section class="grid environment-grid">
      <div class="panel env-summary">
        <div class="section-head">
          <div>
            <h2>托管运行时</h2>
            <p class="muted">用于 MCP 的 npx、uvx、python、java 等命令</p>
          </div>
          <div class="row-actions">
            ${f("refresh","刷新环境状态","gui.loadRuntimeEnvironment()")}
            ${f("terminal","准备运行时","gui.prepareRuntimeEnvironment()")}
          </div>
        </div>
        <div class="runtime-path path" title="${r(e.runtime_dir||"")}">${r(e.runtime_dir||"等待检测")}</div>
        ${e.last_error?`<div class="notice runtime-notice">${r(e.last_error)}</div>`:""}
      </div>
      <div class="panel env-tools">
        <div class="section-head">
          <h2>工具状态</h2>
          <span class="pill ${e.preparing?"processing":"online"}">${e.preparing?"检测中":`${t.length} 项`}</span>
        </div>
        ${n.runtimeLoading?'<div class="empty">正在读取运行时状态</div>':gt(t)}
      </div>
      <div class="panel env-vars">
        <div class="section-head">
          <h2>注入变量</h2>
          <span class="pill processing">${Object.keys(s).length} 项</span>
        </div>
        ${mt(s)}
      </div>
      <div class="panel env-logs">
        <div class="section-head">
          <h2>检测日志</h2>
        </div>
        <div class="logs compact">${(e.logs||[]).map(a=>`<div>${r(a)}</div>`).join("")||'<div class="muted">暂无检测日志</div>'}</div>
      </div>
    </section>
  `}function gt(e){return e.length?`
    <div class="runtime-tools">
      ${e.map(t=>{const s=t.status==="available",a=t.source==="managed"?"托管":"系统";return`
          <div class="runtime-tool">
            <div class="runtime-tool-head">
              <strong>${r(t.name)}</strong>
              <span class="pill ${s?"online":"error"}">${s?a:"不可用"}</span>
            </div>
            <div class="muted">${r(t.version||(s?"已检测到版本":t.error||"不可用"))}</div>
            <div class="path runtime-tool-path" title="${r(t.path||"")}">${r(t.path||"-")}</div>
            ${t.error?`<div class="runtime-error">${r(t.error)}</div>`:""}
          </div>
        `}).join("")}
    </div>
  `:S("还没有运行时检测结果，点击准备运行时。")}function mt(e){const t=Object.entries(e);return t.length?`
    <div class="env-kv-list">
      ${t.map(([s,a])=>`
        <div class="env-kv">
          <span class="mono">${r(s)}</span>
          <span class="path" title="${r(a)}">${r(a)}</span>
        </div>
      `).join("")}
    </div>
  `:S("暂无注入变量。")}function C(e,t,s){return`
    <div class="panel metric">
      <div class="muted">${r(e)}</div>
      <strong>${r(t)}</strong>
      <div class="pill ${s}">${r(t)}</div>
    </div>
  `}function vt(e){return`
    <section class="panel">
      <div class="section-head">
        <h2>本机 Agent</h2>
        <button class="primary" onclick="gui.openCreateDialog()">新建 Agent</button>
      </div>
      ${e.length?Z(e):S("当前设备还没有 agent。")}
    </section>
  `}function Z(e){return`
    <div class="table">
      <div class="tr th">
        <div>名称</div><div>目录</div><div>状态</div><div>端口</div><div>操作</div>
      </div>
      ${e.map(t=>`
        <div class="tr">
          <div>
            <strong>${r(t.name||t.agent_id)}</strong>
            <div class="muted mono">${r(t.agent_id)}</div>
          </div>
          <div class="path">${r(t.project_dir||"-")}</div>
          <div><span class="pill ${t.status==="running"?"online":t.status==="failed"?"error":"offline"}">${r(z[t.status]||t.status||"-")}</span></div>
          <div>${r(t.port||"-")}</div>
          <div class="row-actions">
            ${t.enabled?y("stop",t.agent_id,"停止"):y("start",t.agent_id,"启动")}
            ${He(t.agent_id)}
            ${y("restart",t.agent_id,"重启")}
            ${y("permission",t.agent_id,"请求目录权限")}
            ${y("remove",t.agent_id,"删除","danger-icon")}
          </div>
        </div>
      `).join("")}
    </div>
  `}function ht(e){const t=e.autostart||{},s=e.launcher_version||n.defaults.launcher_version||"-",a=t.path||t.command||"-",i=String(t.command||"").includes("start-terminal-compat.command");return`
    <section class="settings-grid">
      <div class="panel setting-panel">
        <div class="setting-main">
          <div class="setting-title">
            <h2>后台服务</h2>
            <span class="pill ${t.enabled?"online":"offline"}">${t.enabled?"自启动":"手动"}</span>
          </div>
          <div class="setting-meta">
            <span>${r(t.method||"-")}</span>
            <span class="dot"></span>
            <span>GUI 关闭后服务保持运行</span>
          </div>
          <div class="path setting-path" title="${r(a)}">${r(a)}</div>
        </div>
        <div class="setting-actions">
          ${f("power",t.enabled?"关闭自启动":"开启自启动",`gui.toggleAutostart(${!t.enabled})`,t.enabled?"danger-icon":"")}
        </div>
      </div>
      <div class="panel setting-panel">
        <div class="setting-main">
          <div class="setting-title">
            <h2>终端兼容模式</h2>
            <span class="pill ${i?"online":n.terminalCompat?"processing":"offline"}">${i?"已接管":n.terminalCompat?"待接管":"关闭"}</span>
          </div>
          <div class="setting-meta">
            <span>使用 Terminal 权限链路启动 macOS 后台服务</span>
          </div>
          <div class="path setting-path" title="${r(t.command||"")}">${r(t.command||"-")}</div>
        </div>
        <div class="setting-actions">
          <label class="switch" title="${n.terminalCompat?"关闭终端兼容模式":"开启终端兼容模式"}">
            <input type="checkbox" ${n.terminalCompat?"checked":""} onchange="gui.toggleTerminalCompat(this.checked)" />
            <span></span>
          </label>
        </div>
      </div>
      <div class="panel setting-panel">
        <div class="setting-main">
          <div class="setting-title">
            <h2>版本更新</h2>
            <span class="pill processing">${r(s)}</span>
          </div>
          <div class="setting-meta">
            <span>检查并升级 launcher 后台服务</span>
          </div>
          <div class="path setting-path" title="${r(n.defaults.server_url||"")}">${r(n.defaults.server_url||"-")}</div>
        </div>
        <div class="setting-actions">
          ${f("search","检查更新","gui.checkUpdate(false)")}
          ${f("download","立即升级","gui.checkUpdate(true)")}
        </div>
      </div>
    </section>
  `}function ft(){return`
    <section class="panel">
      <div class="section-head">
        <h2>最近日志</h2>
        <button class="ghost" onclick="gui.refreshState()">刷新</button>
      </div>
      <div class="logs">${(n.logs||[]).map(e=>`<div>${r(e)}</div>`).join("")||'<div class="muted">暂无日志</div>'}</div>
    </section>
  `}function wt(){return`
    <div class="modal-backdrop">
      <div class="modal">
        <div class="section-head">
          <h2>新建 Agent</h2>
          <button class="icon-btn" onclick="gui.closeCreateDialog()">关闭</button>
        </div>
        <form onsubmit="gui.submitCreateAgent(event)" class="form">
          <label>Agent 名称</label>
          <input value="${r(n.createName)}" oninput="gui.setCreateName(this.value)" placeholder="例如 chat-codex" />
          <label>项目目录</label>
          <div class="inline">
            <input value="${r(n.createPath)}" oninput="gui.setCreatePath(this.value)" placeholder="/Users/..." />
            <button class="ghost" type="button" onclick="gui.chooseDirectory(document.querySelector('.inline input').value)">定位</button>
          </div>
          <div class="dir-box">
            <div class="dir-head">
              <button class="ghost" type="button" onclick="gui.chooseDirectory('${r(n.dirParent)}')" ${n.dirParent?"":"disabled"}>上级</button>
              <span class="path">${r(n.dirPath||"请选择目录")}</span>
            </div>
            <div class="dir-list">
              ${(n.dirEntries||[]).filter(e=>e.is_dir).map(e=>`
                <button class="dir-item" type="button" onclick="gui.chooseDirectory('${r(e.path)}')">${r(e.name)}</button>
              `).join("")||'<div class="muted">暂无可选目录</div>'}
            </div>
          </div>
          <button class="primary wide" type="submit" ${n.loading?"disabled":""}>创建并启动</button>
        </form>
      </div>
    </div>
  `}function bt(){const e=n.mcpAgent||{},s=(n.mcpStatus||{}).servers||[];return`
    <div class="modal-backdrop">
      <div class="modal mcp-modal">
        <div class="section-head">
          <div>
            <h2>MCP 状态</h2>
            <p class="muted">${r(e.name||e.agent_id||"Agent")}</p>
          </div>
          <div class="row-actions">
            ${f("refresh","刷新 MCP 状态","gui.loadMCPStatus()")}
            ${f("restart","重启 Agent","gui.restartMCPAgent()")}
            ${f("terminal","准备运行时","gui.prepareRuntimeEnvironment()")}
            <button class="icon-action" title="关闭" aria-label="关闭" onclick="gui.closeMCPDialog()">x</button>
          </div>
        </div>
        ${n.mcpLoading?'<div class="empty">正在读取 MCP 状态</div>':$t(s)}
      </div>
    </div>
  `}function $t(e){return e.length?`
    <div class="mcp-server-list">
      ${e.map(t=>{const s=t.tools||[],a=t.status==="connected";return`
          <details class="mcp-server" ${t.error?"open":""}>
            <summary>
              <span class="mcp-server-name">${r(t.name)}</span>
              <span class="pill ${a?"online":t.status==="failed"?"error":"processing"}">${r(yt(t.status))}</span>
              <span class="muted">工具 ${s.length} 个</span>
            </summary>
            ${t.error?`<div class="runtime-error mcp-error">${r(t.error)}</div>`:""}
            ${s.length?`
              <div class="mcp-tool-list">
                ${s.map(i=>`
                  <div class="mcp-tool">
                    <strong>${r(i.id||"")}</strong>
                    ${i.description?`<p>${r(i.description)}</p>`:""}
                  </div>
                `).join("")}
              </div>
            `:'<div class="muted mcp-empty-tools">当前 MCP 没有返回工具。</div>'}
          </details>
        `}).join("")}
    </div>
  `:S("当前 Agent 没有返回 MCP 状态。")}function yt(e){return e==="connected"?"已连接":e==="failed"?"失败":e==="disabled"?"已停用":e==="needs_auth"?"需授权":e==="needs_client_registration"?"需注册":e||"未知"}function St(){const e=n.progress;return e?`
    <div class="progress-backdrop">
      <div class="progress-dialog ${e.error?"has-error":""} ${e.complete?"is-complete":""}">
        <div class="progress-visual" aria-hidden="true">
          <div class="progress-ring"></div>
          <div class="progress-scan">
            <img class="progress-logo" src="${r(te)}" alt="" />
          </div>
        </div>
        <div class="progress-body">
          <div class="progress-head">
            <div>
              <h2 data-progress-title>${r(e.title)}</h2>
              <p data-progress-detail>${r(e.detail)}</p>
            </div>
            <span class="progress-percent" data-progress-percent>${Math.round(e.percent)}%</span>
          </div>
          <div class="progress-track">
            <div class="progress-fill" data-progress-fill style="width: ${Math.max(0,Math.min(100,e.percent))}%"></div>
          </div>
          <div class="progress-steps">
            ${e.steps.map((t,s)=>`
              <div class="progress-step ${r(t.status)}" data-progress-step>
                <span data-progress-marker>${t.status==="done"?"✓":s+1}</span>
                <div data-progress-label>${r(t.label)}</div>
              </div>
            `).join("")}
          </div>
          <div data-progress-runtime>${ee(e.runtime)}</div>
          ${e.error?'<button class="ghost progress-close" data-progress-close onclick="gui.closeProgress()">关闭</button>':""}
        </div>
      </div>
    </div>
  `:""}function At(){const e=n.progress,t=document.querySelector(".progress-dialog");if(!t)return!1;if(!e){const u=document.querySelector(".progress-backdrop");return u&&u.remove(),!0}t.classList.toggle("has-error",!!e.error),t.classList.toggle("is-complete",!!e.complete);const s=t.querySelector("[data-progress-title]"),a=t.querySelector("[data-progress-detail]"),i=t.querySelector("[data-progress-percent]"),l=t.querySelector("[data-progress-fill]"),c=t.querySelectorAll("[data-progress-step]");s&&(s.textContent=e.title||""),a&&(a.textContent=e.detail||""),i&&(i.textContent=`${Math.round(e.percent||0)}%`),l&&(l.style.width=`${Math.max(0,Math.min(100,e.percent||0))}%`),c.forEach((u,m)=>{const A=e.steps[m];if(!A)return;u.className=`progress-step ${A.status||""}`;const E=u.querySelector("[data-progress-marker]"),I=u.querySelector("[data-progress-label]");E&&(E.textContent=A.status==="done"?"✓":String(m+1)),I&&(I.textContent=A.label||"")});const d=t.querySelector("[data-progress-runtime]");d&&(d.innerHTML=ee(e.runtime));const h=t.querySelector("[data-progress-close]");if(e.error&&!h){const u=t.querySelector(".progress-body");u&&u.insertAdjacentHTML("beforeend",'<button class="ghost progress-close" data-progress-close onclick="gui.closeProgress()">关闭</button>')}else!e.error&&h&&h.remove();return!0}function ee(e){var a;if(!e)return"";const t=e.tools||[],s=t.filter(i=>i.status==="available").length;return`
    <div class="progress-runtime">
      <div class="progress-runtime-grid">
        <div>
          <span>托管目录</span>
          <strong title="${r(e.runtimeDir||"")}">${r(e.runtimeDir||"等待检测")}</strong>
        </div>
        <div>
          <span>工具</span>
          <strong>${s}/${t.length||0}</strong>
        </div>
        <div>
          <span>注入变量</span>
          <strong>${e.envCount||0} 项</strong>
        </div>
      </div>
      ${t.length?`
        <div class="progress-tool-row">
          ${t.slice(0,8).map(i=>`
            <span class="${i.status==="available"?"ok":"bad"}" title="${r(i.path||i.error||"")}">
              ${r(i.name)} · ${i.source==="managed"?"托管":"系统"}
            </span>
          `).join("")}
        </div>
      `:""}
      ${(a=e.logs)!=null&&a.length?`
        <div class="progress-log-list">
          ${e.logs.map(i=>`<div>${r(i)}</div>`).join("")}
        </div>
      `:""}
      ${e.lastError?`<div class="progress-runtime-error">${r(e.lastError)}</div>`:""}
    </div>
  `}function S(e){return`<div class="empty">${r(e)}</div>`}F("login");De();je();setInterval(()=>{n.authenticated&&v()},5e3);setInterval(Q,5*60*1e3);Y();
