import React, { useEffect, useId, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { Check, ChevronDown, Search } from 'lucide-react';

// 统一下拉菜单：搜索、键盘选择、焦点恢复，弹层兼容原生 dialog。
export default function Select({ value, onChange, options, label, disabled = false, name, placeholder = '请选择', searchPlaceholder = '输入关键词过滤…', onCreate, createLabel = '新建' }) {
  const id = useId();
  const button = useRef(null);
  const popup = useRef(null);
  const search = useRef(null);
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  const [active, setActive] = useState(0);
  const [position, setPosition] = useState({});
  const selected = options.find(option => String(option.value) === String(value ?? ''));
  const filtered = options.filter(option => option.label.toLocaleLowerCase().includes(query.toLocaleLowerCase()));
  const close = (restore = false) => { setOpen(false); if (restore) button.current?.focus(); };
  const choose = option => { if (!option || option.disabled) return; onChange(String(option.value)); close(true); };
  useEffect(() => {
    if (!open) return;
    const rect = button.current.getBoundingClientRect();
    const height = Math.min(320, window.innerHeight - 24);
    const below = window.innerHeight - rect.bottom - 12;
    setPosition({ left: Math.max(12, Math.min(rect.left, window.innerWidth - Math.max(rect.width, 240) - 12)), width: Math.min(Math.max(rect.width, 240), window.innerWidth - 24), maxHeight: height, ...(below >= Math.min(height, 200) ? { top: rect.bottom + 6, maxHeight: Math.min(height, below) } : { bottom: window.innerHeight - rect.top + 6, maxHeight: Math.min(height, rect.top - 12) }) });
    search.current?.focus();
    const outside = event => { if (!popup.current?.contains(event.target) && !button.current?.contains(event.target)) close(); };
    const scroll = event => { if (!popup.current?.contains(event.target)) close(); };
    document.addEventListener('pointerdown', outside);
    window.addEventListener('resize', scroll);
    window.addEventListener('scroll', scroll, true);
    return () => { document.removeEventListener('pointerdown', outside); window.removeEventListener('resize', scroll); window.removeEventListener('scroll', scroll, true); };
  }, [open]);
  useEffect(() => { popup.current?.querySelector('[data-active="true"]')?.scrollIntoView({ block: 'nearest' }); }, [active]);
  useEffect(() => { if (disabled) setOpen(false); }, [disabled]);
  const keydown = event => {
    if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); close(true); }
    else if (event.key === 'Tab') { if (!onCreate || (event.target === search.current ? event.shiftKey : !event.shiftKey)) close(); }
    else if (event.target.closest('.select-create')) return;
    else if (['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) {
      event.preventDefault();
      setActive(index => event.key === 'Home' ? 0 : event.key === 'End' ? Math.max(0, filtered.length - 1) : Math.max(0, Math.min(filtered.length - 1, index + (event.key === 'ArrowDown' ? 1 : -1))));
    } else if (event.key === 'Enter') { event.preventDefault(); choose(filtered[active]); }
  };
  return <div className="custom-select">
    {name && <input type="hidden" name={name} value={value ?? ''} />}
    <button ref={button} type="button" className="select-trigger" role="combobox" aria-label={label} aria-haspopup="listbox" aria-expanded={open} aria-controls={open ? id : undefined} disabled={disabled} onClick={() => { setQuery(''); setActive(0); setOpen(!open); }} onKeyDown={event => { if (['ArrowDown', 'ArrowUp'].includes(event.key)) { event.preventDefault(); setQuery(''); setActive(0); setOpen(true); } }}><span>{selected?.label || placeholder}</span><ChevronDown size={15} /></button>
    {open && createPortal(<div className="select-popup" ref={popup} style={position} onKeyDown={keydown}>
      <div className="select-search"><Search size={15} /><input ref={search} aria-label={'过滤' + label} role="combobox" aria-autocomplete="list" aria-controls={id} aria-expanded="true" aria-activedescendant={filtered[active] ? id + '-' + active : undefined} placeholder={searchPlaceholder} value={query} onChange={event => { setQuery(event.target.value); setActive(0); }} /></div>
      <div id={id} role="listbox" aria-label={label} className="select-options">{filtered.map((option, index) => <div id={id + '-' + index} key={option.value} role="option" aria-selected={String(option.value) === String(value ?? '')} aria-disabled={Boolean(option.disabled)} data-active={index === active} className="select-option" onPointerMove={() => setActive(index)} onMouseDown={event => event.preventDefault()} onClick={() => choose(option)}><span>{option.label}</span>{String(option.value) === String(value ?? '') && <Check size={15} />}</div>)}{!filtered.length && <p className="select-empty">没有匹配项</p>}</div>
      {onCreate && <button type="button" className="select-create" onClick={() => { close(true); onCreate(query.trim()); }}>＋ {createLabel}</button>}
    </div>, button.current?.closest('dialog') || document.body)}
  </div>;
}
