// Package v1 装配 v1 版本的路由与中间件链。
package v1

import (
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/novapanel/novapanel/internal/api/v1/handler"
	"github.com/novapanel/novapanel/internal/api/v1/middleware"
	"github.com/novapanel/novapanel/internal/api/v1/response"
	"github.com/novapanel/novapanel/internal/pkg/errs"
)

// BasePath 为 v1 接口前缀。
const BasePath = "/api/v1"

// Options 为路由装配参数。
type Options struct {
	Auth   *handler.Auth
	Health *handler.Health
	// File 为主机文件管理处理器；为空时不注册文件路由（未配置白名单等场景）。
	File *handler.File
	// AuthMW 为认证中间件依赖，受保护路由使用。
	AuthMW middleware.AuthDeps
	// StaticFS 为已构建的前端静态资源；为空时不注册 SPA 路由（例如纯 API 部署）。
	StaticFS fs.FS
	// TrustedProxies 为可信代理列表，空表示不信任任何代理头（ClientIP 取直连地址）。
	TrustedProxies []string
	// Debug 为真时启用 gin 调试模式。
	Debug bool
}

// NewEngine 构造 gin 引擎。中间件顺序：Trace → Recovery → AccessLog，
// 保证 panic 与访问日志都带 traceId。
func NewEngine(o Options) (*gin.Engine, error) {
	if o.Debug {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	e := gin.New()
	e.RedirectTrailingSlash = false
	if err := e.SetTrustedProxies(o.TrustedProxies); err != nil {
		return nil, errs.Wrap(err, errs.CodeInvalidParam, "可信代理配置无效")
	}

	e.Use(middleware.Trace(), middleware.Recovery(), middleware.AccessLog())

	e.NoMethod(func(c *gin.Context) {
		response.Fail(c, errs.New(errs.CodeUnsupported, "请求方法不被支持"))
	})
	e.NoRoute(noRoute(o.StaticFS))

	Register(e, o)
	// gin 在路由树为空时进入 405 分支会 panic（make cap = -1），
	// 因此仅在确实注册了路由后开启方法不允许检查。
	e.HandleMethodNotAllowed = len(e.Routes()) > 0
	return e, nil
}

// Register 在给定引擎上注册 v1 路由，便于测试单独装配。
func Register(e *gin.Engine, o Options) {
	v1 := e.Group(BasePath)

	if o.Health != nil {
		v1.GET("/health", o.Health.Check) // 探针接口，无需认证
	}

	if o.Auth != nil {
		public := v1.Group("/auth")
		public.POST("/login", o.Auth.Login)
		public.POST("/2fa/verify", o.Auth.VerifyTwoFA)
		public.POST("/refresh", o.Auth.Refresh)
	}

	if o.AuthMW.Tokens == nil || o.AuthMW.Principals == nil {
		return // 未装配认证依赖时只暴露公开路由
	}
	authed := v1.Group("", middleware.Auth(o.AuthMW))
	if o.Auth != nil {
		authed.POST("/auth/logout", o.Auth.Logout)
		authed.GET("/auth/profile", o.Auth.Profile)
	}
	registerFile(authed, o.File)
}

// registerFile 注册文件管理路由。读、写、删、执行分开鉴权，
// 权限点定义见 internal/rbac/permission_def.go。
func registerFile(authed *gin.RouterGroup, h *handler.File) {
	if h == nil {
		return
	}
	g := authed.Group("/file")

	read := g.Group("", middleware.RequirePermission("file:file:read"))
	read.GET("/stat", h.Stat)
	read.GET("/content", h.Content)
	read.GET("/download", h.Download)
	read.GET("/du", h.Du)

	list := g.Group("", middleware.RequirePermission("file:file:list"))
	list.GET("/roots", h.Roots)
	list.GET("/list", h.List)
	list.GET("/search", h.Search)

	create := g.Group("", middleware.RequirePermission("file:file:create"))
	create.POST("/dir", h.CreateDir)
	create.POST("/file", h.CreateFile)
	create.POST("/upload", h.Upload)
	// 分片上传（含续传查询与放弃）同属「新建文件」语义：
	// abort 只清理服务端自管的分片会话，不触碰用户文件，故不要求 delete 权限
	create.POST("/upload/init", h.UploadInit)
	create.POST("/upload/chunk", h.UploadChunk)
	create.POST("/upload/complete", h.UploadComplete)
	create.GET("/upload/status", h.UploadStatus)
	create.DELETE("/upload/:uploadId", h.UploadAbort)

	update := g.Group("", middleware.RequirePermission("file:file:update"))
	update.PUT("/content", h.Save)
	update.POST("/rename", h.Rename)
	update.POST("/move", h.Move)
	update.POST("/copy", h.Copy)

	g.DELETE("/items", middleware.RequirePermission("file:file:delete"), h.Delete)

	exec := g.Group("", middleware.RequirePermission("file:file:exec"))
	exec.POST("/compress", h.Compress)
	exec.POST("/decompress", h.Decompress)

	g.PUT("/permission", middleware.RequirePermission("file:permission:update"), h.Permission)
}

// noRoute 处理未匹配路由：API 前缀返回 100003 信封，其余交给 SPA。
func noRoute(static fs.FS) gin.HandlerFunc {
	var fileServer http.Handler
	if static != nil {
		fileServer = http.FileServer(http.FS(static))
	}
	return func(c *gin.Context) {
		reqPath := c.Request.URL.Path
		if strings.HasPrefix(reqPath, "/api/") || fileServer == nil {
			response.Fail(c, errs.New(errs.CodeNotFound, "接口不存在"))
			return
		}
		if serveStatic(c, static, fileServer, reqPath) {
			return
		}
		// 前端为 history 路由，未命中文件时回落 index.html
		c.Request.URL.Path = "/"
		fileServer.ServeHTTP(c.Writer, c.Request)
	}
}

func serveStatic(c *gin.Context, static fs.FS, fileServer http.Handler, reqPath string) bool {
	name := strings.TrimPrefix(path.Clean(reqPath), "/")
	if name == "" || name == "." {
		return false
	}
	f, err := static.Open(name)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	if info, err := f.Stat(); err != nil || info.IsDir() {
		return false
	}
	fileServer.ServeHTTP(c.Writer, c.Request)
	return true
}
