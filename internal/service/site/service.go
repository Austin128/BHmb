// Package site 实现网站全生命周期管理：创建、编辑、启停、删除与配置下发。
// 数据库记录与宿主机配置保持一致的原则是「先落库、后下发、失败即回滚并把失败原因写回版本记录」，
// 因此界面上永远能看到真实的下发状态，而不是乐观显示成功。
package site

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/novapanel/novapanel/internal/model"
	"github.com/novapanel/novapanel/internal/pkg/config"
	"github.com/novapanel/novapanel/internal/pkg/errs"
	"github.com/novapanel/novapanel/internal/repository"
	"github.com/novapanel/novapanel/internal/service/webserver"
)

// Actor 描述操作者，用于写入 created_by 与租户隔离。
type Actor struct {
	UserID   int64
	TenantID int64
}

// Service 是网站模块的业务入口。
type Service struct {
	repo      repository.SiteRepository
	web       *webserver.Manager
	renderer  *Renderer
	cfg       config.WebsiteConfig
	panelPort int
	nextID    func() int64
	// allowedRoots 限制站点根目录范围，取文件模块的白名单，避免建站绕过路径管控。
	allowedRoots []string
}

// NewService 构造网站服务。
func NewService(
	repo repository.SiteRepository,
	web *webserver.Manager,
	renderer *Renderer,
	cfg config.WebsiteConfig,
	panelPort int,
	allowedRoots []string,
	nextID func() int64,
) *Service {
	return &Service{
		repo:         repo,
		web:          web,
		renderer:     renderer,
		cfg:          cfg,
		panelPort:    panelPort,
		nextID:       nextID,
		allowedRoots: allowedRoots,
	}
}

// Environment 为建站前提与默认值，供前端在打开创建表单前展示。
type Environment struct {
	*webserver.Status
	// SupportedTypes 只包含当前真正有模板的站点类型。
	SupportedTypes []string `json:"supportedTypes"`
	// SiteCount 为本租户站点数量，用于总览卡片。
	SiteCount int64 `json:"siteCount"`
}

// Env 返回运行前提。Web 服务未安装时 Installed 为假，前端据此禁用创建入口并给出安装指引。
func (s *Service) Env(ctx context.Context, actor Actor) (*Environment, error) {
	st := s.web.Status(ctx)
	types := s.renderer.SupportedTypes()
	sort.Strings(types)
	count, err := s.repo.Count(ctx, actor.TenantID)
	if err != nil {
		return nil, err
	}
	return &Environment{Status: st, SupportedTypes: types, SiteCount: count}, nil
}

// List 分页列出站点。
func (s *Service) List(ctx context.Context, actor Actor, keyword string, page, size int) (*model.SiteListResult, error) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 || size > 200 {
		size = 20
	}
	rows, total, err := s.repo.List(ctx, actor.TenantID, strings.TrimSpace(keyword), (page-1)*size, size)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(rows))
	for i := range rows {
		ids = append(ids, rows[i].ID)
	}
	domains, err := s.repo.DomainsOfSites(ctx, ids)
	if err != nil {
		return nil, err
	}
	items := make([]model.SiteSummary, 0, len(rows))
	for i := range rows {
		cur, err := s.statusConfig(ctx, rows[i].ID)
		if err != nil {
			return nil, err
		}
		items = append(items, buildSummary(&rows[i], domains[rows[i].ID], cur))
	}
	return &model.SiteListResult{Items: items, Total: total, Page: page, Size: size}, nil
}

// Get 返回站点详情。
func (s *Service) Get(ctx context.Context, actor Actor, id int64) (*model.SiteDetail, error) {
	site, err := s.repo.FindByID(ctx, actor.TenantID, id)
	if err != nil {
		return nil, err
	}
	domains, err := s.repo.DomainsOfSite(ctx, site.ID)
	if err != nil {
		return nil, err
	}
	cur, err := s.statusConfig(ctx, site.ID)
	if err != nil {
		return nil, err
	}
	return s.buildDetail(site, domains, cur), nil
}

