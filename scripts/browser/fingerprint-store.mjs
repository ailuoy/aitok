import { mkdir, readFile, writeFile, rename, readlink } from 'node:fs/promises';
import { join } from 'node:path';
import { randomBytes, randomUUID, createHash } from 'node:crypto';

export function profileDirectory(directory, id) {
  if (typeof id !== 'string' || !id || id.length > 300) throw new Error('浏览器环境标识无效');
  return join(directory, createHash('sha256').update(id).digest('hex'));
}

export async function readFingerprint(directory) {
  try {
    const value = JSON.parse(await readFile(join(directory, 'fingerprint.json'), 'utf8'));
    if (value.version !== 1 || !/^[a-f0-9]{64}$/.test(value.seed) || !Number.isSafeInteger(value.generation) || value.generation < 1 || !Number.isFinite(Date.parse(value.created_at))) throw new Error();
    return value;
  } catch (error) {
    if (error.code === 'ENOENT') return null;
    throw new Error('账号指纹配置无法读取，已保留原文件，请检查环境目录');
  }
}

export async function writeFingerprint(directory, previous) {
  const value = { version: 1, seed: randomBytes(32).toString('hex'), generation: (previous?.generation || 0) + 1, created_at: new Date().toISOString() };
  await mkdir(directory, { recursive: true, mode: 0o700 });
  const path = join(directory, 'fingerprint.json'), temporary = path + '.' + randomUUID() + '.tmp';
  await writeFile(temporary, JSON.stringify(value), { mode: 0o600 });
  await rename(temporary, path);
  return value;
}

export function fingerprintSummary(value) {
  if (!value) return null;
  return { id: createHash('sha256').update(value.seed).digest('hex').slice(0, 12), generation: value.generation, created_at: value.created_at, mode: 'native-noise-v1' };
}

// macOS / Linux 下拒绝修改由其他助手实例打开的 Chromium 资料。
export async function assertProfileClosed(directory) {
  try {
    const lock = await readlink(join(directory, 'SingletonLock'));
    const pid = Number(/-(\d+)$/.exec(lock)?.[1]);
    if (pid) {
      try { process.kill(pid, 0); }
      catch (error) { if (error.code === 'ESRCH') return; }
    }
    throw new Error('该账号资料正被其他浏览器进程使用，请先关闭窗口');
  } catch (error) {
    if (error.code === 'ENOENT') return;
    throw error;
  }
}
