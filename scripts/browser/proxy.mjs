import net from 'node:net';
import { once } from 'node:events';

export class ProxyError extends Error {
  constructor(code, stage) { super(code); this.code = code; this.stage = stage; }
}

export function parseProxy(raw) {
  if (!raw) return null;
  if (typeof raw !== 'string' || raw.length > 2048 || /[\r\n\0]/.test(raw)) throw new Error('代理格式无效');
  raw = raw.trim().replace(/^socks:\/\//i, 'socks5://');
  const legacy = /^socks5:\/\/(\[[^\]]+\]|[^:/@\s]+):(\d+):([^:]+):(.+)$/i.exec(raw);
  if (legacy) raw = `socks5://${encodeURIComponent(legacy[3])}:${encodeURIComponent(legacy[4])}@${legacy[1]}:${legacy[2]}`;
  let proxy;
  try { proxy = new URL(raw); } catch { throw new Error('请输入 socks5://主机:端口 格式的代理'); }
  if (proxy.protocol !== 'socks5:' || !proxy.hostname || !proxy.port || Number(proxy.port) > 65535 || Number(proxy.port) < 1 || proxy.search || proxy.hash || (proxy.pathname && proxy.pathname !== '/')) {
    throw new Error('代理必须为 socks5://主机:端口，可包含用户名和密码');
  }
  let username, password;
  try { username = decodeURIComponent(proxy.username); password = decodeURIComponent(proxy.password); }
  catch { throw new Error('代理用户名或密码编码无效'); }
  if (Buffer.byteLength(username) > 255 || Buffer.byteLength(password) > 255 || Boolean(username) !== Boolean(password)) {
    throw new Error('代理用户名和密码需同时填写，且各不超过 255 字节');
  }
  return { host: proxy.hostname.replace(/^\[|\]$/g, ''), port: Number(proxy.port), username, password };
}

// 使用可读流保留握手之后的所有字节，避免丢失与握手一起到达的 TLS 数据。
async function readBytes(socket, count) {
  for (;;) {
    const bytes = socket.read(count);
    if (bytes !== null) return bytes;
    if (socket.destroyed || socket.readableEnded) throw socket.errored || Object.assign(new Error('代理连接中断'), { code: 'ECONNRESET' });
    await new Promise((resolve, reject) => {
      const clean = () => { socket.off('readable', ready); socket.off('close', closed); socket.off('error', failed); };
      const ready = () => { clean(); resolve(); };
      const closed = () => { clean(); reject(socket.errored || Object.assign(new Error('代理连接中断'), { code: 'ECONNRESET' })); };
      const failed = error => { clean(); reject(error); };
      socket.once('readable', ready); socket.once('close', closed); socket.once('error', failed);
    });
  }
}

async function readAddress(socket, type) {
  if (type === 1) return readBytes(socket, 6);
  if (type === 4) return readBytes(socket, 18);
  if (type === 3) {
    const length = await readBytes(socket, 1);
    return Buffer.concat([length, await readBytes(socket, length[0] + 2)]);
  }
  throw new Error('代理地址格式无效');
}

async function handshake(socket, proxy, header, address) {
  let stage = 'tcp';
  try {
    await once(socket, 'connect');
    stage = 'negotiation';
    const method = proxy.username ? 2 : 0;
    socket.write(Buffer.from([5, 1, method]));
    const chosen = await readBytes(socket, 2);
    if (chosen[0] !== 5 || chosen[1] !== method) throw new ProxyError('SOCKS_METHOD', stage);
    if (method === 2) {
      stage = 'authentication';
      const user = Buffer.from(proxy.username), password = Buffer.from(proxy.password);
      socket.write(Buffer.concat([Buffer.from([1, user.length]), user, Buffer.from([password.length]), password]));
      const result = await readBytes(socket, 2);
      if (result[0] !== 1 || result[1] !== 0) throw new ProxyError('SOCKS_AUTH_REJECTED', stage);
    }
    stage = 'target';
    socket.write(Buffer.concat([header, address]));
    const result = await readBytes(socket, 4);
    if (result[0] !== 5) throw new ProxyError('SOCKS_PROTOCOL', stage);
    if (result[1] !== 0) throw new ProxyError('SOCKS_TARGET_' + result[1], stage);
    return Buffer.concat([result, await readAddress(socket, result[3])]);
  } catch (error) {
    if (error instanceof ProxyError) throw error;
    const reason = error.code === 'ETIMEDOUT' ? 'TIMEOUT' : error.code === 'ECONNREFUSED' ? 'REFUSED' : error.code === 'ENOTFOUND' || error.code === 'EAI_AGAIN' ? 'DNS' : 'CLOSED';
    throw new ProxyError('SOCKS_' + reason, stage);
  }
}

export async function connectSocks5(proxy, hostname, port) {
  const socket = net.createConnection({ host: proxy.host, port: proxy.port });
  socket.on('error', () => {});
  socket.setTimeout(10000, () => socket.destroy(Object.assign(new Error('代理连接超时'), { code: 'ETIMEDOUT' })));
  try {
    const name = Buffer.from(hostname), address = Buffer.alloc(name.length + 3);
    address[0] = name.length; name.copy(address, 1); address.writeUInt16BE(port, name.length + 1);
    await handshake(socket, proxy, Buffer.from([5, 1, 0, 3]), address);
    return socket;
  } catch (error) { socket.destroy(); throw error; }
}

// Chromium 不支持带密码的 SOCKS5，使用只监听回环地址的本地桥接完成认证。
// 域名原样转发给远端 SOCKS5，不在本机解析目标域名。
export async function createProxyBridge(proxy) {
  const sockets = new Set();
  const server = net.createServer(client => {
    sockets.add(client);
    client.on('error', () => {});
    client.on('close', () => sockets.delete(client));
    client.setTimeout(15000, () => client.destroy());
    let upstream;
    client.once('close', () => upstream?.destroy());
    (async () => {
      const hello = await readBytes(client, 2);
      if (hello[0] !== 5) throw new Error('SOCKS 版本无效');
      const methods = await readBytes(client, hello[1]);
      if (!methods.includes(0)) throw new Error('SOCKS 认证方式无效');
      client.write(Buffer.from([5, 0]));
      const header = await readBytes(client, 4);
      if (header[0] !== 5 || header[1] !== 1 || header[2] !== 0) throw new Error('只支持 SOCKS5 CONNECT');
      const address = await readAddress(client, header[3]);
      upstream = net.createConnection({ host: proxy.host, port: proxy.port });
      sockets.add(upstream);
      upstream.on('error', () => client.destroy());
      upstream.on('close', () => { sockets.delete(upstream); client.destroy(); });
      upstream.setTimeout(15000, () => upstream.destroy());
      client.write(await handshake(upstream, proxy, header, address));
      client.setTimeout(0); upstream.setTimeout(0);
      upstream.pipe(client); client.pipe(upstream);
    })().catch(() => {
      client.destroy(); upstream?.destroy();
    });
  });
  await new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', resolve);
  });
  return {
    url: `socks5://127.0.0.1:${server.address().port}`,
    close() { for (const socket of sockets) socket.destroy(); server.close(); },
  };
}
