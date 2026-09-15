import React, { useEffect, useId, useRef } from 'react';
import { X } from 'lucide-react';

export default function Dialog({ title, onClose, children }) {
  const ref = useRef(null);
  const titleID = useId();
  useEffect(() => { const dialog = ref.current; dialog.showModal(); return () => dialog.close(); }, []);
  return <dialog ref={ref} className="workspace-dialog" aria-labelledby={titleID} onCancel={event => { event.preventDefault(); onClose(); }}>
    <button type="button" className="close" aria-label="关闭" onClick={onClose}><X size={20} /></button><h2 id={titleID}>{title}</h2>{children}
  </dialog>;
}
