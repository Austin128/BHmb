// Package config 负责控制面配置的加载、默认值、环境变量覆盖与合法性校验。
// 配置键与 docs/03-技术栈与工程规范.md 3.11 一致；主密钥键名为 security.master_key_file。
package config

import (
	"strings"
	"time"

	"github.com/spf13/viper"

	"github.com/novapanel/novapanel/internal/pkg/errs"
)

// Config 为控制面完整配置。
type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Database DatabaseConfig `mapstructure:"database"`
	KV       KVConfig       `mapstructure:"kv"`
	Agent    AgentConfig    `mapstructure:"agent"`
	Security SecurityConfig `mapstructure:"security"`
	Monitor  MonitorConfig  `mapstructure:"monitor"`
	Log      LogConfig      `mapstructure:"log"`
	Runtime  RuntimeConfig  `mapstructure:"runtime"`
	Cluster  ClusterConfig  `mapstructure:"cluster"`
}

// ServerConfig 为 HTTP 服务配置。
type ServerConfig struct {
	Host            string        `mapstructure:"host"`
	Port            int           `mapstructure:"port"`
	BasePath        string        `mapstructure:"base_path"`
	DefaultLocale   string        `mapstructure:"default_locale"`
	ReadTimeout     time.Duration `mapstructure:"read_timeout"`
	WriteTimeout    time.Duration `mapstructure:"write_timeout"`
	IdleTimeout     time.Duration `mapstructure:"idle_timeout"`
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`
	Entrance        string        `mapstructure:"entrance"`
	MaxBodySizeMB   int           `mapstructure:"max_body_size_mb"`
	TLS             TLSConfig     `mapstructure:"tls"`
}

// TLSConfig 为面板 HTTPS 配置。
type TLSConfig struct {
	Enabled        bool   `mapstructure:"enabled"`
	CertFile       string `mapstructure:"cert_file"`
	KeyFile        string `mapstructure:"key_file"`
	MinVersion     string `mapstructure:"min_version"`
	AutoSelfSigned bool   `mapstructure:"auto_self_signed"`
}

// DatabaseConfig 为业务库配置，driver 决定 dsn 与 path 哪个生效。
type DatabaseConfig struct {
	Driver          string        `mapstructure:"driver"`
	DSN             string        `mapstructure:"dsn"`
	Path            string        `mapstructure:"path"`
	MaxOpenConns    int           `mapstructure:"max_open_conns"`
	MaxIdleConns    int           `mapstructure:"max_idle_conns"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`
	SlowThreshold   time.Duration `mapstructure:"slow_threshold"`
	LogLevel        string        `mapstructure:"log_level"`
	AutoMigrate     bool          `mapstructure:"auto_migrate"`
}

// KVConfig 为轻量键值存储配置。
type KVConfig struct {
	Driver string      `mapstructure:"driver"`
	Path   string      `mapstructure:"path"`
	Redis  RedisConfig `mapstructure:"redis"`
}

// RedisConfig 为可选 Redis 配置。
type RedisConfig struct {
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
	PoolSize int    `mapstructure:"pool_size"`
}

// AgentConfig 为被控端接入配置。
type AgentConfig struct {
	GRPCPort           int           `mapstructure:"grpc_port"`
	AdvertiseAddr      string        `mapstructure:"advertise_addr"`
	HeartbeatInterval  time.Duration `mapstructure:"heartbeat_interval"`
	OfflineThreshold   int           `mapstructure:"offline_threshold"`
	SessionTTL         time.Duration `mapstructure:"session_ttl"`
	BufferMaxMB        int           `mapstructure:"buffer_max_mb"`
	TaskDefaultTimeout time.Duration `mapstructure:"task_default_timeout"`
	RegisterTokenTTL   time.Duration `mapstructure:"register_token_ttl"`
	MTLS               MTLSConfig    `mapstructure:"mtls"`
}

// MTLSConfig 为 Agent 双向证书配置。
type MTLSConfig struct {
	CACert        string        `mapstructure:"ca_cert"`
	CAKey         string        `mapstructure:"ca_key"`
	ServerCert    string        `mapstructure:"server_cert"`
	ServerKey     string        `mapstructure:"server_key"`
	ClientCertTTL time.Duration `mapstructure:"client_cert_ttl"`
}

// SecurityConfig 为认证与加密配置。master_key_file 为敏感字段加密的 KEK 路径。
type SecurityConfig struct {
	MasterKeyFile           string        `mapstructure:"master_key_file"`
	JWTAccessTTL            time.Duration `mapstructure:"jwt_access_ttl"`
	JWTRefreshTTL           time.Duration `mapstructure:"jwt_refresh_ttl"`
	JWTRememberTTL          time.Duration `mapstructure:"jwt_remember_ttl"`
	BcryptCost              int           `mapstructure:"bcrypt_cost"`
	LoginFailLimit          int           `mapstructure:"login_fail_limit"`
	LoginLockDuration       time.Duration `mapstructure:"login_lock_duration"`
	IPWhitelist             []string      `mapstructure:"ip_whitelist"`
	TOTPIssuer              string        `mapstructure:"totp_issuer"`
	SessionMaxPerUser       int           `mapstructure:"session_max_per_user"`
	CommandBlacklistEnabled bool          `mapstructure:"command_blacklist_enabled"`
	TerminalRecordEnabled   bool          `mapstructure:"terminal_record_enabled"`
	DangerousActionReauth   bool          `mapstructure:"dangerous_action_reauth"`
}

// MonitorConfig 为监控采集配置。
type MonitorConfig struct {
	Enabled         bool          `mapstructure:"enabled"`
	CollectInterval time.Duration `mapstructure:"collect_interval"`
	QueueSize       int           `mapstructure:"queue_size"`
	BatchSize       int           `mapstructure:"batch_size"`
	BatchInterval   time.Duration `mapstructure:"batch_interval"`
	TSDB            TSDBConfig    `mapstructure:"tsdb"`
}

// TSDBConfig 为时序存储与保留期配置。
type TSDBConfig struct {
	Driver         string        `mapstructure:"driver"`
	RetentionRaw   time.Duration `mapstructure:"retention_raw"`
	Retention5m    time.Duration `mapstructure:"retention_5m"`
	Retention1h    time.Duration `mapstructure:"retention_1h"`
	DownsampleCron string        `mapstructure:"downsample_cron"`
}

// LogConfig 为日志输出配置。
type LogConfig struct {
	Level              string `mapstructure:"level"`
	Dir                string `mapstructure:"dir"`
	MaxSizeMB          int    `mapstructure:"max_size_mb"`
	MaxBackups         int    `mapstructure:"max_backups"`
	MaxAgeDays         int    `mapstructure:"max_age_days"`
	Compress           bool   `mapstructure:"compress"`
	Console            bool   `mapstructure:"console"`
	AuditRetentionDays int    `mapstructure:"audit_retention_days"`
}

// RuntimeConfig 为并发与限流配置。
type RuntimeConfig struct {
	TaskPoolSize       int `mapstructure:"task_pool_size"`
	BGPoolSize         int `mapstructure:"bg_pool_size"`
	MaxStreams         int `mapstructure:"max_streams"`
	GlobalRateLimit    int `mapstructure:"global_rate_limit"`
	UserWriteRateLimit int `mapstructure:"user_write_rate_limit"`
}

// ClusterConfig 为集群模式配置。
type ClusterConfig struct {
	Mode           string `mapstructure:"mode"`
	BuiltinAgent   bool   `mapstructure:"builtin_agent"`
	DriftCheckCron string `mapstructure:"drift_check_cron"`
	NodeID         int64  `mapstructure:"node_id"`
}

// Load 读取配置文件、套用默认值与 NOVA_ 环境变量覆盖，并执行校验。
// path 为空时只使用默认值与环境变量（容器部署场景）。
func Load(path string) (*Config, error) {
	v := viper.New()
	setDefaults(v)
	v.SetEnvPrefix("NOVA")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	v.AutomaticEnv()

	if path != "" {
		v.SetConfigFile(path)
		if err := v.ReadInConfig(); err != nil {
			return nil, errs.Wrap(err, errs.CodeInvalidParam, "配置文件加载失败").
				WithField("configFile", path)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, errs.Wrap(err, errs.CodeInvalidParam, "配置文件格式错误").
			WithField("configFile", path)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func setDefaults(v *viper.Viper) {
	d := map[string]any{
		"server.host":                 "0.0.0.0",
		"server.port":                 34567,
		"server.base_path":            "/",
		"server.default_locale":       "zh-CN",
		"server.read_timeout":         "30s",
		"server.write_timeout":        "30s",
		"server.idle_timeout":         "120s",
		"server.shutdown_timeout":     "15s",
		"server.entrance":             "/",
		"server.max_body_size_mb":     32,
		"server.tls.enabled":          true,
		"server.tls.cert_file":        "/opt/novapanel/conf/certs/panel.crt",
		"server.tls.key_file":         "/opt/novapanel/conf/certs/panel.key",
		"server.tls.min_version":      "1.2",
		"server.tls.auto_self_signed": true,

		"database.driver":            "sqlite",
		"database.dsn":               "",
		"database.path":              "/opt/novapanel/data/nova.db",
		"database.max_open_conns":    25,
		"database.max_idle_conns":    10,
		"database.conn_max_lifetime": "1h",
		"database.slow_threshold":    "200ms",
		"database.log_level":         "warn",
		"database.auto_migrate":      true,

		"kv.driver":          "bbolt",
		"kv.path":            "/opt/novapanel/data/kv.bolt",
		"kv.redis.addr":      "127.0.0.1:6379",
		"kv.redis.password":  "",
		"kv.redis.db":        0,
		"kv.redis.pool_size": 20,

		"agent.grpc_port":            34568,
		"agent.advertise_addr":       "",
		"agent.heartbeat_interval":   "10s",
		"agent.offline_threshold":    3,
		"agent.session_ttl":          "5m",
		"agent.buffer_max_mb":        64,
		"agent.task_default_timeout": "60s",
		"agent.register_token_ttl":   "30m",
		"agent.mtls.ca_cert":         "/opt/novapanel/conf/certs/ca.crt",
		"agent.mtls.ca_key":          "/opt/novapanel/conf/certs/ca.key",
		"agent.mtls.server_cert":     "/opt/novapanel/conf/certs/agent-server.crt",
		"agent.mtls.server_key":      "/opt/novapanel/conf/certs/agent-server.key",
		"agent.mtls.client_cert_ttl": "8760h",

		"security.master_key_file":           "/opt/novapanel/conf/master.key",
		"security.jwt_access_ttl":            "15m",
		"security.jwt_refresh_ttl":           "168h",
		"security.jwt_remember_ttl":          "720h",
		"security.bcrypt_cost":               12,
		"security.login_fail_limit":          5,
		"security.login_lock_duration":       "15m",
		"security.ip_whitelist":              []string{},
		"security.totp_issuer":               "NovaPanel",
		"security.session_max_per_user":      5,
		"security.command_blacklist_enabled": true,
		"security.terminal_record_enabled":   true,
		"security.dangerous_action_reauth":   true,

		"monitor.enabled":              true,
		"monitor.collect_interval":     "15s",
		"monitor.queue_size":           50000,
		"monitor.batch_size":           1000,
		"monitor.batch_interval":       "2s",
		"monitor.tsdb.driver":          "sqlite",
		"monitor.tsdb.retention_raw":   "168h",
		"monitor.tsdb.retention_5m":    "720h",
		"monitor.tsdb.retention_1h":    "8760h",
		"monitor.tsdb.downsample_cron": "*/5 * * * *",

		"log.level":                "info",
		"log.dir":                  "/opt/novapanel/logs",
		"log.max_size_mb":          100,
		"log.max_backups":          10,
		"log.max_age_days":         30,
		"log.compress":             true,
		"log.console":              false,
		"log.audit_retention_days": 180,

		"runtime.task_pool_size":        200,
		"runtime.bg_pool_size":          32,
		"runtime.max_streams":           200,
		"runtime.global_rate_limit":     2000,
		"runtime.user_write_rate_limit": 60,

		"cluster.mode":             "auto",
		"cluster.builtin_agent":    true,
		"cluster.drift_check_cron": "0 * * * *",
		"cluster.node_id":          1,
	}
	for k, val := range d {
		v.SetDefault(k, val)
	}
}
