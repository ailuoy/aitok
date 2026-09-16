import React, { useEffect, useRef, useState } from 'react';
import ImagePreview from './ImagePreview';
import { readImageFile } from './imageFiles';

export default function ImageUpload({ value, onChange, onBusyChange, disabled = false, label }) {
  const input = useRef(null), mounted = useRef(true), importing = useRef(false);
  const [reading, setReading] = useState(false), [error, setError] = useState(''), [removing, setRemoving] = useState(false);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  async function upload(files) {
    if (!files.length || disabled || importing.current) return;
    if (files.length !== 1) { setError('请选择一张二维码截图'); return; }
    importing.current = true; setReading(true); onBusyChange?.(true); setError('');
    try { const src = await readImageFile(files[0]); if (mounted.current) { onChange(src); setRemoving(false); } }
    catch (e) { if (mounted.current) setError(e.message); }
    finally { importing.current = false; if (mounted.current) { setReading(false); onBusyChange?.(false); } }
  }
  return <div className="image-upload" role="group" aria-label={label} tabIndex={0} onPaste={event => {
    const files = Array.from(event.clipboardData?.files || []); if (files.length) { event.preventDefault(); void upload(files); }
  }} onDragOver={event => { if (event.dataTransfer.types.includes('Files')) event.preventDefault(); }} onDrop={event => {
    if (event.dataTransfer.files.length) { event.preventDefault(); void upload(Array.from(event.dataTransfer.files)); }
  }}>
    <span>{label}</span><div className="browser-buttons">{value && <ImagePreview src={value} alt={label} />}
      <button type="button" className="outline small" disabled={disabled || reading} onClick={() => input.current.click()}>{reading ? '读取中…' : value ? '更换截图' : '上传截图'}</button>
      {value && <button type="button" className="outline small" disabled={disabled || reading} onClick={() => setRemoving(true)}>移除截图</button>}
      <input ref={input} type="file" hidden accept=".jpg,.jpeg,.png,.gif,image/png,image/jpeg,image/jpg,image/gif" onChange={event => { void upload(Array.from(event.target.files)); event.target.value = ''; }} />
      <span className="muted">可粘贴或拖入图片</span>
    </div>
    {removing && <div className="browser-buttons"><span>确认移除二维码截图？</span><button type="button" className="outline small" disabled={disabled || reading} onClick={() => { onChange(''); setRemoving(false); }}>确认移除</button><button type="button" className="outline small" onClick={() => setRemoving(false)}>取消</button></div>}
    {error && <p className="error" role="alert">{error}</p>}
  </div>;
}
