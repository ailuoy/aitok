// 此函数同时在登录检查和助手隔离上下文中执行，只读取验证页标记。
export function isVerificationPage() {
  if (document.querySelector('#challenge-form, #cf-challenge-running, #cf-error-details')) return true;
  return [...document.querySelectorAll('iframe[src^="https://challenges.cloudflare.com/"]')].some(frame => {
    const rect = frame.getBoundingClientRect();
    const style = getComputedStyle(frame);
    return rect.width > 0 && rect.height > 0 && style.visibility !== 'hidden' && style.display !== 'none';
  });
}

export function loginCheckSource() {
  return `(${checkLogin.toString()})(${isVerificationPage.toString()})`;
}

async function checkLogin(isVerificationPage) {
  if (location.origin !== 'https://chatgpt.com' || document.readyState !== 'complete') return null;
  if (isVerificationPage()) return { challenge: true };
  try {
    const response = await fetch('/api/auth/session', { credentials: 'include', cache: 'no-store', signal: AbortSignal.timeout(8000) });
    if (response.headers.get('cf-mitigated') === 'challenge') return { challenge: true, status: response.status };
    const body = await response.json().catch(() => ({}));
    let claims = {};
    try { claims = JSON.parse(atob(body.accessToken.split('.')[1].replace(/-/g, '+').replace(/_/g, '/'))); } catch {}
    return {
      status: response.status,
      email: typeof body.user?.email === 'string' ? body.user.email : null,
      plan: body.account?.planType || body.account?.plan_type || body.user?.planType || claims['https://api.openai.com/auth']?.chatgpt_plan_type || null,
    };
  } catch { return { status: 0 }; }
}
