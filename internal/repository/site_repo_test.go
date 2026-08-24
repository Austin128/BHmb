package repository_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/novapanel/novapanel/internal/model"
	"github.com/novapanel/novapanel/internal/pkg/errs"
	"github.com/novapanel/novapanel/internal/repository"
)

const (
	tenantA = int64(1)
	tenantB = int64(2)
)

// newSiteRepo 用单调递增的假雪花生成器，测试里 id 可预期且不重复。
func newSiteRepo(db *gorm.DB) repository.SiteRepository {
	var seq int64 = 1000
	return repository.NewSiteRepository(db, func() int64 { return atomic.AddInt64(&seq, 1) })
}

func newSite(id, tenantID int64, name string) *model.WebSite {
	now := time.Now().UTC()
	return &model.WebSite{
		Base:          model.Base{ID: id, TenantID: tenantID, CreatedBy: 9, CreatedAt: now, UpdatedAt: now},
		Name:          name,
		Type:          model.SiteTypeStatic,
		ServerType:    model.ServerTypeNginx,
		RootPath:      "/www/wwwroot/" + name,
		IndexFiles:    "index.html",
		Status:        model.SiteStatusRunning,
		ConfigVersion: 1,
	}
}

func domain(name string, port int, primary bool) model.WebSiteDomain {
	now := time.Now().UTC()
	return model.WebSiteDomain{
		Base:      model.Base{CreatedAt: now, UpdatedAt: now},
		Domain:    name,
		Port:      port,
		IsPrimary: primary,
	}
}

func TestSiteRepoTenantIsolation(t *testing.T) {
	ctx := context.Background()
	repo := newSiteRepo(setup(t))

	require.NoError(t, repo.Create(ctx, newSite(11, tenantA, "shop"), []model.WebSiteDomain{domain("shop.example.com", 80, true)}))
	require.NoError(t, repo.Create(ctx, newSite(12, tenantB, "blog"), []model.WebSiteDomain{domain("blog.example.com", 80, true)}))

	_, err := repo.FindByID(ctx, tenantB, 11)
	require.Error(t, err)
	assert.Equal(t, errs.CodeSiteNotFound, errs.Code(err), "跨租户读取必须当作站点不存在")

	_, err = repo.FindByName(ctx, tenantB, "shop")
	require.Error(t, err)
	assert.Equal(t, errs.CodeSiteNotFound, errs.Code(err))

	list, total, err := repo.List(ctx, tenantA, "", 0, 20)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	require.Len(t, list, 1)
	assert.Equal(t, "shop", list[0].Name)

	n, err := repo.Count(ctx, tenantB)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)
}

func TestSiteRepoListKeywordMatchesNameRemarkAndDomain(t *testing.T) {
	ctx := context.Background()
	repo := newSiteRepo(setup(t))

	shop := newSite(21, tenantA, "shop")
	shop.Remark = "线上商城"
	require.NoError(t, repo.Create(ctx, shop, []model.WebSiteDomain{domain("mall.example.com", 80, true)}))
	require.NoError(t, repo.Create(ctx, newSite(22, tenantA, "docs"), []model.WebSiteDomain{domain("docs.example.com", 80, true)}))

	byName, _, err := repo.List(ctx, tenantA, "sho", 0, 20)
	require.NoError(t, err)
	require.Len(t, byName, 1)
	assert.Equal(t, "shop", byName[0].Name)

	byRemark, _, err := repo.List(ctx, tenantA, "商城", 0, 20)
	require.NoError(t, err)
	require.Len(t, byRemark, 1)

	byDomain, _, err := repo.List(ctx, tenantA, "mall.example", 0, 20)
	require.NoError(t, err)
	require.Len(t, byDomain, 1)
	assert.Equal(t, "shop", byDomain[0].Name)
}

