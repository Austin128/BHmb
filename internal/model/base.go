// Package model 定义持久化实体。实体只描述数据结构，不含业务逻辑；
// 主键由雪花生成器提供，禁止依赖数据库自增。
package model

import (
	"time"

	"gorm.io/gorm"
)

// Base 为所有业务表的公共字段（docs/04 4.1.2）。
type Base struct {
	ID        int64     `gorm:"column:id;primaryKey;autoIncrement:false"`
	CreatedAt time.Time `gorm:"column:created_at;not null"`
	UpdatedAt time.Time `gorm:"column:updated_at;not null"`
	CreatedBy int64     `gorm:"column:created_by;not null;default:0"`
	TenantID  int64     `gorm:"column:tenant_id;not null;default:0"`
}

// SoftDelete 为需要软删除的业务表提供 deleted_at。
type SoftDelete struct {
	DeletedAt gorm.DeletedAt `gorm:"column:deleted_at;index"`
}

// Relation 为关联表公共字段：无软删、无 updated_at。
type Relation struct {
	ID        int64     `gorm:"column:id;primaryKey;autoIncrement:false"`
	CreatedAt time.Time `gorm:"column:created_at;not null"`
	CreatedBy int64     `gorm:"column:created_by;not null;default:0"`
	TenantID  int64     `gorm:"column:tenant_id;not null;default:0"`
}

// 用户状态。
const (
	UserStatusActive   = "active"
	UserStatusDisabled = "disabled"
	UserStatusLocked   = "locked"
	UserStatusPending  = "pending"
)

// 角色与租户等通用状态。
const (
	StatusActive   = "active"
	StatusDisabled = "disabled"
)

// 数据范围。
const (
	DataScopeAll   = "all"
	DataScopeGroup = "group"
	DataScopeSelf  = "self"
)

// 会话状态。
const (
	SessionStatusActive  = "active"
	SessionStatusRevoked = "revoked"
	SessionStatusExpired = "expired"
)

// 会话吊销原因。
const (
	RevokeReasonLogout          = "logout"
	RevokeReasonKickout         = "kickout"
	RevokeReasonPasswordChanged = "password_changed"
	RevokeReasonAdminRevoke     = "admin_revoke"
	RevokeReasonRefreshReuse    = "refresh_reuse"
)
