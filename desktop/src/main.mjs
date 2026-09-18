import { app, BrowserWindow, Menu, Tray, nativeImage, ipcMain, dialog, shell, safeStorage } from 'electron';
import { join } from 'node:path';
import { homedir } from 'node:os';
import { fileURLToPath } from 'node:url';
import { LauncherManager } from './manager.mjs';
import metadata from '../package.json' with { type: 'json' };
import { profile } from './profiles.mjs';
import { DesktopAuth } from './auth.mjs';

const root = fileURLToPath(new URL('.', import.meta.url));
// 隔离桌面冒烟测试；正式安装包不接受测试目录覆盖。
const smokeDirectory = !app.isPackaged && process.env.AITOK_DESKTOP_SMOKE_DIRECTORY;
app.setName(profile.name);
app.setPath('userData', join(app.getPath('appData'), profile.name));
if (smokeDirectory) app.setPath('userData', smokeDirectory);
let window, tray, manager, authentication, quitting = false, askingQuit = false;
const ownsLock = app.requestSingleInstanceLock();
if (!ownsLock) app.quit();

function showWindow() {
  if (!window) return;
  if (window.isMinimized()) window.restore();
  window.show(); window.focus();
}

const activeCount = () => manager?.snapshot().reduce((sum, site) => sum + site.browsers, 0) || 0;
const loginSettings = () => app.getLoginItemSettings({ args: ['--hidden'] });
async function confirm(message, detail) {
  const options = { type: 'question', title: 'AiTok 助手', message, detail, buttons: ['取消', '确认'], defaultId: 0, cancelId: 0, noLink: true };
  const result = window?.isVisible() ? await dialog.showMessageBox(window, options) : await dialog.showMessageBox(options);
  return result.response === 1;
}

async function quit() {
  if (askingQuit || quitting) return;
  askingQuit = true;
  try {
    if (activeCount() && !await confirm('退出桌面助手？', '由助手打开的账号浏览器也会关闭。')) return;
    quitting = true;
    await authentication?.close();
    await manager?.close();
    tray?.destroy();
    app.quit();
  } finally { askingQuit = false; }
}

function snapshot() {
  return { version: metadata.version, profile, auth: authentication.snapshot(), sites: manager.snapshot(), loginAtStartup: loginSettings().openAtLogin, packaged: app.isPackaged };
}

function updateTray() {
  tray.setContextMenu(Menu.buildFromTemplate([
    { label: '打开 AiTok 助手', click: showWindow },
    { label: `运行中：${manager.snapshot().filter(site => site.running).length} 个站点`, enabled: false },
    { type: 'separator' },
    { label: '退出助手', click: () => void quit() },
  ]));
}

async function handle(action, value) {
  switch (action) {
    case 'state':
      if (authentication.token && authentication.status !== 'pending') await authentication.check().catch(() => {});
      return snapshot();
    case 'login': await authentication.login(); break;
    case 'cancel-login': authentication.cancel(); break;
    case 'logout':
      if (activeCount() && !await confirm('退出登录并关闭账号窗口？', '站点配置、代理及浏览器资料继续保留。')) return snapshot();
      await authentication.logout();
      await manager.run(async () => { for (const id of [...manager.runtimes.keys()]) await manager.stopSite(id); });
      break;
    case 'save': {
      if (!value || typeof value !== 'object') throw new Error('站点配置无效');
      const old = value.id && manager.get(value.id);
      if (old && manager.snapshot().find(site => site.id === old.id)?.browsers &&
          (old.origin !== value.origin || old.port !== Number(value.port) || value.enabled === false) &&
          !await confirm('修改此站点并关闭账号窗口？', '其他站点的浏览器不受影响。')) return snapshot();
      await manager.save(value); break;
    }
    case 'toggle': {
      if (!value || typeof value.enabled !== 'boolean') throw new Error('站点状态无效');
      const site = manager.get(value.id);
      if (!value.enabled && manager.snapshot().find(item => item.id === site.id)?.browsers &&
          !await confirm('停止此站点？', '此站点打开的账号浏览器会关闭，其他站点不受影响。')) return snapshot();
      await manager.toggle(value.id, value.enabled); break;
    }
    case 'remove': {
      const site = manager.get(value);
      if (!await confirm(`移除「${site.name}」？`, '关闭此站点的账号窗口，保留本机代理配置和浏览器数据。')) return snapshot();
      await manager.remove(value); break;
    }
    case 'open-site': await shell.openExternal(manager.get(value).origin + '/admin/accounts'); break;
    case 'login-at-startup':
      if (!app.isPackaged || typeof value !== 'boolean') throw new Error('安装桌面助手后可设置开机启动');
      app.setLoginItemSettings({ openAtLogin: value, args: ['--hidden'], ...(process.platform === 'darwin' ? { openAsHidden: true } : {}) });
      break;
    default: throw new Error('不支持的操作');
  }
  updateTray();
  return snapshot();
}

