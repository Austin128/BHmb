package model

import "time"

// SysUser 对应 sys_user 表。password_hash 与登录风控字段不得出现在 API 响应中。
type SysUser struct {
	Base
	SoftDelete
	Username           string     `gorm:"column:username;not null"`
	Nickname           string     `gorm:"column:nickname;not null;default:''"`
	Email              string     `gorm:"column:email;not null;default:''"`
	Mobile             string     `gorm:"column:mobile;not null;default:''"`
	PasswordHash       string     `gorm:"column:password_hash;not null"`
	Avatar             string     `gorm:"column:avatar;not null;default:''"`
	Status             string     `gorm:"column:status;not null;default:'active'"`
	IsSuper            bool       `gorm:"column:is_super;not null;default:0"`
	TwoFactorEnabled   bool       `gorm:"column:two_factor_enabled;not null;default:0"`
	AllowIPJSON        *string    `gorm:"column:allow_ip_json"`
	LastLoginAt        *time.Time `gorm:"column:last_login_at"`
	LastLoginIP        string     `gorm:"column:last_login_ip;not null;default:''"`
	LoginFailCount     int        `gorm:"column:login_fail_count;not null;default:0"`
	LockedUntil        *time.Time `gorm:"column:locked_until"`
	PasswordUpdatedAt  *time.Time `gorm:"column:password_updated_at"`
	MustChangePassword bool       `gorm:"column:must_change_password;not null;default:0"`
	Lang               string     `gorm:"column:lang;not null;default:'zh-CN'"`
	Timezone           string     `gorm:"column:timezone;not null;default:'Asia/Shanghai'"`
	Remark             string     `gorm:"column:remark;not null;default:''"`
}

// TableName 指定表名。
func (SysUser) TableName() string { return "sys_user" }

// IsLocked 判断账号在给定时刻是否处于锁定期内。
func (u *SysUser) IsLocked(now time.Time) bool {
	return u.LockedUntil != nil && u.LockedUntil.After(now)
}

// IsActive 判断账号状态是否允许登录。
func (u *SysUser) IsActive() bool { return u.Status == UserStatusActive }
