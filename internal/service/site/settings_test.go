package site_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/novapanel/novapanel/internal/model"
	"github.com/novapanel/novapanel/internal/pkg/errs"
	"github.com/novapanel/novapanel/internal/service/site"
)

// vhostOf 读取站点的 vhost 正文。
func vhostOf(t *testing.T, e *env, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(e.cfg.VhostDir, name+".conf"))
	require.NoError(t, err)
	return string(b)
}

func TestSaveSettingsRendersAdvancedDirectives(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	detail, err := e.svc.Create(ctx, e.actor, staticReq("adv", "adv.example.com"))
	require.NoError(t, err)

	out, err := e.svc.SaveSettings(ctx, e.actor, detail.ID, &model.SaveSiteSettingsRequest{
		IndexFiles:   []string{"index.html", "default.html"},
		AutoIndex:    true,
		CacheProfile: model.CacheProfileDay,
		AntiLeech: model.AntiLeechSetting{
			Enabled:           true,
			Extensions:        []string{"png", "JPG"},
			AllowedReferers:   []string{"*.example.com"},
			AllowEmptyReferer: true,
			ReturnCode:        404,
		},
		AccessControl: model.AccessControlSetting{
			Enabled:        true,
			AllowIPs:       []string{"10.0.0.0/8"},
			DenyIPs:        []string{"1.2.3.4"},
			DenyUserAgents: []string{"BadBot"},
		},
		Redirects: []model.RedirectRule{{
			Enabled: true, Path: "/old", Target: "https://new.example.com",
			Code: 301, KeepPath: true, Remark: "旧站迁移",
		}},
		RateLimit:        model.RateLimitSetting{Enabled: true, BandwidthKB: 512, BurstKB: 1024},
		AccessLogEnabled: true,
		ErrorLogEnabled:  true,
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"index.html", "default.html"}, out.IndexFiles)
	assert.Equal(t, 404, out.AntiLeech.ReturnCode)

	cfg := vhostOf(t, e, "adv")
	assert.Contains(t, cfg, "autoindex on;")
	assert.Contains(t, cfg, "# 旧站迁移")
	assert.Contains(t, cfg, "rewrite ^/old(.*)$ https://new.example.com$1? permanent;")
	assert.Contains(t, cfg, "limit_rate 512k;")
	assert.Contains(t, cfg, "limit_rate_after 1024k;")
	assert.Contains(t, cfg, `if ($http_user_agent ~* "BadBot")`)
	assert.Contains(t, cfg, "deny 1.2.3.4;")
	assert.Contains(t, cfg, "allow 10.0.0.0/8;")
	assert.Contains(t, cfg, "deny all;", "配了白名单必须补 deny all，否则放行名单没有意义")
	assert.Contains(t, cfg, "valid_referers none blocked server_names *.example.com;")
	assert.Contains(t, cfg, "return 404;")
	assert.Contains(t, cfg, "expires 1d;")

	// 防盗链与缓存都用扩展名正则 location，重叠会让后声明的那块永不生效，
	// 因此缓存 location 不能再包含已被防盗链接管的 png/jpg。
	assert.Contains(t, cfg, `location ~* \.(jpg|png)$ {`)
	assert.NotContains(t, cfg, `location ~* \.(js|css|png`)
	assert.Contains(t, cfg, "js|css|")
}

func TestSaveSettingsKeepsQueryStringWhenAsked(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	detail, err := e.svc.Create(ctx, e.actor, staticReq("qkeep", "q.example.com"))
	require.NoError(t, err)

	_, err = e.svc.SaveSettings(ctx, e.actor, detail.ID, &model.SaveSiteSettingsRequest{
		Redirects: []model.RedirectRule{{
			Enabled: true, Path: "/go", Target: "https://t.example.com", Code: 302, KeepQuery: true,
		}},
	})
	require.NoError(t, err)

	cfg := vhostOf(t, e, "qkeep")
	// KeepQuery 为真时不补问号，nginx 会自动带上原查询串。
	assert.Contains(t, cfg, "rewrite ^/go(?:/.*)?$ https://t.example.com redirect;")
}

