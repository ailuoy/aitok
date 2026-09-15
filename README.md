# AiTok

启动前需要 Go、Node.js 和 Docker：

```bash
bash run-dev.sh
```

前端地址为 `http://localhost:15680`，后端使用 `15681`，PostgreSQL 使用 `15682`。

## 本地 Docker 启动

需要 Docker、Docker Compose（支持 `up --wait`）和 `lsof`（用于释放端口，macOS 通常已自带），无需在宿主机安装 Go 或 Node.js：

```bash
./run-dev-docker.sh up           # 构建镜像、启动数据库、初始化表结构并启动前后端
./run-dev-docker.sh logs backend # 查看后端日志
./run-dev-docker.sh app-restart  # 重新加载前后端配置，保留数据库运行
./run-dev-docker.sh down         # 移除容器，保留数据库卷
./run-dev-docker.sh help         # 查看应用、数据库分组管理等全部命令
```

首次运行复制 `.env.docker.example` 为 `.env.docker`，未配置的 `JWT_SECRET` 和 `SESSION_ENCRYPTION_KEY` 通过 OpenSSL 生成并保存到该文件。配置按 `.env` → `backend/.env` → `.env.docker` 读取，后者覆盖同名项；`--env=test` 会最后读取 `backend/.env.test`。连接串中的 `$`、`&` 按原值保留。

默认端口仍为 `15680` / `15681` / `15682`，可在 `.env.docker` 调整 `FRONTEND_PORT`、`BACKEND_PORT`、`DB_PORT` 和 `APP_BASE_URL`。容器中的默认数据库地址为 `postgres:5432`；使用已有数据库时，在 `.env.docker` 或指定环境文件中设置容器可访问的 `DATABASE_URL`，宿主机数据库可使用 `host.docker.internal`。自动初始化只作用于脚本管理的本地 PostgreSQL，外部数据库需自行执行上线 SQL。

启动或重启服务前自动释放对应端口：停止冲突的 Docker 容器（保留容器和数据卷），对本机监听进程先发送 TERM，等待约 3 秒后仍占用则发送 KILL。当前项目对应的服务容器会跳过；仅启动数据库时只处理数据库端口，仅启动应用时只处理前后端端口。

Docker 开发使用独立的 `aitok-dev` Compose 项目和 `aitok-dev_getgpt_pgdata` 数据卷，不复用 `run-dev.sh` 的数据库数据。`db-start` 先启动并初始化本地库，随后可用 `app-start` 单独启动应用；`app-restart` 和 `db-restart` 只重建对应容器，不删除数据卷。前后端复用现有 Go / Nginx 镜像配置，修改代码后再次执行 `up` 构建更新，不提供热更新。此模式和原生开发、部署模式使用相同默认端口，不能同时占用。后台账号浏览器需要宿主机图形桌面，请使用 `run-dev.sh` 运行该功能。

开发网络显式使用 `10.253.0.0/24`，避免 Docker 默认地址池耗尽时报 `all predefined address pools have been fully subnetted`。若与已有 Docker 网络、局域网或 VPN 网段冲突，可在 `.env.docker` 设置 `DEV_NETWORK_SUBNET` 为未占用的私有子网；同时运行多个开发项目时需为各项目指定不同子网。已有网络的子网变更需先执行 `./run-dev-docker.sh down`，再执行 `./run-dev-docker.sh up` 重建网络（保留数据库卷）。

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

前端默认发布 `15680`，Web 容器的 Nginx 提供静态文件、页面路由回退以及 `/api/` 到后端的代理；浏览器始终使用同域 `/api`。Linux 宿主机的域名配置位于 `deploy/nginx.conf`，监听 `toktopup.com` 的 HTTP 80 端口并转发到本机 `15680`，安装方式见 [域名反向代理说明](deploy/README.md)。后端默认发布到 `127.0.0.1:15681`，通过 `BACKEND_BIND_HOST` 调整绑定地址。Stripe Webhook 使用 `https://toktopup.com/api/stripe/webhook`。开发服务和部署服务使用相同默认端口，不应同时占用这些端口。

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

添加账号时粘贴完整的 `/api/auth/session` JSON，系统识别名称和邮箱；无法识别时可手动补充。支持 `accessToken` / `access_token`，以及 `tokens`、`credentials` 下的对应字段。名称可自定义，填写的邮箱需要与 Session 中识别到的邮箱一致。Session 加密保存，列表只返回是否已保存。旧账号不必删除，原账号 ID 保留；旧 API Key 不会自动转换为 Session。账号行的“更新 Session”可替换过期凭据，并检查邮箱是否属于同一账号。

