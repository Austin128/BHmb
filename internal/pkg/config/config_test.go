package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/novapanel/novapanel/internal/pkg/errs"
)

// writeConfig 生成一份指向临时目录的最小可用配置文件。
func writeConfig(t *testing.T, extra string) string {
	t.Helper()
	dir := t.TempDir()
	body := fmt.Sprintf(`
server:
  port: 34567
  tls:
    enabled: false
database:
  driver: sqlite
  path: %[1]s/nova.db
kv:
  driver: bbolt
  path: %[1]s/kv.bolt
security:
  master_key_file: %[1]s/master.key
log:
  dir: %[1]s/logs
`, dir)
	path := filepath.Join(dir, "panel.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body+extra), 0o600))
	return path
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(writeConfig(t, ""))
	require.NoError(t, err)

	assert.Equal(t, "0.0.0.0", cfg.Server.Host)
	assert.Equal(t, 34567, cfg.Server.Port)
	assert.Equal(t, 15*time.Second, cfg.Server.ShutdownTimeout)
	assert.Equal(t, "sqlite", cfg.Database.Driver)
	assert.True(t, cfg.Database.AutoMigrate)
	assert.Equal(t, 12, cfg.Security.BcryptCost)
	assert.Equal(t, 15*time.Minute, cfg.Security.JWTAccessTTL)
	assert.Equal(t, 168*time.Hour, cfg.Security.JWTRefreshTTL)
	assert.Equal(t, "info", cfg.Log.Level)
	assert.Equal(t, 34568, cfg.Agent.GRPCPort)
	assert.Equal(t, "auto", cfg.Cluster.Mode)
}

func TestLoadEnvOverride(t *testing.T) {
	path := writeConfig(t, "")
	t.Setenv("NOVA_LOG_LEVEL", "debug")
	t.Setenv("NOVA_SERVER_PORT", "18080")
	t.Setenv("NOVA_SERVER_READ_TIMEOUT", "45s")
	t.Setenv("NOVA_DATABASE_MAX_OPEN_CONNS", "7")
	t.Setenv("NOVA_DATABASE_MAX_IDLE_CONNS", "3")
	t.Setenv("NOVA_DATABASE_AUTO_MIGRATE", "false")

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, "debug", cfg.Log.Level)
	assert.Equal(t, 18080, cfg.Server.Port)
	assert.Equal(t, 45*time.Second, cfg.Server.ReadTimeout)
	assert.Equal(t, 7, cfg.Database.MaxOpenConns)
	assert.Equal(t, 3, cfg.Database.MaxIdleConns)
	assert.False(t, cfg.Database.AutoMigrate)
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "not-exists.yaml"))
	require.Error(t, err)
	assert.Equal(t, errs.CodeInvalidParam, errs.Code(err))
	assert.Contains(t, err.Error(), "配置文件加载失败")
}

func TestLoadRejectsBadPortFromEnv(t *testing.T) {
	path := writeConfig(t, "")
	t.Setenv("NOVA_SERVER_PORT", "70000")

	_, err := Load(path)
	require.Error(t, err)
	assert.Equal(t, errs.CodeInvalidParam, errs.Code(err))
	assert.Contains(t, err.Error(), "server.port")
}

func TestValidateRejectsSqliteWithDSN(t *testing.T) {
	cfg := validCfg(t)
	cfg.Database.DSN = "root@tcp(127.0.0.1:3306)/nova"
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "database.dsn")
}

func TestValidateRejectsMysqlWithoutDSN(t *testing.T) {
	cfg := validCfg(t)
	cfg.Database.Driver = "mysql"
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "database.dsn")
}

func TestValidateRejectsUnknownDriver(t *testing.T) {
	cfg := validCfg(t)
	cfg.Database.Driver = "oracle"
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "database.driver")
}

func TestValidateRejectsNonPositiveDuration(t *testing.T) {
	cfg := validCfg(t)
	cfg.Server.ReadTimeout = 0
	cfg.Agent.SessionTTL = -time.Second
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "server.read_timeout")
	assert.Contains(t, err.Error(), "agent.session_ttl")
}