// statusConfig 返回用于展示下发状态的配置版本。
// 取最新一条：只要最近一次下发失败，就要把失败状态与原因暴露到详情页，
// 哪怕线上仍跑着上一个成功版本（此时 SiteSummary.ConfigVersion 仍是生效版本，两者不冲突）。
// 最新一条成功时回落到当前生效版本，避免展示一个 pending 中间态。
func (s *Service) statusConfig(ctx context.Context, siteID int64) (*model.WebSiteConfig, error) {
	latest, err := s.repo.ListConfigs(ctx, siteID, 1)
	if err != nil {
		return nil, err
	}
	if len(latest) > 0 && latest[0].DeployStatus == model.DeployStatusFailed {
		return &latest[0], nil
	}
	cur, err := s.repo.CurrentConfig(ctx, siteID)
	if err != nil {
		return nil, err
	}
	if cur != nil {
		return cur, nil
	}
	if len(latest) == 0 {
		return nil, nil
	}
	return &latest[0], nil
}

// Create 创建站点：校验 → 落库 → 建目录 → 渲染并下发。
// 下发失败时保留站点记录（状态 error）并把 nginx -t 输出写入配置版本，用户修正后可重建，
// 这样比直接回滚删除更符合运维习惯：域名与目录的准备工作不必重做。
func (s *Service) Create(ctx context.Context, actor Actor, in *model.CreateSiteRequest) (*model.SiteDetail, error) {
	if err := s.requireWebServer(ctx); err != nil {
		return nil, err
	}
	name := strings.ToLower(strings.TrimSpace(in.Name))
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	siteType := strings.TrimSpace(in.Type)
	if siteType == "" {
		siteType = model.SiteTypeStatic
	}
	if !s.typeSupported(siteType) {
		return nil, errs.Newf(errs.CodeUnsupported, "暂不支持的站点类型：%s", siteType)
	}
	if len(in.Remark) > 255 {
		return nil, errs.New(errs.CodeInvalidParam, "备注不能超过 255 个字符")
	}

	if _, err := s.repo.FindByName(ctx, actor.TenantID, name); err == nil {
		return nil, errs.Newf(errs.CodeConflict, "站点标识 %s 已存在", name)
	} else if errs.Code(err) != errs.CodeSiteNotFound {
		return nil, err
	}

	domains, err := s.normalizeDomains(in.Domains)
	if err != nil {
		return nil, err
	}
	if err := s.checkDomainConflict(ctx, domains, 0); err != nil {
		return nil, err
	}

	root := strings.TrimSpace(in.Root)
	if root == "" {
		root = filepath.Join(s.cfg.WwwRoot, name)
	}
	root, err = ValidateRoot(root, s.allowedRoots)
	if err != nil {
		return nil, err
	}

	proxyTarget, proxyHost := "", ""
	if siteType == model.SiteTypeProxy {
		if proxyTarget, err = ValidateProxyTarget(in.ProxyTarget, s.panelPort); err != nil {
			return nil, err
		}
		proxyHost = strings.TrimSpace(in.ProxyHost)
	}

	expire, err := parseExpire(in.ExpireAt)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	site := &model.WebSite{
		Base:             model.Base{ID: s.nextID(), CreatedBy: actor.UserID, TenantID: actor.TenantID, CreatedAt: now, UpdatedAt: now},
		Name:             name,
		Type:             siteType,
		ServerType:       s.serverKind(ctx),
		RootPath:         root,
		IndexFiles:       strings.Join(ParseIndexFiles(in.IndexFiles, siteType), " "),
		ProxyTarget:      proxyTarget,
		ProxyHost:        proxyHost,
		ProxyWebSocket:   in.ProxyWebSocket,
		ListenPort:       domains[0].Port,
		SSLPort:          443,
		HTTP2Enabled:     true,
		AccessLogEnabled: in.AccessLogEnabled,
		ErrorLogEnabled:  in.ErrorLogEnabled,
		AccessLogPath:    s.accessLogPath(name),
		ErrorLogPath:     s.errorLogPath(name),
		Status:           model.SiteStatusDeploying,
		ConfigVersion:    0,
		ExpireAt:         expire,
		GroupName:        defaultString(in.GroupName, "default"),
		Remark:           in.Remark,
	}
	if err := s.repo.Create(ctx, site, domains); err != nil {
		return nil, err
	}

	if err := s.prepareRoot(site, in.CreateDefaultPage); err != nil {
		// 目录准备失败不留下半成品：站点尚未下发任何配置，删除记录比留错误状态更干净。
		_ = s.repo.Delete(ctx, site)
		return nil, err
	}

	if err := s.deploy(ctx, actor, site, domains, model.SiteConfigSourcePanel, "创建站点"); err != nil {
		return nil, err
	}
	return s.Get(ctx, actor, site.ID)
}

