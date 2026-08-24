package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/novapanel/novapanel/internal/model"
	"github.com/novapanel/novapanel/internal/pkg/errs"
)

// SiteRepository 提供站点、域名与配置版本的持久化。
// 站点是聚合根：域名与配置版本只随站点操作变更，因此写入方法都以站点为单位提供事务语义。
type SiteRepository interface {
	// Create 在一个事务内写入站点与其域名，域名唯一冲突返回 CodeConflict。
	Create(ctx context.Context, site *model.WebSite, domains []model.WebSiteDomain) error
	// FindByID 按主键取站点，限定租户避免跨租户读取。
	FindByID(ctx context.Context, tenantID, id int64) (*model.WebSite, error)
	// FindByName 按站点标识取站点，用于重名校验与按名操作。
	FindByName(ctx context.Context, tenantID int64, name string) (*model.WebSite, error)
	// List 分页列出本租户站点，keyword 命中站点名、备注与域名。
	List(ctx context.Context, tenantID int64, keyword string, offset, limit int) ([]model.WebSite, int64, error)
	// Count 返回本租户站点总数，供总览页统计。
	Count(ctx context.Context, tenantID int64) (int64, error)
	// DomainsOfSite 返回单个站点的域名，主域名在前。
	DomainsOfSite(ctx context.Context, siteID int64) ([]model.WebSiteDomain, error)
	// DomainsOfSites 批量取多个站点的域名，避开列表页 N+1。
	DomainsOfSites(ctx context.Context, siteIDs []int64) (map[int64][]model.WebSiteDomain, error)
	// FindDomainConflict 返回与给定「域名+端口」冲突且不属于 excludeSiteID 的绑定。
	FindDomainConflict(ctx context.Context, domains []model.WebSiteDomain, excludeSiteID int64) (*model.WebSiteDomain, error)
	// ReplaceDomains 用给定集合整体替换站点域名。
	ReplaceDomains(ctx context.Context, site *model.WebSite, domains []model.WebSiteDomain) error
	// UpdateSite 保存站点可变字段，不触碰域名与配置版本。
	UpdateSite(ctx context.Context, site *model.WebSite) error
	// Delete 软删除站点及其域名与配置版本。
	Delete(ctx context.Context, site *model.WebSite) error
	// AppendConfig 分配下一个版本号并追加一条配置版本记录。
	// 版本号与写入在同一次调用内完成，唯一冲突时重算重试，避免并发保存撞号。
	AppendConfig(ctx context.Context, cfg *model.WebSiteConfig) error
	// MarkConfigDeployed 将指定版本标记为已生效，并把该站点其余版本置为非当前。
	MarkConfigDeployed(ctx context.Context, cfg *model.WebSiteConfig, at time.Time) error
	// MarkConfigFailed 记录下发失败原因，保留上一版本的 is_current。
	MarkConfigFailed(ctx context.Context, cfg *model.WebSiteConfig, reason string) error
	// CurrentConfig 返回站点当前生效配置，无记录时返回 nil。
	CurrentConfig(ctx context.Context, siteID int64) (*model.WebSiteConfig, error)
	// ListConfigs 返回站点最近的配置版本，倒序。
	ListConfigs(ctx context.Context, siteID int64, limit int) ([]model.WebSiteConfig, error)
	// FindConfig 按版本号取配置，供回滚使用。
	FindConfig(ctx context.Context, siteID int64, version int) (*model.WebSiteConfig, error)
	// FindRewrite 返回站点当前伪静态规则，无记录时返回 nil。
	FindRewrite(ctx context.Context, siteID int64) (*model.WebRewriteRule, error)
	// SaveRewrite 以「一个站点一条规则」的语义写入伪静态：存在则更新，不存在则插入。
	SaveRewrite(ctx context.Context, rule *model.WebRewriteRule) error
	// DeleteRewrite 硬删站点伪静态规则。唯一索引含 deleted_at，软删会占住站点。
	DeleteRewrite(ctx context.Context, siteID int64) error
}

type siteRepo struct {
	db *gorm.DB
	// nextID 由服务层注入雪花生成器，仓储不自行决定主键来源。
	nextID func() int64
}

