import { API } from './apiConfig';
import { adminAuditHeaders } from './adminActivity';
export { API };

export async function request(path, token, { method = 'GET', body, signal, totpCode } = {}) {
  let response;
  try {
    response = await fetch(API + path, {
      method, signal,
      headers: { ...adminAuditHeaders(), ...(totpCode ? { 'X-Aitok-TOTP': totpCode } : {}), ...(token ? { Authorization: `Bearer ${token}` } : {}), ...(body ? { 'Content-Type': 'application/json' } : {}) },
      body: body ? JSON.stringify(body) : undefined,
    });
  } catch (error) {
    if (error.name === 'AbortError') throw error;
    throw new Error('网络连接失败，请稍后重试');
  }
  if (response.status === 204) return null;
  const data = await response.json().catch(() => ({}));
  if (!response.ok) {
    const error = new Error(data.error || `请求失败（${response.status}）`);
    error.status = response.status;
    throw error;
  }
  return data;
}