// Update 编辑站点并重新下发配置。
func (s *Service) Update(ctx context.Context, actor Actor, id int64, in *model.UpdateSiteRequest) (*model.SiteDetail, error) {
	site, err := s.repo.FindByID(ctx, actor.TenantID, id)
	if err != nil {
		return nil, err
	}

	domains, err := s.normalizeDomains(in.Domains)
	if err != nil {
		return nil, err
	}
	if err := s.checkDomainConflict(ctx, domains, site.ID); err != nil {
		return nil, err
	}

	if root := strings.TrimSpace(in.Root); root != "" && root != site.RootPath {
		clean, err := ValidateRoot(root, s.allowedRoots)
		if err != nil {
			return nil, err
		}
		site.RootPath = clean
		if err := s.ensureDir(clean); err != nil {
			return nil, err
		}
	}
	if site.Type == model.SiteTypeProxy {
		if in.ProxyTarget != "" {
			target, err := ValidateProxyTarget(in.ProxyTarget, s.panelPort)
			if err != nil {
				return nil, err
			}
			site.ProxyTarget = target
		}
		site.ProxyHost = strings.TrimSpace(in.ProxyHost)
		if in.ProxyWebSocket != nil {
			site.ProxyWebSocket = *in.ProxyWebSocket
		}
	}
	if len(in.IndexFiles) > 0 {
		site.IndexFiles = strings.Join(ParseIndexFiles(in.IndexFiles, site.Type), " ")
	}
	if in.ExpireAt != nil {
		expire, err := parseExpire(*in.ExpireAt)
		if err != nil {
			return nil, err
		}
		site.ExpireAt = expire
	}
	if in.AccessLogEnabled != nil {
		site.AccessLogEnabled = *in.AccessLogEnabled
	}
	if in.ErrorLogEnabled != nil {
		site.ErrorLogEnabled = *in.ErrorLogEnabled
	}
	if len(in.Remark) > 255 {
		return nil, errs.New(errs.CodeInvalidParam, "备注不能超过 255 个字符")
	}
	site.Remark = in.Remark
	site.GroupName = defaultString(in.GroupName, site.GroupName)
	site.ListenPort = domains[0].Port
	site.UpdatedAt = time.Now()

	if err := s.repo.ReplaceDomains(ctx, site, domains); err != nil {
		return nil, err
	}
	if err := s.repo.UpdateSite(ctx, site); err != nil {
		return nil, err
	}

	// 停用中的站点只更新数据，不恢复线上配置，避免「编辑」意外把站点重新放出去。
	if site.Status == model.SiteStatusStopped {
		return s.Get(ctx, actor, site.ID)
	}
	if err := s.deploy(ctx, actor, site, domains, model.SiteConfigSourcePanel, "修改站点"); err != nil {
		return nil, err
	}
	return s.Get(ctx, actor, site.ID)
}

// ChangeStatus 启停站点。停用即移除 vhost，域名不再被本站点响应；启用则重新下发。
func (s *Service) ChangeStatus(ctx context.Context, actor Actor, id int64, target string) (*model.SiteDetail, error) {
	site, err := s.repo.FindByID(ctx, actor.TenantID, id)
	if err != nil {
		return nil, err
	}
	switch target {
	case model.SiteStatusRunning:
		domains, err := s.repo.DomainsOfSite(ctx, site.ID)
		if err != nil {
			return nil, err
		}
		if err := s.requireWebServer(ctx); err != nil {
			return nil, err
		}
		if err := s.deploy(ctx, actor, site, domains, model.SiteConfigSourcePanel, "启用站点"); err != nil {
			return nil, err
		}
	case model.SiteStatusStopped:
		if err := s.web.Remove(ctx, site.Name); err != nil {
			return nil, err
		}
		site.Status = model.SiteStatusStopped
		site.UpdatedAt = time.Now()
		if err := s.repo.UpdateSite(ctx, site); err != nil {
			return nil, err
		}
	default:
		return nil, errs.Newf(errs.CodeInvalidParam, "不支持的目标状态：%s", target)
	}
	return s.Get(ctx, actor, site.ID)
}

