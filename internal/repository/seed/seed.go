// Package seed 幂等写入内置数据：默认租户、内置角色、权限点与角色权限绑定。
// 可重复执行，用于首次启动与升级后对账（docs/04 4.18）。
package seed

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/novapanel/novapanel/internal/model"
	"github.com/novapanel/novapanel/internal/pkg/errs"
	"github.com/novapanel/novapanel/internal/pkg/logx"
	"github.com/novapanel/novapanel/internal/rbac"
)

// relationIDBase 为内置关联行的 id 基址。内置实体占用 1~9999，
// 内置关联行占用 100000~999999，雪花 ID（>10^18）与两者均不冲突。
const relationIDBase int64 = 100000

// Seeder 写入内置数据。
type Seeder struct {
	db *gorm.DB
}

// New 创建 Seeder。
func New(db *gorm.DB) *Seeder { return &Seeder{db: db} }

// Run 幂等写入默认租户、角色、权限点与角色权限绑定。
func (s *Seeder) Run(ctx context.Context) error {
	now := time.Now().UTC()
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.seedTenant(ctx, tx, now); err != nil {
			return err
		}
		if err := s.seedRoles(ctx, tx, now); err != nil {
			return err
		}
		if err := s.seedPermissions(ctx, tx, now); err != nil {
			return err
		}
		return s.seedRolePermissions(ctx, tx, now)
	})
	if err != nil {
		return errs.Wrap(err, errs.CodeInternal, "内置数据写入失败")
	}
	logx.FromContext(ctx).Info("内置数据已对账", "module", "seed",
		"roles", len(rbac.Roles), "permissions", len(rbac.Permissions))
	return nil
}

func (s *Seeder) seedTenant(ctx context.Context, tx *gorm.DB, now time.Time) error {
	tenant := model.SysTenant{
		Base:   model.Base{ID: rbac.DefaultTenantID, CreatedAt: now, UpdatedAt: now, TenantID: 0},
		Code:   "default",
		Name:   "默认租户",
		Status: model.TenantStatusActive,
		Remark: "单租户部署下的唯一租户，禁止删除",
	}
	return tx.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"name", "status", "updated_at"}),
	}).Create(&tenant).Error
}

func (s *Seeder) seedRoles(ctx context.Context, tx *gorm.DB, now time.Time) error {
	rows := make([]model.SysRole, 0, len(rbac.Roles))
	for _, r := range rbac.Roles {
		rows = append(rows, model.SysRole{
			Base:        model.Base{ID: r.ID, CreatedAt: now, UpdatedAt: now, TenantID: 0},
			Code:        r.Code,
			Name:        r.Name,
			Description: r.Description,
			IsBuiltin:   true,
			DataScope:   r.DataScope,
			Sort:        r.Sort,
			Status:      model.StatusActive,
		})
	}
	return tx.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"code", "name", "description", "is_builtin", "data_scope", "sort", "updated_at",
		}),
	}).Create(&rows).Error
}

func (s *Seeder) seedPermissions(ctx context.Context, tx *gorm.DB, now time.Time) error {
	rows := make([]model.SysPermission, 0, len(rbac.Permissions))
	for i, p := range rbac.Permissions {
		rows = append(rows, model.SysPermission{
			Base:        model.Base{ID: p.ID, CreatedAt: now, UpdatedAt: now, TenantID: 0},
			Code:        p.Code,
			Name:        p.Name,
			Module:      p.Module,
			Resource:    p.Resource,
			Action:      p.Action,
			IsSensitive: p.Sensitive,
			NeedAudit:   p.Action != "list" && p.Action != "read",
			Sort:        i + 1,
		})
	}
	return tx.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"code", "name", "module", "resource", "action", "is_sensitive", "need_audit", "sort", "updated_at",
		}),
	}).Create(&rows).Error
}

// seedRolePermissions 以 rbac 定义为准重建内置角色的绑定：
// 先删除内置角色的旧绑定，再按定义写入，保证移除权限点时数据库同步收敛。
func (s *Seeder) seedRolePermissions(ctx context.Context, tx *gorm.DB, now time.Time) error {
	permIDs := make(map[string]int64, len(rbac.Permissions))
	for _, p := range rbac.Permissions {
		permIDs[p.Code] = p.ID
	}

	builtinRoleIDs := make([]int64, 0, len(rbac.Roles))
	for _, r := range rbac.Roles {
		builtinRoleIDs = append(builtinRoleIDs, r.ID)
	}
	if err := tx.WithContext(ctx).
		Where("role_id IN ?", builtinRoleIDs).
		Delete(&model.SysRolePermission{}).Error; err != nil {
		return err
	}

	var rows []model.SysRolePermission
	for _, r := range rbac.Roles {
		if r.Code == model.RoleSuperAdmin {
			continue // 超管绕过权限校验，不落绑定
		}
		for _, code := range rbac.CodesOfRole(r.Code) {
			pid, ok := permIDs[code]
			if !ok {
				return errs.Newf(errs.CodeInternal, "权限点 %s 未在 rbac.Permissions 中定义", code)
			}
			rows = append(rows, model.SysRolePermission{
				Relation:       model.Relation{ID: relationIDBase + r.ID*10000 + (pid - 1000), CreatedAt: now, TenantID: 0},
				RoleID:         r.ID,
				PermissionID:   pid,
				PermissionCode: code,
			})
		}
	}
	if len(rows) == 0 {
		return nil
	}
	return tx.WithContext(ctx).Create(&rows).Error
}

// EnsureAdmin 在系统尚无用户时创建初始超级管理员并绑定 super_admin 角色。
// 密码由调用方生成（安装向导或 novactl），本函数只接收 bcrypt 哈希。
// 返回 false 表示已存在用户，未做任何写入。
func (s *Seeder) EnsureAdmin(ctx context.Context, username, passwordHash string) (bool, error) {
	if username == "" || passwordHash == "" {
		return false, errs.New(errs.CodeInvalidParam, "初始管理员用户名与密码哈希不能为空")
	}

	created := false
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.WithContext(ctx).Model(&model.SysUser{}).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return nil
		}

		now := time.Now().UTC()
		user := model.SysUser{
			Base:               model.Base{ID: 1, CreatedAt: now, UpdatedAt: now, TenantID: rbac.DefaultTenantID},
			Username:           username,
			Nickname:           "超级管理员",
			PasswordHash:       passwordHash,
			Status:             model.UserStatusActive,
			IsSuper:            true,
			PasswordUpdatedAt:  &now,
			MustChangePassword: true,
			Lang:               "zh-CN",
			Timezone:           "Asia/Shanghai",
			Remark:             "安装时创建的初始账号",
		}
		if err := tx.WithContext(ctx).Create(&user).Error; err != nil {
			return err
		}
		bind := model.SysUserRole{
			Relation: model.Relation{ID: relationIDBase + 1, CreatedAt: now, TenantID: rbac.DefaultTenantID},
			UserID:   user.ID,
			RoleID:   rbac.RoleIDSuperAdmin,
		}
		if err := tx.WithContext(ctx).Create(&bind).Error; err != nil {
			return err
		}
		created = true
		return nil
	})
	if err != nil {
		return false, errs.Wrap(err, errs.CodeInternal, "初始管理员创建失败")
	}
	if created {
		logx.FromContext(ctx).Info("初始管理员已创建", "module", "seed", "username", username)
	}
	return created, nil
}
