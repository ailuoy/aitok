import React from 'react';

// 按字面文本匹配，不将搜索内容作为正则表达式或 HTML 执行。
export default function Highlight({ children, query }) {
  const needle = query?.trim().toLocaleLowerCase();
  if (!needle) return children;
  function visit(node) {
    if (typeof node === 'string' || typeof node === 'number') {
      const text = String(node), lower = text.toLocaleLowerCase(), parts = [];
      let start = 0, at;
      while ((at = lower.indexOf(needle, start)) !== -1) {
        parts.push(text.slice(start, at), <mark className="search-highlight" key={at}>{text.slice(at, at + needle.length)}</mark>);
        start = at + needle.length;
      }
      return parts.length ? [...parts, text.slice(start)] : node;
    }
    if (!React.isValidElement(node) || typeof node.type !== 'string' || ['button', 'svg', 'input', 'textarea'].includes(node.type)) return node;
    return React.cloneElement(node, {}, React.Children.map(node.props.children, visit));
  }
  return <>{React.Children.map(children, visit)}</>;
}
