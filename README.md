# AiTok

启动前需要 Go、Node.js 和 Docker：

```bash
bash run-dev.sh
```

前端地址为 `http://localhost:15680`，后端使用 `15681`，PostgreSQL 使用 `15682`。

## 部署服务

参考 google-maps 的部署方式，使用 Docker Compose 和限制 CPU 的 Buildx 构建器。服务器需要 Docker Engine 20.10+、近期版本的 Docker Compose（支持 `build --builder` 和 `up --wait`）、Buildx 0.14+ 和 Git，无需在宿主机安装 Node.js 或 Go。

在服务器项目根目录配置 `.env`（可选的 `backend/.env` 会覆盖同名项）。设置已有数据库的 `DATABASE_URL`、固定的 `JWT_SECRET`、`SESSION_ENCRYPTION_KEY` 和实际站点地址 `APP_BASE_URL`，邮件、Stripe 等继续使用现有环境变量。

```bash
# 仅检查配置，不拉取、构建或启动
bash scripts/deploy.sh --check

# 拉取当前分支最新代码，构建并更新前后端
bash scripts/deploy.sh

# 直接部署当前代码，例如尚未配置 Git 上游时
bash scripts/deploy.sh --skip-pull
```

部署只使用 `docker-compose.deploy.yml` 中的 `frontend`、`backend` 两个服务，不调用 `run-dev.sh`，不创建、启动、停止、迁移或备份数据库，也不操作数据库卷。现有数据库及表结构由外部准备。`DATABASE_URL` 必须是容器可访问的地址；如果数据库在宿主机上，可使用 `host.docker.internal`，不要用容器内的 `localhost`。脚本提供宿主机地址映射，但不会修改数据库监听或访问权限。

前端默认发布 `15680`，Nginx 提供静态文件、页面路由回退以及 `/api/` 到后端的代理；浏览器始终使用同域 `/api`。Nginx 配置位于 `deploy/nginx.conf`，构建前端镜像时复制到容器的 `/etc/nginx/conf.d/default.conf`。后端默认发布到 `127.0.0.1:15681`，通过 `BACKEND_BIND_HOST` 调整绑定地址。已有域名反向代理指向前端 `15680` 即可，Stripe Webhook 仍使用 `https://你的域名/api/stripe/webhook`。开发服务和部署服务使用相同默认端口，不应同时占用这些端口。

默认 `BUILD_CPU_COUNT=1`、`COMPOSE_PARALLEL_LIMIT=1`，BuildKit 内部串行执行构建任务。可在 `.env` 中调整，CPU 数变化时默认使用对应的新构建器。镜像构建成功后才更新容器，容器配置自动重启及日志轮转。脚本等待两项服务健康检查通过后报告成功，默认等待 120 秒，可通过 `DEPLOY_WAIT_TIMEOUT` 调整；健康检查只验证 HTTP 服务，不检查数据库表结构。启动失败时返回非零退出码，不自动回滚。

## 页面地址

导航使用独立路径：`/features`（功能）、`/plans`（套餐说明）、`/security`（安全）、`/login`（登录）、`/register`（注册）、`/accounts`（账号工作台）、`/wallet`（钱包）。支持直接访问、刷新和浏览器前进/后退。登录后访问首页会进入账号工作台；未登录访问钱包时，登录成功后返回钱包并保留待核对的支付订单。

Stripe 新订单返回 `/wallet?topup=success&order=...`，兼容旧订单的根路径返回地址。收到服务端确认的订单终态后清理支付参数，页面保留结果提示，记录仍可在钱包查询。取消支付返回时清理参数，订单是否到账仍以服务端核对为准。

生产部署需为前端配置 SPA history fallback，将不存在的页面路径回退到 `index.html`，例如 Nginx 的前端 `location /` 使用 `try_files $uri $uri/ /index.html;`。`/api/` 应单独反向代理至 Go 服务，不能回退到前端页面。Vite 开发服务已支持这些路径。

## 环境配置

启动脚本依次读取根目录 `.env` 和 `backend/.env`，后者覆盖同名参数。新环境可以复制 `.env.example` 为 `.env`，再填入邮件凭据和密钥。直接运行 Go 后端时，需要自行将这些参数导出为环境变量。

三个服务脚本统一使用 `scripts/load-env.sh` 按单行 `KEY=VALUE` 读取环境文件，连接串无需加引号，`&`、`$` 等字符按原值保留。兼容单引号、双引号包裹的单行值及注释，但不展开 Shell 变量、不执行命令，也不解释反斜杠转义。不要直接使用 `source .env` 读取包含未加引号连接串的配置。

`JWT_SECRET` 至少 32 个字符，可用 `openssl rand -hex 32` 生成。`SESSION_ENCRYPTION_KEY` 使用 `openssl rand -base64 32` 生成，用于 AES-GCM 加密 Session JSON，需长期保留。本地 `.env` 已生成这两项密钥。

默认超管配置：

```dotenv
SUPER_ADMIN_USERNAME=admin
SUPER_ADMIN_PASSWORD=123456
```

在密码登录页输入用户名和密码即可登录。修改配置后重启后端生效；超管关联的账号数据保留。超管凭据由环境配置管理，不通过邮箱验证码或密码找回修改。首次成功登录时创建内部用户记录，接口返回 `super_admin` 身份。

邮件使用与 google-maps 一致的 Cloudflare Email Sending：

```dotenv
MAIL_PROVIDER=cloudflare
MAIL_FROM_ADDRESS=no-reply@toktopup.com
MAIL_FROM_NAME=AiTok
MAIL_CODE_EXPIRE_MINUTES=10
CLOUDFLARE_ACCOUNT_ID=
CLOUDFLARE_EMAIL_API_TOKEN=
CLOUDFLARE_EMAIL_API_BASE_URL=https://api.cloudflare.com/client/v4
```

