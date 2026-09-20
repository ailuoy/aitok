import React, { useState, useEffect } from 'react';
import { createRoot } from 'react-dom/client';
import { Sparkles, ArrowRight, PanelLeftClose, PanelLeftOpen } from 'lucide-react';
import Dashboard from './Dashboard';
import Auth from './Auth';
import PublicPage from './PublicPage';
import { request } from './api';
import { Link, navigate, useRoute, loginDestination, adminPages, adminPath, canonicalAdminPath } from './router';
import './style.css';
import UserMenu from './UserMenu';
import AdminLayout from './AdminLayout';
import DesktopAuthorize from './DesktopAuthorize';

const titles = { '/': 'ChatGPT 账号管理', '/features': '功能', '/plans': '套餐', '/security': '安全', '/desktop/authorize': '授权登录助手', '/login': '登录', '/register': '注册', '/admin/accounts': '账号管理', '/admin/wallet': '钱包与充值', '/admin/proxies': 'SOCKS5 管理', '/admin/addresses': '地址管理', '/admin/bank-cards': '银行卡管理', '/admin/users': '用户列表', '/admin/orders':'充值订单','/admin/packages':'充值套餐','/admin/notices':'到期与异常','/admin/audit':'操作审计','/admin/proxy-activity':'设备使用记录','/admin/payment-exceptions':'支付退款与异常' };

function App() {
  const route = useRoute();
  const path = canonicalAdminPath(route.pathname);
  const [token, setToken] = useState(() => localStorage.getItem('token'));
  const [user, setUser] = useState(null);
  const [accounts, setAccounts] = useState([]);
  const [sessionError, setSessionError] = useState('');
  const [retry, setRetry] = useState(0);
  const [sidebarCollapsed, setSidebarCollapsed] = useState(() => { try { return localStorage.getItem('admin-sidebar-collapsed') === 'true'; } catch { return false; } });
  function toggleSidebar() {
    setSidebarCollapsed(value => { const next = !value; try { localStorage.setItem('admin-sidebar-collapsed', String(next)); } catch {} return next; });
  }
  function expandSidebar() {
    setSidebarCollapsed(false);
    try { localStorage.setItem('admin-sidebar-collapsed', 'false'); } catch {}
  }
  const desktopAuth = path === '/desktop/authorize';
  const protectedPage = desktopAuth || adminPages.some(page => adminPath(page) === path);
  const authPage = ['/login', '/register'].includes(path);

  useEffect(() => {
    document.title = `${titles[path] || '页面不存在'} · AiTok`;
    // 兼容已经创建的 Stripe 订单返回地址。
    if (route.pathname !== path) {
      navigate(path + route.search + route.hash, { replace: true });
    } else if (path === '/' && route.searchParams.has('topup')) {
      navigate('/admin/wallet' + route.search, { replace: true });
    } else if (protectedPage && !token) {
      navigate('/login?next=' + encodeURIComponent(path + route.search), { replace: true });
    } else if (token && user && (authPage || path === '/')) {
      navigate(authPage ? loginDestination(route) : '/admin/accounts', { replace: true });
    }
  }, [path, route.pathname, route.search, route.hash, token, user, protectedPage, authPage]);

  useEffect(() => {
    if (!token) return;
    const controller = new AbortController();
    let timer;
    const restore = async () => {
      try {
        const data = await request('/me', token, { signal: controller.signal });
        if (controller.signal.aborted) return;
        setUser(data.user); setAccounts(data.accounts || []); setSessionError('');
      } catch (error) {
        if (controller.signal.aborted) return;
        if (error.status === 401) {
          localStorage.removeItem('token'); setToken(null); setUser(null); setAccounts([]); setSessionError('');
        } else {
          setSessionError('暂时无法连接服务，登录状态已保留，正在自动重试。');
          timer = setTimeout(restore, 3000);
        }
      }
    };
    restore();
    const interval = setInterval(restore, 30000);
    window.addEventListener('focus', restore);
    return () => { controller.abort(); clearTimeout(timer); clearInterval(interval); window.removeEventListener('focus', restore); };
  }, [token, retry]);

  const logout = async () => {
    if(token) { try { await request('/logout',token,{method:'POST'}); } catch(error) { if(error.status!==401){setSessionError(error.message);return;} } }
    localStorage.removeItem('token'); setToken(null); setUser(null); setAccounts([]); setSessionError(''); navigate('/');
  };
  let content;
  if (token && !user && (protectedPage || authPage || path === '/')) {
    content = <main className="auth-wrap"><section className="auth-card" aria-label="恢复登录"><h2>正在恢复登录</h2><p role="status">{sessionError || '正在验证登录状态…'}</p>{sessionError && <button className="primary full" onClick={() => setRetry(value => value + 1)}>立即重试</button>}<button className="forgot-link" onClick={logout}>退出登录</button></section></main>;
  } else if (desktopAuth && token && user) {
    content = <DesktopAuthorize token={token} user={user} route={route} />;
  } else if (protectedPage && token && user) {
    content = !['admin','super_admin'].includes(user.role) && !['/admin/accounts', '/admin/wallet'].includes(path) ? <main className="dashboard"><p>无权访问此页面。</p><Link to="/admin/accounts">返回我的账号</Link></main> : <AdminLayout key={user.id + ":" + user.role} collapsed={sidebarCollapsed} onExpandSidebar={expandSidebar} user={user} token={token} path={path}><Dashboard key={user.id + ":" + user.role} user={user} accounts={accounts} setAccounts={setAccounts} token={token} route={route} /></AdminLayout>;
  } else if (authPage || protectedPage) {
    content = <Auth key={path} mode={path === '/register' ? 'signup' : 'login'} onSuccess={value => { localStorage.setItem('token', value); setToken(value); setUser(null); }} onBack={() => navigate('/')} onModeChange={mode => navigate((mode === 'signup' ? '/register' : '/login') + route.search)} />;
  } else {
    content = <PublicPage path={path} signedIn={!!token} />;
  }
  return <><Header sidebarCollapsed={sidebarCollapsed} onToggleSidebar={toggleSidebar} user={user} token={token} path={path} onLogout={logout} />{content}</>;
}

