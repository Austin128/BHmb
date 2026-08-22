package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/novapanel/novapanel/internal/model"
	"github.com/novapanel/novapanel/internal/pkg/config"
	"github.com/novapanel/novapanel/internal/pkg/errs"
	"github.com/novapanel/novapanel/internal/repository"
	"github.com/novapanel/novapanel/internal/repository/migrate"
	"github.com/novapanel/novapanel/internal/repository/seed"
	"github.com/novapanel/novapanel/migrations"
)

// newRenameDB 准备一个带内置数据与 admin 账号的真实 SQLite 库。
func newRenameDB(t *testing.T) *gorm.DB {
	t.Helper()
	ctx := context.Background()

	db, err := repository.Open(&config.DatabaseConfig{
		Driver:          repository.DriverSQLite,
		Path:            filepath.Join(t.TempDir(), "nova.db"),
		MaxOpenConns:    4,
		MaxIdleConns:    2,
		ConnMaxLifetime: time.Hour,
	}, 200*time.Millisecond)
	require.NoError(t, err)
	t.Cleanup(func() { _ = repository.Close(db) })

	_, err = migrate.New(db, migrations.FS, repository.Dialect(db)).Up(ctx)
	require.NoError(t, err)
	require.NoError(t, seed.New(db).Run(ctx))
	_, err = seed.New(db).EnsureAdmin(ctx, "admin", "$2a$04$abcdefghijklmnopqrstuv")
	require.NoError(t, err)
	return db
}

func TestUserRenameSuccessRevokesSessions(t *testing.T) {
	ctx := context.Background()
	db := newRenameDB(t)

	var admin model.SysUser
	require.NoError(t, db.Where("username = ?", "admin").First(&admin).Error)
	hash := admin.PasswordHash

	require.NoError(t, db.Create(&model.SysSession{
		Base:             model.Base{ID: 90001, TenantID: admin.TenantID},
		UserID:           admin.ID,
		JTI:              "jti-active",
		RefreshTokenHash: "hash-active",
		Status:           model.SessionStatusActive,
		LoginAt:          time.Now(),
		LastActiveAt:     time.Now(),
		AccessExpireAt:   time.Now().Add(15 * time.Minute),
		RefreshExpireAt:  time.Now().Add(time.Hour),
	}).Error)

	require.NoError(t, userRename(ctx, db, "admin", "ops.admin"))

	var renamed model.SysUser
	require.NoError(t, db.Where("id = ?", admin.ID).First(&renamed).Error)
	assert.Equal(t, "ops.admin", renamed.Username)
	assert.Equal(t, hash, renamed.PasswordHash, "改名不应影响口令")

	var session model.SysSession
	require.NoError(t, db.Where("id = ?", 90001).First(&session).Error)
	assert.Equal(t, model.SessionStatusRevoked, session.Status)
	assert.Equal(t, model.RevokeReasonAdminRevoke, session.RevokeReason)
}

func TestUserRenameRejectsCaseOnlyChange(t *testing.T) {
	ctx := context.Background()
	db := newRenameDB(t)

	err := userRename(ctx, db, "admin", "Admin")
	require.Error(t, err)
	assert.Equal(t, errs.CodeInvalidParam, errs.Code(err))
	assert.Contains(t, err.Error(), "仅大小写不同")

	var admin model.SysUser
	require.NoError(t, db.Where("username = ?", "admin").First(&admin).Error)
	assert.Equal(t, "admin", admin.Username, "被拒绝时不能落库")
}

func TestUserRenameRejectsConflictIgnoringCase(t *testing.T) {
	ctx := context.Background()
	db := newRenameDB(t)

	var admin model.SysUser
	require.NoError(t, db.Where("username = ?", "admin").First(&admin).Error)
	require.NoError(t, db.Create(&model.SysUser{
		Base:         model.Base{ID: 90002, TenantID: admin.TenantID},
		Username:     "OpsUser",
		Nickname:     "运维",
		PasswordHash: admin.PasswordHash,
		Status:       model.UserStatusActive,
	}).Error)

	err := userRename(ctx, db, "admin", "opsuser")
	require.Error(t, err)
	assert.Equal(t, errs.CodeConflict, errs.Code(err))

	var unchanged model.SysUser
	require.NoError(t, db.Where("id = ?", admin.ID).First(&unchanged).Error)
	assert.Equal(t, "admin", unchanged.Username)
}

func TestUserRenameRejectsInvalidName(t *testing.T) {
	ctx := context.Background()
	db := newRenameDB(t)

	for _, name := range []string{"", "1bad", "ab", "bad name"} {
		err := userRename(ctx, db, "admin", name)
		require.Error(t, err, "用户名 %q 应被拒绝", name)
		assert.Equal(t, errs.CodeInvalidParam, errs.Code(err), "用户名 %q", name)
	}
}
