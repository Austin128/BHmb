package site_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/novapanel/novapanel/internal/model"
	"github.com/novapanel/novapanel/internal/pkg/config"
	"github.com/novapanel/novapanel/internal/pkg/errs"
	"github.com/novapanel/novapanel/internal/pkg/idgen"
	"github.com/novapanel/novapanel/internal/repository"
	"github.com/novapanel/novapanel/internal/repository/migrate"
	"github.com/novapanel/novapanel/internal/service/site"
	"github.com/novapanel/novapanel/internal/service/webserver"
	"github.com/novapanel/novapanel/migrations"
)

// fakeRunner 替换真实 nginx 调用：本地开发机与 CI 上没有 Nginx，
// 但配置校验失败、reload 失败这些关键分支必须被覆盖。
type fakeRunner struct {
	testErr   error
	reloadErr error

	testCalls   int
	reloadCalls int
	lastArgs    []string
}

func (f *fakeRunner) Run(_ context.Context, _ string, args ...string) (webserver.Result, error) {
	f.lastArgs = args
	switch {
	case len(args) > 0 && args[0] == "-v":
		return webserver.Result{Stderr: "nginx version: nginx/1.24.0"}, nil
	case len(args) > 0 && args[0] == "-T":
		// 模拟主配置已 include vhost 目录。
		return webserver.Result{Stdout: "include /tmp/vhost/*.conf;"}, nil
	case len(args) > 0 && args[0] == "-t":
		f.testCalls++
		if f.testErr != nil {
			return webserver.Result{Stderr: "nginx: [emerg] invalid directive"}, f.testErr
		}
		return webserver.Result{Stderr: "syntax is ok\ntest is successful"}, nil
	case len(args) > 1 && args[0] == "-s" && args[1] == "reload":
		f.reloadCalls++
		return webserver.Result{}, f.reloadErr
	}
	return webserver.Result{}, nil
}

type env struct {
	svc    *site.Service
	runner *fakeRunner
	cfg    config.WebsiteConfig
	actor  site.Actor
	db     *gorm.DB
}

// mutateSite 直改数据库里的站点行，用于模拟「记录被篡改」类边界场景（如日志路径越界）。
func (e *env) mutateSite(t *testing.T, id int64, fn func(*model.WebSite)) {
	t.Helper()
	var row model.WebSite
	require.NoError(t, e.db.First(&row, id).Error)
	fn(&row)
	require.NoError(t, e.db.Save(&row).Error)
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	base := t.TempDir()

	db, err := repository.Open(&config.DatabaseConfig{
		Driver:          repository.DriverSQLite,
		Path:            filepath.Join(base, "nova.db"),
		MaxOpenConns:    4,
		MaxIdleConns:    2,
		ConnMaxLifetime: time.Hour,
	}, 200*time.Millisecond)
	require.NoError(t, err)
	t.Cleanup(func() { _ = repository.Close(db) })

	_, err = migrate.New(db, migrations.FS, repository.Dialect(db)).Up(ctx)
	require.NoError(t, err)

	// 用一个可执行的空文件冒充 nginx，让探测逻辑走通；命令输出由 fakeRunner 提供。
	bin := filepath.Join(base, "nginx")
	require.NoError(t, os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755))

	cfg := config.WebsiteConfig{
		Enabled:    true,
		ServerType: "nginx",
		NginxBin:   bin,
		VhostDir:   filepath.Join(base, "vhost"),
		WwwRoot:    filepath.Join(base, "wwwroot"),
		LogDir:     filepath.Join(base, "wwwlogs"),
		BackupKeep: 20,
	}
	runner := &fakeRunner{}
	mgr := webserver.NewManager(cfg, runner)
	renderer, err := site.NewRenderer("")
	require.NoError(t, err)

	gen, err := idgen.NewGenerator(1)
	require.NoError(t, err)
	repo := repository.NewSiteRepository(db, gen.NextID)

	svc := site.NewService(repo, mgr, renderer, cfg, 34567, []string{base}, gen.NextID)
	return &env{svc: svc, runner: runner, cfg: cfg, actor: site.Actor{UserID: 1, TenantID: 1}, db: db}
}

func staticReq(name, domain string) *model.CreateSiteRequest {
	return &model.CreateSiteRequest{
		Name:              name,
		Type:              model.SiteTypeStatic,
		Domains:           []model.SiteDomainDTO{{Domain: domain, Port: 80}},
		CreateDefaultPage: true,
		AccessLogEnabled:  true,
		ErrorLogEnabled:   true,
	}
}

