import { mkdir, readFile, writeFile, rename } from 'node:fs/promises';
import { join } from 'node:path';

async function updateJSON(path, update) {
  let value = {};
  try { value = JSON.parse(await readFile(path, 'utf8')); }
  catch (error) { if (error.code !== 'ENOENT') throw new Error('浏览器资料配置无法读取，请检查账号环境目录'); }
  update(value);
  await writeFile(path + '.aitok.tmp', JSON.stringify(value), { mode: 0o600 });
  await rename(path + '.aitok.tmp', path);
}

// 只修改该账号的独立资料；原生资料菜单使用账号邮箱命名。
export async function configureProfile(directory, email) {
  await mkdir(join(directory, 'Default'), { recursive: true, mode: 0o700 });
  const name = typeof email === 'string' && email.length <= 254 ? email : 'AiTok 账号';
  await updateJSON(join(directory, 'Default', 'Preferences'), value => {
    value.profile = { ...value.profile, name, is_using_default_name: false };
    value.net = { ...value.net, network_prediction_options: 2 };
    value.browser = { ...value.browser, check_default_browser: false };
  });
  await updateJSON(join(directory, 'Local State'), value => {
    value.profile ||= {};
    value.profile.last_used = 'Default';
    value.profile.info_cache ||= {};
    value.profile.info_cache.Default = { ...value.profile.info_cache.Default, name, is_using_default_name: false };
  });
}
