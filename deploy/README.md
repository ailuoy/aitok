# Linux Nginx 域名转发

`nginx.conf` 用于运行 AiTok Docker 服务的 Linux 宿主机。它监听 HTTP 80，将 `toktopup.com` 的全部流量转发到 `127.0.0.1:15680`，保留原路径和查询参数。文件只配置 HTTP 反向代理；如已有面板管理的 HTTPS 站点，可将 `location /` 段合并到该站点。

请求路径：

```text
toktopup.com 的 HTTP 请求 → 宿主机 Nginx（80）→ 127.0.0.1:15680
```

## 安装

1. 将域名解析到 Linux 服务器，确保 AiTok 服务已启动，服务器本机访问 `http://127.0.0.1:15680/healthz` 返回 `ok`。
2. 在服务器项目根目录，将站点配置放入 Nginx 已启用的配置目录。例如主配置已在 `http {}` 内包含 `/etc/nginx/conf.d/*.conf` 时：

   ```bash
   sudo cp "deploy/nginx.conf" "/etc/nginx/conf.d/toktopup.conf"
   sudo nginx -t && sudo systemctl reload nginx
   ```

   如果已有 `toktopup.com` 的站点配置，在原站点中应用相应配置，避免重复添加同域名的 server。使用 `sites-enabled` 或面板时，放到其实际启用的站点位置。项目部署脚本不会修改或重载宿主机 Nginx。

3. 线上 `.env` 按公网访问地址设置。公网 HTTPS 由现有站点或网关配置管理时，使用：

   ```dotenv
   APP_BASE_URL=https://toktopup.com
   MAIL_APP_BASE_URL=https://toktopup.com
   ```

   变更后部署应用服务，Stripe 端点保持 `https://toktopup.com/api/stripe/webhook`。

## 验证

```bash
curl -I "http://toktopup.com/features"
curl -i "http://toktopup.com/api/me"
```

仅使用本文件时，预期页面返回 200，未携带登录 token 时 API 返回 401。API 返回 JSON 表示请求已到达 Go 服务。
