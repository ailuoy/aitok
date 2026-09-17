import { fingerprintSource } from './fingerprint-script.mjs';
import { mkdir, writeFile, rename } from 'node:fs/promises';
import { join } from 'node:path';

const pageTypes = new Set(['page', 'iframe']);
const workerTypes = new Set(['worker', 'shared_worker', 'service_worker']);
const autoAttach = { autoAttach: true, waitForDebuggerOnStart: true, flatten: true,
  filter: [...pageTypes, ...workerTypes].map(type => ({ type, exclude: false })).concat({ exclude: true }) };

export async function prepareFingerprintExtension(directory, fingerprint) {
  const path = join(directory, 'aitok-fingerprint-extension');
  await mkdir(path, { recursive: true, mode: 0o700 });
  const manifest = { manifest_version: 3, name: 'AiTok 账号指纹', version: '1.0.0',
    content_scripts: [{ matches: ['<all_urls>'], js: ['fingerprint.js'], run_at: 'document_start', all_frames: true, match_about_blank: true, match_origin_as_fallback: true, world: 'MAIN' }] };
  for (const [name, content] of [['manifest.json', JSON.stringify(manifest)], ['fingerprint.js', fingerprintSource(fingerprint.seed)]]) {
    const file = join(path, name);
    await writeFile(file + '.tmp', content, { mode: 0o600 });
    await rename(file + '.tmp', file);
  }
  return path;
}

// 新目标只在初始化指纹期间暂停；任何异常都在 finally 中恢复，避免卡住标签与 Worker。
export class FingerprintRuntime {
  constructor(cdp, fingerprint, extensionPath) {
    this.cdp = cdp; this.source = fingerprintSource(fingerprint.seed); this.pending = new Set(); this.sessions = new Map(); this.closed = false;
    this.targets = new Map();
    this.workerBreakpoints = new Map();
    this.extensionPath = extensionPath;
    this.errors = 0;
    this.listener = message => {
      if (message.method === 'Target.attachedToTarget') {
        const { targetInfo, sessionId } = message.params;
        const existing = this.targets.get(targetInfo.targetId);
        if (existing) {
          if (message.params.waitingForDebugger) void existing.task.then(() => cdp.send('Runtime.runIfWaitingForDebugger', {}, sessionId)).catch(() => {});
          return;
        }
        const task = this.configure(message.params).catch(() => { if (!this.closed) this.errors++; }).finally(() => this.pending.delete(task));
        this.pending.add(task);
        this.targets.set(targetInfo.targetId, { sessionId, task });
      } else if (message.method === 'Debugger.paused' && this.workerBreakpoints.has(message.sessionId)) {
        const task = this.initializeWorker(message.sessionId, message.params).catch(() => { if (!this.closed) this.errors++; }).finally(() => this.pending.delete(task));
        this.pending.add(task);
      } else if (message.method === 'Target.detachedFromTarget') {
        const id = this.sessions.get(message.params.sessionId);
        this.sessions.delete(message.params.sessionId);
        this.workerBreakpoints.delete(message.params.sessionId);
        if (id) this.targets.delete(id);
      }
    };
    cdp.on('message', this.listener);
  }

  async pageSession(targetId) {
    let target = this.targets.get(targetId);
    if (!target) {
      const attached = await this.cdp.send('Target.attachToTarget', { targetId, flatten: true });
      target = this.targets.get(targetId);
      if (!target) return attached.sessionId;
    }
    await target.task;
    return target.sessionId;
  }

  async start() {
    // 原生 document_start 保证页面首个脚本、同源与跨源 iframe 均已加载指纹。
    // 扩展仅加载在该账号资料中，调试接口仍使用父进程私有管道。
    try { await this.cdp.send('Extensions.loadUnpacked', { path: this.extensionPath }); }
    catch { throw new Error('浏览器不支持账号指纹扩展，请升级本机 Chrome / Edge 后重试'); }
    await this.cdp.send('Target.setAutoAttach', autoAttach);
    while (this.pending.size) await Promise.all([...this.pending]);
    if (this.errors) throw new Error('浏览器指纹初始化失败，请检查浏览器版本后重试');
  }

  async configure({ sessionId, targetInfo, waitingForDebugger }) {
    if (this.sessions.has(sessionId)) return;
    this.sessions.set(sessionId, targetInfo.targetId);
    try {
      if (this.closed) return;
      if (workerTypes.has(targetInfo.type) && /^(?:chrome|chrome-extension|devtools):/.test(targetInfo.url)) return;
      if (workerTypes.has(targetInfo.type)) {
        await this.cdp.send('Runtime.enable', {}, sessionId);
        if (waitingForDebugger) {
          // Worker 在启动暂停时尚无可执行上下文，等待首个脚本创建后再注入。
          await this.cdp.send('Debugger.enable', {}, sessionId);
          const { breakpointId } = await this.cdp.send('Debugger.setInstrumentationBreakpoint', { instrumentation: 'beforeScriptExecution' }, sessionId);
          this.workerBreakpoints.set(sessionId, breakpointId);
        } else {
          const result = await this.cdp.send('Runtime.evaluate', { expression: this.source }, sessionId);
          if (result.exceptionDetails) throw new Error('Worker 指纹初始化失败');
        }
      }
      // 子目标单独接管，覆盖跨站 iframe 及页面创建的 Worker。
      await this.cdp.send('Target.setAutoAttach', autoAttach, sessionId);
    } finally {
      if (waitingForDebugger) await this.cdp.send('Runtime.runIfWaitingForDebugger', {}, sessionId).catch(() => {});
    }
  }

  async initializeWorker(sessionId, paused) {
    const breakpointId = this.workerBreakpoints.get(sessionId);
    this.workerBreakpoints.delete(sessionId);
    try {
      await this.cdp.send('Debugger.removeBreakpoint', { breakpointId }, sessionId);
      const callFrameId = paused.callFrames?.[0]?.callFrameId;
      const result = await this.cdp.send(callFrameId ? 'Debugger.evaluateOnCallFrame' : 'Runtime.evaluate', { expression: this.source, ...(callFrameId ? { callFrameId } : {}) }, sessionId);
      if (result.exceptionDetails) throw new Error('Worker 指纹初始化失败');
    } finally {
      // 初始化后移除调试器，不能让 Worker 因指纹错误一直暂停。
      await this.cdp.send('Debugger.disable', {}, sessionId).catch(() => this.cdp.send('Debugger.resume', {}, sessionId).catch(() => {}));
    }
  }

  close() { this.closed = true; this.cdp.off('message', this.listener); }
}
