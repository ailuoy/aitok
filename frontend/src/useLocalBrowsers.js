import { useCallback, useEffect, useRef, useState } from 'react';
import { browserEnvironmentID, launcherRequest } from './localBrowser';
import { request } from './api';

export const browserRunning = status => Boolean(status && !['closed', 'unavailable'].includes(status.state));

export default function useLocalBrowsers(accounts, userID, token, setAccounts) {
  const [states, setStates] = useState({});
  const [closing, setClosing] = useState({});
  const versions = useRef({});
  const recorded = useRef({});
  const recording = useRef(new Set());
  const [loginError, setLoginError] = useState('');
  const accountIDs = JSON.stringify(accounts.filter(account => account.user_id === userID).map(account => account.id));
  const update = useCallback((id, status) => {
    versions.current[id] = (versions.current[id] || 0) + 1;
    setStates(current => ({ ...current, [id]: status }));
  }, []);
  useEffect(() => {
    for (const [id, status] of Object.entries(states)) {
      const at = status.authenticated_at;
      const key = `${userID}:${id}:${at}`;
      if (!at || recorded.current[key] || recording.current.has(key)) continue;
      if (!accounts.some(account => String(account.id) === id && account.user_id === userID)) continue;
      recording.current.add(key);
      request('/accounts/' + id + '/login', token, { method: 'POST', body: { logged_in_at: at } })
        .then(data => { recorded.current[key] = true; setLoginError(''); setAccounts(current => current.map(account => String(account.id) === id ? { ...account, last_login_at: data.last_login_at } : account)); })
        .catch(() => setLoginError('登录时间暂未同步，正在重试。'))
        .finally(() => recording.current.delete(key));
    }
  }, [states, accounts, userID, token, setAccounts]);
  useEffect(() => {
    const controller = new AbortController();
    let timer;
    const poll = async () => {
      await Promise.all(JSON.parse(accountIDs).map(async id => {
        const version = versions.current[id];
        try {
          const status = await launcherRequest('/browsers/' + encodeURIComponent(browserEnvironmentID(userID, id)), { signal: controller.signal });
          if (!controller.signal.aborted && versions.current[id] === version) update(id, status);
        } catch { /* 暂时断连时保留状态，避免把仍在运行的窗口误报为已关闭。 */ }
      }));
      if (!controller.signal.aborted) timer = setTimeout(poll, 2000);
    };
    poll();
    return () => { controller.abort(); clearTimeout(timer); };
  }, [accountIDs, userID, update]);
  const close = async id => {
    if (closing[id]) return;
    setClosing(current => ({ ...current, [id]: true }));
    versions.current[id] = (versions.current[id] || 0) + 1;
    try { update(id, await launcherRequest('/browsers/' + encodeURIComponent(browserEnvironmentID(userID, id)), { method: 'DELETE' })); }
    finally { setClosing(current => ({ ...current, [id]: false })); }
  };
  return { states, closing, update, close, loginError };
}
