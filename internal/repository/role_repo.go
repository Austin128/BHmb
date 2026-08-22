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
	// RolesOfUsers 批量取多个用户的角色，避开列表页的 N+1 查询。
	RolesOfUsers(ctx context.Context, userIDs []int64) (map[int64][]model.SysRole, error)
	// PermissionCodesOfUser 返回去重排序后的权限点标识。
	PermissionCodesOfUser(ctx context.Context, userID int64) ([]string, error)
	ListPermissions(ctx context.Context) ([]model.SysPermission, error)
	// ListRoles 列出本租户全部角色（含停用），按 sort 排序。
	ListRoles(ctx context.Context, tenantID int64) ([]model.SysRole, error)
	// CountPermissionsByRole 返回角色 ID 到权限点数量的映射。
	CountPermissionsByRole(ctx context.Context) (map[int64]int64, error)
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

// RolesOfUsers 一次 JOIN 取回所有用户的角色，在内存里分组。
func (r *roleRepo) RolesOfUsers(ctx context.Context, userIDs []int64) (map[int64][]model.SysRole, error) {
	out := make(map[int64][]model.SysRole, len(userIDs))
	if len(userIDs) == 0 {
		return out, nil
	}
	// 分组需要带上 user_id，而 SysRole 没有这个字段，因此用临时结构接收
	type row struct {
		model.SysRole
		UserID int64 `gorm:"column:user_id"`
	}
	var rows []row
	err := r.db.WithContext(ctx).
		Model(&model.SysRole{}).
		Select("sys_role.*, ur.user_id AS user_id").
		Joins("JOIN sys_user_role ur ON ur.role_id = sys_role.id").
		Where("ur.user_id IN ?", userIDs).
		Order("sys_role.sort ASC").
		Find(&rows).Error
	if err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "用户角色查询失败")
	}
	for i := range rows {
		out[rows[i].UserID] = append(out[rows[i].UserID], rows[i].SysRole)
	}
	return out, nil
}

// ListRoles 同时取平台内置角色（tenant_id=0，由 seed 写入）与本租户自建角色，
// 只按 tenantID 过滤会把内置角色全漏掉。
func (r *roleRepo) ListRoles(ctx context.Context, tenantID int64) ([]model.SysRole, error) {
	var roles []model.SysRole
	err := r.db.WithContext(ctx).
		Where("tenant_id IN ?", []int64{0, tenantID}).
		Order("sort ASC").
		Find(&roles).Error
	if err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "角色列表查询失败")
	}
	return roles, nil
}

func (r *roleRepo) CountPermissionsByRole(ctx context.Context) (map[int64]int64, error) {
	type row struct {
		RoleID int64 `gorm:"column:role_id"`
		Total  int64 `gorm:"column:total"`
	}
	var rows []row
	err := r.db.WithContext(ctx).
		Model(&model.SysRolePermission{}).
		Select("role_id, COUNT(*) AS total").
		Group("role_id").
		Find(&rows).Error
	if err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "角色权限统计失败")
	}
	out := make(map[int64]int64, len(rows))
	for _, it := range rows {
		out[it.RoleID] = it.Total
	}
	return out, nil
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