## 在本机打开账号（实验版）

普通用户在自己的账号卡片上点击“打开账号”，使用已保存的 Session JSON 在访问网页的电脑上打开独立 ChatGPT 窗口。没有保存 Session 的账号会禁用此按钮。网站后端可以继续运行在 Docker 中。

1. 在访问网页的电脑上安装 Node.js 22+ 和 Chrome、Chromium 或 Edge，并准备本项目的 `scripts` 目录。
2. 在项目目录运行 `node scripts/session-browser.mjs --origin http://localhost:15680`。如果网站地址不同，使用弹窗中按当前站点生成的命令。
3. 保持终端开启，直接点击账号卡片的“打开账号”。窗口同时打开 `https://api.ipify.org/` 和 `https://cleanip.io/`，通过 ipify 读取浏览器实际出口 IP；与所选 SOCKS5 主机 IP 一致后，保留 IP 标签并新开 `https://chatgpt.com/#settings/Billing`。直连账号获取有效 IP 后直接继续。IP 不一致或获取失败时保留窗口并提示原因，不自动打开 ChatGPT。不需要配对密钥，也无需在弹窗中再次点击；浏览器询问本地网络访问权限时选择允许。
4. 浏览器打开后，账号卡片按钮自动变为“关闭浏览器”，点击即可结束该环境；也可在弹窗点击“关闭账号窗口”。刷新网页会同步现有窗口状态，手动关闭窗口后按钮自动恢复“打开账号”。更新 Session 或代理后重新打开即可生效。页面刷新后无需重新配对。关闭浏览器后，管理弹窗会自动收起。

每个账号的 Chrome 资料名称设为账号邮箱，网页右上角不再显示独立邮箱浮标，邮箱统一在账号助手中展示。Chrome 原生标题栏不支持任意位置自定义文字，资料菜单也可查看账号邮箱。仅修改独立账号目录，不修改日常 Chrome 资料。SOCKS5 使用代理端解析目标域名，并关闭网络预取；不再使用会触发 Chrome 警告的 `--host-resolver-rules` 参数。

本机启动器只监听 `127.0.0.1`，限定 `--origin` 指定的站点及 Host，并要求非简单请求头；普通跨站网页和表单不能调用它。网站自己的登录鉴权和账号归属校验继续生效。

重复运行启动命令会自动结束占用启动器端口（默认 `15683`）的旧进程，再启动新实例；旧启动器管理的浏览器也会关闭。macOS / Linux 使用 `lsof` 查询监听进程，Windows 使用 `netstat` / `taskkill`。仅影响指定端口，`--stdio` 模式不处理端口。

### SOCKS5 管理

工作台的“SOCKS5 管理”支持添加、编辑、删除、测试和获取出口 IP，可配置主机、端口及可选的用户名密码。在账号卡片的 SOCKS5 下拉框选择代理，绑定会保存在本机，下次打开自动使用；切回直连会移除绑定。仍被账号绑定的代理不能删除，需先更改账号选择。

“导入代理”支持一行一条的 `socks5://主机:端口:用户名:密码`，也兼容 `socks5://用户名:密码@主机:端口` 和 `socks://`。导入逐条测试后保存，通过的行自动移除，失败的行保留供修改重试；最多一次 100 条。

添加和编辑表单在保存旁提供“测试”按钮，测试通过才允许保存；修改任何字段后必须重新测试。启动器也校验测试结果与当前配置一致，测试凭证五分钟内有效且只能使用一次。账号和代理删除均需在弹窗中二次确认，可点击取消退出。

测试与获取 IP 都由本机通过所选 SOCKS5 请求 `https://api.ipify.org`，不回退到直连。出口 IP 与代理主机 IP 一致显示绿色，不一致显示红色；代理主机是域名时与其 DNS 解析出的地址比对。不同出口可能是代理服务的正常转发结果，红色仅表示地址不一致。连接失败另行显示错误。

