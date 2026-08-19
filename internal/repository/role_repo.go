package repository

import (
	"context"
	"sort"

	"gorm.io/gorm"

	"github.com/novapanel/novapanel/internal/model"
	"github.com/novapanel/novapanel/internal/pkg/errs"
)

// RoleRepository 提供角色与权限点装载。权限点集合用于登录后一次性下发给前端，
// 并作为服务端鉴权中间件的判定依据。
type RoleRepository interface {
	RolesOfUser(ctx context.Context, userID int64) ([]model.SysRole, error)
	// PermissionCodesOfUser 返回去重排序后的权限点标识。
	PermissionCodesOfUser(ctx context.Context, userID int64) ([]string, error)
	ListPermissions(ctx context.Context) ([]model.SysPermission, error)
	FindByCode(ctx context.Context, tenantID int64, code string) (*model.SysRole, error)
}

type roleRepo struct {
	db *gorm.DB
}

// NewRoleRepository 构造角色仓储。
func NewRoleRepository(db *gorm.DB) RoleRepository { return &roleRepo{db: db} }

func (r *roleRepo) RolesOfUser(ctx context.Context, userID int64) ([]model.SysRole, error) {
	var roles []model.SysRole
	err := r.db.WithContext(ctx).
		Joins("JOIN sys_user_role ur ON ur.role_id = sys_role.id").
		Where("ur.user_id = ? AND sys_role.status = ?", userID, model.StatusActive).
		Order("sys_role.sort ASC").
		Find(&roles).Error
	if err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "用户角色查询失败")
	}
	return roles, nil
}

func (r *roleRepo) PermissionCodesOfUser(ctx context.Context, userID int64) ([]string, error) {
	var codes []string
	err := r.db.WithContext(ctx).
		Model(&model.SysRolePermission{}).
		Distinct("sys_role_permission.permission_code").
		Joins("JOIN sys_user_role ur ON ur.role_id = sys_role_permission.role_id").
		Joins("JOIN sys_role ro ON ro.id = sys_role_permission.role_id").
		Where("ur.user_id = ? AND ro.status = ? AND ro.deleted_at IS NULL", userID, model.StatusActive).
		Pluck("sys_role_permission.permission_code", &codes).Error
	if err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "用户权限点查询失败")
	}
	sort.Strings(codes)
	return codes, nil
}

func (r *roleRepo) ListPermissions(ctx context.Context) ([]model.SysPermission, error) {
	var perms []model.SysPermission
	if err := r.db.WithContext(ctx).Order("sort ASC").Find(&perms).Error; err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "权限点查询失败")
	}
	return perms, nil
}

func (r *roleRepo) FindByCode(ctx context.Context, tenantID int64, code string) (*model.SysRole, error) {
	var role model.SysRole
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND code = ?", tenantID, code).
		First(&role).Error
	if err != nil {
		return nil, wrapFind(err, "角色")
	}
	return &role, nil
}
