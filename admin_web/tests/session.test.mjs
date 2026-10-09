import test from 'node:test';
import assert from 'node:assert/strict';
import { defaultAPIBase, normalizeAPIBase, initialAPIBase, safeBrowserStorage, readJSONResponse, fetchWithTimeout, assertAdmin } from '../session.mjs';

const hosted = new URL('https://console.test/codex-admin/');
for (const path of ['/codex-admin/', '/codex-admin', '/codex/admin/', '/codex/']) {
  test(`API prefix is retained at ${path}`, () => assert.equal(defaultAPIBase(new URL(path, hosted)), 'https://console.test/codex'));
}
test('standalone admin uses root API', () => assert.equal(defaultAPIBase(new URL('http://localhost:8080/admin/')), 'http://localhost:8080'));
for (const path of ['/codex-admin/', '/codex/admin/', '/codex/api/', '/codex/api/auth/login']) {
  test(`pasted page/API URL is normalized: ${path}`, () => assert.equal(normalizeAPIBase(`https://console.test${path}?x=1#skills`, hosted), 'https://console.test/codex'));
}
test('legacy bare origin migrates to deployed prefix', () => assert.deepEqual(initialAPIBase(hosted, hosted.origin), { baseUrl: hosted.origin+'/codex', migrated:true }));
test('legacy IP endpoint migrates to current origin', () => assert.equal(initialAPIBase(hosted, 'http://legacy.test:8888').baseUrl, hosted.origin+'/codex'));
test('explicit HTTPS custom server stays selected', () => assert.equal(initialAPIBase(hosted, 'https://custom.test/service', true).baseUrl, 'https://custom.test/service'));
test('mixed-content custom address is recovered', () => assert.equal(initialAPIBase(hosted, 'http://legacy.test', true).baseUrl, hosted.origin+'/codex'));
test('invalid saved value is recovered', () => assert.equal(initialAPIBase(hosted, 'not a URL').baseUrl, hosted.origin+'/codex'));
test('credentials and unsupported schemes are rejected', () => {
  assert.throws(() => normalizeAPIBase('https://user:secret@console.test', hosted));
  assert.throws(() => normalizeAPIBase('javascript:alert(1)', hosted));
});
test('storage getter denial still allows an in-memory login', () => {
  const browser = { get localStorage() { throw new Error('denied'); } };
  const storage = safeBrowserStorage(browser);
  assert.equal(storage.set('session','fixture'),false);
  assert.equal(storage.get('session'),'fixture');
  storage.remove('session'); assert.equal(storage.get('session'),'');
});
test('storage write denial retains in-memory state', () => {
  const storage = safeBrowserStorage({localStorage:{getItem(){throw Error('denied')},setItem(){throw Error('denied')},removeItem(){throw Error('denied')}}});
  storage.set('session','fixture'); assert.equal(storage.get('session'),'fixture');
});
test('blocked writes and deletes do not revive stale persisted sessions', () => {
  const storage = safeBrowserStorage({localStorage:{getItem(){return 'old-session'},setItem(){throw Error('quota')},removeItem(){throw Error('denied')}}});
  storage.set('session','new-session'); assert.equal(storage.get('session'),'new-session');
  storage.remove('session'); assert.equal(storage.get('session'),'');
});
test('quota failures disable repeated persistent writes while keeping memory session', () => {
  let writes = 0;
  const storage = safeBrowserStorage({localStorage:{getItem(){return ''},setItem(){writes++;throw new Error('QuotaExceededError')},removeItem(){}}});
  assert.equal(storage.set('session','new-session'),false);
  assert.equal(storage.set('username','admin'),false);
  assert.equal(writes,1);
  assert.equal(storage.get('session'),'new-session');
  assert.equal(storage.get('username'),'admin');
});
test('successful JSON is returned', async () => assert.deepEqual(await readJSONResponse(Response.json({ok:true})), {ok:true}));
test('invalid credentials get a readable inline message', async () => {
  await assert.rejects(readJSONResponse(Response.json({error:'username or password invalid'}, {status:401})), error => error.statusCode===401 && error.message.includes('账号或密码'));
});
test('SPA HTML fallback is identified instead of JSON parsing failure', async () => {
  await assert.rejects(readJSONResponse(new Response('<html></html>', {headers:{'content-type':'text/html'}})), /恢复默认服务地址/);
});
test('malformed JSON is identified', async () => await assert.rejects(readJSONResponse(new Response('{', {headers:{'content-type':'application/json'}})), /数据格式/));
test('timeout aborts stalled login', async () => {
  const stalled = (url, options) => new Promise((resolve,reject) => options.signal.addEventListener('abort', () => reject(new DOMException('aborted','AbortError'))));
  await assert.rejects(fetchWithTimeout('https://console.test', {}, 5, stalled), /请求超时/);
});
test('admin identity comes from server operator, not form username', () => {
  assertAdmin({username:'admin'}); assert.throws(() => assertAdmin({username:'member'})); assert.throws(() => assertAdmin({}));
});