func TestCreateStaticSiteDeploysConfig(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	detail, err := e.svc.Create(ctx, e.actor, staticReq("blog", "blog.example.com"))
	require.NoError(t, err)

	assert.Equal(t, model.SiteStatusRunning, detail.Status)
	assert.Equal(t, "blog.example.com", detail.PrimaryDomain)
	assert.Equal(t, model.DeployStatusDeployed, detail.DeployStatus)
	assert.Equal(t, 1, detail.ConfigVersion)
	assert.Equal(t, filepath.Join(e.cfg.WwwRoot, "blog"), detail.RootPath)
	assert.True(t, detail.RootExists)

	vhost := filepath.Join(e.cfg.VhostDir, "blog.conf")
	content, err := os.ReadFile(vhost)
	require.NoError(t, err)
	assert.Contains(t, string(content), "server_name blog.example.com;")
	assert.Contains(t, string(content), "root "+detail.RootPath+";")
	assert.Contains(t, string(content), "index index.html index.htm;")
	assert.Contains(t, string(content), "listen 80;")

	// 默认首页应存在，便于创建后立刻访问验证。
	_, err = os.Stat(filepath.Join(detail.RootPath, "index.html"))
	require.NoError(t, err)

	assert.Equal(t, 1, e.runner.testCalls, "下发必须执行一次配置校验")
	assert.Equal(t, 1, e.runner.reloadCalls, "校验通过后必须重载服务")
}

func TestCreateRejectsDuplicateDomain(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	_, err := e.svc.Create(ctx, e.actor, staticReq("site-a", "same.example.com"))
	require.NoError(t, err)

	_, err = e.svc.Create(ctx, e.actor, staticReq("site-b", "same.example.com"))
	require.Error(t, err)
	assert.Equal(t, errs.CodeDomainExists, errs.Code(err))
}

func TestCreateRejectsDuplicateName(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	_, err := e.svc.Create(ctx, e.actor, staticReq("dup", "one.example.com"))
	require.NoError(t, err)

	_, err = e.svc.Create(ctx, e.actor, staticReq("dup", "two.example.com"))
	require.Error(t, err)
	assert.Equal(t, errs.CodeConflict, errs.Code(err))
}

func TestCreateRollsBackWhenNginxTestFails(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.runner.testErr = os.ErrInvalid

	_, err := e.svc.Create(ctx, e.actor, staticReq("bad", "bad.example.com"))
	require.Error(t, err)
	assert.Equal(t, errs.CodeNginxTestFailed, errs.Code(err))

	// 校验失败必须回滚：新站点不应留下 vhost 文件，也不应触发 reload。
	_, statErr := os.Stat(filepath.Join(e.cfg.VhostDir, "bad.conf"))
	assert.True(t, os.IsNotExist(statErr), "校验失败后配置文件应被删除")
	assert.Zero(t, e.runner.reloadCalls, "校验失败不得重载服务")

	// 站点记录保留为 error 状态，并带上失败原因，供用户修复后重建。
	list, err := e.svc.List(ctx, e.actor, "", 1, 20)
	require.NoError(t, err)
	require.Len(t, list.Items, 1)
	assert.Equal(t, model.SiteStatusError, list.Items[0].Status)
	assert.Equal(t, model.DeployStatusFailed, list.Items[0].DeployStatus)
	assert.Contains(t, list.Items[0].DeployError, "invalid directive")
}

func TestRebuildRecoversFailedSite(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.runner.testErr = os.ErrInvalid

	_, err := e.svc.Create(ctx, e.actor, staticReq("retry", "retry.example.com"))
	require.Error(t, err)

	list, err := e.svc.List(ctx, e.actor, "", 1, 20)
	require.NoError(t, err)
	require.Len(t, list.Items, 1)
	id := list.Items[0].ID

	e.runner.testErr = nil
	detail, err := e.svc.Rebuild(ctx, e.actor, id)
	require.NoError(t, err)
	assert.Equal(t, model.SiteStatusRunning, detail.Status)
	assert.Equal(t, model.DeployStatusDeployed, detail.DeployStatus)
	// 版本只增不改：第一次失败占用 v1，重建为 v2。
	assert.Equal(t, 2, detail.ConfigVersion)
}

func TestUpdateKeepsPreviousConfigWhenValidationFails(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	detail, err := e.svc.Create(ctx, e.actor, staticReq("keep", "keep.example.com"))
	require.NoError(t, err)
	vhost := filepath.Join(e.cfg.VhostDir, "keep.conf")
	before, err := os.ReadFile(vhost)
	require.NoError(t, err)

	e.runner.testErr = os.ErrInvalid
	_, err = e.svc.Update(ctx, e.actor, detail.ID, &model.UpdateSiteRequest{
		Domains: []model.SiteDomainDTO{{Domain: "changed.example.com", Port: 80}},
	})
	require.Error(t, err)

	after, err := os.ReadFile(vhost)
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after), "校验失败必须还原到上一版本配置")
}

