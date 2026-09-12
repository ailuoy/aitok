import React, { useSyncExternalStore } from 'react';

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
  if (!next || !next.startsWith('/') || next.startsWith('//')) return '/accounts';
  const target = new URL(next, location.origin);
  return ['/accounts', '/wallet'].includes(target.pathname) ? target.pathname + target.search : '/accounts';
}
