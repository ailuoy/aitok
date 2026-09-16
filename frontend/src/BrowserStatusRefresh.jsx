import React, { useEffect, useId, useState } from 'react';
import { RefreshCw } from 'lucide-react';

export default function BrowserStatusRefresh({ children, disabled, onClick, className = '', reminderKey }) {
  const [remind, setRemind] = useState(true);
  const hintID = useId();
  useEffect(() => {
    const remindOnReturn = () => { if (!document.hidden) setRemind(true); };
    setRemind(true);
    window.addEventListener('focus', remindOnReturn);
    document.addEventListener('visibilitychange', remindOnReturn);
    return () => {
      window.removeEventListener('focus', remindOnReturn);
      document.removeEventListener('visibilitychange', remindOnReturn);
    };
  }, [reminderKey]);

  return <span className="browser-status-refresh">
    <button type="button" className={`outline browser-status-refresh-button ${className}${remind ? ' needs-refresh' : ''}`} disabled={disabled} aria-describedby={hintID} title="浏览器状态需手动刷新；登录或关闭窗口后，点击更新状态。" onClick={event => { setRemind(false); onClick(event); }}>
      <RefreshCw size={15} aria-hidden="true" />{children}
    </button>
    <small id={hintID} className={remind ? 'refresh-reminder' : ''}>{remind ? '点击更新状态' : '状态需手动刷新'}</small>
  </span>;
}
