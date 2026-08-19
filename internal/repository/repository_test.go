package repository_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/novapanel/novapanel/internal/model"
	"github.com/novapanel/novapanel/internal/pkg/config"
	"github.com/novapanel/novapanel/internal/pkg/errs"
	"github.com/novapanel/novapanel/internal/rbac"
	"github.com/novapanel/novapanel/internal/repository"
	"github.com/novapanel/novapanel/internal/repository/migrate"
	"github.com/novapanel/novapanel/internal/repository/seed"
	"github.com/novapanel/novapanel/migrations"
)

// setup 使用文件型 SQLite（禁止 :memory:，与生产驱动行为一致）建库、迁移、写种子。
func setup(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := repository.Open(&config.DatabaseConfig{
		Driver:          repository.DriverSQLite,
		Path:            filepath.Join(t.TempDir(), "nova.db"),
		MaxOpenConns:    4,
		MaxIdleConns:    2,
		ConnMaxLifetime: time.Hour,
	}, 200*time.Millisecond)
	require.NoError(t, err)
	t.Cleanup(func() { _ = repository.Close(db) })

	ctx := context.Background()
	_, err = migrate.New(db, migrations.FS, repository.Dialect(db)).Up(ctx)
	require.NoError(t, err)
	require.NoError(t, seed.New(db).Run(ctx))
	return db
}

func TestSeedIsIdempotent(t *testing.T) {
	ctx := context.Background()
	db := setup(t)

	countRows := func(m any) int64 {
		var n int64
		require.NoError(t, db.Model(m).Count(&n).Error)
		return n
	}

	roles := countRows(&model.SysRole{})
	perms := countRows(&model.SysPermission{})
	binds := countRows(&model.SysRolePermission{})
	assert.EqualValues(t, len(rbac.Roles), roles)
	assert.EqualValues(t, len(rbac.Permissions), perms)
	assert.Positive(t, binds)

	require.NoError(t, seed.New(db).Run(ctx), "重复执行种子必须成功")
	assert.Equal(t, roles, countRows(&model.SysRole{}))
	assert.Equal(t, perms, countRows(&model.SysPermission{}))
	assert.Equal(t, binds, countRows(&model.SysRolePermission{}), "重复执行不得产生重复绑定")

	var tenant model.SysTenant
	require.NoError(t, db.First(&tenant, "id = ?", rbac.DefaultTenantID).Error)
	assert.Equal(t, "default", tenant.Code)

	// super_admin 绕过权限校验，不落绑定
	var superBinds int64
	require.NoError(t, db.Model(&model.SysRolePermission{}).
		Where("role_id = ?", rbac.RoleIDSuperAdmin).Count(&superBinds).Error)
	assert.Zero(t, superBinds)
}

func TestEnsureAdminOnlyOnce(t *testing.T) {
	ctx := context.Background()
	db := setup(t)
	s := seed.New(db)

	created, err := s.EnsureAdmin(ctx, "admin", "$2a$12$fakehashfakehashfakehashfakehashfakehashfakehashfakeha")
	require.NoError(t, err)
	assert.True(t, created)

	again, err := s.EnsureAdmin(ctx, "admin2", "$2a$12$other")
	require.NoError(t, err)
	assert.False(t, again, "已存在用户时不得再次创建")

	var n int64
	require.NoError(t, db.Model(&model.SysUser{}).Count(&n).Error)
	assert.EqualValues(t, 1, n)

	_, err = s.EnsureAdmin(ctx, "", "")
	require.Error(t, err)
	assert.Equal(t, errs.CodeInvalidParam, errs.Code(err))
}