func TestProxySiteRendersProxyPass(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	detail, err := e.svc.Create(ctx, e.actor, &model.CreateSiteRequest{
		Name:           "api",
		Type:           model.SiteTypeProxy,
		Domains:        []model.SiteDomainDTO{{Domain: "api.example.com", Port: 80}},
		ProxyTarget:    "http://127.0.0.1:3000",
		ProxyWebSocket: true,
	})
	require.NoError(t, err)
	assert.Equal(t, "http://127.0.0.1:3000", detail.ProxyTarget)

	content, err := os.ReadFile(filepath.Join(e.cfg.VhostDir, "api.conf"))
	require.NoError(t, err)
	assert.Contains(t, string(content), "proxy_pass http://127.0.0.1:3000;")
	assert.Contains(t, string(content), "proxy_set_header Upgrade $http_upgrade;")
	assert.Contains(t, string(content), "proxy_set_header Host $host;")
}

func TestProxyRejectsPanelSelf(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	_, err := e.svc.Create(ctx, e.actor, &model.CreateSiteRequest{
		Name:        "loop",
		Type:        model.SiteTypeProxy,
		Domains:     []model.SiteDomainDTO{{Domain: "loop.example.com", Port: 80}},
		ProxyTarget: "http://127.0.0.1:34567",
	})
	require.Error(t, err)
	assert.Equal(t, errs.CodeInvalidParam, errs.Code(err))
	assert.Contains(t, errs.Message(err), "面板自身")
}

func TestSiteCannotUsePanelPort(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	req := staticReq("panelport", "panel.example.com")
	req.Domains[0].Port = 34567
	_, err := e.svc.Create(ctx, e.actor, req)
	require.Error(t, err)
	assert.Equal(t, errs.CodePortOccupied, errs.Code(err))
}

func TestStopAndStartSite(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	detail, err := e.svc.Create(ctx, e.actor, staticReq("toggle", "toggle.example.com"))
	require.NoError(t, err)
	vhost := filepath.Join(e.cfg.VhostDir, "toggle.conf")

	stopped, err := e.svc.ChangeStatus(ctx, e.actor, detail.ID, model.SiteStatusStopped)
	require.NoError(t, err)
	assert.Equal(t, model.SiteStatusStopped, stopped.Status)
	_, statErr := os.Stat(vhost)
	assert.True(t, os.IsNotExist(statErr), "停用后站点配置应从 vhost 目录移除")

	running, err := e.svc.ChangeStatus(ctx, e.actor, detail.ID, model.SiteStatusRunning)
	require.NoError(t, err)
	assert.Equal(t, model.SiteStatusRunning, running.Status)
	_, statErr = os.Stat(vhost)
	require.NoError(t, statErr)
}

func TestDeleteMovesRootToRecycleBin(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	detail, err := e.svc.Create(ctx, e.actor, staticReq("gone", "gone.example.com"))
	require.NoError(t, err)

	require.NoError(t, e.svc.Delete(ctx, e.actor, detail.ID, true))

	_, statErr := os.Stat(filepath.Join(e.cfg.VhostDir, "gone.conf"))
	assert.True(t, os.IsNotExist(statErr))
	_, statErr = os.Stat(detail.RootPath)
	assert.True(t, os.IsNotExist(statErr), "站点目录应被移走而不是留在原处")

	entries, err := os.ReadDir(filepath.Join(e.cfg.WwwRoot, ".recycle"))
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.True(t, strings.HasPrefix(entries[0].Name(), "gone-"))

	// 删除后同名同域名站点可以再次创建。
	_, err = e.svc.Create(ctx, e.actor, staticReq("gone", "gone.example.com"))
	require.NoError(t, err)
}

func TestSaveConfigRejectsStaleVersion(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	detail, err := e.svc.Create(ctx, e.actor, staticReq("cfg", "cfg.example.com"))
	require.NoError(t, err)

	_, err = e.svc.SaveConfig(ctx, e.actor, detail.ID, &model.SaveSiteConfigRequest{
		Content: "server { listen 80; server_name cfg.example.com; }",
		Version: 99,
	})
	require.Error(t, err)
	assert.Equal(t, errs.CodeConflict, errs.Code(err))
}