代理密码与绑定按站点加密保存在启动器目录 `settings/<站点哈希>/proxies.enc`，加密密钥文件 `proxy.key` 只允许当前用户读取。它们属于当前电脑，不随服务器数据库或其他设备同步。编辑表单明文显示原密码，清空用户名和密码可取消认证；代理列表不返回密码。Chromium 通过回环 SOCKS5 桥接使用带密码的代理，凭据不会出现在进程参数中。

### Session JSON 与网页登录

Session 输入框支持 JSON 语法高亮和格式化。普通 `/api/auth/session` 返回的 `accessToken` 是访问凭据，无法转换成服务器签发的登录 Cookie。此前模拟 Session 接口的方式不能恢复真实网页登录，现已移除。

可在导入或更新 Session 时补充“网页登录 Cookie”字段（`__Secure-next-auth.session-token` 的值），或在 JSON 中加入 `sessionToken`。分段 Cookie 使用 `cookies` 数组，每项为 `name`、`value`，可带 `domain`；仅接收 chatgpt.com 的 `__Secure-next-auth.session-token` / `__Secure-authjs.session-token` 及其数字分段，其他站点和无关 Cookie 不会导出。启动器在首次导航之前恢复这些真实 Cookie。

没有登录 Cookie 时，窗口会提示本次未提供 Cookie；可沿用已有浏览器目录，或在独立窗口登录一次。Cookie 是否有效仍由 ChatGPT 校验；不会伪造登录成功。启动器只在 ChatGPT 标签检查登录，不暂停新标签或子页面；标签关闭、导航或检查失败不会结束浏览器。用户自己的 Session 和登录 Cookie 继续加密保存在数据库。

## 后台账号浏览器（实验版）

管理员可直接在“账号管理”完成“导入 Session JSON → 保存 SOCKS5 代理 → 打开独立 Chromium 窗口”。窗口出现在运行 Go 后台的电脑上；需要在该电脑的图形桌面会话中运行后台，并安装 Node.js 22+ 和 Chrome、Chromium 或 Edge。后台按需启动浏览器工作进程，页面不需要填写端口、配对密钥或另行启动服务。

1. 在已登录 ChatGPT 的浏览器中打开 `https://chatgpt.com/api/auth/session`，复制完整 JSON 到 AiTok 的“添加账号”。
2. 使用超级管理员登录，在目标账号行点击“浏览器管理”。管理员可操作全部账号，普通用户不能启动后台电脑的浏览器。
3. “网络连接”默认沿用账号保存的配置。选择“设置 SOCKS5 代理”，填写 `socks5://主机:端口` 或 `socks5://用户名:密码@主机:端口`，点击“保存代理”；用户名或密码中的 `@`、`:`、`/` 等字符需进行 URL 百分号编码。选择“改用直连”并保存即可清除代理，直连不使用系统代理。
4. 点击“打开浏览器”，在后台电脑弹出的窗口中确认 ChatGPT 能否使用。界面显示运行状态及上游账号验证结果。
5. 在同一页面点击“关闭浏览器”。更换代理或更新 Session 前先关闭环境，重新打开后使用最新配置。退出后台时，浏览器工作进程检测到父进程管道关闭，会关闭它管理的浏览器。

每个账号对应独立配置目录，默认在后台运行用户的 `~/.aitok/browsers`；最多同时打开 10 个环境。代理密码和 Session 一起加密保存于已有 `session_ciphertext` 字段，兼容以前保存的原始 Session，无需数据库迁移。更新 Session 保留已配置的代理，页面仅返回脱敏后的代理地址。Chromium 配置目录会保留网站自身保存的状态，目录不会自动删除。

非标准安装可配置 `AITOK_BROWSER_NODE`（Node 可执行文件）、`AITOK_BROWSER_CHROME`（浏览器可执行文件）、`AITOK_BROWSER_SCRIPT`（`scripts/session-browser.mjs` 绝对路径）、`AITOK_BROWSER_DIRECTORY`（独立环境目录）。后台通过私有标准输入输出与 Node 子进程通信，不新增监听端口。Linux 需要可用的 `DISPLAY` 或 `WAYLAND_DISPLAY`；当前默认 Docker 部署镜像没有图形桌面，此开窗模式应在有桌面的宿主系统中原生运行后台。

浏览器通过 Chromium 私有调试管道恢复登录 Cookie，不伪造 Session 接口，也不注入 Authorization。状态通过真实 `/api/auth/session` 返回的邮箱与导入账号进行核对：

