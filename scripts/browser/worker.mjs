import { createInterface } from 'node:readline';

// 仅供后端子进程使用；标准输出只承载协议响应，不输出凭据或调试日志。
export function runWorker(browser, input = process.stdin, output = process.stdout) {
  const reader = createInterface({ input, crlfDelay: Infinity });
  let closed = false;
  const close = () => { closed = true; browser.close(); };
  reader.once('close', close);
  output.on('error', close);
  reader.on('line', line => {
    (async () => {
      let request;
      try { request = JSON.parse(line); } catch { return; }
      if (!Number.isSafeInteger(request?.id) || request.id < 1) return;
      const send = payload => { if (!closed) output.write(JSON.stringify({ id: request.id, ...payload }) + '\n'); };
      try {
        if (line.length > 300000 || !request.params || typeof request.params.environment_id !== 'string') throw new Error('浏览器请求格式错误');
        const id = request.params.environment_id;
        let result;
        if (request.method === 'status') result = browser.status(id);
        else if (request.method === 'start') {
          if (process.platform === 'linux' && !process.env.DISPLAY && !process.env.WAYLAND_DISPLAY) {
            throw new Error('后台所在电脑没有可用桌面，请在图形桌面会话中运行后台');
          }
          result = await browser.start(request.params);
        } else if (request.method === 'stop') result = await browser.stop(id);
        else throw new Error('不支持的浏览器操作');
        send({ result });
      } catch (error) {
        send({ error: /^[\u3400-\u9fff]/.test(error.message) ? error.message : '浏览器操作失败，请检查后台电脑的浏览器和代理配置' });
      }
    })();
  });
  return reader;
}