// NewSiteRepository 构造站点仓储，nextID 用于生成域名与配置版本的主键。
func NewSiteRepository(db *gorm.DB, nextID func() int64) SiteRepository {
	return &siteRepo{db: db, nextID: nextID}
}

func (r *siteRepo) Create(ctx context.Context, site *model.WebSite, domains []model.WebSiteDomain) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(site).Error; err != nil {
			return errs.Wrap(err, errs.CodeInternal, "站点写入失败")
		}
		return r.insertDomains(tx, site, domains)
	})
}

// insertDomains 落域名绑定。主键与站点、租户字段统一在此补齐，避免调用方漏填。
func (r *siteRepo) insertDomains(tx *gorm.DB, site *model.WebSite, domains []model.WebSiteDomain) error {
	if len(domains) == 0 {
		return nil
	}
	rows := make([]model.WebSiteDomain, 0, len(domains))
	for i := range domains {
		d := domains[i]
		if d.ID == 0 {
			d.ID = r.nextID()
		}
		d.SiteID = site.ID
		d.TenantID = site.TenantID
		d.CreatedBy = site.CreatedBy
		rows = append(rows, d)
	}
	if err := tx.Create(&rows).Error; err != nil {
		return errs.Wrap(err, errs.CodeConflict, "域名绑定写入失败，可能与已有站点重复")
	}
	return nil
}

func (r *siteRepo) FindByID(ctx context.Context, tenantID, id int64) (*model.WebSite, error) {
	var s model.WebSite
	err := r.db.WithContext(ctx).Where("id = ? AND tenant_id = ?", id, tenantID).First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// 站点缺失使用网站模块专属错误码（docs/05 300003），便于前端区分
		// 「站点被删除」与其它通用 404。
		return nil, errs.New(errs.CodeSiteNotFound, "站点不存在")
	}
	if err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "站点查询失败")
	}
	return &s, nil
}

func (r *siteRepo) FindByName(ctx context.Context, tenantID int64, name string) (*model.WebSite, error) {
	var s model.WebSite
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND name = ?", tenantID, name).First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errs.New(errs.CodeSiteNotFound, "站点不存在")
	}
	if err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "站点查询失败")
	}
	return &s, nil
}

// List 按 updated_at 倒序：站点列表关注的是最近动过哪些站（docs/07 7.15.3）。
func (r *siteRepo) List(ctx context.Context, tenantID int64, keyword string, offset, limit int) ([]model.WebSite, int64, error) {
	q := r.db.WithContext(ctx).Model(&model.WebSite{}).Where("tenant_id = ?", tenantID)
	if keyword != "" {
		like := "%" + keyword + "%"
		sub := r.db.WithContext(ctx).Model(&model.WebSiteDomain{}).
			Select("site_id").Where("domain LIKE ?", like)
		q = q.Where("name LIKE ? OR remark LIKE ? OR id IN (?)", like, like, sub)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, errs.Wrap(err, errs.CodeInternal, "站点数量统计失败")
	}
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	var list []model.WebSite
	if err := q.Order("updated_at DESC").Offset(offset).Limit(limit).Find(&list).Error; err != nil {
		return nil, 0, errs.Wrap(err, errs.CodeInternal, "站点列表查询失败")
	}
	return list, total, nil
}

func (r *siteRepo) Count(ctx context.Context, tenantID int64) (int64, error) {
	var n int64
	if err := r.db.WithContext(ctx).Model(&model.WebSite{}).Where("tenant_id = ?", tenantID).Count(&n).Error; err != nil {
		return 0, errs.Wrap(err, errs.CodeInternal, "站点数量统计失败")
	}
	return n, nil
}

func (r *siteRepo) DomainsOfSite(ctx context.Context, siteID int64) ([]model.WebSiteDomain, error) {
	var list []model.WebSiteDomain
	err := r.db.WithContext(ctx).Where("site_id = ?", siteID).
		Order("is_primary DESC, id ASC").Find(&list).Error
	if err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "站点域名查询失败")
	}
	return list, nil
}

func (r *siteRepo) DomainsOfSites(ctx context.Context, siteIDs []int64) (map[int64][]model.WebSiteDomain, error) {
	out := make(map[int64][]model.WebSiteDomain, len(siteIDs))
	if len(siteIDs) == 0 {
		return out, nil
	}
	var list []model.WebSiteDomain
	err := r.db.WithContext(ctx).Where("site_id IN ?", siteIDs).
		Order("is_primary DESC, id ASC").Find(&list).Error
	if err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "站点域名查询失败")
	}
	for i := range list {
		out[list[i].SiteID] = append(out[list[i].SiteID], list[i])
	}
	return out, nil
}