func TestSaveSettingsRunPathMustExist(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	detail, err := e.svc.Create(ctx, e.actor, staticReq("run", "run.example.com"))
	require.NoError(t, err)

	_, err = e.svc.SaveSettings(ctx, e.actor, detail.ID,
		&model.SaveSiteSettingsRequest{RunPath: "/public"})
	require.Error(t, err)
	assert.Equal(t, errs.CodeInvalidParam, errs.Code(err))

	require.NoError(t, os.MkdirAll(filepath.Join(detail.RootPath, "public"), 0o755))
	_, err = e.svc.SaveSettings(ctx, e.actor, detail.ID,
		&model.SaveSiteSettingsRequest{RunPath: "public/"})
	require.NoError(t, err)

	cfg := vhostOf(t, e, "run")
	assert.Contains(t, cfg, "root "+filepath.Join(detail.RootPath, "public")+";")
}

func TestSaveSettingsRejectsRunPathEscape(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	detail, err := e.svc.Create(ctx, e.actor, staticReq("esc", "esc.example.com"))
	require.NoError(t, err)

	for _, bad := range []string{"/../../etc", "/pub lic", "/a/b/c/d/e/f/g/h/i"} {
		_, err = e.svc.SaveSettings(ctx, e.actor, detail.ID,
			&model.SaveSiteSettingsRequest{RunPath: bad})
		require.Errorf(t, err, "运行目录 %q 必须被拒绝", bad)
		assert.Equal(t, errs.CodeInvalidParam, errs.Code(err))
	}
}

func TestSaveSettingsRejectsInvalidInput(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	detail, err := e.svc.Create(ctx, e.actor, staticReq("bad-input", "badin.example.com"))
	require.NoError(t, err)

	cases := map[string]*model.SaveSiteSettingsRequest{
		"非法缓存档位": {CacheProfile: "forever"},
		"非法 IP": {AccessControl: model.AccessControlSetting{
			Enabled: true, DenyIPs: []string{"999.1.1.1"}}},
		"非法 CIDR": {AccessControl: model.AccessControlSetting{
			Enabled: true, AllowIPs: []string{"10.0.0.0/64"}}},
		"空规则的访问限制": {AccessControl: model.AccessControlSetting{Enabled: true}},
		"注入式 UA": {AccessControl: model.AccessControlSetting{
			Enabled: true, DenyUserAgents: []string{`bot") { return 403; } if ($x ~ "`}}},
		"防盗链缺扩展名": {AntiLeech: model.AntiLeechSetting{Enabled: true}},
		"防盗链返回码越界": {AntiLeech: model.AntiLeechSetting{
			Enabled: true, Extensions: []string{"png"}, ReturnCode: 500}},
		"非 http 重定向": {Redirects: []model.RedirectRule{{
			Enabled: true, Path: "/x", Target: "javascript:alert(1)"}}},
		"重复重定向路径": {Redirects: []model.RedirectRule{
			{Enabled: true, Path: "/x", Target: "https://a.example.com"},
			{Enabled: true, Path: "/x/", Target: "https://b.example.com"},
		}},
		"限速为零": {RateLimit: model.RateLimitSetting{Enabled: true}},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := e.svc.SaveSettings(ctx, e.actor, detail.ID, req)
			require.Error(t, err)
			assert.Equal(t, errs.CodeInvalidParam, errs.Code(err))
		})
	}
}

func TestSaveSettingsRejectsProxyWholeSiteRedirect(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	detail, err := e.svc.Create(ctx, e.actor, &model.CreateSiteRequest{
		Name:        "gw",
		Type:        model.SiteTypeProxy,
		Domains:     []model.SiteDomainDTO{{Domain: "gw.example.com", Port: 80}},
		ProxyTarget: "http://127.0.0.1:3000",
	})
	require.NoError(t, err)

	_, err = e.svc.SaveSettings(ctx, e.actor, detail.ID, &model.SaveSiteSettingsRequest{
		Redirects: []model.RedirectRule{{Enabled: true, Path: "/", Target: "https://x.example.com"}},
	})
	require.Error(t, err)
	assert.Contains(t, errs.Message(err), "反向代理")
}

