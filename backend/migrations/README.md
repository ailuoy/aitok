# 上线 SQL

使用 PostgreSQL 16。目标数据库应已创建，通常使用 `public` schema；执行 SQL 的连接应与应用使用相同的数据库及 schema，并具备相应建表和修改表权限。

## 执行入口

| 文件 | 用途 |
| --- | --- |
| `release.sql` | 可独立导入的上线 SQL，按顺序合并基础表和 001–008，支持空库初始化与本项目旧版本升级 |
| `verify.sql` | 只读核对 11 张表及所需字段，返回数据库、schema、缺失项和状态 |
| `schema.sql` | 基础表定义，作为合并 SQL 的来源 |
| `001_wallet_and_renewals.sql` | Session、续订日期、钱包、充值、流水、续订与日期审计 |
| `002_payment_quantity_and_history.sql` | 充值数量、单价、价格 ID，以及扣款记录的账号名称和时长快照 |

推荐在部署新服务前执行 `release.sql`。已有项目库无需重新建库或清空数据；重复执行会跳过已存在的表和字段。不要再额外执行一遍编号迁移。

在项目根目录，使用已经配置好的 PostgreSQL 客户端连接参数执行：

```bash
# PGHOST、PGPORT、PGDATABASE、PGUSER 或 PGSERVICE 指向目标数据库。
# 密码使用 .pgpass 或客户端提示输入，不写在 SQL 中。
psql -X --set=ON_ERROR_STOP=1 --file="backend/migrations/release.sql"
psql -X --set=ON_ERROR_STOP=1 --file="backend/migrations/verify.sql"
```

也可以在数据库管理工具中选中目标数据库，整体执行 `release.sql`，然后执行 `verify.sql`。遇到错误应停止执行并回滚事务；不要忽略错误后继续执行剩余语句。文件已包含事务，不再套用 `psql --single-transaction`。

合并 SQL 使用一个事务；锁等待上限 5 秒、单条语句上限 5 分钟。超时或任何语句失败时，本次事务不提交，排查后可以重新执行。文件不包含 `CREATE DATABASE`、数据库用户、权限、测试账号或充值数据。服务部署脚本 `scripts/deploy.sh` 不执行这些 SQL。

`verify.sql` 应返回 11 行 `OK`，`missing_columns` 均为 `{}`。它只检查表和字段是否存在，不会修改数据，也不保证已有字段类型、唯一约束、外键等定义一致。`IF NOT EXISTS` 不会修复手工改过的同名对象；这类库应与 `release.sql` 中的定义单独比对。

## 表与历史数据

| 表 | 内容 |
| --- | --- |
| `users` | 平台用户和密码哈希 |
| `chatgpt_accounts` | 用户绑定的 ChatGPT 账号、加密 Session 和续订日期 |
| `email_codes` | 邮箱验证码哈希及有效期 |
| `wallets` | 用户代币余额 |
| `topup_orders` | Stripe 充值订单、支付状态、金额与数量 |
| `wallet_ledger` | 充值及续订产生的余额流水 |
| `account_renewals` | 账号续订、扣款、日期及账号名称快照 |
| `renewal_date_audit` | 管理员修改续订日期的审计记录 |

- 超管账号由后端首次成功登录时创建，用户名和密码来自环境配置，不在 SQL 中插入。
- 钱包在用户访问钱包或产生钱包交易时按需创建，升级不预填余额。
- 旧账号的 `api_key` 保留；旧值不会自动转换成 Session JSON。
- 旧充值的 `quantity` 默认为 1，`unit_amount_minor`、`price_id` 允许为空；应用显示单价时回退到原订单总额。
- 旧续订的 `account_label`、`months` 允许为空，应用优先回退到当前账号名称，无法还原的时长显示“—”。不回填无法确认的历史信息。
- 续订和日期审计中的 `account_id` 不关联级联删除，账号删除后历史记录仍保留。

## 维护方式

原始迁移文件仍是唯一维护来源。新增编号迁移后，在项目根目录重新生成上线文件：

```bash
bash scripts/build-release-sql.sh > "backend/migrations/release.sql"
```

生成脚本只读取本地 SQL 并输出文本，不读取 `.env`，不连接数据库。新的编号迁移应继续使用能在单个事务中执行的语句；如果包含 `CREATE INDEX CONCURRENTLY` 等禁止在事务中执行的操作，应另行安排上线步骤，不能直接加入此合并入口。修改表或字段时同步更新 `verify.sql` 和本说明。

## 银行卡平台与备注（008）

`008_bank_card_platform_and_notes.sql` 为 `bank_cards` 增加 `platform`（最多 80 字）和 `notes`（最多 1000 字），均为非空文本、默认空字符串。旧银行卡无须回填，平台选项按用户从已有银行卡中去重读取。先执行迁移再部署服务；锁等待超过 5 秒会失败回滚。

应用回滚时保留新增列即可，旧版本忽略这两个字段。结构回滚需要另建前向迁移删除列，会永久丢失平台及备注，应备份并明确确认后执行，不自动回滚数据。
