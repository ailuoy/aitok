import { captureBrowserSession } from './session-capture.mjs';

// 本机自动更新任务独立于网页面板，只向后台发送凭据，状态中不包含凭据。
export class SessionRefresh {
  constructor(environment, endpoint, token, onSaved) {
    this.environment = environment;
    this.endpoint = endpoint;
    this.token = token;
    this.onSaved = onSaved;
    this.status = { state: 'waiting', message: '正在等待 ChatGPT 登录，确认后自动更新并关闭浏览器' };
    this.timer = setTimeout(() => this.fail('等待登录超时，请关闭窗口后重新更新 Session'), 5 * 60 * 1000);
    this.timer.unref();
  }

  fail(message) {
    if (this.status.state !== 'waiting') return;
    this.status = { state: 'failed', message };
    this.token = null;
    clearTimeout(this.timer);
  }

  close() {
    clearTimeout(this.timer);
    this.token = null;
    if (['waiting', 'saving'].includes(this.status.state)) this.status = { state: 'cancelled', message: '浏览器已关闭，Session 更新未完成' };
  }

  async run() {
    if (this.status.state !== 'waiting') return;
    clearTimeout(this.timer);
    this.status = { state: 'saving', message: '正在获取并保存最新 Session' };
    try {
      const session = await captureBrowserSession(this.environment);
      if (this.environment.controller.signal.aborted) return;
      const response = await fetch(this.endpoint + '/session', {
        method: 'POST', redirect: 'error',
        headers: { Authorization: 'Bearer ' + this.token, 'Content-Type': 'application/json' },
        body: JSON.stringify({ session_json: JSON.stringify(session) }),
        signal: AbortSignal.any([this.environment.controller.signal, AbortSignal.timeout(15000)]),
      });
      const data = await response.json().catch(() => ({}));
      if (!response.ok) throw new Error(data.error || '保存 Session 失败，请关闭窗口后重试');
      if (!data.session_updated_at || !Number.isFinite(Date.parse(data.session_updated_at))) throw new Error('后台未返回更新时间，请更新服务端并刷新账号列表确认');
      this.status = { state: 'saved', session_updated_at: data.session_updated_at, message: 'Session 已更新，正在自动关闭浏览器' };
      this.token = null;
      await this.onSaved();
    } catch (error) {
      if (this.status.state === 'cancelled') return;
      if (this.status.state === 'saved') {
        this.status.message = 'Session 已更新，但浏览器关闭失败，请手动关闭';
        return;
      }
      this.status = { state: 'failed', message: /^[\u3400-\u9fff]/.test(error.message) ? error.message : '更新 Session 失败，请检查网络并重试；未确认保存成功' };
      this.token = null;
    }
  }
}
