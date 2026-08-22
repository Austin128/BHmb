package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/novapanel/novapanel/internal/api/v1/middleware"
	"github.com/novapanel/novapanel/internal/api/v1/response"
	"github.com/novapanel/novapanel/internal/model"
	"github.com/novapanel/novapanel/internal/pkg/errs"
	"github.com/novapanel/novapanel/internal/repository"
)

// 列表分页边界：pageSize 上限防止一次拉全表。
const (
	defaultPageSize = 20
	maxPageSize     = 100
)

// Admin 为账号、角色与权限点的只读查询处理器。
// 增删改属于「用户与权限」里程碑，这里只提供页面展示所需的读接口。
type Admin struct {
	users repository.UserRepository
	roles repository.RoleRepository
}

// NewAdmin 构造只读账号处理器。
func NewAdmin(users repository.UserRepository, roles repository.RoleRepository) *Admin {
	return &Admin{users: users, roles: roles}
}

// Users 处理 GET /api/v1/user/users，按租户分页列出账号。
func (h *Admin) Users(c *gin.Context) {
	p, ok := middleware.CurrentPrincipal(c)
	if !ok {
		response.Fail(c, errs.ErrUnauthorized)
		return
	}
	page, pageSize := pageParams(c)

	list, total, err := h.users.List(c.Request.Context(), p.TenantID, (page-1)*pageSize, pageSize)
	if err != nil {
		response.Fail(c, err)
		return
	}

	ids := make([]int64, 0, len(list))
	for i := range list {
		ids = append(ids, list[i].ID)
	}
	rolesOf, err := h.roles.RolesOfUsers(c.Request.Context(), ids)
	if err != nil {
		response.Fail(c, err)
		return
	}

	items := make([]model.UserListItem, 0, len(list))
	for i := range list {
		u := &list[i]
		item := model.UserListItem{
			ID:                 u.ID,
			Username:           u.Username,
			Nickname:           u.Nickname,
			Email:              u.Email,
			Status:             u.Status,
			IsSuper:            u.IsSuper,
			TwoFABound:         u.TwoFactorEnabled,
			Roles:              make([]string, 0, 2),
			RoleCodes:          make([]string, 0, 2),
			LockedUntil:        u.LockedUntil,
			LoginFailCount:     u.LoginFailCount,
			MustChangePassword: u.MustChangePassword,
			LastLoginAt:        u.LastLoginAt,
			LastLoginIP:        u.LastLoginIP,
			CreatedAt:          u.CreatedAt,
		}
		for _, r := range rolesOf[u.ID] {
			item.Roles = append(item.Roles, r.Name)
			item.RoleCodes = append(item.RoleCodes, r.Code)
		}
		items = append(items, item)
	}
	response.Page(c, items, total, page, pageSize)
}

// Roles 处理 GET /api/v1/user/roles，返回角色及其权限点数量。
func (h *Admin) Roles(c *gin.Context) {
	p, ok := middleware.CurrentPrincipal(c)
	if !ok {
		response.Fail(c, errs.ErrUnauthorized)
		return
	}
	roles, err := h.roles.ListRoles(c.Request.Context(), p.TenantID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	counts, err := h.roles.CountPermissionsByRole(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}

	items := make([]model.RoleListItem, 0, len(roles))
	for i := range roles {
		r := &roles[i]
		items = append(items, model.RoleListItem{
			ID:              r.ID,
			Code:            r.Code,
			Name:            r.Name,
			Description:     r.Description,
			DataScope:       r.DataScope,
			IsBuiltin:       r.IsBuiltin,
			Status:          r.Status,
			Sort:            r.Sort,
			PermissionCount: counts[r.ID],
			AllPermissions:  r.Code == model.RoleSuperAdmin,
		})
	}
	response.OK(c, items)
}

// Permissions 处理 GET /api/v1/user/permissions，返回权限点全集并标注当前登录者是否持有。
func (h *Admin) Permissions(c *gin.Context) {
	p, ok := middleware.CurrentPrincipal(c)
	if !ok {
		response.Fail(c, errs.ErrUnauthorized)
		return
	}
	perms, err := h.roles.ListPermissions(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	items := make([]model.PermissionItem, 0, len(perms))
	for i := range perms {
		it := &perms[i]
		items = append(items, model.PermissionItem{
			Code:        it.Code,
			Name:        it.Name,
			Module:      it.Module,
			Resource:    it.Resource,
			Action:      it.Action,
			IsSensitive: it.IsSensitive,
			Granted:     p.Allowed(it.Code),
		})
	}
	response.OK(c, items)
}

// pageParams 解析分页参数，非法值一律回落到默认，不报错打断页面。
func pageParams(c *gin.Context) (page, pageSize int) {
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil || page < 1 {
		page = 1
	}
	pageSize, err = strconv.Atoi(c.DefaultQuery("pageSize", strconv.Itoa(defaultPageSize)))
	if err != nil || pageSize < 1 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	return page, pageSize
}