- “已打开”：浏览器已启动，可以正常浏览；访问 ChatGPT 标签后再检查实际登录状态。
- “已确认登录”：真实网页登录会话的邮箱与导入账号一致。
- “需要网页登录”：未登录或 Cookie 已失效，需要补充 Cookie 或在窗口登录。
- “登录待确认”：网络、代理或网页验证导致无法核对状态。
- “上游未接受凭据”：窗口内登录的邮箱与导入账号不一致。

启动前会根据 JSON 的 `expires` 和 JWT 的 `exp` 提示已知过期情况；本地解析 JWT 不验证签名，不证明凭据有效。没有刷新凭据时需要重新复制 Session。本版没有自动刷新 Token，也不执行购买、支付或续费网页操作。

`GET /api/accounts/:id/browser` 查询状态，`PATCH` 保存代理，`POST` 启动，`DELETE` 关闭；这些操作全部限定超级管理员。服务端从数据库读取会话后直接交给浏览器进程，前端不获取 Token。`PATCH /api/accounts/:id/session` 允许所有者或管理员更新会话。浏览器接口均禁止缓存，凭据不会进入进程命令行。

“打开账号”通过独立启动器命令 `node scripts/session-browser.mjs --origin http://localhost:15680` 和仅限所有者的 `POST /api/accounts/:id/browser-session` 实现本机开窗；管理员的“浏览器管理”仍使用后台进程，不调用本机启动器。

## 账号分组、登录时间与界面

工作台支持分组创建、改名、删除以及账号绑定，账号分组下拉菜单底部可直接新建分组，自动带入搜索内容，创建成功后绑定当前账号；分组选择器和列表筛选均可搜索。分组按用户隔离；管理员可以管理所有用户的分组，但账号只能绑定其所属用户的分组。删除分组需要二次确认，账号保留并变为未分组。

新增迁移为 `backend/migrations/004_account_groups_and_login.sql`，提供 `account_groups`、账号的 `group_id` 与 `last_login_at`。登录时间只在独立浏览器通过 ChatGPT 真实会话接口确认账号一致后记录，每次开窗最多记录一次；接口重试不会倒退或重复刷新时间。时间以绝对时间存储，页面统一显示 UTC+8，历史账号没有记录时显示“尚未登录”。

右上角“我的”提供钱包与充值入口，以及“自动／黑色／白色”三种主题；自动模式跟随系统偏好，选择在本机浏览器中保存。所有选择器统一为可搜索的自定义菜单，支持方向键、Enter 和 Escape。

代币续订已停用，旧 `POST /api/accounts/:id/renew` 返回 410，不再扣币；历史充值和扣款流水保留。管理员仍可以设置或清空会员日期，修改写入日期审计。充值兑换比例由 `TOKENS_PER_USD` 配置。

## SOCKS5 使用记录

SOCKS5 管理中可查看单个代理或全部代理的使用记录，包含测试、获取 IP、账号打开成功或失败、浏览器 IP 核对、登录确认及关闭。记录时间显示 UTC+8，支持分页。记录与代理配置一起加密保存在本机，保留代理名称和地址快照；编辑或删除代理后历史仍可查看。日志不包含密码、Session 或网页访问内容。新功能上线前的使用无法补录。

## 银行卡、地址与浏览器助手

工作台在“SOCKS5 管理”后显示“地址管理”和“银行卡管理”，所有登录用户可使用。原先采集的地址为共享地址，普通用户可以选择，但只能编辑和删除自己新增的地址；管理员可管理全部地址。银行卡按用户隔离，支持搜索、分页、添加、编辑和删除确认。卡平台支持从下拉框选择已有名称，或输入新名称后点击“使用输入的平台”，随银行卡一起保存；选项来自本人全部银行卡，去重且不受搜索和分页影响。卡平台最多 80 字，备注支持多行、最多 1000 字，均为选填且可清空，列表搜索包含平台与备注。账号助手同步展示这两个字段并支持逐项复制。升级需先执行迁移 `backend/migrations/008_bank_card_platform_and_notes.sql`，再部署服务。

地址卡片逐项展示账单姓名、街道、公寓 / 房间、城市、州 / 省、邮编和国家 / 地区，不显示采集来源链接。新增和编辑地址可填写账单姓名，搜索也支持姓名；迁移 `backend/migrations/006_address_full_name.sql` 为旧地址保留空姓名，显示“未填写”，可按实际账单信息补充。