func TestSiteRepoDomainConflictIsGlobal(t *testing.T) {
	ctx := context.Background()
	repo := newSiteRepo(setup(t))

	require.NoError(t, repo.Create(ctx, newSite(31, tenantA, "shop"),
		[]model.WebSiteDomain{domain("shop.example.com", 80, true)}))

	// 同一台机器上 server_name 不分租户，别的租户占用同样的域名+端口也必须算冲突。
	hit, err := repo.FindDomainConflict(ctx, []model.WebSiteDomain{domain("shop.example.com", 80, true)}, 0)
	require.NoError(t, err)
	require.NotNil(t, hit)
	assert.EqualValues(t, 31, hit.SiteID)

	// 端口不同不算冲突。
	hit, err = repo.FindDomainConflict(ctx, []model.WebSiteDomain{domain("shop.example.com", 8080, true)}, 0)
	require.NoError(t, err)
	assert.Nil(t, hit)

	// 排除自身后，站点更新自己的域名不会被自己拦住。
	hit, err = repo.FindDomainConflict(ctx, []model.WebSiteDomain{domain("shop.example.com", 80, true)}, 31)
	require.NoError(t, err)
	assert.Nil(t, hit)
}

func TestSiteRepoReplaceDomainsFreesOldBindings(t *testing.T) {
	ctx := context.Background()
	db := setup(t)
	repo := newSiteRepo(db)

	site := newSite(41, tenantA, "shop")
	require.NoError(t, repo.Create(ctx, site, []model.WebSiteDomain{domain("old.example.com", 80, true)}))
	require.NoError(t, repo.ReplaceDomains(ctx, site, []model.WebSiteDomain{domain("new.example.com", 80, true)}))

	domains, err := repo.DomainsOfSite(ctx, site.ID)
	require.NoError(t, err)
	require.Len(t, domains, 1)
	assert.Equal(t, "new.example.com", domains[0].Domain)

	// 旧绑定必须硬删，否则唯一索引（含 deleted_at）会挡住其它站点复用该域名。
	other := newSite(42, tenantA, "legacy")
	require.NoError(t, repo.Create(ctx, other, []model.WebSiteDomain{domain("old.example.com", 80, true)}))
}

func TestSiteRepoDeleteReleasesDomainsAndKeepsTenantScope(t *testing.T) {
	ctx := context.Background()
	db := setup(t)
	repo := newSiteRepo(db)

	site := newSite(51, tenantA, "shop")
	require.NoError(t, repo.Create(ctx, site, []model.WebSiteDomain{domain("shop.example.com", 80, true)}))
	require.NoError(t, repo.AppendConfig(ctx, &model.WebSiteConfig{
		Base:    model.Base{TenantID: tenantA, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()},
		SiteID:  site.ID,
		Content: "server {}",
	}))

	require.NoError(t, repo.Delete(ctx, site))

	_, err := repo.FindByID(ctx, tenantA, site.ID)
	assert.Equal(t, errs.CodeSiteNotFound, errs.Code(err))

	// 删除后域名立即可被复用。
	require.NoError(t, repo.Create(ctx, newSite(52, tenantB, "shop2"),
		[]model.WebSiteDomain{domain("shop.example.com", 80, true)}))

	// 配置版本是软删：审计仍能查到历史，但常规查询不再返回。
	var live int64
	require.NoError(t, db.Model(&model.WebSiteConfig{}).Where("site_id = ?", site.ID).Count(&live).Error)
	assert.Zero(t, live)
	var archived int64
	require.NoError(t, db.Unscoped().Model(&model.WebSiteConfig{}).Where("site_id = ?", site.ID).Count(&archived).Error)
	assert.EqualValues(t, 1, archived)
}