// FindDomainConflict 逐项比对「域名+端口」。域名唯一约束是全局的（不带 tenant_id），
// 因为同一台机器上 Nginx 的 server_name 不区分租户，冲突必须跨租户拦截。
func (r *siteRepo) FindDomainConflict(ctx context.Context, domains []model.WebSiteDomain, excludeSiteID int64) (*model.WebSiteDomain, error) {
	if len(domains) == 0 {
		return nil, nil
	}
	q := r.db.WithContext(ctx).Model(&model.WebSiteDomain{})
	cond := r.db.WithContext(ctx).Session(&gorm.Session{NewDB: true})
	for i := range domains {
		cond = cond.Or("domain = ? AND port = ?", domains[i].Domain, domains[i].Port)
	}
	q = q.Where(cond)
	if excludeSiteID != 0 {
		q = q.Where("site_id <> ?", excludeSiteID)
	}
	var hit model.WebSiteDomain
	err := q.First(&hit).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "域名冲突检测失败")
	}
	return &hit, nil
}

func (r *siteRepo) ReplaceDomains(ctx context.Context, site *model.WebSite, domains []model.WebSiteDomain) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 硬删旧绑定：唯一索引含 deleted_at，软删记录会让同名域名再次绑定时冲突。
		if err := tx.Unscoped().Where("site_id = ?", site.ID).Delete(&model.WebSiteDomain{}).Error; err != nil {
			return errs.Wrap(err, errs.CodeInternal, "旧域名清理失败")
		}
		return r.insertDomains(tx, site, domains)
	})
}

func (r *siteRepo) UpdateSite(ctx context.Context, site *model.WebSite) error {
	err := r.db.WithContext(ctx).Model(&model.WebSite{}).
		Where("id = ? AND tenant_id = ?", site.ID, site.TenantID).
		Select("type", "root_path", "index_files", "php_version", "runtime_id",
			"proxy_target", "proxy_host", "proxy_websocket",
			"listen_port", "ssl_port", "ssl_enabled", "cert_id", "force_https",
			"http2_enabled", "http3_enabled", "access_log_enabled", "error_log_enabled",
			"access_log_path", "error_log_path", "rate_limit_json", "settings_json",
			"status", "config_version",
			"expire_at", "group_name", "remark", "updated_at").
		Updates(site).Error
	if err != nil {
		return errs.Wrap(err, errs.CodeInternal, "站点更新失败")
	}
	return nil
}

func (r *siteRepo) Delete(ctx context.Context, site *model.WebSite) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 域名硬删：唯一索引包含 deleted_at，留着软删记录会占住域名。
		if err := tx.Unscoped().Where("site_id = ?", site.ID).Delete(&model.WebSiteDomain{}).Error; err != nil {
			return errs.Wrap(err, errs.CodeInternal, "站点域名删除失败")
		}
		if err := tx.Where("site_id = ?", site.ID).Delete(&model.WebSiteConfig{}).Error; err != nil {
			return errs.Wrap(err, errs.CodeInternal, "站点配置删除失败")
		}
		// 伪静态硬删：唯一索引含 deleted_at，软删记录会让同名站点重建时冲突。
		if err := tx.Unscoped().Where("site_id = ?", site.ID).
			Delete(&model.WebRewriteRule{}).Error; err != nil {
			return errs.Wrap(err, errs.CodeInternal, "站点伪静态删除失败")
		}
		if err := tx.Where("id = ? AND tenant_id = ?", site.ID, site.TenantID).
			Delete(&model.WebSite{}).Error; err != nil {
			return errs.Wrap(err, errs.CodeInternal, "站点删除失败")
		}
		return nil
	})
}

// configVersionAttempts 兜底重试次数：正常路径靠站点行锁串行化版本分配，
// 只有跨进程死锁或锁超时才会走到重试。
const configVersionAttempts = 5

