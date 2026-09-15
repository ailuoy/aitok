import { mkdir, open, readFile, rename, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { createCipheriv, createDecipheriv, createHash, randomBytes, randomUUID } from 'node:crypto';
import { parseProxy } from './proxy.mjs';

export function proxyURL(proxy) {
  const host = proxy.host.includes(':') ? `[${proxy.host}]` : proxy.host;
  const auth = proxy.username ? `${encodeURIComponent(proxy.username)}:${encodeURIComponent(proxy.password)}@` : '';
  return `socks5://${auth}${host}:${proxy.port}`;
}

// 代理密码与账号绑定只保存在本机；列表不返回密码，编辑通过专用详情接口读取。
export class ProxyStore {
  constructor(directory) { this.directory = directory; this.queue = Promise.resolve(); }

  async load() {
    await mkdir(this.directory, { recursive: true, mode: 0o700 });
    const keyPath = join(this.directory, 'proxy.key');
    try {
      const file = await open(keyPath, 'wx', 0o600);
      try { await file.writeFile(randomBytes(32)); } finally { await file.close(); }
    } catch (error) { if (error.code !== 'EEXIST') throw error; }
    this.key = await readFile(keyPath);
    this.path = join(this.directory, 'proxies.enc');
    try {
      const data = await readFile(this.path);
      const decipher = createDecipheriv('aes-256-gcm', this.key, data.subarray(0, 12));
      decipher.setAuthTag(data.subarray(12, 28));
      this.data = JSON.parse(Buffer.concat([decipher.update(data.subarray(28)), decipher.final()]).toString());
    } catch (error) {
      if (error.code !== 'ENOENT') throw new Error('本机代理配置无法解密，请检查启动器目录');
      this.data = { proxies: [], bindings: {} };
    }
    return this;
  }

  change(callback) {
    const task = this.queue.then(async () => {
      const next = structuredClone(this.data);
      const result = callback(next);
      const nonce = randomBytes(12), cipher = createCipheriv('aes-256-gcm', this.key, nonce);
      const encrypted = Buffer.concat([cipher.update(JSON.stringify(next)), cipher.final()]);
      const temporary = this.path + '.tmp';
      await writeFile(temporary, Buffer.concat([nonce, cipher.getAuthTag(), encrypted]), { mode: 0o600 });
      await rename(temporary, this.path);
      this.data = next;
      return result;
    });
    this.queue = task.catch(() => {});
    return task;
  }

  list() {
    return { proxies: this.data.proxies.map(({ password, ...proxy }) => ({ ...proxy, has_password: Boolean(password) })), bindings: this.data.bindings };
  }

  get(id) {
    const proxy = this.data.proxies.find(proxy => proxy.id === id);
    if (!proxy) throw new Error('代理不存在，请刷新列表');
    return proxy;
  }

  url(id) {
    return proxyURL(this.get(id));
  }

  prepare(id, input) {
    const old = id ? this.get(id) : null;
    const name = typeof input.name === 'string' ? input.name.trim() : '';
    const host = typeof input.host === 'string' ? input.host.trim().replace(/^\[|\]$/g, '') : '';
    const username = typeof input.username === 'string' ? input.username : '';
    const password = input.password === undefined ? old?.password || '' : input.password;
    if (!name || name.length > 80 || !host || host.length > 253 || /[\s/@?#]/.test(host) || typeof password !== 'string') throw new Error('请填写有效的代理名称、主机和认证信息');
    const port = Number(input.port);
    if (!Number.isInteger(port) || port < 1 || port > 65535) throw new Error('代理端口必须介于 1 和 65535');
    if (Boolean(username) !== Boolean(password)) throw new Error('用户名和密码需同时填写或同时清空');
    const value = { name, host, port, username, password };
    parseProxy(proxyURL(value));
    return value;
  }

  save(id, input, lastTest = null) {
    return this.change(data => {
      const value = { ...this.prepare(id, input), id: id || randomUUID(), last_test: lastTest };
      const old = id ? data.proxies.find(proxy => proxy.id === id) : null;
      if (old) data.proxies[data.proxies.indexOf(old)] = value;
      else {
        if (data.proxies.length >= 500) throw new Error('最多保存 500 条代理');
        data.proxies.push(value);
      }
      return { id: value.id };
    });
  }

  remove(id) {
    return this.change(data => {
      if (Object.values(data.bindings).includes(id)) throw new Error('此代理仍有账号使用，请先切换这些账号的代理再删除');
      data.proxies = data.proxies.filter(proxy => proxy.id !== id);
    });
  }

  bind(environmentID, proxyID) {
    return this.change(data => {
      if (typeof environmentID !== 'string' || !environmentID || environmentID.length > 300) throw new Error('账号环境标识无效');
      if (proxyID !== null && !data.proxies.some(proxy => proxy.id === proxyID)) throw new Error('代理不存在');
      if (proxyID === null) delete data.bindings[environmentID];
      else data.bindings[environmentID] = proxyID;
    });
  }

  recordTest(id, result, testedURL) {
    return this.change(data => {
      const proxy = data.proxies.find(proxy => proxy.id === id);
      if (proxy && this.url(id) === testedURL) proxy.last_test = result;
    });
  }

  recordUsage(proxy, event) {
    // 白名单保存操作元数据，绝不写入 Session、代理密码或访问内容。
    return this.change(data => {
      data.history ||= [];
      data.history.push({ id: randomUUID(), proxy_id: proxy.id, proxy_name: proxy.name, proxy_address: `${proxy.host}:${proxy.port}`, created_at: new Date().toISOString(),
        action: event.action, ok: Boolean(event.ok), environment_id: typeof event.environment_id === 'string' ? event.environment_id.slice(0, 300) : '',
        email: typeof event.email === 'string' ? event.email.slice(0, 254) : '', exit_ip: event.exit_ip || '', matches: event.matches ?? null });
    });
  }

  exportActivity(cursor = 0) {
    const history = this.data.history || [];
    return { device_id: createHash('sha256').update(this.key).digest('hex').slice(0,32), events: history.slice(cursor,cursor+200), next_cursor: Math.min(cursor+200,history.length) };
  }

  history(proxyID, page = 1, pageSize = 20) {
    const rows = (this.data.history || []).filter(row => !proxyID || row.proxy_id === proxyID).slice().reverse();
    return { records: rows.slice((page - 1) * pageSize, page * pageSize), total: rows.length, page, page_size: pageSize };
  }

  recordLogin(environmentID, at) {
    return this.change(data => {
      data.logins ||= {};
      if (!data.logins[environmentID] || Date.parse(at) > Date.parse(data.logins[environmentID])) data.logins[environmentID] = at;
    });
  }
}
