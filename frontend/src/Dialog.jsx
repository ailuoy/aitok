import React, { useEffect, useId, useRef } from 'react';
import { X } from 'lucide-react';

export default function Dialog({ title, titleActions, onClose, children, className = '', accountID }) {
  const ref = useRef(null);
  const titleID = useId();
  useEffect(() => { const dialog = ref.current; dialog.showModal(); return () => dialog.close(); }, []);
  return <dialog ref={ref} data-audit-account-id={accountID} className={'workspace-dialog ' + className} aria-labelledby={titleID} onKeyDown={event => {
    if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); onClose(); }
  }} onCancel={event => { event.preventDefault(); event.stopPropagation(); if (event.target === event.currentTarget) onClose(); }}>
    <div className={'workspace-dialog-heading' + (titleActions ? ' has-actions' : '')}><h2 id={titleID}>{title}</h2>{titleActions}<button type="button" className="close" aria-label="关闭" onClick={onClose}><X size={20} /></button></div>
    <div className="workspace-dialog-body">{children}</div>
  </dialog>;
}
