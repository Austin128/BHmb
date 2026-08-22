package v1_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/novapanel/novapanel/internal/api/v1/handler"
	"github.com/novapanel/novapanel/internal/model"
	"github.com/novapanel/novapanel/internal/pkg/errs"
	"github.com/novapanel/novapanel/internal/rbac"
)

// pageEnvelope 对应 response.Page 的分页信封。
type pageEnvelope struct {
	List     json.RawMessage `json:"list"`
	Total    int64           `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"pageSize"`
}

func TestSystemOverviewRequiresAuth(t *testing.T) {
	a := newApp(t)
	rec, env := a.do(t, httptest.NewRequest(http.MethodGet, "/api/v1/system/overview", nil))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, errs.CodeUnauthorized, env.Code)
}

func TestSystemOverviewReturnsRealRuntimeInfo(t *testing.T) {
	a := newApp(t)
	token, _ := a.login(t)

	rec, env := a.do(t, authed(httptest.NewRequest(http.MethodGet, "/api/v1/system/overview", nil), token))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Zero(t, env.Code)

	var data handler.OverviewResult
	require.NoError(t, json.Unmarshal(env.Data, &data))

	// 面板自身信息在任何平台都必须真实可用
	assert.Equal(t, "test", data.Panel.Version)
	assert.Equal(t, "deadbeef", data.Panel.Commit)
	assert.Positive(t, data.Panel.PID)
	assert.Positive(t, data.Panel.NumCPU)
	assert.Positive(t, data.Panel.Goroutines)
	assert.NotEmpty(t, data.Panel.GoVersion)
	assert.NotEmpty(t, data.Host.OS)
	assert.NotEmpty(t, data.Host.Arch)
	assert.Positive(t, data.CPU.Cores)

	// 数据库探活与连接池
	assert.True(t, data.Database.OK)
	assert.Equal(t, "sqlite", data.Database.Driver)
	assert.GreaterOrEqual(t, data.Database.OpenConnections, 1)

	// 百分比必须落在合法区间，否则前端进度条会越界
	assert.GreaterOrEqual(t, data.CPU.UsagePercent, 0.0)
	assert.LessOrEqual(t, data.CPU.UsagePercent, 100.0)
	assert.GreaterOrEqual(t, data.Memory.UsedPercent, 0.0)
	assert.LessOrEqual(t, data.Memory.UsedPercent, 100.0)

	// 响应里不能出现口令等敏感字段
	assert.NotContains(t, rec.Body.String(), "passwordHash")
}

func TestUserListReturnsAdminWithRoles(t *testing.T) {
	a := newApp(t)
	token, _ := a.login(t)

	rec, env := a.do(t, authed(httptest.NewRequest(http.MethodGet, "/api/v1/user/users", nil), token))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Zero(t, env.Code)

	var page pageEnvelope
	require.NoError(t, json.Unmarshal(env.Data, &page))
	assert.EqualValues(t, 1, page.Total)
	assert.Equal(t, 1, page.Page)
	assert.Equal(t, 20, page.PageSize)

	var items []model.UserListItem
	require.NoError(t, json.Unmarshal(page.List, &items))
	require.Len(t, items, 1)
	assert.Equal(t, "admin", items[0].Username)
	assert.True(t, items[0].IsSuper)
	assert.Equal(t, []string{"超级管理员"}, items[0].Roles)
	assert.Equal(t, []string{model.RoleSuperAdmin}, items[0].RoleCodes)
	// 列表接口绝不能带出口令哈希
	assert.NotContains(t, rec.Body.String(), "passwordHash")
	assert.NotContains(t, rec.Body.String(), "$2a$")
}

func TestUserListPageParamsAreClamped(t *testing.T) {
	a := newApp(t)
	token, _ := a.login(t)

	// 非法页码与超限页大小都要回落到合法值，而不是报错让页面空白
	rec, env := a.do(t, authed(
		httptest.NewRequest(http.MethodGet, "/api/v1/user/users?page=0&pageSize=9999", nil), token))
	require.Equal(t, http.StatusOK, rec.Code)

	var page pageEnvelope
	require.NoError(t, json.Unmarshal(env.Data, &page))
	assert.Equal(t, 1, page.Page)
	assert.Equal(t, 100, page.PageSize)

	// 页码越界时返回空列表但保留 total，前端可据此回退
	_, env = a.do(t, authed(
		httptest.NewRequest(http.MethodGet, "/api/v1/user/users?page=99", nil), token))
	require.NoError(t, json.Unmarshal(env.Data, &page))
	assert.EqualValues(t, 1, page.Total)
	var items []model.UserListItem
	require.NoError(t, json.Unmarshal(page.List, &items))
	assert.Empty(t, items)
}

func TestRoleListCarriesPermissionCounts(t *testing.T) {
	a := newApp(t)
	token, _ := a.login(t)

	rec, env := a.do(t, authed(httptest.NewRequest(http.MethodGet, "/api/v1/user/roles", nil), token))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Zero(t, env.Code)

	var items []model.RoleListItem
	require.NoError(t, json.Unmarshal(env.Data, &items))
	require.Len(t, items, len(rbac.Roles), "内置角色应全部返回")

	byCode := map[string]model.RoleListItem{}
	for _, it := range items {
		byCode[it.Code] = it
	}
	super, ok := byCode[model.RoleSuperAdmin]
	require.True(t, ok)
	assert.True(t, super.IsBuiltin)
	assert.Equal(t, model.DataScopeAll, super.DataScope)
	// 超管绕过权限校验，seed 故意不写绑定行，只能靠 allPermissions 表达
	assert.True(t, super.AllPermissions)
	assert.Zero(t, super.PermissionCount)

	readonly, ok := byCode[model.RoleReadonly]
	require.True(t, ok)
	assert.False(t, readonly.AllPermissions)
	assert.EqualValues(t, len(rbac.CodesOfRole(model.RoleReadonly)), readonly.PermissionCount)

	admin, ok := byCode[model.RoleAdmin]
	require.True(t, ok)
	assert.Greater(t, admin.PermissionCount, readonly.PermissionCount, "管理员权限点应多于只读角色")
}

func TestPermissionListMarksGranted(t *testing.T) {
	a := newApp(t)
	token, _ := a.login(t)

	rec, env := a.do(t, authed(httptest.NewRequest(http.MethodGet, "/api/v1/user/permissions", nil), token))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Zero(t, env.Code)

	var items []model.PermissionItem
	require.NoError(t, json.Unmarshal(env.Data, &items))
	require.Len(t, items, len(rbac.PermissionCodes()))

	modules := map[string]int{}
	for _, it := range items {
		assert.NotEmpty(t, it.Code)
		assert.NotEmpty(t, it.Name)
		// 登录者是超管，所有权限点都应标记为已授权
		assert.True(t, it.Granted, "超管应持有 %s", it.Code)
		modules[it.Module]++
	}
	for _, m := range rbac.Modules() {
		assert.Positive(t, modules[m], "模块 %s 应有权限点", m)
	}

	var sensitive int
	for _, it := range items {
		if it.IsSensitive {
			sensitive++
		}
	}
	assert.Positive(t, sensitive, "敏感权限点标记不能全丢")
}
