import React, { useEffect, useRef, useState } from 'react';
import { ChevronRight, CreditCard, Menu, MessageSquare, Monitor, Settings, X } from 'lucide-react';
import { Link, adminPath } from './router';
import { installAdminActivity, recordAdminActivity } from './adminActivity';

const menuGroups = [
  { id: 'accounts', label: '账号管理', Icon: MessageSquare, items: [['accounts', 'ChatGPT 账号'], ['notices', '到期与异常']] },
  { id: 'recharge', label: '充值管理', Icon: CreditCard, items: [['orders', '充值订单'], ['packages', '充值套餐'], ['bank-cards', '银行卡管理'], ['payment-exceptions', '支付退款与异常']] },
  { id: 'resources', label: '资源管理', Icon: Monitor, items: [['proxies', 'SOCKS5 管理'], ['addresses', '地址管理'], ['proxy-activity', '设备使用记录']] },
  { id: 'system', label: '系统管理', Icon: Settings, items: [['users', '用户列表', 'super_admin'], ['audit', '操作审计']] },
];

export default function AdminLayout({ collapsed = false, onExpandSidebar, user, token, path, children }) {
  const [open, setOpen] = useState(false);
  const [desktop, setDesktop] = useState(() => window.matchMedia('(min-width: 761px)').matches);
  const compact = collapsed && desktop;
  const storageKey = `admin-menu-groups:${user.id}`;
  const activeGroup = menuGroups.find(group => group.items.some(([page]) => path === adminPath(page)))?.id;
  const [expanded, setExpanded] = useState(() => {
    let saved;
    try { saved = JSON.parse(localStorage.getItem(storageKey)); } catch {}
    return Object.fromEntries(menuGroups.map(({ id }) => [id, typeof saved?.[id] === 'boolean' ? saved[id] : id === activeGroup]));
  });
  useEffect(() => {
    const media = window.matchMedia('(min-width: 761px)');
    const change = () => setDesktop(media.matches);
    media.addEventListener('change', change);
    return () => media.removeEventListener('change', change);
  }, []);
  useEffect(() => {
    if (activeGroup) setExpanded(value => value[activeGroup] ? value : { ...value, [activeGroup]: true });
  }, [path, activeGroup]);
  useEffect(() => {
    try { localStorage.setItem(storageKey, JSON.stringify(expanded)); } catch {}
  }, [storageKey, expanded]);
  useEffect(() => installAdminActivity(token, user.id), [token, user.id]);
  useEffect(() => { void recordAdminActivity({ token, userID: user.id, page: path }, { kind: 'page_view', control: '', result: 'visited' }); }, [token, user.id, path]);
  const trigger = useRef(null);
  const isAdmin = ['admin', 'super_admin'].includes(user.role);
  function toggleGroup(id) {
    setExpanded(value => ({ ...value, [id]: compact || !value[id] }));
    if (compact) onExpandSidebar();
  }
  function menuLink([page, label], icon = false) {
    return <Link key={page} to={adminPath(page)} title={label} aria-label={label} aria-current={path === adminPath(page) ? 'page' : undefined}>{icon && <MessageSquare size={17} />}<span className="sidebar-label">{label}</span></Link>;
  }
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
      <nav className="workspace-nav" aria-label="工作台">
        {isAdmin ? menuGroups.map(({ id, label, Icon, items }) => <div className="sidebar-group" key={id}>
          <button type="button" className={'sidebar-group-toggle' + (activeGroup === id ? ' active' : '')} title={label} aria-label={label} aria-expanded={!compact && expanded[id]} aria-controls={`sidebar-group-${id}`} onClick={() => toggleGroup(id)}>
            <Icon size={17} /><span className="sidebar-label">{label}</span><ChevronRight size={14} className="sidebar-chevron" />
          </button>
          <div id={`sidebar-group-${id}`} className="sidebar-submenu" hidden={compact || !expanded[id]}>
            {items.filter(([, , role]) => !role || role === user.role).map(item => menuLink(item))}
          </div>
        </div>) : menuLink(['accounts', 'ChatGPT 账号'], true)}
      </nav>
      <div className="sidebar-role">{{ super_admin: '超级管理员', admin: '管理员' }[user.role] || '用户'}</div>
    </aside>
    <div className="admin-content">{children}</div>
  </div>;
}
