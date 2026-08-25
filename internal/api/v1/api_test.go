package v1_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	v1 "github.com/novapanel/novapanel/internal/api/v1"
	"github.com/novapanel/novapanel/internal/api/v1/handler"
	"github.com/novapanel/novapanel/internal/api/v1/middleware"
	"github.com/novapanel/novapanel/internal/api/v1/response"
	"github.com/novapanel/novapanel/internal/model"
	"github.com/novapanel/novapanel/internal/pkg/config"
	"github.com/novapanel/novapanel/internal/pkg/errs"
	"github.com/novapanel/novapanel/internal/pkg/idgen"
	"github.com/novapanel/novapanel/internal/rbac"
	"github.com/novapanel/novapanel/internal/repository"
	"github.com/novapanel/novapanel/internal/repository/migrate"
	"github.com/novapanel/novapanel/internal/repository/seed"
	"github.com/novapanel/novapanel/internal/security"
	"github.com/novapanel/novapanel/internal/service/auth"
	filesvc "github.com/novapanel/novapanel/internal/service/file"
	"github.com/novapanel/novapanel/internal/service/file/pathguard"
	sitesvc "github.com/novapanel/novapanel/internal/service/site"
	"github.com/novapanel/novapanel/internal/service/sysinfo"
	"github.com/novapanel/novapanel/internal/service/webserver"
	"github.com/novapanel/novapanel/migrations"
)

const testPassword = "Novapanel1!"

type app struct {
	handlerT http.Handler
	db       *gorm.DB
	revokes  *security.MemoryRevocationStore
	tokens   *security.TokenIssuer
	sessions repository.SessionRepository
	authMW   middleware.AuthDeps
	// fileRoot 为文件管理测试的唯一白名单根，限定在临时目录内。
	fileRoot string
	// websiteVhost 为网站测试的 vhost 目录，隔离在临时目录内。
	websiteVhost string
	// websiteRoot 为站点根目录的父目录，用于断言目录创建与回收。
	websiteRoot string
	// websiteLogDir 为站点日志目录，用于断言日志读取与清空。
	websiteLogDir string
}

// appOption 用于按需替换装配依赖，默认场景保持 newApp(t) 原样调用。
type appOption func(*appOptions)

// appOptions 汇总可替换的装配项。
type appOptions struct {
	authMutators []func(*auth.Deps)
	// websiteRunner 不为空时，网站模块会拿到一个可用的假 Web 服务，
	// 用于覆盖创建成功、租户隔离等正向路径。
	websiteRunner webserver.Runner
}

// withTwoFA 注入可控的 2FA 校验器，覆盖「验证通过」这条真实实现尚未提供的分支。
func withTwoFA(v auth.TwoFAVerifier) appOption {
	return func(o *appOptions) {
		o.authMutators = append(o.authMutators, func(d *auth.Deps) { d.TwoFA = v })
	}
}

// withWebServer 让网站模块使用可控的命令执行器，不依赖宿主机真装 Nginx。
func withWebServer(runner webserver.Runner) appOption {
	return func(o *appOptions) { o.websiteRunner = runner }
}

// stubTwoFA 只接受指定的 TOTP 动态码，其余一律 110010。
type stubTwoFA struct{ code string }

func (s stubTwoFA) Verify(_ context.Context, _ int64, method, code string) error {
	if method != model.TwoFAMethodTOTP {
		return errs.New(errs.Code2FAInvalid, "不支持的验证方式")
	}
	if code != s.code {
		return errs.New(errs.Code2FAInvalid, "动态码不正确")
	}
	return nil
}

