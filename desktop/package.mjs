import { spawn } from 'node:child_process';
import { copyFile, mkdir, mkdtemp, readdir, rm } from 'node:fs/promises';
import { createRequire } from 'node:module';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { profiles } from './src/profiles.mjs';

const require = createRequire(import.meta.url);
const directory = fileURLToPath(new URL('.', import.meta.url));
const destination = join(directory, 'dist');
const args = process.argv.slice(2);
const selected = args.find(arg => arg.startsWith('--channel='))?.split('=')[1];
const channels = selected ? [selected] : ['test', 'production'];
if (channels.some(channel => !profiles[channel])) throw new Error('打包环境必须是 test 或 production');

const run = args => new Promise((resolve, reject) => {
  const child = spawn(process.execPath, args, { cwd: directory, stdio: 'inherit' });
  child.once('error', reject);
  child.once('close', (code, signal) => code === 0 ? resolve() : reject(new Error(`桌面打包失败：${signal || code}`)));
});

for (const channel of channels) {
  const staging = await mkdtemp(join(tmpdir(), 'aitok-desktop-package-'));
  try {
    // 隔离每次打包的中间文件，构建成功后才更新安装包。
    await run(['build.mjs', '--channel=' + channel]);
    await run([
      require.resolve('electron-builder/cli.js'),
      ...args.filter(arg => !arg.startsWith('--channel=')),
      '--publish', 'never',
      `--config.directories.output=${staging}`,
      `--config.appId=${profiles[channel].appId}`,
      `--config.productName=${profiles[channel].name}`,
      '--config.artifactName=AiTok-Assistant-${version}-' + channel + '-${os}-${arch}.${ext}',
    ]);

    const artifacts = (await readdir(staging, { withFileTypes: true }))
      .filter(entry => entry.isFile() && /^AiTok-Assistant-.+\.(dmg|exe)$/.test(entry.name));
    if (!artifacts.length) throw new Error('打包未生成可分发安装包');
    await mkdir(destination, { recursive: true });
    for (const artifact of artifacts) {
      await copyFile(join(staging, artifact.name), join(destination, artifact.name));
      console.log(`安装包：${join(destination, artifact.name)}`);
    }
  } finally {
    await rm(staging, { recursive: true, force: true });
  }
}
