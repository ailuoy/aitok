import { createHash } from 'node:crypto';
import { join } from 'node:path';
import { createLauncher } from './launcher-server.mjs';
import { listenLocal } from './launcher-port.mjs';
import { ProxyStore } from './proxy-store.mjs';
import { findChrome, SessionBrowser } from './session.mjs';
import { normalizeOrigin, validateLauncherPort } from '../../shared/local-launcher.mjs';

export async function startLauncher({ origin, port, directory, chrome, browserFactory }) {
  origin = normalizeOrigin(origin);
  port = validateLauncherPort(port);
  const store = await new ProxyStore(join(directory, 'settings', createHash('sha256').update(origin).digest('hex'))).load();
  // 保留现有配置和浏览器目录；标识包含站点，HTTP 层强制校验站点归属。
  const browser = browserFactory ? await browserFactory({ directory, origin }) : new SessionBrowser({ chrome: await findChrome(chrome), directory });
  const server = createLauncher({ origin, browser, store, enforceOriginScope: true });
  server.requestTimeout = 30000;
  try { await listenLocal(server, port); }
  catch (error) { await browser.close(); throw error; }
  let closing;
  return {
    origin, port, browser, store,
    close() {
      return closing ||= (async () => {
        const closed = new Promise(resolve => server.close(resolve));
        server.closeAllConnections();
        await browser.close();
        await closed;
        await store.queue;
      })();
    },
  };
}
