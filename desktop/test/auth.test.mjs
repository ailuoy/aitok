import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, readFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { DesktopAuth } from '../src/auth.mjs';
import { profiles } from '../src/profiles.mjs';
import { LauncherManager } from '../src/manager.mjs';

async function setup(t) {
  const directory = await mkdtemp(join(tmpdir(), 'aitok-auth-test-'));
  let opened, claim, invalid = 0, failure = null, requests = 0;
  const options = { profile: profiles.test, path: join(directory, 'login.json'),
    encryption: { isEncryptionAvailable: () => true, encryptString: value => Buffer.from(value.split('').reverse().join('')), decryptString: value => value.toString().split('').reverse().join('') },
    openExternal: async url => { opened = new URL(url); },
    request: async (url, init) => {
      requests++;
      assert.equal(url, profiles.test.origin + '/api/desktop-auth/session');
      assert.equal(init.headers.Authorization, 'Bearer scoped-desktop-test');
      assert.equal(init.credentials, 'omit');
      assert.equal(init.cache, 'no-store');
      if (failure instanceof Error) throw failure;
      if (failure) return new Response('{}', { status: failure });
      return new Response(JSON.stringify(invalid ? {error:'expired'} : claim), {status: invalid || 200});
    },
  };
  const auth = new DesktopAuth(options); t.after(() => auth.close());
  const begin = async () => {
    await auth.login();
    claim = {user:{id:7,email:'admin@test.local',role:'admin'},expires_at:Math.floor(Date.now()/1000)+3600,state:opened.searchParams.get('state'),channel:'test',origin:profiles.test.origin};
    return opened;
  };
  const post = (url, origin = profiles.test.origin) => fetch(url, {method:'POST',headers:{Origin:origin,'Content-Type':'application/json'},body:JSON.stringify({token:'scoped-desktop-test'})});
  return {auth, options, begin, post, get claim(){return claim}, get requests(){return requests}, fail:value=>{failure=value}, invalidate:(status=401)=>{invalid=status}};
}

test('网络中断、超时和后台错误保留浏览器，重连期间拒绝新操作且恢复后不重开窗口', async t => {
  t.mock.timers.enable({ apis: ['Date'], now: Date.now() });
  const fixture = await setup(t), { auth, begin, post, options } = fixture;
  await post((await begin()).searchParams.get('callback'));
  let started = 0, closed = 0;
  const manager = new LauncherManager({
    profile: profiles.test, configPath: join(dirname(options.path), 'sites.json'),
    authorize: () => auth.check(),
    start: async () => { started++; return { browser: { environments: new Map([['account', {}]]) }, close: async () => { closed++; } }; },
  });
  t.after(() => manager.close());
  auth.onChange = () => { void manager.updateAuthentication(auth.status); };
  await manager.load();
  const saved = await readFile(options.path, 'utf8');
  for (const failure of [new TypeError('fetch failed'), new DOMException('timeout', 'TimeoutError'), 503, 502, 429]) {
    fixture.fail(failure);
    t.mock.timers.tick(30001);
    await assert.rejects(auth.check());
    await manager.queue;
    assert.equal(auth.snapshot().status, 'reconnecting');
    assert.equal(auth.snapshot().user.id, 7);
    assert.equal(manager.snapshot()[0].browsers, 1);
    assert.equal(manager.snapshot()[0].running, true);
    assert.equal(closed, 0);
    assert.equal(await readFile(options.path, 'utf8'), saved);
    const requests = fixture.requests;
    await assert.rejects(auth.check(), /自动重试/);
    assert.equal(fixture.requests, requests, '轮询与新操作不应绕过重试间隔');
  }
  fixture.fail(null);
  t.mock.timers.tick(30001);
  assert.equal((await auth.check()).id, 7);
  await manager.queue;
  assert.equal(auth.snapshot().error, '');
  assert.equal(started, 1);
  assert.equal(closed, 0);
  fixture.invalidate();
  await assert.rejects(auth.check(true), /失效/);
  await manager.queue;
  assert.equal(closed, 1);
  assert.equal(manager.snapshot()[0].running, false);
});

test('离线启动保留加密凭证，恢复后自动登录，重连时主动退出会清除凭证', async t => {
  t.mock.timers.enable({ apis: ['Date'], now: Date.now() });
  const fixture = await setup(t), { begin, post, options } = fixture;
  await post((await begin()).searchParams.get('callback'));
  fixture.fail(503);
  const restored = new DesktopAuth(options); t.after(() => restored.close());
  await restored.restore();
  assert.equal(restored.snapshot().status, 'reconnecting');
  assert.equal(restored.snapshot().user, null);
  await assert.rejects(restored.check(), /自动重试/);
  fixture.fail(null);
  t.mock.timers.tick(30001);
  assert.equal((await restored.check()).id, 7);
  fixture.fail(503);
  await assert.rejects(restored.check(true));
  await restored.logout();
  assert.equal(restored.snapshot().status, 'signed_out');
  assert.equal(JSON.parse(await readFile(options.path, 'utf8')).encrypted, '');
  await assert.rejects(restored.check(), /授权登录/);
});