app.on('second-instance', showWindow);
app.on('activate', showWindow);
app.on('window-all-closed', () => {});
app.on('before-quit', event => { if (!quitting) { event.preventDefault(); void quit(); } });

if (ownsLock) app.whenReady().then(async () => {
  authentication = new DesktopAuth({ profile, path: join(app.getPath('userData'), 'login-' + profile.channel + '.json'), encryption: safeStorage, openExternal: url => shell.openExternal(url), onChange: () => {
    if (!manager || quitting) return;
    void manager.run(async () => {
      if (authentication.status === 'authenticated') {
        for (const site of manager.sites.filter(site => !site.deleted_at && site.enabled)) await manager.startSite(site);
        showWindow();
      } else { for (const id of [...manager.runtimes.keys()]) await manager.stopSite(id); }
      if (tray) updateTray();
    }).catch(() => {});
  } });
  await authentication.restore();
  manager = new LauncherManager({
    configPath: join(app.getPath('userData'), smokeDirectory ? 'sites.json' : 'sites-' + profile.channel + '.json'),
    directory: smokeDirectory ? join(smokeDirectory, 'browsers') : join(homedir(), '.aitok', 'browsers'),
    profile,
    authorize: () => authentication.check(),
  });
  await manager.load();
  window = new BrowserWindow({
    width: 920, height: 690, minWidth: 680, minHeight: 500, show: false,
    title: profile.name, backgroundColor: '#f6f7f9', icon: join(root, 'assets/icon.png'),
    webPreferences: { preload: join(root, 'preload.cjs'), contextIsolation: true, nodeIntegration: false, sandbox: true, webSecurity: true },
  });
  window.webContents.setWindowOpenHandler(() => ({ action: 'deny' }));
  window.webContents.on('will-navigate', event => event.preventDefault());
  window.webContents.on('will-attach-webview', event => event.preventDefault());
  window.webContents.session.setPermissionRequestHandler((_contents, _permission, callback) => callback(false));
  window.webContents.session.setPermissionCheckHandler(() => false);
  window.on('close', event => { if (!quitting) { event.preventDefault(); window.hide(); } });
  ipcMain.handle('aitok:assistant', async (event, action, value) => {
    if (event.sender !== window.webContents || event.senderFrame !== window.webContents.mainFrame) throw new Error('不允许的调用来源');
    try { return { data: await handle(action, value) }; }
    catch (error) { return { error: /^[\u3400-\u9fff]/.test(error.message) ? error.message : '操作失败，请检查本机权限或稍后重试' }; }
  });
  const trayImage = nativeImage.createFromPath(join(root, process.platform === 'darwin' ? 'assets/trayTemplate.png' : 'assets/icon.png')).resize({ width: 20, height: 20 });
  if (process.platform === 'darwin') trayImage.setTemplateImage(true);
  tray = new Tray(trayImage);
  tray.setToolTip('AiTok 助手');
  tray.on('click', showWindow);
  updateTray();
  const menu = [
    ...(process.platform === 'darwin' ? [{ label: 'AiTok 助手', submenu: [{ label: '显示助手', click: showWindow }, { type: 'separator' }, { label: '退出助手', accelerator: 'Cmd+Q', click: () => void quit() }] }] : []),
    { label: '编辑', submenu: [{ role: 'undo' }, { role: 'redo' }, { type: 'separator' }, { role: 'cut' }, { role: 'copy' }, { role: 'paste' }, { role: 'selectAll' }] },
  ];
  Menu.setApplicationMenu(Menu.buildFromTemplate(menu));
  await window.loadFile(join(root, 'ui/index.html'));
  if (!process.argv.includes('--hidden') && !loginSettings().wasOpenedAtLogin && !loginSettings().wasOpenedAsHidden) showWindow();
}).catch(async () => {
  dialog.showErrorBox('AiTok 助手启动失败', '请检查本机配置文件是否有效以及目录访问权限；已有配置不会被覆盖。');
  quitting = true;
  await manager?.close();
  app.quit();
});