// Rebuild 按当前数据重新渲染并下发，用于修好前置问题后重试失败的站点。
func (s *Service) Rebuild(ctx context.Context, actor Actor, id int64) (*model.SiteDetail, error) {
	site, err := s.repo.FindByID(ctx, actor.TenantID, id)
	if err != nil {
		return nil, err
	}
	if err := s.requireWebServer(ctx); err != nil {
		return nil, err
	}
	domains, err := s.repo.DomainsOfSite(ctx, site.ID)
	if err != nil {
		return nil, err
	}
	if err := s.deploy(ctx, actor, site, domains, model.SiteConfigSourcePanel, "重建配置"); err != nil {
		return nil, err
	}
	return s.Get(ctx, actor, site.ID)
}

// Delete 删除站点。deleteRoot 为真时把站点目录移入回收站目录，不做 rm -rf，
// 便于误删后人工恢复（docs/04 4.7.1 要求根目录默认进回收站）。
func (s *Service) Delete(ctx context.Context, actor Actor, id int64, deleteRoot bool) error {
	site, err := s.repo.FindByID(ctx, actor.TenantID, id)
	if err != nil {
		return err
	}
	if err := s.web.Remove(ctx, site.Name); err != nil {
		return err
	}
	if err := s.repo.Delete(ctx, site); err != nil {
		return err
	}
	if deleteRoot {
		if err := s.recycleRoot(site); err != nil {
			return err
		}
	}
	return nil
}

// Config 返回当前配置正文与最近版本列表。
func (s *Service) Config(ctx context.Context, actor Actor, id int64) (*model.SiteConfigDTO, error) {
	site, err := s.repo.FindByID(ctx, actor.TenantID, id)
	if err != nil {
		return nil, err
	}
	versions, err := s.repo.ListConfigs(ctx, site.ID, s.keepVersions())
	if err != nil {
		return nil, err
	}
	out := &model.SiteConfigDTO{
		SiteID:   site.ID,
		Name:     site.Name,
		File:     s.web.VhostPath(site.Name),
		Versions: make([]model.SiteConfigVersionDTO, 0, len(versions)),
	}
	for i := range versions {
		v := &versions[i]
		out.Versions = append(out.Versions, model.SiteConfigVersionDTO{
			Version:       v.Version,
			Source:        v.Source,
			IsCurrent:     v.IsCurrent,
			DeployStatus:  v.DeployStatus,
			DeployError:   v.DeployError,
			ContentHash:   v.ContentHash,
			ChangeSummary: v.ChangeSummary,
			CreatedAt:     v.CreatedAt.UnixMilli(),
			DeployAt:      unixMilliPtr(v.DeployAt),
		})
		if v.IsCurrent {
			out.Version, out.Content = v.Version, v.Content
		}
	}
	// 当前版本缺失（例如全部下发失败）时回退到最新一条，保证编辑器不是空白。
	if out.Content == "" && len(versions) > 0 {
		out.Version, out.Content = versions[0].Version, versions[0].Content
	}
	return out, nil
}

