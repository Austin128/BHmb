package model

import "time"

// 站点类型。本期实现 static 与 proxy，其余类型待运行环境模块落地后开放。
const (
	SiteTypeStatic = "static"
	SiteTypeProxy  = "proxy"
	SiteTypePHP    = "php"
)

// 站点状态机（docs/07 7.2.3）。
const (
	SiteStatusRunning     = "running"
	SiteStatusStopped     = "stopped"
	SiteStatusError       = "error"
	SiteStatusDeploying   = "deploying"
	SiteStatusMaintenance = "maintenance"
)

// Web 服务引擎。
const (
	ServerTypeNginx     = "nginx"
	ServerTypeOpenResty = "openresty"
)

// 配置类型与来源。
const (
	SiteConfigTypeNginx      = "nginx"
	SiteConfigSourcePanel    = "panel"
	SiteConfigSourceManual   = "manual_edit"
	SiteConfigSourceRollback = "rollback"
)

// 配置下发状态。
const (
	DeployStatusPending    = "pending"
	DeployStatusDeployed   = "deployed"
	DeployStatusFailed     = "failed"
	DeployStatusRolledBack = "rolled_back"
)

// WebSite 对应 web_site 表，是站点聚合根。name 同时作为 vhost 文件名与默认目录名。
type WebSite struct {
	Base
	SoftDelete
	NodeID           int64      `gorm:"column:node_id;not null;default:0"`
	Name             string     `gorm:"column:name;not null"`
	Type             string     `gorm:"column:type;not null;default:'static'"`
	ServerType       string     `gorm:"column:server_type;not null;default:'nginx'"`
	RootPath         string     `gorm:"column:root_path;not null;default:''"`
	IndexFiles       string     `gorm:"column:index_files;not null;default:'index.html index.htm'"`
	RuntimeID        int64      `gorm:"column:runtime_id;not null;default:0"`
	PHPVersion       string     `gorm:"column:php_version;not null;default:''"`
	ProxyTarget      string     `gorm:"column:proxy_target;not null;default:''"`
	ProxyHost        string     `gorm:"column:proxy_host;not null;default:''"`
	ProxyWebSocket   bool       `gorm:"column:proxy_websocket;not null;default:true"`
	ListenPort       int        `gorm:"column:listen_port;not null;default:80"`
	SSLPort          int        `gorm:"column:ssl_port;not null;default:443"`
	SSLEnabled       bool       `gorm:"column:ssl_enabled;not null;default:false"`
	CertID           int64      `gorm:"column:cert_id;not null;default:0"`
	ForceHTTPS       bool       `gorm:"column:force_https;not null;default:false"`
	HTTP2Enabled     bool       `gorm:"column:http2_enabled;not null;default:true"`
	HTTP3Enabled     bool       `gorm:"column:http3_enabled;not null;default:false"`
	WAFEnabled       bool       `gorm:"column:waf_enabled;not null;default:false"`
	AccessLogEnabled bool       `gorm:"column:access_log_enabled;not null;default:true"`
	ErrorLogEnabled  bool       `gorm:"column:error_log_enabled;not null;default:true"`
	AccessLogPath    string     `gorm:"column:access_log_path;not null;default:''"`
	ErrorLogPath     string     `gorm:"column:error_log_path;not null;default:''"`
	RateLimitJSON    string     `gorm:"column:rate_limit_json"`
	SettingsJSON     string     `gorm:"column:settings_json"`
	Status           string     `gorm:"column:status;not null;default:'running'"`
	ConfigVersion    int        `gorm:"column:config_version;not null;default:1"`
	ExpireAt         *time.Time `gorm:"column:expire_at"`
	GroupName        string     `gorm:"column:group_name;not null;default:'default'"`
	Remark           string     `gorm:"column:remark;not null;default:''"`
}

// TableName 指定表名。
func (WebSite) TableName() string { return "web_site" }

// WebSiteDomain 对应 web_site_domain 表，一行是一个「域名 + 端口」绑定。
type WebSiteDomain struct {
	Base
	SoftDelete
	SiteID         int64      `gorm:"column:site_id;not null"`
	Domain         string     `gorm:"column:domain;not null"`
	Port           int        `gorm:"column:port;not null;default:80"`
	IsPrimary      bool       `gorm:"column:is_primary;not null;default:false"`
	IsWildcard     bool       `gorm:"column:is_wildcard;not null;default:false"`
	CertID         int64      `gorm:"column:cert_id;not null;default:0"`
	DNSResolvedIP  string     `gorm:"column:dns_resolved_ip;not null;default:''"`
	DNSCheckAt     *time.Time `gorm:"column:dns_check_at"`
	DNSCheckResult string     `gorm:"column:dns_check_result;not null;default:''"`
}

// TableName 指定表名。
func (WebSiteDomain) TableName() string { return "web_site_domain" }

// WebSiteConfig 对应 web_site_config 表，是只增不改的配置版本快照。
type WebSiteConfig struct {
	Base
	SoftDelete
	SiteID        int64      `gorm:"column:site_id;not null"`
	Version       int        `gorm:"column:version;not null"`
	ConfigType    string     `gorm:"column:config_type;not null;default:'nginx'"`
	Content       string     `gorm:"column:content;not null"`
	ContentHash   string     `gorm:"column:content_hash;not null"`
	Source        string     `gorm:"column:source;not null;default:'panel'"`
	IsCurrent     bool       `gorm:"column:is_current;not null;default:false"`
	DeployStatus  string     `gorm:"column:deploy_status;not null;default:'pending'"`
	DeployAt      *time.Time `gorm:"column:deploy_at"`
	DeployError   string     `gorm:"column:deploy_error;not null;default:''"`
	ChangeSummary string     `gorm:"column:change_summary;not null;default:''"`
}

// TableName 指定表名。
func (WebSiteConfig) TableName() string { return "web_site_config" }
