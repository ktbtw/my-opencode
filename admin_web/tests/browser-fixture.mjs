// Isolated browser fixture: never connects to the deployed service or database.
import http from 'node:http';
import { readFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
const root=path.resolve(fileURLToPath(new URL('../',import.meta.url)));
const created='2026-10-07T03:00:00Z';
const resources={
 'mcp-catalog':[
  {id:'fixture-a',name:'fixture-a',title:'本地工作流',description:'面向开发任务的本地工具集合',category:'开发',enabled:true,recommended:true,config:{type:'local',command:['node','fixture.mjs'],environment:{}},launch_ready:true,created_at:created,updated_at:created},
  {id:'fixture-b',name:'fixture-b',title:'归档索引',description:'整理文档与检索归档内容',category:'检索',enabled:false,config:{type:'remote',url:'https://fixture.test/mcp'},updated_at:created}],
 skills:[{id:'skill-fixture',name:'代码审查',description:'检查修改与回归结果',category:'开发',enabled:true,content:'# 审查\n检查测试与边界情况。',updated_at:created}],
 'tool-catalog':[{id:'tool-fixture',name:'终端工具',description:'执行本地命令',category:'基础',enabled:true,permission_key:'bash',pattern:'*',default_action:'ask',updated_at:created}],
 'semantic-agents':[{id:'agent-fixture',name:'代码助手',description:'完成代码修改与测试',enabled:true,built_in:true,prompt:'检查目标后修改代码并验证。',prompt_templates:{'chat.question_policy':'语义版提问策略'},skills:['skill-fixture'],recommended_mcp_servers:['fixture-a'],runtime_requirements:[],updated_at:created}],
 'runtime-catalog':[{id:'node-fixture',name:'Node.js',description:'JavaScript 运行环境',kind:'runtime',install_strategy:'archive',enabled:true,tags:['开发'],versions:[],mirrors:[],artifacts:[],updated_at:created}],
 'environment-presets':[{id:'env-fixture',name:'开发环境',category:'开发',description:'本地构建变量',enabled:true,variables:{MODE:'fixture'},updated_at:created}],
 'semantic-model-prompts':[{id:'prompt-fixture',name:'Codex 提示词',provider:'openai',model_pattern:'codex',prompt:'面向 Codex 的专属提示词。',enabled:true,sort_order:10,updated_at:created}],
 operators:[{id:1,username:'admin',name:'管理员',membership_tier:'pro',upload_policy:{chunk_size:4194304,concurrency:4,target_bytes_per_second:20971520}},{id:2,username:'fixture-member',name:'测试用户',membership_tier:'free',upload_policy:{chunk_size:524288,concurrency:1,target_bytes_per_second:1048576}}],
};
// 提示词注册表夹具：槽位定义 + 设备级覆盖明细，用于控制台 SPA 视图回归。
const promptSlots=[
 {id:'chat.base_prompt',category:'chat',scope:['platform','semantic','device'],risk:'low',default:'你是当前服务中的主工作 Agent。',platform_value:'',platform_set:false,revision:0,overrides:[],device_override_count:0},
 {id:'chat.question_policy',category:'chat',scope:['platform','semantic','device'],risk:'low',default:'需要用户确认时先提问。',platform_value:'平台版提问策略',platform_set:true,revision:2,overrides:[{semantic_agent_id:'agent-fixture',semantic_agent_name:'代码助手'}],device_override_count:1},
 {id:'chat.orchestration_prompt',category:'chat',scope:['platform','semantic','device'],risk:'low',default:'你是面向用户的主 Agent。',platform_value:'',platform_set:false,revision:0,overrides:[],device_override_count:0}];
const promptDeviceAgents=[{operator_id:1,operator_username:'admin',machine_id:'m_fixture',hostname:'fixture-host',launcher_id:'launcher:m_fixture',agent_id:'agent_fixture',agent_name:'造梦西游6',semantic_agent_id:'agent-fixture',semantic_agent_name:'代码助手',status:'running',applied_revision:'abc123'},{operator_id:1,operator_username:'admin',machine_id:'m_fixture',hostname:'fixture-host-2',launcher_id:'launcher:m_fixture',agent_id:'agent_fixture_2',agent_name:'造梦西游7',semantic_agent_id:'agent-fixture',semantic_agent_name:'代码助手',status:'running',applied_revision:''}];
const promptDeviceOverrides=()=>promptSlots.map(slot=>({slot_id:slot.id,device_value:slot.id==='chat.question_policy'?'设备版提问策略':'',semantic_value:slot.id==='chat.question_policy'?'语义版提问策略':'',platform_value:slot.platform_value,effective:slot.id==='chat.question_policy'?'设备版提问策略':slot.default,source:slot.id==='chat.question_policy'?'device':'builtin'}));
const control={fail:'',delay:'',delayMs:0,pending:0};
const send=(res,status,body)=>{res.writeHead(status,{'content-type':'application/json','cache-control':'no-store'});res.end(JSON.stringify(body));};
const body=async req=>{let text='';for await(const chunk of req)text+=chunk;return text?JSON.parse(text):{};};
const server=http.createServer(async(req,res)=>{
 try {
  const url=new URL(req.url,'http://fixture.test');
  let route=url.pathname.replace(/^\/codex(?=\/api\/)/,'');
  if(route==='/_fixture/control'){Object.assign(control,await body(req));return send(res,200,{ok:true});}
  if(route==='/_fixture/pending')return send(res,200,{pending:control.pending});
  if(route==='/api/auth/login'){
   const input=await body(req);
   if(input.username!=='admin'||input.password!=='fixture-password')return send(res,401,{error:'username or password invalid'});
   return send(res,200,{access_token:'fixture-session',operator:{id:1,username:'admin'}});
  }
  if(route==='/api/admin/prompt-registry'&&req.method==='GET'){
    if(req.headers.authorization!=='Bearer fixture-session')return send(res,401,{error:'unauthorized'});
    return send(res,200,{items:promptSlots});
  }
  if(route==='/api/admin/prompt-registry/device-agents'&&req.method==='GET'){
    if(req.headers.authorization!=='Bearer fixture-session')return send(res,401,{error:'unauthorized'});
    return send(res,200,{items:control.emptyDevices?[]:promptDeviceAgents});
  }
  if(route==='/api/admin/prompt-registry/device-overrides'){
    if(req.headers.authorization!=='Bearer fixture-session')return send(res,401,{error:'unauthorized'});
    if(req.method==='GET')return send(res,200,{operator_id:1,machine_id:url.searchParams.get('machine_id')||'',agent_id:url.searchParams.get('agent_id')||'',semantic_agent_id:'agent-fixture',items:promptDeviceOverrides()});
    const input=await body(req);
    const slotID=Object.keys(input.overrides||{})[0];
    const slot=promptSlots.find(item=>item.id===slotID);
    if(slot)slot.device_override_count=String(input.overrides[slotID]||'').trim()?1:0;
    return send(res,200,{success:true,revision:'fixture-revision',pushed:true});
  }
  if(route==='/api/admin/prompt-registry/semantic-overrides'&&req.method==='POST'){
    if(req.headers.authorization!=='Bearer fixture-session')return send(res,401,{error:'unauthorized'});
    const input=await body(req);
    const slotID=Object.keys(input.overrides||{})[0];
    const slot=promptSlots.find(item=>item.id===slotID);
    if(slot)slot.overrides=String(input.overrides[slotID]||'').trim()?[{semantic_agent_id:input.semantic_agent_id,semantic_agent_name:'代码助手'}]:[];
    return send(res,200,{success:true,semantic_agent_id:input.semantic_agent_id,pushed:1});
  }
  if(route.startsWith('/api/admin/prompt-templates/')){
    if(req.headers.authorization!=='Bearer fixture-session')return send(res,401,{error:'unauthorized'});
    const parts=route.replace('/api/admin/prompt-templates/','').split('/');
    const slot=promptSlots.find(item=>item.id===parts[0]);
    if(!slot)return send(res,404,{error:'unknown slot'});
    if(parts[1]==='reset'){slot.platform_value='';slot.platform_set=false;slot.revision=0;return send(res,200,{success:true,pushed:1});}
    const input=await body(req);
    slot.platform_value=String(input.content||'');
    slot.platform_set=Boolean(slot.platform_value.trim());
    slot.revision=(slot.revision||0)+1;
    return send(res,200,{slot_id:slot.id,revision:slot.revision,pushed:1});
  }
  if(route.startsWith('/api/')){
   if(req.headers.authorization!=='Bearer fixture-session')return send(res,401,{error:'unauthorized'});
   if(route==='/api/auth/me')return send(res,200,{id:1,username:'admin'});
   const [group,id,action]=route.replace('/api/admin/','').split('/');
   if(!resources[group])return send(res,404,{error:'fixture endpoint not found'});
   if(req.method==='GET'){
    if(control.fail===group)return send(res,503,{error:'fixture network failure'});
    if(control.delay===group){control.pending++;await new Promise(r=>setTimeout(r,control.delayMs));control.pending--;}
    return send(res,200,{items:resources[group],options:[{tier:'free',label:'普通用户'},{tier:'plus',label:'Plus 会员'},{tier:'pro',label:'Pro 会员'}]});
   }
   const input=await body(req);
   const index=resources[group].findIndex(item=>String(item.id)===id);
   if(action==='delete'){if(index>=0)resources[group].splice(index,1);return send(res,200,{ok:true});}
   if(action==='membership'){if(index>=0)resources[group][index].membership_tier=input.tier;return send(res,200,resources[group][index]);}
   if(action==='enabled'){if(index>=0)resources[group][index].enabled=input.enabled;return send(res,200,resources[group][index]);}
   const value={...(index>=0?resources[group][index]:{}),...input,id:id||input.id,updated_at:new Date().toISOString()};
   if(index>=0)resources[group][index]=value;else resources[group].push(value);
   return send(res,200,value);
  }
  route=route.replace(/^\/(?:codex-admin|codex\/admin)(?=\/)/,'');
  const requested=path.resolve(root,'.'+(route==='/'?'/index.html':route));
  if(!requested.startsWith(root+path.sep)&&requested!==path.join(root,'index.html'))return send(res,403,{error:'outside fixture root'});
  const bytes=await readFile(requested);
  const types={'.html':'text/html','.css':'text/css','.js':'text/javascript','.mjs':'text/javascript','.svg':'image/svg+xml','.woff2':'font/woff2'};
  res.writeHead(200,{'content-type':types[path.extname(requested)]||'text/plain','cache-control':'no-store'});res.end(bytes);
 }catch(error){send(res,error.code==='ENOENT'?404:500,{error:'fixture request failed'});}
});
server.listen(Number(process.env.PORT||18772),'127.0.0.1',()=>console.log('Admin browser fixture ready.'));
