import React, { useEffect, useRef } from 'react';
import { ChevronDown, Coins, LogOut, UserRound } from 'lucide-react';
import { Link } from './router';
import ThemeControl from './ThemeControl';

export default function UserMenu({ user, path, onLogout }) {
  const menu = useRef(null);
  useEffect(() => { if (menu.current) menu.current.open = false; }, [path]);
  useEffect(() => {
    const dismiss = event => { if (event.key === 'Escape' || (event.type === 'pointerdown' && !menu.current?.contains(event.target))) { if (menu.current) menu.current.open = false; } };
    document.addEventListener('pointerdown', dismiss); document.addEventListener('keydown', dismiss);
    return () => { document.removeEventListener('pointerdown', dismiss); document.removeEventListener('keydown', dismiss); };
  }, []);
  return <details className="user-menu" ref={menu}><summary><UserRound size={16} />我的<ChevronDown size={14} /></summary><div className="user-menu-panel"><div className="user-identity"><strong>{user?.username || user?.email || '我的账号'}</strong>{user?.role === 'super_admin' && <small>超级管理员</small>}</div><Link to="/wallet" onClick={() => { menu.current.open = false; }}><Coins size={16} />钱包与充值</Link><div className="user-theme"><span>外观主题</span><ThemeControl /></div><button className="ghost" onClick={onLogout}><LogOut size={16} />退出登录</button></div></details>;
}
