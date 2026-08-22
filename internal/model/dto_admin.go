package model

import "time"

// 本文件为只读的账号/角色/权限点展示 DTO。M0 阶段不提供增删改，
// 因此这里只暴露页面需要的字段，password_hash 等敏感列一律不出现。

// UserListItem 为用户列表项。id 以字符串序列化，避免前端 int64 精度丢失。
type UserListItem struct {
	ID       int64  `json:"id,string"`
	Username string `json:"username"`
	Nickname string `json:"nickname"`
	Email    string `json:"email,omitempty"`
	Status   string `json:"status"`
	IsSuper  bool   `json:"isSuper"`
	// TwoFABound 表示该账号是否已开启二次验证。
	TwoFABound bool `json:"twoFABound"`
	// Roles 为角色名，RoleCodes 为对应的角色标识。
	Roles     []string `json:"roles"`
	RoleCodes []string `json:"roleCodes"`
	// LockedUntil 非空表示账号仍在登录失败锁定期内。
	LockedUntil        *time.Time `json:"lockedUntil,omitempty"`
	LoginFailCount     int        `json:"loginFailCount"`
	MustChangePassword bool       `json:"mustChangePassword"`
	LastLoginAt        *time.Time `json:"lastLoginAt,omitempty"`
	LastLoginIP        string     `json:"lastLoginIp,omitempty"`
	CreatedAt          time.Time  `json:"createdAt"`
}

// RoleListItem 为角色列表项。
type RoleListItem struct {
	ID              int64  `json:"id,string"`
	Code            string `json:"code"`
	Name            string `json:"name"`
	Description     string `json:"description,omitempty"`
	DataScope       string `json:"dataScope"`
	IsBuiltin       bool   `json:"isBuiltin"`
	Status          string `json:"status"`
	Sort            int    `json:"sort"`
	PermissionCount int64  `json:"permissionCount"`
	// AllPermissions 为真表示该角色绕过权限校验（超管）。
	// 超管在数据库里根本没有权限绑定行，PermissionCount 就是 0，
	// 前端必须靠这个字段区分「全部权限」与「确实没权限」。
	AllPermissions bool `json:"allPermissions"`
}

// PermissionItem 为权限点列表项。
type PermissionItem struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Module      string `json:"module"`
	Resource    string `json:"resource"`
	Action      string `json:"action"`
	IsSensitive bool   `json:"isSensitive"`
	// Granted 表示当前登录者是否持有该权限点。
	Granted bool `json:"granted"`
}
