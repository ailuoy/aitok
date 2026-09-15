import React, { useEffect, useRef, useState } from 'react';
import { CreditCard, MapPin, Menu, MessageSquare, Monitor, Users, X } from 'lucide-react';
import { Link, adminPath } from './router';

const items = [
  ['accounts', 'ChatGPT 账号', MessageSquare],
  ['proxies', 'SOCKS5 管理', Monitor],
  ['addresses', '地址管理', MapPin],
  ['bank-cards', '银行卡管理', CreditCard],
];

export default function AdminLayout({ user, path, children }) {
  const [open, setOpen] = useState(false);
  const trigger = useRef(null);
  const links = user.role === 'super_admin' ? [...items, ['users', '用户列表', Users]] : items;
  useEffect(() => { setOpen(false); }, [path]);
  useEffect(() => {
    const escape = event => { if (event.key === 'Escape' && open) { setOpen(false); trigger.current?.focus(); } };
    window.addEventListener('keydown', escape);
    return () => window.removeEventListener('keydown', escape);
  }, [open]);
  return <div className="admin-shell">
    <button ref={trigger} className="sidebar-toggle outline small" aria-controls="admin-sidebar" aria-expanded={open} onClick={() => setOpen(value => !value)}><Menu size={16} />管理菜单</button>
    {open && <button className="sidebar-backdrop" aria-label="关闭管理菜单" onClick={() => setOpen(false)} />}
    <aside id="admin-sidebar" className={'admin-sidebar' + (open ? ' open' : '')}>
      <div className="sidebar-heading"><span>工作台</span><button className="sidebar-close icon-btn" aria-label="收起管理菜单" onClick={() => { setOpen(false); trigger.current?.focus(); }}><X size={18} /></button></div>
      <nav className="workspace-nav" aria-label="工作台">{links.map(([page, label, Icon]) => <Link key={page} to={adminPath(page)} aria-current={path === adminPath(page) ? 'page' : undefined}><Icon size={17} />{label}</Link>)}</nav>
      <div className="sidebar-role">{{ super_admin: '超级管理员', admin: '管理员' }[user.role] || '用户'}</div>
    </aside>
    <div className="admin-content">{children}</div>
  </div>;
}
