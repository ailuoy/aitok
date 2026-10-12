import React, { useRef, useState } from 'react';
import Dialog from './Dialog';
import SessionRefresh from './SessionRefresh';

export default function BatchSessionRefresh({ accounts, userID, token, onStatus, onUpdated, onClose }) {
  // 固定点击时的账号快照；每个任务完成后才挂载下一个，复用单账号更新流程。
  const [queue] = useState(() => accounts);
  const [results, setResults] = useState([]);
  const [stopping, setStopping] = useState(false);
  const settled = useRef(new Set());
  const current = queue[results.length];
  const finished = !current || stopping;
  const saved = results.filter(result => result.ok).length;
  function complete(ok, message) {
    if (settled.current.has(current.id)) return;
    settled.current.add(current.id);
    setResults(previous => [...previous, { id: current.id, email: current.email, ok, message }]);
  }

  return <Dialog title="批量更新 Session" className="local-browser-dialog" onClose={onClose}>
    <div className="batch-session-refresh">
      <p role="status">已完成 {results.length} / {queue.length}，成功 {saved}，失败 {results.length - saved}{stopping ? '，已停止后续更新' : !current ? '，全部处理完毕' : ''}</p>
      <p className="muted">按顺序逐个更新。失败窗口保留供检查；关闭此弹窗会停止后续任务，已打开的窗口仍可继续更新。</p>
      {!!results.length && <ul className="batch-session-results" aria-label="批量更新结果">{results.map(result => <li key={result.id} className={result.ok ? 'success' : 'error'}><strong>{result.email}</strong><span>{result.message}</span></li>)}</ul>}
      {!finished && <SessionRefresh key={current.id} account={current} userID={userID} token={token} onStatus={onStatus} onUpdated={onUpdated} onComplete={() => complete(true, 'Session 已更新，浏览器已关闭')} onFailure={message => complete(false, message)} />}
      <div className="browser-buttons">{!finished && <button className="outline" onClick={() => setStopping(true)}>停止后续更新</button>}<button className="outline" onClick={onClose}>{finished ? '完成' : '关闭'}</button></div>
    </div>
  </Dialog>;
}
