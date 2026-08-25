package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/novapanel/novapanel/internal/api/v1/middleware"
	"github.com/novapanel/novapanel/internal/api/v1/response"
	"github.com/novapanel/novapanel/internal/model"
	"github.com/novapanel/novapanel/internal/pkg/errs"
	sitesvc "github.com/novapanel/novapanel/internal/service/site"
)

// Website 处理站点全生命周期的 HTTP 接口。
type Website struct {
	svc *sitesvc.Service
}

// NewWebsite 构造站点处理器。
func NewWebsite(svc *sitesvc.Service) *Website {
	return &Website{svc: svc}
}

// Env 处理 GET /api/v1/website/env，返回建站前提（Web 服务是否就绪、支持的站点类型）。
func (h *Website) Env(c *gin.Context) {
	actor, ok := siteActor(c)
	if !ok {
		return
	}
	env, err := h.svc.Env(c.Request.Context(), actor)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, env)
}

// List 处理 GET /api/v1/website/sites。
func (h *Website) List(c *gin.Context) {
	actor, ok := siteActor(c)
	if !ok {
		return
	}
	page, pageSize := pageParams(c)
	out, err := h.svc.List(c.Request.Context(), actor, c.Query("keyword"), page, pageSize)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, out)
}

// Get 处理 GET /api/v1/website/sites/:id。
func (h *Website) Get(c *gin.Context) {
	actor, id, ok := siteActorAndID(c)
	if !ok {
		return
	}
	detail, err := h.svc.Get(c.Request.Context(), actor, id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, detail)
}

// Create 处理 POST /api/v1/website/sites。
func (h *Website) Create(c *gin.Context) {
	actor, ok := siteActor(c)
	if !ok {
		return
	}
	var req model.CreateSiteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, errs.Wrap(err, errs.CodeInvalidJSON, "请求体格式错误"))
		return
	}
	detail, err := h.svc.Create(c.Request.Context(), actor, &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, detail)
}

// Update 处理 PUT /api/v1/website/sites/:id。
func (h *Website) Update(c *gin.Context) {
	actor, id, ok := siteActorAndID(c)
	if !ok {
		return
	}
	var req model.UpdateSiteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, errs.Wrap(err, errs.CodeInvalidJSON, "请求体格式错误"))
		return
	}
	detail, err := h.svc.Update(c.Request.Context(), actor, id, &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, detail)
}

// ChangeStatus 处理 POST /api/v1/website/sites/:id/status，启停站点。
func (h *Website) ChangeStatus(c *gin.Context) {
	actor, id, ok := siteActorAndID(c)
	if !ok {
		return
	}
	var req model.ChangeSiteStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, errs.Wrap(err, errs.CodeInvalidJSON, "请求体格式错误"))
		return
	}
	detail, err := h.svc.ChangeStatus(c.Request.Context(), actor, id, req.Target)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, detail)
}

// Rebuild 处理 POST /api/v1/website/sites/:id/rebuild，按当前数据重新下发配置。
func (h *Website) Rebuild(c *gin.Context) {
	actor, id, ok := siteActorAndID(c)
	if !ok {
		return
	}
	detail, err := h.svc.Rebuild(c.Request.Context(), actor, id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, detail)
}

// Delete 处理 DELETE /api/v1/website/sites/:id。
// deleteRoot 走查询参数，便于前端在二次确认弹窗里直接拼接。
func (h *Website) Delete(c *gin.Context) {
	actor, id, ok := siteActorAndID(c)
	if !ok {
		return
	}
	deleteRoot := c.Query("deleteRoot") == "true"
	if err := h.svc.Delete(c.Request.Context(), actor, id, deleteRoot); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"deleted": true, "rootRecycled": deleteRoot})
}

// Config 处理 GET /api/v1/website/sites/:id/config。
func (h *Website) Config(c *gin.Context) {
	actor, id, ok := siteActorAndID(c)
	if !ok {
		return
	}
	cfg, err := h.svc.Config(c.Request.Context(), actor, id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, cfg)
}

// SaveConfig 处理 PUT /api/v1/website/sites/:id/config。
func (h *Website) SaveConfig(c *gin.Context) {
	actor, id, ok := siteActorAndID(c)
	if !ok {
		return
	}
	var req model.SaveSiteConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, errs.Wrap(err, errs.CodeInvalidJSON, "请求体格式错误"))
		return
	}
	cfg, err := h.svc.SaveConfig(c.Request.Context(), actor, id, &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, cfg)
}

