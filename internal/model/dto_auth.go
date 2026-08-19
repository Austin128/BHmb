package model

import "time"

// 本文件为认证链路的请求/响应 DTO，字段与 docs/05-API接口规范.md 5.8.1 一一对应。
// 硬性约束：refreshToken 明文只允许写入 Cookie，任何 DTO 都不得把它序列化进 JSON。

// TokenTypeBearer 为固定的令牌类型。
const TokenTypeBearer = "Bearer"

// 2FA 校验方式。
const (
	TwoFAMethodTOTP     = "totp"
	TwoFAMethodRecovery = "recovery"
)

// LoginRequest 为 POST /api/v1/auth/login 的请求体。
type LoginRequest struct {
	Username    string `json:"username" binding:"required,min=3,max=32"`
	Password    string `json:"password" binding:"required,min=1,max=72"`
	CaptchaID   string `json:"captchaId" binding:"omitempty,max=64"`
	CaptchaCode string `json:"captchaCode" binding:"omitempty,max=16"`
	// Remember 为真时 refreshToken 有效期延长到 security.jwt_remember_ttl。
	Remember bool `json:"remember"`
}

// TwoFAVerifyRequest 为 POST /api/v1/auth/2fa/verify 的请求体。
type TwoFAVerifyRequest struct {
	TwoFAToken string `json:"twoFAToken" binding:"required,max=128"`
	Method     string `json:"method" binding:"required,oneof=totp recovery"`
	Code       string `json:"code" binding:"required,min=6,max=32"`
}

// LoginMeta 为登录来源信息，由 handler 从请求上下文填充。
type LoginMeta struct {
	ClientIP   string
	UserAgent  string
	DeviceType string
}

// UserBrief 为登录与 profile 复用的用户摘要。id 以字符串序列化，避免前端 int64 精度丢失。
type UserBrief struct {
	ID                 int64      `json:"id,string"`
	Username           string     `json:"username"`
	Nickname           string     `json:"nickname"`
	Avatar             *string    `json:"avatar"`
	Email              string     `json:"email,omitempty"`
	IsSuper            bool       `json:"isSuper"`
	Language           string     `json:"language"`
	Theme              string     `json:"theme"`
	MustChangePassword bool       `json:"mustChangePassword"`
	TwoFABound         bool       `json:"twoFABound"`
	LastLoginAt        *time.Time `json:"lastLoginAt,omitempty"`
	LastLoginIP        string     `json:"lastLoginIp,omitempty"`
}

// TwoFAChallenge 为登录一阶段票据，随 110009 一起返回。
type TwoFAChallenge struct {
	TwoFAToken string   `json:"twoFAToken"`
	ExpiresIn  int64    `json:"expiresIn"`
	Methods    []string `json:"methods"`
}

// LoginResult 为登录/2FA 校验成功后的响应体。
// TwoFA 非空表示需要二次验证，此时 AccessToken 为空，handler 必须以 110009 返回 TwoFA。
type LoginResult struct {
	AccessToken string    `json:"accessToken"`
	ExpiresIn   int64     `json:"expiresIn"`
	TokenType   string    `json:"tokenType"`
	User        UserBrief `json:"user"`
	Roles       []string  `json:"roles"`
	Permissions []string  `json:"permissions"`

	TwoFA *TwoFAChallenge `json:"-"`
	// RefreshToken 与 RefreshTTL 仅供 handler 写 Cookie，禁止序列化。
	RefreshToken string        `json:"-"`
	RefreshTTL   time.Duration `json:"-"`
}

// RefreshResult 为 POST /api/v1/auth/refresh 的响应体。
type RefreshResult struct {
	AccessToken string `json:"accessToken"`
	ExpiresIn   int64  `json:"expiresIn"`
	TokenType   string `json:"tokenType"`

	// 轮换后的新 refreshToken，仅供 handler 写 Cookie。
	RefreshToken string        `json:"-"`
	RefreshTTL   time.Duration `json:"-"`
}

// RoleBrief 为 profile 中的角色摘要。
type RoleBrief struct {
	Code      string `json:"code"`
	Name      string `json:"name"`
	DataScope string `json:"dataScope"`
}

// ProfileResult 为 GET /api/v1/auth/profile 的响应体。
// permissions 是前端 usePermissionStore 的唯一数据源；nodeScope 为空表示可见全部节点。
type ProfileResult struct {
	User        UserBrief   `json:"user"`
	Roles       []RoleBrief `json:"roles"`
	Permissions []string    `json:"permissions"`
	NodeScope   []string    `json:"nodeScope"`
	Menus       []string    `json:"menus"`
}
