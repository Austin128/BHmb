# 青垣面板 · API 接口规范

## 文档信息

| 项目 | 内容 |
| --- | --- |
| 文档名称 | 05-API 接口规范 |
| 文档版本 | v1.0 |
| 更新日期 | 2026-08-17 |
| 状态 | 定稿 |
| 适用版本 | 青垣面板 v1.0 |
| 产品代号 | qingyuan |
| 覆盖内容 | REST 设计总则与版本策略、统一响应契约与 response 包实现、请求头与分页校验规范、认证授权与 JWT/API Token/Casbin、全量错误码表、限流防重放与危险操作二次确认、核心接口清单总表、关键接口详细定义、WebSocket 与 SSE 实时通道协议、分片上传下载协议、跨节点调用语义、OpenAPI 3 与 SDK、接口变更废弃流程、调试示例 |
| 关联文档 | [系统架构设计](./02-系统架构设计.md)、[技术栈与工程规范](./03-技术栈与工程规范.md)、[数据库设计](./04-数据库设计.md)、[前端架构与 Arco 规范](./06-前端架构与Arco规范.md)、[网站与运行环境](./07-网站与运行环境.md)、[文件管理与 WebSSH](./08-文件管理与WebSSH.md)、[容器与编排](./09-容器与编排.md)、[多节点集群与 Agent](./10-多节点集群与Agent.md)、[监控告警与可观测](./11-监控告警与可观测.md)、[安全合规与审计](./12-安全合规与审计.md) |

## 目录

