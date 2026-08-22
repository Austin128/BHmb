# NovaPanel

服务器控制面板，Go 后端 + Vue 3 前端，单二进制内置前端产物。

当前仓库完成的是**认证与权限底座**加**文件管理**：统一响应信封、链路追踪、JWT 访问令牌、
服务端 RBAC、不透明刷新令牌轮换与复用检测、登录 / 2FA / 刷新 / 登出 / 个人资料、健康检查，
白名单化的文件浏览 / 读写 / 上传下载（含分片断点续传与秒传）/ 复制移动 / 压缩解压 / 权限修改，
以及配套的 Vue 3 + Arco Design 控制台（登录、二次验证、动态权限菜单、文件管理页、主题、国际化）。

> 文档 `docs/` 描述的 WebSSH、容器编排、监控告警、应用商店、多节点集群等模块
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
  service/file/ 文件管理业务（pathguard 为路径白名单守卫）
  web/          go:embed 前端产物（internal/web/dist）
migrations/     版本化 SQL 迁移（up/down 成对）
web/            Vue 3 前端工程（构建输出到 internal/web/dist）
scripts/        bh 管理命令、install/uninstall、错误码一致性、迁移演练、认证冒烟、开发证书
deploy/         systemd 单元模板
conf/           配置示例
```

## 服务器安装（Linux + systemd）

安装完成后用 **`bh`** 管理面板，作用与宝塔的 `bt` 相同：直接运行进菜单，也能带子命令用。

### 一键安装

```bash
curl -fsSL https://raw.githubusercontent.com/Austin128/BHmb/main/scripts/install.sh | sudo bash
```

脚本按 GitHub Release → 本地包 → 源码构建的顺序取安装内容。**仓库目前还没有发布 Release**，
所以在线安装会回落到源码构建：服务器上具备 `git`、Go 1.24、`pnpm`、`make` 时脚本会自动克隆仓库
到临时目录构建，缺工具则会明确提示改用下面两种方式：

```bash
# 方式一：服务器上克隆后从源码安装（需 Go 1.24 + pnpm）
git clone https://github.com/Austin128/BHmb.git && cd BHmb
sudo NOVA_FROM_SOURCE=1 bash scripts/install.sh
```

```bash
# 方式二：本地打包后离线安装（服务器不需要任何工具链）
make release                                              # 产物在 dist/
scp dist/novapanel-*-linux-amd64.tar.gz root@<服务器>:/tmp/pkg.tar.gz

# 再到服务器上执行
mkdir -p /tmp/nova && tar -xzf /tmp/pkg.tar.gz -C /tmp/nova
NOVA_PKG=/tmp/pkg.tar.gz bash /tmp/nova/scripts/install.sh
```

安装脚本做的事：解包到 `/opt/novapanel`、按示例生成 `conf/panel.yaml`、注册并启用
systemd 服务、生成 16 位随机 admin 口令并只打印一次、探测 `/api/v1/health` 直到通过、
放行 firewalld/ufw 端口、把 `bh` 链接到 `/usr/local/bin`。

可用环境变量：

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `NOVA_HOME` | `/opt/novapanel` | 安装目录 |
| `NOVA_PORT` | `34567` | 面板端口 |
| `NOVA_VERSION` | 最新 Release | 指定版本 tag |
| `NOVA_PKG` | 空 | 用本地 tar.gz 离线安装 |
| `NOVA_FROM_SOURCE` | `0` | 在仓库目录内从源码构建安装 |

重复执行安装脚本即为升级：只替换程序与脚本，`conf/` 与 `data/` 原样保留。

### bh 命令

```bash
bh                 # 进入交互菜单（等同宝塔 bt）
bh info            # 面板地址、账号、关键路径
bh start|stop|restart|reload
bh status          # systemctl status
bh log [行数]      # 最近日志，bh logf 实时跟踪
bh passwd [用户]   # 重置口令，默认 admin，随机生成并只打印一次
bh port 8443       # 改端口并同步放行防火墙
bh update          # 升级
bh uninstall       # 卸载，默认保留 data/ 与 conf/；--purge 连数据一起删
```

### 安全提醒

- 面板默认监听 `0.0.0.0` 且以 root 运行（后续里程碑要管理运行环境与系统服务）。
  公网暴露前请用安全组或 `security.ip_whitelist` 限制来源 IP。
- 默认自签证书会被浏览器标记不受信，生产环境请把 `server.tls.cert_file`/`key_file`
  换成受信证书。
- `/opt/novapanel/conf/master.key` 丢失会导致既有会话与加密字段不可用，务必纳入备份。

## 快速开始（本地开发）

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
- 文件管理的每个路径都先过 `internal/service/file/pathguard`：黑名单优先于白名单，
  解析软链后二次校验，`file.allow_roots` 为空时整个模块拒绝服务（不会退化为放行 `/`）。
  可访问范围、单文件编辑与上传上限均在 `conf/panel.yaml` 的 `file` 段配置。
- 分片上传的分片与会话元数据写在面板自管的 `file.upload_temp_dir`（默认系统临时目录下
  `novapanel-upload`），不写用户目录；会话 24 小时过期（`400007`），`init` 时惰性清理。
  大于 8 MB 的文件前端自动走 init → chunk → complete，刷新页面后可继续续传。

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
profile 权限与菜单、刷新轮换与旧令牌复用拒绝、文件读写与 Range 下载、
分片上传的断点续传 / 缺片 `400006` / 合并哈希校验 / 秒传 / 放弃会话、
登出后令牌失效、SPA 首页与深链回落、
未知 API 路由返回 JSON 404 信封。CI 工作流见 `.github/workflows/ci.yml`。