// AppendConfig 追加一个配置版本。版本号必须连续且唯一，因此在同一事务里
// 先锁住站点行再取 MAX(version)+1：单纯的"查最大值再插入 + 撞号重试"在
// MySQL/PostgreSQL 的行级并发下会连续撞号，重试次数用尽后误报冲突。
func (r *siteRepo) AppendConfig(ctx context.Context, cfg *model.WebSiteConfig) error {
	if cfg.ConfigType == "" {
		cfg.ConfigType = model.SiteConfigTypeNginx
	}
	var lastErr error
	for attempt := 0; attempt < configVersionAttempts; attempt++ {
		err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := lockSite(tx, cfg.SiteID); err != nil {
				return err
			}
			version, err := nextConfigVersion(tx, cfg.SiteID, cfg.ConfigType)
			if err != nil {
				return err
			}
			cfg.Version = version
			// 重试要换新主键：失败的那次 INSERT 可能已经消耗掉这个 id。
			cfg.ID = r.nextID()
			if err := tx.Create(cfg).Error; err != nil {
				return errs.Wrap(err, errs.CodeInternal, "配置版本写入失败")
			}
			return nil
		})
		if err == nil {
			return nil
		}
		lastErr = err
		// 只有撞号、死锁、锁超时这类可重试错误才再试一次。
		if isDuplicatedKey(err) || isRetryableLockErr(err) {
			continue
		}
		return err
	}
	return errs.Wrap(lastErr, errs.CodeConflict, "配置版本冲突，请重试")
}

// lockSite 排他锁定站点行，把同一站点的版本分配串成一条队列。
// SQLite 不支持 SELECT ... FOR UPDATE，但其写事务本身是独占的（BEGIN IMMEDIATE），
// 因此只需确认站点存在。
func lockSite(tx *gorm.DB, siteID int64) error {
	q := tx.Model(&model.WebSite{}).Where("id = ?", siteID)
	if tx.Dialector.Name() != DriverSQLite {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var ids []int64
	if err := q.Limit(1).Pluck("id", &ids).Error; err != nil {
		return errs.Wrap(err, errs.CodeInternal, "站点加锁失败")
	}
	if len(ids) == 0 {
		return errs.New(errs.CodeSiteNotFound, "站点不存在")
	}
	return nil
}

func nextConfigVersion(tx *gorm.DB, siteID int64, configType string) (int, error) {
	var max *int
	err := tx.Model(&model.WebSiteConfig{}).
		Where("site_id = ? AND config_type = ?", siteID, configType).
		Select("MAX(version)").Scan(&max).Error
	if err != nil {
		return 0, errs.Wrap(err, errs.CodeInternal, "配置版本号查询失败")
	}
	if max == nil {
		return 1, nil
	}
	return *max + 1, nil
}

// isRetryableLockErr 覆盖三种数据库的死锁与锁等待超时，这类错误重试即可恢复。
func isRetryableLockErr(err error) bool {
	msg := err.Error()
	for _, s := range []string{
		"database is locked",         // sqlite
		"Deadlock found",             // mysql
		"Lock wait timeout",          // mysql
		"deadlock detected",          // postgres
		"could not serialize access", // postgres
	} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

// isDuplicatedKey 兼容两种情况：GORM 开启 TranslateError 时返回 ErrDuplicatedKey，
// 未开启时只能看驱动原文，三种数据库的唯一冲突文案各不相同。
func isDuplicatedKey(err error) bool {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := err.Error()
	for _, s := range []string{
		"UNIQUE constraint failed", // sqlite
		"Duplicate entry",          // mysql
		"duplicate key value",      // postgres
		"SQLSTATE 23505",           // postgres（驱动包装后）
	} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

func (r *siteRepo) MarkConfigDeployed(ctx context.Context, cfg *model.WebSiteConfig, at time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.WebSiteConfig{}).
			Where("site_id = ? AND config_type = ?", cfg.SiteID, cfg.ConfigType).
			Update("is_current", false).Error; err != nil {
			return errs.Wrap(err, errs.CodeInternal, "旧配置版本状态更新失败")
		}
		err := tx.Model(&model.WebSiteConfig{}).Where("id = ?", cfg.ID).
			Updates(map[string]any{
				"is_current":    true,
				"deploy_status": model.DeployStatusDeployed,
				"deploy_at":     at,
				"deploy_error":  "",
			}).Error
		if err != nil {
			return errs.Wrap(err, errs.CodeInternal, "配置版本状态更新失败")
		}
		return nil
	})
}

