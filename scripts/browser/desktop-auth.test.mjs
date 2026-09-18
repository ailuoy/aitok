import test from 'node:test';
import assert from 'node:assert/strict';
import http from 'node:http';
import { readFile, mkdtemp } from 'node:fs/promises';
import { join } from 'node:path';
import { tmpdir } from 'node:os';
import { spawn } from 'node:child_process';
import { once } from 'node:events';
import { setTimeout as delay } from 'node:timers/promises';
import { CDP } from './cdp.mjs';
import { findChrome } from './session.mjs';
import { DesktopAuth } from '../../desktop/src/auth.mjs';

test('后台登录后返回授权页，确认后真实回调客户端并恢复登录状态', {skip:!process.env.AITOK_BROWSER_SMOKE,timeout:30000}, async t => {
  const directory=await mkdtemp(join(tmpdir(),'aitok-desktop-login-'));
  const server=http.createServer(async(req,res)=>{
    const path=new URL(req.url,'http://localhost').pathname;
    try {
      const file=new URL(path.startsWith('/assets/')?'../../frontend/dist'+path:'../../frontend/dist/index.html',import.meta.url);
      res.setHeader('Content-Type',path.endsWith('.js')?'application/javascript':path.endsWith('.css')?'text/css':'text/html');res.end(await readFile(file));
    } catch {res.writeHead(404);res.end();}
  });
  server.listen(0,'127.0.0.1');await once(server,'listening');t.after(()=>{server.closeAllConnections();server.close();});
  const origin=`http://127.0.0.1:${server.address().port}`;
  let opened,grant;
  const user={id:7,email:'admin@example.test',role:'admin'};
  const auth=new DesktopAuth({profile:{origin,channel:'test'},path:join(directory,'login.json'),encryption:{isEncryptionAvailable:()=>true,encryptString:value=>Buffer.from(value),decryptString:value=>value.toString()},openExternal:async url=>{opened=url},request:async()=>new Response(JSON.stringify(grant))});
  t.after(()=>auth.close());await auth.login();
  const child=spawn(await findChrome(),[`--user-data-dir=${join(directory,'chrome')}`,'--headless=new','--remote-debugging-pipe','--no-first-run','--disable-background-networking','about:blank'],{stdio:['ignore','ignore','ignore','pipe','pipe']});t.after(()=>child.kill());
  const cdp=new CDP(child),{targetId}=await cdp.send('Target.createTarget',{url:'about:blank'}),{sessionId}=await cdp.send('Target.attachToTarget',{targetId,flatten:true});
  await cdp.send('Page.enable',{},sessionId);
  await cdp.send('Fetch.enable',{patterns:[{urlPattern:'*://*/api/*'}]},sessionId);
  cdp.on('message',message=>{
    if(message.method!=='Fetch.requestPaused')return;
    const {requestId,request}=message.params,path=new URL(request.url).pathname;let data={},status=200;
    if(path==='/api/me')data={user,accounts:[]};
    else if(path==='/api/login')data={token:'web-only-test-token'};
    else if(path==='/api/desktop-auth'){
      const input=JSON.parse(request.postData||'{}');
      grant={user,state:input.state,channel:input.channel,origin,expires_at:Math.floor(Date.now()/1000)+3600};data={token:'desktop-only-test-token'};
    }
    if(request.method==='OPTIONS')status=204;
    void cdp.send('Fetch.fulfillRequest',{requestId,responseCode:status,responseHeaders:[{name:'Content-Type',value:'application/json'},{name:'Access-Control-Allow-Origin',value:'*'},{name:'Access-Control-Allow-Headers',value:'Content-Type, Authorization, X-Aitok-Page'},{name:'Access-Control-Allow-Methods',value:'GET, POST, OPTIONS'}],body:Buffer.from(JSON.stringify(data)).toString('base64')},sessionId);
  });
  const evaluate=async expression=>(await cdp.send('Runtime.evaluate',{expression,returnByValue:true,awaitPromise:true},sessionId)).result.value;
  const wait=async check=>{for(let i=0;i<100;i++){if(await check())return;await delay(80);}assert.fail('授权页面等待超时');};
  await cdp.send('Page.navigate',{url:opened},sessionId);
  await wait(()=>evaluate('Boolean(document.querySelector("input[type=password]"))'));
  assert.ok(await evaluate('location.pathname==="/login" && new URLSearchParams(location.search).get("next").startsWith("/desktop/authorize?")'));
  await evaluate(`(() => { const set=(input,value)=>{Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value').set.call(input,value);input.dispatchEvent(new Event('input',{bubbles:true}));};set(document.querySelector('input[autocomplete=username]'),'admin@example.test');set(document.querySelector('input[type=password]'),'test-password');document.querySelector('form').requestSubmit();})()`);
  await wait(()=>evaluate('document.body.textContent.includes("确认授权登录")'));
  assert.equal(auth.snapshot().status,'pending','登录后台后仍需主动确认授权');
  await evaluate('document.querySelector(".auth-card button.primary").click()');
  await wait(()=>evaluate('document.body.textContent.includes("授权成功")'));
  assert.equal(auth.snapshot().status,'authenticated');assert.equal(auth.snapshot().user.id,7);
  await cdp.send('Page.navigate',{url:origin+'/desktop/authorize?state=bad&channel=test&callback=https://evil.invalid'},sessionId);
  await wait(()=>evaluate('document.body.textContent.includes("授权链接无效")'));
  assert.ok(await evaluate('document.querySelector(".auth-card button.primary").disabled'));
});
