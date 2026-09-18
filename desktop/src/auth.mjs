import http from 'node:http';
import { randomBytes } from 'node:crypto';
import { mkdir, readFile, writeFile, rename } from 'node:fs/promises';
import { dirname } from 'node:path';

export class DesktopAuth {
  constructor({ profile, path, encryption, openExternal, onChange = () => {}, request = fetch }) {
    Object.assign(this, { profile, path, encryption, openExternal, onChange, request });
    this.token = ''; this.user = null; this.expires = 0; this.status = 'signed_out'; this.error = ''; this.pending = null; this.generation = 0; this.lastCheck = 0; this.retryAfter = 0; this.failures = 0; this.writes = Promise.resolve();
  }
  snapshot() { return { status: this.status, user: this.user, expires_at: this.expires, error: this.error }; }
  async persist() {
    if (this.token && !this.encryption.isEncryptionAvailable()) throw new Error('系统安全存储不可用，无法保存登录状态');
    const content = JSON.stringify({ version: 1, encrypted: this.token ? this.encryption.encryptString(this.token).toString('base64') : '' });
    const write = this.writes.catch(() => {}).then(async () => {
      await mkdir(dirname(this.path), { recursive: true, mode: 0o700 });
      await writeFile(this.path + '.tmp', content, { mode: 0o600 });
      await rename(this.path + '.tmp', this.path);
    });
    this.writes = write; return write;
  }
  async restore() {
    try {
      const saved = JSON.parse(await readFile(this.path, 'utf8'));
      if (!saved.encrypted) return;
      if (!this.encryption.isEncryptionAvailable()) throw new Error('系统安全存储不可用，请重新授权登录');
      this.token = this.encryption.decryptString(Buffer.from(saved.encrypted, 'base64'));
      await this.check(true);
    } catch (error) {
      if (error.code !== 'ENOENT' && this.status !== 'reconnecting') { this.status = 'signed_out'; this.error = '无法恢复登录，请重新授权登录'; }
    }
  }
  async inspect(token) {
    const response = await this.request(this.profile.origin + '/api/desktop-auth/session', { headers: { Authorization: 'Bearer ' + token }, credentials: 'omit', cache: 'no-store', redirect: 'error', signal: AbortSignal.timeout(15000) });
    if (!response.ok) throw Object.assign(new Error(response.status === 401 || response.status === 403 ? '登录已失效，请重新授权登录' : '后台暂时不可用，请稍后重试'), { invalid: response.status === 401 || response.status === 403, status: response.status });
    const data = await response.json();
    if (data.channel !== this.profile.channel || data.origin !== this.profile.origin || !Number.isSafeInteger(data.user?.id) || data.user.id < 1 || !['admin', 'super_admin'].includes(data.user.role) || !Number.isSafeInteger(data.expires_at) || data.expires_at * 1000 <= Date.now()) throw Object.assign(new Error('登录授权与当前环境不匹配，请重新登录'), { invalid: true });
    return data;
  }
  async check(force = false) {
    if (!this.token) throw new Error('请先在助手中授权登录');
    if (!force && this.status === 'authenticated' && Date.now() - this.lastCheck < 30000 && this.expires * 1000 > Date.now()) return this.user;
    if (!force && this.status === 'reconnecting' && Date.now() < this.retryAfter && (!this.expires || this.expires * 1000 > Date.now())) throw new Error(this.error);
    if (this.checking) return this.checking;
    const token = this.token, generation = this.generation;
    this.checking = (async () => {
      try {
        const data = await (this.expires && this.expires * 1000 <= Date.now()
          ? Promise.reject(Object.assign(new Error('登录已过期，请重新授权登录'), { invalid: true }))
          : this.inspect(token));
        if (generation !== this.generation) throw new Error('登录状态已变更');
        const recovered = this.status !== 'authenticated';
        this.user = data.user; this.expires = data.expires_at; this.status = 'authenticated'; this.error = ''; this.lastCheck = Date.now(); this.retryAfter = 0; this.failures = 0;
        if (recovered) this.onChange();
        return this.user;
      } catch (error) {
        if (generation === this.generation) {
          if (error.invalid) {
            this.token = ''; this.user = null; this.expires = 0; this.status = 'signed_out'; this.error = error.message; this.retryAfter = 0; this.failures = 0;
            await this.persist();
          } else {
            // 暂时无法校验时拒绝新操作，但保留凭证和已有浏览器，等待连接恢复。
            const delay = Math.min(3000 * 2 ** Math.min(this.failures++, 4), 30000);
            const reason = error.status ? `后台返回 HTTP ${error.status}` : error.name === 'TimeoutError' || /TIMEDOUT|TIMEOUT/.test(error.cause?.code || '') ? '连接超时' : error instanceof SyntaxError ? '后台响应格式异常' : '网络连接失败';
            this.status = 'reconnecting'; this.error = `${reason}，正在自动重试；已打开的浏览器会保留，暂不能打开新账号。`; this.retryAfter = Date.now() + delay;
          }
          this.onChange();
        }
        throw error;
      } finally { this.checking = null; }
    })();
    return this.checking;
  }
  cancel() {
    this.generation++;
    if (this.pending) {
      clearTimeout(this.pending.timer); this.pending.server.close(); this.pending.server.closeAllConnections(); this.pending = null;
      this.token = ''; this.user = null; this.expires = 0;
      void this.persist().catch(() => {});
    }
    if (this.status === 'pending') this.status = 'signed_out';
  }
  async login() {
    this.cancel();
    if (this.token && (this.status === 'authenticated' || this.status === 'reconnecting')) throw new Error('请先退出当前账号');
    if (this.token) { this.token = ''; await this.persist(); }
    const generation = this.generation, state = randomBytes(32).toString('base64url');
    const server = http.createServer(async (req, res) => {
      const send = (code, data) => { res.writeHead(code, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' }); res.end(JSON.stringify(data)); };
      if (req.headers.host !== `127.0.0.1:${server.address()?.port}` || req.headers.origin !== this.profile.origin || req.url !== '/callback/' + state || this.pending?.state !== state) { send(403, { error: '授权来源无效' }); return; }
      res.setHeader('Access-Control-Allow-Origin', this.profile.origin); res.setHeader('Vary', 'Origin');
      res.setHeader('Access-Control-Allow-Methods', 'POST, OPTIONS'); res.setHeader('Access-Control-Allow-Headers', 'Content-Type'); res.setHeader('Access-Control-Allow-Private-Network', 'true');
      if (req.method === 'OPTIONS') { res.writeHead(204); res.end(); return; }
      if (req.method !== 'POST' || !req.headers['content-type']?.startsWith('application/json')) { send(405, { error: '授权请求无效' }); return; }
      if (this.pending.processing) { send(409, { error: '正在处理授权' }); return; }
      this.pending.processing = true;
      try {
        let raw = '', size = 0;
        for await (const chunk of req) { size += chunk.length; if (size > 8192) throw new Error('授权数据过大'); raw += chunk; }
        const { token } = JSON.parse(raw);
        if (typeof token !== 'string' || token.length > 4096) throw new Error('授权数据无效');
        const data = await this.inspect(token);
        if (data.state !== state || generation !== this.generation) throw new Error('授权请求已失效');
        this.token = token; this.user = data.user; this.expires = data.expires_at;
        try { await this.persist(); } catch (error) { this.token = ''; this.user = null; throw error; }
        if (generation !== this.generation) throw new Error('授权请求已取消');
        this.status = 'authenticated'; this.error = ''; this.lastCheck = Date.now(); this.retryAfter = 0; this.failures = 0;
        clearTimeout(this.pending.timer); this.pending = null;
        send(200, { ok: true }); server.close(); this.onChange();
      } catch {
        if (this.pending?.state === state) this.pending.processing = false;
        send(400, { error: '授权失败，请回到助手重新登录' });
      }
    });
    server.requestTimeout = 20000;
    await new Promise((resolve, reject) => { server.once('error', reject); server.listen(0, '127.0.0.1', resolve); });
    const timer = setTimeout(() => { if (this.pending?.state === state) { this.cancel(); this.error = '授权已超时，请重新点击登录'; this.onChange(); } }, 5 * 60 * 1000);
    timer.unref();
    this.pending = { server, state, timer }; this.status = 'pending'; this.error = '';
    const url = new URL('/desktop/authorize', this.profile.origin);
    url.search = new URLSearchParams({ state, channel: this.profile.channel, callback: `http://127.0.0.1:${server.address().port}/callback/${state}` });
    try { await this.openExternal(url.href); } catch (error) { this.cancel(); throw error; }
    return this.snapshot();
  }
  async logout() { this.cancel(); this.token = ''; this.user = null; this.expires = 0; this.error = ''; this.status = 'signed_out'; await this.persist(); this.onChange(); }
  async close() { this.cancel(); await this.writes; }
}
