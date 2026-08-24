package site

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/novapanel/novapanel/internal/model"
	"github.com/novapanel/novapanel/internal/pkg/errs"
	"github.com/novapanel/novapanel/internal/service/webserver"
)

// cacheProfileOptions 是暴露给前端的缓存档位顺序，与 model 中的常量一一对应。
var cacheProfileOptions = []string{
	model.CacheProfileNone,
	model.CacheProfileHour,
	model.CacheProfileDay,
	model.CacheProfileMonth,
	model.CacheProfileYear,
}

// Settings 返回站点高级设置。settings_json 为空（老站点）时回落到默认值，
// 因此升级后打开设置页不会报错，保存一次即补齐记录。
func (s *Service) Settings(ctx context.Context, actor Actor, id int64) (*model.SiteSettingsDTO, error) {
	site, err := s.repo.FindByID(ctx, actor.TenantID, id)
	if err != nil {
		return nil, err
	}
	return buildSettingsDTO(site), nil
}

// SaveSettings 保存站点设置并重新下发配置。
// 落库先于下发：下发失败时 vhost 由 webserver.Manager 还原为旧内容，
// 数据库保留用户刚提交的设置并把站点标记为 error，用户修正后点「重建」即可重试，
// 这与站点编辑的失败语义一致，避免用户填的一整页表单被丢弃。
func (s *Service) SaveSettings(ctx context.Context, actor Actor, id int64, in *model.SaveSiteSettingsRequest) (*model.SiteSettingsDTO, error) {
	site, err := s.repo.FindByID(ctx, actor.TenantID, id)
	if err != nil {
		return nil, err
	}

	settings, err := NormalizeSettings(model.SiteSettings{
		RunPath:       in.RunPath,
		AutoIndex:     in.AutoIndex,
		CacheProfile:  in.CacheProfile,
		AntiLeech:     in.AntiLeech,
		AccessControl: in.AccessControl,
		Redirects:     in.Redirects,
	}, site.Type)
	if err != nil {
		return nil, err
	}
	rate, err := NormalizeRateLimit(in.RateLimit)
	if err != nil {
		return nil, err
	}
	settingsJSON, err := settings.Marshal()
	if err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "站点设置序列化失败")
	}
	rateJSON, err := rate.Marshal()
	if err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "限速设置序列化失败")
	}

	// 运行目录指向的实际目录不存在时 nginx 仍能启动，但所有请求都会 404，
	// 属于典型的「保存成功却打不开」，因此提前拦下。
	if settings.RunPath != "" {
		full := joinRunPath(site.RootPath, settings.RunPath)
		if !dirExists(full) {
			return nil, errs.Newf(errs.CodeInvalidParam, "运行目录不存在：%s", full)
		}
	}

	if len(in.IndexFiles) > 0 {
		site.IndexFiles = strings.Join(ParseIndexFiles(in.IndexFiles, site.Type), " ")
	}
	site.SettingsJSON = settingsJSON
	site.RateLimitJSON = rateJSON
	site.AccessLogEnabled = in.AccessLogEnabled
	site.ErrorLogEnabled = in.ErrorLogEnabled
	site.UpdatedAt = time.Now()
	if err := s.repo.UpdateSite(ctx, site); err != nil {
		return nil, err
	}

	// 停用中的站点只存数据，不把配置重新放到线上。
	if site.Status == model.SiteStatusStopped {
		return buildSettingsDTO(site), nil
	}
	if err := s.requireWebServer(ctx); err != nil {
		return nil, err
	}
	domains, err := s.repo.DomainsOfSite(ctx, site.ID)
	if err != nil {
		return nil, err
	}
	if err := s.deploy(ctx, actor, site, domains, model.SiteConfigSourcePanel, "修改站点设置"); err != nil {
		return nil, err
	}
	return buildSettingsDTO(site), nil
}

// RewriteTemplateList 返回内置伪静态规则库。
func (s *Service) RewriteTemplateList() []model.SiteRewriteTemplateDTO {
	tpls := RewriteTemplates()
	out := make([]model.SiteRewriteTemplateDTO, 0, len(tpls))
	for _, t := range tpls {
		out = append(out, model.SiteRewriteTemplateDTO{Code: t.Code, Name: t.Name, Content: t.Content})
	}
	return out
}

// Rewrite 返回站点当前伪静态规则。未配置时返回 none 模板的空规则，前端直接绑定即可。
func (s *Service) Rewrite(ctx context.Context, actor Actor, id int64) (*model.SiteRewriteDTO, error) {
	site, err := s.repo.FindByID(ctx, actor.TenantID, id)
	if err != nil {
		return nil, err
	}
	rule, err := s.repo.FindRewrite(ctx, site.ID)
	if err != nil {
		return nil, err
	}
	out := &model.SiteRewriteDTO{
		SiteID:       site.ID,
		Name:         site.Name,
		TemplateCode: model.RewriteTemplateNone,
		File:         s.web.SnippetPath(webserver.SnippetRewrite, site.Name),
	}
	if rule != nil {
		out.TemplateCode = rule.TemplateCode
		out.Content = rule.Content
		out.Enabled = rule.Enabled
		out.UpdatedAt = rule.UpdatedAt.UnixMilli()
	}
	return out, nil
}

