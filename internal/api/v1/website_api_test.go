package v1_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/novapanel/novapanel/internal/model"
	"github.com/novapanel/novapanel/internal/pkg/errs"
)

// 测试环境刻意不提供 Nginx，因此这些用例锁定的是「前置条件与鉴权」这两条真实路径：
// 缺少 Web 服务时创建必须失败并给出明确原因，而不是留下一个不生效的站点。
func TestWebsiteEnvReportsMissingWebServer(t *testing.T) {
	a := newApp(t)
	token, _ := a.login(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/website/env", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec, env := a.do(t, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Zero(t, env.Code)

	var data struct {
		Installed      bool     `json:"installed"`
		Message        string   `json:"message"`
		SupportedTypes []string `json:"supportedTypes"`
		SiteCount      int64    `json:"siteCount"`
		VhostDir       string   `json:"vhostDir"`
	}
	require.NoError(t, json.Unmarshal(env.Data, &data))
	assert.False(t, data.Installed)
	assert.NotEmpty(t, data.Message, "未安装时必须给出可操作的说明")
	assert.Equal(t, []string{"proxy", "static"}, data.SupportedTypes)
	assert.Zero(t, data.SiteCount)
	assert.Equal(t, a.websiteVhost, data.VhostDir)
}

func TestWebsiteListEmptyState(t *testing.T) {
	a := newApp(t)
	token, _ := a.login(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/website/sites?page=1&pageSize=20", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec, env := a.do(t, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var data model.SiteListResult
	require.NoError(t, json.Unmarshal(env.Data, &data))
	assert.Empty(t, data.Items)
	assert.Zero(t, data.Total)
	assert.Equal(t, 1, data.Page)
	assert.Equal(t, 20, data.Size)
}

func TestWebsiteCreateFailsWithoutWebServer(t *testing.T) {
	a := newApp(t)
	token, _ := a.login(t)

	req := jsonReq(t, http.MethodPost, "/api/v1/website/sites",
		`{"name":"demo","type":"static","domains":[{"domain":"demo.example.com","port":80}]}`)
	req.Header.Set("Authorization", "Bearer "+token)
	rec, env := a.do(t, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, errs.CodeUnsupported, env.Code)
	assert.Contains(t, env.Message, "Nginx")
}

func TestWebsiteCreateValidatesPayload(t *testing.T) {
	a := newApp(t)
	token, _ := a.login(t)

	cases := []struct {
		name string
		body string
	}{
		{"缺少域名", `{"name":"nodomain","type":"static","domains":[]}`},
		{"站点标识非法", `{"name":"Bad Name","type":"static","domains":[{"domain":"a.example.com","port":80}]}`},
		{"未知类型", `{"name":"unknown","type":"wordpress","domains":[{"domain":"a.example.com","port":80}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := jsonReq(t, http.MethodPost, "/api/v1/website/sites", tc.body)
			req.Header.Set("Authorization", "Bearer "+token)
			rec, env := a.do(t, req)
			assert.GreaterOrEqual(t, rec.Code, http.StatusBadRequest)
			assert.NotZero(t, env.Code)
		})
	}
}

func TestWebsiteRoutesRequireAuth(t *testing.T) {
	a := newApp(t)

	paths := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/website/env"},
		{http.MethodGet, "/api/v1/website/sites"},
		{http.MethodGet, "/api/v1/website/sites/1"},
		{http.MethodGet, "/api/v1/website/sites/1/config"},
		{http.MethodPost, "/api/v1/website/nginx/reload"},
	}
	for _, p := range paths {
		rec, env := a.do(t, httptest.NewRequest(p.method, p.path, nil))
		assert.Equal(t, http.StatusUnauthorized, rec.Code, p.path)
		assert.NotZero(t, env.Code, p.path)
	}
}

func TestWebsiteSiteNotFound(t *testing.T) {
	a := newApp(t)
	token, _ := a.login(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/website/sites/999999", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec, env := a.do(t, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, errs.CodeSiteNotFound, env.Code)
}

func TestWebsiteInvalidSiteID(t *testing.T) {
	a := newApp(t)
	token, _ := a.login(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/website/sites/abc", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec, env := a.do(t, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, errs.CodeInvalidParam, env.Code)
}

func TestWebsiteReloadReportsMissingWebServer(t *testing.T) {
	a := newApp(t)
	token, _ := a.login(t)

	req := jsonReq(t, http.MethodPost, "/api/v1/website/nginx/reload", `{}`)
	req.Header.Set("Authorization", "Bearer "+token)
	rec, env := a.do(t, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, errs.CodeUnsupported, env.Code)
}
