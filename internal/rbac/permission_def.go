// Package rbac 是内置角色与权限点的**唯一数据源**（docs/04 4.18）。
// 种子 SQL 与前端菜单均由此导出，禁止在数据库中手工新增权限点。
// 新增模块时在 Permissions 末尾追加，并同步 docs/04 4.18.2 的清单。
package rbac

import "github.com/novapanel/novapanel/internal/model"

// 内置 id 固定占用 1~9999：角色 1~99，权限点自 1001 起；雪花 ID 从 10000 起分配。
const (
	RoleIDSuperAdmin int64 = 1
	RoleIDAdmin      int64 = 2
	RoleIDOps        int64 = 3
	RoleIDDeveloper  int64 = 4
	RoleIDAuditor    int64 = 5
	RoleIDReadonly   int64 = 6
)

// DefaultTenantID 为单租户部署下的默认租户主键。
const DefaultTenantID int64 = 1

// Role 描述内置角色。
type Role struct {
	ID          int64
	Code        string
	Name        string
	Description string
	DataScope   string
	Sort        int
}

// Roles 为六个内置角色。super_admin 绕过权限校验，不需要显式权限绑定。
// 注：docs/04 4.18.1 将 developer 的数据范围记为 custom，但 sys_role.data_scope
// 的枚举只有 all/group/self，此处取语义等价的 group。
var Roles = []Role{
	{ID: RoleIDSuperAdmin, Code: model.RoleSuperAdmin, Name: "超级管理员", Description: "系统最高权限，不受权限校验限制", DataScope: model.DataScopeAll, Sort: 1},
	{ID: RoleIDAdmin, Code: model.RoleAdmin, Name: "系统管理员", Description: "除许可证与升级外的全部权限", DataScope: model.DataScopeAll, Sort: 2},
	{ID: RoleIDOps, Code: model.RoleOps, Name: "运维工程师", Description: "节点/网站/容器/数据库/备份运维权限", DataScope: model.DataScopeAll, Sort: 3},
	{ID: RoleIDDeveloper, Code: model.RoleDeveloper, Name: "开发者", Description: "授权范围内的网站与运行环境权限", DataScope: model.DataScopeGroup, Sort: 4},
	{ID: RoleIDAuditor, Code: model.RoleAuditor, Name: "审计员", Description: "全模块只读与审计日志导出", DataScope: model.DataScopeAll, Sort: 5},
	{ID: RoleIDReadonly, Code: model.RoleReadonly, Name: "只读访客", Description: "全模块只读", DataScope: model.DataScopeAll, Sort: 6},
}

// Permission 描述一个权限点及其内置角色归属。
type Permission struct {
	ID       int64
	Code     string
	Name     string
	Module   string
	Resource string
	Action   string
	// Sensitive 为真时接口需要二次确认票据。
	Sensitive bool
	// Roles 为持有该权限的内置角色 code（super_admin 隐含全部权限，无需列出）。
	Roles []string
}

// 角色组合的简写，便于对照 docs/04 4.18.2 的 a / o / d / au / r 列。
var (
	rAll       = []string{model.RoleAdmin, model.RoleOps, model.RoleDeveloper, model.RoleAuditor, model.RoleReadonly}
	rAdminOnly = []string{model.RoleAdmin}
	rAdminOps  = []string{model.RoleAdmin, model.RoleOps}
	rReadTeam  = []string{model.RoleAdmin, model.RoleAuditor, model.RoleReadonly}
	rReadOps   = []string{model.RoleAdmin, model.RoleOps, model.RoleAuditor, model.RoleReadonly}
	// rWriteDev 为含开发者的写权限组合，用于网站目录与运行环境相关的文件操作。
	rWriteDev = []string{model.RoleAdmin, model.RoleOps, model.RoleDeveloper}
	// rReadDev 为含开发者的读权限组合。
	rReadDev = []string{model.RoleAdmin, model.RoleOps, model.RoleDeveloper, model.RoleAuditor, model.RoleReadonly}
)