func (r *siteRepo) MarkConfigFailed(ctx context.Context, cfg *model.WebSiteConfig, reason string) error {
	// 失败原因常含 nginx -t 的多行输出，按列宽截断避免写入报错。
	if len(reason) > 500 {
		reason = reason[:500]
	}
	err := r.db.WithContext(ctx).Model(&model.WebSiteConfig{}).Where("id = ?", cfg.ID).
		Updates(map[string]any{
			"deploy_status": model.DeployStatusFailed,
			"deploy_error":  reason,
			"is_current":    false,
		}).Error
	if err != nil {
		return errs.Wrap(err, errs.CodeInternal, "配置失败状态更新失败")
	}
	return nil
}

func (r *siteRepo) CurrentConfig(ctx context.Context, siteID int64) (*model.WebSiteConfig, error) {
	var cfg model.WebSiteConfig
	err := r.db.WithContext(ctx).
		Where("site_id = ? AND is_current = ?", siteID, true).First(&cfg).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "当前配置查询失败")
	}
	return &cfg, nil
}

func (r *siteRepo) ListConfigs(ctx context.Context, siteID int64, limit int) ([]model.WebSiteConfig, error) {
	if limit <= 0 {
		limit = 20
	}
	var list []model.WebSiteConfig
	err := r.db.WithContext(ctx).Where("site_id = ?", siteID).
		Order("version DESC").Limit(limit).Find(&list).Error
	if err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "配置版本列表查询失败")
	}
	return list, nil
}

func (r *siteRepo) FindConfig(ctx context.Context, siteID int64, version int) (*model.WebSiteConfig, error) {
	var cfg model.WebSiteConfig
	err := r.db.WithContext(ctx).
		Where("site_id = ? AND version = ?", siteID, version).First(&cfg).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errs.New(errs.CodeNotFound, "配置版本不存在")
	}
	if err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "配置版本查询失败")
	}
	return &cfg, nil
}

func (r *siteRepo) FindRewrite(ctx context.Context, siteID int64) (*model.WebRewriteRule, error) {
	var rule model.WebRewriteRule
	err := r.db.WithContext(ctx).Where("site_id = ?", siteID).
		Order("sort ASC, id ASC").First(&rule).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "伪静态规则查询失败")
	}
	return &rule, nil
}

// SaveRewrite 实现「一个站点一条规则」。uk_rewrite_site 含可空的 deleted_at，
// 存活行的 deleted_at 为 NULL，三种数据库都不会对 NULL 去重，因此唯一索引挡不住
// 并发插入；这里复用站点行锁把同一站点的写入串成队列，与配置版本分配同一套机制。
func (r *siteRepo) SaveRewrite(ctx context.Context, rule *model.WebRewriteRule) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockSite(tx, rule.SiteID); err != nil {
			return err
		}
		var existing model.WebRewriteRule
		err := tx.Where("site_id = ?", rule.SiteID).First(&existing).Error
		switch {
		case err == nil:
			rule.ID = existing.ID
			rule.CreatedAt = existing.CreatedAt
			upd := tx.Model(&model.WebRewriteRule{}).Where("id = ?", existing.ID).
				Select("name", "template_code", "content", "enabled", "sort", "updated_at").
				Updates(rule)
			if upd.Error != nil {
				return errs.Wrap(upd.Error, errs.CodeInternal, "伪静态规则更新失败")
			}
			return nil
		case errors.Is(err, gorm.ErrRecordNotFound):
			if rule.ID == 0 {
				rule.ID = r.nextID()
			}
			if err := tx.Create(rule).Error; err != nil {
				return errs.Wrap(err, errs.CodeInternal, "伪静态规则写入失败")
			}
			return nil
		default:
			return errs.Wrap(err, errs.CodeInternal, "伪静态规则查询失败")
		}
	})
}

func (r *siteRepo) DeleteRewrite(ctx context.Context, siteID int64) error {
	err := r.db.WithContext(ctx).Unscoped().Where("site_id = ?", siteID).
		Delete(&model.WebRewriteRule{}).Error
	if err != nil {
		return errs.Wrap(err, errs.CodeInternal, "伪静态规则删除失败")
	}
	return nil
}