- [5.1 接口设计总则](#51-接口设计总则)
- [5.2 统一响应契约](#52-统一响应契约)
- [5.3 请求规范](#53-请求规范)
- [5.4 认证与授权](#54-认证与授权)
- [5.5 错误码全量清单](#55-错误码全量清单)
- [5.6 限流、防重放与安全约束](#56-限流防重放与安全约束)
- [5.7 核心接口清单总表](#57-核心接口清单总表)
- [5.8 关键接口详细定义](#58-关键接口详细定义)
- [5.9 实时通道协议](#59-实时通道协议)
- [5.10 文件上传下载协议](#510-文件上传下载协议)
- [5.11 跨节点调用语义](#511-跨节点调用语义)
- [5.12 OpenAPI 3 与 SDK](#512-openapi-3-与-sdk)
- [5.13 接口变更与废弃流程](#513-接口变更与废弃流程)
- [5.14 调试示例](#514-调试示例)

## 5.1 接口设计总则

### 5.1.1 REST 风格与三条例外

面板 API 以资源为中心，但运维场景大量存在「不产生资源、只改变状态」的动作（重启服务、下发配置、清理镜像）。强行套 REST 会造出 `PUT /websites/{id}/state {value:"reloaded"}` 这类难以审计的接口，因此明确三条例外：

| 例外 | 形式 | 判据 | 示例 |
| --- | --- | --- | --- |
| 动作型子资源 | `POST /{资源}/{id}/{动作}` | 动作是动词、不可幂等重放、需要独立权限点或审计粒度 | `POST /container/containers/{id}/restart`、`POST /website/certs/{id}/renew` |
| 查询型 POST | `POST /{资源}/{动作}` | 查询条件超出 URL 长度（时序查询、日志检索、多节点选择器），或含敏感条件不宜进访问日志 | `POST /monitor/metric/query`、`POST /monitor/log/search`、`POST /file/search` |
| 批量端点 | `POST /{资源}/batch` | 单次操作 N 个同类对象，需要部分成功语义 | `POST /container/containers/batch`、`POST /alert/event/batch` |

除以上三类，一律用标准方法与集合/成员语义。禁止出现 `GET /getSiteList`、`POST /deleteSite` 这类 RPC 化命名。

### 5.1.2 URI 规范

```text
https://<host>:34567/api/v1/<module>/<resource>[/<id>[/<sub-resource>|<action>]]
                     └─前缀─┘└─模块─┘└──复数资源──┘
```

| 规则 | 约定 | 正例 | 反例 |
| --- | --- | --- | --- |
| 前缀 | 固定 `/api/v1`，不带尾斜杠 | `/api/v1/cluster/nodes` | `/v1/api/nodes` |
| 第一段 | 模块名，单数小写，与 `sys_permission.module` 语义对应，共 20 个 | `/website/...`、`/container/...` | `/websites/...` |
| 第二段 | 资源名复数、小写、多词中划线 | `/cluster/nodes`、`/alert/rules`、`/security/waf-rules` | `/cluster/nodeList`、`/security/waf_rules` |
| 路径参数 | 用资源自身主键，雪花 ID 十进制字符串；名称型主键（卷名、镜像 ref）需 URL 编码 | `/container/volumes/{name}` | `/container/volumes?name=` |
| 查询参数 | camelCase | `?nodeId=&pageSize=` | `?node_id=&page_size=` |
| 动作段 | 小写动词或动词短语，多词中划线 | `/batch-exec`、`/test-notify` | `/batchExec` |
| 大小写 | 路径全小写，路由不区分大小写但只暴露小写形式 | — | `/Website/Sites` |

模块前缀全集（20 个）：`auth`、`user`、`cluster`、`website`、`database`、`env`、`file`、`terminal`、`container`、`monitor`、`alert`、`notify`、`security`、`audit`、`app`、`job`、`backup`、`system`、`public`、`ws`。其中 `public` 为免鉴权白名单（Agent 引导、公开设置），`ws` 为实时通道命名空间。

历史遗留：[产品定位与需求规格](./01-产品定位与需求规格.md) 与 [系统架构设计](./02-系统架构设计.md) 的示例中出现过 `/api/v1/cluster-nodes`、`/api/v1/sites`、`/api/v1/nodes` 三种写法。本规范以模块详设为准（`/api/v1/cluster/nodes` 见 [多节点集群与 Agent](./10-多节点集群与Agent.md)，`/api/v1/website/sites` 见 [网站与运行环境](./07-网站与运行环境.md)），三个旧路径注册为兼容路由，行为见 [5.13](#513-接口变更与废弃流程)。

### 5.1.3 HTTP 方法与状态码

| 方法 | 语义 | 请求体 | 成功状态码 | 备注 |
| --- | --- | --- | --- | --- |
| GET | 读取，无副作用 | 无 | 200 | 不得因 GET 产生审计写入以外的状态变化 |
| POST | 创建 / 动作 / 查询型 | JSON 或 multipart | 200（同步完成）、202（转异步任务） | 创建成功在 `data` 里返回完整对象或至少 `id` |
| PUT | 整体替换 | JSON 全量字段 | 200 | 未传字段按「置为默认值」处理，前端必须先 GET 再 PUT |
| PATCH | 局部更新 | JSON 仅变更字段 | 200 | 仅用于开关型字段（`enabled`、`status`） |
| DELETE | 删除 | 通常无；批量删除允许 JSON | 200 | 幂等：删不存在的资源返回 200 而非 404 |

状态码使用边界（`response.Fail` 由 `errs.HTTPStatus` 决定，实现见 [技术栈与工程规范](./03-技术栈与工程规范.md) 的 3.8.2）：

| 状态码 | 使用场景 | 典型错误码 |
| --- | --- | --- |
| 200 | 成功；业务软失败（`code != 0` 但属可预期业务分支） | 0 |
| 202 | 请求已受理，转异步任务，`data.taskId` 必填 | 0 |
| 400 | 参数格式/取值不合法、配置校验失败 | 100001、300305 |
| 401 | 未登录、Token 过期、需要二次验证 | 110001、110004、110005 |
| 403 | 权限不足、被安全策略拒绝、缺二次确认令牌 | 120003、200031、400309 |
| 404 | 资源不存在 | 100002 |
| 409 | 状态冲突、名称/端口占用、并发写冲突 | 100003、300203 |
| 413 | 请求体超限 | 100007 |
| 422 | 语义校验失败（结构正确但业务规则不通过） | 510423 |
| 423 | 账号锁定、节点维护中 | 110003 |
| 429 | 限流 | 100005 |
| 500 | 服务内部错误 | 100004 |
| 503 | 目标节点离线、依赖组件不可用 | 200021 |
| 504 | 跨节点调用超时 | 100006 |
| 507 | 磁盘空间不足 | 900013 |

[前端架构与 Arco 规范](./06-前端架构与Arco规范.md) 的联调 checklist 中「错误也返回 HTTP 200 除鉴权类」一句与本表不一致，以本表为准：HTTP 状态码承载语义、`code` 承载精确原因，两者同时可用；前端拦截器已同时处理 `code != 0`（响应成功分支）与非 2xx（错误分支），无需改动。

### 5.1.4 幂等性要求

| 类别 | 幂等 | 实现方式 | 重复调用结果 |
| --- | --- | --- | --- |
| GET / HEAD | 是 | 天然 | 相同 |
| PUT / PATCH | 是 | 全量或字段覆盖 | 相同（最后写入生效） |
| DELETE | 是 | 软删除 + 不存在视为已删 | 200，`data.affected=0` |
| POST 创建类 | 条件幂等 | `Idempotency-Key` 头 + 唯一索引兜底 | 返回首次结果，`data` 完全一致 |
| POST 动作类（启停、reload） | 是 | 目标态收敛，已是目标态直接返回 | 200，`data.changed=false` |
| POST 动作类（重启、kill、备份） | 否 | 每次产生新记录 | 产生新 `recordId`/`taskId` |
| POST 批量类 | 条件幂等 | `Idempotency-Key` 覆盖整批 | 返回首批结果 |
| POST 查询类 | 是 | 无副作用 | 相同 |

强制携带 `Idempotency-Key` 的接口（缺失返回 `100008`）：创建网站、创建容器、创建数据库、安装应用、创建备份、执行恢复、发起升级、批量执行命令。这些接口的重复提交会产生真实的资源消耗或不可逆变更，不能只靠唯一索引兜底。

### 5.1.5 版本策略与并存期

| 项 | 约定 |
| --- | --- |
| 版本位置 | URL 路径段 `/api/v1`，不用 Header 协商（便于反代按前缀分流与审计日志直读） |
| 递增条件 | 仅在出现无法兼容的整体重构时递增到 `/api/v2`；单接口不兼容变更走 [5.13](#513-接口变更与废弃流程) 的废弃流程，不递增大版本 |
| 并存期 | `v1` 与 `v2` 至少并存两个 MINOR 版本（约 6 个月），期间 `v1` 只接受安全修复 |
| 路由实现 | `router.Group("/api/v1")` 与 `/api/v2` 各自独立注册，共用 service 层，禁止在 handler 里按版本分支 |
| WebSocket | 通道版本由信封 `v` 字段承载（当前 `v=1`），与 URL 版本解耦，可独立演进 |
| Agent 协议 | gRPC 协议版本独立于 HTTP API，见 [多节点集群与 Agent](./10-多节点集群与Agent.md) 的 10.5.8 |

## 5.2 统一响应契约

### 5.2.1 信封结构

所有 HTTP 接口（含错误、含 202 异步受理）返回同一信封，唯一例外是文件下载与导出的二进制流。

```json
{
  "code": 0,
  "message": "ok",
  "data": { "id": "1832746500123456789", "name": "blog" },
  "traceId": "01J8Z4K2Q7X9N3M5P1R6T8V0",
  "timestamp": 1755400000
}
```

| 字段 | 类型 | 必返 | 说明 |
| --- | --- | --- | --- |
| `code` | int | 是 | 0 成功；非 0 为 6 位业务错误码（见 [5.5](#55-错误码全量清单)） |
| `message` | string | 是 | 面向最终用户的中文提示，按 `Accept-Language` 本地化；成功固定 `ok` |
| `data` | any | 是 | 成功时为业务数据；失败时为 `null`，仅校验类错误可返回 `{"fields":{...}}` |
| `traceId` | string | 是 | 26 位 ULID，贯穿控制面与 Agent 日志，用户可凭此报障 |
| `timestamp` | int64 | 是 | 服务端 Unix **秒**，用于客户端时钟偏移提示 |

### 5.2.2 Go 结构体定义

```go
// internal/api/v1/response/response.go

// Result 是所有接口的统一信封。
type Result[T any] struct {
	Code      int    `json:"code"`
	Message   string `json:"message"`
	Data      T      `json:"data"`
	TraceID   string `json:"traceId"`
	Timestamp int64  `json:"timestamp"`
}

// PageData 是分页接口的 data 部分。
type PageData[T any] struct {
	List     []T   `json:"list"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"pageSize"`
}

// PageResult 供 swag 注解引用：response.PageResult[model.NodeVO]。
type PageResult[T any] = Result[PageData[T]]

// AsyncData 是 202 响应的 data 部分。
type AsyncData struct {
	TaskID string `json:"taskId"`
	Topic  string `json:"topic"` // 进度订阅主题，如 task.progress
}
```

### 5.2.3 response 包实现

```go
// OK 返回成功响应。data 为 nil 时序列化为 null，不改写成 {}。
func OK[T any](c *gin.Context, data T) {
	c.JSON(http.StatusOK, Result[T]{
		Code: 0, Message: "ok", Data: data,
		TraceID: ctxutil.TraceID(c), Timestamp: time.Now().Unix(),
	})
}

// Page 返回分页响应，page/pageSize 从已归一化的查询参数回填。
func Page[T any](c *gin.Context, list []T, total int64) {
	if list == nil {
		list = make([]T, 0) // 空列表必须是 []，不能是 null
	}
	q := ctxutil.PageQuery(c)
	OK(c, PageData[T]{List: list, Total: total, Page: q.Page, PageSize: q.PageSize})
}

// Accepted 返回 202，用于长任务转异步。
func Accepted(c *gin.Context, taskID, topic string) {
	c.JSON(http.StatusAccepted, Result[AsyncData]{
		Code: 0, Message: "ok", Data: AsyncData{TaskID: taskID, Topic: topic},
		TraceID: ctxutil.TraceID(c), Timestamp: time.Now().Unix(),
	})
}

// Fail 把 error 映射为响应：错误码 + 本地化文案 + HTTP 状态码。
// Detail/Fields 只进日志，不返回给前端；super_admin 且开启 debug 时附加 debug 字段。
func Fail(c *gin.Context, err error) {
	code := errs.Code(err)
	status := errs.HTTPStatus(code)
	msg := i18n.ErrorMessage(c, code, errs.Message(err)) // 未登记则回落 errs.Message

	var e *errs.Error
	if errors.As(err, &e) {
		slog.ErrorContext(c.Request.Context(), "api failed",
			slog.Int("code", code), slog.String("detail", e.Detail),
			slog.Any("fields", e.Fields), slog.String("traceId", ctxutil.TraceID(c)))
	}

	body := Result[any]{
		Code: code, Message: msg, Data: nil,
		TraceID: ctxutil.TraceID(c), Timestamp: time.Now().Unix(),
	}
	if code == errs.CodeInvalidParam && e != nil && e.Fields != nil {
		body.Data = map[string]any{"fields": e.Fields} // 表单逐字段回填
	}
	if ctxutil.IsSuperAdmin(c) && config.Get().Server.Debug && e != nil {
		c.Header("X-Debug-Detail", url.QueryEscape(e.Detail))
	}
	c.AbortWithStatusJSON(status, body)
}
```

### 5.2.4 traceId 生成与贯穿

| 环节 | 行为 |
| --- | --- |
| 入口 | `RequestID` 中间件读 `X-Request-Id`；合法（≤64 字符、仅 `[A-Za-z0-9_-]`）则记为 `requestId`，否则丢弃并记 warn |
| 生成 | `traceId` 一律由服务端生成 ULID（26 字符、字典序即时间序），不接受客户端指定，避免伪造串日志 |
| 传播 | 写入 `gin.Context` 与 `context.Context`；`slog` 全局 handler 自动附加；跨节点调用通过 gRPC metadata `nova-trace-id` 传给 Agent |
| 落库 | `audit_operation_log.trace_id` 与 `request_id` 两列分别落盘，可从审计页直接跳日志检索 |
| 回传 | 响应体 `traceId` 字段 + 响应头 `X-Trace-Id`（供 curl/网关采集） |
| 关联 | 一次批量任务的 N 个节点子任务共用同一 `traceId`，`node_exec_task.trace_id` 记录 |

### 5.2.5 时间与时区约定

| 场景 | 格式 | 示例 | 理由 |
| --- | --- | --- | --- |
| 业务时间字段（`createdAt`、`expireAt`、`lastHeartbeatAt`） | RFC3339 带时区偏移 | `2026-08-17T10:30:00+08:00` | 人可读、可直接进 Excel、跨时区无歧义 |
| 信封 `timestamp` | Unix 秒 | `1755400000` | 只用于时钟偏移比对，无需可读 |
| WebSocket 信封 `ts` | Unix **毫秒** | `1755412800000` | 前端计算推送延迟与丢帧，秒级精度不够 |
| 时序数据点时间戳 | Unix 毫秒 | `1755412800000` | 与图表库、TSDB 对齐，避免逐点字符串解析 |
| 时间区间查询参数 | RFC3339 或 `-1h`/`-7d` 相对量 | `from=-6h&to=now` | 相对量免去前端拼接，且刷新时窗口自动滚动 |
| cron 表达式时区 | 独立 `timezone` 字段（IANA 名） | `Asia/Shanghai` | 对应 `job_cron.timezone`，不依赖服务器时区 |

服务端内部一律 UTC 存储（`DATETIME(3)`），序列化时按 `sys_user.timezone` 转换；未登录接口按 `sys_setting` 的面板默认时区。禁止在同一响应里混用两种时间格式。

### 5.2.6 字段命名与空值

| 规则 | 约定 |
| --- | --- |
| 命名 | JSON 一律 camelCase；数据库列 snake_case，由 DTO 显式映射，禁止直接序列化 GORM model |
| 布尔 | 前缀 `is`/`has`/`enable`，如 `isBuiltin`、`hasValue`、`enableSSL`；不用 `flag`、`type=0/1` |
| 枚举 | 小写字符串字面量，与 [数据库设计](./04-数据库设计.md) 的枚举取值完全一致，禁止数字枚举 |
| 数组 | 空数组返回 `[]`，绝不返回 `null`（前端 `v-for` 不做判空） |
| 对象 | 无值返回 `null`（区别于「空对象」语义） |
| 零值 | 数值零值原样返回 `0`，不省略字段；`omitempty` 仅允许用于「未设置与 0 语义不同」的可选入参 DTO |
| 缺失字段 | 响应结构固定，字段永不因无值而消失（前端不做 `in` 判断） |
| 敏感字段 | 密码、私钥、SK 一律不回显，改返回 `hasValue: true` 与 `hint`（后四位）；对应 `*_enc` 列 |
| 时长 | 统一带单位后缀：`timeoutSec`、`durationMs`、`sizeBytes`、`quotaMB`、`bandwidthKb` |

### 5.2.7 大整数 ID 序列化为字符串

所有雪花 ID（`BIGINT`，19 位十进制）在 JSON 中序列化为 **string**，包括 `id`、`nodeId`、`siteId`、`taskId`、`ruleId` 以及数组形式的 `nodeIds`。

理由：JavaScript `Number` 为 IEEE 754 双精度，安全整数上限 `2^53-1 = 9007199254740991`（16 位）。雪花 ID 如 `1832746500123456789` 超出该范围，`JSON.parse` 会静默丢精度变成 `1832746500123456800`，导致「详情页打开的是另一条记录」这类无法自查的错误。前端 axios 默认不做 BigInt 转换，因此必须由后端保证。

| 项 | 约定 |
| --- | --- |
| 实现 | DTO 中声明为 `json:"id,string"`，或使用 `types.ID`（`int64` 别名 + 自定义 `MarshalJSON`） |
| 入参 | 同时接受字符串与数字（`json:",string"` 的 `UnmarshalJSON` 兼容），避免旧客户端报错 |
| 例外 | 自增小整数（`page`、`pageSize`、`exitCode`、`port`、`total`）保持 number |
| 时间戳 | 毫秒时间戳 `1755412800000`（13 位）在安全范围内，保持 number |
| 前端类型 | `types/api.ts` 中 ID 字段类型声明为 `string`，比较用 `===` 而非 `==` |
| 校验 | 契约测试断言 `typeof body.data.id === 'string'`，见 [5.12.4](#5124-契约测试要求) |

## 5.3 请求规范

### 5.3.1 请求头全表

| 头 | 必填 | 取值 | 说明 |
| --- | --- | --- | --- |
| `Authorization` | 是（白名单除外） | `Bearer <accessToken>` | JWT，15 分钟有效；API Token 调用见 [5.4.6](#546-api-token-签名机制) |
| `Content-Type` | 有体时必填 | `application/json;charset=utf-8` 或 `multipart/form-data` | 仅分片上传与证书导入用 multipart |
| `Accept` | 否 | `application/json`（默认）、`text/event-stream`（SSE）、`text/csv`（导出） | 导出接口按 Accept 决定 CSV/JSON |
| `Accept-Language` | 否 | `zh-CN`（默认）、`en-US` | 决定 `message` 与 `error.<code>` 文案；无匹配时回落 `zh-CN` |
| `X-Node-Id` | 视接口 | `node_server.id` 十进制字符串 | 目标节点；缺省为控制面本机节点。清单表标注「需」的接口缺失该头时返回 `200001` 而非默认本机 |
| `X-Request-Id` | 建议 | ≤64 字符 `[A-Za-z0-9_-]` | 客户端请求标识，落审计 `request_id`，与服务端 `traceId` 关联 |
| `Idempotency-Key` | 视接口 | UUIDv4 | 见 [5.6.4](#564-idempotency-key-语义) |
| `X-Confirm-Token` | 视接口 | 服务端签发的一次性令牌 | 危险操作二次确认，见 [5.6.3](#563-危险操作二次确认) |
| `X-Tenant-Id` | 否 | 租户 ID | 仅超管跨租户操作时显式指定，普通用户由 Token 推导，显式传入不一致返回 `120005` |
| `If-Match` | 否 | `web_site.config_version` 等版本号 | 乐观锁；不匹配返回 `100003` |
| `Range` | 否 | `bytes=<start>-<end>` | 文件下载断点续传，见 [5.10.6](#5106-下载与-range) |
| `Sec-WebSocket-Protocol` | WS 必填 | `nova.hub.v1` / `nova.term.v1` / `nova.file.v1` | 子协议标识；票据走 query，不在此头放 Token |
| `User-Agent` | 建议 | — | 落审计 `user_agent`，用于区分浏览器 / novactl / SDK |

响应头：`X-Trace-Id`（同 `traceId`）、`X-RateLimit-Limit`、`X-RateLimit-Remaining`、`Retry-After`（429/503）、`Content-Disposition`（下载，中文名用 `filename*=UTF-8''`）、`Deprecation`/`Sunset`（废弃接口）。

### 5.3.2 分页、排序与过滤

| 参数 | 类型 | 默认 | 约束 |
| --- | --- | --- | --- |
| `page` | int | 1 | ≥1；越界返回空 `list` 与真实 `total`，不报错 |
| `pageSize` | int | 20 | 1–200；`>200` 直接返回 `100001` 而非静默截断（避免前端误以为拿到全量） |
| `keyword` | string | 空 | ≤128 字符，按模块定义的字段做 `LIKE '%kw%'`；`%` 与 `_` 转义 |
| `sort` | string | 模块默认 | 必须命中白名单，见下 |
| `order` | string | `desc` | `asc` / `desc`，其他值返回 `100001` |
| `nodeId` | string | 空 | 部分列表支持跨节点聚合查询，与 `X-Node-Id` 二选一（同时出现以查询参数为准） |
| `startTime` / `endTime` | string | 空 | RFC3339 或相对量；仅 `endTime` 缺省时取 `now` |

`sort` 白名单校验实现（禁止把用户输入拼进 `ORDER BY`）：

```go
// internal/api/v1/middleware/pagination.go
var sortWhitelist = map[string]map[string]string{
	// 路由分组 → 允许的 sort 值 → 实际列名
	"cluster/nodes": {"createdAt": "created_at", "name": "name", "status": "status", "lastHeartbeatAt": "last_heartbeat_at"},
	"website/sites": {"createdAt": "created_at", "name": "name", "expireAt": "expire_at"},
	"alert/events":  {"firstAt": "first_at", "lastAt": "last_at", "severity": "severity"},
}

func ResolveSort(group, sort, order string) (string, error) {
	if sort == "" {
		return "created_at DESC", nil
	}
	col, ok := sortWhitelist[group][sort]
	if !ok {
		return "", errs.ErrInvalidParam.WithField("sort", sort).
			WithDetail("sort %q not in whitelist of %s", sort, group)
	}
	if order != "asc" && order != "desc" {
		return "", errs.ErrInvalidParam.WithField("order", order)
	}
	return col + " " + strings.ToUpper(order), nil
}
```

`sort` 支持多字段，逗号分隔（`sort=severity,firstAt&order=desc,asc`），两个数组长度必须相等且各自逐项校验。列表接口的默认排序必须与索引一致，否则大表分页会退化为全表排序（索引设计见 [数据库设计](./04-数据库设计.md)）。

### 5.3.3 批量操作与部分成功

批量端点固定 `POST /{module}/{resource}/batch`，请求体三段式：

```json
{
  "action": "stop",
  "ids": ["1832746500123456789", "1832746500123456790"],
  "selector": { "nodeIds": [], "groupIds": [], "labels": { "env": "prod" } },
  "params": { "timeout": 30, "force": false },
  "continueOnError": true
}
```

| 字段 | 说明 |
| --- | --- |
| `action` | 动作名，必须在该资源的批量动作白名单内；不同 action 对应不同权限点，逐项校验 |
| `ids` | 显式对象列表，单次上限 500（告警事件同 [监控告警与可观测](./11-监控告警与可观测.md) 的 500 上限）；容器批量上限 200 |
| `selector` | 与 `ids` 二选一，用于按节点/分组/标签选目标；两者同时传返回 `100001` |
| `params` | 动作参数，按 action 定义 |
| `continueOnError` | `true`（默认）遇错继续；`false` 首个失败即中止，已成功项不回滚 |

部分成功统一返回 **HTTP 200 + `code: 0`**，由 `data` 表达逐项结果；调用方必须检查 `failed`，不能只看 HTTP 状态：

```json
{
  "code": 0, "message": "ok",
  "data": {
    "total": 3, "succeeded": 2, "failed": 1, "allSucceeded": false,
    "results": [
      { "id": "1832746500123456789", "ok": true,  "code": 0,      "message": "ok" },
      { "id": "1832746500123456790", "ok": true,  "code": 0,      "message": "ok" },
      { "id": "1832746500123456791", "ok": false, "code": 500409, "message": "容器正在运行，请先停止或使用 force" }
    ]
  },
  "traceId": "01J8Z4K2Q7X9N3M5P1R6T8V0", "timestamp": 1755400000
}
```

若整批因前置校验失败（权限不足、`ids` 为空、action 非法）而一项都未执行，则返回对应错误码与非 200 状态，`data` 为 `null`。批量耗时预估超过 30 秒的（跨节点、镜像清理、批量备份）改走 202 异步任务，见 [5.11.5](#5115-长任务转异步)。

### 5.3.4 字段校验规则表

校验在 handler 层用 `binding` 标签 + `validator.v10` 自定义规则完成，service 层对跨字段规则二次校验。失败统一返回 `100001`，`data.fields` 逐字段给出原因供表单回填。

| 字段类型 | 规则 | 正则 / 范围 | 失败错误码 |
| --- | --- | --- | --- |
| 域名 | 单标签 ≤63、总长 ≤253、不以 `-` 起止；支持泛域名 `*.` 前缀（仅一级）；禁止纯数字顶级 | `^(\*\.)?([a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,63}$` | `300201` |
| IPv4 | 点分十进制，禁止前导零 | `net.ParseIP` + 段数校验 | `100001` |
| IPv6 | 标准或压缩写法 | `net.ParseIP` | `100001` |
| CIDR | `IP/前缀`，IPv4 前缀 0–32、IPv6 0–128；主机位必须为 0 | `net.ParseCIDR` 且 `ip.Equal(ipnet.IP)` | `700102` |
| 端口 | 1–65535；业务监听端口限 1024–65535；禁止 34567/34568 与 `sys_setting` 保留列表 | — | `500414` |
| 端口区间 | `start-end`，`end > start`，跨度 ≤1000 | — | `100001` |
| 绝对路径 | 以 `/` 开头、无 `..`、无 NUL、长度 ≤4096，`filepath.Clean` 后必须落在白名单根内 | 见 [文件管理与 WebSSH](./08-文件管理与WebSSH.md) 的 8.3 | `400401`–`400403` |
| 文件名 | 长度 ≤255，禁止 `/` 与 NUL，禁止 `.`/`..`，Windows 保留名拒绝 | `^[^/\x00]{1,255}$` | `400301` |
| cron 表达式 | 5 或 6 字段（含秒），用 `robfig/cron` 标准解析器；最小间隔 ≥1 分钟；`@every`/`@daily` 宏允许 | — | `810101` |
| 密码强度 | 长度 8–64；至少含大写、小写、数字、符号中的 3 类；不得包含用户名；黑名单 Top1000 弱口令拒绝 | — | `120001` |
| 名称标识（站点名、容器名、卷名） | 小写起始，长度 2–63 | `^[a-z0-9][a-z0-9_-]{1,62}$` | `100001` |
| 容器名 | Docker 规则，允许大写与点 | `^[a-zA-Z0-9][a-zA-Z0-9_.-]{1,63}$` | `500410` |
| 用户名 | 小写字母起始，长度 3–64 | `^[a-z][a-z0-9_]{2,63}$` | `120002` |
| 邮箱 | RFC5322 简化 + 长度 ≤128 | `validator` 的 `email` | `100001` |
| URL | 仅 `http`/`https`；SSRF 白名单校验（禁 `127.0.0.1:34567`、禁内网段除显式放行） | — | `700301` |
| 权限点 | `module:resource:action` 三段，各段 `[a-z][a-z0-9]*`，action 在 7 个枚举内 | `^[a-z][a-z0-9]*(:[a-z][a-z0-9]*){2}$` | `120006` |
| JSON 文本字段 | 必须能 `json.Unmarshal`，深度 ≤32，长度 ≤64KB | — | `100001` |
| 正则（WAF 规则、日志解析） | `regexp.Compile` 成功且编译耗时 <50ms；禁止嵌套量词导致的灾难性回溯 | — | `700201` |

## 5.4 认证与授权

### 5.4.1 登录时序

```mermaid
sequenceDiagram
    autonumber
    participant W as 浏览器 / novactl
    participant MW as 中间件链
    participant H as auth handler
    participant S as auth service
    participant TK as security/token
    participant KV as bbolt / Redis
    participant DB as sys_user / sys_session

    W->>MW: POST /api/v1/auth/login {username, password, captchaToken?}
    MW->>MW: RateLimit（同 IP 5 次/分钟）+ RequestID
    MW->>H: 放行（白名单，无需 JWT）
    H->>S: Login(ctx, cmd)
    S->>DB: 查用户；校验 status、allow_ip_json、locked_until
    S->>S: bcrypt 比对 password_hash（cost=12）
    alt 密码错误
        S->>DB: login_fail_count+1；达 5 次写 locked_until = now+15m
        S-->>W: 401 {code:110002}
    end
    alt 需要 2FA（two_factor_enabled=1）
        S->>KV: 存 mfaToken（TTL 5 分钟，绑定 userId + deviceFp）
        S-->>W: 200 {code:0, data:{mfaRequired:true, mfaToken, methods:["totp","recovery"]}}
        W->>H: POST /api/v1/auth/2fa/verify {mfaToken, code}
        H->>S: VerifyTOTP：last_code_step 严格递增，±1 时间窗
        alt 验证码错误
            S->>DB: verify_fail_count+1（5 次/10 分钟锁定）
            S-->>W: 401 {code:110007}
        end
    end
    S->>TK: 签发 accessToken(15m) + refreshToken(7d)
    TK->>KV: 存 refresh 句柄 jti → {userId, deviceFp, exp, rotateCount}
    S->>DB: 写 sys_session、sys_login_log、audit_operation_log
    S-->>W: 200 + Set-Cookie: nova_rt=<refreshToken>; HttpOnly; Secure; SameSite=Strict; Path=/api/v1/auth
    Note over W: accessToken 只存内存（Pinia），不落 localStorage
    W->>MW: 业务请求 Authorization: Bearer <accessToken>
    MW->>TK: 解析 JWT → 校验 jti 未吊销 → 注入 ctx
    MW->>MW: Casbin 判定 (sub, dom, obj, act)
    MW-->>W: 401 {code:110004} accessToken 过期
    W->>H: POST /api/v1/auth/refresh（自动带 Cookie）
    H->>TK: 校验 refreshToken + 设备指纹；旋转 jti
    alt 检测到旧 jti 复用
        TK->>KV: 吊销该用户全部会话，写高危审计
        TK-->>W: 401 {code:110006}
    end
    TK-->>W: 新 accessToken + 新 refreshToken Cookie
    W->>MW: 重放原请求
```

### 5.4.2 JWT Claims

算法 HS512，密钥取 `security.jwt_secret`（安装时随机生成 64 字节，轮换见 [部署安装与升级](./14-部署安装与升级.md)）。不用 RS256：单控制面无需公钥分发，HMAC 校验快一个数量级。

| Claim | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `iss` | string | 是 | 固定 `novapanel` |
| `sub` | string | 是 | `sys_user.id` 字符串 |
| `aud` | string | 是 | `panel`（浏览器会话）/ `cli`（novactl）/ `api`（API Token 换取的短 Token） |
| `jti` | string | 是 | 会话唯一 ID，对应 `sys_session.jti`，吊销判定的键 |
| `iat` / `exp` | int64 | 是 | 签发与过期（Unix 秒），`exp - iat = 900` |
| `nbf` | int64 | 是 | 等于 `iat`，防时钟提前 |
| `uname` | string | 是 | 登录名，仅用于日志与审计冗余，不作鉴权依据 |
| `tid` | string | 是 | `tenant_id`，Casbin domain |
| `rol` | []string | 是 | 角色 code 列表，如 `["ops"]` |
| `sup` | bool | 是 | `is_super`，为 true 时中间件短路放行 |
| `scp` | []string | 否 | 作用域限制（API Token 换取的 Token 才有），为空表示继承用户全部权限 |
| `dfp` | string | 是 | 设备指纹哈希（UA + Accept-Language + 随机盐的 SHA-256 前 16 字节） |
| `stp` | int64 | 否 | step-up 通过时间（Unix 秒），危险操作要求 `now - stp < 300` |
| `pwd` | int64 | 是 | `password_updated_at` 的 Unix 秒；改密后旧 Token 因该值不匹配立即失效 |

`accessToken` 不放权限点列表：218 个权限点会把 Token 撑到数 KB 且改权限无法即时生效。权限在中间件里按 `sub` 查 L0 进程内缓存（Casbin 策略常驻），改权限后主动 reload。

### 5.4.3 Token 刷新与并发处理

| 项 | 约定 |
| --- | --- |
| refreshToken 载体 | HttpOnly + Secure + SameSite=Strict Cookie，`Path=/api/v1/auth`，7 天；不放响应体，避免 XSS 读取 |
| 存储 | 只存 `sys_session.refresh_token_hash`（SHA-256）与 KV 中的 jti 句柄；明文只在 Set-Cookie 出现一次 |
| 旋转 | 一次性旋转：每次刷新签发新 refreshToken 并使旧 jti 立即失效，`rotateCount+1` |
| 复用检测 | 已失效 jti 再次出现 → 判定令牌被窃 → 吊销该用户**全部**会话，返回 `110006`，写 `high_risk` 审计并触发告警 |
| 并发刷新 | 前端单飞（见 [前端架构与 Arco 规范](./06-前端架构与Arco规范.md) 的 6.4.3）；服务端对同一 jti 加 5 秒 KV 锁，锁内重复请求返回同一新 Token（幂等窗口），避免竞态互相作废 |
| 滑动上限 | 单条会话最多滑动 30 天（`rotateCount * 7d` 上限），到顶必须重新登录 |
| 提前刷新 | 允许在 `exp` 前任意时刻刷新；`accessToken` 剩余 <2 分钟时前端主动刷新，避免长请求中途过期 |

吊销名单：JWT 无状态，吊销依赖 `jti` 黑名单。KV 中 `revoked:<jti> → exp`，TTL 等于 Token 剩余寿命（≤15 分钟），因此黑名单规模上限为「15 分钟内的活跃会话数」，内存可控。触发吊销的动作：登出、改密、禁用用户、角色变更、管理员强制下线、复用检测、`novactl` 重置密码。控制面多实例时黑名单写 Redis；单机 bbolt。

### 5.4.4 2FA 二次校验流程

| 阶段 | 接口 | 要点 |
| --- | --- | --- |
| 登录触发 | `POST /auth/login` | 返回 `mfaRequired:true` + `mfaToken`（TTL 5 分钟、一次性、绑定 deviceFp），**不返回** accessToken |
| 提交验证码 | `POST /auth/2fa/verify` | 入参 `{mfaToken, code, method}`；`method` 取 `totp`/`recovery`；成功后走正常签发流程 |
| 恢复码登录 | 同上，`method=recovery` | 使用后标记 `used_at`；登录成功后强制跳转重新绑定页，该会话不授予 step-up 资格 |
| 绑定 | `POST /user/2fa/totp/setup` → `POST /user/2fa/totp/confirm` | setup 需提供当前登录口令；confirm 成功后一次性返回 10 个恢复码 |
| 危险操作 step-up | `POST /auth/step-up` | 已登录状态下再验一次 TOTP 或口令，通过后签发带 `stp` 的新 accessToken，有效 5 分钟 |
| 解绑 | `DELETE /user/2fa/totp` | 需 step-up；强制策略下的账号不允许解绑（`110009`） |

TOTP 参数与重放防护见 [安全合规与审计](./12-安全合规与审计.md) 的 12.5.2：HMAC-SHA1、6 位、30 秒、±1 步容忍、`last_code_step` 严格递增、5 次/10 分钟限流。

### 5.4.5 免鉴权白名单

白名单极小化，每新增一条需安全评审（清单同时是 [安全合规与审计](./12-安全合规与审计.md) 的攻击面清单）：

| 路径 | 方法 | 保护手段 |
| --- | --- | --- |
| `/api/v1/auth/login` | POST | IP 限流 5 次/分钟 + 失败锁定 + 可选图形验证码 |
| `/api/v1/auth/2fa/verify` | POST | 需有效 `mfaToken`；5 次/10 分钟 |
| `/api/v1/auth/refresh` | POST | 需有效 refreshToken Cookie + 设备指纹 |
| `/api/v1/auth/captcha` | GET | 单 IP 30 次/分钟 |
| `/api/v1/system/health` | GET | 只返回存活、DB 可写、迁移版本；无业务数据（见 [部署安装与升级](./14-部署安装与升级.md)） |
| `/api/v1/public/settings` | GET | 只返回登录页需要的品牌、语言、是否开启 OIDC |
| `/api/v1/public/agent/bootstrap` | GET | 需一次性 join token；见 [多节点集群与 Agent](./10-多节点集群与Agent.md) 的 10.8 |
| `/api/v1/public/agent/install.sh`、`/binary`、`/binary.sha256` | GET | 同上，token 校验 + 下载次数限制 |

### 5.4.6 API Token 签名机制

面向自动化脚本与 CI，避免把口令写进流水线。数据模型见 [数据库设计](./04-数据库设计.md) 的 `sys_api_token`。

| 项 | 约定 |
| --- | --- |
| AK | `nova_ak_` + 20 位 base62，明文存 `access_key`，可公开 |
| SK | 32 字节随机 base62，仅创建时返回一次；库中存 AES-256-GCM 密文（`secret_key_enc`）与后四位提示（`secret_key_hint`） |
| 作用域 | `scope_json` 为权限点数组，空表示继承用户全部权限；实际权限 = 用户权限 ∩ scope |
| IP 限制 | `allow_ip_json` 支持单 IP 与 CIDR；不匹配返回 `110010` |
| 独立限流 | `rate_limit_qps` 默认 20，0 表示走全局默认 |
| 有效期 | `expire_at` 为 NULL 表示永久；建议 ≤90 天，界面对永久 Token 显示告警 |
| 审计 | `audit_operation_log.auth_type='token'`、`token_id` 记录，可追到创建者 |

签名算法（HMAC-SHA256，不用明文 SK 直传，防日志与代理泄露）：

```text
StringToSign = HTTPMethod + "\n" +
               CanonicalURI + "\n" +               // 不含 query，需 URL 编码
               CanonicalQueryString + "\n" +       // key 字典序，k=v 用 & 连接
               "x-nova-ak:" + AK + "\n" +
               "x-nova-timestamp:" + Timestamp + "\n" +   // Unix 秒
               "x-nova-nonce:" + Nonce + "\n" +           // 16 位随机串
               HexSHA256(RequestBody)                     // 无体时为空串的哈希

Signature = HexHMACSHA256(SK, StringToSign)

Authorization: NOVA-HMAC-SHA256 AK=nova_ak_xxx,Timestamp=1755400000,Nonce=a1b2c3d4e5f60718,Signature=<hex>
```

服务端校验顺序：AK 存在且 `status=active` → 未过期 → 来源 IP 匹配 → `|now - Timestamp| ≤ 300s`（`110011`）→ Nonce 在 10 分钟缓存内未出现（`110012`）→ 签名一致（`110013`，`subtle.ConstantTimeCompare`）→ 作用域包含目标权限点。校验通过后在内存里构造等价 JWT 上下文（`aud=api`、`scp` 取 `scope_json`），后续中间件与 JWT 路径完全一致。

也支持简化模式 `Authorization: Bearer <AK>.<SK>` 直传，仅当 `sys_setting` 中 `security.allow_plain_token=true`（默认 false）时开启，用于内网临时调试，开启需二次确认并记审计。

### 5.4.7 Casbin 策略匹配

模型 RBAC with domains + 通配符，`model.conf`：

```ini
[request_definition]
r = sub, dom, obj, act

[policy_definition]
p = sub, dom, obj, act, eft

[role_definition]
g = _, _, _

[policy_effect]
e = some(where (p.eft == allow)) && !some(where (p.eft == deny))

[matchers]
m = g(r.sub, p.sub, r.dom) && r.dom == p.dom \
    && keyMatch2(r.obj, p.obj) && regexMatch(r.act, p.act)
```

| 元素 | 取值 | 示例 |
| --- | --- | --- |
| `sub` | `u:<userId>` 或 `r:<roleCode>` | `u:1832746500123456789`、`r:ops` |
| `dom` | `tenant_id` 字符串，单租户固定 `0` | `0` |
| `obj` | 权限点，支持 `*` 通配 | `web:site:create`、`web:site:*`、`*:*:*` |
| `act` | 固定 `access`（权限点已含 action 语义），保留位用于未来字段级控制 | `access` |
| `eft` | `allow` / `deny`，deny 优先 | `deny` |

策略行示例（对应 `sys_casbin_rule`）：

```csv
p, r:ops,      0, web:site:*,        access, allow
p, r:ops,      0, web:site:delete,   access, deny      # 运维可建改站点但不可删除
p, r:readonly, 0, *:*:list,          access, allow
p, r:readonly, 0, *:*:read,          access, allow
p, u:1832746500123456789, 0, sec:waf:update, access, allow   # 用户级临时加权
g, u:1832746500123456789, r:ops, 0
g, r:admin, r:ops, 0                                   # 角色继承
```

`is_super=1` 的用户在中间件首行短路放行，不进 Casbin（等价 `*:*:*`），全库仅允许 1 个。授权接口禁止授予高于自身的权限或角色，越权授权返回 `120004`；数据范围（`sys_role.data_scope` = all/group/self）在 repository 层追加 `node_id IN (...)` / `created_by = ?` 条件，不由 Casbin 表达。

### 5.4.8 权限点分布与鉴权中间件

权限点命名 `module:resource:action`，17 个模块的权限点分布（完整清单见 [安全合规与审计](./12-安全合规与审计.md) 12.8）：

| 模块前缀 | 资源数 | 权限点数 | 需二次确认的动作 |
| --- | --- | --- | --- |
| `dash` 概览 | 2 | 4 | — |
| `cluster` 集群节点 | 5 | 22 | `node:delete`、`node:exec`、`task:create` |
| `web` 网站 | 6 | 26 | `site:delete` |
| `cert` 证书 | 3 | 12 | `cert:delete` |
| `db` 数据库 | 4 | 19 | `instance:delete`、`database:delete` |
| `runtime` 运行环境 | 4 | 16 | `software:delete` |
| `file` 文件 | 3 | 15 | `file:delete`、`file:chmod` |
| `terminal` 终端 | 5 | 14 | `session:create`、`recording:delete` |
| `container` 容器编排 | 8 | 34 | `container:delete`、`image:prune`、`volume:delete` |
| `monitor` 监控 | 5 | 16 | — |
| `alert` 告警 | 4 | 18 | `heal:approve` |
| `sec` 安全 | 7 | 28 | `ipRule:create`、`firewall:update`、`baseline:fix` |
| `audit` 审计 | 2 | 6 | `log:export` |
| `appstore` 应用商店 | 3 | 13 | `instance:delete` |
| `cron` 计划任务 | 2 | 9 | `job:exec` |
| `backup` 备份恢复 | 4 | 17 | `record:restore`、`record:delete` |
| `setting` 系统设置 | 6 | 24 | `token:create`、`upgrade:exec`、`user:delete` |

鉴权中间件按顺序执行，任一环节失败即短路：

```go
func Authz(pt string, opts ...AuthzOption) gin.HandlerFunc {
    o := newAuthzOptions(opts...)
    return func(c *gin.Context) {
        id := auth.MustIdentity(c)                       // 5.4.2 已注入
        if id.IsSuper {                                  // 超管短路
            c.Set(ctxkey.Permission, pt)
            c.Next()
            return
        }
        if id.IsAPIToken && !id.ScopeContains(pt) {      // Token 作用域先过滤
            response.Fail(c, errs.ErrForbidden.WithDetail("token scope"))
            return
        }
        ok, err := authz.Enforce(id.Subject(), id.TenantID(), pt, "access")
        if err != nil {
            response.Fail(c, errs.Wrap(err, errs.ErrInternal))
            return
        }
        if !ok {
            response.Fail(c, errs.New(120003).WithField("permission", pt))
            return
        }
        if o.NeedConfirm && !confirm.Verify(c, pt) {     // 5.6.3 危险操作
            response.Fail(c, errs.New(100014))
            return
        }
        c.Set(ctxkey.Permission, pt)
        c.Next()
    }
}
```

策略缓存放在进程内 `sync.Map`，key 为 `subject|dom|obj`，TTL 60 秒；角色、权限、用户角色关系任一变更时通过内部事件总线广播失效，控制面多实例部署时经 Redis pub/sub 或数据库版本号轮询同步（见 [部署安装与升级](./14-部署安装与升级.md) 高可用章节）。缓存只缓存判定结果不缓存策略集，避免策略重载期间出现半新半旧状态。

## 5.5 错误码全量清单

错误码为 6 位十进制 `MMSSNN`：`MM` 模块号、`SS` 子域号、`NN` 序号。成功恒为 `0`，不使用其他成功码。前端只依赖错误码决策，不解析 `message` 文本。

| 规则 | 说明 |
| --- | --- |
| 编号不复用 | 一个码一旦发布即固定语义，废弃后留空不再分配 |
| 子域划分 | `SS=00` 为模块通用，`01` 起按资源划分，与权限点的 resource 对应 |
| HTTP 状态 | 400 参数与业务校验、401 认证、403 授权、404 资源不存在、409 冲突、423 锁定、429 限流、500 内部、502/504 下游与节点、507 空间不足 |
| 常量位置 | `internal/pkg/errs/codes.go`，一码一常量，注释即中文提示 |
| 文案来源 | 后端返回中文兜底文案，前端优先用 `error.json` 的本地化文案覆盖（见 [前端架构与 Arco 规范](./06-前端架构与Arco规范.md)） |
| 前端处理 | `toast` 提示、`silent` 静默、`redirect` 跳转、`confirm` 二次确认、`refresh` 刷新数据、`form` 定位到表单字段 |

### 5.5.1 通用与认证（10xxxx、11xxxx）

| 错误码 | HTTP | 常量名 | 中文提示 | 触发场景 | 前端处理 |
| --- | --- | --- | --- | --- | --- |
| 100001 | 400 | `ErrInvalidParam` | 请求参数有误 | 绑定或校验失败，`data.fields` 给字段级错误 | form |
| 100002 | 400 | `ErrInvalidJSON` | 请求体格式错误 | JSON 解析失败 | toast |
| 100003 | 404 | `ErrNotFound` | 资源不存在 | 主键查询为空或已软删 | toast + refresh |
| 100004 | 409 | `ErrConflict` | 资源已存在或状态冲突 | 唯一键冲突、状态机不允许 | toast |
| 100005 | 500 | `ErrInternal` | 服务内部错误 | 未归类 panic 或下游失败 | toast |
| 100006 | 429 | `ErrTooManyRequests` | 请求过于频繁 | 触发限流，带 `Retry-After` | silent + 退避 |
| 100007 | 400 | `ErrUnsupported` | 当前环境不支持该操作 | OS 或组件不满足前置条件 | toast |
| 100008 | 507 | `ErrNoSpace` | 磁盘空间不足 | 预检剩余空间低于阈值 | toast |
| 100009 | 504 | `ErrTimeout` | 操作超时 | 上下文超时 | toast |
| 100010 | 400 | `ErrIdempotentReplay` | 重复提交 | `Idempotency-Key` 命中且参数不同 | toast |
| 100011 | 409 | `ErrResourceBusy` | 资源正被其他操作占用 | 未获取到互斥锁 | toast + refresh |
| 100012 | 400 | `ErrBadPagination` | 分页参数超出范围 | `pageSize > 200` 或 `sort` 不在白名单 | silent |
| 100013 | 503 | `ErrMaintenance` | 系统维护中 | 升级或迁移窗口 | redirect |
| 100014 | 403 | `ErrNeedConfirm` | 危险操作需二次确认 | 缺少或无效 `X-Confirm-Token` | confirm |
| 100015 | 413 | `ErrBodyTooLarge` | 请求体过大 | 超过 `server.max_body_size` | toast |
| 100016 | 500 | `ErrExecutorFailed` | 系统命令执行失败 | executor 返回非零退出码，`data.stderr` 截断回传 | toast |
| 110001 | 401 | `ErrUnauthorized` | 未登录或登录已过期 | 缺少或无效 Token | redirect 登录页 |
| 110002 | 401 | `ErrTokenExpired` | 登录已过期 | accessToken 过期，触发静默刷新 | silent + refresh token |
| 110003 | 401 | `ErrTokenRevoked` | 登录已失效，请重新登录 | 命中吊销名单或被强制下线 | redirect |
| 110004 | 400 | `ErrBadCredentials` | 用户名或密码错误 | 登录校验失败，不区分账号是否存在 | toast |
| 110005 | 423 | `ErrAccountLocked` | 账号已锁定，请稍后再试 | 连续失败达阈值，`data.unlockAt` | toast |
| 110006 | 403 | `ErrAccountDisabled` | 账号已停用 | `status=disabled` | toast |
| 110007 | 400 | `ErrCaptchaRequired` | 请先完成验证码校验 | 失败次数达验证码阈值 | form |
| 110008 | 400 | `ErrCaptchaInvalid` | 验证码错误或已过期 | 校验失败 | form |

### 5.5.2 二次认证与 API Token（11xxxx 续）

| 错误码 | HTTP | 常量名 | 中文提示 | 触发场景 | 前端处理 |
| --- | --- | --- | --- | --- | --- |
| 110009 | 401 | `ErrNeed2FA` | 需要二次验证 | 登录一阶段通过，返回 `data.twoFAToken` | 跳 2FA 表单 |
| 110010 | 400 | `Err2FAInvalid` | 验证码不正确 | TOTP 校验失败（含 ±1 时间窗） | form |
| 110011 | 401 | `ErrSignTimestamp` | 签名时间戳超出允许范围 | `\|now-Timestamp\|>300s` | toast（校时） |
| 110012 | 401 | `ErrSignNonceReplay` | 请求重放 | Nonce 在 10 分钟窗口内重复 | toast |
| 110013 | 401 | `ErrSignInvalid` | 签名校验失败 | HMAC 不一致（常量时间比较） | toast |
| 110014 | 403 | `ErrTokenIPDenied` | 来源 IP 不在允许范围 | API Token 绑定 IP 不匹配 | toast |
| 110015 | 403 | `ErrTokenExpiredAK` | API Token 已过期或停用 | `expire_at` 已到或 `status!=active` | toast |
| 110016 | 400 | `Err2FANotBound` | 尚未绑定二次验证 | 未绑定却提交校验 | redirect 绑定页 |
| 110017 | 409 | `Err2FAAlreadyBound` | 已绑定二次验证 | 重复绑定 | toast |
| 110018 | 400 | `ErrRecoveryCodeInvalid` | 恢复码无效或已使用 | 恢复码校验失败 | form |
| 110019 | 403 | `ErrIPBlocked` | 当前 IP 已被封禁 | 命中 `sec_ip_rule` 黑名单 | toast |
| 110020 | 403 | `ErrEntryPathInvalid` | 安全入口不匹配 | 未通过安全入口 path 访问 | 返回 404 伪装页 |
| 110021 | 401 | `ErrRefreshInvalid` | 刷新凭证无效 | refreshToken 不存在或已轮换 | redirect |
| 110022 | 409 | `ErrPasswordExpired` | 密码已过期，请修改 | 密码策略强制过期 | redirect 改密页 |
| 110023 | 403 | `ErrSessionKicked` | 会话已在其他位置登出 | 管理员强制下线 | redirect |

### 5.5.3 用户与权限（12xxxx）

| 错误码 | HTTP | 常量名 | 中文提示 | 触发场景 | 前端处理 |
| --- | --- | --- | --- | --- | --- |
| 120001 | 409 | `ErrUserExists` | 用户名已存在 | `sys_user.username` 唯一冲突 | form |
| 120002 | 400 | `ErrWeakPassword` | 密码强度不足 | 不满足 `security.password_policy` | form |
| 120003 | 403 | `ErrForbidden` | 没有该操作权限 | Casbin 拒绝，`data.permission` 回传权限点 | toast |
| 120004 | 403 | `ErrGrantEscalation` | 不能授予超出自身的权限 | 授权集合非自身权限子集 | toast |
| 120005 | 409 | `ErrRoleInUse` | 角色已被用户引用，无法删除 | 存在关联用户 | toast |
| 120006 | 400 | `ErrCannotDeleteSelf` | 不能删除或停用当前登录账号 | 自我操作保护 | toast |
| 120007 | 403 | `ErrSuperAdminProtected` | 超级管理员不可修改 | 目标为 `is_super=1` | toast |
| 120008 | 404 | `ErrRoleNotFound` | 角色不存在 | 角色 ID 无效 | toast |
| 120009 | 400 | `ErrPermissionUnknown` | 权限点不存在 | 提交了未注册的权限点 | form |
| 120010 | 409 | `ErrRoleCodeExists` | 角色标识已存在 | `sys_role.code` 冲突 | form |
| 120011 | 403 | `ErrDataScopeDenied` | 数据范围之外的对象 | `data_scope` 校验失败 | toast |
| 120012 | 400 | `ErrOldPasswordWrong` | 原密码不正确 | 自助改密校验失败 | form |
| 120013 | 409 | `ErrLastSuperAdmin` | 系统至少保留一个超级管理员 | 撤销最后一个超管 | toast |

### 5.5.4 集群节点与网站证书（20xxxx、30xxxx、31xxxx）

| 错误码 | HTTP | 常量名 | 中文提示 | 触发场景 | 前端处理 |
| --- | --- | --- | --- | --- | --- |
| 200001 | 404 | `ErrNodeNotFound` | 节点不存在 | `X-Node-Id` 或路径 ID 无效 | toast |
| 200002 | 502 | `ErrNodeOffline` | 节点离线 | 心跳超时被判离线 | toast + 禁用操作区 |
| 200003 | 409 | `ErrNodeRegistered` | 节点已注册 | 重复注册同一指纹 | toast |
| 200004 | 400 | `ErrNodeTokenInvalid` | 注册令牌无效或已过期 | bootstrap token 校验失败 | toast |
| 200005 | 409 | `ErrNodeVersionMismatch` | Agent 版本过低，请先升级 | 低于 `agent.min_version` | 引导升级 |
| 200006 | 504 | `ErrNodeRPCTimeout` | 节点响应超时 | gRPC 调用超时 | toast + 重试 |
| 200007 | 409 | `ErrNodeHasResource` | 节点仍有资源，无法移除 | 存在网站或容器等占用 | toast |
| 200008 | 400 | `ErrNodeSSHFailed` | SSH 连接失败 | 批量部署预检失败，`data.stage` | toast |
| 200009 | 409 | `ErrNodeGroupInUse` | 分组已被节点引用 | 删除非空分组 | toast |
| 200010 | 400 | `ErrNodeSelectorEmpty` | 选择器未匹配到任何节点 | 批量任务目标为空 | form |
| 200011 | 409 | `ErrTaskRunning` | 已有同类任务在执行 | 批量任务互斥 | toast + refresh |
| 200012 | 403 | `ErrNodeCertRevoked` | 节点证书已吊销 | mTLS 校验命中 CRL | silent |
| 300001 | 409 | `ErrDomainExists` | 域名已被其他站点占用 | `web_domain.domain` 冲突 | form |
| 300002 | 400 | `ErrDomainInvalid` | 域名格式不合法 | 正则或 IDN 转换失败 | form |
| 300003 | 404 | `ErrSiteNotFound` | 站点不存在 | 站点 ID 无效 | toast |
| 300004 | 409 | `ErrSiteRootConflict` | 站点根目录与已有站点冲突 | 目录重复绑定 | form |
| 300005 | 400 | `ErrNginxTestFailed` | 配置校验未通过，已回滚 | `nginx -t` 失败，`data.stderr` | toast + 查看详情 |
| 300006 | 409 | `ErrPortOccupied` | 端口已被占用 | 监听端口探测失败 | form |
| 300007 | 400 | `ErrRewriteInvalid` | 伪静态规则不合法 | 规则语法校验失败 | form |
| 300008 | 409 | `ErrSiteStopped` | 站点已停用 | 对停用站点执行需运行态的操作 | toast |
| 300009 | 400 | `ErrUpstreamUnreachable` | 反向代理目标不可达 | 预检连通性失败 | toast |
| 300010 | 409 | `ErrRuntimeInUse` | 运行环境被站点引用 | 删除仍被绑定的运行时 | toast |
| 310001 | 400 | `ErrACMEChallengeFailed` | 证书签发校验失败 | HTTP-01 或 DNS-01 校验未通过，`data.reason` | toast + 查看日志 |
| 310002 | 400 | `ErrCertKeyMismatch` | 证书与私钥不匹配 | 上传证书校验失败 | form |
| 310003 | 409 | `ErrCertExpired` | 证书已过期 | 部署已过期证书 | toast |
| 310004 | 400 | `ErrDNSProviderFailed` | DNS 服务商接口调用失败 | 凭证或配额问题，`data.provider` | toast |
| 310005 | 429 | `ErrACMERateLimited` | CA 签发频率受限，请稍后重试 | Let's Encrypt 限流 | toast + 退避 |
| 310006 | 409 | `ErrCertInUse` | 证书已被站点引用，无法删除 | 存在部署引用 | toast |
| 310007 | 400 | `ErrCertChainInvalid` | 证书链不完整 | 缺少中间证书 | form |

### 5.5.5 数据库、运行环境、文件与终端（32xxxx、33xxxx、40xxxx、41xxxx）

| 错误码 | HTTP | 常量名 | 中文提示 | 触发场景 | 前端处理 |
| --- | --- | --- | --- | --- | --- |
| 320001 | 502 | `ErrDBConnectFailed` | 数据库连接失败 | 目标实例不可达或凭证错误 | toast |
| 320002 | 409 | `ErrDBExists` | 同名数据库已存在 | 创建重名库 | form |
| 320003 | 409 | `ErrDBUserExists` | 数据库账号已存在 | 创建重名账号 | form |
| 320004 | 400 | `ErrDBNameInvalid` | 数据库名不合法 | 字符集或长度校验失败 | form |
| 320005 | 409 | `ErrDBInUse` | 数据库被站点或应用引用 | 删除仍被引用的库 | toast |
| 320006 | 400 | `ErrSQLImportFailed` | SQL 导入失败 | 语法错误或权限不足，`data.line` | toast + 查看日志 |
| 320007 | 507 | `ErrDBBackupNoSpace` | 备份空间不足 | 预检失败 | toast |
| 320008 | 400 | `ErrDBRootPasswordWrong` | 管理员密码不正确 | root 凭证校验失败 | form |
| 320009 | 409 | `ErrDBServiceStopped` | 数据库服务未运行 | 服务处于 stopped | toast + 启动引导 |
| 330001 | 409 | `ErrRuntimeExists` | 该版本运行环境已安装 | 重复安装 | toast |
| 330002 | 400 | `ErrRuntimeInstallFailed` | 运行环境安装失败 | 编译或包管理器失败，`data.logId` | toast + 查看日志 |
| 330003 | 404 | `ErrRuntimeNotFound` | 运行环境不存在 | ID 无效 | toast |
| 330004 | 400 | `ErrExtensionUnavailable` | 扩展不可用于当前版本 | 版本兼容矩阵不满足 | toast |
| 330005 | 409 | `ErrRuntimeVersionLocked` | 站点已锁定运行时版本 | 切换被禁止 | toast |
| 330006 | 400 | `ErrSupervisorFailed` | 进程守护配置下发失败 | supervisor 或 systemd 报错 | toast |
| 400001 | 403 | `ErrPathEscape` | 路径越界 | 规范化后逃出允许根目录 | toast |
| 400002 | 404 | `ErrFileNotFound` | 文件或目录不存在 | stat 失败 | toast + refresh |
| 400003 | 409 | `ErrFileExists` | 目标已存在 | 未指定覆盖策略 | confirm |
| 400004 | 403 | `ErrFilePermission` | 没有文件系统权限 | 宿主 EACCES | toast |
| 400005 | 413 | `ErrFileTooLarge` | 文件超出大小限制 | 超过 `file.max_edit_size` 或上传上限 | toast |
| 400006 | 400 | `ErrChunkChecksum` | 分片校验失败 | 分片 MD5 不匹配 | 自动重传该分片 |
| 400007 | 409 | `ErrUploadSessionExpired` | 上传会话已过期 | 会话超过 24 小时 | 重新初始化 |
| 400008 | 400 | `ErrArchiveUnsupported` | 不支持的压缩格式 | 后缀与探测均失败 | toast |
| 400009 | 400 | `ErrArchiveCorrupt` | 压缩包损坏或密码错误 | 解压失败 | toast |
| 400010 | 400 | `ErrNotUTF8` | 文件不是文本或编码不受支持 | 在线编辑探测到二进制 | toast |
| 400011 | 409 | `ErrFileChanged` | 文件已被其他人修改 | 保存时 ETag 不一致 | confirm 覆盖或对比 |
| 400012 | 403 | `ErrProtectedPath` | 受保护路径禁止操作 | 命中系统路径黑名单 | toast |
| 400013 | 507 | `ErrDiskFull` | 磁盘已满 | 写入 ENOSPC | toast |
| 400014 | 400 | `ErrRecycleRestoreConflict` | 还原目标已存在 | 回收站还原冲突 | confirm |
| 410001 | 502 | `ErrSSHConnectFailed` | 终端连接失败 | 建立 PTY 失败，`data.reason` | toast |
| 410002 | 403 | `ErrCommandBlocked` | 命令被高危策略拦截 | 命中 `term_rule` 拦截规则，`data.rule` | toast |
| 410003 | 409 | `ErrSessionClosed` | 会话已结束 | 向已关闭会话写入 | 关闭标签页 |
| 410004 | 429 | `ErrSessionLimit` | 终端会话数已达上限 | 超过 `terminal.max_sessions` | toast |
| 410005 | 404 | `ErrRecordingNotFound` | 录像不存在或已清理 | 回放请求失败 | toast |
| 410006 | 403 | `ErrShareExpired` | 分享链接已失效 | 分享超时或被撤销 | redirect |
| 410007 | 400 | `ErrCredentialInvalid` | 凭证校验失败 | 私钥口令或密码错误 | form |

### 5.5.6 容器、编排与仓库（50xxxx、51xxxx、52xxxx）

| 错误码 | HTTP | 常量名 | 中文提示 | 触发场景 | 前端处理 |
| --- | --- | --- | --- | --- | --- |
| 500001 | 502 | `ErrDockerUnavailable` | Docker 服务不可用 | socket 连接失败 | toast + 安装引导 |
| 500002 | 404 | `ErrContainerNotFound` | 容器不存在 | ID 或名称无效 | toast + refresh |
| 500003 | 409 | `ErrContainerRunning` | 容器正在运行 | 需要停止后才能执行 | confirm |
| 500004 | 409 | `ErrContainerNameExists` | 容器名称已被占用 | 创建重名容器 | form |
| 500005 | 400 | `ErrPortMappingConflict` | 端口映射冲突 | 宿主端口已被占用 | form |
| 500006 | 400 | `ErrMountPathInvalid` | 挂载路径不合法 | 命中挂载黑名单或路径不存在 | form |
| 500007 | 400 | `ErrPrivilegedDenied` | 特权容器已被安全策略禁止 | `container.allow_privileged=false` | toast |
| 500008 | 409 | `ErrContainerExitedAbnormal` | 容器异常退出 | 启动后立即退出，`data.exitCode` | toast + 查看日志 |
| 500009 | 400 | `ErrDaemonConfigInvalid` | daemon 配置不合法，已回滚 | `daemon.json` 校验失败 | toast |
| 500010 | 409 | `ErrVolumeInUse` | 卷正被容器使用 | 删除占用中的卷 | toast |
| 500011 | 409 | `ErrNetworkInUse` | 网络正被容器使用 | 删除占用中的网络 | toast |
| 500012 | 400 | `ErrExecFailed` | 进入容器失败 | exec 创建失败或无 shell | toast |
| 500013 | 409 | `ErrImageInUse` | 镜像已被容器引用 | 删除被引用镜像 | confirm 强制删除 |
| 510001 | 400 | `ErrComposeInvalid` | Compose 文件不合法 | compose-go 解析失败，`data.line` | toast + 定位编辑器 |
| 510002 | 409 | `ErrStackExists` | 编排项目已存在 | 项目名冲突 | form |
| 510003 | 404 | `ErrStackNotFound` | 编排项目不存在 | 项目 ID 无效 | toast |
| 510004 | 400 | `ErrComposeUpFailed` | 编排启动失败 | `compose up` 非零退出，`data.logId` | toast + 查看日志 |
| 510005 | 409 | `ErrStackDrifted` | 线上状态与配置不一致 | 检测到人工改动 | confirm 同步 |
| 510006 | 400 | `ErrEnvFileInvalid` | 环境变量文件格式错误 | `.env` 解析失败 | form |
| 510007 | 502 | `ErrK8sUnreachable` | Kubernetes 集群不可达 | kubeconfig 或网络问题 | toast |
| 510008 | 403 | `ErrK8sForbidden` | Kubernetes 权限不足 | ServiceAccount RBAC 不足 | toast |
| 520001 | 502 | `ErrRegistryUnreachable` | 镜像仓库不可达 | 网络或 DNS 失败 | toast |
| 520002 | 401 | `ErrRegistryAuthFailed` | 镜像仓库认证失败 | 凭证错误 | form |
| 520003 | 404 | `ErrImageNotFound` | 镜像或标签不存在 | manifest 404 | toast |
| 520004 | 400 | `ErrImagePullFailed` | 镜像拉取失败 | 传输中断或校验失败 | toast + 重试 |
| 520005 | 400 | `ErrImageBuildFailed` | 镜像构建失败 | Dockerfile 执行失败，`data.step` | toast + 查看日志 |
| 520006 | 409 | `ErrRegistryExists` | 仓库配置已存在 | 同 URL 重复添加 | form |
| 520007 | 403 | `ErrImageNotAllowed` | 镜像来源不在白名单 | 命中镜像来源策略 | toast |

### 5.5.7 监控与告警（60xxxx、61xxxx）

| 错误码 | HTTP | 常量名 | 中文提示 | 触发场景 | 前端处理 |
| --- | --- | --- | --- | --- | --- |
| 600001 | 400 | `ErrMetricUnknown` | 指标不存在 | 查询未注册指标 | form |
| 600002 | 400 | `ErrTimeRangeInvalid` | 时间范围不合法 | `from>=to` 或跨度超上限 | form |
| 600003 | 400 | `ErrStepTooSmall` | 采样粒度过小，返回点数超限 | 点数 > 11000 | 自动升粒度重试 |
| 600004 | 502 | `ErrTSDBUnavailable` | 时序存储不可用 | 外接 VictoriaMetrics 故障 | toast |
| 600005 | 404 | `ErrDashboardNotFound` | 仪表盘不存在 | ID 无效 | toast |
| 600006 | 409 | `ErrDashboardBuiltin` | 内置仪表盘不可修改 | 编辑内置模板 | 引导另存 |
| 600007 | 400 | `ErrLogQueryInvalid` | 日志查询表达式不合法 | 语法错误，`data.pos` | form |
| 600008 | 429 | `ErrLogScanLimit` | 日志扫描量超出限制 | 超过 `monitor.log_scan_limit` | toast + 缩小范围 |
| 600009 | 404 | `ErrReportNotFound` | 巡检报告不存在 | ID 无效或已清理 | toast |
| 600010 | 409 | `ErrRetentionConflict` | 保留策略配置冲突 | 高粒度保留长于低粒度 | form |
| 610001 | 400 | `ErrAlertExprInvalid` | 告警表达式不合法 | DSL 解析失败，`data.pos` | form + 高亮 |
| 610002 | 404 | `ErrAlertRuleNotFound` | 告警规则不存在 | ID 无效 | toast |
| 610003 | 409 | `ErrAlertRuleExists` | 同名规则已存在 | 名称冲突 | form |
| 610004 | 400 | `ErrChannelTestFailed` | 通知通道测试失败 | webhook 或 SMTP 报错，`data.reason` | toast |
| 610005 | 404 | `ErrChannelNotFound` | 通知通道不存在 | ID 无效 | toast |
| 610006 | 409 | `ErrChannelInUse` | 通道被规则引用，无法删除 | 存在引用 | toast |
| 610007 | 400 | `ErrTemplateRenderFailed` | 通知模板渲染失败 | 模板变量缺失 | form |
| 610008 | 409 | `ErrEventResolved` | 事件已恢复，无需处理 | 对已恢复事件确认 | refresh |
| 610009 | 409 | `ErrSilenceOverlap` | 静默规则时间区间重叠 | 与既有静默冲突 | confirm |
| 610010 | 403 | `ErrHealNeedApprove` | 自愈动作需人工审批 | 高危自愈动作 | 跳审批页 |
| 610011 | 409 | `ErrHealCooldown` | 自愈处于冷却期 | 距上次执行不足冷却时长 | toast |

### 5.5.8 安全、审计、应用商店与计划任务（70xxxx、71xxxx、80xxxx、81xxxx）

| 错误码 | HTTP | 常量名 | 中文提示 | 触发场景 | 前端处理 |
| --- | --- | --- | --- | --- | --- |
| 700001 | 400 | `ErrFirewallRuleInvalid` | 防火墙规则不合法 | 端口或 CIDR 校验失败 | form |
| 700002 | 409 | `ErrFirewallRuleExists` | 规则已存在 | 同五元组重复 | form |
| 700003 | 502 | `ErrFirewallBackendFailed` | 防火墙后端执行失败 | firewalld/ufw/nftables 报错 | toast |
| 700004 | 403 | `ErrSelfLockout` | 该规则会导致面板失联，已阻止 | 规则会封禁当前来源或面板端口 | toast |
| 700005 | 400 | `ErrWAFRuleInvalid` | WAF 规则不合法 | 正则编译失败或性能预检不通过 | form |
| 700006 | 409 | `ErrWAFRuleBuiltin` | 内置 WAF 规则不可删除 | 删除内置规则 | 引导停用 |
| 700007 | 400 | `ErrSSHHardenFailed` | SSH 加固配置校验失败，已回滚 | `sshd -t` 失败 | toast |
| 700008 | 409 | `ErrBaselineRunning` | 基线核查正在执行 | 重复触发 | toast + refresh |
| 700009 | 404 | `ErrBaselineItemNotFound` | 核查项不存在 | ID 无效 | toast |
| 700010 | 400 | `ErrBaselineFixFailed` | 一键修复失败 | 修复脚本非零退出，`data.itemId` | toast + 查看详情 |
| 700011 | 409 | `ErrIPRuleConflict` | 黑白名单条目冲突 | 同 IP 同时命中黑白名单 | form |
| 700012 | 400 | `ErrScanTargetInvalid` | 扫描目标不合法 | 目标不在受管资产内 | form |
| 700013 | 429 | `ErrScanConcurrency` | 扫描任务并发已达上限 | 超过并发限制 | toast |
| 700014 | 403 | `ErrTwoManRuleRequired` | 该操作需双人复核 | 命中双人复核策略 | 跳审批流 |
| 710001 | 400 | `ErrAuditExportTooLarge` | 导出数据量过大 | 超过 `audit.export_max_rows` | toast + 缩小范围 |
| 710002 | 404 | `ErrAuditNotFound` | 审计记录不存在 | ID 无效或已归档 | toast |
| 710003 | 409 | `ErrAuditImmutable` | 审计记录不可修改或删除 | 尝试写操作 | toast |
| 710004 | 500 | `ErrAuditChainBroken` | 审计链校验失败 | 哈希链断裂，`data.brokenAt` | 告警 + 引导排查 |
| 710005 | 502 | `ErrAuditForwardFailed` | 审计日志外发失败 | syslog/Kafka 不可达 | silent + 重试队列 |
| 800001 | 404 | `ErrAppNotFound` | 应用不存在 | 应用 key 无效 | toast |
| 800002 | 409 | `ErrAppInstalled` | 应用已安装 | 同实例名重复安装 | form |
| 800003 | 400 | `ErrAppManifestInvalid` | 应用清单不合法 | `app.yaml` 校验失败，`data.field` | toast |
| 800004 | 400 | `ErrAppDepMissing` | 依赖未满足 | 缺少运行时或数据库，`data.missing` | 引导安装依赖 |
| 800005 | 400 | `ErrAppInstallFailed` | 应用安装失败 | compose 拉起失败，`data.logId` | toast + 查看日志 |
| 800006 | 409 | `ErrAppRunning` | 应用正在运行 | 需先停止 | confirm |
| 800007 | 502 | `ErrRepoSyncFailed` | 应用仓库同步失败 | 索引下载或签名校验失败 | toast |
| 800008 | 403 | `ErrAppSignatureInvalid` | 应用包签名校验失败 | 签名不匹配 | toast |
| 800009 | 409 | `ErrAppVersionDowngrade` | 不支持降级安装 | 目标版本低于当前 | toast |
| 800010 | 409 | `ErrAppParamImmutable` | 该参数安装后不可修改 | 修改 `immutable` 参数 | form |
| 810001 | 400 | `ErrCronExprInvalid` | 定时表达式不合法 | cron 解析失败 | form |
| 810002 | 404 | `ErrJobNotFound` | 任务不存在 | ID 无效 | toast |
| 810003 | 409 | `ErrJobRunning` | 任务正在执行 | 并发策略为 skip | toast |
| 810004 | 409 | `ErrJobDisabled` | 任务已停用 | 手动触发已停用任务 | confirm 启用 |
| 810005 | 400 | `ErrJobScriptEmpty` | 任务脚本为空 | 校验失败 | form |
| 810006 | 504 | `ErrJobTimeout` | 任务执行超时已被终止 | 超过 `timeout` | toast |
| 810007 | 400 | `ErrJobTargetInvalid` | 任务目标节点不合法 | 选择器为空或含离线节点 | form |

### 5.5.9 备份恢复与系统升级（90xxxx、91xxxx）

| 错误码 | HTTP | 常量名 | 中文提示 | 触发场景 | 前端处理 |
| --- | --- | --- | --- | --- | --- |
| 900001 | 404 | `ErrBackupNotFound` | 备份记录不存在 | ID 无效 | toast |
| 900002 | 409 | `ErrBackupRunning` | 已有备份任务在执行 | 同目标互斥 | toast + refresh |
| 900003 | 502 | `ErrStorageUnreachable` | 存储后端不可达 | S3/OSS/WebDAV 连接失败 | toast |
| 900004 | 401 | `ErrStorageAuthFailed` | 存储后端认证失败 | AK/SK 错误 | form |
| 900005 | 507 | `ErrBackupNoSpace` | 备份目标空间不足 | 预检剩余空间不足 | toast |
| 900006 | 400 | `ErrBackupChecksumFailed` | 备份完整性校验失败 | SHA256 不一致 | toast |
| 900007 | 400 | `ErrRestoreVersionMismatch` | 备份版本与当前系统不兼容 | 版本矩阵不满足 | toast |
| 900008 | 409 | `ErrRestoreConflict` | 恢复目标已存在同名对象 | 需选择覆盖策略 | confirm |
| 900009 | 400 | `ErrBackupEncryptedNoKey` | 备份已加密，需提供密钥 | 缺少解密口令 | form |
| 900010 | 400 | `ErrBackupCorrupt` | 备份文件损坏 | 解包失败 | toast |
| 900011 | 409 | `ErrRetentionPolicyInvalid` | 保留策略不合法 | GFS 参数矛盾 | form |
| 900012 | 409 | `ErrRestoreRunning` | 已有恢复任务在执行 | 恢复互斥 | toast |
| 900013 | 400 | `ErrPartialRestoreUnsupported` | 该备份类型不支持粒度恢复 | 类型限制 | toast |
| 910001 | 409 | `ErrUpgradeRunning` | 升级任务正在执行 | 重复触发 | toast + refresh |
| 910002 | 400 | `ErrUpgradePackageInvalid` | 升级包校验失败 | 签名或哈希不匹配 | toast |
| 910003 | 409 | `ErrUpgradePreflightFailed` | 升级前置检查未通过 | 空间、备份或版本跨度不满足，`data.checks` | 展示检查清单 |
| 910004 | 409 | `ErrRollbackUnavailable` | 无可回滚版本 | 缺少上一版本或迁移不可逆 | toast |
| 910005 | 400 | `ErrMigrationFailed` | 数据库迁移失败，已回滚 | 迁移脚本报错，`data.version` | toast + 查看日志 |
| 910006 | 409 | `ErrVersionSkewTooLarge` | 版本跨度过大，请分步升级 | 跨多个不兼容版本 | 展示升级路径 |
| 910007 | 502 | `ErrUpdateChannelUnreachable` | 更新源不可达 | 更新服务器网络失败 | toast |

### 5.5.10 错误码使用规范

后端只在 service 层构造带业务码的错误，handler 层统一交给 `response.Fail` 转换：

```go
// service/website/site.go
func (s *SiteService) Create(ctx context.Context, in *dto.SiteCreateIn) (*model.WebSite, error) {
    if exists, err := s.domainRepo.ExistsAny(ctx, in.Domains); err != nil {
        return nil, errs.Wrap(err, errs.ErrInternal)
    } else if exists {
        return nil, errs.New(300001).WithField("domain", in.Domains[0])
    }
    if err := s.nginx.Test(ctx, conf); err != nil {
        // 保留底层 stderr 供前端展开查看，但不泄露绝对路径之外的敏感信息
        return nil, errs.New(300005).WithDetail(executor.TrimStderr(err, 2048))
    }
    ...
}
```

| 约定 | 要求 |
| --- | --- |
| 禁止裸 500 | 任何可预期失败都要有专属码，`ErrInternal` 只兜底未识别错误 |
| 字段级错误 | 表单类失败用 `WithField(name, value)`，聚合到 `data.fields[]`，前端按 `name` 定位输入框 |
| 下游原文 | 外部命令 stderr 经 `TrimStderr` 截断至 2 KB 放 `data.stderr`，前端折叠展示 |
| 节点相关 | 跨节点失败必须带 `data.nodeId` 与 `data.nodeName`，前端在多节点批量视图里逐节点标记 |
| 日志级别 | 4xx 记 `warn` 且不打堆栈，5xx 记 `error` 且带堆栈与 `traceId` |
| 测试要求 | 每个错误码至少一个集成测试断言 `code`，见 [测试与质量保障](./15-测试与质量保障.md) |
| 文档同步 | 新增错误码必须同一 PR 内更新本节表格与 `errs/codes.go`，CI 用脚本比对两侧一致性 |

CI 校验脚本 `scripts/check_error_codes.sh` 从 `codes.go` 抽取所有 `errs.New(` 字面量与常量定义，再解析本文件 5.5 节所有表格首列，双向比对，出现单侧存在即失败。

## 5.6 限流、防重放与安全约束

### 5.6.1 限流分层

限流分三层，自外向内依次生效，任一层拒绝即返回 `100006` 并带 `Retry-After` 秒数与 `X-RateLimit-*` 响应头。

| 层 | 维度 | 默认配额 | 实现 | 超限行为 |
| --- | --- | --- | --- | --- |
| L1 全局 | 进程级 QPS | 2000 r/s | `golang.org/x/time/rate` 单例令牌桶 | 直接 429，不记审计 |
| L2 身份 | 用户或 API Token | 300 r/min，突发 60 | 滑动窗口，key `rl:u:<uid>` | 429，记 `warn` |
| L3 接口 | 权限点 + 身份 | 见下表 | 令牌桶，key `rl:<pt>:<uid>` | 429，敏感接口同时记审计 |

单机默认用进程内 `sync.Map` + 分片锁存计数器；配置 `kv.driver=redis` 时自动切到 Redis Lua 脚本实现，保证多副本共享配额（见 [部署安装与升级](./14-部署安装与升级.md) 的 HA 章节）。

| 接口类别 | 权限点样例 | 配额 | 理由 |
| --- | --- | --- | --- |
| 登录 | `POST /auth/login` | 5 次 / 5 分钟 / IP+用户名 | 抗暴破，独立于 L2 |
| 2FA 校验 | `POST /auth/2fa/verify` | 10 次 / 5 分钟 / twoFAToken | 抗爆破 6 位码 |
| 证书签发 | `cert:order:create` | 5 次 / 小时 / 租户 | 规避 CA 侧限流 |
| 备份触发 | `backup:task:exec` | 3 次 / 小时 / 任务 | 避免堆积占满 IO |
| 镜像构建 | `container:image:create` | 5 次 / 10 分钟 / 节点 | 构建吃满 CPU |
| 日志检索 | `monitor:log:list` | 30 次 / 分钟 / 用户 | 扫描成本高 |
| 批量任务 | `cluster:task:create` | 10 次 / 分钟 / 租户 | 防止节点侧雪崩 |
| 只读列表 | `*:*:list` | 走 L2 默认 | 无需单独限制 |
| WebSocket 建连 | `ws/*` | 20 次 / 分钟 / 用户，并发 30 连接 | 限制句柄与内存 |

```go
// internal/middleware/ratelimit.go
func RateLimit(scope Scope, n int, per time.Duration, burst int) gin.HandlerFunc {
    lim := ratelimit.New(scope, n, per, burst) // 按 kv.driver 选进程内或 Redis
    return func(c *gin.Context) {
        key := scope.Key(c) // 组合 uid / ip / nodeId / 路由权限点
        res, err := lim.Allow(c.Request.Context(), key)
        if err != nil { // 限流器自身故障时放行，避免可用性依赖
            slog.Warn("ratelimit degraded", "err", err, "key", key)
            c.Next()
            return
        }
        c.Header("X-RateLimit-Limit", strconv.Itoa(res.Limit))
        c.Header("X-RateLimit-Remaining", strconv.Itoa(res.Remaining))
        c.Header("X-RateLimit-Reset", strconv.FormatInt(res.ResetAt.Unix(), 10))
        if !res.Allowed {
            c.Header("Retry-After", strconv.Itoa(int(res.RetryAfter.Seconds())))
            response.Fail(c, errs.ErrTooManyRequests.WithField("scope", scope.Name()))
            return
        }
        c.Next()
    }
}
```

限流器故障时选择放行而非拒绝，因为限流是防滥用手段而不是安全边界，安全边界由认证授权保证。

### 5.6.2 幂等与防重放

写接口分三类处理重复提交：

| 类别 | 判定方式 | 行为 |
| --- | --- | --- |
| 天然幂等 | PUT/PATCH 全量或部分更新、DELETE | 重复执行结果一致，无需额外机制 |
| 需幂等键 | POST 创建类、耗时任务触发类 | 客户端可选传 `Idempotency-Key`（UUID v4） |
| 强制幂等键 | 涉及计费、外部签发、不可逆变更（证书签发、备份恢复、系统升级） | 缺失时返回 `100001` 并提示 |

`Idempotency-Key` 处理流程：以 `idem:<uid>:<key>` 为主键写入 KV，值含请求体 SHA256 与响应快照，TTL 24 小时。

```
收到请求
  ├─ KV 无记录 → 抢占写入 pending（SETNX）→ 执行 → 回填响应快照 → 返回
  ├─ 记录为 pending → 返回 100011（资源正被占用），前端退避重试
  ├─ 记录已完成 & bodyHash 相同 → 直接回放快照，响应头带 Idempotent-Replay: true
  └─ 记录已完成 & bodyHash 不同 → 返回 100010（重复提交）
```

前端由请求层统一注入：`useRequest` 对 `method !== 'GET'` 且标记 `idempotent: true` 的调用自动生成并缓存 key，同一操作在网络抖动重试时复用同一 key（见 [前端架构与 Arco 规范](./06-前端架构与Arco规范.md) 请求层章节）。

API Token 签名请求的防重放独立于幂等：`Timestamp` 偏差窗口 300 秒、`Nonce` 在 KV 中保留 600 秒，两者共同保证签名不可复用（`110011`、`110012`）。会话 Token（JWT）不做 Nonce 校验，靠 HTTPS 与短过期保证。

### 5.6.3 危险操作二次确认

高危动作要求携带 `X-Confirm-Token`，缺失或无效返回 `100014`。

```
POST /api/v1/auth/confirm         { "permission": "cluster:node:delete", "password": "...", "code": "123456" }
→ { "code": 0, "data": { "confirmToken": "cft_9f8...", "expireIn": 300, "scope": "cluster:node:delete" } }
```

| 属性 | 规则 |
| --- | --- |
| 凭据 | 已绑定 2FA 时要求 TOTP，未绑定时要求当前登录密码 |
| 有效期 | 300 秒，一次性消费，用后立即从 KV 删除 |
| 作用域 | 绑定单个权限点，跨权限点复用返回 `100014` |
| 存储 | KV `confirm:<uid>:<token>`，值含权限点与签发时间 |
| 免除 | 同一权限点 60 秒内的连续操作可复用（批量删除场景），由前端在同一批次内传同一 token |

需要二次确认的动作清单见 5.4.8 权限点分布表的「需二次确认」列，典型包括：删除节点、删除站点及其数据、删除数据库、清空回收站、强制删除镜像、系统升级、执行恢复、修改防火墙放行规则、重置超管密码、吊销 API Token 全部、关闭审计外发。

### 5.6.4 其他安全约束

| 约束 | 规则 |
| --- | --- |
| 传输 | 强制 HTTPS，HSTS `max-age=31536000; includeSubDomains`；HTTP 端口仅做 301 |
| CORS | 默认同源，`server.cors.allow_origins` 显式白名单，不支持 `*` 与 `credentials` 并存 |
| CSRF | 状态变更接口要求 `Origin` 或 `Referer` 与白名单匹配；refreshToken Cookie 为 `SameSite=Strict; HttpOnly; Secure` |
| 响应头 | `X-Content-Type-Options: nosniff`、`X-Frame-Options: DENY`、`Referrer-Policy: no-referrer`、CSP 见 [安全合规与审计](./12-安全合规与审计.md) |
| 请求体上限 | 默认 10 MB（`server.max_body_size`），上传走分片接口不受此限 |
| 超时 | 读 15s、写 30s、空闲 120s；长任务一律异步化返回 `taskId`，不占用连接 |
| 敏感字段 | 密码、私钥、AK/SK 在响应中一律以 `null` 或掩码 `****` 返回，日志中经 `slog` ReplaceAttr 脱敏 |
| 错误信息 | 不回传绝对路径之外的宿主信息、不回传 SQL 原文、不回传内部 IP 拓扑 |
| 审计 | 所有非 GET 请求与敏感 GET（导出、下载、日志检索）写 `audit_operation` |

## 5.7 核心接口清单总表

本节按模块列出全部接口的方法、路径、权限点与幂等属性，作为路由注册与前端 API 层的唯一索引。列含义：

| 列 | 说明 |
| --- | --- |
| 方法与路径 | 省略 `/api/v1` 前缀；`{id}` 为雪花 ID 字符串 |
| 权限点 | 送入 `Authz` 中间件的标识；`—` 表示仅需登录，`public` 表示匿名可访问 |
| 节点 | `Y` 表示需 `X-Node-Id`（缺省落到主控节点），`M` 表示支持多节点批量，`—` 表示控制面本地 |
| 幂等 | `I` 建议传 `Idempotency-Key`，`I!` 强制，`—` 天然幂等或只读 |
| 确认 | `C` 需 `X-Confirm-Token` |

### 5.7.1 认证、用户与权限

| 方法与路径 | 说明 | 权限点 | 节点 | 幂等 | 确认 |
| --- | --- | --- | --- | --- | --- |
| `POST /auth/login` | 账号密码登录，可能返回需 2FA | public | — | — | |
| `POST /auth/2fa/verify` | 提交 TOTP 完成登录 | public | — | — | |
| `POST /auth/refresh` | 用 refreshToken 换新 accessToken | public | — | — | |
| `POST /auth/logout` | 注销当前会话 | — | — | — | |
| `GET /auth/captcha` | 获取图形验证码 | public | — | — | |
| `POST /auth/confirm` | 换取危险操作确认票据 | — | — | — | |
| `GET /auth/profile` | 当前用户信息、角色与权限点集合 | — | — | — | |
| `PUT /auth/profile` | 修改昵称、邮箱、语言、主题 | — | — | — | |
| `PUT /auth/password` | 自助修改密码 | — | — | — | |
| `GET /auth/sessions` | 我的登录会话列表 | — | — | — | |
| `DELETE /auth/sessions/{id}` | 下线指定会话 | — | — | — | |
| `POST /auth/2fa/bind` | 生成 TOTP 密钥与二维码 | — | — | — | |
| `POST /auth/2fa/confirm` | 校验并启用 2FA，返回恢复码 | — | — | — | |
| `DELETE /auth/2fa` | 解绑 2FA | — | — | — | C |
| `GET /user/users` | 用户列表 | `user:user:list` | — | — | |
| `POST /user/users` | 创建用户 | `user:user:create` | — | I | |
| `GET /user/users/{id}` | 用户详情 | `user:user:read` | — | — | |
| `PUT /user/users/{id}` | 修改用户 | `user:user:update` | — | — | |
| `DELETE /user/users/{id}` | 删除用户 | `user:user:delete` | — | — | C |
| `PUT /user/users/{id}/status` | 启用或停用 | `user:user:update` | — | — | |
| `PUT /user/users/{id}/password` | 重置密码 | `user:user:update` | — | — | C |
| `PUT /user/users/{id}/roles` | 分配角色 | `user:user:update` | — | — | |
| `DELETE /user/users/{id}/2fa` | 强制解绑 2FA | `user:user:update` | — | — | C |
| `POST /user/users/{id}/kick` | 强制下线全部会话 | `user:user:update` | — | — | |
| `GET /user/roles` | 角色列表 | `user:role:list` | — | — | |
| `POST /user/roles` | 创建角色 | `user:role:create` | — | I | |
| `GET /user/roles/{id}` | 角色详情含权限点 | `user:role:read` | — | — | |
| `PUT /user/roles/{id}` | 修改角色与权限点 | `user:role:update` | — | — | |
| `DELETE /user/roles/{id}` | 删除角色 | `user:role:delete` | — | — | C |
| `GET /user/permissions` | 权限点字典树 | `user:role:list` | — | — | |
| `GET /user/tokens` | API Token 列表 | `user:token:list` | — | — | |
| `POST /user/tokens` | 创建 Token，仅此次返回 SK | `user:token:create` | — | I | |
| `PUT /user/tokens/{id}` | 修改备注、IP 白名单、作用域 | `user:token:update` | — | — | |
| `DELETE /user/tokens/{id}` | 吊销 Token | `user:token:delete` | — | — | C |

### 5.7.2 概览与集群节点

| 方法与路径 | 说明 | 权限点 | 节点 | 幂等 | 确认 |
| --- | --- | --- | --- | --- | --- |
| `GET /dashboard/overview` | 首页卡片聚合：节点、站点、容器、告警计数 | `dashboard:overview:read` | — | — | |
| `GET /dashboard/resource` | 主控与选定节点的实时资源快照 | `dashboard:overview:read` | Y | — | |
| `GET /dashboard/todo` | 待处理项：证书即将过期、离线节点、未确认告警 | `dashboard:overview:read` | — | — | |
| `GET /dashboard/quick-actions` | 快捷入口配置 | `dashboard:overview:read` | — | — | |
| `GET /system/health` | 健康检查，用于探针 | public | — | — | |
| `GET /system/info` | 面板版本、构建信息、许可信息 | — | — | — | |
| `PUT /system/log-level` | 动态调整日志级别 | `setting:system:update` | — | — | |
| `GET /cluster/nodes` | 节点列表，支持分组与标签过滤 | `cluster:node:list` | — | — | |
| `POST /cluster/nodes` | 登记节点并生成 bootstrap token | `cluster:node:create` | — | I | |
| `GET /cluster/nodes/{id}` | 节点详情含硬件、组件、版本 | `cluster:node:read` | — | — | |
| `PUT /cluster/nodes/{id}` | 修改别名、分组、标签、备注 | `cluster:node:update` | — | — | |
| `DELETE /cluster/nodes/{id}` | 移除节点 | `cluster:node:delete` | — | — | C |
| `POST /cluster/nodes/{id}/reconnect` | 主动触发重连探测 | `cluster:node:exec` | — | — | |
| `POST /cluster/nodes/{id}/upgrade` | 升级单节点 Agent | `cluster:node:exec` | — | I | |
| `POST /cluster/nodes/{id}/cert/rotate` | 轮换节点证书 | `cluster:node:update` | — | I | |
| `GET /cluster/nodes/{id}/metrics` | 节点实时指标 | `cluster:node:read` | — | — | |
| `POST /cluster/nodes/install` | SSH 批量安装 Agent | `cluster:node:create` | — | I! | |
| `GET /cluster/nodes/install/{taskId}` | 安装任务进度与逐机结果 | `cluster:node:read` | — | — | |
| `GET /cluster/groups` | 分组列表 | `cluster:group:list` | — | — | |
| `POST /cluster/groups` | 创建分组 | `cluster:group:create` | — | I | |
| `PUT /cluster/groups/{id}` | 修改分组 | `cluster:group:update` | — | — | |
| `DELETE /cluster/groups/{id}` | 删除分组 | `cluster:group:delete` | — | — | |
| `GET /cluster/tags` | 标签列表与引用计数 | `cluster:group:list` | — | — | |
| `POST /cluster/tasks` | 创建批量任务 | `cluster:task:create` | M | I! | C |
| `GET /cluster/tasks` | 批量任务列表 | `cluster:task:list` | — | — | |
| `GET /cluster/tasks/{id}` | 任务详情与逐节点状态 | `cluster:task:read` | — | — | |
| `POST /cluster/tasks/{id}/cancel` | 取消未开始的子任务 | `cluster:task:exec` | — | — | |
| `POST /cluster/tasks/{id}/retry` | 重试失败子任务 | `cluster:task:exec` | — | I | |
| `GET /cluster/tasks/{id}/nodes/{nodeId}/log` | 单节点执行日志 | `cluster:task:read` | — | — | |
| `GET /cluster/agent/versions` | 可用 Agent 版本列表 | `cluster:node:list` | — | — | |
| `GET /public/agent/bootstrap` | Agent 首次注册引导（携 token） | public | — | — | |
| `GET /public/agent/binary` | 下载 Agent 二进制 | public | — | — | |
| `GET /public/agent/install` | 获取一键安装脚本 | public | — | — | |

### 5.7.3 网站、证书、数据库与运行环境

| 方法与路径 | 说明 | 权限点 | 节点 | 幂等 | 确认 |
| --- | --- | --- | --- | --- | --- |
| `GET /website/sites` | 站点列表 | `website:site:list` | M | — | |
| `POST /website/sites` | 创建站点（静态、PHP、反代、Node、Java、Python、负载均衡） | `website:site:create` | Y | I | |
| `GET /website/sites/{id}` | 站点详情 | `website:site:read` | Y | — | |
| `PUT /website/sites/{id}` | 修改基础配置 | `website:site:update` | Y | — | |
| `DELETE /website/sites/{id}` | 删除站点，可选连带目录与数据库 | `website:site:delete` | Y | — | C |
| `PUT /website/sites/{id}/status` | 启停站点 | `website:site:update` | Y | — | |
| `GET /website/sites/{id}/domains` | 域名与端口绑定列表 | `website:site:read` | Y | — | |
| `POST /website/sites/{id}/domains` | 新增域名绑定 | `website:site:update` | Y | I | |
| `DELETE /website/sites/{id}/domains/{did}` | 解绑域名 | `website:site:update` | Y | — | |
| `GET /website/sites/{id}/config` | 读取 OpenResty 配置原文 | `website:site:read` | Y | — | |
| `PUT /website/sites/{id}/config` | 直接编辑配置（先 test 后 reload，失败回滚） | `website:site:update` | Y | — | C |
| `GET /website/sites/{id}/rewrite` | 伪静态规则 | `website:site:read` | Y | — | |
| `PUT /website/sites/{id}/rewrite` | 设置伪静态（内置模板或自定义） | `website:site:update` | Y | — | |
| `GET /website/sites/{id}/ssl` | SSL 配置与当前证书 | `website:site:read` | Y | — | |
| `PUT /website/sites/{id}/ssl` | 绑定证书、开关强制跳转与 HSTS | `website:site:update` | Y | — | |
| `GET /website/sites/{id}/logs` | 访问与错误日志配置及切割策略 | `website:site:read` | Y | — | |
| `PUT /website/sites/{id}/logs` | 修改日志配置 | `website:site:update` | Y | — | |
| `GET /website/sites/{id}/security` | 防盗链、IP 黑白名单、目录保护、限速 | `website:site:read` | Y | — | |
| `PUT /website/sites/{id}/security` | 修改站点安全策略 | `website:site:update` | Y | — | |
| `POST /website/sites/{id}/redirect` | 配置重定向规则 | `website:site:update` | Y | — | |
| `GET /website/sites/{id}/backups` | 站点备份列表 | `website:site:read` | Y | — | |
| `POST /website/sites/{id}/backups` | 立即备份站点 | `website:site:exec` | Y | I | |
| `GET /website/templates` | 站点类型模板与默认参数 | `website:site:list` | — | — | |
| `POST /website/nginx/reload` | 全局 reload | `website:service:exec` | Y | — | |
| `GET /website/nginx/config` | 主配置与性能参数 | `website:service:read` | Y | — | |
| `PUT /website/nginx/config` | 修改主配置 | `website:service:update` | Y | — | C |
| `GET /website/nginx/status` | 连接数、QPS、worker 状态 | `website:service:read` | Y | — | |
| `GET /cert/certs` | 证书列表含到期天数 | `cert:cert:list` | M | — | |
| `POST /cert/certs` | 上传自有证书 | `cert:cert:create` | — | I | |
| `GET /cert/certs/{id}` | 证书详情与部署引用 | `cert:cert:read` | — | — | |
| `DELETE /cert/certs/{id}` | 删除证书 | `cert:cert:delete` | — | — | C |
| `POST /cert/certs/{id}/deploy` | 部署到站点或节点 | `cert:cert:exec` | M | I | |
| `GET /cert/certs/{id}/download` | 下载证书包 | `cert:cert:export` | — | — | |
| `POST /cert/orders` | 申请 ACME 证书 | `cert:order:create` | — | I! | |
| `GET /cert/orders/{id}` | 签发进度与校验记录 | `cert:order:read` | — | — | |
| `POST /cert/orders/{id}/renew` | 手动续签 | `cert:order:exec` | — | I | |
| `GET /cert/accounts` | ACME 账户列表 | `cert:account:list` | — | — | |
| `POST /cert/accounts` | 注册 ACME 账户 | `cert:account:create` | — | I | |
| `GET /cert/dns-providers` | DNS 服务商凭证列表 | `cert:dns:list` | — | — | |
| `POST /cert/dns-providers` | 添加 DNS 凭证 | `cert:dns:create` | — | I | |
| `POST /cert/dns-providers/{id}/test` | 测试 DNS 凭证 | `cert:dns:exec` | — | — | |
| `GET /database/instances` | 数据库服务实例列表（本机与远程） | `database:instance:list` | M | — | |
| `POST /database/instances` | 登记远程实例 | `database:instance:create` | — | I | |
| `PUT /database/instances/{id}` | 修改实例连接信息 | `database:instance:update` | — | — | |
| `DELETE /database/instances/{id}` | 移除实例登记 | `database:instance:delete` | — | — | C |
| `POST /database/instances/{id}/test` | 连通性测试 | `database:instance:exec` | — | — | |
| `GET /database/databases` | 数据库列表 | `database:database:list` | M | — | |
| `POST /database/databases` | 创建库与账号 | `database:database:create` | Y | I | |
| `DELETE /database/databases/{id}` | 删除库 | `database:database:delete` | Y | — | C |
| `PUT /database/databases/{id}/password` | 重置账号密码 | `database:database:update` | Y | — | C |
| `PUT /database/databases/{id}/permission` | 修改账号权限与访问来源 | `database:database:update` | Y | — | |
| `POST /database/databases/{id}/backup` | 备份单库 | `database:database:exec` | Y | I | |
| `POST /database/databases/{id}/restore` | 从备份恢复 | `database:database:exec` | Y | I! | C |
| `POST /database/databases/{id}/import` | 导入 SQL 文件 | `database:database:exec` | Y | I | |
| `GET /database/databases/{id}/export` | 导出 SQL | `database:database:export` | Y | — | |
| `GET /database/service/config` | 数据库服务配置（my.cnf 等） | `database:service:read` | Y | — | |
| `PUT /database/service/config` | 修改服务配置 | `database:service:update` | Y | — | C |
| `GET /database/service/status` | 运行状态、连接数、慢查询计数 | `database:service:read` | Y | — | |
| `GET /database/service/slow-log` | 慢查询日志 | `database:service:read` | Y | — | |
| `POST /database/service/{action}` | 启动、停止、重启服务 | `database:service:exec` | Y | — | C |
| `GET /runtime/runtimes` | 运行环境列表（PHP、Node、Java、Python、Go） | `runtime:runtime:list` | M | — | |
| `POST /runtime/runtimes` | 安装指定版本 | `runtime:runtime:create` | Y | I | |
| `DELETE /runtime/runtimes/{id}` | 卸载版本 | `runtime:runtime:delete` | Y | — | C |
| `GET /runtime/runtimes/{id}/config` | 配置文件（php.ini 等） | `runtime:runtime:read` | Y | — | |
| `PUT /runtime/runtimes/{id}/config` | 修改配置 | `runtime:runtime:update` | Y | — | |
| `GET /runtime/runtimes/{id}/extensions` | 扩展列表与状态 | `runtime:runtime:read` | Y | — | |
| `POST /runtime/runtimes/{id}/extensions` | 安装扩展 | `runtime:runtime:exec` | Y | I | |
| `DELETE /runtime/runtimes/{id}/extensions/{name}` | 卸载扩展 | `runtime:runtime:exec` | Y | — | |
| `POST /runtime/runtimes/{id}/{action}` | 启停重载运行时服务 | `runtime:runtime:exec` | Y | — | |
| `GET /runtime/processes` | 进程守护列表 | `runtime:process:list` | M | — | |
| `POST /runtime/processes` | 新增守护进程 | `runtime:process:create` | Y | I | |
| `PUT /runtime/processes/{id}` | 修改守护配置 | `runtime:process:update` | Y | — | |
| `DELETE /runtime/processes/{id}` | 删除守护进程 | `runtime:process:delete` | Y | — | C |
| `POST /runtime/processes/{id}/{action}` | 启停重启守护进程 | `runtime:process:exec` | Y | — | |
| `GET /runtime/software` | 软件商店列表与安装状态 | `runtime:software:list` | M | — | |
| `POST /runtime/software/{key}/install` | 安装软件 | `runtime:software:create` | Y | I | |
| `DELETE /runtime/software/{key}` | 卸载软件 | `runtime:software:delete` | Y | — | C |
| `GET /database/ftp/accounts` | FTP 账号列表 | `database:ftp:list` | M | — | |
| `POST /database/ftp/accounts` | 创建 FTP 账号 | `database:ftp:create` | Y | I | |
| `PUT /database/ftp/accounts/{id}` | 修改账号（目录、配额、状态） | `database:ftp:update` | Y | — | |
| `DELETE /database/ftp/accounts/{id}` | 删除账号 | `database:ftp:delete` | Y | — | |

### 5.7.4 文件与终端

| 方法与路径 | 说明 | 权限点 | 节点 | 幂等 | 确认 |
| --- | --- | --- | --- | --- | --- |
| `GET /file/roots` | 白名单根目录列表（前端目录树入口） | `file:file:list` | Y | — | |
| `GET /file/list` | 目录列表，支持隐藏文件与排序 | `file:file:list` | Y | — | |
| `GET /file/stat` | 单个文件或目录属性 | `file:file:read` | Y | — | |
| `GET /file/content` | 读取文本内容，返回 ETag | `file:file:read` | Y | — | |
| `PUT /file/content` | 保存文本内容，带 `If-Match` 乐观锁 | `file:file:update` | Y | — | |
| `POST /file/dir` | 新建目录 | `file:file:create` | Y | I | |
| `POST /file/file` | 新建空文件 | `file:file:create` | Y | I | |
| `POST /file/rename` | 重命名 | `file:file:update` | Y | — | |
| `POST /file/move` | 移动（可跨目录，支持覆盖策略） | `file:file:update` | Y | I | |
| `POST /file/copy` | 复制 | `file:file:create` | Y | I | |
| `DELETE /file/items` | 删除（当前为直接删除，回收站未实现） | `file:file:delete` | Y | — | C |
| `PUT /file/permission` | 修改权限与属主，可递归 | `file:permission:update` | Y | — | C |
| `POST /file/compress` | 压缩为 zip/tar.gz/tar（tar.zst 未实现） | `file:file:exec` | Y | I | |
| `POST /file/decompress` | 解压（zip/tar/tar.gz/tar.bz2） | `file:file:exec` | Y | I | |
| `GET /file/search` | 按名称、大小、时间、内容检索 | `file:file:list` | Y | — | |
| `GET /file/du` | 目录占用统计 | `file:file:read` | Y | — | |
| `POST /file/upload` | 单文件整体上传（multipart，受 `file.max_upload_size` 限制） | `file:file:create` | Y | — | |
| `POST /file/upload/init` | 初始化分片上传，返回 uploadId；命中同名同哈希文件时秒传 | `file:file:create` | Y | I | |
| `POST /file/upload/chunk` | 上传单个分片（multipart，逐片校验和） | `file:file:create` | Y | — | |
| `POST /file/upload/complete` | 合并分片并落地 | `file:file:create` | Y | — | |
| `GET /file/upload/status` | 查询已上传/缺失分片，用于断点续传 | `file:file:create` | Y | — | |
| `DELETE /file/upload/{uploadId}` | 放弃上传并清理临时分片（幂等） | `file:file:create` | Y | — | |
| `GET /file/download` | 单文件下载，支持 Range | `file:file:read` | Y | — | |
| `POST /file/download/archive` | 多选打包下载，返回 taskId（未实现） | `file:file:export` | Y | I | |
| `POST /file/remote-download` | 从 URL 下载到服务器（未实现） | `file:file:create` | Y | I | |
| `GET /file/recycle` | 回收站列表（未实现） | `file:recycle:list` | Y | — | |
| `POST /file/recycle/{id}/restore` | 还原（未实现） | `file:recycle:update` | Y | — | |
| `DELETE /file/recycle/{id}` | 彻底删除单项（未实现） | `file:recycle:delete` | Y | — | C |
| `DELETE /file/recycle` | 清空回收站（未实现） | `file:recycle:delete` | Y | — | C |
| `GET /file/transfers` | 传输任务列表（上传、下载、压缩、远程拉取；未实现） | `file:transfer:list` | Y | — | |
| `POST /file/transfers/{id}/cancel` | 取消传输任务（未实现） | `file:transfer:exec` | Y | — | |
| `GET /file/shares` | 文件分享列表（未实现） | `file:share:list` | — | — | |
| `POST /file/shares` | 创建分享链接（有效期、密码、下载次数；未实现） | `file:share:create` | Y | I | |
| `DELETE /file/shares/{id}` | 撤销分享（未实现） | `file:share:delete` | — | — | |
| `GET /public/share/{code}` | 匿名访问分享内容（未实现） | public | — | — | |
| `GET /file/favorites` | 收藏目录列表（未实现） | `file:file:list` | — | — | |
| `POST /file/favorites` | 添加收藏（未实现） | `file:file:create` | — | I | |
| `GET /terminal/sessions` | 在线终端会话列表 | `terminal:session:list` | M | — | |
| `POST /terminal/sessions` | 创建会话，返回 sessionId 与 ws ticket | `terminal:session:create` | Y | I | |
| `DELETE /terminal/sessions/{id}` | 强制结束会话 | `terminal:session:delete` | — | — | |
| `POST /terminal/ticket` | 单独换取 WebSocket ticket | `terminal:session:create` | Y | — | |
| `POST /terminal/batch` | 批量执行命令（多节点广播） | `terminal:batch:exec` | M | I! | C |
| `GET /terminal/batch/{taskId}` | 批量执行结果 | `terminal:batch:read` | — | — | |
| `GET /terminal/recordings` | 录像列表 | `terminal:recording:list` | M | — | |
| `GET /terminal/recordings/{id}` | 录像元数据 | `terminal:recording:read` | — | — | |
| `GET /terminal/recordings/{id}/cast` | 下载 asciicast 文件用于回放 | `terminal:recording:export` | — | — | |
| `DELETE /terminal/recordings/{id}` | 删除录像 | `terminal:recording:delete` | — | — | C |
| `GET /terminal/rules` | 高危命令规则列表 | `terminal:rule:list` | — | — | |
| `POST /terminal/rules` | 新增规则（拦截、告警、审批） | `terminal:rule:create` | — | I | |
| `PUT /terminal/rules/{id}` | 修改规则 | `terminal:rule:update` | — | — | |
| `DELETE /terminal/rules/{id}` | 删除规则 | `terminal:rule:delete` | — | — | |
| `GET /terminal/credentials` | SSH 凭证列表（密码、私钥） | `terminal:credential:list` | — | — | |
| `POST /terminal/credentials` | 新增凭证，私钥加密入库 | `terminal:credential:create` | — | I | |
| `PUT /terminal/credentials/{id}` | 修改凭证 | `terminal:credential:update` | — | — | |
| `DELETE /terminal/credentials/{id}` | 删除凭证 | `terminal:credential:delete` | — | — | C |
| `GET /terminal/audits` | 终端命令审计检索 | `terminal:audit:list` | M | — | |
| `GET /terminal/audits/export` | 导出命令审计 | `terminal:audit:export` | — | — | |
| `GET /terminal/shares` | 会话协作分享列表 | `terminal:share:list` | — | — | |
| `POST /terminal/shares` | 创建只读协作分享 | `terminal:share:create` | — | I | |
| `DELETE /terminal/shares/{id}` | 撤销分享 | `terminal:share:delete` | — | — | |

### 5.7.5 容器、编排与镜像仓库

| 方法与路径 | 说明 | 权限点 | 节点 | 幂等 | 确认 |
| --- | --- | --- | --- | --- | --- |
| `GET /container/containers` | 容器列表 | `container:container:list` | M | — | |
| `POST /container/containers` | 创建并启动容器 | `container:container:create` | Y | I | |
| `GET /container/containers/{id}` | 容器详情（inspect 精简） | `container:container:read` | Y | — | |
| `PUT /container/containers/{id}` | 更新资源限制与重启策略 | `container:container:update` | Y | — | |
| `DELETE /container/containers/{id}` | 删除容器，可选连带卷 | `container:container:delete` | Y | — | C |
| `POST /container/containers/{id}/{action}` | start/stop/restart/pause/unpause/kill | `container:container:exec` | Y | — | |
| `POST /container/containers/{id}/rename` | 重命名 | `container:container:update` | Y | — | |
| `POST /container/containers/{id}/recreate` | 按新参数重建（保留卷） | `container:container:update` | Y | I | C |
| `GET /container/containers/{id}/logs` | 拉取历史日志（分页、时间窗） | `container:container:read` | Y | — | |
| `GET /container/containers/{id}/stats` | 单次资源采样 | `container:container:read` | Y | — | |
| `GET /container/containers/{id}/processes` | 容器内进程列表 | `container:container:read` | Y | — | |
| `GET /container/containers/{id}/files` | 浏览容器内文件 | `container:container:read` | Y | — | |
| `POST /container/containers/{id}/exec` | 创建 exec 会话，返回 ws ticket | `container:container:exec` | Y | — | |
| `POST /container/containers/{id}/commit` | 提交为镜像 | `container:image:create` | Y | I | |
| `GET /container/images` | 镜像列表 | `container:image:list` | M | — | |
| `POST /container/images/pull` | 拉取镜像，返回 taskId | `container:image:create` | Y | I | |
| `POST /container/images/build` | 构建镜像 | `container:image:create` | Y | I | |
| `POST /container/images/push` | 推送镜像 | `container:image:exec` | Y | I | |
| `DELETE /container/images/{id}` | 删除镜像 | `container:image:delete` | Y | — | C |
| `POST /container/images/{id}/tag` | 打标签 | `container:image:update` | Y | — | |
| `GET /container/images/{id}/history` | 镜像层历史 | `container:image:read` | Y | — | |
| `POST /container/images/import` | 从 tar 导入 | `container:image:create` | Y | I | |
| `GET /container/images/{id}/export` | 导出为 tar | `container:image:export` | Y | — | |
| `GET /container/networks` | 网络列表 | `container:network:list` | M | — | |
| `POST /container/networks` | 创建网络 | `container:network:create` | Y | I | |
| `DELETE /container/networks/{id}` | 删除网络 | `container:network:delete` | Y | — | |
| `POST /container/networks/{id}/connect` | 容器加入网络 | `container:network:update` | Y | — | |
| `GET /container/volumes` | 卷列表与占用 | `container:volume:list` | M | — | |
| `POST /container/volumes` | 创建卷 | `container:volume:create` | Y | I | |
| `DELETE /container/volumes/{id}` | 删除卷 | `container:volume:delete` | Y | — | C |
| `GET /container/host/daemon-config` | 读取 daemon.json | `container:host:read` | Y | — | |
| `PUT /container/host/daemon-config` | 修改并校验 daemon.json | `container:host:update` | Y | — | C |
| `POST /container/host/{action}` | Docker 服务启停重启 | `container:host:exec` | Y | — | C |
| `POST /container/host/install` | 安装 Docker | `container:host:create` | Y | I | |
| `GET /container/system/df` | 磁盘占用汇总 | `container:host:read` | Y | — | |
| `POST /container/system/prune` | 清理未使用资源 | `container:host:exec` | Y | I | C |
| `GET /container/registries` | 镜像仓库列表 | `container:registry:list` | — | — | |
| `POST /container/registries` | 添加仓库并登录校验 | `container:registry:create` | — | I | |
| `PUT /container/registries/{id}` | 修改仓库配置 | `container:registry:update` | — | — | |
| `DELETE /container/registries/{id}` | 删除仓库 | `container:registry:delete` | — | — | |
| `GET /container/registries/{id}/repositories` | 浏览远端仓库与标签 | `container:registry:read` | — | — | |
| `GET /container/compose/stacks` | 编排项目列表 | `container:compose:list` | M | — | |
| `POST /container/compose/stacks` | 创建项目（上传或编辑 compose.yaml） | `container:compose:create` | Y | I | |
| `GET /container/compose/stacks/{id}` | 项目详情与服务状态 | `container:compose:read` | Y | — | |
| `PUT /container/compose/stacks/{id}` | 修改 compose 与 env | `container:compose:update` | Y | — | |
| `DELETE /container/compose/stacks/{id}` | 删除项目 | `container:compose:delete` | Y | — | C |
| `POST /container/compose/stacks/{id}/{action}` | up/down/restart/pull/stop/start | `container:compose:exec` | Y | I | |
| `POST /container/compose/validate` | 校验 compose 语法 | `container:compose:read` | Y | — | |
| `GET /container/compose/stacks/{id}/services/{name}` | 单服务详情 | `container:compose:read` | Y | — | |
| `POST /container/compose/stacks/{id}/services/{name}/scale` | 调整副本数 | `container:compose:exec` | Y | — | |
| `GET /container/k8s/clusters` | 纳管的 K8s 集群列表 | `container:k8s:list` | — | — | |
| `POST /container/k8s/clusters` | 导入 kubeconfig 纳管集群 | `container:k8s:create` | — | I | |
| `GET /container/k8s/clusters/{id}/{resource}` | 通用资源列表（nodes/pods/deployments 等） | `container:k8s:read` | — | — | |
| `POST /container/k8s/clusters/{id}/apply` | 应用 YAML 清单 | `container:k8s:exec` | — | I | C |

### 5.7.6 监控、告警与通知

| 方法与路径 | 说明 | 权限点 | 节点 | 幂等 | 确认 |
| --- | --- | --- | --- | --- | --- |
| `GET /monitor/overview` | 监控总览：集群水位与 TopN | `monitor:metric:read` | — | — | |
| `POST /monitor/metric/query` | 指标查询（单点、范围、多序列） | `monitor:metric:read` | M | — | |
| `POST /monitor/metric/topn` | TopN 排名（节点、进程、容器、站点） | `monitor:metric:read` | M | — | |
| `GET /monitor/metric/catalog` | 指标字典与标签枚举 | `monitor:metric:read` | — | — | |
| `POST /monitor/log/search` | 日志检索 | `monitor:log:list` | M | — | |
| `POST /monitor/log/tail` | 申请日志 tail 的 ws ticket | `monitor:log:read` | Y | — | |
| `GET /monitor/log/sources` | 已接入日志源 | `monitor:log:list` | Y | — | |
| `POST /monitor/log/sources` | 新增日志源 | `monitor:log:create` | Y | I | |
| `GET /monitor/dashboard` | 仪表盘列表 | `monitor:dashboard:list` | — | — | |
| `POST /monitor/dashboard` | 创建仪表盘 | `monitor:dashboard:create` | — | I | |
| `GET /monitor/dashboard/{id}` | 仪表盘定义 | `monitor:dashboard:read` | — | — | |
| `PUT /monitor/dashboard/{id}` | 修改布局与面板 | `monitor:dashboard:update` | — | — | |
| `DELETE /monitor/dashboard/{id}` | 删除仪表盘 | `monitor:dashboard:delete` | — | — | |
| `POST /monitor/panel/preview` | 面板预览（不落库） | `monitor:dashboard:read` | — | — | |
| `GET /monitor/report` | 巡检报告列表 | `monitor:report:list` | — | — | |
| `POST /monitor/report` | 立即生成巡检报告 | `monitor:report:create` | M | I | |
| `GET /monitor/report/{id}` | 报告详情 | `monitor:report:read` | — | — | |
| `GET /monitor/report/{id}/export` | 导出 PDF 或 HTML | `monitor:report:export` | — | — | |
| `GET /monitor/storage/config` | 时序存储与保留策略 | `monitor:storage:read` | — | — | |
| `PUT /monitor/storage/config` | 修改保留与降采样策略 | `monitor:storage:update` | — | — | C |
| `GET /monitor/storage/stats` | 存储占用与写入速率 | `monitor:storage:read` | — | — | |
| `GET /alert/rule` | 告警规则列表 | `alert:rule:list` | — | — | |
| `POST /alert/rule` | 创建规则 | `alert:rule:create` | — | I | |
| `GET /alert/rule/{id}` | 规则详情 | `alert:rule:read` | — | — | |
| `PUT /alert/rule/{id}` | 修改规则 | `alert:rule:update` | — | — | |
| `DELETE /alert/rule/{id}` | 删除规则 | `alert:rule:delete` | — | — | |
| `PUT /alert/rule/{id}/status` | 启停规则 | `alert:rule:update` | — | — | |
| `POST /alert/rule/validate` | 校验表达式并试算 | `alert:rule:read` | — | — | |
| `GET /alert/rule/templates` | 内置规则模板 | `alert:rule:list` | — | — | |
| `GET /alert/event` | 告警事件列表 | `alert:event:list` | M | — | |
| `GET /alert/event/{id}` | 事件详情与关联指标快照 | `alert:event:read` | — | — | |
| `POST /alert/event/{id}/ack` | 确认事件 | `alert:event:update` | — | — | |
| `POST /alert/event/{id}/close` | 手动关闭事件 | `alert:event:update` | — | — | |
| `POST /alert/event/{id}/comment` | 追加处理备注 | `alert:event:update` | — | — | |
| `GET /alert/event/export` | 导出事件 | `alert:event:export` | — | — | |
| `GET /alert/silence` | 静默规则列表 | `alert:silence:list` | — | — | |
| `POST /alert/silence` | 创建静默 | `alert:silence:create` | — | I | |
| `DELETE /alert/silence/{id}` | 删除静默 | `alert:silence:delete` | — | — | |
| `GET /alert/notify-log` | 通知发送记录 | `alert:notify:list` | — | — | |
| `POST /alert/notify-log/{id}/resend` | 重发通知 | `alert:notify:exec` | — | I | |
| `POST /alert/heal/{eventId}/approve` | 审批自愈动作 | `alert:heal:exec` | — | — | C |
| `GET /alert/heal/actions` | 自愈动作模板列表 | `alert:heal:list` | — | — | |
| `GET /notify/channel` | 通知通道列表 | `alert:channel:list` | — | — | |
| `POST /notify/channel` | 新增通道（邮件、Webhook、钉钉、企微、飞书、Slack、短信、Telegram） | `alert:channel:create` | — | I | |
| `PUT /notify/channel/{id}` | 修改通道 | `alert:channel:update` | — | — | |
| `DELETE /notify/channel/{id}` | 删除通道 | `alert:channel:delete` | — | — | |
| `POST /notify/channel/{id}/test` | 发送测试通知 | `alert:channel:exec` | — | — | |
| `GET /notify/template` | 通知模板列表 | `alert:template:list` | — | — | |
| `POST /notify/template` | 新增模板 | `alert:template:create` | — | I | |
| `PUT /notify/template/{id}` | 修改模板 | `alert:template:update` | — | — | |
| `POST /notify/template/{id}/preview` | 渲染预览 | `alert:template:read` | — | — | |

### 5.7.7 安全、审计、应用商店、计划任务、备份与设置

| 方法与路径 | 说明 | 权限点 | 节点 | 幂等 | 确认 |
| --- | --- | --- | --- | --- | --- |
| `GET /security/firewall/rules` | 防火墙规则列表 | `security:firewall:list` | M | — | |
| `POST /security/firewall/rules` | 新增放行或拒绝规则 | `security:firewall:create` | Y | I | C |
| `PUT /security/firewall/rules/{id}` | 修改规则 | `security:firewall:update` | Y | — | C |
| `DELETE /security/firewall/rules/{id}` | 删除规则 | `security:firewall:delete` | Y | — | C |
| `GET /security/firewall/status` | 防火墙后端类型与运行状态 | `security:firewall:read` | Y | — | |
| `POST /security/firewall/{action}` | 启停防火墙 | `security:firewall:exec` | Y | — | C |
| `GET /security/ip-rules` | IP 黑白名单 | `security:ip:list` | — | — | |
| `POST /security/ip-rules` | 新增条目 | `security:ip:create` | — | I | |
| `DELETE /security/ip-rules/{id}` | 删除条目 | `security:ip:delete` | — | — | |
| `GET /security/ssh/config` | SSH 加固当前配置 | `security:ssh:read` | Y | — | |
| `PUT /security/ssh/config` | 修改端口、禁 root、密钥登录等 | `security:ssh:update` | Y | — | C |
| `GET /security/ssh/keys` | 授权公钥列表 | `security:ssh:list` | Y | — | |
| `POST /security/ssh/keys` | 添加公钥 | `security:ssh:create` | Y | I | |
| `DELETE /security/ssh/keys/{id}` | 移除公钥 | `security:ssh:delete` | Y | — | C |
| `GET /security/fail2ban` | 封禁状态与规则 | `security:fail2ban:read` | Y | — | |
| `PUT /security/fail2ban` | 修改封禁策略 | `security:fail2ban:update` | Y | — | |
| `DELETE /security/fail2ban/bans/{ip}` | 解封 IP | `security:fail2ban:delete` | Y | — | |
| `GET /security/waf/config` | WAF 全局配置 | `security:waf:read` | Y | — | |
| `PUT /security/waf/config` | 修改 WAF 模式与阈值 | `security:waf:update` | Y | — | |
| `GET /security/waf/rules` | WAF 规则列表 | `security:waf:list` | Y | — | |
| `POST /security/waf/rules` | 新增自定义规则 | `security:waf:create` | Y | I | |
| `PUT /security/waf/rules/{id}` | 修改规则或启停 | `security:waf:update` | Y | — | |
| `DELETE /security/waf/rules/{id}` | 删除规则 | `security:waf:delete` | Y | — | |
| `GET /security/waf/logs` | 拦截日志 | `security:waf:list` | M | — | |
| `GET /security/ids/events` | 入侵检测事件 | `security:ids:list` | M | — | |
| `PUT /security/ids/config` | 修改检测策略 | `security:ids:update` | Y | — | |
| `GET /security/baseline/items` | 基线核查项与状态 | `security:baseline:list` | M | — | |
| `POST /security/baseline/scan` | 触发基线核查 | `security:baseline:exec` | M | I | |
| `POST /security/baseline/items/{id}/fix` | 一键修复单项 | `security:baseline:exec` | Y | I | C |
| `GET /security/baseline/reports/{id}` | 核查报告 | `security:baseline:read` | — | — | |
| `GET /security/vuln/scans` | 漏洞扫描任务列表 | `security:vuln:list` | — | — | |
| `POST /security/vuln/scans` | 触发扫描（系统包、镜像、依赖） | `security:vuln:create` | M | I | |
| `GET /security/vuln/scans/{id}` | 扫描结果与修复建议 | `security:vuln:read` | — | — | |
| `GET /security/panel/config` | 面板自身加固配置 | `security:panel:read` | — | — | |
| `PUT /security/panel/config` | 修改端口、安全入口、IP 限制、会话策略 | `security:panel:update` | — | — | C |
| `GET /audit/operations` | 操作审计检索 | `audit:operation:list` | — | — | |
| `GET /audit/operations/{id}` | 单条审计详情含前后变更 | `audit:operation:read` | — | — | |
| `GET /audit/operations/export` | 导出审计（CSV、JSON） | `audit:operation:export` | — | — | |
| `GET /audit/logins` | 登录审计 | `audit:login:list` | — | — | |
| `POST /audit/verify` | 校验哈希链完整性 | `audit:operation:exec` | — | — | |
| `GET /audit/forward/config` | 外部转发配置（syslog、Kafka、S3） | `audit:forward:read` | — | — | |
| `PUT /audit/forward/config` | 修改转发配置 | `audit:forward:update` | — | — | C |
| `GET /audit/archives` | 归档批次列表 | `audit:archive:list` | — | — | |
| `POST /audit/archives` | 手动归档 | `audit:archive:create` | — | I | |
| `GET /appstore/apps` | 应用列表（分类、标签、关键词） | `appstore:app:list` | — | — | |
| `GET /appstore/apps/{key}` | 应用详情与版本列表 | `appstore:app:read` | — | — | |
| `GET /appstore/apps/{key}/params` | 安装参数表单定义 | `appstore:app:read` | — | — | |
| `GET /appstore/repos` | 应用仓库列表 | `appstore:repo:list` | — | — | |
| `POST /appstore/repos` | 添加仓库 | `appstore:repo:create` | — | I | |
| `POST /appstore/repos/{id}/sync` | 同步索引 | `appstore:repo:exec` | — | I | |
| `DELETE /appstore/repos/{id}` | 删除仓库 | `appstore:repo:delete` | — | — | |
| `GET /appstore/instances` | 已安装实例列表 | `appstore:instance:list` | M | — | |
| `POST /appstore/instances` | 安装应用 | `appstore:instance:create` | Y | I! | |
| `GET /appstore/instances/{id}` | 实例详情、参数、访问地址 | `appstore:instance:read` | Y | — | |
| `PUT /appstore/instances/{id}` | 修改参数并重建 | `appstore:instance:update` | Y | I | C |
| `DELETE /appstore/instances/{id}` | 卸载实例，可选保留数据 | `appstore:instance:delete` | Y | — | C |
| `POST /appstore/instances/{id}/{action}` | start/stop/restart | `appstore:instance:exec` | Y | — | |
| `POST /appstore/instances/{id}/upgrade` | 升级到指定版本 | `appstore:instance:exec` | Y | I! | C |
| `GET /appstore/instances/{id}/logs` | 实例日志 | `appstore:instance:read` | Y | — | |
| `GET /cron/jobs` | 计划任务列表 | `cron:job:list` | M | — | |
| `POST /cron/jobs` | 创建任务（脚本、备份、日志切割、URL 探测、同步） | `cron:job:create` | M | I | |
| `GET /cron/jobs/{id}` | 任务详情 | `cron:job:read` | — | — | |
| `PUT /cron/jobs/{id}` | 修改任务 | `cron:job:update` | — | — | |
| `DELETE /cron/jobs/{id}` | 删除任务 | `cron:job:delete` | — | — | C |
| `PUT /cron/jobs/{id}/status` | 启停任务 | `cron:job:update` | — | — | |
| `POST /cron/jobs/{id}/run` | 立即执行一次 | `cron:job:exec` | — | I | |
| `GET /cron/jobs/{id}/records` | 执行历史 | `cron:job:read` | — | — | |
| `GET /cron/records/{id}/log` | 单次执行输出 | `cron:job:read` | — | — | |
| `POST /cron/validate` | 校验 cron 表达式并预测执行时间 | `cron:job:read` | — | — | |
| `GET /backup/storages` | 存储后端列表 | `backup:storage:list` | — | — | |
| `POST /backup/storages` | 新增存储（本地、S3、OSS、COS、WebDAV、SFTP） | `backup:storage:create` | — | I | |
| `PUT /backup/storages/{id}` | 修改存储配置 | `backup:storage:update` | — | — | |
| `DELETE /backup/storages/{id}` | 删除存储 | `backup:storage:delete` | — | — | C |
| `POST /backup/storages/{id}/test` | 连通性与读写测试 | `backup:storage:exec` | — | — | |
| `GET /backup/policies` | 备份策略列表 | `backup:policy:list` | — | — | |
| `POST /backup/policies` | 创建策略（目标、周期、GFS 保留、加密） | `backup:policy:create` | — | I | |
| `PUT /backup/policies/{id}` | 修改策略 | `backup:policy:update` | — | — | |
| `DELETE /backup/policies/{id}` | 删除策略 | `backup:policy:delete` | — | — | C |
| `POST /backup/policies/{id}/run` | 立即执行备份 | `backup:task:exec` | M | I! | |
| `GET /backup/records` | 备份记录列表 | `backup:record:list` | M | — | |
| `GET /backup/records/{id}` | 记录详情与内容清单 | `backup:record:read` | — | — | |
| `POST /backup/records/{id}/verify` | 完整性校验 | `backup:record:exec` | — | I | |
| `DELETE /backup/records/{id}` | 删除备份 | `backup:record:delete` | — | — | C |
| `GET /backup/records/{id}/download` | 下载备份文件 | `backup:record:export` | — | — | |
| `POST /backup/restores` | 创建恢复任务（整机、站点、库、文件粒度） | `backup:restore:create` | Y | I! | C |
| `GET /backup/restores` | 恢复任务列表 | `backup:restore:list` | — | — | |
| `GET /backup/restores/{id}` | 恢复进度与逐项结果 | `backup:restore:read` | — | — | |
| `POST /backup/drills` | 发起恢复演练 | `backup:drill:create` | — | I | |
| `GET /backup/drills/{id}` | 演练报告与 RTO/RPO 实测 | `backup:drill:read` | — | — | |
| `GET /setting/settings` | 全部设置项（分组返回） | `setting:setting:read` | — | — | |
| `PUT /setting/settings` | 批量修改设置 | `setting:setting:update` | — | — | |
| `GET /setting/license` | 许可与版本信息 | `setting:setting:read` | — | — | |
| `GET /setting/upgrade/check` | 检查新版本 | `setting:upgrade:read` | — | — | |
| `POST /setting/upgrade` | 执行面板升级 | `setting:upgrade:exec` | — | I! | C |
| `GET /setting/upgrade/{taskId}` | 升级进度与日志 | `setting:upgrade:read` | — | — | |
| `POST /setting/upgrade/rollback` | 回滚到上一版本 | `setting:upgrade:exec` | — | I! | C |
| `GET /setting/panel-logs` | 面板运行日志 | `setting:setting:read` | — | — | |
| `POST /setting/panel/restart` | 重启面板服务 | `setting:setting:exec` | — | — | C |
| `GET /public/settings` | 登录页所需公开配置（标题、Logo、是否需验证码） | public | — | — | |

以上共 20 个分组、约 240 条接口。路由注册按分组拆分为 `internal/router/<module>.go`，每个文件导出 `Register<Module>(g *gin.RouterGroup)`，在 `router.go` 中统一挂载，保证清单与代码一一对应。CI 用 `scripts/check_routes.sh` 遍历 gin 路由树与本节表格比对，缺漏或多出即失败。

### 5.7.8 已知路径命名不一致登记

早期模块详设中出现过两种写法，以本节清单为准，另一种作为兼容别名保留至 v1.5 后移除：

| 规范路径（本节） | 兼容别名 | 处理 |
| --- | --- | --- |
| `/api/v1/cluster/nodes` | `/api/v1/cluster-nodes` | 别名 301 重定向，响应头带 `Deprecation` 与 `Sunset` |
| `/api/v1/website/sites` | `/api/v1/sites` | 同上 |

别名注册集中在 `internal/router/legacy.go`，每条别名附 `// deprecated: v1.5 移除` 注释，并在 5.13 的废弃流程中登记。

## 5.8 关键接口详细定义

本节挑选每个模块最复杂、契约最容易分歧的接口给出完整定义。未列出的接口遵循 5.2 的响应契约与 5.3 的分页约定，字段命名与本节同类接口保持一致。示例中的 `traceId` 与 `timestamp` 省略以缩短篇幅，实际响应必然包含。

### 5.8.1 登录与二次认证

**`POST /api/v1/auth/login`**

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `username` | string | 是 | 3-32 字符 |
| `password` | string | 是 | 明文，仅允许 HTTPS 传输 |
| `captchaId` | string | 否 | 触发验证码后必填 |
| `captchaCode` | string | 否 | 同上 |
| `remember` | bool | 否 | 为真时 refreshToken 有效期延长到 30 天 |

登录成功且未开启 2FA：

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "accessToken": "eyJhbGciOiJIUzI1NiIs...",
    "expiresIn": 900,
    "tokenType": "Bearer",
    "user": {
      "id": "1874923847293847",
      "username": "admin",
      "nickname": "系统管理员",
      "avatar": null,
      "isSuper": true,
      "language": "zh-CN",
      "theme": "auto",
      "mustChangePassword": false,
      "twoFABound": false
    },
    "roles": ["super_admin"],
    "permissions": ["*"]
  }
}
```

`refreshToken` 不出现在响应体，通过 `Set-Cookie: nova_rt=<token>; Path=/api/v1/auth; HttpOnly; Secure; SameSite=Strict; Max-Age=604800` 下发。

已开启 2FA 时返回 `110009`，`data` 携带一阶段票据：

```json
{
  "code": 110009,
  "message": "需要二次验证",
  "data": { "twoFAToken": "tfa_7c9e2b...", "expiresIn": 300, "methods": ["totp", "recovery"] }
}
```

**`POST /api/v1/auth/2fa/verify`**

```json
{ "twoFAToken": "tfa_7c9e2b...", "method": "totp", "code": "123456" }
```

校验通过后返回与登录成功完全相同的结构，便于前端复用同一处理分支。`method=recovery` 时 `code` 为恢复码，使用后立即失效并在 `data.recoveryRemaining` 返回剩余可用数量。

**`POST /api/v1/auth/refresh`**

请求体为空，凭 Cookie 中的 refreshToken 换新。服务端执行轮换：旧 refreshToken 立即失效并写入吊销名单，新 token 通过 Set-Cookie 下发；若检测到已失效 token 被再次使用，判定为窃取，吊销该用户全部会话并返回 `110021`。

```json
{ "code": 0, "data": { "accessToken": "eyJ...", "expiresIn": 900, "tokenType": "Bearer" } }
```

**`GET /api/v1/auth/profile`**

```json
{
  "code": 0,
  "data": {
    "user": { "id": "1874923847293847", "username": "ops01", "nickname": "运维一号", "email": "o***@example.com", "isSuper": false, "language": "zh-CN", "theme": "dark", "twoFABound": true, "lastLoginAt": "2026-08-17T09:12:31+08:00", "lastLoginIp": "10.0.3.21" },
    "roles": [{ "code": "site_admin", "name": "站点管理员", "dataScope": "group" }],
    "permissions": ["website:site:list", "website:site:read", "website:site:update", "file:file:list", "monitor:metric:read"],
    "nodeScope": ["1874900000000001", "1874900000000002"],
    "menus": ["dashboard", "website", "file", "monitor"]
  }
}
```

`permissions` 是前端 `usePermissionStore` 的唯一数据源，`nodeScope` 为空数组表示可见全部节点，`menus` 为后端预计算的顶层模块列表，前端仍按 `permissions` 做细粒度控制。

### 5.8.2 节点注册与批量安装

**`POST /api/v1/cluster/nodes`**

登记一个待接入节点，返回一次性 bootstrap token。

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `name` | string | 是 | 节点别名，租户内唯一 |
| `groupId` | string | 否 | 分组 ID |
| `tags` | string[] | 否 | 标签，最多 20 个 |
| `expectIp` | string | 否 | 预期来源 IP 或 CIDR，注册时校验 |
| `remark` | string | 否 | 备注 |
| `tokenTTL` | int | 否 | token 有效期秒，默认 3600，上限 86400 |

```json
{
  "code": 0,
  "data": {
    "node": { "id": "1874900000000007", "name": "hk-web-03", "status": "pending", "groupId": "1874800000000002", "tags": ["web", "hk"] },
    "bootstrap": {
      "token": "nbt_2f9a...",
      "expireAt": "2026-08-17T11:30:00+08:00",
      "serverAddr": "panel.example.com:34568",
      "caFingerprint": "SHA256:9f2c8b1e...",
      "installCommand": "curl -fsSL https://panel.example.com:34567/api/v1/public/agent/install | bash -s -- --token nbt_2f9a... --server panel.example.com:34568 --ca-fingerprint SHA256:9f2c8b1e..."
    }
  }
}
```

`installCommand` 由后端拼装并对参数做 shell 转义，前端只做展示与复制，不允许自行拼接。

**`POST /api/v1/cluster/nodes/install`**

通过 SSH 批量安装 Agent，强制 `Idempotency-Key`。

```json
{
  "targets": [
    { "host": "10.0.3.21", "port": 22, "user": "root", "credentialId": "1874700000000003", "name": "hk-web-04" },
    { "host": "10.0.3.22", "port": 22, "user": "deploy", "credentialId": "1874700000000003", "name": "hk-web-05", "sudo": true }
  ],
  "groupId": "1874800000000002",
  "tags": ["web", "hk"],
  "concurrency": 5,
  "installOptions": { "version": "1.0.0", "mirror": "cn", "skipPreflight": false }
}
```

```json
{
  "code": 0,
  "data": {
    "taskId": "1874950000000011",
    "total": 2,
    "wsTopic": "cluster.install.1874950000000011"
  }
}
```

进度通过 WebSocket `cluster.install.<taskId>` 推送，也可轮询 `GET /cluster/nodes/install/{taskId}`：

```json
{
  "code": 0,
  "data": {
    "taskId": "1874950000000011",
    "status": "running",
    "startedAt": "2026-08-17T10:31:02+08:00",
    "summary": { "total": 2, "pending": 0, "running": 1, "success": 1, "failed": 0 },
    "items": [
      { "host": "10.0.3.21", "status": "success", "nodeId": "1874900000000008", "stage": "registered", "durationMs": 18420 },
      { "host": "10.0.3.22", "status": "running", "stage": "installing", "progress": 60, "message": "正在下载 Agent 二进制" }
    ]
  }
}
```

`stage` 枚举固定为 `connecting → preflight → uploading → installing → starting → registered`，失败项额外带 `errorCode` 与 `stderr`（截断 2 KB），前端按 stage 渲染步骤条。

### 5.8.3 创建站点与配置下发

**`POST /api/v1/website/sites`**

请求头需带 `X-Node-Id`。请求体按 `type` 分支，公共字段在外层，类型专属字段收在 `typeConfig`。

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `name` | string | 是 | 站点标识，节点内唯一，仅小写字母数字与短横线 |
| `type` | string | 是 | `static` / `php` / `proxy` / `node` / `java` / `python` / `loadbalance` |
| `domains` | object[] | 是 | 至少一项，元素为 `{ domain, port, isDefault }` |
| `rootPath` | string | 条件 | 静态与脚本类必填，绝对路径 |
| `indexes` | string[] | 否 | 默认文档，默认 `["index.html","index.php"]` |
| `typeConfig` | object | 条件 | 见下 |
| `ssl` | object | 否 | `{ certId, forceHttps, hsts }` |
| `database` | object | 否 | 同时创建库：`{ create, type, name, user, password }` |
| `ftp` | object | 否 | 同时创建 FTP 账号 |
| `remark` | string | 否 | 备注 |

`typeConfig` 按类型的必要字段：

| type | 字段 |
| --- | --- |
| `php` | `runtimeId`（PHP 运行环境）、`phpVersion`、`rewritePreset` |
| `proxy` | `upstreams[]`（`{ url, weight }`）、`hostHeader`、`cacheEnable`、`cacheTTL`、`wsEnable` |
| `node` / `python` / `java` | `runtimeId`、`entry`、`listenPort`、`envs`（KV 数组）、`processManaged` |
| `loadbalance` | `upstreams[]`、`algorithm`（`round_robin`/`least_conn`/`ip_hash`）、`healthCheck` |

```json
{
  "name": "blog-prod",
  "type": "php",
  "domains": [
    { "domain": "blog.example.com", "port": 443, "isDefault": true },
    { "domain": "www.blog.example.com", "port": 443, "isDefault": false }
  ],
  "rootPath": "/opt/novapanel/data/vhost/blog-prod/public",
  "indexes": ["index.php", "index.html"],
  "typeConfig": { "runtimeId": "1874600000000005", "phpVersion": "8.3", "rewritePreset": "wordpress" },
  "ssl": { "certId": "1874500000000012", "forceHttps": true, "hsts": true },
  "database": { "create": true, "type": "mysql", "name": "blog_prod", "user": "blog_prod", "password": null },
  "remark": "生产博客"
}
```

`database.password` 传 `null` 表示由服务端生成强随机密码，仅在本次响应返回一次。

```json
{
  "code": 0,
  "data": {
    "site": {
      "id": "1874400000000031",
      "name": "blog-prod",
      "type": "php",
      "status": "running",
      "nodeId": "1874900000000002",
      "rootPath": "/opt/novapanel/data/vhost/blog-prod/public",
      "confPath": "/opt/novapanel/data/vhost/conf/blog-prod.conf",
      "domains": [{ "id": "1874400000000032", "domain": "blog.example.com", "port": 443, "isDefault": true }],
      "sslEnabled": true,
      "certId": "1874500000000012",
      "certExpireAt": "2026-11-15T00:00:00+08:00",
      "createdAt": "2026-08-17T10:41:07+08:00"
    },
    "database": { "id": "1874300000000018", "name": "blog_prod", "user": "blog_prod", "password": "Xk8#mQ2vLp9wZr4T", "host": "127.0.0.1", "port": 3306 },
    "warnings": ["域名 www.blog.example.com 未解析到本节点公网 IP"]
  }
}
```

创建是复合事务：写库、生成 vhost 配置、`nginx -t`、reload、可选建库建 FTP。任一步失败按逆序补偿（删配置文件、删库、回滚记录），失败响应带 `data.rollback: true` 与 `data.failedStage`。`warnings` 为非阻断提示，前端用 `Alert` 展示而非报错。

**`PUT /api/v1/website/sites/{id}/config`**

直接编辑 OpenResty 配置原文，属危险操作，需 `X-Confirm-Token`。

```json
{ "content": "server {\n    listen 443 ssl;\n    ...\n}\n", "etag": "W/\"a3f1c9e2\"" }
```

处理顺序：ETag 比对（不一致返 `400011`）→ 写入临时文件 → `nginx -t -c` 校验 → 通过则原子替换并 reload、备份旧版本到 `data/vhost/history/<site>/<ts>.conf` → 失败则丢弃临时文件并返回 `300005`：

```json
{
  "code": 300005,
  "message": "配置校验未通过，已回滚",
  "data": {
    "stderr": "nginx: [emerg] unknown directive \"prox_pass\" in /opt/novapanel/data/vhost/conf/blog-prod.conf:24\nnginx: configuration file test failed",
    "line": 24,
    "column": 5
  }
}
```

后端从 stderr 解析出行列号供前端 Monaco 编辑器定位（见 [前端架构与 Arco 规范](./06-前端架构与Arco规范.md) 的 CodeEditor 契约）。历史版本可通过 `GET /website/sites/{id}/config?version=<ts>` 读取并一键回滚。

**`POST /api/v1/cert/orders`**

申请 ACME 证书，强制幂等键。

```json
{
  "accountId": "1874500000000001",
  "domains": ["example.com", "*.example.com"],
  "challengeType": "dns-01",
  "dnsProviderId": "1874500000000004",
  "keyType": "ec256",
  "autoRenew": true,
  "renewBeforeDays": 30,
  "deployTargets": [{ "type": "site", "id": "1874400000000031" }]
}
```

```json
{
  "code": 0,
  "data": {
    "orderId": "1874500000000021",
    "status": "processing",
    "domains": ["example.com", "*.example.com"],
    "challengeType": "dns-01",
    "wsTopic": "cert.order.1874500000000021",
    "estimatedSeconds": 90
  }
}
```

泛域名强制走 `dns-01`，传 `http-01` 返回 `100001`。签发进度通过 topic 推送 `stage` ∈ `authorizing / challenge_pending / challenge_verifying / finalizing / issued / failed`，前端按阶段展示，失败时 `data.reason` 给出 CA 原始应答摘要。

### 5.8.4 文件列表、保存与分片上传

**`GET /api/v1/file/list`**

| 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `path` | string | 是 | 绝对路径，服务端规范化后校验是否越界 |
| `showHidden` | bool | 否 | 默认 false |
| `sort` | string | 否 | `name`/`size`/`mtime`/`type`，默认 `type` |
| `order` | string | 否 | `asc`/`desc` |
| `page` / `pageSize` | int | 否 | 大目录分页，`pageSize` 默认 200、上限 2000 |
| `filter` | string | 否 | 名称通配 |

```json
{
  "code": 0,
  "data": {
    "path": "/opt/novapanel/data/vhost/blog-prod",
    "parent": "/opt/novapanel/data/vhost",
    "writable": true,
    "total": 4,
    "page": 1,
    "pageSize": 200,
    "list": [
      { "name": "public", "type": "dir", "size": 4096, "mode": "0755", "modeText": "drwxr-xr-x", "owner": "www", "group": "www", "mtime": "2026-08-17T10:41:07+08:00", "isSymlink": false, "childCount": 27 },
      { "name": "wp-config.php", "type": "file", "size": 3210, "mode": "0640", "modeText": "-rw-r-----", "owner": "www", "group": "www", "mtime": "2026-08-17T10:42:15+08:00", "isSymlink": false, "mime": "text/x-php", "editable": true },
      { "name": "latest.tar.gz", "type": "file", "size": 24815723, "mode": "0644", "modeText": "-rw-r--r--", "owner": "root", "group": "root", "mtime": "2026-08-16T22:10:03+08:00", "isSymlink": false, "mime": "application/gzip", "editable": false, "archive": true },
      { "name": "current", "type": "link", "size": 18, "mode": "0777", "modeText": "lrwxrwxrwx", "owner": "root", "group": "root", "mtime": "2026-08-10T08:00:00+08:00", "isSymlink": true, "linkTarget": "/opt/releases/v12", "linkBroken": false }
    ]
  }
}
```

`editable` 由 MIME 与大小共同决定（文本类且 < `file.max_edit_size`），`archive` 标记可解压，前端据此决定右键菜单项启用状态。目录不返回递归大小，避免大目录阻塞，需要时调 `GET /file/du`。

**`PUT /api/v1/file/content`**

```
PUT /api/v1/file/content
X-Node-Id: 1874900000000002
If-Match: W/"a3f1c9e2"
Content-Type: application/json

{ "path": "/opt/novapanel/data/vhost/blog-prod/wp-config.php", "content": "<?php\n...\n", "encoding": "utf-8", "createBackup": true }
```

`If-Match` 缺失时服务端拒绝并返回 `100001`，强制前端读改写闭环。冲突返回 `400011` 并带双方摘要：

```json
{
  "code": 400011,
  "message": "文件已被其他人修改",
  "data": { "currentEtag": "W/\"b7d2e441\"", "currentMtime": "2026-08-17T10:50:11+08:00", "currentSize": 3260, "modifiedBy": "ops02" }
}
```

`modifiedBy` 只在能从审计日志关联到面板内操作时给出，宿主外部改动为 `null`。`createBackup: true` 时保存前将原文件复制到同目录 `.<name>.bak.<ts>`，保留最近 5 份。

**分片上传三步**

```
POST /api/v1/file/upload/init
{ "path": "/opt/backup", "filename": "app-v3.tar.gz", "size": 1073741824, "chunkSize": 8388608, "hash": "sha256:9f2c...", "conflict": "rename" }
→ { "code": 0, "data": { "uploadId": "up_7f3a9c2b1d4e5a60788b9c0d", "chunkSize": 8388608, "totalChunks": 128, "uploadedChunks": [], "expireAt": "2026-08-18T10:55:00+08:00", "quickUpload": false } }
```

`hash` 为整文件 sha256（形如 `sha256:<hex>`），服务端在目标路径已存在同大小同哈希文件时返回 `quickUpload: true` 与 `entry`，直接完成秒传。`conflict` ∈ `reject`（默认，冲突返 `400003`）/`overwrite`/`rename`（自动加 `(1)` 后缀），与其余文件接口的冲突参数同名。`chunkSize` 未传取 8 MB，服务端会夹到 64 KB~64 MB，分片数上限 20000。`path`、`filename`、`size`、`chunkSize`、`conflict` 完全一致且未过期的会话会被复用：重复 `init` 返回同一个 `uploadId` 与已收分片，刷新页面后可继续断点续传。

```
POST /api/v1/file/upload/chunk
Content-Type: multipart/form-data
uploadId=up_7f3a9c2b1d4e5a60788b9c0d & index=17 & checksum=md5:3f9a... & chunk=<binary>
→ { "code": 0, "data": { "uploadId": "up_7f3a9c2b1d4e5a60788b9c0d", "index": 17, "received": 8388608, "uploadedCount": 18, "totalChunks": 128 } }
```

分片可并发上传，默认建议并发 3；`checksum` 支持 `md5:<hex>` 与 `sha256:<hex>`，缺省则只校长度。校验和或长度不符返回 `400006`，前端只重传该分片。重传同一 `index` 幂等覆盖。

```
POST /api/v1/file/upload/complete
{ "uploadId": "up_7f3a9c2b1d4e5a60788b9c0d", "hash": "sha256:9f2c..." }
→ { "code": 0, "data": { "entry": { "name": "app-v3.tar.gz", "path": "/opt/backup/app-v3.tar.gz", "size": 1073741824 }, "path": "/opt/backup/app-v3.tar.gz", "size": 1073741824, "hash": "sha256:9f2c...", "durationMs": 96233 } }
```

合并为同步操作，直接返回落地条目（`data.taskId` 异步合并与 `file.transfer.<taskId>` 订阅尚未实现）。缺片时返回 `400006` 并在 `data.missing` 列出索引；合并失败（缺片、整文件哈希不符）不会破坏会话，修正后可重试而无需重传全部分片。断点续传前调 `GET /file/upload/status?uploadId=up_7f3a9c2b1d4e5a60788b9c0d` 拿 `uploadedChunks` 与 `missingChunks`；`DELETE /file/upload/{uploadId}` 放弃会话并清理分片，会话已不存在时同样返回成功。会话有效期 24 小时，过期访问返回 `400007`，需重新 `init`。分片落在服务端自管的临时目录（`file.upload_temp_dir`），不写进用户目录，上传中断不会在站点目录留下垃圾。

### 5.8.5 容器创建与 Compose 部署

**`POST /api/v1/container/containers`**

```json
{
  "name": "redis-cache",
  "image": "redis:7.4-alpine",
  "pullPolicy": "if-not-present",
  "command": ["redis-server", "--appendonly", "yes"],
  "env": [{ "key": "TZ", "value": "Asia/Shanghai" }],
  "ports": [{ "hostIp": "127.0.0.1", "hostPort": 6379, "containerPort": 6379, "protocol": "tcp" }],
  "volumes": [
    { "type": "volume", "source": "redis-data", "target": "/data", "readOnly": false },
    { "type": "bind", "source": "/opt/novapanel/data/redis/redis.conf", "target": "/etc/redis.conf", "readOnly": true }
  ],
  "networks": [{ "name": "nova-br0", "aliases": ["cache"] }],
  "restartPolicy": { "name": "unless-stopped", "maximumRetryCount": 0 },
  "resources": { "cpuLimit": 1.5, "memoryLimit": "512m", "memoryReservation": "256m", "pidsLimit": 200 },
  "healthcheck": { "test": ["CMD", "redis-cli", "ping"], "interval": "10s", "timeout": "3s", "retries": 3, "startPeriod": "5s" },
  "labels": { "nova.managed": "true", "nova.app": "cache" },
  "privileged": false,
  "capAdd": [],
  "autoStart": true
}
```

```json
{
  "code": 0,
  "data": {
    "container": {
      "id": "9f2c8b1e4a7d",
      "name": "redis-cache",
      "image": "redis:7.4-alpine",
      "imageId": "sha256:c3f1...",
      "state": "running",
      "status": "Up 2 seconds (health: starting)",
      "health": "starting",
      "createdAt": "2026-08-17T11:02:44+08:00",
      "ports": [{ "hostIp": "127.0.0.1", "hostPort": 6379, "containerPort": 6379, "protocol": "tcp" }],
      "nodeId": "1874900000000002"
    },
    "pullTaskId": null
  }
}
```

`pullPolicy=always` 或镜像不存在时先拉取，响应 `pullTaskId` 非空，前端订阅 `container.image.pull.<taskId>` 展示进度后再刷新列表。`privileged: true` 在 `container.allow_privileged=false` 时直接返回 `500007`，不进入 Docker API。挂载 `bind` 类型的 `source` 需通过路径白名单校验，命中黑名单（`/`、`/etc`、`/var/run/docker.sock` 等）返回 `500006`。

**`POST /api/v1/container/compose/stacks`**

```json
{
  "name": "monitoring",
  "path": "/opt/novapanel/data/compose/monitoring",
  "source": "inline",
  "content": "services:\n  grafana:\n    image: grafana/grafana:11.2.0\n    ports:\n      - \"3000:3000\"\n    volumes:\n      - grafana-data:/var/lib/grafana\nvolumes:\n  grafana-data:\n",
  "env": [{ "key": "GF_SECURITY_ADMIN_PASSWORD", "value": "***", "secret": true }],
  "autoUp": true,
  "pullBeforeUp": true
}
```

`source` ∈ `inline`（正文在 `content`）/`file`（`path` 下已有 compose 文件）/`git`（额外传 `git: { url, ref, subdir, credentialId }`）。创建时先用 compose-go 解析，语法错误返回 `510001` 并带行号：

```json
{ "code": 510001, "message": "Compose 文件不合法", "data": { "line": 6, "column": 9, "detail": "services.grafana.ports must be a list of strings or mappings" } }
```

校验通过后落库并写入 `compose.yaml` 与 `.env`（`secret: true` 的值加密存储，落盘时才解密，文件权限 `0600`）。`autoUp: true` 时返回 `taskId`：

```json
{
  "code": 0,
  "data": {
    "stack": { "id": "1874200000000009", "name": "monitoring", "path": "/opt/novapanel/data/compose/monitoring", "status": "deploying", "serviceCount": 1, "nodeId": "1874900000000002" },
    "taskId": "1874950000000023",
    "wsTopic": "container.compose.logs.1874200000000009"
  }
}
```

**`GET /api/v1/container/containers/{id}/logs`**

| 参数 | 类型 | 说明 |
| --- | --- | --- |
| `tail` | int | 末尾行数，默认 200，上限 5000 |
| `since` / `until` | string | RFC3339 时间窗 |
| `stdout` / `stderr` | bool | 默认均为 true |
| `timestamps` | bool | 是否带时间戳前缀，默认 true |
| `keyword` | string | 服务端过滤关键词 |

```json
{
  "code": 0,
  "data": {
    "lines": [
      { "ts": "2026-08-17T11:02:45.113+08:00", "stream": "stdout", "text": "1:C 17 Aug 2026 11:02:45.113 * oO0OoO0OoO0Oo Redis is starting" },
      { "ts": "2026-08-17T11:02:45.114+08:00", "stream": "stdout", "text": "1:M 17 Aug 2026 11:02:45.114 * Ready to accept connections tcp" }
    ],
    "truncated": false,
    "nextSince": "2026-08-17T11:02:45.114+08:00"
  }
}
```

实时跟随不走本接口，改用 WebSocket `container.logs.<containerId>`，`nextSince` 用于历史翻页与实时接续的衔接点。

### 5.8.6 指标查询与告警规则

**`POST /api/v1/monitor/metric/query`**

用 POST 而非 GET，因为多序列查询的参数结构远超 URL 长度与可读性上限。

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `queries` | object[] | 是 | 至多 20 条子查询 |
| `queries[].refId` | string | 是 | 前端关联用标识，如 `A`、`B` |
| `queries[].metric` | string | 是 | 指标名，须在字典内 |
| `queries[].labels` | object | 否 | 精确匹配的标签，如 `{ "device": "eth0" }` |
| `queries[].nodeIds` | string[] | 否 | 空则取 `X-Node-Id` 或全部可见节点 |
| `queries[].agg` | string | 否 | `avg`/`max`/`min`/`sum`/`last`/`rate`/`p95`/`p99` |
| `queries[].groupBy` | string[] | 否 | 按标签分组，如 `["nodeId"]` |
| `from` / `to` | string | 是 | RFC3339 或相对表达式 `now-1h` |
| `step` | string | 否 | `15s`/`1m`/`5m`/`1h`，省略时按跨度自动选择 |
| `maxPoints` | int | 否 | 默认 720，上限 11000 |
| `fill` | string | 否 | `null`（默认）/`zero`/`previous` |

```json
{
  "queries": [
    { "refId": "A", "metric": "node_cpu_usage", "agg": "avg", "groupBy": ["nodeId"], "nodeIds": ["1874900000000002", "1874900000000003"] },
    { "refId": "B", "metric": "node_net_recv_bytes", "labels": { "device": "eth0" }, "agg": "rate" }
  ],
  "from": "now-6h",
  "to": "now",
  "step": "1m",
  "fill": "null"
}
```

```json
{
  "code": 0,
  "data": {
    "step": "1m",
    "from": "2026-08-17T05:05:00+08:00",
    "to": "2026-08-17T11:05:00+08:00",
    "resolution": "1m",
    "downsampled": true,
    "series": [
      {
        "refId": "A",
        "metric": "node_cpu_usage",
        "labels": { "nodeId": "1874900000000002", "nodeName": "hk-web-02" },
        "unit": "percent",
        "points": [[1755400500, 23.4], [1755400560, 25.1], [1755400620, null]]
      },
      {
        "refId": "B",
        "metric": "node_net_recv_bytes",
        "labels": { "nodeId": "1874900000000002", "device": "eth0" },
        "unit": "bytesPerSecond",
        "points": [[1755400500, 1048576.0], [1755400560, 983040.0]]
      }
    ],
    "stats": { "seriesCount": 3, "pointCount": 1080, "scannedRows": 21600, "elapsedMs": 47 }
  }
}
```

`points` 用 `[unixSeconds, value]` 二元数组而非对象，一次查询能省下约 60% 传输体积；`null` 表示该点无数据，ECharts 直接识别为断线。`downsampled` 为真表示命中降采样表而非原始表，`resolution` 告知真实粒度，前端在图例角标提示以免误读精度。`unit` 由指标字典给出，前端 `ChartWrapper` 据此选格式化器。

**`POST /api/v1/alert/rule`**

```json
{
  "name": "节点 CPU 持续高负载",
  "description": "5 分钟平均 CPU 超过 85% 触发",
  "severity": "critical",
  "enabled": true,
  "target": { "type": "node", "selector": { "groupIds": ["1874800000000002"], "tags": ["prod"], "excludeNodeIds": [] } },
  "expr": "avg_over_time(node_cpu_usage[5m]) > 85",
  "for": "5m",
  "evalInterval": "30s",
  "noDataPolicy": "keep",
  "labels": { "team": "sre" },
  "annotations": { "summary": "{{ $labels.nodeName }} CPU 使用率 {{ $value | printf \"%.1f\" }}%", "runbook": "https://wiki.example.com/runbook/cpu" },
  "notify": {
    "channelIds": ["1874100000000003", "1874100000000005"],
    "templateId": "1874100000000011",
    "repeatInterval": "1h",
    "escalations": [{ "afterMinutes": 30, "channelIds": ["1874100000000007"] }]
  },
  "silenceWindows": [{ "cron": "0 2 * * *", "durationMinutes": 60, "reason": "夜间备份窗口" }],
  "heal": { "enabled": false, "action": null, "needApprove": true, "cooldownMinutes": 30 }
}
```

`expr` 使用内置 DSL（语法定义见 [监控告警与可观测](./11-监控告警与可观测.md)），提交时经解析器校验，语法错误返回 `610001` 并带位置：

```json
{ "code": 610001, "message": "告警表达式不合法", "data": { "pos": 22, "token": "[5m", "detail": "区间选择器缺少右方括号" } }
```

```json
{
  "code": 0,
  "data": {
    "rule": { "id": "1874100000000031", "name": "节点 CPU 持续高负载", "severity": "critical", "enabled": true, "matchedNodeCount": 12, "nextEvalAt": "2026-08-17T11:05:30+08:00", "createdAt": "2026-08-17T11:05:12+08:00" },
    "dryRun": { "currentlyFiring": 1, "samples": [{ "nodeId": "1874900000000009", "nodeName": "hk-job-01", "value": 91.3 }] }
  }
}
```

创建时自动跑一次试算，`dryRun` 告知按当前数据会立刻触发多少实例，避免上线即告警风暴。`noDataPolicy` ∈ `keep`（维持上次状态）/`alerting`（判为异常）/`ok`（判为正常）/`nodata`（独立的无数据事件）。

**`GET /api/v1/alert/event`**

除标准分页外支持 `severity`、`status`（`firing`/`acked`/`resolved`）、`ruleId`、`nodeIds`、`from`、`to`、`labelSelector`。

```json
{
  "code": 0,
  "data": {
    "total": 137,
    "page": 1,
    "pageSize": 20,
    "list": [
      {
        "id": "1874100000000501",
        "ruleId": "1874100000000031",
        "ruleName": "节点 CPU 持续高负载",
        "severity": "critical",
        "status": "firing",
        "nodeId": "1874900000000009",
        "nodeName": "hk-job-01",
        "value": 93.7,
        "threshold": 85,
        "summary": "hk-job-01 CPU 使用率 93.7%",
        "startsAt": "2026-08-17T10:47:00+08:00",
        "lastEvalAt": "2026-08-17T11:04:30+08:00",
        "endsAt": null,
        "durationSeconds": 1050,
        "notifyCount": 2,
        "ackedBy": null,
        "healStatus": "none",
        "labels": { "team": "sre", "env": "prod" }
      }
    ],
    "aggregation": { "firing": 3, "acked": 1, "resolved": 133, "bySeverity": { "critical": 2, "warning": 2 } }
  }
}
```

`aggregation` 随列表一并返回，前端顶部统计卡无需二次请求。`healStatus` ∈ `none`/`pending_approve`/`running`/`succeeded`/`failed`。

### 5.8.7 备份、恢复与应用安装

**`POST /api/v1/backup/policies`**

```json
{
  "name": "生产站点每日备份",
  "enabled": true,
  "targets": [
    { "type": "site", "ids": ["1874400000000031"], "includeDatabase": true, "excludePatterns": ["*.log", "cache/**"] },
    { "type": "database", "ids": ["1874300000000018"] },
    { "type": "panel", "ids": [] }
  ],
  "nodeSelector": { "nodeIds": ["1874900000000002"] },
  "schedule": { "cron": "30 3 * * *", "timezone": "Asia/Shanghai", "jitterSeconds": 300 },
  "storageIds": ["1874000000000002", "1874000000000005"],
  "compression": { "algorithm": "zstd", "level": 6 },
  "encryption": { "enabled": true, "keyRef": "master" },
  "retention": { "mode": "gfs", "daily": 7, "weekly": 4, "monthly": 6, "yearly": 1, "minKeep": 3 },
  "options": { "preScript": null, "postScript": null, "bandwidthLimitMBps": 50, "verifyAfterBackup": true, "concurrency": 2 },
  "notify": { "onSuccess": false, "onFailure": true, "channelIds": ["1874100000000003"] }
}
```

`storageIds` 多个表示同一份备份分发到多处，任一成功即算成功、部分失败在记录里标 `partial`。`retention.mode` ∈ `count`（保留最近 N 份）/`days`（保留 N 天）/`gfs`（祖父-父-子）；`minKeep` 是硬底线，任何策略都不会让可用备份少于该数量。`encryption.keyRef=master` 表示用面板主密钥派生，`custom` 时需另传 `passphrase` 且服务端只存派生校验值不存明文。

**`POST /api/v1/backup/restores`**

强制幂等键与二次确认。

```json
{
  "recordId": "1874000000000311",
  "targetNodeId": "1874900000000004",
  "mode": "selective",
  "items": [
    { "type": "site", "sourceId": "1874400000000031", "newName": "blog-staging", "overwrite": false },
    { "type": "database", "sourceId": "1874300000000018", "newName": "blog_staging", "overwrite": false }
  ],
  "passphrase": null,
  "options": { "stopServiceBeforeRestore": true, "keepOriginalPath": false, "targetPath": "/opt/novapanel/data/vhost/blog-staging", "dryRun": false }
}
```

```json
{
  "code": 0,
  "data": {
    "restoreId": "1874000000000412",
    "status": "running",
    "mode": "selective",
    "itemCount": 2,
    "wsTopic": "backup.restore.1874000000000412",
    "preflight": {
      "passed": true,
      "checks": [
        { "name": "备份完整性", "result": "pass", "detail": "SHA256 匹配" },
        { "name": "目标空间", "result": "pass", "detail": "需要 2.4 GB，可用 87 GB" },
        { "name": "版本兼容", "result": "pass", "detail": "备份自 v1.0.0，当前 v1.0.3" },
        { "name": "命名冲突", "result": "warn", "detail": "blog_staging 不存在，将新建" }
      ]
    }
  }
}
```

`mode` ∈ `full`（整机恢复，仅允许目标为空节点）/`selective`（按项恢复）/`file`（仅取备份内指定路径）。`dryRun: true` 时只跑 preflight 与内容清单比对，不落地任何改动，用于演练。预检不通过直接返回对应错误码（`900007` 版本、`900005` 空间、`900008` 冲突），`data.preflight.checks` 始终返回全部检查项供前端展示清单。

**`POST /api/v1/appstore/instances`**

```json
{
  "appKey": "wordpress",
  "version": "6.6.1",
  "instanceName": "blog",
  "nodeId": "1874900000000002",
  "params": {
    "site_domain": "blog.example.com",
    "db_type": "mysql",
    "db_name": "wp_blog",
    "db_user": "wp_blog",
    "db_password": null,
    "admin_user": "admin",
    "admin_password": "***",
    "admin_email": "ops@example.com",
    "php_version": "8.3",
    "port": 0
  },
  "resources": { "cpuLimit": 2, "memoryLimit": "1g" },
  "linkSite": { "create": true, "siteName": "blog-prod", "certId": "1874500000000012" },
  "backupPolicyId": "1874000000000021"
}
```

`params` 的键与 `GET /appstore/apps/{key}/params` 返回的表单定义严格对应，服务端按定义的 `type`、`required`、`pattern`、`enum`、`min`/`max` 逐项校验，不合法返回 `100001` 并在 `data.fields` 逐字段说明。`port: 0` 表示由服务端在配置区间内自动分配空闲端口。

```json
{
  "code": 0,
  "data": {
    "instance": {
      "id": "1873900000000027",
      "appKey": "wordpress",
      "appName": "WordPress",
      "version": "6.6.1",
      "instanceName": "blog",
      "status": "installing",
      "nodeId": "1874900000000002",
      "workDir": "/opt/novapanel/data/appstore/wordpress-blog",
      "accessUrls": [],
      "createdAt": "2026-08-17T11:12:33+08:00"
    },
    "taskId": "1874950000000041",
    "wsTopic": "appstore.install.1873900000000027",
    "steps": ["校验依赖", "分配端口", "创建数据库", "渲染 compose", "拉取镜像", "启动服务", "健康检查", "创建站点", "写入备份策略"],
    "generated": { "db_password": "只在安装完成事件中返回一次" }
  }
}
```

安装是长流程，`steps` 提前告知全部阶段名，WebSocket 逐步推送 `{ step, index, status, message }`，完成事件里带 `accessUrls` 与所有服务端生成的凭据（仅此一次）。安装失败自动清理已创建的容器、卷、数据库与站点，并在 `data.rollback` 里列出已回收的资源。

### 5.8.8 批量任务、审计检索与面板升级

**`POST /api/v1/cluster/tasks`**

批量任务是集群能力的核心入口，强制幂等键与二次确认。

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `type` | string | 是 | `command`/`script`/`file_push`/`file_pull`/`package`/`service`/`agent_upgrade`/`baseline_fix`/`backup` |
| `name` | string | 是 | 任务名，用于列表展示 |
| `selector` | object | 是 | 目标选择器，见下 |
| `payload` | object | 是 | 按 `type` 分支的载荷 |
| `strategy` | object | 否 | 执行策略 |
| `timeoutSeconds` | int | 否 | 单节点超时，默认 300，上限 3600 |

`selector` 支持三种维度取并集后再排除，最终目标为空返回 `200010`：

```json
{
  "selector": {
    "nodeIds": ["1874900000000002"],
    "groupIds": ["1874800000000002"],
    "tagExpr": "prod && web && !canary",
    "excludeNodeIds": ["1874900000000009"],
    "includeOffline": false
  }
}
```

`tagExpr` 支持 `&&`、`||`、`!` 与括号，解析为标签集合运算。`includeOffline: false` 时离线节点直接标记 `skipped` 而非 `failed`，避免离线机器污染失败率统计。

`strategy` 决定灰度节奏：

```json
{
  "strategy": {
    "mode": "batched",
    "batchSize": 5,
    "batchIntervalSeconds": 30,
    "failurePolicy": "pause",
    "maxFailureRatio": 0.2,
    "canaryNodeIds": ["1874900000000003"],
    "canaryWaitSeconds": 120
  }
}
```

| 字段 | 取值与含义 |
| --- | --- |
| `mode` | `parallel` 全量并发（受 `cluster.max_concurrency` 限制）、`batched` 分批、`serial` 串行 |
| `failurePolicy` | `continue` 继续、`pause` 暂停待人工决策、`abort` 立即终止剩余 |
| `maxFailureRatio` | 失败比例超过即按 `failurePolicy` 处理，`pause` 时任务转 `paused` 等待 `resume` |
| `canaryNodeIds` | 金丝雀节点先跑，等待期内无失败才继续 |

`command` 类型的 payload：

```json
{ "payload": { "command": "systemctl restart nginx", "workDir": "/root", "user": "root", "env": {}, "shell": "/bin/bash", "captureOutput": true, "maxOutputBytes": 65536 } }
```

命令在 Agent 侧仍走 `executor` 白名单与高危规则校验，命中拦截规则的节点返回 `410002` 并计入 `blocked` 而非 `failed`。

```json
{
  "code": 0,
  "data": {
    "taskId": "1874950000000057",
    "type": "command",
    "status": "running",
    "resolvedNodes": 12,
    "skippedNodes": 2,
    "summary": { "total": 12, "pending": 7, "running": 5, "success": 0, "failed": 0, "blocked": 0, "skipped": 2 },
    "wsTopic": "cluster.task.1874950000000057",
    "createdAt": "2026-08-17T11:20:04+08:00"
  }
}
```

`GET /cluster/tasks/{id}` 返回逐节点明细，单节点输出超过 `maxOutputBytes` 时截断并置 `outputTruncated: true`，完整输出通过 `GET /cluster/tasks/{id}/nodes/{nodeId}/log` 流式取回：

```json
{
  "code": 0,
  "data": {
    "task": { "taskId": "1874950000000057", "status": "paused", "pausedReason": "失败比例 0.25 超过阈值 0.2", "startedAt": "2026-08-17T11:20:05+08:00", "finishedAt": null },
    "summary": { "total": 12, "pending": 4, "running": 0, "success": 5, "failed": 3, "blocked": 0, "skipped": 2 },
    "items": [
      { "nodeId": "1874900000000002", "nodeName": "hk-web-02", "status": "success", "exitCode": 0, "durationMs": 1240, "stdout": "", "stderr": "", "outputTruncated": false, "startedAt": "2026-08-17T11:20:05+08:00" },
      { "nodeId": "1874900000000006", "nodeName": "hk-web-06", "status": "failed", "exitCode": 1, "errorCode": 100016, "durationMs": 2130, "stderr": "Job for nginx.service failed because the control process exited with error code.", "outputTruncated": false },
      { "nodeId": "1874900000000011", "nodeName": "sg-db-01", "status": "skipped", "errorCode": 200002, "message": "节点离线" }
    ]
  }
}
```

暂停后可调 `POST /cluster/tasks/{id}/resume`（继续剩余）、`POST /cluster/tasks/{id}/retry`（仅重试失败项，生成子任务并沿用原 selector 快照）或 `POST /cluster/tasks/{id}/cancel`（取消未开始项，运行中项不中断）。selector 在创建时解析并快照，之后节点分组变化不影响已创建任务的目标集合。

**`GET /api/v1/audit/operations`**

| 参数 | 说明 |
| --- | --- |
| `from` / `to` | 时间窗，默认最近 7 天，跨度上限 90 天 |
| `userId` / `username` | 操作人 |
| `module` | 模块，如 `website` |
| `permission` | 权限点精确匹配 |
| `resourceType` / `resourceId` | 资源定位 |
| `result` | `success`/`failure` |
| `nodeIds` | 目标节点 |
| `ip` | 来源 IP，支持 CIDR |
| `riskLevel` | `low`/`medium`/`high` |
| `keyword` | 在摘要与资源名上做模糊匹配 |

```json
{
  "code": 0,
  "data": {
    "total": 2841,
    "page": 1,
    "pageSize": 20,
    "list": [
      {
        "id": "1873800000001204",
        "seq": 1204,
        "at": "2026-08-17T11:23:41.882+08:00",
        "userId": "1874923847293848",
        "username": "ops02",
        "ip": "203.0.113.44",
        "location": "香港",
        "userAgent": "Mozilla/5.0 ...",
        "module": "website",
        "permission": "website:site:delete",
        "action": "DELETE /api/v1/website/sites/1874400000000029",
        "resourceType": "site",
        "resourceId": "1874400000000029",
        "resourceName": "old-promo",
        "nodeId": "1874900000000002",
        "nodeName": "hk-web-02",
        "riskLevel": "high",
        "result": "success",
        "durationMs": 842,
        "confirmed": true,
        "traceId": "01J5X8ZQ3K7N2M9P4R6T8V0W1Y",
        "hasDiff": true,
        "hashPrev": "9f2c8b1e...",
        "hash": "3a7d5f01..."
      }
    ],
    "aggregation": { "byResult": { "success": 2790, "failure": 51 }, "byRiskLevel": { "high": 63, "medium": 411, "low": 2367 } }
  }
}
```

`GET /audit/operations/{id}` 额外返回 `request`（脱敏后的入参）、`diff`（变更前后字段对比）、`chain`（前后各 1 条记录的哈希，用于本地校验链）。审计记录只增不改，任何 PUT/DELETE 返回 `710003`。

**`POST /api/v1/setting/upgrade`**

```json
{ "version": "1.0.4", "source": "official", "skipBackup": false, "upgradeAgents": true, "agentStrategy": { "mode": "batched", "batchSize": 10 } }
```

```json
{
  "code": 0,
  "data": {
    "taskId": "1873700000000005",
    "fromVersion": "1.0.3",
    "toVersion": "1.0.4",
    "status": "preflight",
    "wsTopic": "system.upgrade.1873700000000005",
    "preflight": {
      "passed": true,
      "checks": [
        { "name": "磁盘空间", "result": "pass", "detail": "需要 380 MB，可用 87 GB" },
        { "name": "配置备份", "result": "pass", "detail": "已备份至 data/backup/upgrade/1.0.3-20260817112500.tar.zst" },
        { "name": "数据库迁移", "result": "pass", "detail": "待执行 3 个迁移，均可逆" },
        { "name": "Agent 兼容", "result": "warn", "detail": "2 个节点 Agent 版本低于 1.0.0，将在面板升级后自动升级" },
        { "name": "运行中任务", "result": "pass", "detail": "无阻塞任务" }
      ]
    },
    "estimatedDowntimeSeconds": 25,
    "rollbackAvailable": true
  }
}
```

预检失败返回 `910003` 并在 `data.checks` 标出 `fail` 项。升级过程中面板会短暂重启，前端在收到 `stage=restarting` 后切换到轮询 `GET /system/health` 的重连模式，健康后拉 `GET /setting/upgrade/{taskId}` 补齐结束状态。整个流程的详细阶段与回滚边界见 [部署安装与升级](./14-部署安装与升级.md)。

## 5.9 实时通道协议

### 5.9.1 连接建立与信封格式

WebSocket 不接受 `Authorization` 头（浏览器 API 不支持自定义头），也不允许把 accessToken 放到 query 里（会进入访问日志）。统一走一次性 ticket：

```
POST /api/v1/ws/ticket
Authorization: Bearer <accessToken>
{ "channel": "hub", "nodeId": null }
→ { "code": 0, "data": { "ticket": "wst_3f9a2c7e...", "expiresIn": 30, "wsUrl": "wss://panel.example.com:34567/api/v1/ws/hub?ticket=wst_3f9a2c7e..." } }
```

| 规则 | 说明 |
| --- | --- |
| 有效期 | 30 秒，一次性消费，握手成功即从 KV 删除 |
| 绑定 | 绑定用户、channel、可选 nodeId 与客户端 IP，任一不符拒绝握手（HTTP 401，不进入 WS 协议） |
| 权限 | 签发时校验 channel 对应权限点，握手时不再查库，连接期内权限变更由服务端主动下发 `error` 并关闭 |
| 复用 | 同一用户可持有多条连接，`hub` 频道建议只保持 1 条，业务频道按需建 |
| 子协议 | 不使用 WebSocket 子协议协商，一律 JSON 文本帧；二进制帧仅用于终端与文件传输通道的原始数据 |

信封与 [前端架构与 Arco 规范](./06-前端架构与Arco规范.md) 定义完全一致，服务端与前端共用同一份 TypeScript 类型（由 `make proto` 生成）：

```ts
interface WsEnvelope<T = unknown> {
  v: 1                       // 协议版本，不兼容变更时递增
  type: 'sub' | 'unsub' | 'ping' | 'pong' | 'event' | 'ack' | 'error'
  id?: string                // 客户端生成的请求标识，ack/error 原样回带
  topic?: string             // <module>.<subject>[.<resourceId>]
  nodeId?: string            // 目标节点，控制面本地事件为空
  payload?: T
  ts: number                 // 毫秒时间戳
}
```

| type | 方向 | 语义 |
| --- | --- | --- |
| `sub` | C→S | 订阅一个或多个 topic，`payload: { topics: string[], params?: object }` |
| `unsub` | C→S | 取消订阅 |
| `ping` | 双向 | 心跳，客户端 25 秒一次；服务端 30 秒未收到即关闭 |
| `pong` | 双向 | 心跳应答，`payload: { serverTime }` 供前端校时 |
| `event` | S→C | 业务事件推送 |
| `ack` | S→C | 对 `sub`/`unsub` 的确认，`payload: { topics: string[], rejected?: {topic,code}[] }` |
| `error` | S→C | 错误，`payload: { code, message, topic? }`，致命错误后服务端主动关闭 |

订阅示例与应答：

```json
{ "v": 1, "type": "sub", "id": "req-7", "payload": { "topics": ["cluster.node.status", "alert.event.firing", "monitor.live"], "params": { "monitor.live": { "metrics": ["node_cpu_usage", "node_mem_usage"], "nodeIds": ["1874900000000002"], "intervalSeconds": 5 } } }, "ts": 1755403441882 }
```

```json
{ "v": 1, "type": "ack", "id": "req-7", "payload": { "topics": ["cluster.node.status", "monitor.live"], "rejected": [{ "topic": "alert.event.firing", "code": 120003 }] }, "ts": 1755403441905 }
```

部分订阅被拒不影响其余 topic，前端按 `rejected` 隐藏对应视图区域。

关闭码约定：

| Code | 含义 | 前端行为 |
| --- | --- | --- |
| 1000 | 正常关闭 | 不重连 |
| 1001 | 服务端下线或重启 | 退避重连 |
| 4001 | ticket 无效或过期 | 重新换 ticket 再连 |
| 4003 | 权限变更导致订阅失效 | 刷新 profile 后重连 |
| 4008 | 连接数或帧率超限 | 长退避（30 秒起）重连 |
| 4009 | 空闲超时 | 立即重连 |

重连策略与 Agent 通道一致：`min(1s * 2^(n-1), 60s)` 叠加 ±20% 抖动，重连成功后前端重放订阅列表并对增量数据做一次全量刷新以补齐断线期间的丢失。

### 5.9.2 Topic 清单与载荷

topic 命名 `<module>.<subject>` 或 `<module>.<subject>.<resourceId>`，带资源 ID 的形式表示只推该资源的事件。

| Topic | 权限点 | 推送频率 | 载荷要点 |
| --- | --- | --- | --- |
| `cluster.node.status` | `cluster:node:list` | 状态变更时 | `{ nodeId, nodeName, from, to, reason, at }` |
| `cluster.node.metrics` | `cluster:node:read` | 5 秒 | 节点核心指标快照，仅推订阅的 nodeIds |
| `cluster.task.<taskId>` | `cluster:task:read` | 逐项变更 | `{ taskId, nodeId, status, progress, stage, message, summary }` |
| `cluster.install.<taskId>` | `cluster:node:read` | 逐项变更 | 同上，额外带 `host` 与 `stage` |
| `website.site.status` | `website:site:list` | 状态变更时 | `{ siteId, status, reason }` |
| `cert.order.<orderId>` | `cert:order:read` | 阶段变更 | `{ orderId, stage, progress, reason }` |
| `file.transfer.<taskId>` | `file:transfer:list` | 1 秒或每 1% | `{ taskId, type, progress, speedBps, transferred, total, etaSeconds, status }` |
| `container.status` | `container:container:list` | Docker 事件驱动 | `{ containerId, name, action, state, health }` |
| `container.stats.<containerId>` | `container:container:read` | 2 秒 | CPU、内存、网络、块设备 |
| `container.logs.<containerId>` | `container:container:read` | 实时 | `{ ts, stream, text }`，按行推送 |
| `container.image.pull.<taskId>` | `container:image:read` | 每层进度 | `{ layerId, status, current, total, overallProgress }` |
| `container.compose.logs.<stackId>` | `container:compose:read` | 实时 | `{ service, ts, stream, text }` |
| `monitor.live` | `monitor:metric:read` | 参数指定，最小 2 秒 | 多序列最新点，`{ metric, labels, ts, value }[]` |
| `monitor.log-tail.<sourceId>` | `monitor:log:read` | 实时 | `{ ts, level, source, text, fields }` |
| `monitor.dashboard.<dashboardId>` | `monitor:dashboard:read` | 面板刷新间隔 | 按面板定义批量返回各 panel 数据 |
| `alert.event.firing` | `alert:event:list` | 事件产生时 | 事件对象精简版 + `aggregation` 增量 |
| `alert.event.resolved` | `alert:event:list` | 恢复时 | `{ eventId, ruleId, endsAt, durationSeconds }` |
| `appstore.install.<instanceId>` | `appstore:instance:read` | 步骤变更 | `{ step, index, total, status, message }` |
| `backup.task.<taskId>` | `backup:record:read` | 1 秒 | `{ stage, progress, transferred, total, storageId, speedBps }` |
| `backup.restore.<restoreId>` | `backup:restore:read` | 步骤变更 | `{ item, stage, progress, status, message }` |
| `system.upgrade.<taskId>` | `setting:upgrade:read` | 阶段变更 | `{ stage, progress, message, restarting }` |
| `system.notification` | — | 事件产生时 | 面板内通知，喂给 `useNotificationStore` |
| `security.event` | `security:ids:list` | 事件产生时 | `{ type, severity, nodeId, srcIp, detail }` |

节点指标推送示例：

```json
{
  "v": 1,
  "type": "event",
  "topic": "cluster.node.metrics",
  "nodeId": "1874900000000002",
  "payload": {
    "at": 1755403445000,
    "cpu": { "usage": 23.4, "load1": 0.82, "load5": 0.95, "load15": 1.02, "cores": 8 },
    "memory": { "total": 16777216000, "used": 9663676416, "usage": 57.6, "swapUsage": 0 },
    "disk": [{ "mount": "/", "total": 214748364800, "used": 96636764160, "usage": 45.0 }],
    "network": [{ "device": "eth0", "recvBps": 1048576, "sentBps": 524288 }],
    "process": { "total": 187, "running": 2 },
    "uptime": 1892344
  },
  "ts": 1755403445012
}
```

节点状态变更示例：

```json
{ "v": 1, "type": "event", "topic": "cluster.node.status", "payload": { "nodeId": "1874900000000009", "nodeName": "hk-job-01", "from": "online", "to": "offline", "reason": "心跳连续 3 次未达", "at": 1755403450000 }, "ts": 1755403450008 }
```

服务端下发约束：

| 约束 | 规则 |
| --- | --- |
| 帧率上限 | 单连接 50 帧/秒，超限触发合并；持续超限关闭连接（4008） |
| 合并策略 | 同 topic 同资源的高频事件在 200 ms 窗口内合并为最新值（指标、进度类），日志与终端类不合并 |
| 背压 | 单连接发送缓冲 4 MB，写阻塞超过 5 秒丢弃非关键帧（指标、stats），关键帧（状态、告警、错误）改为落库后由前端补拉 |
| 载荷上限 | 单帧 256 KB，日志类超长行截断并置 `truncated: true` |
| 顺序 | 同 topic 严格有序，跨 topic 不保证；进度类载荷自带 `progress` 便于前端丢弃过期帧 |
| 断线补偿 | 状态与告警类事件同时落库，前端重连后拉一次列表即可对齐；日志与指标不补偿 |

终端与容器 exec 通道是特例：握手后不再使用 JSON 信封，直接双向传原始字节流，控制指令（窗口大小变更、心跳）用一个字节的前缀区分，协议细节见 [文件管理与 WebSSH](./08-文件管理与WebSSH.md)。

## 5.10 文件上传下载协议

### 5.10.1 上传

面板内所有上传统一走 5.8.4 的分片协议，不提供单请求整体上传接口，理由是分片协议天然支持断点续传、秒传与进度反馈，维护两套逻辑会让边界情况成倍增加。小文件（< `chunkSize`）走同一流程，只是分片数为 1。

| 环节 | 约束 |
| --- | --- |
| 分片大小 | 服务端在 init 时决定并返回，默认 8 MB；总分片数上限 10000，据此自动放大分片 |
| 并发 | 建议 3，服务端不限制，但单连接受 L2 限流约束 |
| 临时区 | `data/tmp/upload/<uploadId>/<index>.part`，权限 0600，属主为面板运行用户 |
| 清理 | 会话 24 小时过期，`cron` 内置任务每小时清理过期临时分片 |
| 校验 | 分片级 MD5（快）+ 整文件级 SHA256（准），前者用于重传决策，后者用于秒传与完整性 |
| 磁盘预留 | init 时校验目标分区剩余空间 ≥ `size * 1.1`，不足返回 `400013` |
| 跨节点 | 上传到非主控节点时，控制面接收分片后经 gRPC 流转发到目标 Agent，落盘在目标节点；控制面不留完整副本 |
| 病毒扫描 | 配置 `security.upload_scan=true` 时在 complete 阶段调用 ClamAV，命中返回 `700012` 并删除文件 |

前端封装为 `useUploader`，串起 init、并发 chunk、complete 与失败重试，进度来源优先用 WebSocket `file.transfer.<taskId>`（跨节点场景下 XHR 的 `onprogress` 只反映到控制面的进度，不代表落盘进度）。

### 5.10.2 下载

| 场景 | 接口 | 机制 |
| --- | --- | --- |
| 单文件 | `GET /file/download?path=...` | 支持 `Range`，`Accept-Ranges: bytes`，ETag 与 `If-Range` |
| 多选打包 | `POST /file/download/archive` → `taskId` | 异步打包到临时区，完成后前端用返回的 `downloadToken` 拉取 |
| 备份文件 | `GET /backup/records/{id}/download` | 本地存储直传，对象存储签发临时直链（默认 15 分钟） |
| 导出类 | `GET /audit/operations/export` 等 | 流式生成，不落盘，`Transfer-Encoding: chunked` |
| 日志与录像 | `GET /terminal/recordings/{id}/cast` | 直传 asciicast 文件 |

响应头约定：

```
Content-Type: application/octet-stream
Content-Disposition: attachment; filename="app.tar.gz"; filename*=UTF-8''app%E5%8C%85.tar.gz
Content-Length: 24815723
Accept-Ranges: bytes
ETag: "9f2c8b1e-17a3f"
X-Content-Sha256: 9f2c8b1e4a7d...
Cache-Control: no-store
```

`filename*` 用 RFC 5987 编码承载中文名，同时保留 ASCII 回退的 `filename`。`X-Content-Sha256` 仅在服务端已缓存该文件哈希时给出，供客户端校验。

浏览器下载无法携带 `Authorization` 头，因此下载类接口额外支持一次性 `downloadToken`：

```
POST /api/v1/file/download/token      { "path": "/opt/backup/app.tar.gz" }
→ { "code": 0, "data": { "token": "dlt_8c3f...", "expiresIn": 60, "url": "/api/v1/file/download?token=dlt_8c3f..." } }
```

token 绑定用户、路径、节点与来源 IP，60 秒有效、一次性消费、只授予读权限。审计记录在 token 签发时落库而非下载时，避免 token 泄露导致审计缺失。

导出类接口对超量数据的处理：预估行数超过阈值（审计 100 万行、日志 500 万行）返回 `710001` 或 `600008`，提示缩小时间窗或改用归档下载。未超限时流式写出，不在内存中构造完整结果集：

```go
func (h *AuditHandler) Export(c *gin.Context) {
    q, err := binding.BindQuery[dto.AuditQuery](c)
    if err != nil { response.Fail(c, err); return }
    n, err := h.svc.Count(c, q)
    if err != nil { response.Fail(c, err); return }
    if n > h.cfg.ExportMaxRows {
        response.Fail(c, errs.New(710001).WithField("count", n))
        return
    }
    c.Header("Content-Type", "text/csv; charset=utf-8")
    c.Header("Content-Disposition", response.Attachment("audit-"+time.Now().Format("20060102150405")+".csv"))
    c.Writer.WriteString("\xEF\xBB\xBF") // BOM，保证 Excel 正确识别 UTF-8
    w := csv.NewWriter(c.Writer)
    defer w.Flush()
    _ = w.Write(dto.AuditCSVHeader)
    // 游标分页，每批 1000 行，边读边写，内存恒定
    err = h.svc.IterateForExport(c, q, 1000, func(rows []*model.AuditOperation) error {
        for _, r := range rows {
            if err := w.Write(r.ToCSVRecord()); err != nil { return err }
        }
        w.Flush()
        return w.Error()
    })
    if err != nil {
        slog.Error("audit export aborted", "err", err, "traceId", trace.ID(c))
        // 响应头已发出，只能截断连接，前端按不完整文件处理
    }
}
```

## 5.11 跨节点调用语义

带 `X-Node-Id` 的请求由控制面统一路由，handler 与 service 不感知节点位置差异。

```
浏览器 ──HTTP──> 控制面 handler ──> service ──> NodeInvoker
                                                   ├─ nodeId 为主控 → 直接调本地 executor
                                                   └─ nodeId 为远程 → gRPC 双向流上的请求响应对
```

| 语义 | 规则 |
| --- | --- |
| 缺省节点 | 未传 `X-Node-Id` 时落到主控节点（`node_node.is_master=1`）；不接受隐式「任意节点」 |
| 无效节点 | 节点不存在返回 `200001`，不属于当前用户 `nodeScope` 返回 `120011` |
| 离线节点 | 只读接口若有缓存（最近一次上报快照）则返回缓存并置 `data.stale: true` 与 `data.staleAt`；写接口一律返回 `200002` |
| 超时 | 默认 30 秒，长任务类接口在 Agent 侧转异步并立即返回 `taskId`，不占用流 |
| 幂等 | `Idempotency-Key` 连同请求透传给 Agent，Agent 侧同样做去重，保证控制面重试不产生重复副作用 |
| 多节点批量 | 标 `M` 的列表接口支持 `nodeIds` 查询参数，控制面并发扇出后合并，每项带 `nodeId`；部分节点失败时整体仍返回 `code=0`，失败信息在 `data.errors[]` |
| 版本差异 | Agent 不支持某能力时返回 `100007`，`data.requiredAgentVersion` 告知最低版本 |
| 链路追踪 | `X-Request-Id` 透传到 Agent 并写入两侧日志，`traceId` 在响应中回带 |

多节点扇出的响应形态：

```json
{
  "code": 0,
  "data": {
    "list": [
      { "nodeId": "1874900000000002", "nodeName": "hk-web-02", "id": "9f2c8b1e4a7d", "name": "redis-cache", "state": "running" },
      { "nodeId": "1874900000000003", "nodeName": "hk-web-03", "id": "3a7d5f01c2e8", "name": "redis-cache", "state": "running" }
    ],
    "total": 2,
    "page": 1,
    "pageSize": 20,
    "errors": [
      { "nodeId": "1874900000000009", "nodeName": "hk-job-01", "code": 200002, "message": "节点离线" }
    ],
    "partial": true
  }
}
```

`partial: true` 提示结果不完整，前端在表格上方用 `Alert` 列出失败节点，而不是把整页判为错误——多节点视图里一台机器故障不应阻断其余机器的运维操作。

扇出并发受 `cluster.fanout_concurrency`（默认 20）限制，单节点超时 10 秒（短于普通调用，避免慢节点拖垮整体响应），超时节点计入 `errors` 而非无限等待。分页在扇出场景下按「先各节点取 `page*pageSize` 条，合并排序后切片」处理，节点数多时会有放大效应，因此多节点列表默认按节点分组展示并建议前端限定 `nodeIds`。

## 5.12 OpenAPI 3 与 SDK

### 5.12.1 注解与生成

用 swaggo 从 Go 注解生成 OpenAPI 文档，避免手写 YAML 与代码脱节。约定每个 handler 必须带完整注解，缺失的会被 lint 规则拦下：

```go
// CreateSite 创建站点
//
//	@Summary		创建站点
//	@Description	在指定节点创建站点，支持静态、PHP、反代、Node、Java、Python、负载均衡七种类型；
//	@Description	可选同时创建数据库与 FTP 账号，任一环节失败按逆序补偿回滚。
//	@Tags			website
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			X-Node-Id		header		string					true	"目标节点 ID"
//	@Param			Idempotency-Key	header		string					false	"幂等键"
//	@Param			body			body		dto.SiteCreateIn		true	"站点参数"
//	@Success		200				{object}	response.Body[dto.SiteCreateOut]
//	@Failure		400				{object}	response.Body[any]	"100001 参数错误、300002 域名格式非法、300005 配置校验失败"
//	@Failure		409				{object}	response.Body[any]	"300001 域名已占用、300004 根目录冲突、300006 端口占用"
//	@Failure		502				{object}	response.Body[any]	"200002 节点离线"
//	@Router			/website/sites [post]
//	@x-permission	website:site:create
func (h *SiteHandler) Create(c *gin.Context) { ... }
```

| 约定 | 规则 |
| --- | --- |
| 泛型响应 | `response.Body[T]` 让 swaggo 展开出 `{code,message,data,traceId,timestamp}` 的具体 data 类型 |
| 分页响应 | `response.Body[response.Page[dto.SiteItem]]` |
| 扩展字段 | `@x-permission` 写权限点，生成后由脚本校验与 5.7 清单一致 |
| Tags | 与 17 个业务模块一一对应，不新增 |
| 失败描述 | 按 HTTP 状态归并，描述里列出该状态下可能的业务码，供前端查阅 |
| 枚举 | DTO 字段用 `enums:"static,php,proxy,node,java,python,loadbalance"` 标注 |
| 示例 | 复杂 DTO 用 `example:` 标注，或在 `docs/examples/` 放独立 JSON 由 `@Param ... example` 引用 |

生成与校验：

```bash
make swagger        # swag init --parseDependency --parseInternal -o api/openapi
make swagger-lint   # spectral lint api/openapi/swagger.json，规则集见 .spectral.yaml
make swagger-diff   # oasdiff 对比上一 tag，破坏性变更导致 CI 失败
```

产物 `api/openapi/swagger.json` 与 `swagger.yaml` 入库，方便 review 时直接看契约变更。面板内置 Swagger UI 挂在 `/api/v1/docs`，默认仅 `is_super=1` 可访问，生产可通过 `server.docs_enabled=false` 完全关闭。

### 5.12.2 SDK 与客户端生成

| 目标 | 方式 | 产物 |
| --- | --- | --- |
| 前端 TypeScript | `openapi-typescript` 生成类型 + 自研模板生成请求函数 | `web/src/api/generated/` |
| Go SDK | `oapi-codegen` 生成 client | `pkg/sdk/` 单独 module，供 novactl 与第三方使用 |
| Python | 不官方维护，提供 OpenAPI 文件由使用者自行生成 | — |
| CLI | novactl 直接依赖 Go SDK，不重复实现 HTTP 层 | — |

前端只使用生成的类型，请求函数手写在 `web/src/api/<module>.ts`，因为生成的函数无法表达幂等键注入、节点头透传、错误码分支这些项目特定逻辑。类型与手写函数的一致性由 `tsc` 保证（见 [前端架构与 Arco 规范](./06-前端架构与Arco规范.md) 请求层章节）。

Go SDK 使用示例：

```go
cli, err := sdk.New("https://panel.example.com:34567",
    sdk.WithHMACAuth(ak, sk),            // 或 sdk.WithToken(accessToken)
    sdk.WithNode("1874900000000002"),
    sdk.WithTimeout(30*time.Second),
    sdk.WithRetry(3, sdk.BackoffExponential),
    sdk.WithCACert(caPEM),
)
if err != nil { return err }

site, err := cli.Website.CreateSite(ctx, &sdk.SiteCreateIn{
    Name: "blog-prod", Type: sdk.SiteTypePHP,
    Domains: []sdk.Domain{{Domain: "blog.example.com", Port: 443, IsDefault: true}},
    RootPath: "/opt/novapanel/data/vhost/blog-prod/public",
}, sdk.WithIdempotencyKey(uuid.NewString()))
switch {
case errs.Is(err, 300001):
    log.Printf("域名已占用: %v", errs.Field(err, "domain"))
case err != nil:
    return err
}
```

SDK 自动处理 accessToken 刷新、幂等键透传、429 退避重试与 `errs` 错误码还原，使调用方能用与服务端一致的方式判错。

## 5.13 接口变更与废弃流程

版本策略是「路径版本 + 字段兼容演进」：`/api/v1` 在整个 v1.x 生命周期不变，只做向后兼容的字段增补；需要破坏性变更时新增 `/api/v2` 并与 v1 并行至少两个大版本。

| 变更类型 | 是否破坏性 | 处理方式 |
| --- | --- | --- |
| 新增可选请求字段 | 否 | 直接加，给默认值 |
| 新增响应字段 | 否 | 直接加，前端必须容忍未知字段 |
| 新增枚举值 | 是（对客户端） | 提前一个版本在文档公告，前端对未知枚举走 default 分支 |
| 请求字段改必填 | 是 | 禁止，改为新增接口 |
| 字段改名或改类型 | 是 | 新旧并存一个版本，旧字段标 deprecated 并在响应中同时返回 |
| 删除字段或接口 | 是 | 走废弃流程，最少两个 minor 版本的过渡期 |
| 错误码语义变更 | 是 | 禁止，只能新增码 |
| 默认值变化 | 是 | 需在 CHANGELOG 的「行为变更」段落显著说明 |

废弃流程四步：

1. 标记。文档中该接口标题加 `（已废弃，v1.5 移除）`，代码注解加 `@Deprecated`，OpenAPI 中 `deprecated: true`。
2. 响应告知。响应头带 `Deprecation: true`、`Sunset: Wed, 01 Apr 2027 00:00:00 GMT`、`Link: </api/v1/website/sites>; rel="successor-version"`，前端请求层统一在控制台打警告。
3. 观测。埋点统计废弃接口调用量与调用方（用户或 Token），面板设置页展示「废弃接口使用情况」，管理员可据此判断能否移除。
4. 移除。达到 Sunset 且连续 30 天调用量为 0 才移除；仍有调用则顺延一个版本并通知调用方。

变更评审要求：任何涉及本文件的 PR 必须勾选检查项——是否更新 5.5 错误码表、是否更新 5.7 清单、是否更新 swaggo 注解、是否为破坏性变更、是否需要前端同步改造、是否需要迁移文件。`make swagger-diff` 检出破坏性变更时 CI 强制要求 PR 描述里包含 `BREAKING CHANGE:` 段落说明迁移路径。

## 5.14 调试示例

### 5.14.1 curl 全流程

```bash
BASE=https://panel.example.com:34567/api/v1
JAR=/tmp/nova-cookie.txt

# 1. 登录，refreshToken 存 cookie jar，accessToken 取到变量
AT=$(curl -sS -c "$JAR" -X POST "$BASE/auth/login" \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"YourPassw0rd"}' | jq -r '.data.accessToken')

# 2. 带 token 调只读接口
curl -sS "$BASE/cluster/nodes?page=1&pageSize=20&sort=name&order=asc" \
  -H "Authorization: Bearer $AT" | jq '.data.list[] | {id,name,status}'

# 3. 写接口带幂等键与节点头
curl -sS -X POST "$BASE/website/sites" \
  -H "Authorization: Bearer $AT" \
  -H "X-Node-Id: 1874900000000002" \
  -H "Idempotency-Key: $(uuidgen)" \
  -H 'Content-Type: application/json' \
  -d @site-create.json | jq '{code,message,site:.data.site.id}'

# 4. 危险操作：先换确认票据再调用
CFT=$(curl -sS -X POST "$BASE/auth/confirm" \
  -H "Authorization: Bearer $AT" -H 'Content-Type: application/json' \
  -d '{"permission":"website:site:delete","password":"YourPassw0rd"}' | jq -r '.data.confirmToken')

curl -sS -X DELETE "$BASE/website/sites/1874400000000031?deleteData=true" \
  -H "Authorization: Bearer $AT" -H "X-Node-Id: 1874900000000002" \
  -H "X-Confirm-Token: $CFT" | jq

# 5. accessToken 过期后刷新
AT=$(curl -sS -b "$JAR" -c "$JAR" -X POST "$BASE/auth/refresh" | jq -r '.data.accessToken')
```

### 5.14.2 API Token HMAC 签名

签名串为 `Method\nPath\nSortedQuery\nSHA256(Body)\nTimestamp\nNonce`，Path 不含 query，query 按键名排序后 `k=v&` 拼接（无 query 则为空行）。

```bash
AK=nova_ak_7f3a9c2b
SK=nova_sk_8e1d5f0a3c7b9e2d4f6a8c0b1d3e5f79
METHOD=GET
PATH=/api/v1/cluster/nodes
QUERY='page=1&pageSize=20'
BODY=''
TS=$(date +%s)
NONCE=$(head -c 16 /dev/urandom | xxd -p)
BODY_HASH=$(printf '%s' "$BODY" | openssl dgst -sha256 -hex | awk '{print $2}')
SIGN_STR=$(printf '%s\n%s\n%s\n%s\n%s\n%s' "$METHOD" "$PATH" "$QUERY" "$BODY_HASH" "$TS" "$NONCE")
SIG=$(printf '%s' "$SIGN_STR" | openssl dgst -sha256 -hmac "$SK" -hex | awk '{print $2}')

curl -sS "https://panel.example.com:34567${PATH}?${QUERY}" \
  -H "Authorization: NOVA-HMAC-SHA256 AK=${AK},Timestamp=${TS},Nonce=${NONCE},Signature=${SIG}" | jq
```

常见失败对照：`110011` 检查服务器与客户端时钟偏差是否超过 300 秒；`110012` 换新 Nonce，不要复用；`110013` 优先核对 query 是否排序、body 哈希是否为空串哈希、换行符是否为 `\n` 而非 `\r\n`；`110014` 检查 Token 绑定的 IP 白名单。

### 5.14.3 WebSocket 调试

```bash
# 换 ticket
TICKET=$(curl -sS -X POST "$BASE/ws/ticket" \
  -H "Authorization: Bearer $AT" -H 'Content-Type: application/json' \
  -d '{"channel":"hub"}' | jq -r '.data.ticket')

# 用 websocat 连接并订阅
websocat "wss://panel.example.com:34567/api/v1/ws/hub?ticket=$TICKET" <<'EOF'
{"v":1,"type":"sub","id":"r1","payload":{"topics":["cluster.node.status","alert.event.firing"]},"ts":0}
EOF
```

握手返回 401 时先确认 ticket 未过期（30 秒）且未被使用过；连接后立刻收到 `error` 帧则看 `payload.code`，`120003` 表示 channel 权限不足。前端联调时可在浏览器控制台执行 `window.__NOVA_WS__.debug = true` 打开全部帧日志（仅开发构建注入）。

### 5.14.4 novactl 与 traceId 排障

```bash
novactl login --server https://panel.example.com:34567 --username admin
novactl node ls -o wide
novactl site create -f site-create.yaml --node hk-web-02
novactl exec --tag 'prod && web' -- systemctl reload nginx
novactl log tail --module website --level warn
novactl trace 01J5X8ZQ3K7N2M9P4R6T8V0W1Y     # 按 traceId 串联控制面与 Agent 两侧日志
```

任何异常响应都带 `traceId`，把它交给 `novactl trace` 或在面板日志检索里搜索，可拿到该请求在控制面的完整调用链（handler → service → repository → executor 或 NodeInvoker）与 Agent 侧对应日志。生产排障的标准流程是：前端截图取 `traceId` → `novactl trace` 定位失败环节 → 若在 Agent 侧则用 `novactl node log <nodeId>` 拉取该节点日志。
