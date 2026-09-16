import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, readFile, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { LauncherManager } from '../src/manager.mjs';

async function setup(t) {
  const directory = await mkdtemp(join(tmpdir(), 'aitok-desktop-test-'));
  const starts = [], closes = [];
  const options = { directory, configPath: join(directory, 'sites.json'), start: async site => {
    starts.push(site);
    if (site.port === 18003) throw Object.assign(new Error('端口 18003 已被占用'), { code: 'EADDRINUSE' });
    return { browser: { environments: new Map() }, close: async () => closes.push(site.origin) };
  } };
  const manager = new LauncherManager(options);
  t.after(() => manager.close());
  await manager.load();
  return { manager, starts, closes, options };
}

test('线上和开发环境同时启动，重复点击启动不生成第二个实例', async t => {
  const { manager, starts, closes } = await setup(t);
  assert.deepEqual(starts.map(site => site.port), [15683, 15684]);
  const [prod, dev] = manager.snapshot();
  await Promise.all([manager.toggle(prod.id, true), manager.toggle(prod.id, true)]);
  assert.equal(starts.length, 2);
  await manager.toggle(dev.id, false);
  assert.deepEqual(closes, [dev.origin]);
  assert.equal(manager.snapshot().find(site => site.id === prod.id).running, true);
  assert.equal(manager.snapshot().find(site => site.id === dev.id).enabled, false);
});

test('修改站点端口只重启该环境，设置持久化且重启后仍然生效', async t => {
  const { manager, starts, closes, options } = await setup(t);
  const [prod, dev] = manager.snapshot();
  await manager.save({ ...dev, port: 18002 });
  assert.deepEqual(closes, [dev.origin]);
  assert.equal(starts.at(-1).port, 18002);
  await manager.toggle(prod.id, false);
  await manager.close();
  const reloaded = new LauncherManager(options);
  t.after(() => reloaded.close());
  await reloaded.load();
  assert.equal(reloaded.snapshot().find(site => site.id === dev.id).port, 18002);
  assert.equal(reloaded.snapshot().find(site => site.id === prod.id).running, false);
});

test('排队的启停使用最新配置，不覆盖之前保存的端口和名称', async t => {
  const { manager, starts, options } = await setup(t);
  const [, dev] = manager.snapshot();
  await Promise.all([
    manager.save({ ...dev, name: '开发新配置', port: 18002 }),
    manager.toggle(dev.id, false),
    manager.toggle(dev.id, true),
  ]);
  const site = manager.get(dev.id);
  assert.equal(site.name, '开发新配置');
  assert.equal(site.port, 18002);
  assert.equal(site.enabled, true);
  assert.equal(starts.at(-1).port, 18002);
  const saved = JSON.parse(await readFile(options.configPath, 'utf8'));
  assert.equal(saved.sites.find(item => item.id === dev.id).port, 18002);
});

test('端口或地址重复被拒绝，移除保留配置历史并可重新添加', async t => {
  const { manager, options } = await setup(t);
  const [prod, dev] = manager.snapshot();
  await assert.rejects(manager.save({ ...dev, port: prod.port }), /已使用/);
  await assert.rejects(manager.save({ ...dev, port: 0 }), /1024/);
  await assert.rejects(manager.save({ name: '重复', origin: prod.origin, port: 18001 }), /已使用/);
  await assert.rejects(manager.save({ ...prod, origin: 'https://toktopup.com/other' }), /不包含路径/);
  await manager.remove(dev.id);
  assert.equal(manager.snapshot().length, 1);
  const saved = JSON.parse(await readFile(options.configPath, 'utf8'));
  assert.ok(saved.sites.find(site => site.id === dev.id).deleted_at);
  await manager.save({ name: '新开发环境', origin: dev.origin, port: dev.port });
  assert.notEqual(manager.snapshot()[1].id, dev.id);
});

test('编辑和启停同时排队时，启停保留刚保存的站点配置', async t => {
  const { manager, options } = await setup(t);
  const [, dev] = manager.snapshot();
  await Promise.all([
    manager.save({ ...dev, name: '新开发环境', port: 18002 }),
    manager.toggle(dev.id, false),
  ]);
  const site = manager.snapshot().find(item => item.id === dev.id);
  assert.equal(site.name, '新开发环境');
  assert.equal(site.port, 18002);
  assert.equal(site.enabled, false);
  assert.equal(site.running, false);
  const saved = JSON.parse(await readFile(options.configPath, 'utf8'));
  assert.equal(saved.sites.find(item => item.id === dev.id).port, 18002);
});

test('一个站点启动失败不阻止其他站点，损坏的配置不会被默认值覆盖', async t => {
  const { manager, options } = await setup(t);
  await manager.save({ name: '冲突环境', origin: 'https://test.example', port: 18003 });
  const sites = manager.snapshot();
  assert.equal(sites.filter(site => site.running).length, 2);
  assert.match(sites.at(-1).error, /已被占用/);
  await manager.close();
  await writeFile(options.configPath, 'broken config');
  const broken = new LauncherManager(options);
  await assert.rejects(broken.load(), /原文件未覆盖/);
  assert.equal(await readFile(options.configPath, 'utf8'), 'broken config');
});