func TestAppendConfigAssignsSequentialVersions(t *testing.T) {
	ctx := context.Background()
	repo := newSiteRepo(setup(t))

	site := newSite(61, tenantA, "shop")
	require.NoError(t, repo.Create(ctx, site, []model.WebSiteDomain{domain("shop.example.com", 80, true)}))

	for want := 1; want <= 3; want++ {
		cfg := &model.WebSiteConfig{
			Base:    model.Base{TenantID: tenantA, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()},
			SiteID:  site.ID,
			Content: fmt.Sprintf("server { # v%d }", want),
		}
		require.NoError(t, repo.AppendConfig(ctx, cfg))
		assert.Equal(t, want, cfg.Version)
		assert.NotZero(t, cfg.ID)
	}
}

func TestAppendConfigIsSafeUnderConcurrency(t *testing.T) {
	ctx := context.Background()
	repo := newSiteRepo(setup(t))

	site := newSite(71, tenantA, "shop")
	require.NoError(t, repo.Create(ctx, site, []model.WebSiteDomain{domain("shop.example.com", 80, true)}))

	const writers = 6
	var wg sync.WaitGroup
	errCh := make(chan error, writers)
	versions := make(chan int, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cfg := &model.WebSiteConfig{
				Base:    model.Base{TenantID: tenantA, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()},
				SiteID:  site.ID,
				Content: fmt.Sprintf("server { # writer %d }", i),
			}
			if err := repo.AppendConfig(ctx, cfg); err != nil {
				errCh <- err
				return
			}
			versions <- cfg.Version
		}(i)
	}
	wg.Wait()
	close(errCh)
	close(versions)

	for err := range errCh {
		require.NoError(t, err, "并发追加配置版本不得因撞号失败")
	}
	seen := map[int]bool{}
	for v := range versions {
		require.False(t, seen[v], "版本号 %d 被分配了两次", v)
		seen[v] = true
	}
	assert.Len(t, seen, writers)
}

func TestMarkConfigDeployedKeepsSingleCurrent(t *testing.T) {
	ctx := context.Background()
	repo := newSiteRepo(setup(t))

	site := newSite(81, tenantA, "shop")
	require.NoError(t, repo.Create(ctx, site, []model.WebSiteDomain{domain("shop.example.com", 80, true)}))

	appendCfg := func(content string) *model.WebSiteConfig {
		cfg := &model.WebSiteConfig{
			Base:    model.Base{TenantID: tenantA, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()},
			SiteID:  site.ID,
			Content: content,
		}
		require.NoError(t, repo.AppendConfig(ctx, cfg))
		return cfg
	}

	v1 := appendCfg("server { # v1 }")
	require.NoError(t, repo.MarkConfigDeployed(ctx, v1, time.Now().UTC()))
	cur, err := repo.CurrentConfig(ctx, site.ID)
	require.NoError(t, err)
	require.NotNil(t, cur)
	assert.Equal(t, 1, cur.Version)

	v2 := appendCfg("server { # v2 }")
	require.NoError(t, repo.MarkConfigDeployed(ctx, v2, time.Now().UTC()))
	cur, err = repo.CurrentConfig(ctx, site.ID)
	require.NoError(t, err)
	require.NotNil(t, cur)
	assert.Equal(t, 2, cur.Version, "只能有一个当前版本")

	v3 := appendCfg("server { # v3 }")
	require.NoError(t, repo.MarkConfigFailed(ctx, v3, "nginx -t 失败"))
	cur, err = repo.CurrentConfig(ctx, site.ID)
	require.NoError(t, err)
	require.NotNil(t, cur)
	assert.Equal(t, 2, cur.Version, "下发失败不得顶掉上一个生效版本")

	history, err := repo.ListConfigs(ctx, site.ID, 10)
	require.NoError(t, err)
	require.Len(t, history, 3)
	assert.Equal(t, 3, history[0].Version, "历史按版本倒序")
	assert.Equal(t, model.DeployStatusFailed, history[0].DeployStatus)
	assert.Equal(t, "nginx -t 失败", history[0].DeployError)

	got, err := repo.FindConfig(ctx, site.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, "server { # v1 }", got.Content)

	_, err = repo.FindConfig(ctx, site.ID, 99)
	require.Error(t, err)
}
