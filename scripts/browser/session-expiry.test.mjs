import test from 'node:test';
import assert from 'node:assert/strict';
import { sessionExpiry } from '../../frontend/src/sessionExpiry.mjs';

test('Session 倒计时按天、小时、分、秒展示，正确处理到期边界', () => {
  const now = Date.parse('2030-01-01T00:00:00Z');
  const at = seconds => new Date(now + seconds * 1000).toISOString();
  assert.deepEqual(sessionExpiry(at(169203), true, now), { text: '剩余 1天23小时3秒', status: 'valid' });
  assert.equal(sessionExpiry(at(86461), true, now).text, '剩余 1天1分1秒');
  assert.equal(sessionExpiry(at(3600), true, now).text, '剩余 1小时0秒');
  assert.deepEqual(sessionExpiry(at(86400), true, now), { text: '剩余 1天0秒', status: 'soon' });
  assert.equal(sessionExpiry(at(0.1), true, now).text, '剩余 1秒');
  assert.deepEqual(sessionExpiry(at(0), true, now), { text: '已过期', status: 'expired' });
  assert.equal(sessionExpiry(at(-10), true, now).text, '已过期');
  assert.equal(sessionExpiry(at(2), true, now + 1000).text, '剩余 1秒');
  for (const value of [null, '', 'invalid']) assert.deepEqual(sessionExpiry(value, true, now), { text: '到期时间未知', status: 'unknown' });
  assert.equal(sessionExpiry(at(60), false, now).text, '未保存 Session');
});
