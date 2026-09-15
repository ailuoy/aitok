import React, { useEffect, useRef, useState } from 'react';
import { CreditCard, MapPin, Menu, MessageSquare, Monitor, Users, X } from 'lucide-react';
import { Link, adminPath } from './router';
import { installAdminActivity, recordAdminActivity } from './adminActivity';

const items = [
  ['accounts', 'ChatGPT 账号', MessageSquare],
  ['orders','充值订单',CreditCard],
  ['packages','充值套餐',CreditCard],
  ['notices','到期与异常',MessageSquare],
  ['proxy-activity','设备使用记录',Monitor],
  ['proxies', 'SOCKS5 管理', Monitor],
  ['addresses', '地址管理', MapPin],
  ['bank-cards', '银行卡管理', CreditCard],
];

export default function AdminLayout({ collapsed = false, user, token, path, children }) {
  const [open, setOpen] = useState(false);
  useEffect(() => installAdminActivity(token, user.id), [token, user.id]);
  useEffect(() => { void recordAdminActivity({ token, userID: user.id, page: path }, { kind: 'page_view', control: '', result: 'visited' }); }, [token, user.id, path]);
  const trigger = useRef(null);
  const allLinks = [...items, ...(['admin','super_admin'].includes(user.role) ? [['payment-exceptions','支付退款与异常',CreditCard],['audit','操作审计',Users]] : []), ...(user.role==='super_admin' ? [['users','用户列表',Users]] : [])];
  const links = ['admin','super_admin'].includes(user.role) ? allLinks : items.filter(([page]) => page === 'accounts');
  useEffect(() => { setOpen(false); }, [path]);
  useEffect(() => {
    const escape = event => { if (event.key === 'Escape' && open) { setOpen(false); trigger.current?.focus(); } };
    window.addEventListener('keydown', escape);
    return () => window.removeEventListener('keydown', escape);
  }, [open]);
  return <div className={"admin-shell" + (collapsed ? " sidebar-collapsed" : "") }>
    <button ref={trigger} className="sidebar-toggle outline small" aria-controls="admin-sidebar" aria-expanded={open} onClick={() => setOpen(value => !value)}><Menu size={16} />管理菜单</button>
    {open && <button className="sidebar-backdrop" aria-label="关闭管理菜单" onClick={() => setOpen(false)} />}
    <aside id="admin-sidebar" className={'admin-sidebar' + (open ? ' open' : '')}>
      <div className="sidebar-heading"><button className="sidebar-close icon-btn" aria-label="收起管理菜单" onClick={() => { setOpen(false); trigger.current?.focus(); }}><X size={18} /></button></div>
      <nav className="workspace-nav" aria-label="工作台">{links.map(([page, label, Icon]) => <Link key={page} to={adminPath(page)} title={label} aria-label={label} aria-current={path === adminPath(page) ? 'page' : undefined}><Icon size={17} /><span className="sidebar-label">{label}</span></Link>)}</nav>
      <div className="sidebar-role">{{ super_admin: '超级管理员', admin: '管理员' }[user.role] || '用户'}</div>
    </aside>
    <div className="admin-content">{children}</div>
  </div>;
}
