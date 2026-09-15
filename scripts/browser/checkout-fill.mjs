export const checkoutHosts = ['chatgpt.com', 'checkout.stripe.com', 'pay.openai.com', 'pay.chatgpt.com'];
export function assistantPage(url) { try { const parsed = new URL(url); return parsed.protocol === 'https:' && checkoutHosts.includes(parsed.hostname); } catch { return false; } }

export function fillSource(card, address, cvc) {
  return `(${fillCheckout.toString()})(${JSON.stringify({ card, address, cvc })})`;
}

// 只填写已识别的收银字段，不勾选协议、不点击提交或付款按钮。
function fillCheckout({ card, address, cvc }) {
  if (location.protocol !== 'https:' || !['chatgpt.com', 'checkout.stripe.com', 'pay.openai.com', 'pay.chatgpt.com', 'js.stripe.com', 'hooks.stripe.com'].includes(location.hostname)) return [];
  const values = {
    'cc-number': card?.number, 'cc-name': card?.cardholder, 'cc-exp-month': card && String(card.exp_month).padStart(2, '0'), 'cc-exp-year': card && String(card.exp_year),
    'cc-exp': card && String(card.exp_month).padStart(2, '0') + '/' + String(card.exp_year).slice(-2), 'cc-csc': cvc,
    name: card?.cardholder, 'address-line1': address?.address_line1, 'address-line2': address?.address_line2,
    'address-level2': address?.city, 'address-level1': address?.state, 'postal-code': address?.postal_code, country: address?.country,
  };
  const names = {
    cardnumber: 'cc-number', cardname: 'cc-name', cardholder: 'cc-name', cardholdername: 'cc-name', billingname: 'name',
    cardexpiry: 'cc-exp', expiry: 'cc-exp', expmonth: 'cc-exp-month', expyear: 'cc-exp-year', cardcvc: 'cc-csc', cvc: 'cc-csc', cvv: 'cc-csc',
    billingaddressline1: 'address-line1', addressline1: 'address-line1', line1: 'address-line1', billingaddressline2: 'address-line2', addressline2: 'address-line2', line2: 'address-line2',
    billinglocality: 'address-level2', billingcity: 'address-level2', city: 'address-level2', billingadministrativearea: 'address-level1', billingstate: 'address-level1', state: 'address-level1',
    billingpostalcode: 'postal-code', postalcode: 'postal-code', zip: 'postal-code', billingcountry: 'country', country: 'country', countrycode: 'country',
  };
  const filled = [];
  for (const element of document.querySelectorAll('input,select')) {
    if (element.disabled || element.readOnly || !element.getClientRects().length || ['hidden', 'checkbox', 'radio', 'submit', 'button'].includes(element.type)) continue;
    const autocomplete = (element.autocomplete || '').split(/\s+/).at(-1);
    const key = autocomplete in values ? autocomplete : names[(element.name || element.id || '').replace(/[^a-z0-9]/gi, '').toLowerCase()];
    let value = values[key];
    if (!value) continue;
    if (element.tagName === 'SELECT') {
      const option = [...element.options].find(option => option.value.toLowerCase() === String(value).toLowerCase() || option.textContent.trim().toLowerCase() === String(value).toLowerCase());
      if (!option) continue;
      value = option.value;
    }
    const prototype = element.tagName === 'SELECT' ? HTMLSelectElement.prototype : HTMLInputElement.prototype;
    Object.getOwnPropertyDescriptor(prototype, 'value').set.call(element, value);
    element.dispatchEvent(new Event('input', { bubbles: true }));
    element.dispatchEvent(new Event('change', { bubbles: true }));
    filled.push(key);
  }
  return filled;
}