function Header({ sidebarCollapsed, onToggleSidebar, user, token, path, onLogout }) {
  return <header className={path.startsWith('/admin/') ? 'admin-header' : undefined}>
    <Link to="/" className="brand" aria-label="AiTok 首页"><span className="logo"><Sparkles size={18} /></span><b>AiTok</b></Link>
    {user && path.startsWith("/admin/") && <button type="button" className="sidebar-collapse icon-btn" aria-label={sidebarCollapsed ? "展开侧边栏" : "折叠侧边栏"} title={sidebarCollapsed ? "展开侧边栏" : "折叠侧边栏"} aria-controls="admin-sidebar" aria-expanded={!sidebarCollapsed} onClick={onToggleSidebar}>{sidebarCollapsed ? <PanelLeftOpen size={18} /> : <PanelLeftClose size={18} />}</button>}
    {!path.startsWith('/admin/') && <nav aria-label="主导航">{[['/features', '功能'], ['/plans', '套餐'], ['/security', '安全']].map(([to, label]) => <Link key={to} to={to} aria-current={path === to ? 'page' : undefined}>{label}</Link>)}</nav>}
    {token ? <div className="userbar"><Link className="outline small" to="/admin/accounts">工作台</Link><UserMenu user={user} token={token} path={path} onLogout={onLogout} /></div> : <div className="actions"><Link className="ghost" to="/login">登录</Link><Link className="primary small" to="/register">免费开始 <ArrowRight size={15} /></Link></div>}
  </header>;
}

createRoot(document.getElementById('root')).render(<App />);
