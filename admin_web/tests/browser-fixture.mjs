// Isolated browser fixture: never connects to the deployed service or database.
import http from 'node:http';
import { readFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
const root=fileURLToPath(new URL('../',import.meta.url));
const created='2026-10-07T03:00:00Z';
const resources={
 'mcp-catalog':[
  {id:'fixture-a',name:'fixture-a',title:'本地工作流',description:'面向开发任务的本地工具集合',category:'开发',enabled:true,recommended:true,config:{type:'local',command:['node','fixture.mjs'],environment:{}},launch_ready:true,created_at:created,updated_at:created},
  {id:'fixture-b',name:'fixture-b',title:'归档索引',description:'整理文档与检索归档内容',category:'检索',enabled:false,config:{type:'remote',url:'https://fixture.test/mcp'},updated_at:created}],
 skills:[{id:'skill-fixture',name:'代码审查',description:'检查修改与回归结果',category:'开发',enabled:true,content:'# 审查\n检查测试与边界情况。',updated_at:created}],
 'tool-catalog':[{id:'tool-fixture',name:'终端工具',description:'执行本地命令',category:'基础',enabled:true,permission_key:'bash',pattern:'*',default_action:'ask',updated_at:created}],
 'semantic-agents':[{id:'agent-fixture',name:'代码助手',description:'完成代码修改与测试',enabled:true,built_in:true,prompt:'检查目标后修改代码并验证。',skills:['skill-fixture'],recommended_mcp_servers:['fixture-a'],runtime_requirements:[],updated_at:created}],
 'runtime-catalog':[{id:'node-fixture',name:'Node.js',description:'JavaScript 运行环境',kind:'runtime',install_strategy:'archive',enabled:true,tags:['开发'],versions:[],mirrors:[],artifacts:[],updated_at:created}],
 'environment-presets':[{id:'env-fixture',name:'开发环境',category:'开发',description:'本地构建变量',enabled:true,variables:{MODE:'fixture'},updated_at:created}],
 operators:[{id:1,username:'admin',name:'管理员',membership_tier:'pro',upload_policy:{chunk_size:4194304,concurrency:4,target_bytes_per_second:20971520}},{id:2,username:'fixture-member',name:'测试用户',membership_tier:'free',upload_policy:{chunk_size:524288,concurrency:1,target_bytes_per_second:1048576}}],
};
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
