// Package handler 实现 v1 版本的 HTTP 处理器。
// 处理器只负责参数绑定、Cookie 读写与响应封装，业务判定全部下沉到 service。
package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/novapanel/novapanel/internal/api/v1/middleware"
	"github.com/novapanel/novapanel/internal/api/v1/response"
	"github.com/novapanel/novapanel/internal/model"
	"github.com/novapanel/novapanel/internal/pkg/errs"
	"github.com/novapanel/novapanel/internal/service/auth"
)

// refreshToken Cookie 约定（docs/05 5.8.1）：仅在认证路径下发送，禁止 JS 读取。
const (
	RefreshCookieName = "nova_rt"
	RefreshCookiePath = "/api/v1/auth"
)

// Auth 为认证处理器。
type Auth struct {
	svc *auth.Service
	// secureCookie 为 false 时不设置 Secure 属性，仅允许本地 HTTP 调试使用。
	secureCookie bool
}

// NewAuth 构造认证处理器。secureCookie 应与是否启用 HTTPS 一致。
func NewAuth(svc *auth.Service, secureCookie bool) *Auth {
	return &Auth{svc: svc, secureCookie: secureCookie}
}

// Login 处理 POST /api/v1/auth/login。
func (h *Auth) Login(c *gin.Context) {
	var req model.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, bindError(err))
		return
	}
	res, err := h.svc.Login(c.Request.Context(), req, h.meta(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	h.writeLoginResult(c, res)
}

// VerifyTwoFA 处理 POST /api/v1/auth/2fa/verify。
func (h *Auth) VerifyTwoFA(c *gin.Context) {
	var req model.TwoFAVerifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, bindError(err))
		return
	}
	res, err := h.svc.VerifyTwoFA(c.Request.Context(), req, h.meta(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	h.writeLoginResult(c, res)
}

// Refresh 处理 POST /api/v1/auth/refresh，凭 Cookie 轮换令牌。
func (h *Auth) Refresh(c *gin.Context) {
	token, err := c.Cookie(RefreshCookieName)
	if err != nil || token == "" {
		response.Fail(c, errs.ErrRefreshInvalid)
		return
	}
	res, err := h.svc.Refresh(c.Request.Context(), token, h.meta(c))
	if err != nil {
		h.clearRefreshCookie(c) // 凭证失效时同步清 Cookie，避免前端反复重试
		response.Fail(c, err)
		return
	}
	h.setRefreshCookie(c, res.RefreshToken, res.RefreshTTL)
	response.OK(c, res)
}

// Logout 处理 POST /api/v1/auth/logout，需已登录。
func (h *Auth) Logout(c *gin.Context) {
	claims, ok := middleware.CurrentClaims(c)
	if !ok {
		response.Fail(c, errs.ErrUnauthorized)
		return
	}
	var expireAt time.Time
	if claims.ExpiresAt != nil {
		expireAt = claims.ExpiresAt.Time
	}
	if err := h.svc.Logout(c.Request.Context(), claims.SessionID, claims.ID, expireAt); err != nil {
		response.Fail(c, err)
		return
	}
	h.clearRefreshCookie(c)
	response.OK[any](c, nil)
}

// Profile 处理 GET /api/v1/auth/profile。
func (h *Auth) Profile(c *gin.Context) {
	p, ok := middleware.CurrentPrincipal(c)
	if !ok {
		response.Fail(c, errs.ErrUnauthorized)
		return
	}
	res, err := h.svc.Profile(c.Request.Context(), p.UserID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// writeLoginResult 按是否需要二次验证选择响应形态。
func (h *Auth) writeLoginResult(c *gin.Context, res *model.LoginResult) {
	if res.TwoFA != nil {
		// 一阶段：以 110009 返回票据，不下发任何令牌与 Cookie
		response.FailWith(c, errs.CodeNeed2FA, errs.ErrNeed2FA.Message, res.TwoFA)
		return
	}
	h.setRefreshCookie(c, res.RefreshToken, res.RefreshTTL)
	response.OK(c, res)
}

func (h *Auth) setRefreshCookie(c *gin.Context, token string, ttl time.Duration) {
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(RefreshCookieName, token, int(ttl.Seconds()), RefreshCookiePath, "", h.secureCookie, true)
}

func (h *Auth) clearRefreshCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(RefreshCookieName, "", -1, RefreshCookiePath, "", h.secureCookie, true)
}

func (h *Auth) meta(c *gin.Context) model.LoginMeta {
	return model.LoginMeta{
		ClientIP:   c.ClientIP(),
		UserAgent:  c.Request.UserAgent(),
		DeviceType: "web",
	}
}

// bindError 把绑定/校验失败统一成 100002 或 100001。
func bindError(err error) error {
	return errs.Wrap(err, errs.CodeInvalidParam, "请求参数有误")
}
