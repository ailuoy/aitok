import { homedir } from 'node:os';
import { join, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import { parseArgs } from 'node:util';
import { findChrome, SessionBrowser } from './browser/session.mjs';
import { runWorker } from './browser/worker.mjs';
import { startLauncher } from './browser/launcher-runtime.mjs';
import { defaultLauncherPort, normalizeOrigin, validateLauncherPort } from '../shared/local-launcher.mjs';

export { createLauncher } from './browser/launcher-server.mjs';

async function main() {
  const { values } = parseArgs({ options: {
    origin: { type: 'string', default: 'http://localhost:15680' },
    port: { type: 'string' },
    chrome: { type: 'string' },
    directory: { type: 'string', default: join(homedir(), '.aitok', 'browsers') },
    help: { type: 'boolean', default: false },
    stdio: { type: 'boolean', default: false },
  } });
  if (values.help) {
    console.log('用法：node scripts/session-browser.mjs [--stdio] [--origin https://站点] [--port 15683] [--chrome 浏览器路径] [--directory 环境目录]');
    console.log('默认：线上站点 15683，本机开发站点 15684；自定义端口后请同步修改网页的「本机助手连接」。端口占用时不会结束其他进程。');
    return;
  }
  const origin = normalizeOrigin(values.origin);
  const port = validateLauncherPort(values.port || defaultLauncherPort(origin));
  if (values.stdio) {
    const browser = new SessionBrowser({ chrome: await findChrome(values.chrome), directory: resolve(values.directory) });
    runWorker(browser);
    const shutdown = () => { browser.close(); process.stdin.destroy(); };
    process.once('SIGINT', shutdown); process.once('SIGTERM', shutdown);
    return;
  }
  const runtime = await startLauncher({ origin, port, chrome: values.chrome, directory: resolve(values.directory) });
  console.log(`AiTok 本机浏览器启动器
允许站点：${origin}
本地地址：http://127.0.0.1:${port}
无需配对，网页点击“打开账号”即可。
请保持此终端运行。使用桌面助手可免终端运行。`);
  const shutdown = () => { void runtime.close().finally(() => { process.exitCode = 0; }); };
  process.once('SIGINT', shutdown); process.once('SIGTERM', shutdown);
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  main().catch(error => {
    const message = /[\u3400-\u9fff]/.test(error.message) ? error.message : ({ EACCES: '没有权限访问浏览器、配置目录或监听端口', ENOENT: '浏览器路径不存在', EADDRINUSE: '端口已占用，请修改助手和网页的连接端口' }[error.code] || '请检查浏览器路径及启动参数');
    console.error(`启动失败：${message}。使用 --help 查看说明。`);
    process.exitCode = 1;
  });
}