账户和 Token 必须有对应发件域名的发送权限。验证码只通过邮件发送；缺少配置或发送失败时接口返回错误，不回传验证码。`.env` 已加入 Git 忽略规则。

## 账号与续订

添加账号时输入名称、邮箱及完整 Session JSON 对象字符串。Session 加密保存，列表只返回是否已保存。旧账号不必删除，原账号 ID 保留；旧 API Key 不会自动转换为 Session。

超管可以查看所有用户的账号、设置或清空续订日期，修改会记录操作人及前后日期。普通用户只能查看自己的账号，使用自己的钱包代币续订。

默认 1 美元兑换 1 代币，20 代币续订 1 个月，通过以下配置修改：

```dotenv
TOKENS_PER_USD=1
RENEWAL_TOKEN_COST=20
RENEWAL_MONTHS=1
```

日期按北京时间计算。未过期账号从原日期延长，过期或未设置日期的账号从今天起算；月末日期会落在目标月份最后一个有效日。当前续订更新 AiTok 平台记录的会员有效期，尚未接入 OpenAI 官方购买或代充执行服务。

## Stripe 充值

复用 google-maps 的 Stripe Go SDK、Checkout、回调验签和事务入账方式。充值金额由服务端限定为 1 / 100 美元，分别使用配置的 Stripe Price ID。创建 Checkout 前会查询 Stripe 校验价格处于启用状态、单次支付、USD 币种且金额一致。

在 `.env` 中设置：

```dotenv
STRIPE_SECRET_KEY=sk_test_...
STRIPE_WEBHOOK_SECRET=whsec_...
STRIPE_1_PRICE_ID=price_1UEpzZBpVFyADK24y9lszR9t
STRIPE_100_PRICE_ID=price_1UEq0pBpVFyADK24MOR6FUDL
APP_BASE_URL=http://localhost:15680
```

本地 .env 已配置沙盒私钥、两个 Price ID 和 Stripe CLI 签名密钥；当前兑换规则下，1 USD 获得 1 代币，100 USD 获得 100 代币。上线时将 `APP_BASE_URL` 改为正式站点地址，在 Stripe 为 AiTok 单独配置 Webhook：

```text
POST https://你的域名/api/stripe/webhook
```

订阅事件：`checkout.session.completed`、`checkout.session.async_payment_succeeded`、`checkout.session.async_payment_failed`、`checkout.session.expired`。

本地测试：先运行应用，然后在另一个终端持续运行事件转发脚本。脚本从 .env 读取同一沙盒密钥：

```bash
bash run-stripe.sh
```

本地签名密钥可通过带有同一 STRIPE_API_KEY 环境变量的 `stripe listen --print-secret` 获取。线上进入 Stripe 沙盒的 Workbench → Webhooks，选中或添加对应端点，点击 Signing secret 的 Reveal 查看 `whsec_...`。将对应签名密钥填入 `STRIPE_WEBHOOK_SECRET` 后重启。CLI 密钥与线上 Webhook 密钥不同。使用测试密钥测试 Checkout，钱包仅在有效支付回调通过订单归属、金额、币种和状态校验后入账。浏览器返回成功页不会直接增加余额。

充值入账和续订扣币均有幂等约束与事务保护；余额、钱包流水、订单状态或账号日期同步提交。钱包显示最近 100 条流水与充值订单。

充值支持输入 1～100 的整数数量：总金额 = 所选档位单价 × 数量，到账代币按相同数量计算。服务端校验数量、单价及订单总额，重试同一请求不会创建重复支付。

回调未到达时，打开钱包、支付返回页或点击“刷新并核对支付”会主动核对待支付订单；也可以在充值记录中对单笔订单点击“核对支付”。后端仅通过 Stripe 查询的实际支付状态入账，金额、币种和归属校验与回调共用同一事务。用户只能核对自己的订单，超级管理员可核对指定漏回调订单。重复核账或之后再收到回调不会重复入账。

交易记录分为“充值记录”“账号扣款记录”“全部流水”。充值记录包含单价、数量、总额、到账代币和支付状态；扣款记录包含账号、时长、续订日期、扣币数及扣款后余额。新扣款会保存账号名称快照，删除账号后仍可查阅；旧记录中无法还原的时长显示为“—”。

登录 token 保存在浏览器中，在有效期内，只要 `JWT_SECRET` 保持不变，重启后端不会使其失效。服务暂时不可用或网络失败时，前端保留 token 并自动重试；仅服务器明确返回 401 或主动退出时清除登录状态。

## 数据库升级

上线 SQL 已整理为 [backend/migrations/release.sql](backend/migrations/release.sql)，可对空库或本项目旧库整体执行。表结构、执行命令与历史字段兼容说明见 [上线 SQL 说明](backend/migrations/README.md)，执行后可使用 [verify.sql](backend/migrations/verify.sql) 只读核对表和字段。上线 SQL 由基础表和编号迁移生成，服务部署脚本不会自动执行。

`run-dev.sh` 初始化基础表后，会按编号执行 `backend/migrations` 的增量 SQL。此次新增 Session 密文字段、续订日期、钱包、充值订单、钱包流水和续订记录；保留旧账号数据。SQL 支持重复执行，迁移失败时脚本停止启动。

## 验证

```bash
cd backend
go test ./...
go vet ./...
```

设置 `TEST_DATABASE_URL` 后，测试还会验证账号权限、Session 加密、管理员日期设置、Stripe 验签与防重复入账、余额不足、续订幂等和月末日期。测试仅使用当前数据库连接的临时表，不修改持久化数据；邮件与 Stripe 使用模拟服务，不会实际发信或扣款。

前端构建：在 `frontend` 目录执行 `npm run build`。
