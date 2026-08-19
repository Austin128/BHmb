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
)

// Permissions 为 M0 覆盖的权限点（dashboard / user / ops 三个模块）。
// 其余模块随对应里程碑追加，id 顺延分配，已分配的 id 不得复用。
var Permissions = []Permission{
	{ID: 1001, Code: "dashboard:overview:read", Name: "查看总览", Module: "dashboard", Resource: "overview", Action: "read", Roles: rAll},

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
