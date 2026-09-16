import Highlight from './Highlight';
import React from 'react';
import { ArrowDown, ArrowUp, ArrowUpDown } from 'lucide-react';

export default function DataTable({ label, columns, className = '', empty, children, stickyActions = true, searchQuery = '', sort, onSort }) {
  return <div className="table-wrap data-table-wrap" role="region" aria-label={label} tabIndex={0}>
    <table className={'data-table ' + className} aria-label={label}>
      <thead><tr>{columns.map((column, index) => {
        const { label: title, key, disabled } = typeof column === 'string' ? { label: column } : column;
        const selected = key && sort?.key === key;
        const Icon = selected ? sort.direction === 'asc' ? ArrowUp : ArrowDown : ArrowUpDown;
        return <th scope="col" aria-sort={key ? selected ? sort.direction === 'asc' ? 'ascending' : 'descending' : 'none' : undefined} className={stickyActions && index === columns.length - 1 ? 'table-actions' : undefined} key={key || title}>
          {key && onSort ? <button type="button" className="table-sort" disabled={disabled} onClick={() => onSort(key)} aria-label={`${title}：${selected && sort.direction === 'asc' ? '降序' : '升序'}排序`}>{title}<Icon size={14} aria-hidden="true" /></button> : title}
        </th>;
      })}</tr></thead>
      <tbody>{empty ? <tr><td colSpan={columns.length} className="table-empty">{empty}</td></tr> : <Highlight query={searchQuery}>{children}</Highlight>}</tbody>
    </table>
  </div>;
}
