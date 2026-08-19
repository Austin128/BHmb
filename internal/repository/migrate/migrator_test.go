package migrate

import (
	"context"
	"path/filepath"
	"testing"
	"testing/fstest"

	sqlitedriver "github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/novapanel/novapanel/internal/pkg/errs"
	"github.com/novapanel/novapanel/migrations"
)

// openSQLite 使用文件型 SQLite（不用 :memory:，与生产行为一致）。
func openSQLite(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "nova.db") + "?_pragma=foreign_keys(1)&_txlock=immediate"
	db, err := gorm.Open(sqlitedriver.Open(dsn), &gorm.Config{Logger: gormlogger.Discard})
	require.NoError(t, err)
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func tableExists(t *testing.T, db *gorm.DB, name string) bool {
	t.Helper()
	var n int64
	require.NoError(t, db.Raw(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name = ?", name).Scan(&n).Error)
	return n == 1
}

var coreTables = []string{
	"sys_tenant", "sys_setting", "sys_user", "sys_session",
	"sys_role", "sys_user_role", "sys_permission", "sys_role_permission",
}

func TestUpDownUp(t *testing.T) {
	ctx := context.Background()
	db := openSQLite(t)
	m := New(db, migrations.FS, "sqlite")

	applied, err := m.Up(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, applied)
	for _, tbl := range coreTables {
		assert.True(t, tableExists(t, db, tbl), "up 后应存在表 %s", tbl)
	}

	pending, err := m.Pending(ctx)
	require.NoError(t, err)
	assert.Zero(t, pending)

	again, err := m.Up(ctx)
	require.NoError(t, err)
	assert.Empty(t, again, "重复 up 必须幂等")

	rolled, err := m.Down(ctx, 0)
	require.NoError(t, err)
	assert.Len(t, rolled, len(applied))
	for _, tbl := range coreTables {
		assert.False(t, tableExists(t, db, tbl), "down 后表 %s 应被删除", tbl)
	}
	assert.True(t, tableExists(t, db, tableName), "记录表本身不随迁移删除")

	reapplied, err := m.Up(ctx)
	require.NoError(t, err)
	assert.Len(t, reapplied, len(applied), "down 之后必须能重新 up")
	for _, tbl := range coreTables {
		assert.True(t, tableExists(t, db, tbl))
	}
}

func TestDownSteps(t *testing.T) {
	ctx := context.Background()
	db := openSQLite(t)
	m := New(db, migrations.FS, "sqlite")

	applied, err := m.Up(ctx)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(applied), 2)

	rolled, err := m.Down(ctx, 1)
	require.NoError(t, err)
	require.Len(t, rolled, 1)
	assert.Equal(t, applied[len(applied)-1].Version, rolled[0].Version, "回滚顺序为版本倒序")

	pending, err := m.Pending(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, pending)
}

func TestStatusDetectsTamperedScript(t *testing.T) {
	ctx := context.Background()
	db := openSQLite(t)

	original := fstest.MapFS{
		"20260101000000_plat_create_demo.up.sql":   {Data: []byte("CREATE TABLE demo (id BIGINT NOT NULL, PRIMARY KEY (id));")},
		"20260101000000_plat_create_demo.down.sql": {Data: []byte("DROP TABLE IF EXISTS demo;")},
	}
	require.NoError(t, func() error { _, err := New(db, original, "sqlite").Up(ctx); return err }())

	tampered := fstest.MapFS{
		"20260101000000_plat_create_demo.up.sql":   {Data: []byte("CREATE TABLE demo (id BIGINT NOT NULL, extra TEXT, PRIMARY KEY (id));")},
		"20260101000000_plat_create_demo.down.sql": {Data: []byte("DROP TABLE IF EXISTS demo;")},
	}
	m2 := New(db, tampered, "sqlite")

	st, err := m2.Status(ctx)
	require.NoError(t, err)
	require.Len(t, st, 1)
	assert.True(t, st[0].Applied)
	assert.True(t, st[0].Dirty, "校验和不一致应标记为 dirty")

	_, err = m2.Up(ctx)
	require.Error(t, err, "已应用脚本被修改时必须拒绝继续")
	assert.Contains(t, err.Error(), "被修改")
}

func TestLoadRejectsMissingPair(t *testing.T) {
	db := openSQLite(t)
	only := fstest.MapFS{
		"20260101000000_plat_create_demo.up.sql": {Data: []byte("CREATE TABLE demo (id BIGINT);")},
	}
	_, err := New(db, only, "sqlite").Up(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "缺少 up 或 down 脚本")
}

func TestLoadRejectsEmptyDir(t *testing.T) {
	db := openSQLite(t)
	_, err := New(db, fstest.MapFS{}, "sqlite").Up(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "未找到任何迁移脚本")
}

func TestResolvePrefersDialectOverride(t *testing.T) {
	fsys := fstest.MapFS{
		"20260101000000_plat_create_demo.up.sql":                 {Data: []byte("-- base\nCREATE TABLE demo (id BIGINT);")},
		"20260101000000_plat_create_demo.down.sql":               {Data: []byte("DROP TABLE demo;")},
		"dialect/mysql/20260101000000_plat_create_demo.up.sql":   {Data: []byte("-- mysql\nCREATE TABLE demo (id BIGINT);")},
		"dialect/mysql/20260101000000_plat_create_demo.down.sql": {Data: []byte("DROP TABLE demo;")},
	}

	mysqlM := New(nil, fsys, "mysql")
	list, err := mysqlM.load()
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "dialect/mysql/20260101000000_plat_create_demo.up.sql", list[0].UpPath)

	sqliteM := New(nil, fsys, "sqlite")
	list, err = sqliteM.load()
	require.NoError(t, err)
	assert.Equal(t, "20260101000000_plat_create_demo.up.sql", list[0].UpPath, "无覆盖时回落到根目录脚本")
}

func TestRealMigrationsHaveDialectOverridesForAllDrivers(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		list, err := New(nil, migrations.FS, dialect).load()
		require.NoError(t, err, dialect)
		require.NotEmpty(t, list, dialect)
		for _, mg := range list {
			_, _, err := New(nil, migrations.FS, dialect).read(mg.UpPath)
			require.NoError(t, err, "%s %d", dialect, mg.Version)
			_, _, err = New(nil, migrations.FS, dialect).read(mg.DownPath)
			require.NoError(t, err, "%s %d", dialect, mg.Version)
		}
	}
}

func TestSplitStatements(t *testing.T) {
	stmts, err := SplitStatements("-- 注释\nCREATE TABLE a (id BIGINT);\n\nCREATE INDEX i ON a (id);\n")
	require.NoError(t, err)
	require.Len(t, stmts, 2)
	assert.Equal(t, "CREATE TABLE a (id BIGINT)", stmts[0])
	assert.Equal(t, "CREATE INDEX i ON a (id)", stmts[1])

	_, err = SplitStatements("CREATE TABLE a (id BIGINT)")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "缺少分号")

	_, err = SplitStatements("-- 仅注释\n")
	require.Error(t, err)
	assert.Equal(t, errs.CodeInternal, errs.Code(err))
}
