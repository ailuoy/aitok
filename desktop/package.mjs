import { spawn } from 'node:child_process';
import { copyFile, mkdir, mkdtemp, readdir, rm } from 'node:fs/promises';
import { createRequire } from 'node:module';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';

const require = createRequire(import.meta.url);
const directory = fileURLToPath(new URL('.', import.meta.url));
const staging = await mkdtemp(join(tmpdir(), 'aitok-desktop-package-'));
const destination = join(directory, 'dist');

try {
  // 隔离每次打包的中间文件，构建成功后才更新安装包。
  await new Promise((resolve, reject) => {
    const child = spawn(process.execPath, [
      require.resolve('electron-builder/cli.js'),
      ...process.argv.slice(2),
      '--publish', 'never',
      `--config.directories.output=${staging}`,
    ], { cwd: directory, stdio: 'inherit' });
    child.once('error', reject);
    child.once('close', (code, signal) => {
      if (code === 0) resolve();
      else reject(new Error(`桌面打包失败：${signal || code}`));
    });
  });

  const artifacts = (await readdir(staging, { withFileTypes: true }))
    .filter(entry => entry.isFile() && /^AiTok-Assistant-.+\.(dmg|zip|exe)$/.test(entry.name));
  if (!artifacts.length) throw new Error('打包未生成可分发安装包');
  await mkdir(destination, { recursive: true });
  for (const artifact of artifacts) {
    await copyFile(join(staging, artifact.name), join(destination, artifact.name));
    console.log(`安装包：${join(destination, artifact.name)}`);
  }
} finally {
  await rm(staging, { recursive: true, force: true });
}
