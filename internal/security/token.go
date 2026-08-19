package security

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/novapanel/novapanel/internal/pkg/errs"
)

// TokenIssuer 的固定参数。
const (
	// Issuer 为 JWT iss 声明。
	Issuer = "novapanel"
	// refreshTokenBytes 为不透明 refreshToken 的随机字节数（docs/12 12.7.5）。
	refreshTokenBytes = 32
)

// Claims 为 accessToken 承载的声明（docs/12 12.7.5）。
// rol 仅供前端菜单渲染，服务端鉴权一律以数据库中的权限点为准。
type Claims struct {
	jwt.RegisteredClaims
	TenantID int64    `json:"ten"`
	Roles    []string `json:"rol,omitempty"`
	// PwdUpdatedAt 为用户 password_updated_at 的 Unix 秒，用于改密后批量失效旧令牌。
	PwdUpdatedAt int64 `json:"pwd"`
	// SessionID 为 sys_session.id，便于按会话吊销。
	SessionID int64 `json:"sid,string"`
}

// UserID 返回 sub 解析出的用户主键。
func (c *Claims) UserID() (int64, error) {
	id, err := strconv.ParseInt(c.Subject, 10, 64)
	if err != nil {
		return 0, errs.Wrap(err, errs.CodeUnauthorized, "登录凭证无效")
	}
	return id, nil
}

// TokenIssuer 使用 HS256 签发与校验 accessToken。
type TokenIssuer struct {
	secret    []byte
	accessTTL time.Duration
}

// NewTokenIssuer 构造签发器。secret 应由主密钥派生（见 DeriveKey）。
func NewTokenIssuer(secret []byte, accessTTL time.Duration) (*TokenIssuer, error) {
	if len(secret) < 32 {
		return nil, errs.New(errs.CodeInvalidParam, "令牌签名密钥不足 32 字节")
	}
	if accessTTL <= 0 {
		return nil, errs.New(errs.CodeInvalidParam, "accessToken 有效期必须为正")
	}
	return &TokenIssuer{secret: secret, accessTTL: accessTTL}, nil
}

// AccessTTL 返回 accessToken 有效期，便于响应体填写 expiresIn。
func (t *TokenIssuer) AccessTTL() time.Duration { return t.accessTTL }

// Sign 签发 accessToken，返回令牌串与过期时间。
func (t *TokenIssuer) Sign(userID, tenantID, sessionID int64, jti string, roles []string, pwdUpdatedAt time.Time, now time.Time) (string, time.Time, error) {
	if jti == "" {
		return "", time.Time{}, errs.New(errs.CodeInvalidParam, "jti 不能为空")
	}
	expireAt := now.Add(t.accessTTL)
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    Issuer,
			Subject:   strconv.FormatInt(userID, 10),
			ID:        jti,
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expireAt),
		},
		TenantID:     tenantID,
		Roles:        roles,
		PwdUpdatedAt: pwdUpdatedAt.Unix(),
		SessionID:    sessionID,
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(t.secret)
	if err != nil {
		return "", time.Time{}, errs.Wrap(err, errs.CodeInternal, "令牌签发失败")
	}
	return signed, expireAt, nil
}

// Parse 校验签名与时间窗口并返回声明。过期与非法分别映射到 110002 与 110001。
func (t *TokenIssuer) Parse(token string) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(token, claims, func(*jwt.Token) (any, error) {
		return t.secret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithIssuer(Issuer))
	switch {
	case err == nil:
		return claims, nil
	case errors.Is(err, jwt.ErrTokenExpired):
		return nil, errs.Wrap(err, errs.CodeTokenExpired, "登录已过期")
	default:
		return nil, errs.Wrap(err, errs.CodeUnauthorized, "未登录或登录已过期")
	}
}

// NewJTI 生成 accessToken 的唯一标识（128 位随机，十六进制）。
func NewJTI() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", errs.Wrap(err, errs.CodeInternal, "jti 生成失败")
	}
	return hex.EncodeToString(b), nil
}

// NewRefreshToken 生成不透明 refreshToken 与其 SHA-256 十六进制哈希。
// 明文只下发到 Cookie，数据库仅存哈希（docs/04 4.5.8）。
func NewRefreshToken() (token, hash string, err error) {
	b := make([]byte, refreshTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", "", errs.Wrap(err, errs.CodeInternal, "刷新凭证生成失败")
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, HashRefreshToken(token), nil
}

// HashRefreshToken 计算 refreshToken 的存储哈希。
func HashRefreshToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