func newApp(t *testing.T, opts ...appOption) *app {
	t.Helper()
	ctx := context.Background()

	var options appOptions
	for _, opt := range opts {
		opt(&options)
	}

	db, err := repository.Open(&config.DatabaseConfig{
		Driver:          repository.DriverSQLite,
		Path:            filepath.Join(t.TempDir(), "nova.db"),
		MaxOpenConns:    4,
		MaxIdleConns:    2,
		ConnMaxLifetime: time.Hour,
	}, 200*time.Millisecond)
	require.NoError(t, err)
	t.Cleanup(func() { _ = repository.Close(db) })

	_, err = migrate.New(db, migrations.FS, repository.Dialect(db)).Up(ctx)
	require.NoError(t, err)
	require.NoError(t, seed.New(db).Run(ctx))

	hasher := security.NewHasher(4)
	hash, err := hasher.Hash(testPassword)
	require.NoError(t, err)
	_, err = seed.New(db).EnsureAdmin(ctx, "admin", hash)
	require.NoError(t, err)

	tokens, err := security.NewTokenIssuer(make([]byte, 32), 15*time.Minute)
	require.NoError(t, err)
	revokes := security.NewMemoryRevocationStore()

	users := repository.NewUserRepository(db)
	roles := repository.NewRoleRepository(db)
	sessions := repository.NewSessionRepository(db)

	deps := auth.Deps{
		Users: users, Roles: roles, Sessions: sessions,
		Hasher: hasher, Tokens: tokens, Revokes: revokes,
	}
	for _, mutate := range options.authMutators {
		mutate(&deps)
	}
	svc, err := auth.New(deps, auth.Config{LoginFailLimit: 5, LockDuration: 15 * time.Minute})
	require.NoError(t, err)

	authMW := middleware.AuthDeps{
		Tokens:     tokens,
		Revokes:    revokes,
		Principals: middleware.NewPrincipalStore(users, roles),
	}
	// 文件管理测试只开放临时目录，避免测试触及宿主真实路径
	fileRoot := filepath.Join(t.TempDir(), "www")
	require.NoError(t, os.MkdirAll(fileRoot, 0o755))
	if real, err := filepath.EvalSymlinks(fileRoot); err == nil {
		fileRoot = real // macOS 的 /var 是符号链接，守卫返回的是解析后路径
	}
	fileSvc, err := filesvc.New(pathguard.New(pathguard.Config{
		AllowRoots: []string{fileRoot},
		DenyPaths:  []string{filepath.Join(fileRoot, "secret")},
	}), filesvc.Config{
		MaxEditSize:   1 << 20,
		MaxUploadSize: 1 << 20,
		// 分片会话不落在白名单目录内，测试里也隔离到各自的临时目录
		UploadTempDir: filepath.Join(filepath.Dir(fileRoot), "upload-tmp"),
	})
	require.NoError(t, err)

	// 网站模块：默认测试环境没有 Nginx，用指向不存在可执行文件的配置验证
	// 「前置条件缺失」这条真实路径；注入 withWebServer 后才走正向流程。
	websiteBase := t.TempDir()
	websiteCfg := config.WebsiteConfig{
		Enabled:    true,
		ServerType: "auto",
		NginxBin:   filepath.Join(websiteBase, "absent-nginx"),
		VhostDir:   filepath.Join(websiteBase, "vhost"),
		WwwRoot:    filepath.Join(websiteBase, "wwwroot"),
		LogDir:     filepath.Join(websiteBase, "wwwlogs"),
		BackupKeep: 20,
	}
	var siteRunner webserver.Runner = webserver.NewExecRunner(2 * time.Second)
	if options.websiteRunner != nil {
		// 假 nginx 只需存在且可执行，Detect 的 stat 检查就能通过，
		// 真正的命令行为由注入的 Runner 决定。
		websiteCfg.NginxBin = filepath.Join(websiteBase, "nginx")
		require.NoError(t, os.WriteFile(websiteCfg.NginxBin, []byte("#!/bin/sh\nexit 0\n"), 0o755))
		siteRunner = options.websiteRunner
	}
	renderer, err := sitesvc.NewRenderer("")
	require.NoError(t, err)
	siteSvc := sitesvc.NewService(
		repository.NewSiteRepository(db, idgen.NextID),
		webserver.NewManager(websiteCfg, siteRunner),
		renderer, websiteCfg, 34567, []string{websiteCfg.WwwRoot}, idgen.NextID,
	)

	engine, err := v1.NewEngine(v1.Options{
		Auth:   handler.NewAuth(svc, false), // 测试走 HTTP，不设置 Secure
		Health: handler.NewHealth(db, handler.BuildInfo{Version: "test"}, time.Now()),
		File:   handler.NewFile(fileSvc),
		System: handler.NewSystem(
			sysinfo.NewCollector(sysinfo.Build{Version: "test", Commit: "deadbeef"}, time.Now(), t.TempDir()), db),
		Admin:   handler.NewAdmin(users, roles),
		Website: handler.NewWebsite(siteSvc),
		AuthMW:  authMW,
	})
	require.NoError(t, err)

	// 额外挂一条需要具体权限点的路由，用于验证 RequirePermission
	engine.GET("/api/v1/test/perm",
		middleware.Auth(authMW),
		middleware.RequirePermission("user:user:create"),
		func(c *gin.Context) { response.OK(c, gin.H{"ok": true}) })

	return &app{handlerT: engine, db: db, revokes: revokes, tokens: tokens, sessions: sessions,
		authMW: authMW, fileRoot: fileRoot,
		websiteVhost: websiteCfg.VhostDir, websiteRoot: websiteCfg.WwwRoot, websiteLogDir: websiteCfg.LogDir}
}

