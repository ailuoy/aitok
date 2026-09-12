import React from 'react';
import { Sparkles, ShieldCheck, CalendarDays, Coins, MessageSquare, ArrowRight, Check } from 'lucide-react';
import { Link } from './router';

const features = [
  [MessageSquare, 'ChatGPT 账号管理', '添加账号名称、邮箱及 Session JSON，在工作台集中查看账号。'],
  [CalendarDays, '会员日期与续订', '查看每个账号的续订日期，使用钱包代币延长 AiTok 平台会员有效期。'],
  [Coins, '钱包与交易记录', '通过 Stripe 按数量充值代币，查阅充值、账号扣款和余额变动。'],
];
const security = [
  [ShieldCheck, 'Session 加密保存', 'Session JSON 在服务端加密存储，账号列表不返回原始凭证。'],
  [MessageSquare, '账号访问权限', '普通用户管理自己的账号；超级管理员可查看账号信息并设置续订日期。'],
  [Coins, '支付确认与记录', '支付由 Stripe 处理，服务端核实支付结果后入账。充值和续订扣款均保留流水。'],
];

function Cards({ items }) {
  return <section className="feature-grid">{items.map(([Icon, title, description]) => <article className="feature" key={title}><div className="icon"><Icon size={20} /></div><h2>{title}</h2><p>{description}</p></article>)}</section>;
}

export default function PublicPage({ path, signedIn }) {
  const start = signedIn ? '/accounts' : '/register';
  if (!['/', '/features', '/plans', '/security'].includes(path)) return <main className="hero"><h1>页面不存在</h1><p>这个地址暂时没有对应页面。</p><Link className="primary" to="/">返回首页</Link></main>;
  if (path === '/plans') return <main className="public-page">
    <section className="hero"><span className="pill"><Coins size={14} /> 按需充值</span><h1>为你的账号<br /><em>续上每一份灵感。</em></h1><p>通过 Stripe 充值钱包代币，再为指定 ChatGPT 账号续订 AiTok 平台会员。</p></section>
    <section className="plans"><article className="plan subscription-plan"><h2>ChatGPT 账号会员</h2><p>充值前可在钱包查看当前兑换比例、续订费用及续订时长。</p>{['支持输入充值数量，付款前展示总额', '续订前确认账号、代币费用和余额', '未过期账号从原续订日期延长', '充值记录、账号扣款记录与全部流水可查'].map(text => <div className="check" key={text}><Check size={16} />{text}</div>)}<p className="notice">当前续订更新 AiTok 平台会员日期，尚未接入 OpenAI 官方订阅购买或代充服务。</p><Link className="primary full" to={signedIn ? '/wallet' : '/login?next=%2Fwallet'}>{signedIn ? '查看钱包与续订费用' : '登录查看充值与续订费用'}<ArrowRight size={16} /></Link></article></section>
  </main>;
  if (path === '/security') return <main className="public-page"><section className="hero"><span className="pill"><ShieldCheck size={14} /> 安全与权限</span><h1>清晰的权限，<br /><em>可查的交易。</em></h1><p>了解账号凭证如何保存，以及钱包支付如何确认。</p></section><Cards items={security} /><div className="page-cta"><Link className="primary" to={start}>进入账号管理 <ArrowRight size={16} /></Link></div></main>;
  return <main className="public-page"><section className="hero"><span className="pill"><Sparkles size={14} /> ChatGPT 账号管理平台</span><h1>{path === '/features' ? <>账号、续订、钱包，<br /><em>一个工作台管理。</em></> : <>让你的 AI 工作流<br /><em>更快、更自由。</em></>}</h1><p>集中保存 ChatGPT 账号，查看会员日期，用钱包代币续订并追踪每笔交易。</p><div className="hero-actions"><Link className="primary" to={start}>{signedIn ? '进入工作台' : '立即开始'} <ArrowRight size={17} /></Link><Link className="outline" to="/plans">查看套餐</Link></div></section><Cards items={features} /></main>;
}