// SaveConfig 保存手工编辑的配置并下发。version 为编辑基线，落后于当前版本时拒绝，避免覆盖他人改动。
func (s *Service) SaveConfig(ctx context.Context, actor Actor, id int64, in *model.SaveSiteConfigRequest) (*model.SiteConfigDTO, error) {
	site, err := s.repo.FindByID(ctx, actor.TenantID, id)
	if err != nil {
		return nil, err
	}
	if err := s.requireWebServer(ctx); err != nil {
		return nil, err
	}
	content := strings.TrimSpace(in.Content)
	if content == "" {
		return nil, errs.New(errs.CodeInvalidParam, "配置内容不能为空")
	}
	if len(content) > 512*1024 {
		return nil, errs.New(errs.CodeInvalidParam, "配置内容不能超过 512 KB")
	}
	if in.Version > 0 && in.Version != site.ConfigVersion {
		return nil, errs.Newf(errs.CodeConflict,
			"配置已被其他会话更新到版本 %d，请重新打开后再编辑", site.ConfigVersion)
	}
	if err := s.deployContent(ctx, actor, site, content+"\n", model.SiteConfigSourceManual, "手工编辑配置"); err != nil {
		return nil, err
	}
	return s.Config(ctx, actor, site.ID)
}

// Rollback 回滚到指定版本：把旧正文作为新版本重新下发，保持版本表只增不改。
func (s *Service) Rollback(ctx context.Context, actor Actor, id int64, version int) (*model.SiteConfigDTO, error) {
	site, err := s.repo.FindByID(ctx, actor.TenantID, id)
	if err != nil {
		return nil, err
	}
	old, err := s.repo.FindConfig(ctx, site.ID, version)
	if err != nil {
		return nil, err
	}
	if err := s.requireWebServer(ctx); err != nil {
		return nil, err
	}
	summary := fmt.Sprintf("回滚到版本 %d", version)
	if err := s.deployContent(ctx, actor, site, old.Content, model.SiteConfigSourceRollback, summary); err != nil {
		return nil, err
	}
	return s.Config(ctx, actor, site.ID)
}

// ReloadWebServer 重载 Web 服务，供「重载配置」按钮使用。
func (s *Service) ReloadWebServer(ctx context.Context) error { return s.web.Reload(ctx) }

// TestWebServer 执行配置校验，返回校验输出。
func (s *Service) TestWebServer(ctx context.Context) (string, error) { return s.web.Test(ctx) }

// deploy 渲染并下发站点配置。
// 伪静态片段先按数据库状态同步到宿主机，再渲染 vhost，保证 include 的文件一定存在，
// 否则 nginx -t 会因为找不到片段直接失败。
func (s *Service) deploy(ctx context.Context, actor Actor, site *model.WebSite, domains []model.WebSiteDomain, source, summary string) error {
	rewritePath, err := s.syncRewriteFile(ctx, site)
	if err != nil {
		return err
	}
	content, _, err := s.render(site, domains, rewritePath)
	if err != nil {
		return err
	}
	return s.deployContent(ctx, actor, site, content, source, summary)
}

// syncRewriteFile 把数据库里的伪静态规则落到片段文件，返回可 include 的绝对路径。
// 规则缺失或未启用时删除片段文件并返回空串，让模板不输出 include。
func (s *Service) syncRewriteFile(ctx context.Context, site *model.WebSite) (string, error) {
	rule, err := s.repo.FindRewrite(ctx, site.ID)
	if err != nil {
		return "", err
	}
	if rule == nil || !rule.Enabled || strings.TrimSpace(rule.Content) == "" {
		if err := s.web.RemoveSnippet(webserver.SnippetRewrite, site.Name); err != nil {
			return "", err
		}
		return "", nil
	}
	if err := s.web.EnsureDirs(); err != nil {
		return "", err
	}
	body := rewriteSnippetBody(site.Name, rule)
	if err := s.web.WriteSnippet(webserver.SnippetRewrite, site.Name, body); err != nil {
		return "", err
	}
	return s.web.SnippetPath(webserver.SnippetRewrite, site.Name), nil
}

// rewriteSnippetBody 给片段加上来源说明，便于运维在文件系统里识别归属。
func rewriteSnippetBody(siteName string, rule *model.WebRewriteRule) string {
	var b strings.Builder
	b.WriteString("# 本文件由青垣面板自动生成，站点：")
	b.WriteString(siteName)
	b.WriteString("（伪静态）\n# 规则来源：")
	b.WriteString(rule.TemplateCode)
	b.WriteString("\n# 请在面板「站点设置 - 伪静态」中修改，手工编辑会被覆盖。\n")
	b.WriteString(strings.TrimRight(rule.Content, "\n"))
	b.WriteString("\n")
	return b.String()
}