迁移 `backend/migrations/005_bank_cards_and_address_owners.sql` 新增银行卡表和地址归属字段。卡号通过现有 `SESSION_ENCRYPTION_KEY` 加密，列表只显示尾号；详情仅允许本人读取。安全码（CVV/CVC）不入库，只在浏览器助手中临时输入，填充后清空。

从工作台重新“打开账号”后，ChatGPT 网页右侧显示可折叠的“账号助手 0.0.1”（版本号在脚本中统一定义），收起后的“账号助手”按钮可拖拽或使用方向键移动，窗口缩小与重新展开时自动限制在可视区域；刷新页面后恢复默认右侧位置。助手包含邮箱、真实登录状态、官网返回的套餐、银行卡和账单地址选择，以及手动填充和切换功能。不包含删除登录状态、登录其他账号或获取充值队列。套餐按钮进入官网套餐选择页，并不直接创建订单或承诺某个方案可购买，实际方案与价格以官网为准。

助手通过 Chromium 调试接口在隔离脚本环境中注入网页 HTML，并使用封闭 Shadow DOM 隔离界面样式；它不是浏览器原生侧栏或扩展侧栏。

助手也可在允许的官方收银页面显示，选中银行卡后读取并明文展示完整卡号、持卡人、卡类型及有效期，同时显示所选地址的必要字段，每个字段后有小型复制按钮，未填写的字段不可复制。套餐入口按 Plus / 5X / 20X 横向排列。安全码详情实时展示并支持复制本次输入值，填充后或切卡时清空；名称、持卡人和卡号均匹配内置演示记录时显示明确标注的测试安全码（Visa / Mastercard 为 123，Amex 为 1234），普通卡不推断安全码。只填写识别出的可见支付字段，不提交付款。不同收银页面或嵌入字段可能需要手动补充，请核对信息后自行付款。仅使用与你付款信息一致的账单地址。助手用独立的 12 小时只读授权访问本人银行卡与可用地址，授权保留在本机进程，不向 ChatGPT 注入平台登录凭据。更改启动器版本后需重新打开账号才能更新助手。

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

浏览器启动器验证（使用模拟会话和本地代理，不需要真实账号）：

```bash
node --test scripts/browser/session.test.mjs scripts/browser/browser-ip.test.mjs scripts/browser/proxy-store.test.mjs scripts/browser/launcher-port.test.mjs
# 可选：真实 Chromium 冒烟测试，仅打开 about:blank，检查 Cookie 恢复和登录状态判定。
AITOK_BROWSER_SMOKE=1 node --test scripts/browser/session.test.mjs scripts/browser/browser-ip.test.mjs
# 在 frontend 中完成 npm run build 后，验证导入、代理配置、启动和关闭入口及移动端布局。
AITOK_BROWSER_SMOKE=1 node --test scripts/browser/frontend.test.mjs
```


## 地址库

所有登录用户可在工作台访问地址管理，按姓名、街道、城市、州、邮编、电话或邮箱搜索。普通用户可读取共享地址及自己的地址，只能修改自己的记录；超级管理员可管理全部地址，删除需二次确认。

基础字段保存在 PostgreSQL 的 addresses 表，迁移 007_address_source_data.sql 增加 JSONB source_data，完整保留来源接口返回的字段（包括未知的新字段）。列表直接显示姓名、街道、城市、州、邮编、国家、电话和邮箱，其他生成资料在“完整来源资料”中展开查看。原始来源快照只读，编辑基础地址不会覆盖原始快照。来源生成的身份、卡号和安全码只作资料展示，不自动加入付款银行卡库。

页面每次生成一条随机资料，并非有限的可枚举全站数据库。本项目采集 100 条完整资料，原始 JSON 保存在 backend/data/oregon-profiles.json；旧的基础地址快照仍在 backend/data/oregon-addresses.json。未再次出现的旧地址不拼接其他人的姓名；同一地址再次出现时，仅补充未编辑的共享精简记录，其他旧地址保留。新导入或更新的记录优先展示。

采集依赖 Python 3 和 curl，每获得一条去重记录即保存，失败可重试；只有达到指定数量才生成导入 SQL：

```bash
python3 scripts/import-oregon-addresses.py --count 100 --output backend/data/oregon-profiles.json --sql /tmp/oregon-profiles.sql
# 对已执行迁移的目标库运行生成的 SQL；事务写入并按地址去重。
```