func TestSaveSettingsTenantIsolation(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	detail, err := e.svc.Create(ctx, e.actor, staticReq("mine", "mine.example.com"))
	require.NoError(t, err)

	other := site.Actor{UserID: 9, TenantID: 2}
	_, err = e.svc.Settings(ctx, other, detail.ID)
	require.Error(t, err)
	assert.Equal(t, errs.CodeSiteNotFound, errs.Code(err))

	_, err = e.svc.SaveSettings(ctx, other, detail.ID, &model.SaveSiteSettingsRequest{AutoIndex: true})
	require.Error(t, err)
	assert.Equal(t, errs.CodeSiteNotFound, errs.Code(err))

	_, err = e.svc.Rewrite(ctx, other, detail.ID)
	require.Error(t, err)
	assert.Equal(t, errs.CodeSiteNotFound, errs.Code(err))

	_, err = e.svc.SaveRewrite(ctx, other, detail.ID,
		&model.SaveSiteRewriteRequest{TemplateCode: "wordpress", Enabled: true})
	require.Error(t, err)
	assert.Equal(t, errs.CodeSiteNotFound, errs.Code(err))
}

func TestRewriteTemplateLibraryPassesValidation(t *testing.T) {
	tpls := site.RewriteTemplates()
	// none 之外的模板都必须真实打包进二进制，否则规则库是空壳。
	require.Len(t, tpls, 12)
	for _, tpl := range tpls {
		if tpl.Code == model.RewriteTemplateNone {
			assert.Empty(t, tpl.Content)
			continue
		}
		assert.NotEmpty(t, tpl.Content, "模板 %s 内容为空", tpl.Code)
		assert.NoError(t, site.ValidateRewrite(tpl.Content, model.SiteTypeStatic),
			"模板 %s 未通过静态站校验", tpl.Code)
	}
}

func TestSaveRewriteWritesSnippetAndIncludesIt(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	detail, err := e.svc.Create(ctx, e.actor, staticReq("wp", "wp.example.com"))
	require.NoError(t, err)

	out, err := e.svc.SaveRewrite(ctx, e.actor, detail.ID,
		&model.SaveSiteRewriteRequest{TemplateCode: "wordpress", Enabled: true})
	require.NoError(t, err)
	assert.Equal(t, "wordpress", out.TemplateCode)
	assert.True(t, out.Enabled)

	snippet := filepath.Join(e.cfg.VhostDir, "rewrite", "wp.conf")
	assert.Equal(t, snippet, out.File)
	body, err := os.ReadFile(snippet)
	require.NoError(t, err)
	assert.Contains(t, string(body), "try_files $uri $uri/ /index.php?$args;")

	cfg := vhostOf(t, e, "wp")
	assert.Contains(t, cfg, "include "+snippet+";")
	// 伪静态自带 location /，模板不能再输出默认的 location / 造成重复声明。
	assert.NotContains(t, cfg, "try_files $uri $uri/ =404;")

	// 片段目录不能落在 vhost 根下，否则会被主配置的 *.conf 当成独立 server 加载。
	entries, err := os.ReadDir(e.cfg.VhostDir)
	require.NoError(t, err)
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		assert.Equal(t, "wp.conf", ent.Name())
	}
}

func TestSaveRewriteDisableRemovesSnippet(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	detail, err := e.svc.Create(ctx, e.actor, staticReq("off", "off.example.com"))
	require.NoError(t, err)

	_, err = e.svc.SaveRewrite(ctx, e.actor, detail.ID,
		&model.SaveSiteRewriteRequest{TemplateCode: "laravel", Enabled: true})
	require.NoError(t, err)

	out, err := e.svc.SaveRewrite(ctx, e.actor, detail.ID,
		&model.SaveSiteRewriteRequest{TemplateCode: model.RewriteTemplateNone})
	require.NoError(t, err)
	assert.Equal(t, model.RewriteTemplateNone, out.TemplateCode)
	assert.False(t, out.Enabled)

	_, statErr := os.Stat(filepath.Join(e.cfg.VhostDir, "rewrite", "off.conf"))
	assert.True(t, os.IsNotExist(statErr), "关闭伪静态后片段文件应被删除")

	cfg := vhostOf(t, e, "off")
	assert.NotContains(t, cfg, "include ")
	assert.Contains(t, cfg, "try_files $uri $uri/ =404;", "关闭伪静态后应恢复默认 location /")
}

