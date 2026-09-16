// 仅绑定回环地址；冲突时保留已有服务，绝不按端口结束其他进程。
export function listenLocal(server, port) {
  return new Promise((resolve, reject) => {
    const failed = error => {
      server.off('listening', ready);
      if (error.code === 'EADDRINUSE') error.message = `端口 ${port} 已被占用，请退出对应旧启动器，或在助手和网页中选择其他端口`;
      reject(error);
    };
    const ready = () => { server.off('error', failed); resolve(); };
    server.once('error', failed); server.once('listening', ready);
    server.listen(port, '127.0.0.1');
  });
}
