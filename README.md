# NovaPanel

服务器控制面板，Go 后端 + Vue 3 前端，单二进制内置前端产物。

当前仓库完成的是**认证与权限底座**：统一响应信封、链路追踪、JWT 访问令牌、服务端 RBAC、
不透明刷新令牌轮换与复用检测、登录 / 2FA / 刷新 / 登出 / 个人资料、健康检查，
以及配套的 Vue 3 + Arco Design 控制台（登录、二次验证、动态权限菜单、主题、国际化）。

> 文档 `docs/` 描述的文件管理、WebSSH、容器编排、监控告警、应用商店、多节点集群等模块
> **尚未实现**；`cmd/agent` 目前只是可编译的占位程序，运行会明确提示不支持。

## 技术栈

| 层 | 选型 |
| --- | --- |
| 后端 | Go 1.24、Gin、GORM、SQLite / MySQL / PostgreSQL、slog |
| 认证 | HS256 JWT（访问令牌）+ 不透明刷新令牌（仅存 SHA-256 摘要）、bcrypt、HKDF 派生密钥 |
| 前端 | Vue 3、TypeScript、Arco Design Vue、Pinia、Vue Router、Vue I18n、Vite 6、Vitest |

## 目录结构

```
cmd/            panel（控制面）、novactl（运维 CLI）、agent（未实现占位）
internal/
  api/v1/       路由、中间件（trace / recovery / auth）、handler、统一响应
  app/          依赖装配、迁移与种子、密钥初始化、HTTP/TLS、优雅停机
  model/        实体与 DTO
  pkg/          config、errs、logx、ctxkey、idgen
  rbac/         内置角色与权限点定义
  repository/   数据库、迁移器、种子、用户/角色/会话仓储
  security/     主密钥、HKDF、bcrypt、JWT、刷新令牌、撤销、自签证书
  service/auth/ 认证业务编排
  web/          go:embed 前端产物（internal/web/dist）
migrations/     版本化 SQL 迁移（up/down 成对）
web/            Vue 3 前端工程（构建输出到 internal/web/dist）
scripts/        错误码一致性、迁移演练、认证冒烟、开发证书
conf/           配置示例
```

## 快速开始

```bash
# 1. 前端产物（Go 需要它来 embed）
make web

# 2. 准备配置：示例默认指向 /opt/novapanel，本地开发请改成可写路径
cp conf/panel.example.yaml conf/panel.yaml

# 3. 启动控制面（首次启动自动迁移、写入种子数据、创建 admin）
make run
```

首次启动会在标准输出打印一次初始管理员口令（不写日志、不入库）。
也可以自行指定：

```bash
NOVA_INITIAL_ADMIN_PASSWORD='YourStr0ng-Pass!' make run
```

配置项均可用 `NOVA_` 前缀环境变量覆盖（点号换下划线），例如
`NOVA_SERVER_PORT`、`NOVA_DATABASE_DRIVER`、`NOVA_LOG_LEVEL`。
不想使用配置文件时传空路径：`go run ./cmd/panel -config ""`。

TLS 默认开启；`server.tls.auto_self_signed` 为 true 时，证书缺失会自动生成自签证书
（浏览器会提示不受信任，生产请替换）。也可用 `make certs` 单独生成开发证书。

### 前端开发

```bash
cd web
pnpm install
pnpm dev        # 开发服务器，/api 代理到本地面板
pnpm typecheck
pnpm test
pnpm build      # 输出到 ../internal/web/dist
```

## 命令行

```bash
novactl migrate up            # 执行未应用的迁移
novactl migrate status        # 查看迁移状态
novactl migrate down -step 1  # 回滚（需 NOVA_CONFIRM_DOWN=yes）
novactl seed                  # 幂等对账角色与权限
novactl passwd -u admin       # 重置口令（不传 -p 则随机生成）
novactl config check          # 校验配置
novactl version
```

公共参数 `-c <配置路径>`，可放在子命令前后；`-c ""` 表示只用默认值与环境变量。

## 认证与安全约定

- 访问令牌为 HS256 JWT，默认 15 分钟；签名密钥由主密钥经 HKDF 派生，不直接复用主密钥。
- 刷新令牌是不透明随机串，**仅以 SHA-256 摘要入库**，绝不出现在 JSON 响应里；
  通过 `nova_rt` Cookie 下发（HttpOnly、SameSite=Strict、Path=/api/v1/auth、Secure 跟随 TLS）。
- 每次刷新都会轮换刷新令牌；旧令牌再次使用视为泄露，返回 `110021` 并撤销该会话族。
- 权限在服务端解析，不写入 JWT 声明；改密会上移水位线，使该用户既有访问令牌立即失效。
- 前端只在内存 / localStorage 持有访问令牌，刷新令牌由浏览器 Cookie 管理，JS 不可读。
- 主密钥文件与私钥权限为 0600，务必纳入备份；丢失将无法解密既有敏感字段。

错误码为 6 位 `MMSSNN`，以 `internal/pkg/errs/code.go`、`statusMap` 与
`docs/05-API接口规范.md` 三处一致为准，由 `make check-errcode` 守护。

## 验证

```bash
make vet            # go vet
make test           # go test -race ./...
make test-cover     # service 层覆盖率门槛 70%
make check-errcode  # 错误码三处一致
make migrate-test   # sqlite 迁移 up -> down -> up（设置 MYSQL_DSN / POSTGRES_DSN 可扩展）
make web-typecheck  # 前端类型检查
make web-test       # 前端单测
make smoke          # 真实启动面板，跑完整认证链路与内置 SPA
```

`make smoke` 覆盖：健康检查信封、登录下发令牌与 HttpOnly Cookie、匿名访问 401、
profile 权限与菜单、刷新轮换与旧令牌复用拒绝、登出后令牌失效、SPA 首页与深链回落、
未知 API 路由返回 JSON 404 信封。CI 工作流见 `.github/workflows/ci.yml`。