type envelope struct {
	Code      int             `json:"code"`
	Message   string          `json:"message"`
	Data      json.RawMessage `json:"data"`
	TraceID   string          `json:"traceId"`
	Timestamp int64           `json:"timestamp"`
}

func (a *app) do(t *testing.T, req *http.Request) (*httptest.ResponseRecorder, envelope) {
	t.Helper()
	rec := httptest.NewRecorder()
	a.handlerT.ServeHTTP(rec, req)

	var env envelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env), "响应体必须是统一信封")
	assert.True(t, idgen.IsULID(env.TraceID), "traceId 必须是 26 位 ULID：%s", env.TraceID)
	assert.Positive(t, env.Timestamp)
	return rec, env
}

func jsonReq(t *testing.T, method, path, body string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

// login 返回 accessToken 与 refresh Cookie。
func (a *app) login(t *testing.T) (string, *http.Cookie) {
	t.Helper()
	rec, env := a.do(t, jsonReq(t, http.MethodPost, "/api/v1/auth/login",
		`{"username":"admin","password":"`+testPassword+`"}`))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Zero(t, env.Code)

	var data model.LoginResult
	require.NoError(t, json.Unmarshal(env.Data, &data))
	require.NotEmpty(t, data.AccessToken)

	cookies := rec.Result().Cookies()
	var rt *http.Cookie
	for _, ck := range cookies {
		if ck.Name == handler.RefreshCookieName {
			rt = ck
		}
	}
	require.NotNil(t, rt, "必须下发 nova_rt Cookie")
	return data.AccessToken, rt
}

func TestHealthEndpoint(t *testing.T) {
	a := newApp(t)
	rec, env := a.do(t, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Zero(t, env.Code)
	assert.Equal(t, "ok", env.Message)
	assert.NotEmpty(t, rec.Header().Get("X-Trace-Id"))

	var data handler.HealthStatus
	require.NoError(t, json.Unmarshal(env.Data, &data))
	assert.Equal(t, "ok", data.Status)
	assert.Equal(t, "test", data.Version)
	assert.Equal(t, repository.DriverSQLite, data.Database.Driver)
	assert.True(t, data.Database.OK)
}

func TestLoginSetsRefreshCookieAndHidesToken(t *testing.T) {
	a := newApp(t)
	rec, env := a.do(t, jsonReq(t, http.MethodPost, "/api/v1/auth/login",
		`{"username":"admin","password":"`+testPassword+`"}`))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Zero(t, env.Code)

	assert.NotContains(t, rec.Body.String(), "refreshToken", "响应体不得包含 refreshToken 字段")

	var data model.LoginResult
	require.NoError(t, json.Unmarshal(env.Data, &data))
	assert.Equal(t, model.TokenTypeBearer, data.TokenType)
	assert.EqualValues(t, 900, data.ExpiresIn)
	assert.Equal(t, []string{model.PermissionAll}, data.Permissions)
	assert.Empty(t, data.RefreshToken, "序列化后 refreshToken 必须为空")
	assert.NotContains(t, rec.Body.String(), "passwordHash")

	var rt *http.Cookie
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == handler.RefreshCookieName {
			rt = ck
		}
	}
	require.NotNil(t, rt)
	assert.True(t, rt.HttpOnly, "nova_rt 必须 HttpOnly")
	assert.Equal(t, http.SameSiteStrictMode, rt.SameSite)
	assert.Equal(t, "/api/v1/auth", rt.Path)
	assert.Equal(t, 7*24*3600, rt.MaxAge)
	assert.NotEmpty(t, rt.Value)

	// Cookie 中的明文与库中哈希不同
	sess, err := a.sessions.FindByRefreshHash(context.Background(), security.HashRefreshToken(rt.Value))
	require.NoError(t, err)
	assert.NotEqual(t, rt.Value, sess.RefreshTokenHash)
}

func TestLoginFailures(t *testing.T) {
	a := newApp(t)

	rec, env := a.do(t, jsonReq(t, http.MethodPost, "/api/v1/auth/login",
		`{"username":"admin","password":"wrong-password"}`))
	assert.Equal(t, http.StatusBadRequest, rec.Code, "docs/05 5.5 规定 110004 映射 400")
	assert.Equal(t, errs.CodeBadCredentials, env.Code)
	assert.Empty(t, rec.Result().Cookies())

	rec, env = a.do(t, jsonReq(t, http.MethodPost, "/api/v1/auth/login", `{"username":"ad"}`))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, errs.CodeInvalidParam, env.Code, "校验失败必须是 100001")

	rec, env = a.do(t, jsonReq(t, http.MethodPost, "/api/v1/auth/login", `{`))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, errs.CodeInvalidParam, env.Code)
	assert.NotContains(t, rec.Body.String(), "unexpected EOF", "解析细节属私有诊断，不得外泄")
}

