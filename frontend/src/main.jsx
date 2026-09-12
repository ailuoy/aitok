import React, { useState, useEffect } from 'react';
import { createRoot } from 'react-dom/client';
import { Sparkles, LogOut, ArrowRight } from 'lucide-react';
import Dashboard from './Dashboard';
import Auth from './Auth';
import PublicPage from './PublicPage';
import { request } from './api';
import { Link, navigate, useRoute, loginDestination } from './router';
import './style.css';

const titles = { '/': 'ChatGPT 账号管理', '/features': '功能', '/plans': '套餐', '/security': '安全', '/login': '登录', '/register': '注册', '/accounts': '账号管理', '/wallet': '钱包与充值' };

function App() {
  const route = useRoute();
  const path = route.pathname;
  const [token, setToken] = useState(() => localStorage.getItem('token'));
  const [user, setUser] = useState(null);
  const [accounts, setAccounts] = useState([]);
  const [sessionError, setSessionError] = useState('');
  const [retry, setRetry] = useState(0);
  const protectedPage = ['/accounts', '/wallet'].includes(path);
  const authPage = ['/login', '/register'].includes(path);

  useEffect(() => {
    document.title = `${titles[path] || '页面不存在'} · AiTok`;
    // 兼容已经创建的 Stripe 订单返回地址。
    if (path === '/' && route.searchParams.has('topup')) {
      navigate('/wallet' + route.search, { replace: true });
    } else if (protectedPage && !token) {
      navigate('/login?next=' + encodeURIComponent(path + route.search), { replace: true });
    } else if (token && user && (authPage || path === '/')) {
      navigate(authPage ? loginDestination(route) : '/accounts', { replace: true });
    }
  }, [path, route.search, token, user, protectedPage, authPage]);

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
    return () => { controller.abort(); clearTimeout(timer); };
  }, [token, retry]);

  const logout = () => {
    localStorage.removeItem('token'); setToken(null); setUser(null); setAccounts([]); setSessionError(''); navigate('/');
  };
  let content;
  if (token && !user && (protectedPage || authPage || path === '/')) {
    content = <main className="auth-wrap"><section className="auth-card" aria-label="恢复登录"><h2>正在恢复登录</h2><p role="status">{sessionError || '正在验证登录状态…'}</p>{sessionError && <button className="primary full" onClick={() => setRetry(value => value + 1)}>立即重试</button>}<button className="forgot-link" onClick={logout}>退出登录</button></section></main>;
  } else if (protectedPage && token && user) {
    content = <Dashboard user={user} accounts={accounts} setAccounts={setAccounts} token={token} route={route} />;
  } else if (authPage || protectedPage) {
    content = <Auth key={path} mode={path === '/register' ? 'signup' : 'login'} onSuccess={value => { localStorage.setItem('token', value); setToken(value); setUser(null); }} onBack={() => navigate('/')} onModeChange={mode => navigate((mode === 'signup' ? '/register' : '/login') + route.search)} />;
  } else {
    content = <PublicPage path={path} signedIn={!!token} />;
  }
  return <><Header user={user} token={token} path={path} onLogout={logout} />{content}</>;
}

function Header({ user, token, path, onLogout }) {
  return <header>
    <Link to="/" className="brand" aria-label="AiTok 首页"><span className="logo"><Sparkles size={18} /></span><b>AiTok</b></Link>
    <nav aria-label="主导航">{[['/features', '功能'], ['/plans', '套餐'], ['/security', '安全']].map(([to, label]) => <Link key={to} to={to} aria-current={path === to ? 'page' : undefined}>{label}</Link>)}</nav>
    {token ? <div className="userbar"><span>{user?.username || user?.email}{user?.role === 'super_admin' ? ' · 超级管理员' : ''}</span><Link className="outline small" to="/accounts">工作台</Link><button className="ghost" onClick={onLogout}><LogOut size={16} />退出</button></div> : <div className="actions"><Link className="ghost" to="/login">登录</Link><Link className="primary small" to="/register">免费开始 <ArrowRight size={15} /></Link></div>}
  </header>;
}

createRoot(document.getElementById('root')).render(<App />);
