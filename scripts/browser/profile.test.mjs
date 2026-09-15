import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, mkdir, writeFile, readFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { configureProfile } from './profile.mjs';

test('账号邮箱写入独立浏览器原生资料名称并保留其他设置', async () => {
  const directory = await mkdtemp(join(tmpdir(), 'aitok-profile-'));
  await mkdir(join(directory, 'Default'));
  await writeFile(join(directory, 'Default', 'Preferences'), JSON.stringify({ profile: { avatar_index: 7 }, unrelated: { keep: true } }));
  await configureProfile(directory, 'account@example.com');
  const preferences = JSON.parse(await readFile(join(directory, 'Default', 'Preferences')));
  const state = JSON.parse(await readFile(join(directory, 'Local State')));
  assert.equal(preferences.profile.name, 'account@example.com');
  assert.equal(preferences.profile.avatar_index, 7);
  assert.equal(preferences.unrelated.keep, true);
  assert.equal(preferences.net.network_prediction_options, 2);
  assert.equal(state.profile.info_cache.Default.name, 'account@example.com');
  assert.equal(state.profile.info_cache.Default.is_using_default_name, false);
});
