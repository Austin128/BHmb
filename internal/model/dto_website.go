package model

// SiteDomainDTO 是站点域名的读写通用结构。
// 雪花 ID 超过 JS 安全整数范围，统一以字符串序列化（与 dto_admin.go 一致）。
type SiteDomainDTO struct {
	ID        int64  `json:"id,string"`
	Domain    string `json:"domain"`
	Port      int    `json:"port"`
	IsPrimary bool   `json:"isPrimary"`
	// IsWildcard 由服务端按域名前缀推导，请求中传值会被忽略。
	IsWildcard bool `json:"isWildcard"`
}

// SiteSummary 是站点列表项。列表页不返回配置正文，避免响应体过大。
type SiteSummary struct {
	ID            int64           `json:"id,string"`
	Name          string          `json:"name"`
	Type          string          `json:"type"`
	Status        string          `json:"status"`
	ServerType    string          `json:"serverType"`
	PrimaryDomain string          `json:"primaryDomain"`
	Domains       []SiteDomainDTO `json:"domains"`
	RootPath      string          `json:"rootPath"`
	ProxyTarget   string          `json:"proxyTarget"`
	SSLEnabled    bool            `json:"sslEnabled"`
	GroupName     string          `json:"groupName"`
	Remark        string          `json:"remark"`
	ConfigVersion int             `json:"configVersion"`
	// DeployStatus 与 DeployError 来自当前配置版本，让列表能直接暴露下发失败原因。
	DeployStatus string `json:"deployStatus"`
	DeployError  string `json:"deployError"`
	// ExpireAt 为 0 表示永不过期，单位毫秒。
	ExpireAt  int64 `json:"expireAt"`
	CreatedAt int64 `json:"createdAt"`
	UpdatedAt int64 `json:"updatedAt"`
}

// SiteDetail 在列表项之上补充目录、日志与配置信息。
type SiteDetail struct {
	SiteSummary
	IndexFiles       []string `json:"indexFiles"`
	AccessLogEnabled bool     `json:"accessLogEnabled"`
	ErrorLogEnabled  bool     `json:"errorLogEnabled"`
	AccessLogPath    string   `json:"accessLogPath"`
	ErrorLogPath     string   `json:"errorLogPath"`
	ProxyHost        string   `json:"proxyHost"`
	ProxyWebSocket   bool     `json:"proxyWebSocket"`
	// VhostFile 为面板托管的配置文件路径，便于用户用文件管理器查看。
	VhostFile string `json:"vhostFile"`
	// RootExists 反映站点目录是否真实存在，目录被外部删除时前端可提示。
	RootExists bool `json:"rootExists"`
}

// SiteListResult 为分页列表响应。
type SiteListResult struct {
	Items []SiteSummary `json:"items"`
	Total int64         `json:"total"`
	Page  int           `json:"page"`
	Size  int           `json:"size"`
}

// CreateSiteRequest 为创建站点入参。
type CreateSiteRequest struct {
	Name    string          `json:"name"`
	Type    string          `json:"type"`
	Domains []SiteDomainDTO `json:"domains"`
	// Root 为空时取 <www_root>/<name>。
	Root       string   `json:"root"`
	IndexFiles []string `json:"indexFiles"`
	Remark     string   `json:"remark"`
	GroupName  string   `json:"groupName"`
	// ExpireAt 为毫秒时间戳，0 表示永不过期。
	ExpireAt int64 `json:"expireAt"`
	// 反代站参数。
	ProxyTarget    string `json:"proxyTarget"`
	ProxyHost      string `json:"proxyHost"`
	ProxyWebSocket bool   `json:"proxyWebSocket"`
	// CreateDefaultPage 为真时给静态站生成一个默认首页，方便创建后立刻验证。
	CreateDefaultPage bool `json:"createDefaultPage"`
	AccessLogEnabled  bool `json:"accessLogEnabled"`
	ErrorLogEnabled   bool `json:"errorLogEnabled"`
}

// UpdateSiteRequest 为编辑站点入参。域名为整体替换语义：传空数组会被拒绝。
type UpdateSiteRequest struct {
	Domains          []SiteDomainDTO `json:"domains"`
	Root             string          `json:"root"`
	IndexFiles       []string        `json:"indexFiles"`
	Remark           string          `json:"remark"`
	GroupName        string          `json:"groupName"`
	ExpireAt         *int64          `json:"expireAt"`
	ProxyTarget      string          `json:"proxyTarget"`
	ProxyHost        string          `json:"proxyHost"`
	ProxyWebSocket   *bool           `json:"proxyWebSocket"`
	AccessLogEnabled *bool           `json:"accessLogEnabled"`
	ErrorLogEnabled  *bool           `json:"errorLogEnabled"`
}

// SiteConfigVersionDTO 为配置版本列表项，不含正文。
type SiteConfigVersionDTO struct {
	Version       int    `json:"version"`
	Source        string `json:"source"`
	IsCurrent     bool   `json:"isCurrent"`
	DeployStatus  string `json:"deployStatus"`
	DeployError   string `json:"deployError"`
	ContentHash   string `json:"contentHash"`
	ChangeSummary string `json:"changeSummary"`
	CreatedAt     int64  `json:"createdAt"`
	DeployAt      int64  `json:"deployAt"`
}

