package model

import "time"

// SysSession 对应 sys_session 表。只存 refreshToken 的 SHA-256，不存明文。
type SysSession struct {
	Base
	UserID           int64     `gorm:"column:user_id;not null"`
	JTI              string    `gorm:"column:jti;not null"`
	RefreshTokenHash string    `gorm:"column:refresh_token_hash;not null"`
	DeviceType       string    `gorm:"column:device_type;not null;default:'web'"`
	UserAgent        string    `gorm:"column:user_agent;not null;default:''"`
	ClientIP         string    `gorm:"column:client_ip;not null;default:''"`
	LoginAt          time.Time `gorm:"column:login_at;not null"`
	LastActiveAt     time.Time `gorm:"column:last_active_at;not null"`
	AccessExpireAt   time.Time `gorm:"column:access_expire_at;not null"`
	RefreshExpireAt  time.Time `gorm:"column:refresh_expire_at;not null"`
	Status           string    `gorm:"column:status;not null;default:'active'"`
	RevokeReason     string    `gorm:"column:revoke_reason;not null;default:''"`
}

// TableName 指定表名。
func (SysSession) TableName() string { return "sys_session" }

// IsUsable 判断会话在给定时刻是否可用于刷新。
func (s *SysSession) IsUsable(now time.Time) bool {
	return s.Status == SessionStatusActive && s.RefreshExpireAt.After(now)
}

// SysTenant 对应 sys_tenant 表，自身 tenant_id 恒为 0。
type SysTenant struct {
	Base
	SoftDelete
	Code          string     `gorm:"column:code;not null"`
	Name          string     `gorm:"column:name;not null"`
	Status        string     `gorm:"column:status;not null;default:'active'"`
	ExpireAt      *time.Time `gorm:"column:expire_at"`
	QuotaJSON     *string    `gorm:"column:quota_json"`
	ContactName   string     `gorm:"column:contact_name;not null;default:''"`
	ContactEmail  string     `gorm:"column:contact_email;not null;default:''"`
	ContactMobile string     `gorm:"column:contact_mobile;not null;default:''"`
	Remark        string     `gorm:"column:remark;not null;default:''"`
}

// TableName 指定表名。
func (SysTenant) TableName() string { return "sys_tenant" }

// 租户状态。
const (
	TenantStatusActive    = "active"
	TenantStatusSuspended = "suspended"
	TenantStatusExpired   = "expired"
)

// SysSetting 对应 sys_setting 表。value_type=secret 时 item_value 必须加密存储。
type SysSetting struct {
	Base
	GroupKey     string  `gorm:"column:group_key;not null"`
	ItemKey      string  `gorm:"column:item_key;not null"`
	ItemValue    *string `gorm:"column:item_value"`
	ValueType    string  `gorm:"column:value_type;not null;default:'string'"`
	DefaultValue *string `gorm:"column:default_value"`
	Title        string  `gorm:"column:title;not null;default:''"`
	Description  string  `gorm:"column:description;not null;default:''"`
	IsEncrypted  bool    `gorm:"column:is_encrypted;not null;default:0"`
	IsPublic     bool    `gorm:"column:is_public;not null;default:0"`
	Sort         int     `gorm:"column:sort;not null;default:0"`
}

// TableName 指定表名。
func (SysSetting) TableName() string { return "sys_setting" }

// 设置项值类型。
const (
	ValueTypeString = "string"
	ValueTypeInt    = "int"
	ValueTypeBool   = "bool"
	ValueTypeJSON   = "json"
	ValueTypeSecret = "secret"
)

// SysSchemaMigration 对应 sys_schema_migration 迁移记录表。
type SysSchemaMigration struct {
	Version    int64     `gorm:"column:version;primaryKey;autoIncrement:false"`
	Name       string    `gorm:"column:name;not null"`
	AppliedAt  time.Time `gorm:"column:applied_at;not null"`
	Checksum   string    `gorm:"column:checksum;not null"`
	DurationMs int64     `gorm:"column:duration_ms;not null;default:0"`
}

// TableName 指定表名。
func (SysSchemaMigration) TableName() string { return "sys_schema_migration" }