func TestLoginTwoFAReturnsChallenge(t *testing.T) {
	a := newApp(t)
	require.NoError(t, a.db.Model(&model.SysUser{}).
		Where("username = ?", "admin").
		Update("two_factor_enabled", true).Error)

	rec, env := a.do(t, jsonReq(t, http.MethodPost, "/api/v1/auth/login",
		`{"username":"admin","password":"`+testPassword+`"}`))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, errs.CodeNeed2FA, env.Code)
	assert.Empty(t, rec.Result().Cookies(), "一阶段不得下发 Cookie")

	var ch model.TwoFAChallenge
	require.NoError(t, json.Unmarshal(env.Data, &ch))
	assert.Contains(t, ch.TwoFAToken, "tfa_")
	assert.EqualValues(t, 300, ch.ExpiresIn)

	// M0 未提供 2FA 密钥存储，默认校验器明确返回 110016
	rec, env = a.do(t, jsonReq(t, http.MethodPost, "/api/v1/auth/2fa/verify",
		`{"twoFAToken":"`+ch.TwoFAToken+`","method":"totp","code":"123456"}`))
	assert.Equal(t, errs.Code2FANotBound, env.Code)
	assert.NotEqual(t, http.StatusOK, rec.Code)
}

// TestTwoFAVerifyIssuesTokens 覆盖二阶段验证通过后的令牌签发，以及票据的一次性语义。
func TestTwoFAVerifyIssuesTokens(t *testing.T) {
	a := newApp(t, withTwoFA(stubTwoFA{code: "654321"}))
	require.NoError(t, a.db.Model(&model.SysUser{}).
		Where("username = ?", "admin").
		Update("two_factor_enabled", true).Error)

	_, env := a.do(t, jsonReq(t, http.MethodPost, "/api/v1/auth/login",
		`{"username":"admin","password":"`+testPassword+`"}`))
	require.Equal(t, errs.CodeNeed2FA, env.Code)
	var ch model.TwoFAChallenge
	require.NoError(t, json.Unmarshal(env.Data, &ch))

	// 错误动态码：110010，且不得下发任何 Cookie
	rec, env := a.do(t, jsonReq(t, http.MethodPost, "/api/v1/auth/2fa/verify",
		`{"twoFAToken":"`+ch.TwoFAToken+`","method":"totp","code":"000000"}`))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, errs.Code2FAInvalid, env.Code)
	assert.Empty(t, rec.Result().Cookies())

	// 票据被消费后需重新登录换票
	_, env = a.do(t, jsonReq(t, http.MethodPost, "/api/v1/auth/login",
		`{"username":"admin","password":"`+testPassword+`"}`))
	require.Equal(t, errs.CodeNeed2FA, env.Code)
	require.NoError(t, json.Unmarshal(env.Data, &ch))

	rec, env = a.do(t, jsonReq(t, http.MethodPost, "/api/v1/auth/2fa/verify",
		`{"twoFAToken":"`+ch.TwoFAToken+`","method":"totp","code":"654321"}`))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Zero(t, env.Code)

	var data model.LoginResult
	require.NoError(t, json.Unmarshal(env.Data, &data))
	assert.NotEmpty(t, data.AccessToken)
	assert.Empty(t, data.RefreshToken, "刷新令牌只走 Cookie")
	assert.NotContains(t, rec.Body.String(), "refreshToken")

	var rt *http.Cookie
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == handler.RefreshCookieName {
			rt = ck
		}
	}
	require.NotNil(t, rt, "二阶段通过后必须下发 nova_rt")
	assert.True(t, rt.HttpOnly)

	// 令牌可直接访问受保护接口
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/profile", nil)
	req.Header.Set("Authorization", "Bearer "+data.AccessToken)
	rec, env = a.do(t, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Zero(t, env.Code)

	// 同一票据重放：已消费 -> 110010
	_, env = a.do(t, jsonReq(t, http.MethodPost, "/api/v1/auth/2fa/verify",
		`{"twoFAToken":"`+ch.TwoFAToken+`","method":"totp","code":"654321"}`))
	assert.Equal(t, errs.Code2FAInvalid, env.Code, "一阶段票据必须一次性")
}

