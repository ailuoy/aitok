import React, { useSyncExternalStore } from 'react';

export const adminPages = ['accounts', 'proxies', 'addresses', 'bank-cards', 'users', 'wallet', 'orders', 'packages', 'notices', 'audit', 'proxy-activity', 'payment-exceptions'];
export const adminPath = page => '/admin/' + page;
export function canonicalAdminPath(path) {
  if (path === '/admin' || path === '/admin/') return adminPath('accounts');
  const page = path.replace(/^\//, '');
  return adminPages.includes(page) ? adminPath(page) : path;
}

const snapshot = () => location.pathname + location.search + location.hash;
function subscribe(listener) {
  window.addEventListener('popstate', listener);
  return () => window.removeEventListener('popstate', listener);
}

export function navigate(to, { replace = false } = {}) {
  if (snapshot() === to) return;
  history[replace ? 'replaceState' : 'pushState'](null, '', to);
  window.dispatchEvent(new PopStateEvent('popstate'));
  if (!replace) window.scrollTo(0, 0);
}

export function useRoute() {
  const value = useSyncExternalStore(subscribe, snapshot);
  return new URL(value, location.origin);
}

export function Link({ to, children, onClick, ...props }) {
  return <a href={to} {...props} onClick={event => {
    onClick?.(event);
    if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey || props.target || props.download) return;
    event.preventDefault();
    navigate(to);
  }}>{children}</a>;
}

export function loginDestination(route) {
  const next = route.searchParams.get('next');
  if (!next || !next.startsWith('/') || next.startsWith('//')) return adminPath('accounts');
  const target = new URL(next, location.origin);
  const path = canonicalAdminPath(target.pathname);
  return target.origin === location.origin && (path === '/desktop/authorize' || adminPages.some(page => adminPath(page) === path)) ? path + target.search + target.hash : adminPath('accounts');
}
