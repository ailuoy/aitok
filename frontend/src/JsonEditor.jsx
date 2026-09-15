import React, { useId, useMemo, useRef, useState } from 'react';

function highlight(text) {
  const pattern = /"(?:[^"\\]|\\.)*"(?:\s*(?=:))?|-?\b\d+(?:\.\d+)?(?:[eE][+-]?\d+)?\b|\b(?:true|false|null)\b/g;
  const result = []; let last = 0;
  for (const match of text.matchAll(pattern)) {
    if (match.index > last) result.push(text.slice(last, match.index));
    const value = match[0];
    const type = value.startsWith('"') ? /^\s*:/.test(text.slice(match.index + value.length)) ? 'key' : 'string' : ['true', 'false', 'null'].includes(value) ? 'literal' : 'number';
    result.push(<span className={'json-' + type} key={match.index}>{value}</span>);
    last = match.index + value.length;
  }
  result.push(text.slice(last) + '\n');
  return result;
}

export default function JsonEditor({ value, onChange }) {
  const id = useId(), overlay = useRef(null), fileInput = useRef(null), readVersion = useRef(0);
  const [error, setError] = useState('');
  const [dragging, setDragging] = useState(false);
  const highlighted = useMemo(() => highlight(value), [value]);
  async function importFile(files) {
    const version = ++readVersion.current;
    if (files.length !== 1) { setError('请一次拖入一个 JSON 文件'); return; }
    const file = files[0];
    if (!/\.json$/i.test(file.name)) { setError('请选择 .json 文件'); return; }
    if (file.size > 240000) { setError('JSON 文件不能超过 240 KB'); return; }
    try {
      const raw = (await file.text()).replace(/^\uFEFF/, '');
      if (version !== readVersion.current) return;
      const session = JSON.parse(raw);
      if (!session || typeof session !== 'object' || Array.isArray(session) || !Object.keys(session).length) {
        setError('JSON 文件必须包含单个非空 Session 对象'); return;
      }
      onChange(raw); setError('');
    } catch { if (version === readVersion.current) setError('无法读取有效 JSON，请检查文件内容'); }
  }
  function format() {
    readVersion.current++;
    try {
      const formatted = JSON.stringify(JSON.parse(value), null, 2);
      if (formatted.length > 240000) throw new Error('格式化后的 JSON 超过 240 KB');
      onChange(formatted); setError('');
    } catch { setError('JSON 格式不正确或内容过大，请检查后重试'); }
  }
  return <div className={'json-field' + (dragging ? ' dragging' : '')} onDragOver={event => { if (!Array.from(event.dataTransfer.types).includes('Files')) return; event.preventDefault(); event.dataTransfer.dropEffect = 'copy'; setDragging(true); }} onDragLeave={event => { if (!event.currentTarget.contains(event.relatedTarget)) setDragging(false); }} onDrop={event => { if (!Array.from(event.dataTransfer.types).includes('Files')) return; event.preventDefault(); setDragging(false); void importFile(Array.from(event.dataTransfer.files)); }}>
    <div className="json-toolbar"><label htmlFor={id}>Session JSON</label><div className="json-tools"><input ref={fileInput} type="file" accept=".json,application/json" hidden onChange={event => { const files = Array.from(event.target.files || []); event.target.value = ''; if (files.length) void importFile(files); }} /><button type="button" className="text-btn" onClick={() => fileInput.current.click()}>选择 JSON 文件</button><button type="button" className="text-btn" onClick={format}>格式化 JSON</button></div></div>
    <div className="json-editor"><pre ref={overlay} aria-hidden="true">{highlighted}</pre><textarea id={id} name="session_json" required maxLength={240000} value={value} onChange={event => { readVersion.current++; setError(''); onChange(event.target.value); }} onScroll={event => { overlay.current.scrollTop = event.target.scrollTop; overlay.current.scrollLeft = event.target.scrollLeft; }} placeholder={'{\n  "user": { "email": "you@example.com" },\n  "accessToken": "…"\n}'} autoComplete="off" autoCapitalize="off" spellCheck={false} autoFocus /></div>
    <p className="muted json-drop-hint">{dragging ? '松开以读取 JSON 文件' : '粘贴 JSON，或拖入一个 .json 文件（最多 240 KB）'}</p>
    {error && <p className="error" role="alert">{error}</p>}
  </div>;
}
