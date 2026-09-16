import { useCallback, useEffect, useRef, useState } from 'react';
import { browserEnvironmentID, launcherRequest } from './localBrowser';
import { request } from './api';
import useLauncherPort from './useLauncherPort';

export const browserRunning = status => Boolean(status && !['closed', 'unavailable'].includes(status.state));

export default function useLocalBrowsers(accounts, userID, token, setAccounts) {
  const port = useLauncherPort();
  const [states, setStates] = useState({});
  const [closing, setClosing] = useState({});
  const versions = useRef({});
  const refreshController = useRef(null);
  const [refreshing, setRefreshing] = useState(false);
  const recorded = useRef({});
  const recording = useRef(new Set());
  const [loginError, setLoginError] = useState('');
  const update = useCallback((id, status) => {
    versions.current[id] = (versions.current[id] || 0) + 1;
    setStates(current => ({ ...current, [id]: status }));
  }, []);
  useEffect(() => {
    for (const [id, status] of Object.entries(states)) {
      const at = status.authenticated_at;
      const key = `${userID}:${id}:${at}`;
      if (!at || recorded.current[key] || recording.current.has(key)) continue;
      if (!accounts.some(account => String(account.id) === id)) continue;
      recording.current.add(key);
      request('/accounts/' + id + '/login', token, { method: 'POST', body: { logged_in_at: at } })
        .then(data => { recorded.current[key] = true; setLoginError(''); setAccounts(current => current.map(account => String(account.id) === id ? { ...account, last_login_at: data.last_login_at } : account)); })
        .catch(() => setLoginError('登录时间暂未同步，请刷新浏览器状态重试。'))
        .finally(() => recording.current.delete(key));
    }
  }, [states, accounts, userID, token, setAccounts]);
  useEffect(() => {
    setStates({});
    return () => { refreshController.current?.abort(); };
  }, [userID, port]);
  const refresh = async visibleAccounts => {
    if (refreshController.current) return;
    const controller = new AbortController();
    refreshController.current = controller;
    setRefreshing(true);
    try {
      // 仅手动检查当前页，逐个请求；助手不可达时立即停止。
      for (const { id } of visibleAccounts) {
        if (controller.signal.aborted) return;
        const version = versions.current[id];
        const status = await launcherRequest('/browsers/' + encodeURIComponent(browserEnvironmentID(userID, id)), { signal: controller.signal, port });
        if (!controller.signal.aborted && versions.current[id] === version) update(id, status);
      }
    } catch (error) {
      if (!controller.signal.aborted) throw error;
    } finally {
      refreshController.current = null;
      setRefreshing(false);
    }
  };
  const close = async id => {
    if (closing[id]) return;
    setClosing(current => ({ ...current, [id]: true }));
    versions.current[id] = (versions.current[id] || 0) + 1;
    try { update(id, await launcherRequest('/browsers/' + encodeURIComponent(browserEnvironmentID(userID, id)), { method: 'DELETE' })); }
    finally { setClosing(current => ({ ...current, [id]: false })); }
  };
  return { states, closing, update, close, refresh, refreshing, loginError };
}