func TestProfileRequiresValidToken(t *testing.T) {
	a := newApp(t)

	rec, env := a.do(t, httptest.NewRequest(http.MethodGet, "/api/v1/auth/profile", nil))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, errs.CodeUnauthorized, env.Code)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/profile", nil)
	req.Header.Set("Authorization", "Token abc")
	_, env = a.do(t, req)
	assert.Equal(t, errs.CodeUnauthorized, env.Code)

	req = httptest.NewRequest(http.MethodGet, "/api/v1/auth/profile", nil)
	req.Header.Set("Authorization", "Bearer not-a-jwt")
	_, env = a.do(t, req)
	assert.Equal(t, errs.CodeUnauthorized, env.Code)

	accessToken, _ := a.login(t)
	req = httptest.NewRequest(http.MethodGet, "/api/v1/auth/profile", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	rec, env = a.do(t, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Zero(t, env.Code)

	var profile model.ProfileResult
	require.NoError(t, json.Unmarshal(env.Data, &profile))
	assert.Equal(t, "admin", profile.User.Username)
	assert.True(t, profile.User.IsSuper)
	assert.Equal(t, []string{model.PermissionAll}, profile.Permissions)
	assert.Equal(t, rbac.Modules(), profile.Menus)
	assert.Empty(t, profile.NodeScope)
}

func TestRefreshRotatesCookie(t *testing.T) {
	a := newApp(t)
	_, rt := a.login(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	req.AddCookie(rt)
	rec, env := a.do(t, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Zero(t, env.Code)

	var refreshed model.RefreshResult
	require.NoError(t, json.Unmarshal(env.Data, &refreshed))
	assert.NotEmpty(t, refreshed.AccessToken)
	assert.NotContains(t, rec.Body.String(), rt.Value, "新旧 refreshToken 都不得出现在响应体")

	var newRT *http.Cookie
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == handler.RefreshCookieName {
			newRT = ck
		}
	}
	require.NotNil(t, newRT)
	assert.NotEqual(t, rt.Value, newRT.Value, "刷新必须轮换 refreshToken")

	// 旧 Cookie 立即失效
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	req.AddCookie(rt)
	_, env = a.do(t, req)
	assert.Equal(t, errs.CodeRefreshInvalid, env.Code)
}

func TestRefreshWithoutCookie(t *testing.T) {
	a := newApp(t)
	rec, env := a.do(t, httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, errs.CodeRefreshInvalid, env.Code)
}

func TestLogoutInvalidatesAccessAndRefresh(t *testing.T) {
	a := newApp(t)
	accessToken, rt := a.login(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.AddCookie(rt)
	rec, env := a.do(t, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Zero(t, env.Code)

	// Cookie 被清除
	var cleared bool
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == handler.RefreshCookieName && ck.MaxAge < 0 {
			cleared = true
		}
	}
	assert.True(t, cleared, "登出必须清除 nova_rt")

	// accessToken 进吊销名单
	req = httptest.NewRequest(http.MethodGet, "/api/v1/auth/profile", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	_, env = a.do(t, req)
	assert.Equal(t, errs.CodeTokenRevoked, env.Code)

	// refreshToken 不可再用
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	req.AddCookie(rt)
	_, env = a.do(t, req)
	assert.Equal(t, errs.CodeRefreshInvalid, env.Code)
}

func TestPasswordChangeInvalidatesToken(t *testing.T) {
	a := newApp(t)
	accessToken, _ := a.login(t)

	// 改密后 pwd claim 与用户当前 password_updated_at 不匹配
	users := repository.NewUserRepository(a.db)
	u, err := users.FindByUsername(context.Background(), rbac.DefaultTenantID, "admin")
	require.NoError(t, err)
	require.NoError(t, users.UpdatePassword(context.Background(), u.ID, "$2a$04$another", time.Now().UTC().Add(time.Second)))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/profile", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	_, env := a.do(t, req)
	assert.Equal(t, errs.CodeTokenRevoked, env.Code)
}

func TestTraceAndRequestIDHandling(t *testing.T) {
	a := newApp(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	req.Header.Set("X-Request-Id", "client-req-0001")
	rec, env := a.do(t, req)
	assert.Equal(t, "client-req-0001", rec.Header().Get("X-Request-Id"), "合法请求 ID 应回显")
	assert.NotEqual(t, "client-req-0001", env.TraceID, "客户端不得指定 traceId")
	assert.Equal(t, env.TraceID, rec.Header().Get("X-Trace-Id"))

	// 非法请求 ID 直接丢弃
	req = httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	req.Header.Set("X-Request-Id", "bad id with spaces\r\ninjected: 1")
	rec, _ = a.do(t, req)
	assert.Empty(t, rec.Header().Get("X-Request-Id"))

	// 两次请求 traceId 不同
	_, first := a.do(t, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
	_, second := a.do(t, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
	assert.NotEqual(t, first.TraceID, second.TraceID)
}

// readonlyToken 落一个绑定内置 readonly 角色的用户并签发 accessToken，
// 用于验证「已登录但缺权限」这条分支。
func (a *app) readonlyToken(t *testing.T) string {
	t.Helper()
	now := time.Now().UTC()
	viewer := model.SysUser{
		Base:         model.Base{ID: 80001, CreatedAt: now, UpdatedAt: now, TenantID: rbac.DefaultTenantID},
		Username:     "viewer",
		Nickname:     "只读访客",
		PasswordHash: "$2a$04$placeholder",
		Status:       model.UserStatusActive,
		Lang:         "zh-CN",
	}
	require.NoError(t, a.db.Create(&viewer).Error)
	require.NoError(t, a.db.Create(&model.SysUserRole{
		Relation: model.Relation{ID: 80002, CreatedAt: now, TenantID: rbac.DefaultTenantID},
		UserID:   viewer.ID,
		RoleID:   rbac.RoleIDReadonly,
	}).Error)

	jti, err := security.NewJTI()
	require.NoError(t, err)
	token, _, err := a.tokens.Sign(viewer.ID, viewer.TenantID, 80003, jti,
		[]string{model.RoleReadonly}, viewer.CreatedAt, now)
	require.NoError(t, err)
	return token
}

func TestRequirePermission(t *testing.T) {
	a := newApp(t)

	// 超管持通配权限，直接放行
	superToken, _ := a.login(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/test/perm", nil)
	req.Header.Set("Authorization", "Bearer "+superToken)
	rec, env := a.do(t, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Zero(t, env.Code)

	// 只读访客缺少 user:user:create
	req = httptest.NewRequest(http.MethodGet, "/api/v1/test/perm", nil)
	req.Header.Set("Authorization", "Bearer "+a.readonlyToken(t))
	rec, env = a.do(t, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, errs.CodeForbidden, env.Code)
}

func TestStaticSPAFallback(t *testing.T) {
	static := fstest.MapFS{
		"index.html":    {Data: []byte("<!doctype html><title>nova</title>")},
		"assets/app.js": {Data: []byte("console.log('nova')")},
		"favicon.ico":   {Data: []byte("icon")},
	}
	engine, err := v1.NewEngine(v1.Options{StaticFS: static})
	require.NoError(t, err)

	// 命中真实文件
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "console.log")

	// history 路由回落 index.html
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/dashboard/detail", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "<!doctype html>")

	// API 前缀不回落，仍返回信封
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/ghost", nil))
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":100003`)
}

func TestUnknownRouteAndMethod(t *testing.T) {
	a := newApp(t)

	rec, env := a.do(t, httptest.NewRequest(http.MethodGet, "/api/v1/does-not-exist", nil))
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, errs.CodeNotFound, env.Code)

	rec, env = a.do(t, httptest.NewRequest(http.MethodGet, "/api/v1/auth/login", nil))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, errs.CodeUnsupported, env.Code)

	// 未装配静态资源时，非 API 路径也返回信封而不是 gin 默认 404 文本
	rec, env = a.do(t, httptest.NewRequest(http.MethodGet, "/dashboard", nil))
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, errs.CodeNotFound, env.Code)
}
