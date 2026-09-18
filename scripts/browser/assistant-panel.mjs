import { isVerificationPage } from './page-verification.mjs';

export const ASSISTANT_VERSION = '0.0.3';
export function assistantPanelSource() { return `(${installPanel.toString()})(${JSON.stringify(ASSISTANT_VERSION)}, ${isVerificationPage.toString()})`; }

function installPanel(version, isVerificationPage) {
  if (window !== window.top || location.protocol !== 'https:' || !['chatgpt.com', 'checkout.stripe.com', 'pay.openai.com', 'pay.chatgpt.com'].includes(location.hostname) || window.__aitokPanelInstalled) return;
  window.__aitokPanelInstalled = true;
  let sequence = 0;
  const pending = new Map();
  const call = (action, values = {}) => new Promise((resolve, reject) => {
    const id = ++sequence;
    const timer = setTimeout(() => { pending.delete(id); reject(new Error('助手请求超时，请重试')); }, 25000);
    pending.set(id, { resolve, reject, timer });
    window.aitokAssistant(JSON.stringify({ id, action, ...values }));
  });
  window.__aitokAssistantReply = (id, result) => { const request = pending.get(id); if (!request) return; pending.delete(id); clearTimeout(request.timer); result.error ? request.reject(new Error(result.error)) : request.resolve(result); };
  const mount = () => {
    if (!document.body || document.readyState === 'loading') return;
    const existing = document.getElementById('aitok-assistant');
    if (isVerificationPage()) { existing?.remove(); return; }
    if (existing) return;
    const host = document.createElement('aside'); host.id = 'aitok-assistant'; host.setAttribute('aria-label', 'AiTok 账号助手');
    host.style.cssText = 'all:initial!important;position:fixed!important;right:12px!important;top:88px!important;z-index:2147483646!important;';
    const root = host.attachShadow({ mode: 'closed' });
    const style = document.createElement('style');
    style.textContent = `:host{font:13px/1.5 system-ui;color:#18382e}*{box-sizing:border-box}.panel{width:320px;max-width:calc(100vw - 24px);max-height:calc(100dvh - 24px);overflow:auto;background:#fafffc;border:1px solid #b9dacc;border-radius:14px;box-shadow:0 12px 40px #143c2826;padding:16px;color:#18382e;font:13px/1.5 system-ui}h2{font-size:17px;margin:0}.version{font-size:11px;font-weight:500}.copy-email{font-size:11px;padding:3px 7px;margin-top:6px}.selected-details{margin:6px 0 10px;padding:9px 10px;border:1px solid #b9dacc;border-radius:8px}.selected-details>div{display:grid;grid-template-columns:64px minmax(0,1fr);gap:6px;padding:3px 0}.selected-details dt{font-size:11px;color:#5f756a}.selected-details dd{display:grid;grid-template-columns:minmax(0,1fr) auto;align-items:start;gap:5px;font-size:12px;margin:0;overflow-wrap:anywhere;user-select:text}.field-value{white-space:pre-wrap}.copy-field{font-size:10px;line-height:1.4;padding:2px 4px;border-radius:4px;white-space:nowrap}.copy-field:disabled{cursor:default}.plans{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:6px}.plans button{padding:6px 8px}.selected-details p{margin:0}h3{font-size:13px;border-left:3px solid #19a66a;padding-left:8px;margin:18px 0 10px}.head{display:flex;align-items:center;justify-content:space-between;gap:8px}.info{padding:12px;background:#eff6f2;border-radius:8px;margin-top:12px;overflow-wrap:anywhere}.info strong,.info span{display:block}small,.hint{color:#5f756a;font-size:11px}.hint{margin-top:8px}button{font:inherit;border:1px solid #bedbcd;border-radius:7px;padding:9px 10px;cursor:pointer;color:#176343;background:#e5f2eb}button:hover{background:#d6eddf}button:focus-visible,input:focus-visible,summary:focus-visible{outline:2px solid #16925c;outline-offset:2px}button:disabled{opacity:.5;cursor:wait}.head button{padding:4px 8px}.stack{display:grid;gap:7px}.primary{background:#159c60;color:white;border-color:#159c60}.primary:hover{background:#138955}input{width:100%;min-width:0;box-sizing:border-box;font:inherit;padding:9px;border:1px solid #bdd6c8;border-radius:6px;background:white;color:#18382e}label{display:grid;gap:6px;margin-top:10px}label[hidden]{display:none}details{position:relative;margin:8px 0}summary{padding:9px;border:1px solid #bdd6c8;border-radius:7px;cursor:pointer;overflow-wrap:anywhere;list-style:none;background:#f0f7f3}summary::after{content:' ▾';float:right}.options{max-height:180px;overflow:auto;display:grid;gap:4px;margin-top:5px}.options button{text-align:left}.picker{padding:7px;border:1px solid #bdd6c8;border-radius:8px;background:#fff}.message{font-size:12px;margin-top:10px;white-space:pre-wrap;overflow-wrap:anywhere}.collapsed{display:none}.tab{display:none;background:#159c60;color:#fff;cursor:grab;touch-action:none;user-select:none}.tab:active{cursor:grabbing}:host(.compact) .panel{display:none}:host(.compact) .tab{display:block}a{color:#167c50;text-decoration:underline}@media(prefers-color-scheme:dark){.panel{background:#14251e;color:#e5f3eb;border-color:#365348}.info,summary{background:#20392d;color:#e5f3eb}.picker,input{background:#182e23;color:#e5f3eb}button{background:#274f3a;color:#dcf1e4;border-color:#436450}.primary,.tab{background:#159c60;color:#fff}small,.hint,.selected-details dt{color:#a8c8b5}.selected-details{border-color:#365348}}`;
    root.append(style);
    const create = (tag, text, className, parent) => { const element = document.createElement(tag); if (text) element.textContent = text; if (className) element.className = className; parent?.append(element); return element; };
    const panel = create('section', '', 'panel', root);
    const head = create('div', '', 'head', panel); const heading = create('h2', '账号助手 ', '', head); create('small', version, 'version', heading);
    const collapse = create('button', '收起', '', head); const tab = create('button', '账号助手', 'tab', root);
    let right = 12, top = 88, drag = null, suppressClick = false;
    function position() {
      const viewportWidth = document.documentElement.clientWidth, viewportHeight = window.innerHeight;
      const rect = host.getBoundingClientRect();
      right = Math.max(12, Math.min(right, viewportWidth - rect.width - 12));
      top = Math.max(12, Math.min(top, viewportHeight - rect.height - 12));
      host.style.setProperty('right', right + 'px', 'important');
      host.style.setProperty('top', top + 'px', 'important');
    }
    collapse.onclick = () => { host.classList.add('compact'); position(); tab.focus(); };
    tab.title = '点击展开；拖拽或使用方向键移动';
    tab.onclick = event => {
      if (suppressClick && event.detail !== 0) { suppressClick = false; return; }
      host.classList.remove('compact'); position(); collapse.focus();
    };
    tab.onpointerdown = event => {
      if (!event.isPrimary || event.button !== 0) return;
      suppressClick = false;
      drag = { id: event.pointerId, x: event.clientX, y: event.clientY, right, top };
      tab.setPointerCapture(event.pointerId);
    };
    tab.onpointermove = event => {
      if (!drag || drag.id !== event.pointerId) return;
      const dx = event.clientX - drag.x, dy = event.clientY - drag.y;
      if (!suppressClick && Math.hypot(dx, dy) < 5) return;
      suppressClick = true;
      right = drag.right - dx; top = drag.top + dy; position();
    };
    const endDrag = () => { drag = null; };
    tab.onpointerup = endDrag; tab.onpointercancel = endDrag; tab.onlostpointercapture = endDrag;
    tab.onkeydown = event => {
      const offsets = { ArrowLeft: [10, 0], ArrowRight: [-10, 0], ArrowUp: [0, -10], ArrowDown: [0, 10] };
      if (!offsets[event.key]) return;
      event.preventDefault(); right += offsets[event.key][0]; top += offsets[event.key][1]; position();
    };
    const resizeObserver = new ResizeObserver(position);
    resizeObserver.observe(host);
    window.addEventListener('resize', position);
    const info = create('div', '', 'info', panel); const login = create('small', '正在确认登录…', '', info); const email = create('strong', '—', '', info); const plan = create('span', '当前套餐：读取中', '', info);
    async function copyValue(name, value) {
      try { await navigator.clipboard.writeText(String(value)); message.textContent = name + '已复制'; }
      catch { message.textContent = '无法写入剪贴板，请手动复制' + name; }
    }
    const copy = create('button', '复制邮箱', 'copy-email', info); copy.onclick = event => { if (event.isTrusted) void copyValue('邮箱', email.textContent); };
    create('h3', '官方套餐', '', panel); const plans = create('div', '', 'plans', panel);
    for (const [text, name] of [['Plus', 'Plus'], ['5X', 'Pro 5x'], ['20X', 'Pro 20x']]) { const button = create('button', text, '', plans); button.onclick = event => { if (event.isTrusted) run(button, () => call('plan', { plan: name })); }; }
    create('p', '在官网选择对应方案，价格及额度以官网为准。', 'hint', panel);
    create('h3', '当前收银页填充', '', panel);
    let data = { cards: [], addresses: [] }, cardID = '', addressID = '', cardRequest = 0, selectedCard = null, codeField = null;
    const picker = (label, empty, onSelect) => {
      const details = create('details', '', '', panel); const summary = create('summary', empty, '', details);
      const box = create('div', '', 'picker', details); const input = create('input', '', '', box); input.placeholder = '过滤' + label; input.setAttribute('aria-label', '过滤' + label);
      const options = create('div', '', 'options', box); let entries = [];
      const render = () => { options.replaceChildren(); for (const entry of entries.filter(entry => entry.label.toLowerCase().includes(input.value.toLowerCase()))) { const button = create('button', entry.label, '', options); button.onclick = () => { onSelect(entry.id); summary.textContent = entry.label; details.open = false; }; } if (!options.children.length) create('small', '没有匹配项', '', options); };
      input.oninput = render;
      return { set(entriesValue, selected) { entries = entriesValue; summary.textContent = entries.find(entry => String(entry.id) === String(selected))?.label || empty; render(); } };
    };
    const cards = picker('银行卡', '请选择银行卡', id => { cardID = id; cvc.value = ''; void showCard(); });
    const cardDetails = create('dl', '', 'selected-details card-details', panel);
    const label = create('label', '安全码（未保存时可临时填写）', '', panel); const cvc = create('input', '', '', label); cvc.type = 'text'; cvc.inputMode = 'numeric'; cvc.maxLength = 4; cvc.autocomplete = 'off'; cvc.placeholder = '输入所选银行卡的安全码';
    cvc.oninput = updateCode;
    const addresses = picker('账单地址', '请选择账单地址', id => { addressID = id; showAddress(); });
    const addressDetails = create('dl', '', 'selected-details address-details', panel);
    function showFields(container, fields) {
      container.replaceChildren();
      for (const [name, value] of fields) addField(container, name, value);
    }
    function addField(container, name, value) {
      const row = create('div', '', '', container); const title = create('dt', name, '', row);
      const detail = create('dd', '', '', row); const content = create('span', '', 'field-value', detail);
      const button = create('button', '复制', 'copy-field', detail);
      const update = (next, titleText = name) => {
        const text = next == null ? '' : String(next);
        title.textContent = titleText; content.textContent = text || '未填写';
        button.disabled = !text; button.setAttribute('aria-label', '复制' + titleText);
        button.onclick = event => { if (event.isTrusted && text) void copyValue(titleText, text); };
      };
      update(value); return update;
    }
    function testCode(card) {
      const samples = {
        '4242424242424242': ['演示 Visa（测试卡）', '123'],
        '5555555555554444': ['演示 Mastercard（测试卡）', '123'],
        '378282246310005': ['演示 Amex（测试卡）', '1234'],
      };
      const sample = samples[card?.number];
      return sample && card.label === sample[0] && card.cardholder === 'TEST USER' ? sample[1] : '';
    }
    function updateCode() {
      const sample = testCode(selectedCard);
      codeField?.(cvc.value || selectedCard?.cvc || sample, sample && !cvc.value && !selectedCard?.cvc ? '测试安全码' : '安全码');
    }
    async function showCard() {
      const request = ++cardRequest, selected = cardID;
      selectedCard = null; codeField = null; cvc.value = ''; label.hidden = true; updateCopy();
      cardDetails.replaceChildren();
      if (!selected) { create('p', data.payment_card_id ? '账号绑定的付款卡不可用，请在后台检查绑定，或手动选择其他卡片。' : '暂无银行卡，请先在后台添加。', 'hint', cardDetails); return; }
      create('p', '正在读取银行卡…', 'hint', cardDetails);
      try {
        const { card } = await call('card', { card_id: selected });
        if (request !== cardRequest || selected !== cardID) return;
        selectedCard = card; label.hidden = Boolean(card.cvc);
        showFields(cardDetails, [['名称', card.label], ['卡平台', card.platform], ['卡类型', card.brand], ['持卡人', card.cardholder], ['完整卡号', card.number], ['有效期', String(card.exp_month).padStart(2, '0') + '/' + card.exp_year], ['备注', card.notes]]);
        codeField = addField(cardDetails, '安全码', ''); updateCode(); updateCopy();
      } catch (error) { if (request === cardRequest) { cardDetails.replaceChildren(); create('p', error.message, 'hint', cardDetails); } }
    }
    function addressFields(address) {
      return [['账单姓名', address.full_name], ['街道地址', address.address_line1], ['公寓 / 房间', address.address_line2], ['城市', address.city], ['州 / 省', address.source_data?.State_Full ? address.source_data.State_Full + ' (' + address.state + ')' : address.state], ['邮编', address.postal_code], ['国家', address.country], ...(address.source_data?.Telephone ? [['电话', address.source_data.Telephone]] : [])];
    }
    function showAddress() {
      const address = data.addresses.find(item => item.id === addressID);
      updateCopy();
      if (!address) { addressDetails.replaceChildren(); if (data.billing_address_id) create('p', '账号绑定的地址不可用，请在后台检查绑定，或手动选择其他地址。', 'hint', addressDetails); return; }
      showFields(addressDetails, addressFields(address));
    }
    const actions = create('div', '', 'stack', panel); actions.style.marginTop = '12px';
    const copyPayment = create('button', '复制卡号和地址', 'primary', actions);
    copyPayment.disabled = true;
    function updateCopy() { copyPayment.disabled = !selectedCard || !data.addresses.some(item => item.id === addressID); }
    copyPayment.onclick = event => {
      if (!event.isTrusted || !selectedCard) return;
      const address = data.addresses.find(item => item.id === addressID);
      if (!address) return;
      const fields = [
        ['持卡人', selectedCard.cardholder], ['卡号', selectedCard.number],
        ['有效期', String(selectedCard.exp_month).padStart(2, '0') + '/' + selectedCard.exp_year],
        ['安全码', cvc.value || selectedCard.cvc || testCode(selectedCard)],
      ];

      const format = values => values.filter(([, value]) => value != null && String(value).trim()).map(([name, value]) => name + '：' + value).join('\n');
      void copyValue('卡号和地址', '银行卡\n' + format(fields) + '\n\n账单地址\n' + format(addressFields(address)));
    };
    const fill = create('button', '填充全部表单', '', actions);
    fill.onclick = event => { if (!event.isTrusted) return; const value = cvc.value || selectedCard?.cvc || testCode(selectedCard); cvc.value = ''; updateCode(); run(fill, () => call('fill', { card_id: cardID, address_id: addressID, cvc: value })); };
    const nextAddress = create('button', '切换账单地址', '', actions); nextAddress.onclick = () => { const index = data.addresses.findIndex(item => item.id === addressID); addressID = data.addresses[(index + 1) % data.addresses.length]?.id || ''; updatePickers(); showAddress(); };
    const nextCard = create('button', '切换下一张卡', '', actions); nextCard.onclick = () => { const index = data.cards.findIndex(item => item.id === cardID); cardID = data.cards[(index + 1) % data.cards.length]?.id || ''; cvc.value = ''; updatePickers(); void showCard(); };
    const refresh = create('button', '刷新银行卡和地址', '', actions);
    create('p', '使用与你付款信息一致的账单地址。填充后请核对，付款由你手动提交。', 'hint', panel);
    const message = create('p', '', 'message', panel); message.setAttribute('role', 'status');
    function updateStatus(status) { email.textContent = status.email || '—'; login.textContent = status.state === 'authenticated' ? '当前已登录' : status.state === 'rejected' ? '登录账号不一致' : status.state === 'login_required' ? '需要登录' : '正在确认登录'; plan.textContent = '当前套餐：' + (status.plan || '官网暂未提供'); }
    function updatePickers() { cards.set(data.cards.map(card => ({ id: card.id, label: card.label + ' · ' + card.brand + ' •••• ' + card.last4 })), cardID); addresses.set(data.addresses.map(address => ({ id: address.id, label: address.address_line1 + ', ' + address.city + ', ' + address.state + ' ' + address.postal_code })), addressID); }
    async function run(button, action) { button.disabled = true; message.textContent = '处理中…'; try { const result = await action(); message.textContent = result.message || ''; } catch (error) { message.textContent = error.message; } finally { button.disabled = false; } }
    async function load() {
      const result = await call('load'); data = result;
      const preferredCard = data.payment_card_id || cardID;
      cardID = data.cards.some(card => card.id === preferredCard) ? preferredCard : data.payment_card_id ? '' : data.cards[0]?.id || '';
      cvc.value = '';
      const preferredAddress = data.billing_address_id || addressID;
      addressID = data.addresses.some(address => address.id === preferredAddress) ? preferredAddress : data.billing_address_id ? '' : data.addresses[0]?.id || '';
      updatePickers(); showAddress(); updateStatus(result.status); await showCard();
      return { message: '已读取 ' + data.cards.length + ' 张银行卡、' + data.addresses.length + ' 条地址' };
    }
    refresh.onclick = event => { if (event.isTrusted) run(refresh, load); };
    document.body.append(host);
    position();
    run(refresh, load);
    const timer = setInterval(() => { if (!host.isConnected) { clearInterval(timer); resizeObserver.disconnect(); window.removeEventListener('resize', position); return; } call('status').then(updateStatus).catch(() => {}); }, 5000);
  };
  document.addEventListener('DOMContentLoaded', mount, { once: true });
  mount(); new MutationObserver(mount).observe(document, { childList: true, subtree: true, attributes: true, attributeFilter: ['style', 'class', 'hidden'] });
}