// Permissions 为 M0、文件管理（M1）与网站管理（M2）覆盖的权限点。
// 其余模块随对应里程碑追加，id 顺延分配，已分配的 id 不得复用。
var Permissions = []Permission{
	{ID: 1001, Code: "dashboard:overview:read", Name: "查看总览", Module: "dashboard", Resource: "overview", Action: "read", Roles: rAll},

	// 声明顺序即菜单顺序：建站与文件是日常运维入口，排在账号与系统设置之前。
	// id 不随声明位置变动，已分配的 id 不得复用。
	{ID: 1031, Code: "website:site:list", Name: "站点列表", Module: "website", Resource: "site", Action: "list", Roles: rReadDev},
	{ID: 1032, Code: "website:site:read", Name: "站点详情", Module: "website", Resource: "site", Action: "read", Roles: rReadDev},
	{ID: 1033, Code: "website:site:create", Name: "创建站点", Module: "website", Resource: "site", Action: "create", Roles: rWriteDev},
	{ID: 1034, Code: "website:site:update", Name: "编辑站点与域名", Module: "website", Resource: "site", Action: "update", Roles: rWriteDev},
	{ID: 1035, Code: "website:site:delete", Name: "删除站点", Module: "website", Resource: "site", Action: "delete", Sensitive: true, Roles: rWriteDev},
	{ID: 1036, Code: "website:site:exec", Name: "启停站点与重载服务", Module: "website", Resource: "site", Action: "exec", Sensitive: true, Roles: rWriteDev},
	{ID: 1037, Code: "website:config:read", Name: "查看站点配置", Module: "website", Resource: "config", Action: "read", Roles: rReadDev},
	{ID: 1038, Code: "website:config:update", Name: "编辑站点配置与回滚", Module: "website", Resource: "config", Action: "update", Sensitive: true, Roles: rWriteDev},
	{ID: 1039, Code: "website:setting:read", Name: "查看站点设置", Module: "website", Resource: "setting", Action: "read", Roles: rReadDev},
	{ID: 1040, Code: "website:setting:update", Name: "修改站点设置", Module: "website", Resource: "setting", Action: "update", Roles: rWriteDev},
	// 伪静态是直接下发到 Nginx 的原始片段，风险高于普通设置项，单列权限并标记为敏感。
	{ID: 1041, Code: "website:rewrite:read", Name: "查看伪静态规则", Module: "website", Resource: "rewrite", Action: "read", Roles: rReadDev},
	{ID: 1042, Code: "website:rewrite:update", Name: "修改伪静态规则", Module: "website", Resource: "rewrite", Action: "update", Sensitive: true, Roles: rWriteDev},
	// 日志可能含访客 IP 与 URL 参数，读权限与站点详情一致；清空不可撤销，标记为敏感。
	{ID: 1043, Code: "website:log:read", Name: "查看站点日志", Module: "website", Resource: "log", Action: "read", Roles: rReadDev},
	{ID: 1044, Code: "website:log:exec", Name: "清空站点日志", Module: "website", Resource: "log", Action: "exec", Sensitive: true, Roles: rWriteDev},

	{ID: 1024, Code: "file:file:list", Name: "浏览目录", Module: "file", Resource: "file", Action: "list", Roles: rReadDev},
	{ID: 1025, Code: "file:file:read", Name: "读取与下载文件", Module: "file", Resource: "file", Action: "read", Roles: rReadDev},
	{ID: 1026, Code: "file:file:create", Name: "新建与上传文件", Module: "file", Resource: "file", Action: "create", Roles: rWriteDev},
	{ID: 1027, Code: "file:file:update", Name: "编辑与移动文件", Module: "file", Resource: "file", Action: "update", Roles: rWriteDev},
	{ID: 1028, Code: "file:file:delete", Name: "删除文件", Module: "file", Resource: "file", Action: "delete", Sensitive: true, Roles: rWriteDev},
	{ID: 1029, Code: "file:file:exec", Name: "压缩与解压", Module: "file", Resource: "file", Action: "exec", Roles: rWriteDev},
	{ID: 1030, Code: "file:permission:update", Name: "修改文件权限与属主", Module: "file", Resource: "permission", Action: "update", Sensitive: true, Roles: rAdminOps},

	{ID: 1002, Code: "user:user:list", Name: "用户列表", Module: "user", Resource: "user", Action: "list", Roles: rReadTeam},
	{ID: 1003, Code: "user:user:read", Name: "用户详情", Module: "user", Resource: "user", Action: "read", Roles: rReadTeam},
	{ID: 1004, Code: "user:user:create", Name: "新建用户", Module: "user", Resource: "user", Action: "create", Roles: rAdminOnly},
	{ID: 1005, Code: "user:user:update", Name: "编辑用户", Module: "user", Resource: "user", Action: "update", Roles: rAdminOnly},
	{ID: 1006, Code: "user:user:delete", Name: "删除用户", Module: "user", Resource: "user", Action: "delete", Sensitive: true, Roles: rAdminOnly},
	{ID: 1007, Code: "user:user:exec", Name: "重置密码/强制下线", Module: "user", Resource: "user", Action: "exec", Sensitive: true, Roles: rAdminOnly},
	{ID: 1008, Code: "user:role:list", Name: "角色列表", Module: "user", Resource: "role", Action: "list", Roles: rReadTeam},
	{ID: 1009, Code: "user:role:read", Name: "角色详情", Module: "user", Resource: "role", Action: "read", Roles: rReadTeam},
	{ID: 1010, Code: "user:role:create", Name: "新建角色", Module: "user", Resource: "role", Action: "create", Roles: rAdminOnly},
	{ID: 1011, Code: "user:role:update", Name: "编辑角色与授权", Module: "user", Resource: "role", Action: "update", Roles: rAdminOnly},
	{ID: 1012, Code: "user:role:delete", Name: "删除角色", Module: "user", Resource: "role", Action: "delete", Sensitive: true, Roles: rAdminOnly},
	{ID: 1013, Code: "user:permission:list", Name: "权限点列表", Module: "user", Resource: "permission", Action: "list", Roles: rReadTeam},
	{ID: 1014, Code: "user:token:list", Name: "API Token 列表", Module: "user", Resource: "token", Action: "list", Roles: rReadOps},
	{ID: 1015, Code: "user:token:create", Name: "创建 API Token", Module: "user", Resource: "token", Action: "create", Roles: rAdminOps},
	{ID: 1016, Code: "user:token:delete", Name: "吊销 API Token", Module: "user", Resource: "token", Action: "delete", Sensitive: true, Roles: rAdminOps},

	{ID: 1017, Code: "ops:setting:list", Name: "系统设置列表", Module: "ops", Resource: "setting", Action: "list", Roles: rReadOps},
	{ID: 1018, Code: "ops:setting:update", Name: "修改系统设置", Module: "ops", Resource: "setting", Action: "update", Roles: rAdminOnly},
	{ID: 1019, Code: "ops:task:list", Name: "异步任务列表", Module: "ops", Resource: "task", Action: "list", Roles: rAll},
	{ID: 1020, Code: "ops:task:read", Name: "任务进度详情", Module: "ops", Resource: "task", Action: "read", Roles: rAll},
	{ID: 1021, Code: "ops:task:delete", Name: "取消/清理任务", Module: "ops", Resource: "task", Action: "delete", Roles: rAdminOps},
	{ID: 1022, Code: "ops:tenant:list", Name: "租户列表", Module: "ops", Resource: "tenant", Action: "list", Roles: rReadTeam},
	{ID: 1023, Code: "ops:upgrade:read", Name: "查看版本与更新", Module: "ops", Resource: "upgrade", Action: "read", Roles: rReadOps},
}

