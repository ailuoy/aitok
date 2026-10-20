import React, { useState, useEffect, useRef } from 'react';
import { Sparkles, ArrowRight } from 'lucide-react';
import { request } from './api';

export default function Auth({ mode, onSuccess, onBack, onModeChange }) {
  const [tab, setTab] = useState(mode === 'signup' ? 'signup' : 'login');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [code, setCode] = useState('');
  const [challenge, setChallenge] = useState('');
  const [totpCode, setTOTPCode] = useState('');
  const [forgot, setForgot] = useState(false);
  const [msg, setMsg] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const [sending, setSending] = useState(false);
  const [cooldown, setCooldown] = useState(0);
  const emailInput = useRef(null);
  useEffect(() => {
    if (!cooldown) return;
    const timer = setTimeout(() => setCooldown(value => value - 1), 1000);
    return () => clearTimeout(timer);
  }, [cooldown]);
  function switchTab(value, reset = false) {
    setError(''); setMsg(''); setCode(''); setPassword(''); setChallenge(''); setTOTPCode(''); setForgot(reset);
    if ((value === 'signup') !== (mode === 'signup')) onModeChange(value);
    else setTab(value);
  }
  async function send() {
    if (sending || cooldown || !emailInput.current.reportValidity()) return;
    setSending(true); setError(''); setMsg('');
    try {
      await request('/send-code', null, { method: 'POST', body: { email: email.trim(), purpose: forgot ? 'reset' : 'login' } });
      setMsg('验证码已发送，请查收邮箱'); setCooldown(60);
    } catch (error) { setError(error.message); } finally { setSending(false); }
  }
  async function submit(event) {
    event.preventDefault(); if (loading) return;
    setLoading(true); setError(''); setMsg('');
    try {
      let url = '/login', body = { email: email.trim(), password };
      if (challenge) body = { challenge, totp_code: totpCode };
      else if (forgot) { url = '/reset-password'; body = { email: email.trim(), code, password }; }
      else if (tab === 'code') { url = '/login-code'; body = { email: email.trim(), code }; }
      else if (tab === 'signup') url = '/register';
      const data = await request(url, null, { method: 'POST', body });
      if (data.two_factor_required) { setChallenge(data.challenge); setPassword(''); setCode(''); }
      else if (data.token) onSuccess(data.token);
      else { setMsg('密码已重置，请使用新密码登录'); setForgot(false); setTab('login'); setPassword(''); setCode(''); }
    } catch (error) {
      setError(error.message);
      if (challenge && error.status === 401) setChallenge('');
    } finally { setLoading(false); setTOTPCode(''); }
  }
  return <main className="auth-wrap"><section className="auth-card">
    <button className="back" onClick={onBack}>← 返回首页</button><div className="auth-logo"><Sparkles size={20} /></div>
    <h2>{challenge ? '两步验证' : forgot ? '找回密码' : tab === 'signup' ? '创建你的账号' : '欢迎回来'}</h2>
    <p>{challenge ? '请输入验证器中的六位验证码完成登录' : forgot ? '通过邮箱验证码重置密码' : tab === 'code' ? '验证码登录，无需密码' : '登录后管理你的 ChatGPT 订阅'}</p>
    {!challenge && !forgot && tab !== 'signup' && <div className="auth-tabs">{[['login', '密码登录'], ['code', '验证码登录']].map(([value, label]) => <button key={value} disabled={loading || sending} className={tab === value ? 'active' : ''} onClick={() => switchTab(value)}>{label}</button>)}</div>}
    <form onSubmit={submit}>
      {challenge ? <label>验证器验证码<input aria-label="验证器验证码" inputMode="numeric" autoComplete="one-time-code" pattern="[0-9]{6}" maxLength={6} value={totpCode} onChange={event => setTOTPCode(event.target.value.replace(/\D/g, ''))} placeholder="6 位验证码" required autoFocus disabled={loading} /></label> : <>
      <label>{tab === 'login' && !forgot ? '用户名或邮箱' : '邮箱地址'}<input ref={emailInput} type={tab === 'login' && !forgot ? 'text' : 'email'} autoComplete="username" required placeholder={tab === 'login' && !forgot ? '用户名或邮箱' : 'you@example.com'} value={email} onChange={event => setEmail(event.target.value)} /></label>
      {(tab === 'code' || forgot) && <label>邮箱验证码<div className="code-row"><input required inputMode="numeric" autoComplete="one-time-code" pattern="[0-9]{6}" maxLength={6} placeholder="6 位验证码" value={code} onChange={event => setCode(event.target.value)} /><button type="button" className="outline code-btn" disabled={sending || loading || cooldown > 0} onClick={send}>{sending ? '发送中…' : cooldown ? `${cooldown} 秒后重发` : '发送验证码'}</button></div></label>}
      {(tab === 'login' || tab === 'signup' || forgot) && <label>{forgot ? '新密码' : '密码'}<input type="password" autoComplete={forgot || tab === 'signup' ? 'new-password' : 'current-password'} required minLength={tab === 'login' && !forgot ? 1 : 8} placeholder={tab === 'login' && !forgot ? '请输入密码' : '8 至 72 字节密码'} value={password} onChange={event => setPassword(event.target.value)} /></label>}
      </>}
      {msg && <div className="success" role="status">{msg}</div>}{error && <div className="error" role="alert">{error}</div>}
      <button className="primary full" disabled={loading || sending}>{loading ? '处理中…' : challenge ? '验证并登录' : forgot ? '重置密码' : tab === 'code' ? '验证码登录' : tab === 'signup' ? '注册并开始' : '登录'} <ArrowRight size={16} /></button>
    </form>
    {!challenge && !forgot && tab === 'login' && <button className="forgot-link" disabled={loading || sending} onClick={() => switchTab('login', true)}>忘记密码？</button>}
    <div className="switch">{challenge ? '重新验证登录身份？' : forgot ? '想起密码了？' : tab === 'signup' ? '已有账号？' : '还没有账号？'} <button disabled={loading || sending} onClick={() => switchTab(challenge || forgot || tab === 'signup' ? 'login' : 'signup')}>{challenge || forgot ? '返回登录' : tab === 'signup' ? '立即登录' : '免费注册'}</button></div>
  </section></main>;
}
