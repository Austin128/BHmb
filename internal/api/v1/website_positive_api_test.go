package v1_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/novapanel/novapanel/internal/model"
	"github.com/novapanel/novapanel/internal/pkg/errs"
	"github.com/novapanel/novapanel/internal/rbac"
	"github.com/novapanel/novapanel/internal/security"
	"github.com/novapanel/novapanel/internal/service/webserver"
)

// okRunner 扮演一台装好 Nginx 且主配置正常的机器：-v 报版本、-T 含托管目录、-t 通过、reload 成功。
type okRunner struct {
	mu       sync.Mutex
	vhostDir string
	// testErr 置真后 nginx -t 开始失败，用于覆盖下发失败这条路径。
	testErr bool
}

func (r *okRunner) failTest(v bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.testErr = v
}

func (r *okRunner) Run(_ context.Context, _ string, args ...string) (webserver.Result, error) {
	r.mu.Lock()
	failing := r.testErr
	r.mu.Unlock()

	switch {
	case len(args) == 1 && args[0] == "-v":
		return webserver.Result{Stderr: "nginx version: nginx/1.24.0"}, nil
	case len(args) == 1 && args[0] == "-T":
		return webserver.Result{Stdout: "http { include " + r.vhostDir + "/*.conf; }"}, nil
	case len(args) == 1 && args[0] == "-t":
		if failing {
			return webserver.Result{Stderr: "nginx: [emerg] invalid directive"}, assert.AnError
		}
		return webserver.Result{Stderr: "test is successful"}, nil
	default:
		return webserver.Result{Stdout: "ok"}, nil
	}
}

// newWebsiteApp 装配一个「Web 服务可用」的应用，用于覆盖创建成功等正向路径。
func newWebsiteApp(t *testing.T) (*app, *okRunner) {
	t.Helper()
	runner := &okRunner{}
	a := newApp(t, withWebServer(runner))
	runner.mu.Lock()
	runner.vhostDir = a.websiteVhost
	runner.mu.Unlock()
	return a, runner
}

func (a *app) req(t *testing.T, token, method, path, body string) (*httptest.ResponseRecorder, envelope) {
	t.Helper()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = jsonReq(t, method, path, body)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return a.do(t, req)
}

// tenantToken 落一个属于指定租户的管理员用户并签发 token，用于验证租户隔离。
func (a *app) tenantToken(t *testing.T, tenantID int64, username string) string {
	t.Helper()
	now := time.Now().UTC()
	id := 90000 + tenantID*10
	user := model.SysUser{
		Base:         model.Base{ID: id, CreatedAt: now, UpdatedAt: now, TenantID: tenantID},
		Username:     username,
		Nickname:     username,
		PasswordHash: "$2a$04$placeholder",
		Status:       model.UserStatusActive,
		Lang:         "zh-CN",
	}
	require.NoError(t, a.db.Create(&user).Error)
	require.NoError(t, a.db.Create(&model.SysUserRole{
		Relation: model.Relation{ID: id + 1, CreatedAt: now, TenantID: tenantID},
		UserID:   user.ID,
		RoleID:   rbac.RoleIDAdmin,
	}).Error)

	jti, err := security.NewJTI()
	require.NoError(t, err)
	token, _, err := a.tokens.Sign(user.ID, user.TenantID, id+2, jti,
		[]string{model.RoleAdmin}, user.CreatedAt, now)
	require.NoError(t, err)
	return token
}

func createStaticSite(t *testing.T, a *app, token, name, domain string) model.SiteDetail {
	t.Helper()
	rec, env := a.req(t, token, http.MethodPost, "/api/v1/website/sites",
		`{"name":"`+name+`","type":"static","domains":[{"domain":"`+domain+`","port":80}],`+
			`"createDefaultPage":true,"remark":"集成测试"}`)
	require.Equal(t, http.StatusOK, rec.Code, "创建站点失败：%s", env.Message)
	require.Zero(t, env.Code)
	var detail model.SiteDetail
	require.NoError(t, json.Unmarshal(env.Data, &detail))
	return detail
}

