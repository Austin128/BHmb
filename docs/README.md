# NovaPanel 开发文档集

| 项目 | 内容 |
| --- | --- |
| 产品名称 | NovaPanel（中文名「星舵面板」） |
| 文档版本 | v1.0 |
| 更新日期 | 2026-08-17 |
| 适用版本 | NovaPanel v1.0 |
| 状态 | 定稿 |

NovaPanel 是一套面向 Linux 服务器的运维管理面板，覆盖网站与运行环境托管、文件与终端、容器与编排、多节点集群纳管、监控告警、安全合规审计、应用商店与备份恢复。相比宝塔面板一类的单机工具，NovaPanel 的差异化集中在四处：**多节点集群管理**、**容器与编排**、**监控告警与可观测**、**安全与合规审计**。

技术形态是单二进制控制面 + 单二进制被控端：Go 编写，默认 SQLite 存储，无运行时依赖；前端 Vue 3 + Arco Design Vue，构建产物内嵌进二进制。

## 文档地图

| 文件 | 内容 | 主要读者 |
| --- | --- | --- |
| [01-产品定位与需求规格](./01-产品定位与需求规格.md) | 产品定位、竞品对比、功能清单与优先级、角色权限矩阵、用户故事与验收标准、非功能需求、范围外事项 | 产品、全体 |
| [02-系统架构设计](./02-系统架构设计.md) | 分层架构、控制面与被控端职责、通信协议总览、核心业务时序图、12 条 ADR 选型论证、并发与容错模型、容量估算 | 架构、后端 |
| [03-技术栈与工程规范](./03-技术栈与工程规范.md) | 依赖清单、开发环境与 Makefile、目录结构、Go/TS 编码规范、errs 包、配置与迁移规范、Git 工作流、质量门禁、CI/CD | 全体开发 |
| [04-数据库设计](./04-数据库设计.md) | 全量表清单与 DDL、ER 图、三数据库类型映射、索引策略、时序表降采样、GORM 模型示例、种子数据、多租户隔离 | 后端 |
| [05-API接口规范](./05-API接口规范.md) | REST 总则、统一响应契约、认证授权、完整错误码表、接口清单与详细定义、实时通道协议、上传下载协议、OpenAPI | 前后端 |
| [06-前端架构与Arco规范](./06-前端架构与Arco规范.md) | 工程初始化、请求层、路由与权限、Pinia、布局系统、Arco 设计令牌与双主题、12 个业务组件契约、实时数据、性能与无障碍 | 前端 |
| [07-网站与运行环境](./07-网站与运行环境.md) | 网站类型矩阵、OpenResty 配置模板与下发流程、域名与伪静态、SSL/ACME 与 DNS 适配、运行时多版本、数据库服务、FTP、软件商店 | 后端、运维 |
| [08-文件管理与WebSSH](./08-文件管理与WebSSH.md) | 文件管理器、路径安全、分片上传下载、压缩解压、在线编辑、回收站、传输任务、WebSSH 与 PTY、命令录像与高危拦截 | 后端、前端 |
| [09-容器与编排](./09-容器与编排.md) | Docker 环境与 daemon 管理、容器全生命周期、日志与 exec、镜像与仓库、网络与卷、Compose 编排、K8s 纳管、容器安全基线 | 后端 |
| [10-多节点集群与Agent](./10-多节点集群与Agent.md) | 集群架构、Agent 设计、完整 proto 协议、mTLS 与注册流程、节点状态机、心跳与故障判定、批量任务调度、自升级、HA 与规模化 | 架构、后端 |
| [11-监控告警与可观测](./11-监控告警与可观测.md) | 采集器与指标字典、内置时序存储与降采样、日志可观测、告警规则 DSL 与评估器、事件与静默、通知通道、仪表盘与巡检报告 | 后端、前端 |
| [12-安全合规与审计](./12-安全合规与审计.md) | 纵深防御与威胁建模、认证体系与 2FA、Casbin 授权与权限点、审计模型与防篡改、合规映射、防火墙、WAF、入侵检测、基线核查、面板加固 | 安全、后端 |
| [13-应用商店与备份恢复](./13-应用商店与备份恢复.md) | 应用包与 app.yaml 规范、仓库与同步、实例生命周期、计划任务、存储后端抽象、备份策略与 GFS、恢复与迁移、演练与 RTO/RPO | 后端 |
| [14-部署安装与升级](./14-部署安装与升级.md) | 五种部署形态、环境要求、一键安装脚本、Agent 部署、systemd 与容器化、配置全表、novactl、升级回滚、HA、灾备、排错手册 | 运维、DevOps |
| [15-测试与质量保障](./15-测试与质量保障.md) | 质量目标与门禁、测试策略与环境、单测与集成测试、前端与 E2E、性能与混沌、兼容性、安全测试、缺陷管理、CI 卡点、用例集 | 测试、全体 |
| [16-里程碑与迭代排期](./16-里程碑与迭代排期.md) | 总体规划、版本路线图、Sprint 排期与甘特、开发顺序依赖、人力与工作量、里程碑、风险登记册、协作机制、上线运营准备 | PM、管理者 |

## 全局约定速查

写代码前先把这一节的契约记牢，全套文档都以它为准。任何变更需要同步修改本表与对应文档。

