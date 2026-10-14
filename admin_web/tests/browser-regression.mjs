// Run with the isolated browser-fixture.mjs server, using the installed agent-browser CLI.
import {spawnSync} from 'node:child_process';
import assert from 'node:assert/strict';
const base='http://127.0.0.1:18772';
const session='chat-admin-regression';
let passes=0;
function run(...args){
 const r=spawnSync('agent-browser',['--session',session,'--json',...args],{encoding:'utf8',timeout:45000});
 if(r.status!==0)throw new Error(r.stderr||r.stdout||'browser command failed');
 const value=JSON.parse(r.stdout.trim());
 if(!value.success)throw new Error(value.error);
 return value.data;
}
function check(expression,message){assert.equal(run('eval',expression).result,true,message);passes++;console.log('PASS '+message);}
function ready(){run('wait','--fn','!document.querySelector("#appView").classList.contains("hidden") && document.querySelector("#loadingState").classList.contains("hidden")');}
async function control(value){const response=await fetch(base+'/_fixture/control',{method:'POST',body:JSON.stringify(value)});assert.equal(response.status,200);}
async function login(){run('fill','#loginUsername','admin');run('fill','#loginPassword','fixture-password');run('click','#loginButton');ready();}
try{
 run('set','viewport','1440','960');
 run('open',base+'/');
 run('click','#loginButton');
 check('!document.querySelector("#loginError").classList.contains("hidden")','empty credentials show inline validation');
 run('fill','#loginUsername','admin');run('fill','#loginPassword','wrong-fixture-password');run('click','#loginButton');
 run('wait','--fn','document.querySelector("#loginError").textContent.includes("账号或密码")');
 check('document.querySelector("#appView").classList.contains("hidden") && !document.querySelector("#loginButton").disabled','failed login remains usable');
 await login();
 check('document.querySelectorAll("#grid article").length===2 && document.querySelector("#statTotal").textContent==="2"','login reaches real resource rendering');
 run('eval','document.activeElement.blur()');run('press','/');
 check('document.activeElement.id==="searchInput"','slash focuses search');
 run('fill','#searchInput','no-matching-fixture');
 check('!document.querySelector("#emptyState").classList.contains("hidden") && document.querySelectorAll("#grid article").length===0','search shows contextual empty state');
 run('click','#emptyResetButton');
 run('wait','--fn','document.querySelector("#searchInput").value==="" && document.querySelectorAll("#grid article").length===2');
 run('click','#statusFilterGroup .custom-select-trigger');
 run('wait','--fn','document.querySelector("#statusFilterGroup .custom-select").classList.contains("open")');
 run('click','#statusFilterGroup [data-select-value=disabled]');
 run('wait','--fn','document.querySelector("#statusFilter").value==="disabled"');
 check('document.querySelectorAll("#grid article").length===1','status filtering works');
 run('click','#clearFiltersButton');
 run('wait','--fn','document.querySelector("#statusFilter").value==="all"');
 run('click','#listViewButton');run('reload');ready();
 check('document.querySelector("#grid").classList.contains("list-mode") && document.querySelector("#listViewButton").getAttribute("aria-pressed")==="true"','list display mode survives reload');
 run('press','Control+k');run('fill','#commandInput','Skill');run('press','Enter');ready();
 check('document.querySelector("#topbarTitle").textContent==="Skill 库" && document.querySelectorAll("#grid article").length===1','command search and keyboard navigation work');
 for(const [view,title] of [['tools','工具库'],['semantic','语义 Agent'],['runtime','运行时环境'],['environment','环境变量预设'],['modelPrompts','模型提示词预设'],['prompts','提示词管理'],['operators','会员管理']]){
  run('scrollintoview',`a[data-view=${view}]`);run('click',`a[data-view=${view}]`);ready();
  check(`document.querySelector('#topbarTitle').textContent===${JSON.stringify(title)} && document.querySelectorAll('#grid article').length>0`,`${title} renders successfully`);
 }
 check('document.querySelector("#newButton").classList.contains("hidden") && document.querySelector("#statusFilterGroup").classList.contains("hidden")','membership view hides irrelevant resource controls');
 for(const view of ['skills','semantic','runtime','environment','modelPrompts','prompts','tools']){
  run('scrollintoview',`a[data-view=${view}]`);run('click',`a[data-view=${view}]`);ready();run('click','#grid [data-action=edit]');
  run('wait','--fn','!document.querySelector("#drawerOverlay").classList.contains("hidden")');
  check('document.querySelector("#appView").inert && document.querySelector("#drawerOverlay input:focus")!==null',`${view} editor opens with keyboard focus`);
  if(view==='modelPrompts'){
   check('document.querySelector("#modelPromptFieldPrompt")!==null && [...document.querySelectorAll(".form-section")].filter(s=>!s.classList.contains("hidden")).length===1','model prompt editor shows only its own section');
  }
  if(view==='prompts'){
   check('document.querySelector("#promptPlatformValue")!==null && document.querySelectorAll("#promptLevelTabs [data-prompt-level]").length===3 && [...document.querySelectorAll(".form-section")].filter(s=>!s.classList.contains("hidden")).length===1','prompt registry editor shows three override levels in one section');
   run('click','[data-prompt-level=device]');
   run('wait','--fn','document.querySelector("#promptDeviceAgentSelect").options.length>0 && document.querySelector("#promptDeviceHint").textContent.includes("生效来源")');
   check('document.querySelector("#promptDeviceHint").textContent.length>0','device level loads per-slot override detail');
  }
  run('press','Escape');
 }
 run('click','a[data-view=mcpCatalog]');ready();run('click','#grid [data-id=fixture-a][data-action=edit]');
 run('fill','#fieldDescription','unsaved fixture change');run('press','Escape');
 check('!document.querySelector("#confirmOverlay").classList.contains("hidden")','closing a modified editor asks to discard changes');
 run('press','Escape');
 check('!document.querySelector("#drawerOverlay").classList.contains("hidden") && document.querySelector("#fieldDescription").value==="unsaved fixture change"','canceling discard preserves the draft');
 run('press','Escape');run('click','#confirmOk');
 check('document.querySelector("#drawerOverlay").classList.contains("hidden") && !document.querySelector("#appView").inert','confirming discard restores the workspace');
 run('click','#grid [data-id=fixture-a][data-action=edit]');run('fill','#fieldTitle','保存后的工作流');run('click','#saveButton');
 run('wait','--fn','document.querySelector("#drawerOverlay").classList.contains("hidden") && document.querySelector("#grid").textContent.includes("保存后的工作流")');
 check('document.querySelector("#grid").textContent.includes("保存后的工作流")','existing resource edit is saved and refreshed');
 run('click','#grid [data-id=fixture-a][data-action=toggle]');
 run('wait','--fn','document.querySelector("#grid [data-id=fixture-a][data-action=toggle]").textContent.trim()==="启用"');
 check('document.querySelector("#statDisabled").textContent==="2"','enable and disable updates statistics');
 run('click','#newButton');run('fill','#catalogFieldId','fixture-created');run('fill','#fieldName','fixture-created');run('fill','#fieldTitle','新增回归资源');run('fill','#fieldCommand','node\nfixture.mjs');run('click','#saveButton');
 run('wait','--fn','document.querySelector("#drawerOverlay").classList.contains("hidden") && document.querySelectorAll("#grid article").length===3');
 check('document.querySelectorAll("#grid article").length===3','resource creation keeps existing behavior');
 run('click','#grid [data-id=fixture-created][data-action=delete]');run('click','#confirmCancel');
 check('document.querySelectorAll("#grid article").length===3','canceling deletion leaves the resource intact');
 run('click','#grid [data-id=fixture-created][data-action=delete]');run('click','#confirmOk');
 run('wait','--fn','document.querySelectorAll("#grid article").length===2');
 check('document.querySelectorAll("#grid article").length===2','confirmed deletion refreshes the resource list');
 run('scrollintoview','a[data-view=modelPrompts]');run('click','a[data-view=modelPrompts]');ready();
 run('click','#newButton');run('fill','#modelPromptFieldId','fixture-deepseek');run('fill','#modelPromptFieldName','DeepSeek 预设');run('fill','#modelPromptFieldPattern','deepseek');run('fill','#modelPromptFieldPrompt','面向 DeepSeek 的提示词。');
 run('click','#saveButton');
 run('wait','--fn','document.querySelector("#drawerOverlay").classList.contains("hidden") && document.querySelectorAll("#grid article").length===2');
 check('document.querySelector("#grid").textContent.includes("DeepSeek 预设")','model prompt preset creation persists');
 run('click','#grid [data-id=fixture-deepseek][data-action=toggle]');
 run('wait','--fn','document.querySelector("#grid [data-id=fixture-deepseek][data-action=toggle]").textContent.trim()==="启用"');
 check('document.querySelector("#grid").textContent.includes("DeepSeek 预设")','disabling a model prompt preset keeps its other fields');
 run('click','#grid [data-id=fixture-deepseek][data-action=delete]');run('click','#confirmOk');
 run('wait','--fn','document.querySelectorAll("#grid article").length===1');
 check('document.querySelectorAll("#grid article").length===1 && !document.querySelector("#grid").textContent.includes("DeepSeek 预设")','confirmed deletion removes the model prompt preset');
 run('click','#newButton');run('fill','#modelPromptFieldId','fixture-invalid');run('fill','#modelPromptFieldPrompt','缺少匹配规则');run('click','#saveButton');
 run('wait','--fn','document.querySelector("#toast").textContent.includes("供应商与模型名至少填写一项")');
 check('!document.querySelector("#drawerOverlay").classList.contains("hidden")','model prompt preset rejects a missing match rule');
 run('press','Escape');run('press','Escape');run('press','Escape');run('click','#confirmOk');
 check('document.querySelector("#drawerOverlay").classList.contains("hidden") && !document.querySelector("#appView").inert','discarding the invalid model prompt draft closes the editor');
 run('scrollintoview','a[data-view=mcpCatalog]');run('click','a[data-view=mcpCatalog]');ready();
 await control({fail:'mcp-catalog'});run('click','#refreshButton');
 run('wait','--fn','!document.querySelector("#loadError").classList.contains("hidden")');
 check('!document.querySelector("#appView").classList.contains("hidden") && document.querySelector("#emptyState").classList.contains("hidden")','network failure is not mistaken for logout or empty data');
 await control({fail:''});run('click','#retryLoadButton');ready();
 check('document.querySelector("#loadError").classList.contains("hidden") && document.querySelectorAll("#grid article").length===2','retry recovers resource loading');
 await control({delay:'skills',delayMs:1500});
 run('click','a[data-view=skills]');run('click','a[data-view=tools]');ready();
 while((await (await fetch(base+'/_fixture/pending')).json()).pending)await new Promise(r=>setTimeout(r,100));
 check('document.querySelector("#topbarTitle").textContent==="工具库" && document.querySelectorAll("#grid article").length===1','late module response does not replace the active view');
 await control({delay:'',delayMs:0});
 run('set','viewport','390','844');run('click','#menuButton');
 check('document.querySelector("#sidebar").classList.contains("menu-open")','mobile navigation opens on demand');
 run('click','a[data-view=mcpCatalog]');ready();
 check('!document.querySelector("#sidebar").classList.contains("menu-open") && document.documentElement.scrollWidth<=innerWidth','mobile layout stays within the viewport');
 run('click','#grid [data-id=fixture-a][data-action=edit]');
 check('document.documentElement.scrollWidth<=innerWidth && !document.querySelector("#drawerOverlay").classList.contains("hidden")','mobile editor remains usable without horizontal scrolling');
 run('press','Escape');run('set','viewport','1440','960');
 run('click','#logoutButton');
 check('document.querySelector("#appView").classList.contains("hidden") && document.querySelector("#loginError").classList.contains("hidden")','explicit logout returns to a clean login form');
 await login();run('eval','localStorage.setItem("chat_codex_admin_token","expired-fixture-session")');run('reload');
 run('wait','--fn','document.querySelector("#loginError").textContent.includes("登录已过期")');
 check('!localStorage.getItem("chat_codex_admin_token") && document.querySelector("#appView").classList.contains("hidden")','expired saved session is cleared with an actionable message');
 run('eval','localStorage.setItem("chat_codex_admin_base_url","http://legacy-fixture.test:8888");localStorage.setItem("chat_codex_admin_custom_server","false");localStorage.setItem("chat_codex_admin_token","fixture-session")');
 run('open',base+'/codex-admin/');
 check(`document.querySelector('#loginBaseUrl').value===${JSON.stringify(base+'/codex')} && !localStorage.getItem('chat_codex_admin_token')`,'legacy service address is migrated at the deployed-style entry');
 await login();
 check('document.querySelectorAll("#grid article").length===2','login also works under the deployed API prefix');
 assert.deepEqual(run('errors').errors||[],[]);
 console.log(`PASS ${passes} browser assertions`);
}finally{
 await control({fail:'',delay:'',delayMs:0});
 run('close');
}
