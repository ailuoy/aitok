import React, { useEffect, useState } from 'react';
import { Monitor, Moon, Sun } from 'lucide-react';

const choices = [['auto', '自动', Monitor], ['dark', '黑色', Moon], ['light', '白色', Sun]];
export default function ThemeControl() {
  const [theme, setTheme] = useState(() => { try { const value = localStorage.getItem('aitok-theme'); return choices.some(choice => choice[0] === value) ? value : 'auto'; } catch { return 'auto'; } });
  useEffect(() => {
    const media = window.matchMedia('(prefers-color-scheme: dark)');
    const apply = () => { document.documentElement.dataset.theme = theme === 'auto' ? media.matches ? 'dark' : 'light' : theme; };
    apply();
    try { localStorage.setItem('aitok-theme', theme); } catch { /* 隐私模式下仍可切换主题。 */ }
    media.addEventListener('change', apply);
    return () => media.removeEventListener('change', apply);
  }, [theme]);
  return <div className="theme-control" role="group" aria-label="主题">{choices.map(([value, label, Icon]) => <button key={value} type="button" aria-pressed={theme === value} title={label + '主题'} onClick={() => setTheme(value)}><Icon size={14} /><span>{label}</span></button>)}</div>;
}
