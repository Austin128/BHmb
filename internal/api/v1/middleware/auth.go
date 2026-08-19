package middleware

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/novapanel/novapanel/internal/api/v1/response"
	"github.com/novapanel/novapanel/internal/model"
	"github.com/novapanel/novapanel/internal/pkg/ctxkey"
	"github.com/novapanel/novapanel/internal/pkg/errs"
	"github.com/novapanel/novapanel/internal/repository"
	"github.com/novapanel/novapanel/internal/security"
)

// gin.Context 键名。业务代码一律通过本包的访问函数读取，禁止散落字符串字面量。
const (
	ctxKeyClaims    = "nova_claims"
	ctxKeyPrincipal = "nova_principal"
)

// Principal 为鉴权所需的最小主体信息。
type Principal struct {
	UserID            int64
	TenantID          int64
	Username          string
	IsSuper           bool
	Status            string
	PasswordUpdatedAt time.Time
	Permissions       []string
}

// Allowed 判断是否持有指定权限点。超级管理员与通配权限直接放行。
func (p *Principal) Allowed(code string) bool {
	if p.IsSuper {
		return true
	}
	for _, c := range p.Permissions {
		if c == model.PermissionAll || c == code {
			return true
		}
	}
	return false
}

// PrincipalStore 装载主体信息。M0 每次请求直查数据库；
// 进程内权限缓存（L0）与失效广播随集群里程碑落地。
type PrincipalStore interface {
	Load(ctx context.Context, userID int64) (*Principal, error)
}

type repoPrincipalStore struct {
	users repository.UserRepository
	roles repository.RoleRepository
}

// NewPrincipalStore 用用户与角色仓储构造主体装载器。
func NewPrincipalStore(users repository.UserRepository, roles repository.RoleRepository) PrincipalStore {
	return &repoPrincipalStore{users: users, roles: roles}
}

func (s *repoPrincipalStore) Load(ctx context.Context, userID int64) (*Principal, error) {
	u, err := s.users.FindByID(ctx, userID)
	if err != nil {
		if errs.Code(err) == errs.CodeNotFound {
			return nil, errs.ErrUnauthorized
		}
		return nil, err
	}
	p := &Principal{
		UserID:   u.ID,
		TenantID: u.TenantID,
		Username: u.Username,
		IsSuper:  u.IsSuper,
		Status:   u.Status,
	}
	if u.PasswordUpdatedAt != nil {
		p.PasswordUpdatedAt = *u.PasswordUpdatedAt
	} else {
		p.PasswordUpdatedAt = u.CreatedAt
	}
	if u.IsSuper {
		p.Permissions = []string{model.PermissionAll}
		return p, nil
	}
	codes, err := s.roles.PermissionCodesOfUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	p.Permissions = codes
	return p, nil
}

// AuthDeps 为认证中间件依赖。
type AuthDeps struct {
	Tokens     *security.TokenIssuer
	Revokes    security.RevocationStore
	Principals PrincipalStore
}

// Auth 校验 accessToken：签名与时间窗 → jti 吊销名单 → 用户级吊销水位线 →
// 改密水位线 → 账号状态，通过后把 userID/tenantID 注入 context。
func Auth(d AuthDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := bearerToken(c.Request)
		if err != nil {
			response.Fail(c, err)
			return
		}
		claims, err := d.Tokens.Parse(token)
		if err != nil {
			response.Fail(c, err)
			return
		}
		ctx := c.Request.Context()

		revoked, err := d.Revokes.Revoked(ctx, claims.ID)
		if err != nil {
			response.Fail(c, err) // 名单查询失败按拒绝处理
			return
		}
		if revoked {
			response.Fail(c, errs.ErrTokenRevoked)
			return
		}

		userID, err := claims.UserID()
		if err != nil {
			response.Fail(c, err)
			return
		}
		watermark, err := d.Revokes.UserWatermark(ctx, userID)
		if err != nil {
			response.Fail(c, err)
			return
		}
		if !watermark.IsZero() && claims.IssuedAt != nil && claims.IssuedAt.Time.Before(watermark) {
			response.Fail(c, errs.ErrTokenRevoked)
			return
		}

		principal, err := d.Principals.Load(ctx, userID)
		if err != nil {
			response.Fail(c, err)
			return
		}
		if principal.Status != model.UserStatusActive {
			response.Fail(c, errs.ErrAccountDisabled)
			return
		}
		if principal.PasswordUpdatedAt.Unix() != claims.PwdUpdatedAt {
			// 口令已变更，旧令牌立即失效
			response.Fail(c, errs.ErrTokenRevoked)
			return
		}

		ctx = ctxkey.WithUserID(ctx, principal.UserID)
		ctx = ctxkey.WithTenantID(ctx, principal.TenantID)
		c.Request = c.Request.WithContext(ctx)
		c.Set(ctxKeyClaims, claims)
		c.Set(ctxKeyPrincipal, principal)
		c.Next()
	}
}

// RequirePermission 校验权限点，缺失返回 120003。多个权限点为「任一满足」。
func RequirePermission(codes ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		p, ok := CurrentPrincipal(c)
		if !ok {
			response.Fail(c, errs.ErrUnauthorized)
			return
		}
		for _, code := range codes {
			if p.Allowed(code) {
				c.Next()
				return
			}
		}
		response.Fail(c, errs.ErrForbidden)
	}
}

// CurrentClaims 返回当前请求的令牌声明。
func CurrentClaims(c *gin.Context) (*security.Claims, bool) {
	v, ok := c.Get(ctxKeyClaims)
	if !ok {
		return nil, false
	}
	claims, ok := v.(*security.Claims)
	return claims, ok
}

// CurrentPrincipal 返回当前请求的主体信息。
func CurrentPrincipal(c *gin.Context) (*Principal, bool) {
	v, ok := c.Get(ctxKeyPrincipal)
	if !ok {
		return nil, false
	}
	p, ok := v.(*Principal)
	return p, ok
}

// bearerToken 从 Authorization 头提取 Bearer 令牌。
func bearerToken(r *http.Request) (string, error) {
	h := r.Header.Get("Authorization")
	if h == "" {
		return "", errs.ErrUnauthorized
	}
	scheme, token, ok := strings.Cut(h, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(token) == "" {
		return "", errs.New(errs.CodeUnauthorized, "Authorization 头格式应为 Bearer <token>")
	}
	return strings.TrimSpace(token), nil
}