| 类别 | 约定 |
| --- | --- |
| 二进制 | 控制面 `novapanel`、被控端 `nova-agent`、命令行 `novactl` |
| 目录 | 根 `/opt/novapanel`，含 `bin/`、`conf/panel.yaml`、`data/{db,vhost,backup,recycle,compose,ssl}`、`logs/`、`web/` |
| 端口 | 面板 34567（HTTPS，支持随机化与安全入口 path）、Agent gRPC 34568 |
| 服务 | systemd `novapanel.service`、`nova-agent.service` |
| 后端栈 | Go 1.24+、Gin v1.10、GORM v1.25、Casbin v2、gRPC-go、robfig/cron v3、coder/websocket、viper、log/slog + lumberjack、swaggo |
| 存储 | 默认 SQLite（modernc.org/sqlite，纯 Go 免 CGO），可选 MySQL 8.0+ / PostgreSQL 14+；KV 用 bbolt，可选 Redis 7 |
| 时序 | 内置 SQLite 多级降采样（raw 7 天 / 1m 30 天 / 1h 1 年 / 1d 永久），可选外接 VictoriaMetrics |
| 前端栈 | Vue 3.5、TypeScript 5.6、Vite 6、Arco Design Vue 2.56、Pinia 2、Vue Router 4、ECharts 5、@xterm/xterm、monaco-editor、pnpm 9 |
| 容器 | Docker Engine API v1.45 + compose-go；K8s 用 client-go（v2.0） |
| API | 前缀 `/api/v1`，资源复数中划线；响应 `{code,message,data,traceId,timestamp}`，`code=0` 为成功 |
| 分页 | 请求 `page`（从 1）/`pageSize`（默认 20、上限 200）/`keyword`/`sort`/`order`；响应 `{list,total,page,pageSize}` |
| 认证 | `Authorization: Bearer <accessToken>`（JWT 15 分钟）+ refreshToken（7 天，HttpOnly Cookie） |
| 请求头 | `X-Node-Id` 目标节点、`X-Request-Id` 链路、`Idempotency-Key` 幂等、`X-Confirm-Token` 危险操作二次确认 |
| 错误码 | 6 位 `MMSSNN`：模块 10 通用、11 认证、12 用户权限、20 集群、30 网站、31 证书、32 数据库、33 运行环境、40 文件、41 终端、50 容器、51 编排、52 仓库、60 监控、61 告警、70 安全、71 审计、80 应用商店、81 计划任务、90 备份、91 升级 |
| 权限点 | `module:resource:action`，action 取 `list/read/create/update/delete/exec/export` |
| 实时通道 | WebSocket 信封 `{v,type,id,topic,nodeId,payload,ts}`，`type` ∈ `sub/unsub/ping/pong/event/ack/error`，topic 为 `<module>.<subject>`，连接前用一次性 ticket（30 秒）换取 |
| Agent 通道 | Agent 主动反向拨号，gRPC 双向流 over mTLS；心跳 10s，3 次未达判离线；重连退避 `min(1s*2^(n-1),60s)±20%` |
| 数据表 | 小写下划线加模块前缀：`sys_ node_ web_ dbs_ env_ file_ term_ ct_ mon_ alert_ sec_ audit_ app_ job_ bak_ ops_`；公共字段 `id`（雪花）、`created_at`、`updated_at`、`deleted_at`、`created_by`、`tenant_id` |
| 业务模块 | dashboard、cluster、website、cert、database、runtime、file、terminal、container、monitor、alert、security、audit、appstore、cron、backup、setting |
| 错误处理 | `errs` 包：`New/Newf/Wrap/Code/Message/HTTPStatus`、`WithDetail/WithField`；响应辅助 `response.OK/Page/Fail` |
| 分层规约 | handler 只做参数校验与响应，service 承载事务，repository 只做数据访问，所有系统调用收口在 `internal/executor`（参数化白名单，禁止拼接 shell） |
| 前端契约 | CSS 变量前缀 `--nova-`；store 为 `useUserStore/usePermissionStore/useAppStore/useNodeStore/useTabStore/useNotificationStore/useTerminalStore`；E2E 选择器 `data-testid` |

## 阅读路径

| 角色 | 建议顺序 |
| --- | --- |
| 新加入的成员 | 01 → 02 → 03，再按分工进对应模块详设 |
| 后端开发 | 03（环境与规范）→ 04（建表）→ 05（接口契约）→ 02（分层与 ADR）→ 负责模块的详设 |
| 前端开发 | 06 → 05（接口与实时通道）→ 01（功能与角色）→ 涉及页面的模块详设 |
| 测试 | 01（验收标准）→ 15（策略与用例）→ 05（契约测试）→ 14（环境搭建） |
| 运维与实施 | 14（部署与升级）→ 10（集群与 Agent）→ 13（备份恢复）→ 12（安全加固） |
| 安全评审 | 12 全文 → 05（认证授权与限流）→ 10（mTLS）→ 08（路径安全与终端审计） |

## 文档维护约定

改动接口先改 [05-API接口规范](./05-API接口规范.md) 再改代码；改动表结构先改 [04-数据库设计](./04-数据库设计.md) 并补 `migrations/` 迁移文件；模块详设与实现不一致时以详设为准，实现有正当理由偏离则必须回写文档并在 PR 说明。跨文档共享的契约（错误码分段、权限点、WebSocket topic、表前缀）只在本 README 的速查表与其定义文档中各存一份，禁止在多处重复定义。

每份文档顶部的文档信息表需随实质性修改更新版本号与日期，仅修订错别字不计入版本变更。

## 变更记录

| 版本 | 日期 | 变更内容 |
| --- | --- | --- |
| v1.0 | 2026-08-17 | 首版定稿，覆盖 16 份文档 |