// deployContent 负责版本记录与宿主机下发的一致性：
// 先写 pending 版本，再下发，成功置 deployed，失败置 failed 并把站点标为 error。
func (s *Service) deployContent(ctx context.Context, actor Actor, site *model.WebSite, content, source, summary string) error {
	now := time.Now()
	cfg := &model.WebSiteConfig{
		Base:          model.Base{CreatedBy: actor.UserID, TenantID: site.TenantID, CreatedAt: now, UpdatedAt: now},
		SiteID:        site.ID,
		ConfigType:    model.SiteConfigTypeNginx,
		Content:       content,
		ContentHash:   hashOf(content),
		Source:        source,
		DeployStatus:  model.DeployStatusPending,
		ChangeSummary: summary,
	}
	// 版本号由仓储在写入时分配，避免「先算 MAX+1 再插入」被并发保存撞号。
	if err := s.repo.AppendConfig(ctx, cfg); err != nil {
		return err
	}
	version := cfg.Version

	if deployErr := s.web.Deploy(ctx, site.Name, content); deployErr != nil {
		reason := errs.Message(deployErr)
		if e, ok := errs.As(deployErr); ok && e.Detail != "" {
			reason = reason + "：" + e.Detail
		}
		_ = s.repo.MarkConfigFailed(ctx, cfg, reason)
		site.Status = model.SiteStatusError
		site.UpdatedAt = time.Now()
		_ = s.repo.UpdateSite(ctx, site)
		return deployErr
	}

	if err := s.repo.MarkConfigDeployed(ctx, cfg, time.Now()); err != nil {
		return err
	}
	site.Status = model.SiteStatusRunning
	site.ConfigVersion = version
	site.UpdatedAt = time.Now()
	return s.repo.UpdateSite(ctx, site)
}

// render 组装渲染上下文。端口去重后升序，保证同一份数据渲染结果稳定（便于哈希比对）。
// rewritePath 为空表示该站点没有启用伪静态。
func (s *Service) render(site *model.WebSite, domains []model.WebSiteDomain, rewritePath string) (string, string, error) {
	names := make([]string, 0, len(domains))
	portSet := make(map[int]struct{}, len(domains))
	for i := range domains {
		names = append(names, domains[i].Domain)
		portSet[domains[i].Port] = struct{}{}
	}
	ports := make([]int, 0, len(portSet))
	for p := range portSet {
		ports = append(ports, p)
	}
	sort.Ints(ports)
	if len(names) == 0 {
		return "", "", errs.New(errs.CodeInvalidParam, "站点至少需要绑定一个域名")
	}

	hostHeader := "$host"
	if site.ProxyHost != "" {
		hostHeader = site.ProxyHost
	}
	ctx := &RenderCtx{
		Name:             site.Name,
		Type:             site.Type,
		GeneratedAt:      time.Now().Format("2006-01-02 15:04:05"),
		ServerNames:      names,
		ListenPorts:      ports,
		Root:             site.RootPath,
		IndexFiles:       strings.Fields(site.IndexFiles),
		AccessLogEnabled: site.AccessLogEnabled,
		ErrorLogEnabled:  site.ErrorLogEnabled,
		AccessLog:        site.AccessLogPath,
		ErrorLog:         site.ErrorLogPath,
		ProxyTarget:      site.ProxyTarget,
		ProxyHostHeader:  hostHeader,
		ProxyWebSocket:   site.ProxyWebSocket,
	}
	// 设置全部经过 NormalizeSettings 校验后入库，渲染期只做映射，不再重复判断。
	applySettings(ctx, site.RootPath,
		model.ParseSiteSettings(site.SettingsJSON),
		model.ParseRateLimit(site.RateLimitJSON),
		rewritePath)
	return s.renderer.Render(site.Type, ctx)
}

