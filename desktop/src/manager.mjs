import { mkdir, readFile, writeFile, rename } from 'node:fs/promises';
import { dirname } from 'node:path';
import { randomUUID } from 'node:crypto';
import { startLauncher } from '../../scripts/browser/launcher-runtime.mjs';
import { defaultLauncherPort, normalizeOrigin, validateLauncherPort } from '../../shared/local-launcher.mjs';

const defaults = () => [
  { id: randomUUID(), name: '线上环境', origin: 'https://toktopup.com', port: 15683, enabled: true },
  { id: randomUUID(), name: '本地开发', origin: 'http://localhost:15680', port: 15684, enabled: true },
];

export class LauncherManager {
  constructor({ configPath, directory, start = startLauncher, profile, authorize }) {
    this.configPath = configPath;
    this.directory = directory;
    this.start = start;
    this.profile = profile;
    this.authorize = authorize;
    this.sites = [];
    this.runtimes = new Map();
    this.errors = new Map();
    this.queue = Promise.resolve();
    this.closed = false;
  }

  // 设置写入和启停串行执行，双击或重复 IPC 不会启动第二个实例。
  run(action) {
    const result = this.queue.then(() => {
      if (this.closed) throw new Error('助手正在退出');
      return action();
    });
    this.queue = result.catch(() => {});
    return result;
  }

  async load() {
    try {
      const data = JSON.parse(await readFile(this.configPath, 'utf8'));
      if (data.version !== 1 || !Array.isArray(data.sites) || data.sites.length > 1000) throw new Error('桌面助手配置格式不正确');
      this.sites = data.sites;
      const ids = new Set(), origins = new Set(), ports = new Set();
      for (const site of this.sites) {
        if (typeof site.id !== 'string' || !site.id || ids.has(site.id)) throw new Error('站点标识重复或无效');
        ids.add(site.id);
        if (site.deleted_at) continue;
        this.validate(site, site.id);
        if (origins.has(site.origin) || ports.has(site.port)) throw new Error('站点地址或端口重复');
        origins.add(site.origin); ports.add(site.port);
      }
    } catch (error) {
      if (error.code !== 'ENOENT') throw new Error('无法读取桌面助手配置，请检查配置文件；原文件未覆盖');
      this.sites = this.profile ? [{ id: randomUUID(), name: this.profile.label, origin: this.profile.origin, port: this.profile.port, enabled: true }] : defaults();
      await this.persist();
    }
    for (const site of this.sites.filter(site => !site.deleted_at && site.enabled)) await this.startSite(site);
    return this.snapshot();
  }

  snapshot() {
    return this.sites.filter(site => !site.deleted_at).map(site => ({
      ...site, running: this.runtimes.has(site.id), error: this.errors.get(site.id) || '',
      browsers: this.runtimes.get(site.id)?.browser.environments.size || 0,
    }));
  }

  get(id) {
    const site = this.sites.find(site => site.id === id && !site.deleted_at);
    if (!site) throw new Error('站点不存在');
    return site;
  }

  validate(input, id) {
    const name = typeof input.name === 'string' ? input.name.trim() : '';
    if (!name || name.length > 60) throw new Error('站点名称为 1 至 60 个字符');
    const origin = normalizeOrigin(input.origin);
    if (this.profile && origin !== this.profile.origin) throw new Error('此安装包只能连接' + this.profile.label + '后台');
    const port = validateLauncherPort(input.port ?? defaultLauncherPort(origin));
    if (this.sites.some(site => !site.deleted_at && site.id !== id && (site.origin === origin || site.port === port))) {
      throw new Error('站点地址或端口已使用，请为不同站点选择不同端口');
    }
    return { name, origin, port, enabled: input.enabled !== false };
  }

  async persist() {
    await mkdir(dirname(this.configPath), { recursive: true, mode: 0o700 });
    const temp = this.configPath + '.tmp';
    await writeFile(temp, JSON.stringify({ version: 1, sites: this.sites }, null, 2), { mode: 0o600 });
    await rename(temp, this.configPath);
  }

  async startSite(site) {
    if (this.runtimes.has(site.id)) return;
    try {
      if (this.authorize) await this.authorize();
      const runtime = await this.start({ origin: site.origin, port: site.port, directory: this.directory, authorize: this.authorize });
      this.runtimes.set(site.id, runtime);
      this.errors.delete(site.id);
    } catch (error) {
      // 不将系统异常、Session 或代理凭据写入界面和日志。
      this.errors.set(site.id, /^[\u3400-\u9fff]/.test(error.message) ? error.message : '启动失败，请检查 Chrome / Edge 是否安装及本机权限');
    }
  }

  async stopSite(id) {
    const runtime = this.runtimes.get(id);
    if (runtime) { await runtime.close(); this.runtimes.delete(id); }
    this.errors.delete(id);
  }

  save(input) {
    return this.run(() => this.saveSite(input));
  }

  async saveSite(input) {
    const old = input.id ? this.get(input.id) : null;
    if (!old && this.sites.filter(site => !site.deleted_at).length >= 20) throw new Error('最多添加 20 个站点');
    const value = this.validate(input, old?.id);
    const now = new Date().toISOString();
    const site = { ...old, ...value, id: old?.id || randomUUID(), created_at: old?.created_at || now, updated_at: now, deleted_at: null };
    const before = this.sites;
    this.sites = old ? this.sites.map(item => item.id === old.id ? site : item) : [...this.sites, site];
    try { await this.persist(); } catch (error) { this.sites = before; throw error; }
    if (old && (old.origin !== site.origin || old.port !== site.port || !site.enabled)) await this.stopSite(old.id);
    if (site.enabled) await this.startSite(site);
    return this.snapshot();
  }

  toggle(id, enabled) {
    return this.run(() => this.saveSite({ ...this.get(id), enabled }));
  }

  remove(id) {
    return this.run(async () => {
      const site = this.get(id);
      const now = new Date().toISOString();
      const before = this.sites;
      this.sites = this.sites.map(item => item.id === site.id ? { ...item, enabled: false, deleted_at: now, updated_at: now } : item);
      try { await this.persist(); } catch (error) { this.sites = before; throw error; }
      await this.stopSite(id);
      return this.snapshot();
    });
  }

  async close() {
    this.closed = true;
    await this.queue;
    await Promise.all([...this.runtimes.keys()].map(id => this.stopSite(id)));
  }
}
