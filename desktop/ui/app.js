const $ = selector => document.querySelector(selector);
const editor = $('#editor'), form = $('#site-form');
let state, editingID, busy = false;
const showError = (node, text = '') => { node.textContent = text; node.hidden = !text; };
const el = (tag, text, className) => { const node = document.createElement(tag); if (text) node.textContent = text; if (className) node.className = className; return node; };

function render(next) {
  state = next;
  $('#version').textContent = next.version;
  $('#environment-description').textContent = next.profile.label + ' · ' + next.profile.origin;
  $('#add').hidden = next.sites.length > 0;
  document.title = next.profile.name;
  const auth = next.auth, loggedIn = auth.status === 'authenticated', pending = auth.status === 'pending';
  $('#login-title').textContent = loggedIn ? '已登录 · ' + (auth.user.username || auth.user.email) : pending ? '等待后台授权' : '授权登录';
  $('#login-description').textContent = loggedIn ? '已通过后台鉴权，打开账号仍需两步验证。' : pending ? '请在打开的后台页面登录并确认授权，然后返回助手。' : '点击登录，将在浏览器打开' + next.profile.label + '后台进行鉴权。';
  $('#login').textContent = loggedIn ? '退出登录' : pending ? '取消登录' : '授权登录';
  $('#login').disabled = busy;
  showError($('#login-error'), auth.error);
  $('#summary').textContent = `${next.sites.filter(site => site.running).length} 个站点运行中`;
  $('#startup').checked = next.loginAtStartup;
  $('#startup').disabled = !next.packaged || busy;
  $('#startup').title = next.packaged ? '' : '安装桌面助手后可设置';
  const list = $('#sites'); list.replaceChildren();
  if (!next.sites.length) list.append(el('div', '添加一个站点，连接你的 AiTok 后台。', 'empty'));
  for (const site of next.sites) {
    const card = el('article', '', 'site'), top = el('div', '', 'site-top'), info = el('div'), title = el('div', '', 'site-title');
    title.append(el('h2', site.name), el('span', site.running ? '已启动' : site.error ? '未连接' : '已停止', `status ${site.running ? 'running' : site.error ? 'failed' : ''}`));
    info.append(title, el('p', site.origin, 'origin')); top.append(info);
    const open = el('button', '打开后台'); open.addEventListener('click', () => act(() => window.assistant.openSite(site.id))); top.append(open);
    const bottom = el('div', '', 'site-bottom'), buttons = el('div', '', 'buttons');
    bottom.append(el('span', `本机端口 ${site.port}  ·  ${site.browsers} 个账号窗口`, 'meta'));
    for (const [label, callback, style] of [
      [site.running ? '停止' : '启动', () => act(() => window.assistant.toggle(site.id, !site.running)), ''],
      ['编辑', () => edit(site), ''],
      ['移除', () => act(() => window.assistant.remove(site.id)), 'danger'],
    ]) { const button = el('button', label, style); button.disabled = busy; button.addEventListener('click', callback); buttons.append(button); }
    bottom.append(buttons); card.append(top);
    if (site.error) card.append(el('p', site.error, 'error'));
    card.append(bottom); list.append(card);
  }
}

$('#login').addEventListener('click', () => act(() => state.auth.status === 'authenticated' ? window.assistant.logout() : state.auth.status === 'pending' ? window.assistant.cancelLogin() : window.assistant.login()));

async function act(action) {
  if (busy) return;
  busy = true; showError($('#error')); if (state) render(state);
  try { render(await action()); }
  catch (error) { showError($('#error'), error.message); }
  finally { busy = false; if (state) render(state); }
}

function edit(site) {
  editingID = site?.id;
  form.reset();
  form.elements.name.value = site?.name || '';
  form.elements.origin.value = state.profile.origin;
  form.elements.origin.readOnly = true;
  form.elements.port.value = site?.port || state.profile.port;
  form.elements.enabled.checked = site?.enabled !== false;
  $('#editor-title').textContent = site ? '编辑站点' : '添加站点';
  showError($('#form-error'));
  editor.showModal();
}
$('#add').addEventListener('click', () => edit());
$('#cancel').addEventListener('click', () => editor.close());
$('#cancel-x').addEventListener('click', () => editor.close());
form.elements.origin.addEventListener('change', () => {
  if (editingID) return;
  try { form.elements.port.value = ['localhost', '127.0.0.1', '[::1]'].includes(new URL(form.elements.origin.value).hostname) ? 15684 : 15683; } catch {}
});
form.addEventListener('submit', async event => {
  event.preventDefault(); if (busy) return;
  busy = true; showError($('#form-error'));
  const submit = form.querySelector('[type=submit]'); submit.disabled = true;
  try {
    render(await window.assistant.save({ id: editingID, name: form.elements.name.value, origin: form.elements.origin.value, port: Number(form.elements.port.value), enabled: form.elements.enabled.checked }));
    editor.close();
  } catch (error) { showError($('#form-error'), error.message); }
  finally { busy = false; submit.disabled = false; if (state) render(state); }
});
$('#startup').addEventListener('change', event => {
  const enabled = event.target.checked;
  void act(() => window.assistant.loginAtStartup(enabled));
});
async function refresh() {
  if (busy) return;
  try { render(await window.assistant.state()); }
  catch (error) { showError($('#error'), error.message); }
}
void refresh();
setInterval(refresh, 2500);