func TestUserRepositoryLoginFlow(t *testing.T) {
	ctx := context.Background()
	db := setup(t)
	require.NoError(t, func() error {
		_, err := seed.New(db).EnsureAdmin(ctx, "admin", "$2a$12$hash")
		return err
	}())

	repo := repository.NewUserRepository(db)

	u, err := repo.FindByUsername(ctx, rbac.DefaultTenantID, "admin")
	require.NoError(t, err)
	assert.True(t, u.IsSuper)
	assert.True(t, u.IsActive())
	assert.True(t, u.MustChangePassword)

	_, err = repo.FindByUsername(ctx, rbac.DefaultTenantID, "nobody")
	require.Error(t, err)
	assert.Equal(t, errs.CodeNotFound, errs.Code(err))
	assert.True(t, errors.Is(err, errs.ErrNotFound))

	now := time.Now().UTC().Truncate(time.Millisecond)
	for i := 1; i <= 2; i++ {
		count, err := repo.MarkLoginFailure(ctx, u.ID, 3, 15*time.Minute, now)
		require.NoError(t, err)
		assert.Equal(t, i, count)
	}
	got, err := repo.FindByID(ctx, u.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, got.LoginFailCount)
	assert.False(t, got.IsLocked(now))

	count, err := repo.MarkLoginFailure(ctx, u.ID, 3, 15*time.Minute, now)
	require.NoError(t, err)
	assert.Equal(t, 3, count)

	got, err = repo.FindByID(ctx, u.ID)
	require.NoError(t, err)
	require.NotNil(t, got.LockedUntil)
	assert.True(t, got.IsLocked(now), "达到上限应写入锁定截止时间")
	assert.Zero(t, got.LoginFailCount, "锁定后失败计数归零")

	require.NoError(t, repo.MarkLoginSuccess(ctx, u.ID, now, "10.0.0.9"))
	got, err = repo.FindByID(ctx, u.ID)
	require.NoError(t, err)
	assert.Nil(t, got.LockedUntil)
	assert.Equal(t, "10.0.0.9", got.LastLoginIP)
	require.NotNil(t, got.LastLoginAt)

	require.NoError(t, repo.UpdatePassword(ctx, u.ID, "$2a$12$newhash", now))
	got, err = repo.FindByID(ctx, u.ID)
	require.NoError(t, err)
	assert.Equal(t, "$2a$12$newhash", got.PasswordHash)
	assert.False(t, got.MustChangePassword)
}

func TestRoleRepositoryLoadsPermissions(t *testing.T) {
	ctx := context.Background()
	db := setup(t)
	now := time.Now().UTC()

	// 造一个仅有 auditor 角色的用户，验证权限点按角色装载
	user := model.SysUser{
		Base:         model.Base{ID: 20001, CreatedAt: now, UpdatedAt: now, TenantID: rbac.DefaultTenantID},
		Username:     "auditor01",
		PasswordHash: "$2a$12$hash",
		Status:       model.UserStatusActive,
		Lang:         "zh-CN",
		Timezone:     "Asia/Shanghai",
	}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&model.SysUserRole{
		Relation: model.Relation{ID: 20002, CreatedAt: now, TenantID: rbac.DefaultTenantID},
		UserID:   user.ID,
		RoleID:   rbac.RoleIDAuditor,
	}).Error)

	repo := repository.NewRoleRepository(db)

	roles, err := repo.RolesOfUser(ctx, user.ID)
	require.NoError(t, err)
	require.Len(t, roles, 1)
	assert.Equal(t, model.RoleAuditor, roles[0].Code)

	codes, err := repo.PermissionCodesOfUser(ctx, user.ID)
	require.NoError(t, err)
	assert.ElementsMatch(t, rbac.CodesOfRole(model.RoleAuditor), codes)
	assert.NotContains(t, codes, "user:user:create", "审计员不得有写权限")

	perms, err := repo.ListPermissions(ctx)
	require.NoError(t, err)
	assert.Len(t, perms, len(rbac.Permissions))

	role, err := repo.FindByCode(ctx, 0, model.RoleAdmin)
	require.NoError(t, err)
	assert.Equal(t, rbac.RoleIDAdmin, role.ID)
	assert.True(t, role.IsBuiltin)
}

