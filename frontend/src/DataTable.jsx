import Highlight from './Highlight';
import React from 'react';

export default function DataTable({ label, columns, className = '', empty, children, stickyActions = true, searchQuery = '' }) {
  return <div className="table-wrap data-table-wrap" role="region" aria-label={label} tabIndex={0}>
    <table className={'data-table ' + className} aria-label={label}>
      <thead><tr>{columns.map((column, index) => <th scope="col" className={stickyActions && index === columns.length - 1 ? 'table-actions' : undefined} key={column}>{column}</th>)}</tr></thead>
      <tbody>{empty ? <tr><td colSpan={columns.length} className="table-empty">{empty}</td></tr> : <Highlight query={searchQuery}>{children}</Highlight>}</tbody>
    </table>
  </div>;
}