// normalizeDomains 校验并归一化域名列表，首项为主域名。
func (s *Service) normalizeDomains(in []model.SiteDomainDTO) ([]model.WebSiteDomain, error) {
	if len(in) == 0 {
		return nil, errs.New(errs.CodeInvalidParam, "至少需要绑定一个域名")
	}
	if len(in) > 50 {
		return nil, errs.New(errs.CodeInvalidParam, "单站点域名不能超过 50 个")
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]model.WebSiteDomain, 0, len(in))
	for i := range in {
		domain, err := NormalizeDomain(in[i].Domain)
		if err != nil {
			return nil, err
		}
		port := in[i].Port
		if port == 0 {
			port = 80
		}
		if err := ValidatePort(port, s.panelPort); err != nil {
			return nil, err
		}
		key := fmt.Sprintf("%s:%d", domain, port)
		if _, ok := seen[key]; ok {
			return nil, errs.Newf(errs.CodeInvalidParam, "域名重复：%s", key)
		}
		seen[key] = struct{}{}
		out = append(out, model.WebSiteDomain{
			Domain:     domain,
			Port:       port,
			IsPrimary:  i == 0,
			IsWildcard: strings.HasPrefix(domain, "*."),
		})
	}
	return out, nil
}

func (s *Service) checkDomainConflict(ctx context.Context, domains []model.WebSiteDomain, excludeSiteID int64) error {
	hit, err := s.repo.FindDomainConflict(ctx, domains, excludeSiteID)
	if err != nil {
		return err
	}
	if hit != nil {
		return errs.Newf(errs.CodeDomainExists, "域名 %s:%d 已被其他站点占用", hit.Domain, hit.Port).
			WithField("domain", hit.Domain)
	}
	return nil
}

// requireWebServer 在任何会写宿主机配置的操作前确认 Web 服务可用，
// 避免创建出一个数据库里存在、线上永远不生效的「幽灵站点」。
func (s *Service) requireWebServer(ctx context.Context) error {
	_, err := s.web.Detect(ctx)
	return err
}

func (s *Service) typeSupported(t string) bool {
	for _, v := range s.renderer.SupportedTypes() {
		if v == t {
			return true
		}
	}
	return false
}

func (s *Service) serverKind(ctx context.Context) string {
	if eng, err := s.web.Detect(ctx); err == nil {
		return eng.Kind
	}
	return model.ServerTypeNginx
}

func (s *Service) accessLogPath(name string) string {
	return filepath.Join(s.cfg.LogDir, name+".log")
}

func (s *Service) errorLogPath(name string) string {
	return filepath.Join(s.cfg.LogDir, name+".error.log")
}

func (s *Service) keepVersions() int {
	if s.cfg.BackupKeep > 0 {
		return s.cfg.BackupKeep
	}
	return 20
}

// prepareRoot 建站点目录，并可选写入默认首页。
func (s *Service) prepareRoot(site *model.WebSite, withPage bool) error {
	if err := s.ensureDir(site.RootPath); err != nil {
		return err
	}
	if err := s.web.EnsureDirs(); err != nil {
		return err
	}
	if !withPage || site.Type != model.SiteTypeStatic {
		return nil
	}
	index := filepath.Join(site.RootPath, "index.html")
	if _, err := os.Stat(index); err == nil {
		// 目录里已有首页时不覆盖，防止重建站点抹掉用户内容。
		return nil
	}
	if err := os.WriteFile(index, []byte(defaultIndexHTML(site.Name)), 0o644); err != nil {
		return errs.Wrapf(err, errs.CodeInternal, "写入默认首页失败：%s", index)
	}
	return nil
}

func (s *Service) ensureDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return errs.Wrapf(err, errs.CodeInternal, "创建站点目录失败：%s", dir)
	}
	return nil
}

// recycleRoot 把站点目录移入 <www_root>/.recycle/<name>-<时间戳>。
func (s *Service) recycleRoot(site *model.WebSite) error {
	if site.RootPath == "" {
		return nil
	}
	if _, err := os.Stat(site.RootPath); err != nil {
		return nil
	}
	recycle := filepath.Join(s.cfg.WwwRoot, ".recycle")
	if err := os.MkdirAll(recycle, 0o750); err != nil {
		return errs.Wrap(err, errs.CodeInternal, "创建回收站目录失败")
	}
	dst := filepath.Join(recycle, fmt.Sprintf("%s-%s", site.Name, time.Now().Format("20060102150405")))
	if err := os.Rename(site.RootPath, dst); err != nil {
		return errs.Wrapf(err, errs.CodeInternal, "站点目录移入回收站失败：%s", site.RootPath)
	}
	return nil
}

