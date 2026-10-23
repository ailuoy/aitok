import React, { useRef, useState } from 'react';
import Dialog from './Dialog';
import SessionRefresh from './SessionRefresh';

export default function BatchSessionRefresh({ accounts, userID, token, onStatus, onUpdated, onClose }) {
  // 固定点击时的账号快照；每个任务完成后才挂载下一个，复用单账号更新流程。
  const [queue] = useState(() => accounts);
  const [run, setRun] = useState(() => ({ accounts, index: 0, attempt: 0 }));
  const [results, setResults] = useState([]);
  const [stopping, setStopping] = useState(false);
  const settled = useRef(new Set());
  const current = run.accounts[run.index];
  const finished = !current || stopping;
  const saved = results.filter(result => result.ok).length;
  const failed = results.filter(result => !result.ok);
  const retryingIDs = new Set(!finished && run.attempt > 0 ? run.accounts.slice(run.index).map(account => account.id) : []);
  function complete(ok, message) {
    const key = `${run.attempt}:${current.id}`;
    if (settled.current.has(key)) return;
    settled.current.add(key);
    const result = { id: current.id, email: current.email, ok, message };
    setResults(previous => previous.some(item => item.id === current.id)
      ? previous.map(item => item.id === current.id ? result : item)
      : [...previous, result]);
    setRun(previous => previous === run ? { ...previous, index: previous.index + 1 } : previous);
  }

  function retry(ids) {
    if (!finished) return;
    const retryAccounts = queue.filter(account => ids.includes(account.id) && failed.some(result => result.id === account.id));
    if (!retryAccounts.length) return;
    setStopping(false);
    setRun(previous => ({ accounts: retryAccounts, index: 0, attempt: previous.attempt + 1 }));
  }

  return <Dialog title="批量更新 Session" titleActions={<button className="outline small" disabled={!finished || !failed.length} onClick={() => retry(failed.map(result => result.id))}>一键重试失败</button>} className="local-browser-dialog" onClose={onClose}>
    <div className="batch-session-refresh">
      <p role="status">已完成 {results.length} / {queue.length}，成功 {saved}，失败 {failed.length}{stopping ? '，已停止后续更新' : !current ? results.length === queue.length ? '，全部处理完毕' : '，本轮处理完毕，仍有未执行账号' : run.attempt > 0 ? `，正在重试 ${run.index + 1} / ${run.accounts.length}` : ''}</p>
      <p className="muted">按顺序逐个更新。失败窗口保留供检查，重试前请先关闭对应浏览器窗口；关闭此弹窗会停止后续任务，已打开的窗口仍可继续更新。</p>
      {!!results.length && <ul className="batch-session-results" aria-label="批量更新结果">{results.map(result => <li key={result.id} className={result.ok ? 'success' : 'error'}>
        <div className="batch-session-result-details">
          <strong>{result.email}</strong>
          <span>{retryingIDs.has(result.id) ? current.id === result.id ? '正在重试…' : '等待重试…' : result.message}</span>
        </div>
        {!result.ok && <button className="outline small" data-audit-account-id={result.id} disabled={!finished} onClick={() => retry([result.id])}>重试</button>}
      </li>)}</ul>}
      {!finished && <SessionRefresh key={`${run.attempt}:${current.id}`} account={current} userID={userID} token={token} onStatus={onStatus} onUpdated={onUpdated} onComplete={() => complete(true, 'Session 已更新，浏览器已关闭')} onFailure={message => complete(false, message)} />}
      <div className="browser-buttons">{!finished && <button className="outline" onClick={() => setStopping(true)}>停止后续更新</button>}<button className="outline" onClick={onClose}>{finished ? '完成' : '关闭'}</button></div>
    </div>
  </Dialog>;
}