// Rollback 处理 POST /api/v1/website/sites/:id/config/rollback/:version。
func (h *Website) Rollback(c *gin.Context) {
	actor, id, ok := siteActorAndID(c)
	if !ok {
		return
	}
	version, err := strconv.Atoi(c.Param("version"))
	if err != nil || version <= 0 {
		response.Fail(c, errs.New(errs.CodeInvalidParam, "配置版本号不合法"))
		return
	}
	cfg, err := h.svc.Rollback(c.Request.Context(), actor, id, version)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, cfg)
}

// Reload 处理 POST /api/v1/website/nginx/reload。
func (h *Website) Reload(c *gin.Context) {
	if _, ok := siteActor(c); !ok {
		return
	}
	if err := h.svc.ReloadWebServer(c.Request.Context()); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"reloaded": true})
}

// Test 处理 POST /api/v1/website/nginx/test，返回 nginx -t 输出。
func (h *Website) Test(c *gin.Context) {
	if _, ok := siteActor(c); !ok {
		return
	}
	out, err := h.svc.TestWebServer(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"ok": true, "output": out})
}

// Settings 处理 GET /api/v1/website/sites/:id/settings。
func (h *Website) Settings(c *gin.Context) {
	actor, id, ok := siteActorAndID(c)
	if !ok {
		return
	}
	out, err := h.svc.Settings(c.Request.Context(), actor, id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, out)
}

// SaveSettings 处理 PUT /api/v1/website/sites/:id/settings。
func (h *Website) SaveSettings(c *gin.Context) {
	actor, id, ok := siteActorAndID(c)
	if !ok {
		return
	}
	var req model.SaveSiteSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, errs.Wrap(err, errs.CodeInvalidJSON, "请求体格式错误"))
		return
	}
	out, err := h.svc.SaveSettings(c.Request.Context(), actor, id, &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, out)
}

// RewriteTemplates 处理 GET /api/v1/website/rewrite/templates，返回内置伪静态规则库。
// 规则库与具体站点无关，因此不接收站点 ID。
func (h *Website) RewriteTemplates(c *gin.Context) {
	if _, ok := siteActor(c); !ok {
		return
	}
	response.OK(c, gin.H{"items": h.svc.RewriteTemplateList()})
}

// Rewrite 处理 GET /api/v1/website/sites/:id/rewrite。
func (h *Website) Rewrite(c *gin.Context) {
	actor, id, ok := siteActorAndID(c)
	if !ok {
		return
	}
	out, err := h.svc.Rewrite(c.Request.Context(), actor, id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, out)
}

// SaveRewrite 处理 PUT /api/v1/website/sites/:id/rewrite。
func (h *Website) SaveRewrite(c *gin.Context) {
	actor, id, ok := siteActorAndID(c)
	if !ok {
		return
	}
	var req model.SaveSiteRewriteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, errs.Wrap(err, errs.CodeInvalidJSON, "请求体格式错误"))
		return
	}
	out, err := h.svc.SaveRewrite(c.Request.Context(), actor, id, &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, out)
}

// Logs 处理 GET /api/v1/website/sites/:id/logs，返回日志尾部若干行。
func (h *Website) Logs(c *gin.Context) {
	actor, id, ok := siteActorAndID(c)
	if !ok {
		return
	}
	kind := c.DefaultQuery("type", model.SiteLogAccess)
	lines := 0
	if raw := c.Query("lines"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			response.Fail(c, errs.New(errs.CodeInvalidParam, "lines 需为正整数"))
			return
		}
		lines = n
	}
	out, err := h.svc.Logs(c.Request.Context(), actor, id, kind, lines)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, out)
}

// ClearLog 处理 POST /api/v1/website/sites/:id/logs/clear，清空指定日志。
func (h *Website) ClearLog(c *gin.Context) {
	actor, id, ok := siteActorAndID(c)
	if !ok {
		return
	}
	out, err := h.svc.ClearLog(c.Request.Context(), actor, id, c.DefaultQuery("type", model.SiteLogAccess))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, out)
}

// siteActor 从登录态推导操作者，失败时已写好响应。
func siteActor(c *gin.Context) (sitesvc.Actor, bool) {
	p, ok := middleware.CurrentPrincipal(c)
	if !ok {
		response.Fail(c, errs.ErrUnauthorized)
		return sitesvc.Actor{}, false
	}
	return sitesvc.Actor{UserID: p.UserID, TenantID: p.TenantID}, true
}

func siteActorAndID(c *gin.Context) (sitesvc.Actor, int64, bool) {
	actor, ok := siteActor(c)
	if !ok {
		return actor, 0, false
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.Fail(c, errs.New(errs.CodeInvalidParam, "站点 ID 不合法"))
		return actor, 0, false
	}
	return actor, id, true
}
