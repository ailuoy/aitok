// 沿用历史套餐标识，统一展示当前产品名称。
export const packagePlans = [{ value: 'plus', label: 'Plus' }, { value: 'pro_5x', label: '5X' }, { value: 'pro_20x', label: '10X' }];

export function formatAccountPlan(plan) {
  const label = packagePlans.find(item => item.value === String(plan || '').toLowerCase())?.label;
  return label ? label === 'Plus' ? 'PLUS' : `PRO-${label}` : '未设置';
}

export function formatPackageName(name) {
  return String(name || '').replace(/\b20x\b/gi, '10X');
}