// SaveRewrite 保存伪静态并下发。
// 顺序为「校验正文 → 落库 → 写片段 → 渲染下发（含 nginx -t）」；
// 下发失败时把片段文件还原成旧内容，让磁盘状态与已回滚的 vhost 保持一致，
// 否则目录里会残留一个线上并未生效的规则文件，误导后续排查。
func (s *Service) SaveRewrite(ctx context.Context, actor Actor, id int64, in *model.SaveSiteRewriteRequest) (*model.SiteRewriteDTO, error) {
	site, err := s.repo.FindByID(ctx, actor.TenantID, id)
	if err != nil {
		return nil, err
	}

	code := strings.TrimSpace(in.TemplateCode)
	if code == "" {
		code = model.RewriteTemplateNone
	}
	content := in.Content
	switch code {
	case model.RewriteTemplateNone:
		content = ""
	case model.RewriteTemplateCustom:
		content = strings.TrimSpace(content)
		if content == "" {
			return nil, errs.New(errs.CodeInvalidParam, "自定义伪静态内容不能为空")
		}
	default:
		// 内置模板以库中正文为准，避免前端改了内容却仍标记成该模板，
		// 导致回显与线上实际规则不一致。
		body, ok := RewriteTemplateContent(code)
		if !ok {
			return nil, errs.Newf(errs.CodeInvalidParam, "未知的伪静态模板：%s", code)
		}
		content = body
	}
	if content != "" {
		if err := ValidateRewrite(content, site.Type); err != nil {
			return nil, err
		}
	}

	enabled := in.Enabled && content != ""
	now := time.Now()
	rule := &model.WebRewriteRule{
		Base:         model.Base{CreatedBy: actor.UserID, TenantID: site.TenantID, CreatedAt: now, UpdatedAt: now},
		SiteID:       site.ID,
		Name:         site.Name,
		TemplateCode: code,
		Content:      content,
		Enabled:      enabled,
	}
	if code == model.RewriteTemplateNone {
		// 「不启用」等价于移除规则：留一条空记录会让后续判断多一个分支。
		if err := s.repo.DeleteRewrite(ctx, site.ID); err != nil {
			return nil, err
		}
	} else if err := s.repo.SaveRewrite(ctx, rule); err != nil {
		return nil, err
	}

	if site.Status == model.SiteStatusStopped {
		// 停用站点不碰线上文件，启用时 deploy 会按数据库状态重新同步片段。
		return s.Rewrite(ctx, actor, site.ID)
	}
	if err := s.requireWebServer(ctx); err != nil {
		return nil, err
	}
	domains, err := s.repo.DomainsOfSite(ctx, site.ID)
	if err != nil {
		return nil, err
	}
	oldSnippet, hadSnippet := s.web.ReadSnippet(webserver.SnippetRewrite, site.Name)
	if err := s.deploy(ctx, actor, site, domains, model.SiteConfigSourcePanel, "修改伪静态规则"); err != nil {
		s.restoreSnippet(site.Name, oldSnippet, hadSnippet)
		return nil, err
	}
	return s.Rewrite(ctx, actor, site.ID)
}

// restoreSnippet 尽力还原片段文件。还原失败只影响磁盘残留，
// 线上配置已由 webserver.Manager 回滚，因此不覆盖调用方要返回的原始错误。
func (s *Service) restoreSnippet(name, content string, existed bool) {
	if !existed {
		_ = s.web.RemoveSnippet(webserver.SnippetRewrite, name)
		return
	}
	_ = s.web.WriteSnippet(webserver.SnippetRewrite, name, content)
}

// buildSettingsDTO 把站点记录转成设置视图。
func buildSettingsDTO(site *model.WebSite) *model.SiteSettingsDTO {
	s := model.ParseSiteSettings(site.SettingsJSON)
	return &model.SiteSettingsDTO{
		SiteID:           site.ID,
		Name:             site.Name,
		Type:             site.Type,
		IndexFiles:       strings.Fields(site.IndexFiles),
		RunPath:          s.RunPath,
		AutoIndex:        s.AutoIndex,
		CacheProfile:     s.CacheProfile,
		AntiLeech:        s.AntiLeech,
		AccessControl:    s.AccessControl,
		Redirects:        s.Redirects,
		RateLimit:        model.ParseRateLimit(site.RateLimitJSON),
		AccessLogEnabled: site.AccessLogEnabled,
		ErrorLogEnabled:  site.ErrorLogEnabled,
		AccessLogPath:    site.AccessLogPath,
		ErrorLogPath:     site.ErrorLogPath,
		RootPath:         site.RootPath,
		CacheProfiles:    cacheProfileOptions,
	}
}

// joinRunPath 拼出运行目录的绝对路径。runPath 已经 NormalizeSettings 校验，
// 不含 .. 与异常字符，因此可直接拼接。
func joinRunPath(root, runPath string) string {
	return filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(runPath, "/")))
}

// dirExists 判断路径存在且为目录。
func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}