// SiteConfigDTO 返回当前配置正文与历史版本。
type SiteConfigDTO struct {
	SiteID   int64                  `json:"siteId,string"`
	Name     string                 `json:"name"`
	File     string                 `json:"file"`
	Version  int                    `json:"version"`
	Content  string                 `json:"content"`
	Versions []SiteConfigVersionDTO `json:"versions"`
}

// SaveSiteConfigRequest 为手工编辑配置入参。
type SaveSiteConfigRequest struct {
	Content string `json:"content"`
	// Version 为编辑基线版本，与当前版本不一致时拒绝保存，避免覆盖他人改动。
	Version int `json:"version"`
}

// ChangeSiteStatusRequest 为启停入参，target 取 running/stopped。
type ChangeSiteStatusRequest struct {
	Target string `json:"target"`
}

// DeleteSiteRequest 为删除入参。
type DeleteSiteRequest struct {
	// DeleteRoot 为真时把站点目录移入回收站，而不是直接删除。
	DeleteRoot bool `json:"deleteRoot"`
}

// SiteSettingsDTO 是站点高级设置的读写结构（docs/07 7.10.1）。
// 默认文档、日志开关与限速在数据库里分别落在 index_files、
// access_log_enabled/error_log_enabled、rate_limit_json，这里统一成一份
// 前端可直接绑定的视图，避免设置页要分多个接口拼装。
type SiteSettingsDTO struct {
	SiteID int64  `json:"siteId,string"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	// IndexFiles 为默认文档顺序列表。
	IndexFiles []string `json:"indexFiles"`
	// RunPath 为相对站点根目录的运行目录，如 /public；空表示直接用站点根。
	RunPath          string               `json:"runPath"`
	AutoIndex        bool                 `json:"autoIndex"`
	CacheProfile     string               `json:"cacheProfile"`
	AntiLeech        AntiLeechSetting     `json:"antiLeech"`
	AccessControl    AccessControlSetting `json:"accessControl"`
	Redirects        []RedirectRule       `json:"redirects"`
	RateLimit        RateLimitSetting     `json:"rateLimit"`
	AccessLogEnabled bool                 `json:"accessLogEnabled"`
	ErrorLogEnabled  bool                 `json:"errorLogEnabled"`
	AccessLogPath    string               `json:"accessLogPath"`
	ErrorLogPath     string               `json:"errorLogPath"`
	// RootPath 回显给前端做运行目录提示。
	RootPath string `json:"rootPath"`
	// CacheProfiles 为可选缓存档位，前端据此渲染下拉而不写死枚举。
	CacheProfiles []string `json:"cacheProfiles"`
}

// SaveSiteSettingsRequest 为保存站点设置入参。
// 所有字段均为整体替换语义：设置页一次提交完整状态，避免部分更新造成的歧义。
type SaveSiteSettingsRequest struct {
	IndexFiles       []string             `json:"indexFiles"`
	RunPath          string               `json:"runPath"`
	AutoIndex        bool                 `json:"autoIndex"`
	CacheProfile     string               `json:"cacheProfile"`
	AntiLeech        AntiLeechSetting     `json:"antiLeech"`
	AccessControl    AccessControlSetting `json:"accessControl"`
	Redirects        []RedirectRule       `json:"redirects"`
	RateLimit        RateLimitSetting     `json:"rateLimit"`
	AccessLogEnabled bool                 `json:"accessLogEnabled"`
	ErrorLogEnabled  bool                 `json:"errorLogEnabled"`
}

// SiteRewriteDTO 是伪静态规则的读结构。
type SiteRewriteDTO struct {
	SiteID int64  `json:"siteId,string"`
	Name   string `json:"name"`
	// TemplateCode 取内置模板代码、custom 或 none。
	TemplateCode string `json:"templateCode"`
	Content      string `json:"content"`
	Enabled      bool   `json:"enabled"`
	// File 为伪静态片段落盘路径，便于用户核对。
	File      string `json:"file"`
	UpdatedAt int64  `json:"updatedAt"`
}

// SiteRewriteTemplateDTO 是内置伪静态模板列表项。
type SiteRewriteTemplateDTO struct {
	Code    string `json:"code"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

// SaveSiteRewriteRequest 为保存伪静态入参。
// TemplateCode 为内置模板时忽略 Content，直接取模板正文，
// 避免前端改了模板内容却仍标记为该模板导致回显与实际不一致。
type SaveSiteRewriteRequest struct {
	TemplateCode string `json:"templateCode"`
	Content      string `json:"content"`
	Enabled      bool   `json:"enabled"`
}

// 站点日志类型。
const (
	SiteLogAccess = "access"
	SiteLogError  = "error"
)

// SiteLogDTO 是站点日志尾部视图（docs/07 7.9）。
// 只读尾部而不整文件返回：访问日志动辄上百 MB，整读会打满内存与带宽。
type SiteLogDTO struct {
	SiteID int64  `json:"siteId,string"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Path   string `json:"path"`
	// Enabled 反映站点设置里的日志开关，关闭时文件通常不存在。
	Enabled bool  `json:"enabled"`
	Exists  bool  `json:"exists"`
	Size    int64 `json:"size"`
	// ModifiedAt 为文件最后写入时间（毫秒），文件不存在时为 0。
	ModifiedAt int64 `json:"modifiedAt"`
	// Lines 为倒序读取后按时间顺序排列的日志行。
	Lines []string `json:"lines"`
	// Truncated 为真表示文件比本次读取窗口更长，前端应提示只显示了尾部。
	Truncated bool `json:"truncated"`
}