test('短暂断线快速重试，持续故障退避，手动重试立即恢复且不泄露异常内容', async t => {
  t.mock.timers.enable({ apis: ['Date'], now: Date.now() });
  const fixture = await setup(t), { auth, begin, post } = fixture;
  await post((await begin()).searchParams.get('callback'));
  fixture.fail(new TypeError('private-token-must-not-appear'));
  for (const delay of [3000, 6000, 12000, 24000, 30000, 30000]) {
    await assert.rejects(auth.check(true));
    assert.equal(auth.retryAfter - Date.now(), delay);
    assert.ok(auth.snapshot().error.startsWith('网络连接失败'));
    assert.ok(!JSON.stringify(auth.snapshot()).includes('private-token'));
    const count = fixture.requests;
    t.mock.timers.tick(delay - 1);
    await assert.rejects(auth.check());
    assert.equal(fixture.requests, count);
    t.mock.timers.tick(1);
  }
  fixture.fail(null);
  await auth.check(true);
  assert.equal(auth.status, 'authenticated');
  assert.equal(auth.failures, 0);
  fixture.fail(503);
  await assert.rejects(auth.check(true));
  assert.equal(auth.retryAfter - Date.now(), 3000);
  assert.match(auth.snapshot().error, /HTTP 503/);
  fixture.fail(null);
  assert.equal((await auth.check(true)).id, 7, '立即重试绕过冷却时间');
});

test('离线不延长授权有效期，明确拒绝仍清除凭证', async t => {
  t.mock.timers.enable({ apis: ['Date'], now: Date.now() });
  const fixture = await setup(t), { auth, begin, post, options } = fixture;
  await post((await begin()).searchParams.get('callback'));
  fixture.invalidate(403);
  await assert.rejects(auth.check(true), /失效/);
  assert.equal(auth.snapshot().status, 'signed_out');
  fixture.invalidate(0);
  await post((await begin()).searchParams.get('callback'));
  fixture.fail(503);
  await assert.rejects(auth.check(true));
  t.mock.timers.tick(3600001);
  await assert.rejects(auth.check(), /过期/);
  assert.equal(auth.snapshot().status, 'signed_out');
  assert.equal(JSON.parse(await readFile(options.path, 'utf8')).encrypted, '');
});

test('后台授权回调校验来源和随机状态，保存加密凭证，重启恢复，过期失效', async t => {
  const fixture = await setup(t), {auth, begin, post, options} = fixture;
  const opened = await begin(), callback=opened.searchParams.get('callback');
  assert.equal(opened.origin, profiles.test.origin);
  assert.equal(opened.pathname, '/desktop/authorize');
  assert.equal(auth.snapshot().status, 'pending');
  assert.equal((await post(callback,'https://evil.invalid')).status,403);
  fixture.claim.state='wrong';
  assert.equal((await post(callback)).status,400);
  fixture.claim.state=opened.searchParams.get('state');
  fixture.claim.channel='production';
  assert.equal((await post(callback)).status,400);
  fixture.claim.channel='test';
  assert.equal((await post(callback)).status,200);
  assert.equal(auth.snapshot().user.id,7);
  assert.equal(auth.snapshot().status,'authenticated');
  assert.ok(!JSON.stringify(auth.snapshot()).includes('scoped-desktop-test'));
  assert.ok(!(await readFile(options.path,'utf8')).includes('scoped-desktop-test'));
  await assert.rejects(post(callback));
  const restored=new DesktopAuth(options);t.after(()=>restored.close());
  await restored.restore();assert.equal(restored.snapshot().user.id,7);
  fixture.invalidate();await assert.rejects(restored.check(true),/失效/);
  assert.equal(restored.snapshot().status,'signed_out');
  assert.equal(JSON.parse(await readFile(options.path,'utf8')).encrypted,'');
});

test('取消授权后旧回调不可用，安全存储不可用时不保存明文', async t => {
  const {auth,begin,post,options}=await setup(t);
  let opened=await begin();auth.cancel();
  await assert.rejects(post(opened.searchParams.get('callback')));
  opened=await begin();options.encryption.isEncryptionAvailable=()=>false;
  assert.equal((await post(opened.searchParams.get('callback'))).status,400);
  assert.notEqual(auth.snapshot().status,'authenticated');
});

test('两个版本默认站点独立，不能跨环境配置，未登录不启动', async t => {
  for (const profile of Object.values(profiles)) {
    const directory=await mkdtemp(join(tmpdir(),'aitok-profile-test-'));let loggedIn=false,started=0;
    const manager=new LauncherManager({profile,directory,configPath:join(directory,'sites.json'),authorize:async()=>{if(!loggedIn)throw new Error('请先授权登录');return{id:7}},start:async()=>{started++;return{browser:{environments:new Map()},close:async()=>{}}}});
    t.after(()=>manager.close());await manager.load();
    assert.equal(manager.snapshot().length,1);assert.equal(manager.snapshot()[0].origin,profile.origin);assert.equal(started,0);
    await assert.rejects(manager.save({...manager.snapshot()[0],origin:profile.channel==='test'?profiles.production.origin:profiles.test.origin}),/只能连接/);
    loggedIn=true;await manager.toggle(manager.snapshot()[0].id,true);assert.equal(started,1);
  }
});
