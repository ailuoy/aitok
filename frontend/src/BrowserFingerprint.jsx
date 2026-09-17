import React from 'react';
import { formatUTC8 } from './time';

// 与 native-noise-v1 的实现对应；未识别的模式不推断其指纹策略。
const nativeNoiseSettings = [
  ['Canvas / OffscreenCanvas', '按账号固定的像素微扰'],
  ['WebGL 像素读取', '按账号固定的像素微扰'],
  ['ClientRects 布局测量', '按账号固定的坐标微扰'],
  ['User-Agent / UA-CH', '浏览器原生值'],
  ['GPU / CPU / 内存', '浏览器原生值'],
  ['音频指纹', '浏览器原生值'],
  ['语言 / 时区', '浏览器原生值'],
  ['屏幕 / 分辨率', '浏览器原生值'],
];

export default function BrowserFingerprint({ status }) {
  const fingerprint = status?.fingerprint;
  const nativeNoise = fingerprint?.mode === 'native-noise-v1';
  const saved = status?.state === 'closed';
  const rows = fingerprint ? [
    ['指纹编号', fingerprint.id || '未提供'],
    ['指纹代数', fingerprint.generation ? `第 ${fingerprint.generation} 代` : '未提供'],
    ['生成时间（UTC+8）', Number.isFinite(Date.parse(fingerprint.created_at)) ? formatUTC8(fingerprint.created_at) : '未提供'],
    ['指纹模式', nativeNoise ? '原生参数 + 稳定微扰' : fingerprint.mode || '未提供'],
    ...(nativeNoise ? nativeNoiseSettings : []),
  ] : [];

  return <details className="browser-fingerprint" open>
    <summary>浏览器指纹配置{fingerprint && <span>{saved ? '已保存 · 下次打开生效' : '当前账号'}</span>}</summary>
    {fingerprint ? <>
      <dl className="browser-fingerprint-grid">{rows.map(([label, value]) => <div key={label}><dt>{label}</dt><dd>{value}</dd></div>)}</dl>
      <p className="muted">{nativeNoise ? '原生值沿用账号浏览器自身配置，未作改写；此处展示配置策略，不是实时检测值。' : '当前服务未提供可识别的配置策略，请更新并重启浏览器服务后重试。'}</p>
    </> : <p className="muted">{!status ? '打开浏览器或刷新状态后显示指纹配置。' : saved ? '此账号尚无指纹配置，首次打开浏览器时自动生成。' : '当前服务未返回指纹配置，请刷新状态；仍未显示时，请更新并重启浏览器服务。'}</p>}
  </details>;
}
