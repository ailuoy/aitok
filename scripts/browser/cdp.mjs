import { EventEmitter } from 'node:events';

// Chrome 的调试管道只连接父进程，不对本机或网络开放调试端口。
export class CDP extends EventEmitter {
  constructor(child) {
    super();
    this.child = child;
    this.sequence = 0;
    this.pending = new Map();
    this.buffer = '';
    child.stdio[4].setEncoding('utf8');
    child.stdio[4].on('data', chunk => {
      this.buffer += chunk;
      let end;
      while ((end = this.buffer.indexOf('\0')) !== -1) {
        const raw = this.buffer.slice(0, end);
        this.buffer = this.buffer.slice(end + 1);
        try {
          const message = JSON.parse(raw);
          if (message.id) {
            const pending = this.pending.get(message.id);
            if (!pending) continue;
            clearTimeout(pending.timer);
            this.pending.delete(message.id);
            if (message.error) pending.reject(new Error(`浏览器指令执行失败：${pending.method}（${message.error.code}）`));
            else pending.resolve(message.result);
          } else this.emit('message', message);
        } catch { /* 忽略非协议消息，不记录包含凭据的响应。 */ }
      }
    });
    this.failure = () => {
      for (const pending of this.pending.values()) {
        clearTimeout(pending.timer);
        pending.reject(new Error('浏览器连接已关闭'));
      }
      this.pending.clear();
    };
    child.stdio[3].on('error', this.failure);
    child.stdio[4].on('error', this.failure);
    child.once('exit', this.failure);
    child.once('error', this.failure);
  }

  send(method, params = {}, sessionId) {
    const id = ++this.sequence;
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id);
        reject(new Error('浏览器响应超时'));
      }, 15000);
      this.pending.set(id, { resolve, reject, timer, method });
      this.child.stdio[3].write(JSON.stringify({ id, method, params, ...(sessionId ? { sessionId } : {}) }) + '\0');
    });
  }
}
