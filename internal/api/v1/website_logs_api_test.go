package v1_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/novapanel/novapanel/internal/model"
	"github.com/novapanel/novapanel/internal/pkg/errs"
)

// seedLog 直接往日志目录写文件，模拟 Nginx 已经产生访问日志。
func seedLog(t *testing.T, dir, name string, lines []string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644))
	return p
}

func TestWebsiteLogsThroughAPI(t *testing.T) {
	a, _ := newWebsiteApp(t)
	token, _ := a.login(t)
	detail := createStaticSite(t, a, token, "logs", "logs.example.com")
	id := itoa(detail.ID)

	// 未产生日志时不能报错，前端据 exists 展示空态。
	rec, env := a.req(t, token, http.MethodGet, "/api/v1/website/sites/"+id+"/logs?type=access", "")
	require.Equal(t, http.StatusOK, rec.Code, env.Message)
	var empty model.SiteLogDTO
	require.NoError(t, json.Unmarshal(env.Data, &empty))
	assert.False(t, empty.Exists)
	assert.Empty(t, empty.Lines)
	assert.True(t, empty.Enabled)
	assert.Equal(t, filepath.Join(a.websiteLogDir, "logs.log"), empty.Path)
	// 站点 ID 以字符串下发，避免前端大整数精度丢失。
	assert.Contains(t, string(env.Data), `"siteId":"`+id+`"`)

	path := seedLog(t, a.websiteLogDir, "logs.log", []string{"a 1", "b 2", "c 3"})
	rec, env = a.req(t, token, http.MethodGet, "/api/v1/website/sites/"+id+"/logs?type=access&lines=2", "")
	require.Equal(t, http.StatusOK, rec.Code, env.Message)
	var tail model.SiteLogDTO
	require.NoError(t, json.Unmarshal(env.Data, &tail))
	assert.Equal(t, []string{"b 2", "c 3"}, tail.Lines)
	assert.True(t, tail.Exists)
	assert.True(t, tail.Truncated)

	// 错误日志与访问日志各自独立。
	seedLog(t, a.websiteLogDir, "logs.error.log", []string{"oops"})
	rec, env = a.req(t, token, http.MethodGet, "/api/v1/website/sites/"+id+"/logs?type=error", "")
	require.Equal(t, http.StatusOK, rec.Code, env.Message)
	var errLog model.SiteLogDTO
	require.NoError(t, json.Unmarshal(env.Data, &errLog))
	assert.Equal(t, []string{"oops"}, errLog.Lines)

	rec, env = a.req(t, token, http.MethodPost, "/api/v1/website/sites/"+id+"/logs/clear?type=access", `{}`)
	require.Equal(t, http.StatusOK, rec.Code, env.Message)
	var cleared model.SiteLogDTO
	require.NoError(t, json.Unmarshal(env.Data, &cleared))
	assert.Empty(t, cleared.Lines)
	assert.Zero(t, cleared.Size)

	st, err := os.Stat(path)
	require.NoError(t, err, "清空必须保留文件本身，否则 Nginx 仍会写向已删除的 inode")
	assert.Zero(t, st.Size())

	// 清空访问日志不应波及错误日志。
	rec, env = a.req(t, token, http.MethodGet, "/api/v1/website/sites/"+id+"/logs?type=error", "")
	require.Equal(t, http.StatusOK, rec.Code, env.Message)
	require.NoError(t, json.Unmarshal(env.Data, &errLog))
	assert.Equal(t, []string{"oops"}, errLog.Lines)
}

func TestWebsiteLogsRejectsBadParams(t *testing.T) {
	a, _ := newWebsiteApp(t)
	token, _ := a.login(t)
	detail := createStaticSite(t, a, token, "badp", "badp.example.com")
	id := itoa(detail.ID)

	for _, q := range []string{"?type=syslog", "?type=access&lines=0", "?type=access&lines=abc"} {
		rec, env := a.req(t, token, http.MethodGet, "/api/v1/website/sites/"+id+"/logs"+q, "")
		assert.Equal(t, http.StatusBadRequest, rec.Code, q)
		assert.Equal(t, errs.CodeInvalidParam, env.Code, q)
	}

	rec, env := a.req(t, token, http.MethodGet, "/api/v1/website/sites/abc/logs?type=access", "")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, errs.CodeInvalidParam, env.Code)
}

func TestWebsiteLogsRBACAndTenantBoundary(t *testing.T) {
	a, _ := newWebsiteApp(t)
	token, _ := a.login(t)
	detail := createStaticSite(t, a, token, "acl", "acl.example.com")
	id := itoa(detail.ID)
	path := seedLog(t, a.websiteLogDir, "acl.log", []string{"keep-me"})

	// 只读角色可以看日志，但不能清空。
	viewer := a.readonlyToken(t)
	rec, env := a.req(t, viewer, http.MethodGet, "/api/v1/website/sites/"+id+"/logs?type=access", "")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Zero(t, env.Code)

	rec, env = a.req(t, viewer, http.MethodPost, "/api/v1/website/sites/"+id+"/logs/clear?type=access", `{}`)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, errs.CodeForbidden, env.Code)

	body, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(body), "keep-me", "越权请求不得改动日志文件")

	// 跨租户表现为站点不存在，且不泄漏日志内容。
	other := a.tenantToken(t, 2, "tenant2-logs")
	for _, c := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/website/sites/" + id + "/logs?type=access"},
		{http.MethodPost, "/api/v1/website/sites/" + id + "/logs/clear?type=access"},
	} {
		rec, env := a.req(t, other, c.method, c.path, `{}`)
		assert.Equal(t, http.StatusNotFound, rec.Code, c.path)
		assert.Equal(t, errs.CodeSiteNotFound, env.Code, c.path)
		assert.NotContains(t, string(env.Data), "keep-me", c.path)
	}

	body, err = os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(body), "keep-me")
}

func TestWebsiteLogsRequireAuth(t *testing.T) {
	a, _ := newWebsiteApp(t)

	for _, c := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/website/sites/1/logs?type=access"},
		{http.MethodPost, "/api/v1/website/sites/1/logs/clear?type=access"},
	} {
		rec, env := a.req(t, "", c.method, c.path, `{}`)
		assert.Equal(t, http.StatusUnauthorized, rec.Code, c.path)
		assert.Equal(t, errs.CodeUnauthorized, env.Code, c.path)
	}
}