func TestSaveRewriteRestoresSnippetWhenNginxTestFails(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	detail, err := e.svc.Create(ctx, e.actor, staticReq("rb", "rb.example.com"))
	require.NoError(t, err)

	_, err = e.svc.SaveRewrite(ctx, e.actor, detail.ID,
		&model.SaveSiteRewriteRequest{TemplateCode: "wordpress", Enabled: true})
	require.NoError(t, err)

	snippet := filepath.Join(e.cfg.VhostDir, "rewrite", "rb.conf")
	before, err := os.ReadFile(snippet)
	require.NoError(t, err)
	vhostBefore := vhostOf(t, e, "rb")

	e.runner.testErr = os.ErrInvalid
	_, err = e.svc.SaveRewrite(ctx, e.actor, detail.ID,
		&model.SaveSiteRewriteRequest{TemplateCode: "laravel", Enabled: true})
	require.Error(t, err)
	assert.Equal(t, errs.CodeNginxTestFailed, errs.Code(err))

	after, err := os.ReadFile(snippet)
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after),
		"下发失败后片段必须还原，否则磁盘上的规则与线上配置不一致")
	assert.Equal(t, vhostBefore, vhostOf(t, e, "rb"))

	// 失败语义：数据库保留用户刚提交的规则（不丢表单），线上仍是旧规则，
	// 站点被标记为失败；用户修好 nginx 后点「重建」即可把新规则真正下发。
	cur, err := e.svc.Rewrite(ctx, e.actor, detail.ID)
	require.NoError(t, err)
	assert.Equal(t, "laravel", cur.TemplateCode)

	failed, err := e.svc.Get(ctx, e.actor, detail.ID)
	require.NoError(t, err)
	assert.Equal(t, model.DeployStatusFailed, failed.DeployStatus)
	assert.NotEmpty(t, failed.DeployError)

	e.runner.testErr = nil
	fixed, err := e.svc.Rebuild(ctx, e.actor, detail.ID)
	require.NoError(t, err)
	assert.Equal(t, model.DeployStatusDeployed, fixed.DeployStatus)
	rebuilt, err := os.ReadFile(snippet)
	require.NoError(t, err)
	assert.NotEqual(t, string(before), string(rebuilt), "重建应把数据库中的新规则同步到磁盘")
	assert.Contains(t, vhostOf(t, e, "rb"), "include "+cur.File+";")
}

func TestSaveRewriteRejectsUnsafeContent(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	detail, err := e.svc.Create(ctx, e.actor, staticReq("unsafe", "unsafe.example.com"))
	require.NoError(t, err)

	cases := map[string]string{
		"改文档根":  "root /etc;",
		"读任意文件": "alias /etc/passwd;",
		"套嵌 server": `server {
    listen 81;
}`,
		"外部包含": "include /etc/nginx/evil.conf;",
		"执行外部程序": `location / {
    fastcgi_pass unix:/tmp/evil.sock;
}`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := e.svc.SaveRewrite(ctx, e.actor, detail.ID, &model.SaveSiteRewriteRequest{
				TemplateCode: model.RewriteTemplateCustom, Content: content, Enabled: true,
			})
			require.Error(t, err)
			assert.Equal(t, errs.CodeRewriteInvalid, errs.Code(err))
		})
	}

	_, err = e.svc.SaveRewrite(ctx, e.actor, detail.ID, &model.SaveSiteRewriteRequest{
		TemplateCode: model.RewriteTemplateCustom, Enabled: true,
	})
	require.Error(t, err)
	assert.Equal(t, errs.CodeInvalidParam, errs.Code(err), "自定义模板必须给正文")

	_, err = e.svc.SaveRewrite(ctx, e.actor, detail.ID,
		&model.SaveSiteRewriteRequest{TemplateCode: "no-such-framework", Enabled: true})
	require.Error(t, err)
	assert.Equal(t, errs.CodeInvalidParam, errs.Code(err))
}