func TestWebsiteCreateStaticSiteDeploysRealConfig(t *testing.T) {
	a, _ := newWebsiteApp(t)
	token, _ := a.login(t)

	detail := createStaticSite(t, a, token, "shop", "shop.example.com")
	assert.Equal(t, model.SiteStatusRunning, detail.Status)
	assert.Equal(t, model.SiteTypeStatic, detail.Type)
	assert.Equal(t, 1, detail.ConfigVersion)
	assert.Equal(t, model.DeployStatusDeployed, detail.DeployStatus)
	assert.Empty(t, detail.DeployError)
	assert.Equal(t, "shop.example.com", detail.PrimaryDomain)
	assert.Equal(t, filepath.Join(a.websiteRoot, "shop"), detail.RootPath)
	assert.True(t, detail.RootExists, "创建成功后站点目录必须真实存在")
	assert.Equal(t, filepath.Join(a.websiteVhost, "shop.conf"), detail.VhostFile)

	// 真实产物：vhost 配置与默认首页。
	conf, err := os.ReadFile(detail.VhostFile)
	require.NoError(t, err)
	assert.Contains(t, string(conf), "server_name shop.example.com;")
	assert.Contains(t, string(conf), "root "+detail.RootPath+";")
	_, err = os.Stat(filepath.Join(detail.RootPath, "index.html"))
	require.NoError(t, err, "静态站应生成默认首页")

	// 列表与环境计数同步。
	rec, env := a.req(t, token, http.MethodGet, "/api/v1/website/sites?page=1&pageSize=20", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var list model.SiteListResult
	require.NoError(t, json.Unmarshal(env.Data, &list))
	require.Len(t, list.Items, 1)
	assert.EqualValues(t, 1, list.Total)

	_, env = a.req(t, token, http.MethodGet, "/api/v1/website/env", "")
	var envData struct {
		Installed bool  `json:"installed"`
		SiteCount int64 `json:"siteCount"`
	}
	require.NoError(t, json.Unmarshal(env.Data, &envData))
	assert.True(t, envData.Installed)
	assert.EqualValues(t, 1, envData.SiteCount)

	// ID 必须以字符串序列化，避免前端精度丢失。
	rec, env = a.req(t, token, http.MethodGet, "/api/v1/website/sites/"+itoa(detail.ID), "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, string(env.Data), `"id":"`+itoa(detail.ID)+`"`)
}

func itoa(id int64) string { return strconv.FormatInt(id, 10) }

func TestWebsiteCreateRejectsDuplicateDomain(t *testing.T) {
	a, _ := newWebsiteApp(t)
	token, _ := a.login(t)
	createStaticSite(t, a, token, "shop", "shop.example.com")

	rec, env := a.req(t, token, http.MethodPost, "/api/v1/website/sites",
		`{"name":"shop2","type":"static","domains":[{"domain":"shop.example.com","port":80}]}`)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Equal(t, errs.CodeDomainExists, env.Code)

	// 重名站点标识同样必须被拦下。
	rec, env = a.req(t, token, http.MethodPost, "/api/v1/website/sites",
		`{"name":"shop","type":"static","domains":[{"domain":"other.example.com","port":80}]}`)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.NotZero(t, env.Code)
}

func TestWebsiteCreateRejectsProxyToPanelItself(t *testing.T) {
	a, _ := newWebsiteApp(t)
	token, _ := a.login(t)

	rec, env := a.req(t, token, http.MethodPost, "/api/v1/website/sites",
		`{"name":"loop","type":"proxy","domains":[{"domain":"loop.example.com","port":80}],"proxyTarget":"http://127.0.0.1:34567"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, errs.CodeInvalidParam, env.Code)
	assert.Contains(t, env.Message, "面板自身", "必须说明具体原因，前端才不需要用泛化文案")

	// 失败的创建不得留下任何记录或配置文件。
	var n int64
	require.NoError(t, a.db.Model(&model.WebSite{}).Count(&n).Error)
	assert.Zero(t, n)
	_, statErr := os.Stat(filepath.Join(a.websiteVhost, "loop.conf"))
	assert.True(t, os.IsNotExist(statErr))
}

func TestWebsiteCreateProxySiteRendersUpstream(t *testing.T) {
	a, _ := newWebsiteApp(t)
	token, _ := a.login(t)

	rec, env := a.req(t, token, http.MethodPost, "/api/v1/website/sites",
		`{"name":"api","type":"proxy","domains":[{"domain":"api.example.com","port":80}],`+
			`"proxyTarget":"http://127.0.0.1:3000","proxyHost":"api.internal","proxyWebSocket":true}`)
	require.Equal(t, http.StatusOK, rec.Code, env.Message)
	var detail model.SiteDetail
	require.NoError(t, json.Unmarshal(env.Data, &detail))
	assert.Equal(t, model.SiteTypeProxy, detail.Type)
	assert.Equal(t, "http://127.0.0.1:3000", detail.ProxyTarget)
	assert.Equal(t, "api.internal", detail.ProxyHost)
	assert.True(t, detail.ProxyWebSocket)

	conf, err := os.ReadFile(detail.VhostFile)
	require.NoError(t, err)
	assert.Contains(t, string(conf), "proxy_pass http://127.0.0.1:3000;")
	assert.Contains(t, string(conf), "proxy_set_header Host api.internal;")
	assert.Contains(t, string(conf), `proxy_set_header Connection "upgrade";`)
}

func TestWebsiteDeployFailureKeepsSiteInspectable(t *testing.T) {
	a, runner := newWebsiteApp(t)
	token, _ := a.login(t)
	runner.failTest(true)

	rec, env := a.req(t, token, http.MethodPost, "/api/v1/website/sites",
		`{"name":"broken","type":"static","domains":[{"domain":"broken.example.com","port":80}]}`)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, errs.CodeNginxTestFailed, env.Code)

	// 站点记录保留为 error 状态，配置版本记录失败原因，供用户修好后重建。
	rec, env = a.req(t, token, http.MethodGet, "/api/v1/website/sites?page=1&pageSize=20", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var list model.SiteListResult
	require.NoError(t, json.Unmarshal(env.Data, &list))
	require.Len(t, list.Items, 1)
	assert.Equal(t, model.SiteStatusError, list.Items[0].Status)
	assert.Equal(t, model.DeployStatusFailed, list.Items[0].DeployStatus)
	assert.NotEmpty(t, list.Items[0].DeployError)

	// 宿主机上不应留下未通过校验的配置。
	_, statErr := os.Stat(filepath.Join(a.websiteVhost, "broken.conf"))
	assert.True(t, os.IsNotExist(statErr))

	// 修好前置问题后重建应恢复为 running。
	runner.failTest(false)
	rec, env = a.req(t, token, http.MethodPost,
		"/api/v1/website/sites/"+itoa(list.Items[0].ID)+"/rebuild", `{}`)
	require.Equal(t, http.StatusOK, rec.Code, env.Message)
	var rebuilt model.SiteDetail
	require.NoError(t, json.Unmarshal(env.Data, &rebuilt))
	assert.Equal(t, model.SiteStatusRunning, rebuilt.Status)
	assert.Equal(t, model.DeployStatusDeployed, rebuilt.DeployStatus)
}

func TestWebsiteLifecycleStopStartAndDelete(t *testing.T) {
	a, _ := newWebsiteApp(t)
	token, _ := a.login(t)
	detail := createStaticSite(t, a, token, "shop", "shop.example.com")
	id := itoa(detail.ID)

	// 停用：移除 vhost，域名不再由本站点响应。
	rec, env := a.req(t, token, http.MethodPost, "/api/v1/website/sites/"+id+"/status", `{"target":"stopped"}`)
	require.Equal(t, http.StatusOK, rec.Code, env.Message)
	var stopped model.SiteDetail
	require.NoError(t, json.Unmarshal(env.Data, &stopped))
	assert.Equal(t, model.SiteStatusStopped, stopped.Status)
	_, statErr := os.Stat(detail.VhostFile)
	assert.True(t, os.IsNotExist(statErr), "停用后不应保留生效的 vhost")

	// 启用：重新下发。
	rec, env = a.req(t, token, http.MethodPost, "/api/v1/website/sites/"+id+"/status", `{"target":"running"}`)
	require.Equal(t, http.StatusOK, rec.Code, env.Message)
	var running model.SiteDetail
	require.NoError(t, json.Unmarshal(env.Data, &running))
	assert.Equal(t, model.SiteStatusRunning, running.Status)
	_, err := os.Stat(detail.VhostFile)
	require.NoError(t, err)

	// 非法目标状态被拒绝。
	rec, env = a.req(t, token, http.MethodPost, "/api/v1/website/sites/"+id+"/status", `{"target":"deleted"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, errs.CodeInvalidParam, env.Code)

	// 删除并回收根目录：目录进回收站而不是被 rm。
	rec, env = a.req(t, token, http.MethodDelete, "/api/v1/website/sites/"+id+"?deleteRoot=true", "")
	require.Equal(t, http.StatusOK, rec.Code, env.Message)
	_, statErr = os.Stat(detail.VhostFile)
	assert.True(t, os.IsNotExist(statErr))
	_, statErr = os.Stat(detail.RootPath)
	assert.True(t, os.IsNotExist(statErr), "根目录应被移走")

	entries, err := os.ReadDir(filepath.Join(a.websiteRoot, ".recycle"))
	require.NoError(t, err)
	require.NotEmpty(t, entries, "根目录必须进回收站，便于误删恢复")
	assert.True(t, strings.HasPrefix(entries[0].Name(), "shop-"))

	// 删除后域名释放，可被新站点复用。
	createStaticSite(t, a, token, "shop-new", "shop.example.com")
}

func TestWebsiteUpdateDomainsAndRemark(t *testing.T) {
	a, _ := newWebsiteApp(t)
	token, _ := a.login(t)
	detail := createStaticSite(t, a, token, "shop", "shop.example.com")
	id := itoa(detail.ID)

	rec, env := a.req(t, token, http.MethodPut, "/api/v1/website/sites/"+id,
		`{"domains":[{"domain":"www.example.com","port":80},{"domain":"m.example.com","port":80}],"remark":"改过备注"}`)
	require.Equal(t, http.StatusOK, rec.Code, env.Message)
	var updated model.SiteDetail
	require.NoError(t, json.Unmarshal(env.Data, &updated))
	assert.Equal(t, "www.example.com", updated.PrimaryDomain)
	require.Len(t, updated.Domains, 2)
	assert.Equal(t, "改过备注", updated.Remark)
	assert.Equal(t, 2, updated.ConfigVersion, "改域名要产生新的配置版本")

	conf, err := os.ReadFile(updated.VhostFile)
	require.NoError(t, err)
	assert.Contains(t, string(conf), "server_name www.example.com m.example.com;")
	assert.NotContains(t, string(conf), "shop.example.com")

	// 旧域名释放后可被别的站点使用。
	createStaticSite(t, a, token, "legacy", "shop.example.com")
}

func TestWebsiteConfigSaveRollbackAndOptimisticLock(t *testing.T) {
	a, _ := newWebsiteApp(t)
	token, _ := a.login(t)
	detail := createStaticSite(t, a, token, "shop", "shop.example.com")
	id := itoa(detail.ID)

	rec, env := a.req(t, token, http.MethodGet, "/api/v1/website/sites/"+id+"/config", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var cfg model.SiteConfigDTO
	require.NoError(t, json.Unmarshal(env.Data, &cfg))
	assert.Equal(t, 1, cfg.Version)
	assert.Contains(t, cfg.Content, "server_name shop.example.com;")
	require.Len(t, cfg.Versions, 1)
	assert.True(t, cfg.Versions[0].IsCurrent)

	// 基线落后时拒绝保存，避免覆盖他人改动。
	rec, env = a.req(t, token, http.MethodPut, "/api/v1/website/sites/"+id+"/config",
		`{"content":"server { listen 80; }","version":0}`)
	require.Equal(t, http.StatusOK, rec.Code, env.Message)
	var saved model.SiteConfigDTO
	require.NoError(t, json.Unmarshal(env.Data, &saved))
	assert.Equal(t, 2, saved.Version)

	rec, env = a.req(t, token, http.MethodPut, "/api/v1/website/sites/"+id+"/config",
		`{"content":"server { listen 8080; }","version":1}`)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Equal(t, errs.CodeConflict, env.Code)

	// 回滚把旧正文作为新版本重新下发，历史只增不改。
	rec, env = a.req(t, token, http.MethodPost, "/api/v1/website/sites/"+id+"/config/rollback/1", `{}`)
	require.Equal(t, http.StatusOK, rec.Code, env.Message)
	var rolled model.SiteConfigDTO
	require.NoError(t, json.Unmarshal(env.Data, &rolled))
	assert.Equal(t, 3, rolled.Version)
	assert.Contains(t, rolled.Content, "server_name shop.example.com;")
	require.Len(t, rolled.Versions, 3)
	assert.Equal(t, model.SiteConfigSourceRollback, rolled.Versions[0].Source)

	// 落到宿主机的内容与回滚结果一致。
	conf, err := os.ReadFile(detail.VhostFile)
	require.NoError(t, err)
	assert.Contains(t, string(conf), "server_name shop.example.com;")

	// 空内容与不存在的版本都要被拒绝。
	rec, _ = a.req(t, token, http.MethodPut, "/api/v1/website/sites/"+id+"/config", `{"content":"   "}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	rec, _ = a.req(t, token, http.MethodPost, "/api/v1/website/sites/"+id+"/config/rollback/99", `{}`)
	assert.GreaterOrEqual(t, rec.Code, http.StatusBadRequest)
}

func TestWebsiteReadonlyRoleCannotWrite(t *testing.T) {
	a, _ := newWebsiteApp(t)
	adminToken, _ := a.login(t)
	detail := createStaticSite(t, a, adminToken, "shop", "shop.example.com")
	id := itoa(detail.ID)

	viewer := a.readonlyToken(t)

	// 只读角色可以看列表、详情与配置。
	for _, path := range []string{
		"/api/v1/website/env",
		"/api/v1/website/sites",
		"/api/v1/website/sites/" + id,
		"/api/v1/website/sites/" + id + "/config",
	} {
		rec, env := a.req(t, viewer, http.MethodGet, path, "")
		assert.Equal(t, http.StatusOK, rec.Code, path)
		assert.Zero(t, env.Code, path)
	}

	// 写操作一律 403，且不产生任何副作用。
	writes := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/api/v1/website/sites", `{"name":"x","type":"static","domains":[{"domain":"x.example.com","port":80}]}`},
		{http.MethodPut, "/api/v1/website/sites/" + id, `{"domains":[{"domain":"y.example.com","port":80}]}`},
		{http.MethodDelete, "/api/v1/website/sites/" + id + "?deleteRoot=false", ""},
		{http.MethodPost, "/api/v1/website/sites/" + id + "/status", `{"target":"stopped"}`},
		{http.MethodPost, "/api/v1/website/sites/" + id + "/rebuild", `{}`},
		{http.MethodPut, "/api/v1/website/sites/" + id + "/config", `{"content":"server { }"}`},
		{http.MethodPost, "/api/v1/website/sites/" + id + "/config/rollback/1", `{}`},
		{http.MethodPost, "/api/v1/website/nginx/reload", `{}`},
	}
	for _, w := range writes {
		rec, env := a.req(t, viewer, w.method, w.path, w.body)
		assert.Equal(t, http.StatusForbidden, rec.Code, w.method+" "+w.path)
		assert.Equal(t, errs.CodeForbidden, env.Code, w.method+" "+w.path)
	}

	// 站点仍在，且未被停用或改动。
	rec, env := a.req(t, adminToken, http.MethodGet, "/api/v1/website/sites/"+id, "")
	require.Equal(t, http.StatusOK, rec.Code)
	var after model.SiteDetail
	require.NoError(t, json.Unmarshal(env.Data, &after))
	assert.Equal(t, model.SiteStatusRunning, after.Status)
	assert.Equal(t, "shop.example.com", after.PrimaryDomain)
}

func TestWebsiteTenantIsolation(t *testing.T) {
	a, _ := newWebsiteApp(t)
	adminToken, _ := a.login(t)
	detail := createStaticSite(t, a, adminToken, "shop", "shop.example.com")
	id := itoa(detail.ID)

	other := a.tenantToken(t, 2, "tenant2-admin")

	// 另一个租户看不到这个站点，读写都当作不存在。
	rec, env := a.req(t, other, http.MethodGet, "/api/v1/website/sites/"+id, "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, errs.CodeSiteNotFound, env.Code)

	for _, c := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPut, "/api/v1/website/sites/" + id, `{"domains":[{"domain":"z.example.com","port":80}]}`},
		{http.MethodDelete, "/api/v1/website/sites/" + id + "?deleteRoot=true", ""},
		{http.MethodPost, "/api/v1/website/sites/" + id + "/status", `{"target":"stopped"}`},
		{http.MethodGet, "/api/v1/website/sites/" + id + "/config", ""},
		{http.MethodPost, "/api/v1/website/sites/" + id + "/config/rollback/1", `{}`},
	} {
		rec, env := a.req(t, other, c.method, c.path, c.body)
		assert.Equal(t, http.StatusNotFound, rec.Code, c.method+" "+c.path)
		assert.Equal(t, errs.CodeSiteNotFound, env.Code, c.method+" "+c.path)
	}

	// 列表按租户隔离。
	rec, env = a.req(t, other, http.MethodGet, "/api/v1/website/sites?page=1&pageSize=20", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var list model.SiteListResult
	require.NoError(t, json.Unmarshal(env.Data, &list))
	assert.Empty(t, list.Items)
	assert.Zero(t, list.Total)

	// 但域名唯一性是全局的：同一台机器上不同租户也不能抢同一个域名+端口。
	rec, env = a.req(t, other, http.MethodPost, "/api/v1/website/sites",
		`{"name":"shop-b","type":"static","domains":[{"domain":"shop.example.com","port":80}]}`)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Equal(t, errs.CodeDomainExists, env.Code)

	// 原租户的站点与线上配置未受影响。
	_, err := os.Stat(detail.VhostFile)
	require.NoError(t, err)
}

func TestWebsiteResponsesDoNotLeakSensitiveFields(t *testing.T) {
	a, _ := newWebsiteApp(t)
	token, _ := a.login(t)
	detail := createStaticSite(t, a, token, "shop", "shop.example.com")
	id := itoa(detail.ID)

	for _, path := range []string{
		"/api/v1/website/env",
		"/api/v1/website/sites",
		"/api/v1/website/sites/" + id,
		"/api/v1/website/sites/" + id + "/config",
	} {
		_, env := a.req(t, token, http.MethodGet, path, "")
		body := strings.ToLower(string(env.Data))
		for _, leak := range []string{"passwordhash", "password", "totpsecret", "privatekey", "dsn"} {
			assert.NotContains(t, body, leak, "%s 不得出现在 %s 响应中", leak, path)
		}
	}
}

func TestWebsiteSettingsRoundTripThroughAPI(t *testing.T) {
	a, _ := newWebsiteApp(t)
	token, _ := a.login(t)
	detail := createStaticSite(t, a, token, "shop", "shop.example.com")
	id := itoa(detail.ID)

	// 读接口先给出默认值与可选缓存档位，前端据此渲染表单。
	rec, env := a.req(t, token, http.MethodGet, "/api/v1/website/sites/"+id+"/settings", "")
	require.Equal(t, http.StatusOK, rec.Code, env.Message)
	var got model.SiteSettingsDTO
	require.NoError(t, json.Unmarshal(env.Data, &got))
	assert.Equal(t, detail.ID, got.SiteID)
	assert.Equal(t, model.CacheProfileNone, got.CacheProfile)
	assert.NotEmpty(t, got.CacheProfiles)
	assert.Equal(t, detail.RootPath, got.RootPath)
	// 站点 ID 必须以字符串序列化，避免前端精度丢失。
	assert.Contains(t, string(env.Data), `"siteId":"`+id+`"`)

	require.NoError(t, os.MkdirAll(filepath.Join(detail.RootPath, "public"), 0o755))
	rec, env = a.req(t, token, http.MethodPut, "/api/v1/website/sites/"+id+"/settings",
		`{"indexFiles":["index.html"],"runPath":"/public","autoIndex":true,"cacheProfile":"month",`+
			`"accessControl":{"enabled":true,"denyIps":["203.0.113.7"],"denyUserAgents":["curl"]},`+
			`"redirects":[{"enabled":true,"path":"/legacy","target":"https://new.example.com","code":301,"keepPath":true}],`+
			`"rateLimit":{"enabled":true,"bandwidthKb":256},`+
			`"accessLogEnabled":true,"errorLogEnabled":false}`)
	require.Equal(t, http.StatusOK, rec.Code, env.Message)
	var saved model.SiteSettingsDTO
	require.NoError(t, json.Unmarshal(env.Data, &saved))
	assert.Equal(t, "/public", saved.RunPath)
	assert.True(t, saved.AutoIndex)
	assert.False(t, saved.ErrorLogEnabled)

	// 落到宿主机的配置必须与保存结果一致。
	conf, err := os.ReadFile(detail.VhostFile)
	require.NoError(t, err)
	assert.Contains(t, string(conf), "root "+filepath.Join(detail.RootPath, "public")+";")
	assert.Contains(t, string(conf), "autoindex on;")
	assert.Contains(t, string(conf), "expires 30d;")
	assert.Contains(t, string(conf), "deny 203.0.113.7;")
	assert.Contains(t, string(conf), "limit_rate 256k;")
	assert.Contains(t, string(conf), "https://new.example.com")
	assert.NotContains(t, string(conf), "error_log", "关闭错误日志后不应再写 error_log 指令")

	// 设置改动走配置版本，可回滚。
	rec, env = a.req(t, token, http.MethodGet, "/api/v1/website/sites/"+id+"/config", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var cfg model.SiteConfigDTO
	require.NoError(t, json.Unmarshal(env.Data, &cfg))
	assert.Equal(t, 2, cfg.Version)

	// 重读应与保存结果一致，不依赖前端缓存。
	_, env = a.req(t, token, http.MethodGet, "/api/v1/website/sites/"+id+"/settings", "")
	var reread model.SiteSettingsDTO
	require.NoError(t, json.Unmarshal(env.Data, &reread))
	assert.Equal(t, saved, reread)

	// 非法入参由服务层拦下，返回可读原因。
	rec, env = a.req(t, token, http.MethodPut, "/api/v1/website/sites/"+id+"/settings",
		`{"runPath":"/../etc"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, errs.CodeInvalidParam, env.Code)
	assert.NotEmpty(t, env.Message)
}

func TestWebsiteRewriteRoundTripThroughAPI(t *testing.T) {
	a, _ := newWebsiteApp(t)
	token, _ := a.login(t)
	detail := createStaticSite(t, a, token, "blog", "blog.example.com")
	id := itoa(detail.ID)

	// 规则库供下拉选择，内置模板必须带正文。
	rec, env := a.req(t, token, http.MethodGet, "/api/v1/website/rewrite/templates", "")
	require.Equal(t, http.StatusOK, rec.Code, env.Message)
	var lib struct {
		Items []model.SiteRewriteTemplateDTO `json:"items"`
	}
	require.NoError(t, json.Unmarshal(env.Data, &lib))
	require.NotEmpty(t, lib.Items)
	var wp *model.SiteRewriteTemplateDTO
	for i := range lib.Items {
		if lib.Items[i].Code == "wordpress" {
			wp = &lib.Items[i]
		}
	}
	require.NotNil(t, wp, "规则库必须包含 WordPress")
	assert.NotEmpty(t, wp.Content)

	// 未配置时返回 none，前端不需要处理 404。
	rec, env = a.req(t, token, http.MethodGet, "/api/v1/website/sites/"+id+"/rewrite", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var cur model.SiteRewriteDTO
	require.NoError(t, json.Unmarshal(env.Data, &cur))
	assert.Equal(t, model.RewriteTemplateNone, cur.TemplateCode)

	rec, env = a.req(t, token, http.MethodPut, "/api/v1/website/sites/"+id+"/rewrite",
		`{"templateCode":"wordpress","enabled":true}`)
	require.Equal(t, http.StatusOK, rec.Code, env.Message)
	var saved model.SiteRewriteDTO
	require.NoError(t, json.Unmarshal(env.Data, &saved))
	assert.Equal(t, "wordpress", saved.TemplateCode)
	assert.Equal(t, wp.Content, saved.Content, "内置模板以规则库正文为准")
	assert.Equal(t, filepath.Join(a.websiteVhost, "rewrite", "blog.conf"), saved.File)

	snippet, err := os.ReadFile(saved.File)
	require.NoError(t, err)
	assert.Contains(t, string(snippet), "index.php")
	conf, err := os.ReadFile(detail.VhostFile)
	require.NoError(t, err)
	assert.Contains(t, string(conf), "include "+saved.File+";")

	// 自定义规则可以提交，危险指令被拒绝且不改动线上片段。
	rec, env = a.req(t, token, http.MethodPut, "/api/v1/website/sites/"+id+"/rewrite",
		`{"templateCode":"custom","content":"root /etc;","enabled":true}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, errs.CodeRewriteInvalid, env.Code)
	after, err := os.ReadFile(saved.File)
	require.NoError(t, err)
	assert.Equal(t, string(snippet), string(after))

	rec, env = a.req(t, token, http.MethodPut, "/api/v1/website/sites/"+id+"/rewrite",
		`{"templateCode":"custom","content":"rewrite ^/feed$ /feed.xml last;","enabled":true}`)
	require.Equal(t, http.StatusOK, rec.Code, env.Message)
	var custom model.SiteRewriteDTO
	require.NoError(t, json.Unmarshal(env.Data, &custom))
	assert.Equal(t, model.RewriteTemplateCustom, custom.TemplateCode)
	body, err := os.ReadFile(custom.File)
	require.NoError(t, err)
	assert.Contains(t, string(body), "rewrite ^/feed$ /feed.xml last;")

	// 关闭后片段与 include 一起消失。
	rec, env = a.req(t, token, http.MethodPut, "/api/v1/website/sites/"+id+"/rewrite",
		`{"templateCode":"none"}`)
	require.Equal(t, http.StatusOK, rec.Code, env.Message)
	_, statErr := os.Stat(custom.File)
	assert.True(t, os.IsNotExist(statErr))
	conf, err = os.ReadFile(detail.VhostFile)
	require.NoError(t, err)
	assert.NotContains(t, string(conf), "include ")
}

func TestWebsiteSettingsRBACAndTenantBoundary(t *testing.T) {
	a, _ := newWebsiteApp(t)
	adminToken, _ := a.login(t)
	detail := createStaticSite(t, a, adminToken, "shop", "shop.example.com")
	id := itoa(detail.ID)

	viewer := a.readonlyToken(t)
	for _, path := range []string{
		"/api/v1/website/sites/" + id + "/settings",
		"/api/v1/website/sites/" + id + "/rewrite",
		"/api/v1/website/rewrite/templates",
	} {
		rec, env := a.req(t, viewer, http.MethodGet, path, "")
		assert.Equal(t, http.StatusOK, rec.Code, path)
		assert.Zero(t, env.Code, path)
	}
	for _, w := range []struct {
		path string
		body string
	}{
		{"/api/v1/website/sites/" + id + "/settings", `{"autoIndex":true}`},
		{"/api/v1/website/sites/" + id + "/rewrite", `{"templateCode":"wordpress","enabled":true}`},
	} {
		rec, env := a.req(t, viewer, http.MethodPut, w.path, w.body)
		assert.Equal(t, http.StatusForbidden, rec.Code, w.path)
		assert.Equal(t, errs.CodeForbidden, env.Code, w.path)
	}

	// 只读角色的越权请求不得改动线上配置。
	conf, err := os.ReadFile(detail.VhostFile)
	require.NoError(t, err)
	assert.NotContains(t, string(conf), "autoindex on;")
	_, statErr := os.Stat(filepath.Join(a.websiteVhost, "rewrite", "shop.conf"))
	assert.True(t, os.IsNotExist(statErr))

	other := a.tenantToken(t, 2, "tenant2-settings")
	for _, c := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/v1/website/sites/" + id + "/settings", ""},
		{http.MethodPut, "/api/v1/website/sites/" + id + "/settings", `{"autoIndex":true}`},
		{http.MethodGet, "/api/v1/website/sites/" + id + "/rewrite", ""},
		{http.MethodPut, "/api/v1/website/sites/" + id + "/rewrite", `{"templateCode":"wordpress","enabled":true}`},
	} {
		rec, env := a.req(t, other, c.method, c.path, c.body)
		assert.Equal(t, http.StatusNotFound, rec.Code, c.method+" "+c.path)
		assert.Equal(t, errs.CodeSiteNotFound, env.Code, c.method+" "+c.path)
	}
}

func TestWebsiteNginxTestAndReload(t *testing.T) {
	a, runner := newWebsiteApp(t)
	token, _ := a.login(t)

	rec, env := a.req(t, token, http.MethodPost, "/api/v1/website/nginx/test", `{}`)
	require.Equal(t, http.StatusOK, rec.Code, env.Message)
	assert.Contains(t, string(env.Data), "test is successful")

	rec, env = a.req(t, token, http.MethodPost, "/api/v1/website/nginx/reload", `{}`)
	require.Equal(t, http.StatusOK, rec.Code, env.Message)
	assert.Zero(t, env.Code)

	runner.failTest(true)
	rec, env = a.req(t, token, http.MethodPost, "/api/v1/website/nginx/test", `{}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, errs.CodeNginxTestFailed, env.Code)
}
