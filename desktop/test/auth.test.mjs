import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, readFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { DesktopAuth } from '../src/auth.mjs';
import { profiles } from '../src/profiles.mjs';
import { LauncherManager } from '../src/manager.mjs';

async function setup(t) {
  const directory = await mkdtemp(join(tmpdir(), 'aitok-auth-test-'));
  let opened, claim, invalid = false;
  const options = { profile: profiles.test, path: join(directory, 'login.json'),
    encryption: { isEncryptionAvailable: () => true, encryptString: value => Buffer.from(value.split('').reverse().join('')), decryptString: value => value.toString().split('').reverse().join('') },
    openExternal: async url => { opened = new URL(url); },
    request: async (url, init) => {
      assert.equal(url, profiles.test.origin + '/api/desktop-auth/session');
      assert.equal(init.headers.Authorization, 'Bearer scoped-desktop-test');
      return new Response(JSON.stringify(invalid ? {error:'expired'} : claim), {status: invalid ? 401 : 200});
    },
  };
  const auth = new DesktopAuth(options); t.after(() => auth.close());
  const begin = async () => {
    await auth.login();
    claim = {user:{id:7,email:'admin@test.local',role:'admin'},expires_at:Math.floor(Date.now()/1000)+3600,state:opened.searchParams.get('state'),channel:'test',origin:profiles.test.origin};
    return opened;
  };
  const post = (url, origin = profiles.test.origin) => fetch(url, {method:'POST',headers:{Origin:origin,'Content-Type':'application/json'},body:JSON.stringify({token:'scoped-desktop-test'})});
  return {auth, options, begin, post, get claim(){return claim}, invalidate:()=>{invalid=true}};
}

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
