package repository_test

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/novapanel/novapanel/internal/model"
	"github.com/novapanel/novapanel/internal/pkg/config"
	"github.com/novapanel/novapanel/internal/repository"
	"github.com/novapanel/novapanel/internal/repository/migrate"
	"github.com/novapanel/novapanel/migrations"
)

// AppendConfig 依赖唯一索引冲突重试来分配版本号，而"冲突"的判定各数据库
// 报错文本不同。SQLite 由默认用例覆盖，MySQL/PostgreSQL 需要真实实例：
// 设置以下环境变量后本用例才会运行（指向一次性测试库，会清空业务表）。
//
//	NOVA_TEST_MYSQL_DSN="nova:nova@tcp(127.0.0.1:3306)/nova_test?charset=utf8mb4&parseTime=True&loc=Local"
//	NOVA_TEST_POSTGRES_DSN="host=127.0.0.1 user=nova password=nova dbname=nova_test port=5432 sslmode=disable"
func TestAppendConfigConcurrencyAcrossDialects(t *testing.T) {
	cases := []struct {
		driver string
		env    string
	}{
		{repository.DriverMySQL, "NOVA_TEST_MYSQL_DSN"},
		{repository.DriverPostgres, "NOVA_TEST_POSTGRES_DSN"},
	}

	for _, c := range cases {
		t.Run(c.driver, func(t *testing.T) {
			dsn := os.Getenv(c.env)
			if dsn == "" {
				t.Skipf("未设置 %s，跳过 %s 方言验证", c.env, c.driver)
			}
			db := setupDialect(t, c.driver, dsn)
			repo := newSiteRepo(db)
			ctx := context.Background()

			site := newSite(9101, tenantA, "dialect-shop")
			require.NoError(t, repo.Create(ctx, site, []model.WebSiteDomain{domain("dialect.example.com", 80, true)}))

			const workers = 8
			var wg sync.WaitGroup
			errCh := make(chan error, workers)
			for i := 0; i < workers; i++ {
				wg.Add(1)
				go func(n int) {
					defer wg.Done()
					cfg := &model.WebSiteConfig{
						Base:         model.Base{TenantID: tenantA, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()},
						SiteID:       site.ID,
						ConfigType:   model.SiteConfigTypeNginx,
						Content:      "server { }",
						DeployStatus: model.DeployStatusPending,
						Source:       model.SiteConfigSourcePanel,
					}
					errCh <- repo.AppendConfig(ctx, cfg)
				}(i)
			}
			wg.Wait()
			close(errCh)
			for err := range errCh {
				require.NoError(t, err, "并发写入配置版本不应失败：唯一冲突必须被识别并重试")
			}

			var versions []int
			require.NoError(t, db.Model(&model.WebSiteConfig{}).
				Where("site_id = ?", site.ID).
				Order("version asc").
				Pluck("version", &versions).Error)
			require.Len(t, versions, workers, "%d 个并发追加应各得到一个版本", workers)
			for i, v := range versions {
				assert.Equal(t, i+1, v, "版本号必须连续且唯一")
			}
		})
	}
}

// setupDialect 在真实 MySQL/PostgreSQL 上重建 schema：先全部回滚再迁移，
// 保证同一测试库可以反复使用。
func setupDialect(t *testing.T, driver, dsn string) *gorm.DB {
	t.Helper()
	db, err := repository.Open(&config.DatabaseConfig{
		Driver:          driver,
		DSN:             dsn,
		MaxOpenConns:    8,
		MaxIdleConns:    4,
		ConnMaxLifetime: time.Hour,
	}, 200*time.Millisecond)
	require.NoError(t, err)
	t.Cleanup(func() { _ = repository.Close(db) })

	ctx := context.Background()
	m := migrate.New(db, migrations.FS, repository.Dialect(db))
	total := len(migrationVersions(t))
	_, _ = m.Up(ctx)
	_, err = m.Down(ctx, total)
	require.NoError(t, err)
	_, err = m.Up(ctx)
	require.NoError(t, err)
	return db
}

func migrationVersions(t *testing.T) []string {
	t.Helper()
	entries, err := migrations.FS.ReadDir(".")
	require.NoError(t, err)
	var out []string
	for _, e := range entries {
		if !e.IsDir() && len(e.Name()) > 7 && e.Name()[len(e.Name())-7:] == ".up.sql" {
			out = append(out, e.Name())
		}
	}
	return out
}
