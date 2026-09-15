import { assistantPanelSource } from './assistant-panel.mjs';
import { assistantPage, fillSource } from './checkout-fill.mjs';

const world = 'aitok-assistant';
export class BrowserAssistant {
  constructor(environment, endpoint, token) {
    this.environment = environment; this.endpoint = endpoint; this.token = token; this.pages = new Map(); this.busy = new Set();
    this.listener = message => { if (message.method === 'Runtime.bindingCalled' && message.params.name === 'aitokAssistant') void this.handle(message); };
    environment.cdp.on('message', this.listener);
  }
  close() { this.environment.cdp.off('message', this.listener); this.token = null; this.data = null; this.pages.clear(); }
  status() { const env = this.environment; return { state: env.state, email: env.actualEmail || env.expectedEmail || env.session?.user?.email, plan: ({ free: 'Free', plus: 'Plus', pro: 'Pro', pro_20x: 'Pro 20x', pro_5x: 'Pro 5x', team: 'Team', business: 'Business' })[env.plan] || env.plan || null }; }
  async request(path = '') {
    if (!this.token) throw new Error('请从后台重新打开账号以启用助手');
    const response = await fetch(this.endpoint + path, { headers: { Authorization: 'Bearer ' + this.token }, redirect: 'error', signal: AbortSignal.timeout(15000) });
    if (!response.ok) throw new Error(response.status === 401 ? '助手授权已过期，请重新打开账号' : '无法读取银行卡或地址，请回后台检查');
    return response.json();
  }
  async sync(targetInfos) {
    const live = new Set(targetInfos.map(target => target.targetId));
    for (const id of this.pages.keys()) if (!live.has(id)) this.pages.delete(id);
    for (const target of targetInfos) {
      if (target.type !== 'page' || !assistantPage(target.url) || this.pages.has(target.targetId)) continue;
      let attachedSession;
      try {
        const { cdp } = this.environment;
        const { sessionId } = await cdp.send('Target.attachToTarget', { targetId: target.targetId, flatten: true });
        attachedSession = sessionId;
        await cdp.send('Page.enable', {}, sessionId); await cdp.send('Runtime.enable', {}, sessionId);
        await cdp.send('Runtime.addBinding', { name: 'aitokAssistant', executionContextName: world }, sessionId);
        await cdp.send('Page.addScriptToEvaluateOnNewDocument', { source: assistantPanelSource(), worldName: world }, sessionId);
        const { frameTree } = await cdp.send('Page.getFrameTree', {}, sessionId);
        const { executionContextId } = await cdp.send('Page.createIsolatedWorld', { frameId: frameTree.frame.id, worldName: world }, sessionId);
        this.pages.set(target.targetId, sessionId);
        await cdp.send('Runtime.evaluate', { expression: assistantPanelSource(), contextId: executionContextId }, sessionId);
      } catch {
        this.pages.delete(target.targetId);
        if (attachedSession) await this.environment.cdp.send('Target.detachFromTarget', { sessionId: attachedSession }).catch(() => {});
        /* 导航期间稍后重试，不影响账号浏览器。 */
      }
    }
  }
  async handle(message) {
    const { cdp } = this.environment;
    const sessionId = message.sessionId, contextId = message.params.executionContextId;
    if (![...this.pages.values()].includes(sessionId) || message.params.payload.length > 2048) return;
    let input;
    try { input = JSON.parse(message.params.payload); } catch { return; }
    if (!Number.isSafeInteger(input.id) || !['load', 'status', 'card', 'fill', 'plan'].includes(input.action)) return;
    let result;
    try {
      const location = await cdp.send('Runtime.evaluate', { expression: 'window === window.top ? location.href : ""', contextId, returnByValue: true }, sessionId);
      if (!assistantPage(location.result?.value)) return;
      if (input.action === 'status') result = this.status();
      else if (input.action === 'load') { this.data = await this.request(); result = { ...this.data, status: this.status() }; }
      else if (input.action === 'card') {
        if (!Number.isSafeInteger(input.card_id) || input.card_id < 1) throw new Error('请选择有效银行卡');
        result = await this.request('/cards/' + input.card_id);
      }
      else if (input.action === 'plan') {
        if (!['Plus', 'Pro 20x', 'Pro 5x'].includes(input.plan)) throw new Error('套餐无效');
        await cdp.send('Target.createTarget', { url: 'https://chatgpt.com/#pricing' });
        result = { message: '已打开官网套餐页，请选择 ' + input.plan + '；以官网可购买的方案为准。' };
      } else {
        if (this.busy.has(sessionId)) throw new Error('正在填充，请稍后');
        this.busy.add(sessionId);
        try { result = await this.fill(sessionId, input); } finally { this.busy.delete(sessionId); }
      }
    } catch (error) { result = { error: /^[\u3400-\u9fff]/.test(error.message) ? error.message : '助手操作失败，请刷新后重试' }; }
    try { await cdp.send('Runtime.evaluate', { expression: `window.__aitokAssistantReply?.(${input.id},${JSON.stringify(result)})`, contextId }, sessionId); } catch { /* 页面已导航。 */ }
  }
  async fill(sessionId, input) {
    if (!Number.isSafeInteger(input.card_id) || !Number.isSafeInteger(input.address_id)) throw new Error('请先选择银行卡和账单地址');
    if (typeof input.cvc !== 'string' || !/^\d{3,4}$/.test(input.cvc)) throw new Error('请输入 3 或 4 位安全码');
    const { cdp } = this.environment;
    const { frameTree } = await cdp.send('Page.getFrameTree', {}, sessionId);
    if (!assistantPage(frameTree.frame.url)) throw new Error('请在官方收银页面填充');
    const data = await this.request();
    const address = data.addresses.find(address => address.id === input.address_id);
    if (!address || !data.cards.some(card => card.id === input.card_id)) throw new Error('所选银行卡或地址已删除，请刷新');
    const { card } = await this.request('/cards/' + input.card_id);
    const now = new Date();
    if (card.exp_year < now.getUTCFullYear() || (card.exp_year === now.getUTCFullYear() && card.exp_month < now.getUTCMonth() + 1)) throw new Error('银行卡已过期，请更新有效期');
    const fields = new Set(); let inaccessible = 0;
    const visit = async tree => {
      const frame = tree.frame;
      const origin = (() => { try { return new URL(frame.url).origin; } catch { return ''; } })();
      if (assistantPage(frame.url) || ['https://js.stripe.com', 'https://hooks.stripe.com'].includes(origin)) {
        try {
          let targetSession = sessionId, context;
          try { context = await cdp.send('Page.createIsolatedWorld', { frameId: frame.id, worldName: world }, targetSession); }
          catch { const attached = await cdp.send('Target.attachToTarget', { targetId: frame.id, flatten: true }); targetSession = attached.sessionId; context = await cdp.send('Page.createIsolatedWorld', { frameId: frame.id, worldName: world }, targetSession); }
          const result = await cdp.send('Runtime.evaluate', { expression: fillSource(card, address, input.cvc), contextId: context.executionContextId, returnByValue: true }, targetSession);
          for (const field of result.result?.value || []) fields.add(field);
        } catch { inaccessible++; }
      }
      for (const child of tree.childFrames || []) await visit(child);
    };
    await visit(frameTree);
    return { message: fields.size ? `已填充 ${fields.size} 类字段，请核对收银表单后手动付款。${inaccessible ? '部分嵌入字段无法访问，请手动补充。' : ''}` : '当前页面未找到可填充的收银字段，请先打开官方付款页面。' };
  }
}
