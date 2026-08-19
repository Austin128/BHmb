package model

// SysRole 对应 sys_role 表。code 在 Casbin 中的主体形式为 role:<code>。
type SysRole struct {
	Base
	SoftDelete
	Code          string  `gorm:"column:code;not null"`
	Name          string  `gorm:"column:name;not null"`
	Description   string  `gorm:"column:description;not null;default:''"`
	IsBuiltin     bool    `gorm:"column:is_builtin;not null;default:0"`
	DataScope     string  `gorm:"column:data_scope;not null;default:'self'"`
	ScopeNodeJSON *string `gorm:"column:scope_node_json"`
	Sort          int     `gorm:"column:sort;not null;default:0"`
	Status        string  `gorm:"column:status;not null;default:'active'"`
}

// TableName 指定表名。
func (SysRole) TableName() string { return "sys_role" }

// SysUserRole 对应 sys_user_role 关联表，变更即物理删除后重建。
type SysUserRole struct {
	Relation
	UserID int64 `gorm:"column:user_id;not null"`
	RoleID int64 `gorm:"column:role_id;not null"`
}

// TableName 指定表名。
func (SysUserRole) TableName() string { return "sys_user_role" }

// SysPermission 对应 sys_permission 表，为迁移脚本按 code 幂等写入的内置数据。
type SysPermission struct {
	Base
	Code        string `gorm:"column:code;not null"`
	Name        string `gorm:"column:name;not null"`
	Module      string `gorm:"column:module;not null"`
	Resource    string `gorm:"column:resource;not null"`
	Action      string `gorm:"column:action;not null"`
	APIPath     string `gorm:"column:api_path;not null;default:''"`
	HTTPMethod  string `gorm:"column:http_method;not null;default:''"`
	IsSensitive bool   `gorm:"column:is_sensitive;not null;default:0"`
	NeedAudit   bool   `gorm:"column:need_audit;not null;default:1"`
	Sort        int    `gorm:"column:sort;not null;default:0"`
}

// TableName 指定表名。
func (SysPermission) TableName() string { return "sys_permission" }

// SysRolePermission 对应 sys_role_permission 关联表，permission_code 为冗余字段，
// 供 Casbin 装载策略时免 JOIN。
type SysRolePermission struct {
	Relation
	RoleID         int64  `gorm:"column:role_id;not null"`
	PermissionID   int64  `gorm:"column:permission_id;not null"`
	PermissionCode string `gorm:"column:permission_code;not null"`
}

// TableName 指定表名。
func (SysRolePermission) TableName() string { return "sys_role_permission" }

// PermissionAll 为超级管理员的通配权限标识。
const PermissionAll = "*"

// 内置角色标识。
const (
	RoleSuperAdmin = "super_admin"
	RoleAdmin      = "admin"
	RoleOps        = "ops"
	RoleDeveloper  = "developer"
	RoleAuditor    = "auditor"
	RoleReadonly   = "readonly"
)