func TestSessionRepositoryLifecycle(t *testing.T) {
	ctx := context.Background()
	db := setup(t)
	repo := repository.NewSessionRepository(db)
	now := time.Now().UTC().Truncate(time.Millisecond)

	s := &model.SysSession{
		Base:             model.Base{ID: 30001, CreatedAt: now, UpdatedAt: now, TenantID: rbac.DefaultTenantID},
		UserID:           1,
		JTI:              "jti-1",
		RefreshTokenHash: "hash-1",
		DeviceType:       "web",
		ClientIP:         "10.0.0.1",
		LoginAt:          now,
		LastActiveAt:     now,
		AccessExpireAt:   now.Add(15 * time.Minute),
		RefreshExpireAt:  now.Add(168 * time.Hour),
		Status:           model.SessionStatusActive,
	}
	require.NoError(t, repo.Create(ctx, s))

	got, err := repo.FindByRefreshHash(ctx, "hash-1")
	require.NoError(t, err)
	assert.Equal(t, s.ID, got.ID)
	assert.True(t, got.IsUsable(now))

	got, err = repo.FindByJTI(ctx, "jti-1")
	require.NoError(t, err)
	assert.Equal(t, s.ID, got.ID)

	later := now.Add(time.Minute)
	require.NoError(t, repo.Rotate(ctx, s.ID, "jti-2", "hash-2",
		later.Add(15*time.Minute), later.Add(168*time.Hour), later))

	_, err = repo.FindByRefreshHash(ctx, "hash-1")
	require.Error(t, err, "轮换后旧哈希必须失效")
	assert.Equal(t, errs.CodeNotFound, errs.Code(err))

	got, err = repo.FindByRefreshHash(ctx, "hash-2")
	require.NoError(t, err)
	assert.Equal(t, "jti-2", got.JTI)

	active, err := repo.ActiveOfUser(ctx, 1, later)
	require.NoError(t, err)
	require.Len(t, active, 1)

	require.NoError(t, repo.Touch(ctx, s.ID, later.Add(time.Minute)))

	require.NoError(t, repo.Revoke(ctx, s.ID, model.RevokeReasonLogout, later))
	got, err = repo.FindByID(ctx, s.ID)
	require.NoError(t, err)
	assert.Equal(t, model.SessionStatusRevoked, got.Status)
	assert.Equal(t, model.RevokeReasonLogout, got.RevokeReason)
	assert.False(t, got.IsUsable(later))

	err = repo.Rotate(ctx, s.ID, "jti-3", "hash-3", later, later, later)
	require.Error(t, err, "已吊销会话不得轮换")
	assert.Equal(t, errs.CodeRefreshInvalid, errs.Code(err))

	active, err = repo.ActiveOfUser(ctx, 1, later)
	require.NoError(t, err)
	assert.Empty(t, active)
}

func TestSessionRepositoryRevokeAllAndGC(t *testing.T) {
	ctx := context.Background()
	db := setup(t)
	repo := repository.NewSessionRepository(db)
	now := time.Now().UTC().Truncate(time.Millisecond)

	for i := range 3 {
		require.NoError(t, repo.Create(ctx, &model.SysSession{
			Base:             model.Base{ID: int64(40001 + i), CreatedAt: now, UpdatedAt: now, TenantID: rbac.DefaultTenantID},
			UserID:           7,
			JTI:              "jti-" + string(rune('a'+i)),
			RefreshTokenHash: "hash-" + string(rune('a'+i)),
			LoginAt:          now,
			LastActiveAt:     now.Add(time.Duration(i) * time.Minute),
			AccessExpireAt:   now.Add(15 * time.Minute),
			RefreshExpireAt:  now.Add(168 * time.Hour),
			Status:           model.SessionStatusActive,
		}))
	}

	n, err := repo.RevokeAllOfUser(ctx, 7, model.RevokeReasonPasswordChanged, now)
	require.NoError(t, err)
	assert.EqualValues(t, 3, n)

	// 过期会话物理清理
	expired := &model.SysSession{
		Base:             model.Base{ID: 40100, CreatedAt: now, UpdatedAt: now, TenantID: rbac.DefaultTenantID},
		UserID:           8,
		JTI:              "jti-expired",
		RefreshTokenHash: "hash-expired",
		LoginAt:          now.Add(-30 * 24 * time.Hour),
		LastActiveAt:     now.Add(-30 * 24 * time.Hour),
		AccessExpireAt:   now.Add(-30 * 24 * time.Hour),
		RefreshExpireAt:  now.Add(-20 * 24 * time.Hour),
		Status:           model.SessionStatusExpired,
	}
	require.NoError(t, repo.Create(ctx, expired))

	deleted, err := repo.DeleteExpiredBefore(ctx, now.Add(-7*24*time.Hour))
	require.NoError(t, err)
	assert.EqualValues(t, 1, deleted)

	_, err = repo.FindByID(ctx, expired.ID)
	require.Error(t, err)
	assert.Equal(t, errs.CodeNotFound, errs.Code(err))
}
