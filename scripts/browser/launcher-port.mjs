import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { setTimeout as delay } from 'node:timers/promises';

const execute = promisify(execFile);

async function listeners(port) {
  if (process.platform === 'win32') {
    const { stdout } = await execute('netstat', ['-ano', '-p', 'tcp']);
    return [...new Set(stdout.split('\n').flatMap(line => {
      const fields = line.trim().split(/\s+/);
      return fields[0] === 'TCP' && fields[1]?.endsWith(`:${port}`) && fields[3] === 'LISTENING' ? [Number(fields[4])] : [];
    }))];
  }
  try {
    const { stdout } = await execute('lsof', ['-nP', `-iTCP:${port}`, '-sTCP:LISTEN', '-t']);
    return [...new Set(stdout.trim().split(/\s+/).map(Number).filter(pid => pid > 0))];
  } catch (error) {
    if (error.code === 1 && !error.stdout && !error.stderr) return [];
    throw new Error('无法查询占用端口的进程，请检查 lsof 是否安装及当前用户权限');
  }
}

function listen(server, port) {
  return new Promise((resolve, reject) => {
    const failed = error => { server.off('listening', ready); reject(error); };
    const ready = () => { server.off('error', failed); resolve(); };
    server.once('error', failed); server.once('listening', ready);
    server.listen(port, '127.0.0.1');
  });
}

export async function listenReplacing(server, port) {
  try { await listen(server, port); return; }
  catch (error) { if (error.code !== 'EADDRINUSE') throw error; }
  const pids = await listeners(port);
  if (!pids.length) { await listen(server, port); return; }
  if (pids.some(pid => pid === process.pid || pid === process.ppid || pid <= 1)) throw new Error('端口被当前进程或父进程占用，请使用 --port 指定其他端口');
  console.log(`端口 ${port} 已被占用，正在结束旧进程并重新启动。`);
  for (const pid of pids) {
    try {
      if (process.platform === 'win32') await execute('taskkill', ['/PID', String(pid), '/T', '/F']);
      else process.kill(pid, 'SIGTERM');
    } catch (error) { if (error.code !== 'ESRCH') throw new Error(`无法结束占用端口 ${port} 的进程，请检查当前用户权限`); }
  }
  for (let attempt = 0; attempt < 30; attempt++) {
    await delay(100);
    try { await listen(server, port); return; }
    catch (error) { if (error.code !== 'EADDRINUSE') throw error; }
  }
  // 只强制结束仍在监听此端口的原进程，避免影响随后启动的其他进程。
  for (const pid of await listeners(port)) {
    if (pids.includes(pid)) {
      try { process.kill(pid, 'SIGKILL'); }
      catch (error) { if (error.code !== 'ESRCH') throw new Error(`无法释放端口 ${port}，请检查当前用户权限`); }
    }
  }
  await delay(200);
  await listen(server, port);
}
