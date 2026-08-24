package model

import "encoding/json"

// 静态缓存档位（docs/07 7.10.1）。expires 指令的可选档位，UI 只暴露这几档，
// 避免用户写出无法校验的任意时间字符串。
const (
	CacheProfileNone  = "none"
	CacheProfileHour  = "hour"
	CacheProfileDay   = "day"
	CacheProfileMonth = "month"
	CacheProfileYear  = "year"
)

// CacheProfileExpires 把档位映射为 nginx expires 值。
var CacheProfileExpires = map[string]string{
	CacheProfileHour:  "1h",
	CacheProfileDay:   "1d",
	CacheProfileMonth: "30d",
	CacheProfileYear:  "365d",
}

// 重定向状态码：只允许 301/302，避免误配成 30x 之外的值。
const (
	RedirectCodePermanent = 301
	RedirectCodeTemporary = 302
)

// 伪静态模板代码。custom 表示用户自定义正文，none 表示不启用。
const (
	RewriteTemplateNone   = "none"
	RewriteTemplateCustom = "custom"
)

// AntiLeechSetting 防盗链设置。命中非法 Referer 时返回 ReturnCode。
type AntiLeechSetting struct {
	Enabled           bool     `json:"enabled"`
	Extensions        []string `json:"extensions"`
	AllowedReferers   []string `json:"allowedReferers"`
	AllowEmptyReferer bool     `json:"allowEmptyReferer"`
	ReturnCode        int      `json:"returnCode"`
}

// AccessControlSetting 访问限制设置。
// AllowIPs 非空时只放行这些来源，其余一律拒绝；DenyIPs 用于黑名单。
// DenyUserAgents 按不区分大小写的正则匹配 User-Agent。
type AccessControlSetting struct {
	Enabled        bool     `json:"enabled"`
	AllowIPs       []string `json:"allowIps"`
	DenyIPs        []string `json:"denyIps"`
	DenyUserAgents []string `json:"denyUserAgents"`
}

// RedirectRule 单条重定向。Path 为 / 时表示整站跳转。
type RedirectRule struct {
	Enabled   bool   `json:"enabled"`
	Path      string `json:"path"`
	Target    string `json:"target"`
	Code      int    `json:"code"`
	KeepPath  bool   `json:"keepPath"`
	KeepQuery bool   `json:"keepQuery"`
	Remark    string `json:"remark"`
}

// RateLimitSetting 站点限速。
// 只包含 server 块内可独立生效的带宽限制：limit_conn/limit_req 需要在 http 块
// 声明 zone，面板当前只纳管 vhost 目录，因此不在此暴露。
type RateLimitSetting struct {
	Enabled     bool `json:"enabled"`
	BandwidthKB int  `json:"bandwidthKb"`
	BurstKB     int  `json:"burstKb"`
}

// SiteSettings 是站点级可下发设置的聚合，序列化后存 web_site.settings_json。
// 默认文档、日志开关、限速分别落在 index_files、access_log_enabled /
// error_log_enabled、rate_limit_json 上，此结构只承载其余能力。
type SiteSettings struct {
	// RunPath 是相对站点根目录的运行目录（如 /public），空表示直接用站点根。
	RunPath       string               `json:"runPath"`
	AutoIndex     bool                 `json:"autoIndex"`
	CacheProfile  string               `json:"cacheProfile"`
	AntiLeech     AntiLeechSetting     `json:"antiLeech"`
	AccessControl AccessControlSetting `json:"accessControl"`
	Redirects     []RedirectRule       `json:"redirects"`
}

// DefaultSiteSettings 返回全部关闭的安全默认值：目录浏览关闭、无缓存、
// 不启用防盗链与访问限制、无重定向。
func DefaultSiteSettings() SiteSettings {
	return SiteSettings{
		AutoIndex:     false,
		CacheProfile:  CacheProfileNone,
		AntiLeech:     AntiLeechSetting{ReturnCode: 403},
		AccessControl: AccessControlSetting{},
		Redirects:     []RedirectRule{},
	}
}

// ParseSiteSettings 解析 settings_json。空值或非法 JSON 都退回默认值，
// 避免一条坏数据导致站点无法渲染配置。
func ParseSiteSettings(raw string) SiteSettings {
	s := DefaultSiteSettings()
	if raw == "" {
		return s
	}
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return DefaultSiteSettings()
	}
	if s.CacheProfile == "" {
		s.CacheProfile = CacheProfileNone
	}
	if s.AntiLeech.ReturnCode == 0 {
		s.AntiLeech.ReturnCode = 403
	}
	if s.Redirects == nil {
		s.Redirects = []RedirectRule{}
	}
	return s
}

// Marshal 序列化为存库字符串。
func (s SiteSettings) Marshal() (string, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ParseRateLimit 解析 rate_limit_json，同样对坏数据做兜底。
func ParseRateLimit(raw string) RateLimitSetting {
	var r RateLimitSetting
	if raw == "" {
		return r
	}
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		return RateLimitSetting{}
	}
	return r
}

// Marshal 序列化限速设置。
func (r RateLimitSetting) Marshal() (string, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// WebRewriteRule 对应 web_rewrite_rule 表，一个站点保留一条当前生效的伪静态规则。
type WebRewriteRule struct {
	Base
	SoftDelete
	SiteID       int64  `gorm:"column:site_id;not null"`
	Name         string `gorm:"column:name;not null;default:''"`
	TemplateCode string `gorm:"column:template_code;not null;default:''"`
	Content      string `gorm:"column:content;not null"`
	Enabled      bool   `gorm:"column:enabled;not null;default:true"`
	Sort         int    `gorm:"column:sort;not null;default:0"`
}

// TableName 指定表名。
func (WebRewriteRule) TableName() string { return "web_rewrite_rule" }