func (s *Service) buildDetail(site *model.WebSite, domains []model.WebSiteDomain, cur *model.WebSiteConfig) *model.SiteDetail {
	_, rootErr := os.Stat(site.RootPath)
	return &model.SiteDetail{
		SiteSummary:      buildSummary(site, domains, cur),
		IndexFiles:       strings.Fields(site.IndexFiles),
		AccessLogEnabled: site.AccessLogEnabled,
		ErrorLogEnabled:  site.ErrorLogEnabled,
		AccessLogPath:    site.AccessLogPath,
		ErrorLogPath:     site.ErrorLogPath,
		ProxyHost:        site.ProxyHost,
		ProxyWebSocket:   site.ProxyWebSocket,
		VhostFile:        s.web.VhostPath(site.Name),
		RootExists:       rootErr == nil,
	}
}

func buildSummary(site *model.WebSite, domains []model.WebSiteDomain, cur *model.WebSiteConfig) model.SiteSummary {
	items := make([]model.SiteDomainDTO, 0, len(domains))
	primary := ""
	for i := range domains {
		d := &domains[i]
		items = append(items, model.SiteDomainDTO{
			ID: d.ID, Domain: d.Domain, Port: d.Port, IsPrimary: d.IsPrimary, IsWildcard: d.IsWildcard,
		})
		if d.IsPrimary && primary == "" {
			primary = d.Domain
		}
	}
	if primary == "" && len(items) > 0 {
		primary = items[0].Domain
	}
	sum := model.SiteSummary{
		ID:            site.ID,
		Name:          site.Name,
		Type:          site.Type,
		Status:        site.Status,
		ServerType:    site.ServerType,
		PrimaryDomain: primary,
		Domains:       items,
		RootPath:      site.RootPath,
		ProxyTarget:   site.ProxyTarget,
		SSLEnabled:    site.SSLEnabled,
		GroupName:     site.GroupName,
		Remark:        site.Remark,
		ConfigVersion: site.ConfigVersion,
		ExpireAt:      unixMilliPtr(site.ExpireAt),
		CreatedAt:     site.CreatedAt.UnixMilli(),
		UpdatedAt:     site.UpdatedAt.UnixMilli(),
	}
	if cur != nil {
		sum.DeployStatus, sum.DeployError = cur.DeployStatus, cur.DeployError
	}
	return sum
}

func parseExpire(ms int64) (*time.Time, error) {
	if ms == 0 {
		return nil, nil
	}
	if ms < 0 {
		return nil, errs.New(errs.CodeInvalidParam, "到期时间不合法")
	}
	t := time.UnixMilli(ms)
	if t.Before(time.Now()) {
		return nil, errs.New(errs.CodeInvalidParam, "到期时间必须晚于当前时间")
	}
	return &t, nil
}

func unixMilliPtr(t *time.Time) int64 {
	if t == nil {
		return 0
	}
	return t.UnixMilli()
}

func defaultString(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return strings.TrimSpace(v)
}

func defaultIndexHTML(name string) string {
	return `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>` + name + `</title>
<style>
  body { margin:0; min-height:100vh; display:flex; align-items:center; justify-content:center;
         font-family: -apple-system, "Segoe UI", "PingFang SC", sans-serif; background:#f5f7fa; color:#1d2129; }
  .card { padding:40px 48px; background:#fff; border-radius:8px; box-shadow:0 2px 12px rgba(0,0,0,.06); text-align:center; }
  h1 { margin:0 0 8px; font-size:20px; font-weight:600; }
  p { margin:0; font-size:13px; color:#86909c; }
</style>
</head>
<body>
  <div class="card">
    <h1>` + name + ` 已创建</h1>
    <p>站点由青垣面板创建，请将站点文件上传到根目录后替换本页面。</p>
  </div>
</body>
</html>
`
}