func TestValidateRejectsMasterKeyPermission(t *testing.T) {
	cfg := validCfg(t)
	require.NoError(t, os.WriteFile(cfg.Security.MasterKeyFile, []byte("0123456789abcdef0123456789abcdef"), 0o644))

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "security.master_key_file")
	assert.Contains(t, err.Error(), "0600")

	require.NoError(t, os.Chmod(cfg.Security.MasterKeyFile, 0o600))
	assert.NoError(t, cfg.Validate(), "权限修正为 0600 后应通过")
}

func TestValidateAllowsMissingMasterKey(t *testing.T) {
	cfg := validCfg(t)
	_, statErr := os.Stat(cfg.Security.MasterKeyFile)
	require.True(t, os.IsNotExist(statErr))
	assert.NoError(t, cfg.Validate(), "首次启动允许主密钥缺失，由启动流程生成")
}

func TestValidateRejectsRetentionOrder(t *testing.T) {
	cfg := validCfg(t)
	cfg.Monitor.TSDB.RetentionRaw = 800 * time.Hour
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "monitor.tsdb.retention_5m")
}

func TestValidateRejectsTLSFilesMissing(t *testing.T) {
	cfg := validCfg(t)
	cfg.Server.TLS.Enabled = true
	cfg.Server.TLS.AutoSelfSigned = false
	cfg.Server.TLS.CertFile = filepath.Join(t.TempDir(), "panel.crt")
	cfg.Server.TLS.KeyFile = filepath.Join(t.TempDir(), "panel.key")

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "server.tls.cert_file")
	assert.Contains(t, err.Error(), "server.tls.key_file")
}

func TestValidateRejectsSamePort(t *testing.T) {
	cfg := validCfg(t)
	cfg.Agent.GRPCPort = cfg.Server.Port
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agent.grpc_port")
}

func TestValidateRejectsBadIPWhitelist(t *testing.T) {
	cfg := validCfg(t)
	cfg.Security.IPWhitelist = []string{"10.0.0.0/8", "300.1.1.1", "not-a-cidr/33"}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "security.ip_whitelist[1]")
	assert.Contains(t, err.Error(), "security.ip_whitelist[2]")
	assert.NotContains(t, err.Error(), "security.ip_whitelist[0]")
}

func TestValidateRejectsTokenTTLOrder(t *testing.T) {
	cfg := validCfg(t)
	cfg.Security.JWTAccessTTL = 200 * time.Hour
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "security.jwt_access_ttl")
}

func TestValidateReportsAllKeysAtOnce(t *testing.T) {
	cfg := validCfg(t)
	cfg.Server.Port = 0
	cfg.Log.Level = "trace"
	cfg.Cluster.Mode = "ha"

	err := cfg.Validate()
	require.Error(t, err)
	for _, key := range []string{"server.port", "log.level", "cluster.mode"} {
		assert.Contains(t, err.Error(), key)
	}
	e, ok := errs.As(err)
	require.True(t, ok)
	assert.Len(t, e.Fields, 3, "每个非法键都应出现在结构化字段中")
}

func TestCheckPortsDetectsOccupied(t *testing.T) {
	cfg := validCfg(t)
	cfg.Server.Host = "127.0.0.1"
	ln := mustListen(t, cfg.Server.Host)
	defer ln.close()
	cfg.Server.Port = ln.port

	err := cfg.CheckPorts()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "server.port")
}

type listener struct {
	port  int
	close func()
}

func mustListen(t *testing.T, host string) listener {
	t.Helper()
	ln, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	require.NoError(t, err)
	return listener{
		port:  ln.Addr().(*net.TCPAddr).Port,
		close: func() { _ = ln.Close() },
	}
}

// validCfg 通过临时目录构造一份通过校验的配置。
func validCfg(t *testing.T) *Config {
	t.Helper()
	cfg, err := Load(writeConfig(t, ""))
	require.NoError(t, err)
	require.True(t, strings.HasSuffix(cfg.Database.Path, "nova.db"))
	return cfg
}