// PermissionCodes 返回全部权限点标识。
func PermissionCodes() []string {
	out := make([]string, 0, len(Permissions))
	for _, p := range Permissions {
		out = append(out, p.Code)
	}
	return out
}

// Modules 返回权限点声明顺序去重后的模块列表，即前端顶层菜单的全集。
func Modules() []string {
	seen := make(map[string]struct{}, len(Permissions))
	out := make([]string, 0, 8)
	for _, p := range Permissions {
		if _, ok := seen[p.Module]; ok {
			continue
		}
		seen[p.Module] = struct{}{}
		out = append(out, p.Module)
	}
	return out
}

// MenusOf 由权限点集合推导可见的顶层模块，顺序与 Permissions 声明一致。
// codes 含通配 "*" 时返回全部模块。
func MenusOf(codes []string) []string {
	granted := make(map[string]struct{}, len(codes))
	for _, c := range codes {
		if c == model.PermissionAll {
			return Modules()
		}
		granted[c] = struct{}{}
	}

	seen := make(map[string]struct{}, 8)
	out := make([]string, 0, 8)
	for _, p := range Permissions {
		if _, ok := granted[p.Code]; !ok {
			continue
		}
		if _, ok := seen[p.Module]; ok {
			continue
		}
		seen[p.Module] = struct{}{}
		out = append(out, p.Module)
	}
	return out
}

// CodesOfRole 返回指定内置角色的权限点标识；super_admin 返回通配 ["*"]。
func CodesOfRole(roleCode string) []string {
	if roleCode == model.RoleSuperAdmin {
		return []string{model.PermissionAll}
	}
	var out []string
	for _, p := range Permissions {
		for _, r := range p.Roles {
			if r == roleCode {
				out = append(out, p.Code)
				break
			}
		}
	}
	return out
}
