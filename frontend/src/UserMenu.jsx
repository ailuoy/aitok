import React, { useEffect, useRef, useState } from 'react';
import { ChevronDown, Coins, LogOut, UserRound } from 'lucide-react';
import { Link } from './router';
import ThemeControl from './ThemeControl';
import TwoFactor from './TwoFactor';
import Dialog from './Dialog';

export default function UserMenu({ user, token, path, onLogout }) {
  const menu = useRef(null);
  const [security, setSecurity] = useState(false);
  const admin = ['admin','super_admin'].includes(user?.role);
  useEffect(() => { if (menu.current) menu.current.open = false; }, [path]);
  useEffect(() => {
    const dismiss = event => { if (event.key === 'Escape' || (event.type === 'pointerdown' && !menu.current?.contains(event.target))) { if (menu.current) menu.current.open = false; } };
    document.addEventListener('pointerdown', dismiss); document.addEventListener('keydown', dismiss);
    return () => { document.removeEventListener('pointerdown', dismiss); document.removeEventListener('keydown', dismiss); };
  }, []);
  return <><details className="user-menu" ref={menu}><summary><UserRound size={16} />我的<ChevronDown size={14} /></summary><div className="user-menu-panel"><div className="user-identity"><strong>{user?.username || user?.email || '我的账号'}</strong><small>{{ super_admin: '超级管理员', admin: '管理员' }[user?.role] || '用户'}</small></div>{admin && <><button className="ghost" onClick={() => { menu.current.open = false; setSecurity(true); }}>两步验证</button><Link to="/admin/wallet" onClick={() => { menu.current.open = false; }}><Coins size={16} />钱包与充值</Link></>}<div className="user-theme"><span>外观主题</span><ThemeControl /></div><button className="ghost" onClick={onLogout}><LogOut size={16} />退出登录</button></div></details>{security && admin && <Dialog title="两步验证" onClose={() => setSecurity(false)}><TwoFactor token={token} /></Dialog>}</>;
}