func TestSaveRewriteProxySiteRejectsLocation(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	detail, err := e.svc.Create(ctx, e.actor, &model.CreateSiteRequest{
		Name:        "pxy",
		Type:        model.SiteTypeProxy,
		Domains:     []model.SiteDomainDTO{{Domain: "pxy.example.com", Port: 80}},
		ProxyTarget: "http://127.0.0.1:3000",
	})
	require.NoError(t, err)

	// 反代模板自带 location /，伪静态再声明 location 会与之冲突。
	_, err = e.svc.SaveRewrite(ctx, e.actor, detail.ID, &model.SaveSiteRewriteRequest{
		TemplateCode: model.RewriteTemplateCustom,
		Content:      "location /api { return 403; }",
		Enabled:      true,
	})
	require.Error(t, err)
	assert.Equal(t, errs.CodeRewriteInvalid, errs.Code(err))

	// 纯 rewrite 语句可以用。
	out, err := e.svc.SaveRewrite(ctx, e.actor, detail.ID, &model.SaveSiteRewriteRequest{
		TemplateCode: model.RewriteTemplateCustom,
		Content:      "rewrite ^/v1/(.*)$ /$1 break;",
		Enabled:      true,
	})
	require.NoError(t, err)
	assert.True(t, out.Enabled)
	assert.Contains(t, vhostOf(t, e, "pxy"), "include "+out.File+";")
}

func TestStoppedSiteSavesSettingsWithoutDeploy(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	detail, err := e.svc.Create(ctx, e.actor, staticReq("idle", "idle.example.com"))
	require.NoError(t, err)
	_, err = e.svc.ChangeStatus(ctx, e.actor, detail.ID, model.SiteStatusStopped)
	require.NoError(t, err)

	testCalls := e.runner.testCalls
	out, err := e.svc.SaveSettings(ctx, e.actor, detail.ID,
		&model.SaveSiteSettingsRequest{AutoIndex: true, CacheProfile: model.CacheProfileHour})
	require.NoError(t, err)
	assert.True(t, out.AutoIndex)
	assert.Equal(t, testCalls, e.runner.testCalls, "停用站点不应触发配置下发")

	_, err = e.svc.SaveRewrite(ctx, e.actor, detail.ID,
		&model.SaveSiteRewriteRequest{TemplateCode: "spa", Enabled: true})
	require.NoError(t, err)
	assert.Equal(t, testCalls, e.runner.testCalls)
	_, statErr := os.Stat(filepath.Join(e.cfg.VhostDir, "rewrite", "idle.conf"))
	assert.True(t, os.IsNotExist(statErr), "停用站点不应把片段写到线上目录")

	// 启用后按数据库状态补齐片段与 include。
	_, err = e.svc.ChangeStatus(ctx, e.actor, detail.ID, model.SiteStatusRunning)
	require.NoError(t, err)
	_, statErr = os.Stat(filepath.Join(e.cfg.VhostDir, "rewrite", "idle.conf"))
	require.NoError(t, statErr)
	cfg := vhostOf(t, e, "idle")
	assert.Contains(t, cfg, "autoindex on;")
	assert.Contains(t, cfg, "expires 1h;")
	assert.Contains(t, cfg, "include ")
}

func TestDeleteSiteRemovesRewriteAndAllowsRecreate(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	detail, err := e.svc.Create(ctx, e.actor, staticReq("cycle", "cycle.example.com"))
	require.NoError(t, err)
	_, err = e.svc.SaveRewrite(ctx, e.actor, detail.ID,
		&model.SaveSiteRewriteRequest{TemplateCode: "typecho", Enabled: true})
	require.NoError(t, err)

	require.NoError(t, e.svc.Delete(ctx, e.actor, detail.ID, false))

	again, err := e.svc.Create(ctx, e.actor, staticReq("cycle", "cycle.example.com"))
	require.NoError(t, err)
	// 旧规则不能被新站点继承：伪静态随站点硬删。
	out, err := e.svc.Rewrite(ctx, e.actor, again.ID)
	require.NoError(t, err)
	assert.Equal(t, model.RewriteTemplateNone, out.TemplateCode)
	assert.Empty(t, out.Content)
}
