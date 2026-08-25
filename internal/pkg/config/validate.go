package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/novapanel/novapanel/internal/pkg/errs"
)

// validator 收集全部违规键，一次性报出，避免逐条修配置。
type validator struct {
	problems map[string]string
}

func newValidator() *validator { return &validator{problems: map[string]string{}} }

func (v *validator) add(key, reason string) { v.problems[key] = reason }

func (v *validator) addf(key, format string, args ...any) {
	v.problems[key] = fmt.Sprintf(format, args...)
}

func (v *validator) positive(key string, d time.Duration) {
	if d <= 0 {
		v.addf(key, "时长必须为正，当前 %v", d)
	}
}

func (v *validator) positiveInt(key string, n int) {
	if n <= 0 {
		v.addf(key, "必须为正整数，当前 %d", n)
	}
}

func (v *validator) port(key string, p int) {
	if p < 1 || p > 65535 {
		v.addf(key, "端口需在 1-65535 之间，当前 %d", p)
	}
}

func (v *validator) oneOf(key, got string, allowed ...string) {
	for _, a := range allowed {
		if got == a {
			return
		}
	}
	v.addf(key, "取值必须是 %s 之一，当前 %q", strings.Join(allowed, "/"), got)
}

func (v *validator) err() error {
	if len(v.problems) == 0 {
		return nil
	}
	keys := make([]string, 0, len(v.problems))
	for k := range v.problems {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	detail := make([]string, 0, len(keys))
	fields := make(map[string]any, len(keys))
	for _, k := range keys {
		detail = append(detail, k+": "+v.problems[k])
		fields[k] = v.problems[k]
	}
	e := errs.New(errs.CodeInvalidParam, "配置校验失败："+strings.Join(detail, "；")).
		WithDetail("%s", strings.Join(detail, "; "))
	for k, val := range fields {
		e = e.WithField(k, val)
	}
	return e
}

// Validate 校验配置合法性：端口范围、TLS 文件可读、数据目录可写、
// master_key_file 权限 0600、driver 与配置段一致、时长为正、保留期递增。
// 端口占用属于运行时状态，由 CheckPorts 单独检查，便于单测与热加载复用本方法。
func (c *Config) Validate() error {
	v := newValidator()
	c.validateServer(v)
	c.validateDatabase(v)
	c.validateKV(v)
	c.validateAgent(v)
	c.validateSecurity(v)
	c.validateWebsite(v)
	c.validateMonitor(v)
	c.validateLog(v)
	c.validateRuntime(v)
	c.validateCluster(v)
	return v.err()
}

func (c *Config) validateServer(v *validator) {
	s := c.Server
	v.port("server.port", s.Port)
	if !strings.HasPrefix(s.BasePath, "/") {
		v.addf("server.base_path", "必须以 / 开头，当前 %q", s.BasePath)
	}
	if !strings.HasPrefix(s.Entrance, "/") {
		v.addf("server.entrance", "必须以 / 开头，当前 %q", s.Entrance)
	}
	if s.Host == "" {
		v.add("server.host", "监听地址不能为空")
	}
	v.oneOf("server.default_locale", s.DefaultLocale, "zh-CN", "en-US")
	v.positive("server.read_timeout", s.ReadTimeout)
	v.positive("server.write_timeout", s.WriteTimeout)
	v.positive("server.idle_timeout", s.IdleTimeout)
	v.positive("server.shutdown_timeout", s.ShutdownTimeout)
	v.positiveInt("server.max_body_size_mb", s.MaxBodySizeMB)

	if !s.TLS.Enabled {
		return
	}
	v.oneOf("server.tls.min_version", s.TLS.MinVersion, "1.2", "1.3")
	if s.TLS.CertFile == "" {
		v.add("server.tls.cert_file", "启用 TLS 时不能为空")
	}
	if s.TLS.KeyFile == "" {
		v.add("server.tls.key_file", "启用 TLS 时不能为空")
	}
	if s.TLS.AutoSelfSigned {
		// 允许文件缺失，启动时自动生成，但目录必须可写
		checkParentWritable(v, "server.tls.cert_file", s.TLS.CertFile)
		return
	}
	checkReadable(v, "server.tls.cert_file", s.TLS.CertFile)
	checkReadable(v, "server.tls.key_file", s.TLS.KeyFile)
}

func (c *Config) validateDatabase(v *validator) {
	d := c.Database
	v.oneOf("database.driver", d.Driver, "sqlite", "mysql", "postgres")
	switch d.Driver {
	case "sqlite":
		if d.Path == "" {
			v.add("database.path", "driver=sqlite 时必填")
		} else {
			checkParentWritable(v, "database.path", d.Path)
		}
		if d.DSN != "" {
			v.add("database.dsn", "driver=sqlite 时必须为空，避免配置歧义")
		}
	case "mysql", "postgres":
		if d.DSN == "" {
			v.addf("database.dsn", "driver=%s 时必填", d.Driver)
		}
	}
	v.positiveInt("database.max_open_conns", d.MaxOpenConns)
	v.positiveInt("database.max_idle_conns", d.MaxIdleConns)
	if d.MaxIdleConns > d.MaxOpenConns {
		v.addf("database.max_idle_conns", "不能大于 max_open_conns(%d)，当前 %d", d.MaxOpenConns, d.MaxIdleConns)
	}
	v.positive("database.conn_max_lifetime", d.ConnMaxLifetime)
	v.positive("database.slow_threshold", d.SlowThreshold)
	v.oneOf("database.log_level", d.LogLevel, "silent", "error", "warn", "info")
}

func (c *Config) validateKV(v *validator) {
	k := c.KV
	v.oneOf("kv.driver", k.Driver, "bbolt", "redis")
	switch k.Driver {
	case "bbolt":
		if k.Path == "" {
			v.add("kv.path", "driver=bbolt 时必填")
		} else {
			checkParentWritable(v, "kv.path", k.Path)
		}
	case "redis":
		if k.Redis.Addr == "" {
			v.add("kv.redis.addr", "driver=redis 时必填")
		}
		v.positiveInt("kv.redis.pool_size", k.Redis.PoolSize)
		if k.Redis.DB < 0 {
			v.addf("kv.redis.db", "不能为负，当前 %d", k.Redis.DB)
		}
	}
}

func (c *Config) validateAgent(v *validator) {
	a := c.Agent
	v.port("agent.grpc_port", a.GRPCPort)
	if a.GRPCPort == c.Server.Port {
		v.addf("agent.grpc_port", "不能与 server.port(%d) 相同", c.Server.Port)
	}
	v.positive("agent.heartbeat_interval", a.HeartbeatInterval)
	v.positiveInt("agent.offline_threshold", a.OfflineThreshold)
	v.positive("agent.session_ttl", a.SessionTTL)
	v.positiveInt("agent.buffer_max_mb", a.BufferMaxMB)
	v.positive("agent.task_default_timeout", a.TaskDefaultTimeout)
	v.positive("agent.register_token_ttl", a.RegisterTokenTTL)
	v.positive("agent.mtls.client_cert_ttl", a.MTLS.ClientCertTTL)
}

func (c *Config) validateSecurity(v *validator) {
	s := c.Security
	if s.MasterKeyFile == "" {
		v.add("security.master_key_file", "不能为空，敏感字段加密依赖该 KEK")
	} else {
		checkMasterKey(v, "security.master_key_file", s.MasterKeyFile)
	}
	v.positive("security.jwt_access_ttl", s.JWTAccessTTL)
	v.positive("security.jwt_refresh_ttl", s.JWTRefreshTTL)
	v.positive("security.jwt_remember_ttl", s.JWTRememberTTL)
	if s.JWTAccessTTL >= s.JWTRefreshTTL {
		v.addf("security.jwt_access_ttl", "必须小于 jwt_refresh_ttl(%v)，当前 %v", s.JWTRefreshTTL, s.JWTAccessTTL)
	}
	if s.JWTRefreshTTL > s.JWTRememberTTL {
		v.addf("security.jwt_remember_ttl", "不能小于 jwt_refresh_ttl(%v)，当前 %v", s.JWTRefreshTTL, s.JWTRememberTTL)
	}
	if s.BcryptCost < 10 || s.BcryptCost > 31 {
		v.addf("security.bcrypt_cost", "需在 10-31 之间（规范值 12），当前 %d", s.BcryptCost)
	}
	v.positiveInt("security.login_fail_limit", s.LoginFailLimit)
	v.positive("security.login_lock_duration", s.LoginLockDuration)
	v.positiveInt("security.session_max_per_user", s.SessionMaxPerUser)
	if s.TOTPIssuer == "" {
		v.add("security.totp_issuer", "不能为空")
	}
	for i, entry := range s.IPWhitelist {
		if entry == "" {
			v.addf(fmt.Sprintf("security.ip_whitelist[%d]", i), "不能为空字符串")
			continue
		}
		if strings.Contains(entry, "/") {
			if _, _, err := net.ParseCIDR(entry); err != nil {
				v.addf(fmt.Sprintf("security.ip_whitelist[%d]", i), "非法 CIDR：%s", entry)
			}
			continue
		}
		if net.ParseIP(entry) == nil {
			v.addf(fmt.Sprintf("security.ip_whitelist[%d]", i), "非法 IP：%s", entry)
		}
	}
}

// validateWebsite 只校验配置自身的自洽性。Nginx 是否已安装属于运行时状态，
// 由建站服务在下发时探测并返回可操作的提示，启动阶段不因未装 Web 服务而拒绝运行。
func (c *Config) validateWebsite(v *validator) {
	w := c.Website
	if !w.Enabled {
		return
	}
	v.oneOf("website.server_type", w.ServerType, "auto", "nginx", "openresty")
	absDir(v, "website.vhost_dir", w.VhostDir)
	absDir(v, "website.www_root", w.WwwRoot)
	absDir(v, "website.log_dir", w.LogDir)
	if w.NginxBin != "" && !filepath.IsAbs(w.NginxBin) {
		v.addf("website.nginx_bin", "必须是绝对路径，当前 %q", w.NginxBin)
	}
	v.positiveInt("website.backup_keep", w.BackupKeep)
}

func absDir(v *validator, key, dir string) {
	if dir == "" {
		v.add(key, "不能为空")
		return
	}
	if !filepath.IsAbs(dir) {
		v.addf(key, "必须是绝对路径，当前 %q", dir)
	}
}

func (c *Config) validateMonitor(v *validator) {
	m := c.Monitor
	if !m.Enabled {
		return
	}
	v.positive("monitor.collect_interval", m.CollectInterval)
	v.positive("monitor.batch_interval", m.BatchInterval)
	v.positiveInt("monitor.queue_size", m.QueueSize)
	v.positiveInt("monitor.batch_size", m.BatchSize)
	if m.BatchSize > m.QueueSize {
		v.addf("monitor.batch_size", "不能大于 queue_size(%d)，当前 %d", m.QueueSize, m.BatchSize)
	}
	v.oneOf("monitor.tsdb.driver", m.TSDB.Driver, "sqlite", "victoriametrics")
	v.positive("monitor.tsdb.retention_raw", m.TSDB.RetentionRaw)
	v.positive("monitor.tsdb.retention_5m", m.TSDB.Retention5m)
	v.positive("monitor.tsdb.retention_1h", m.TSDB.Retention1h)
	if m.TSDB.RetentionRaw >= m.TSDB.Retention5m {
		v.addf("monitor.tsdb.retention_5m", "必须大于 retention_raw(%v)，当前 %v", m.TSDB.RetentionRaw, m.TSDB.Retention5m)
	}
	if m.TSDB.Retention5m >= m.TSDB.Retention1h {
		v.addf("monitor.tsdb.retention_1h", "必须大于 retention_5m(%v)，当前 %v", m.TSDB.Retention5m, m.TSDB.Retention1h)
	}
	if m.TSDB.DownsampleCron == "" {
		v.add("monitor.tsdb.downsample_cron", "不能为空")
	}
}

func (c *Config) validateLog(v *validator) {
	l := c.Log
	v.oneOf("log.level", l.Level, "debug", "info", "warn", "error")
	if l.Dir == "" {
		v.add("log.dir", "不能为空")
	} else {
		checkDirWritable(v, "log.dir", l.Dir)
	}
	v.positiveInt("log.max_size_mb", l.MaxSizeMB)
	v.positiveInt("log.max_backups", l.MaxBackups)
	v.positiveInt("log.max_age_days", l.MaxAgeDays)
	v.positiveInt("log.audit_retention_days", l.AuditRetentionDays)
}

func (c *Config) validateRuntime(v *validator) {
	r := c.Runtime
	v.positiveInt("runtime.task_pool_size", r.TaskPoolSize)
	v.positiveInt("runtime.bg_pool_size", r.BGPoolSize)
	v.positiveInt("runtime.max_streams", r.MaxStreams)
	v.positiveInt("runtime.global_rate_limit", r.GlobalRateLimit)
	v.positiveInt("runtime.user_write_rate_limit", r.UserWriteRateLimit)
}

func (c *Config) validateCluster(v *validator) {
	cl := c.Cluster
	v.oneOf("cluster.mode", cl.Mode, "auto", "standalone", "cluster")
	if cl.NodeID < 0 || cl.NodeID > 1023 {
		v.addf("cluster.node_id", "雪花 workerID 需在 0-1023 之间，当前 %d", cl.NodeID)
	}
	if cl.DriftCheckCron == "" {
		v.add("cluster.drift_check_cron", "不能为空")
	}
}

// CheckPorts 检查监听端口是否已被占用，属运行时校验，仅在真正启动前调用。
func (c *Config) CheckPorts() error {
	v := newValidator()
	checkListenable(v, "server.port", c.Server.Host, c.Server.Port)
	checkListenable(v, "agent.grpc_port", c.Server.Host, c.Agent.GRPCPort)
	return v.err()
}

func checkListenable(v *validator, key, host string, port int) {
	ln, err := net.Listen("tcp", net.JoinHostPort(host, fmt.Sprint(port)))
	if err != nil {
		v.addf(key, "端口 %d 无法监听：%v", port, err)
		return
	}
	_ = ln.Close()
}

func checkReadable(v *validator, key, path string) {
	if path == "" {
		return
	}
	f, err := os.Open(path) //nolint:gosec // 路径来自受信配置
	if err != nil {
		v.addf(key, "文件不存在或不可读：%v", err)
		return
	}
	_ = f.Close()
}

func checkMasterKey(v *validator, key, path string) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		// 首次启动允许缺失，由启动流程生成 32 字节随机密钥，但目录必须可写
		checkParentWritable(v, key, path)
		return
	}
	if err != nil {
		v.addf(key, "无法读取主密钥文件：%v", err)
		return
	}
	if info.IsDir() {
		v.add(key, "必须是文件，当前为目录")
		return
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		v.addf(key, "权限必须为 0600，当前 %04o", perm)
	}
}

func checkParentWritable(v *validator, key, path string) {
	checkDirWritable(v, key, filepath.Dir(path))
}

// checkDirWritable 逐级上溯到最近存在的祖先目录判断可写性，
// 缺失的中间目录由启动流程按 0750 创建。
func checkDirWritable(v *validator, key, dir string) {
	probe := dir
	for {
		info, err := os.Stat(probe)
		if err == nil {
			if !info.IsDir() {
				v.addf(key, "%s 不是目录", probe)
				return
			}
			if !writable(probe) {
				v.addf(key, "目录不可写：%s", probe)
			}
			return
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			v.addf(key, "目录不存在且无法上溯：%s", dir)
			return
		}
		probe = parent
	}
}

func writable(dir string) bool {
	f, err := os.CreateTemp(dir, ".nova-write-probe-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}
