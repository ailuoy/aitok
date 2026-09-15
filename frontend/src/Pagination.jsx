import React, { useEffect } from 'react';
import Select from './Select';

export default function Pagination({ page, pageSize, total = 0, onPageChange, onPageSizeChange, disabled = false }) {
  const pages = Math.max(1, Math.ceil(total / pageSize));
  useEffect(() => { if (!disabled && page > pages) onPageChange(pages); }, [disabled, page, pages, onPageChange]);
  const numbers = [...new Set([1, pages, ...Array.from({ length: 5 }, (_, i) => page + i - 2)])].filter(n => n >= 1 && n <= pages).sort((a, b) => a - b);
  return <nav className="address-pagination" aria-label="列表分页">
    <span className="muted">共 {total} 条 · 第 {page} / {pages} 页</span>
    <Select label="每页条数" value={pageSize} disabled={disabled} options={[20, 50, 100].map(value => ({ value, label: `${value} 条 / 页` }))} onChange={value => { onPageChange(1); onPageSizeChange(Number(value)); }} />
    <div className="pagination-pages">
      <button type="button" className="outline small" disabled={disabled || page <= 1} onClick={() => onPageChange(page - 1)}>上一页</button>
      {numbers.map((n, i) => <React.Fragment key={n}>{i > 0 && n - numbers[i - 1] > 1 && <span className="pagination-gap">…</span>}<button type="button" className="outline small pagination-number" aria-label={`第 ${n} 页`} aria-current={n === page ? 'page' : undefined} disabled={disabled} onClick={() => onPageChange(n)}>{n}</button></React.Fragment>)}
      <button type="button" className="outline small" disabled={disabled || page >= pages} onClick={() => onPageChange(page + 1)}>下一页</button>
    </div>
  </nav>;
}