func TestSaveConfigAndRollback(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	detail, err := e.svc.Create(ctx, e.actor, staticReq("edit", "edit.example.com"))
	require.NoError(t, err)
	vhost := filepath.Join(e.cfg.VhostDir, "edit.conf")
	original, err := os.ReadFile(vhost)
	require.NoError(t, err)

	manual := "server { listen 80; server_name edit.example.com; root /tmp; }"
	saved, err := e.svc.SaveConfig(ctx, e.actor, detail.ID, &model.SaveSiteConfigRequest{
		Content: manual,
		Version: detail.ConfigVersion,
	})
	require.NoError(t, err)
	assert.Equal(t, 2, saved.Version)
	assert.Contains(t, saved.Content, "root /tmp;")

	onDisk, err := os.ReadFile(vhost)
	require.NoError(t, err)
	assert.Contains(t, string(onDisk), "root /tmp;")

	rolled, err := e.svc.Rollback(ctx, e.actor, detail.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, 3, rolled.Version, "回滚写入新版本而不是改历史")
	restored, err := os.ReadFile(vhost)
	require.NoError(t, err)
	assert.Equal(t, string(original), string(restored))
}

func TestListFiltersByKeywordAndDomain(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	_, err := e.svc.Create(ctx, e.actor, staticReq("alpha", "alpha.example.com"))
	require.NoError(t, err)
	_, err = e.svc.Create(ctx, e.actor, staticReq("beta", "beta.example.org"))
	require.NoError(t, err)

	byName, err := e.svc.List(ctx, e.actor, "alph", 1, 20)
	require.NoError(t, err)
	require.Len(t, byName.Items, 1)
	assert.Equal(t, "alpha", byName.Items[0].Name)

	byDomain, err := e.svc.List(ctx, e.actor, "example.org", 1, 20)
	require.NoError(t, err)
	require.Len(t, byDomain.Items, 1)
	assert.Equal(t, "beta", byDomain.Items[0].Name)
}

func TestTenantIsolation(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	detail, err := e.svc.Create(ctx, e.actor, staticReq("mine", "mine.example.com"))
	require.NoError(t, err)

	other := site.Actor{UserID: 2, TenantID: 2}
	_, err = e.svc.Get(ctx, other, detail.ID)
	require.Error(t, err)
	assert.Equal(t, errs.CodeSiteNotFound, errs.Code(err))

	list, err := e.svc.List(ctx, other, "", 1, 20)
	require.NoError(t, err)
	assert.Empty(t, list.Items)
}

func TestEnvReportsPrerequisites(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	envInfo, err := e.svc.Env(ctx, e.actor)
	require.NoError(t, err)
	assert.True(t, envInfo.Installed)
	assert.Equal(t, "1.24.0", envInfo.Version)
	assert.Contains(t, envInfo.SupportedTypes, model.SiteTypeStatic)
	assert.Contains(t, envInfo.SupportedTypes, model.SiteTypeProxy)
	assert.Zero(t, envInfo.SiteCount)
}

func TestCreateRejectedWithoutWebServer(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	// 指向不存在的可执行文件，模拟宿主机未安装 Nginx。
	cfg := e.cfg
	cfg.NginxBin = filepath.Join(t.TempDir(), "missing-nginx")
	mgr := webserver.NewManager(cfg, e.runner)
	renderer, err := site.NewRenderer("")
	require.NoError(t, err)
	gen, err := idgen.NewGenerator(2)
	require.NoError(t, err)

	db, err := repository.Open(&config.DatabaseConfig{
		Driver: repository.DriverSQLite, Path: filepath.Join(t.TempDir(), "n.db"),
		MaxOpenConns: 2, MaxIdleConns: 1, ConnMaxLifetime: time.Hour,
	}, 200*time.Millisecond)
	require.NoError(t, err)
	t.Cleanup(func() { _ = repository.Close(db) })
	_, err = migrate.New(db, migrations.FS, repository.Dialect(db)).Up(ctx)
	require.NoError(t, err)

	svc := site.NewService(repository.NewSiteRepository(db, gen.NextID), mgr, renderer, cfg, 34567, nil, gen.NextID)

	_, err = svc.Create(ctx, e.actor, staticReq("nows", "nows.example.com"))
	require.Error(t, err)
	assert.Equal(t, errs.CodeUnsupported, errs.Code(err))
	assert.Contains(t, errs.Message(err), "未检测到 Nginx")

	// 前置条件缺失时不得留下任何站点记录。
	list, err := svc.List(ctx, e.actor, "", 1, 20)
	require.NoError(t, err)
	assert.Empty(t, list.Items)
}

func TestRootMustStayInsideAllowedRoots(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	req := staticReq("escape", "escape.example.com")
	req.Root = "/etc/nginx"
	_, err := e.svc.Create(ctx, e.actor, req)
	require.Error(t, err)
	assert.Equal(t, errs.CodeInvalidParam, errs.Code(err))
}

func TestExpireMustBeFuture(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	req := staticReq("expired", "expired.example.com")
	req.ExpireAt = time.Now().Add(-time.Hour).UnixMilli()
	_, err := e.svc.Create(ctx, e.actor, req)
	require.Error(t, err)
	assert.Equal(t, errs.CodeInvalidParam, errs.Code(err))
}
