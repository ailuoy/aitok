import React, { useEffect, useRef, useState } from 'react';
import { ArrowDown, ArrowUp, ImagePlus, Plus, Trash2 } from 'lucide-react';

import ImagePreview from './ImagePreview';
import { readImageFile, imagePattern } from './imageFiles';

const maxDocumentBytes = 8 * 1024 * 1024;
const encode = blocks => JSON.stringify({ format: 'aitok-evidence-v1', blocks: blocks.map(({ id, ...block }) => block) });
function parse(value) {
  try {
    const document = JSON.parse(value);
    if (document.format === 'aitok-evidence-v1' && Array.isArray(document.blocks)) return document.blocks;
  } catch {}
  return value ? [{ type: 'text', text: value }] : [];
}
export const hasEvidence = value => parse(value).some(block => block.type === 'text' ? block.text?.trim() : block.type === 'image' && imagePattern.test(block.src));

export function EvidenceView({ value }) {
  return <div className="evidence-view">{parse(value).map((block, index) => block.type === 'text' ? <p key={index}>{block.text}</p> : block.type === 'image' && imagePattern.test(block.src) ? <figure key={index}><ImagePreview src={block.src} alt={block.caption || '凭据图片'} />{block.caption && <figcaption>{block.caption}</figcaption>}</figure> : null)}</div>;
}

export default function EvidenceEditor({ value = '', onChange, onBusyChange, disabled = false }) {
  const [blocks, setBlocks] = useState(() => (parse(value).length ? parse(value) : [{ type: 'text', text: '' }]).map(block => ({ ...block, id: crypto.randomUUID() })));
  const [error, setError] = useState(''), [reading, setReading] = useState(false), [removing, setRemoving] = useState(null);
  const current = useRef(blocks), input = useRef(null), mounted = useRef(true), importing = useRef(false);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  const locked = disabled || reading;
  function update(next) {
    if (next.reduce((total, block) => total + (block.type === 'text' ? Array.from(block.text || '').length : 0), 0) > 4000) { setError('凭据文字合计不能超过 4000 字'); return; }
    const encoded = encode(next);
    if (new TextEncoder().encode(encoded).length > maxDocumentBytes) { setError('凭据总大小不能超过 8 MB'); return; }
    current.current = next; setBlocks(next); onChange(encoded); setError('');
  }
  async function addImages(files) {
    if (!files.length || disabled || importing.current) return;
    if (current.current.length + files.length > 40) { setError('每份凭据最多 40 个内容块'); return; }
    if (current.current.filter(b => b.type === 'image').length + files.length > 6) { setError('每份凭据最多添加 6 张图片'); return; }
    importing.current = true; setReading(true); onBusyChange?.(true); setError('');
    try {
      const additions = await Promise.all(files.map(async file => ({ id: crypto.randomUUID(), type: 'image', src: await readImageFile(file), caption: '' })));
      if (mounted.current) update([...current.current, ...additions]);
    } catch (e) { if (mounted.current) setError(e.message); }
    finally { importing.current = false; if (mounted.current) { setReading(false); onBusyChange?.(false); } }
  }
  function move(index, offset) {
    const next = [...blocks]; [next[index], next[index + offset]] = [next[index + offset], next[index]]; update(next);
  }
  return <div className="evidence-editor" role="group" aria-label="图文凭据" onPaste={event => {
    const files = Array.from(event.clipboardData?.files || []); if (files.length) { event.preventDefault(); void addImages(files); }
  }} onDragOver={event => { if (event.dataTransfer.types.includes('Files')) event.preventDefault(); }} onDrop={event => {
    if (event.dataTransfer.files.length) { event.preventDefault(); void addImages(Array.from(event.dataTransfer.files)); }
  }}>
    <div className="evidence-toolbar"><span>核对依据 / 凭证</span><button type="button" className="outline small" disabled={locked || blocks.length >= 40} onClick={() => update([...blocks, { id: crypto.randomUUID(), type: 'text', text: '' }])}><Plus size={14} />文字</button><button type="button" className="outline small" disabled={locked} onClick={() => input.current.click()}><ImagePlus size={14} />图片</button><input ref={input} type="file" hidden multiple accept=".jpg,.jpeg,.png,.gif,image/png,image/jpeg,image/jpg,image/gif" onChange={event => { void addImages(Array.from(event.target.files)); event.target.value = ''; }} /></div>
    {blocks.map((block, index) => <div className="evidence-block" key={block.id}>
      <div className="evidence-block-actions"><span>{block.type === 'text' ? '文字' : '图片'} {index + 1}</span><button type="button" className="icon-btn" aria-label={`上移第 ${index + 1} 块`} disabled={locked || index === 0} onClick={() => move(index, -1)}><ArrowUp size={14} /></button><button type="button" className="icon-btn" aria-label={`下移第 ${index + 1} 块`} disabled={locked || index === blocks.length - 1} onClick={() => move(index, 1)}><ArrowDown size={14} /></button><button type="button" className="icon-btn" aria-label={`删除第 ${index + 1} 块`} disabled={locked} onClick={() => setRemoving(block.id)}><Trash2 size={14} /></button></div>
      {block.type === 'text' ? <textarea aria-label={`凭据文字 ${index + 1}`} placeholder="填写核对说明，可粘贴或拖入图片" value={block.text} maxLength={4000} disabled={locked} onChange={event => update(blocks.map(b => b.id === block.id ? { ...b, text: event.target.value } : b))} /> : <><ImagePreview src={block.src} alt={block.caption || '凭据图片预览'} /><input aria-label={`图片说明 ${index + 1}`} placeholder="图片说明（可选）" maxLength={200} value={block.caption} disabled={locked} onChange={event => update(blocks.map(b => b.id === block.id ? { ...b, caption: event.target.value } : b))} /></>}
      {removing === block.id && <div className="browser-buttons"><span>删除此内容？</span><button type="button" className="outline small" disabled={locked} onClick={() => { update(blocks.filter(b => b.id !== block.id)); setRemoving(null); }}>确认删除</button><button type="button" className="outline small" onClick={() => setRemoving(null)}>取消</button></div>}
    </div>)}
    {reading && <p className="muted" role="status">正在读取图片…</p>}{error && <p className="error" role="alert">{error}</p>}
  </div>;
}
