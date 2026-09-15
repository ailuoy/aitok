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
  const id = useId(), overlay = useRef(null);
  const [error, setError] = useState('');
  const highlighted = useMemo(() => highlight(value), [value]);
  function format() {
    try {
      const formatted = JSON.stringify(JSON.parse(value), null, 2);
      if (formatted.length > 240000) throw new Error('格式化后的 JSON 超过 240 KB');
      onChange(formatted); setError('');
    } catch { setError('JSON 格式不正确或内容过大，请检查后重试'); }
  }
  return <div className="json-field"><div className="json-toolbar"><label htmlFor={id}>Session JSON</label><button type="button" className="text-btn" onClick={format}>格式化 JSON</button></div>
    <div className="json-editor"><pre ref={overlay} aria-hidden="true">{highlighted}</pre><textarea id={id} name="session_json" required maxLength={240000} value={value} onChange={event => { setError(''); onChange(event.target.value); }} onScroll={event => { overlay.current.scrollTop = event.target.scrollTop; overlay.current.scrollLeft = event.target.scrollLeft; }} placeholder={'{\n  "user": { "email": "you@example.com" },\n  "accessToken": "…"\n}'} autoComplete="off" autoCapitalize="off" spellCheck={false} autoFocus /></div>
    {error && <p className="error" role="alert">{error}</p>}
  </div>;
}
