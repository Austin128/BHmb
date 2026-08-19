package logx

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/novapanel/novapanel/internal/pkg/ctxkey"
	"github.com/novapanel/novapanel/internal/pkg/errs"
)

func initTemp(t *testing.T, level string) string {
	t.Helper()
	dir := t.TempDir()
	lg, err := Init(Options{
		Level: level, Dir: dir, MaxSizeMB: 1, MaxBackups: 1, MaxAgeDays: 1,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = lg.Close() })
	return dir
}

// readLines 读取日志文件并解析为 JSON 记录列表。
func readLines(t *testing.T, dir, name string) []map[string]any {
	t.Helper()
	f, err := os.Open(filepath.Join(dir, name))
	if os.IsNotExist(err) {
		return nil // lumberjack 首次写入时才创建文件
	}
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var out []map[string]any
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &m), line)
		out = append(out, m)
	}
	require.NoError(t, sc.Err())
	return out
}

func TestInitCreatesThreeLogFiles(t *testing.T) {
	dir := initTemp(t, "info")
	L().Info("面板启动", "module", "system")
	Access().Info("请求完成", "module", "http")
	Audit().Info("登录成功", "module", "auth")

	for _, name := range []string{FilePanel, FileAccess, FileAudit} {
		info, err := os.Stat(filepath.Join(dir, name))
		require.NoError(t, err, name)
		assert.Positive(t, info.Size(), name)
	}
}

func TestFromContextInjectsTraceFields(t *testing.T) {
	dir := initTemp(t, "info")

	ctx := ctxkey.WithTraceID(context.Background(), "01J9Z3M8Q0ABCDEFGHJKMNPQRS")
	ctx = ctxkey.WithUserID(ctx, 1874923847293847)
	ctx = ctxkey.WithNodeID(ctx, 42)

	FromContext(ctx).Info("站点创建成功", "module", "site", "siteId", int64(9))

	recs := readLines(t, dir, FilePanel)
	require.Len(t, recs, 1)
	r := recs[0]
	assert.Equal(t, "01J9Z3M8Q0ABCDEFGHJKMNPQRS", r["traceId"])
	assert.Equal(t, float64(1874923847293847), r["userId"])
	assert.Equal(t, float64(1), r["tenantId"], "缺省租户为 1")
	assert.Equal(t, float64(42), r["nodeId"])
	assert.Equal(t, "site", r["module"])
	assert.Equal(t, "站点创建成功", r["msg"])
	assert.Equal(t, "INFO", r["level"])
	assert.Contains(t, r, "caller")
	assert.Contains(t, r["time"], "T", "时间为 RFC3339Nano")
	assert.True(t, strings.HasSuffix(r["time"].(string), "Z"), "时间必须为 UTC：%v", r["time"])
}

func TestRedaction(t *testing.T) {
	dir := initTemp(t, "info")
	L().Info("敏感字段脱敏",
		"module", "auth",
		"password", "P@ssw0rd",
		"access_token", "eyJhbGciOi",
		"secretKey", "AKIA123",
		"PrivateKey", "-----BEGIN",
		"authorization", "Bearer x",
		"cookie", "nova_rt=abc",
		"totpCode", "123456",
		"username", "admin",
	)

	r := readLines(t, dir, FilePanel)[0]
	for _, k := range []string{"password", "access_token", "secretKey", "PrivateKey", "authorization", "cookie", "totpCode"} {
		assert.Equal(t, redactMask, r[k], k)
	}
	assert.Equal(t, "admin", r["username"], "非敏感字段不应被脱敏")
}

func TestShouldRedact(t *testing.T) {
	for _, k := range []string{"password", "passwd", "user_password", "APISecret", "token", "accessKey", "secret_key", "private-key", "Authorization", "Cookie", "totp"} {
		assert.True(t, ShouldRedact(k), k)
	}
	for _, k := range []string{"username", "siteId", "module", "keyName"} {
		assert.False(t, ShouldRedact(k), k)
	}
}

func TestRedactCommand(t *testing.T) {
	assert.Equal(t, "mysql -uroot -p*** -e 'show databases'", RedactCommand("mysql -uroot -pS3cret! -e 'show databases'"))
	assert.Contains(t, RedactCommand("curl --token=abcdef https://x"), "--token=***")
	assert.Equal(t, "ls -al /tmp", RedactCommand("ls -al /tmp"))
}

func TestCommandFieldRedacted(t *testing.T) {
	dir := initTemp(t, "info")
	L().Info("执行命令", "module", "executor", "cmd", "mysqldump -uroot -pS3cret nova")
	r := readLines(t, dir, FilePanel)[0]
	assert.Equal(t, "mysqldump -uroot -p*** nova", r["cmd"])
}

func TestLevelFilteringAndHotReload(t *testing.T) {
	dir := initTemp(t, "info")

	L().Debug("不应输出", "module", "system")
	assert.Empty(t, readLines(t, dir, FilePanel))

	require.NoError(t, SetLevel("debug"))
	assert.Equal(t, "debug", Level())
	L().Debug("应输出", "module", "system")
	require.Len(t, readLines(t, dir, FilePanel), 1)

	require.NoError(t, SetLevel("warn"))
	L().Info("再次被过滤", "module", "system")
	assert.Len(t, readLines(t, dir, FilePanel), 1)
}

func TestParseLevelRejectsUnknown(t *testing.T) {
	_, err := ParseLevel("trace")
	require.Error(t, err)
	assert.Equal(t, errs.CodeInvalidParam, errs.Code(err))
	require.Error(t, SetLevel("verbose"))
}

func TestInitRejectsEmptyDir(t *testing.T) {
	_, err := Init(Options{Level: "info", Dir: ""})
	require.Error(t, err)
	assert.Equal(t, errs.CodeInvalidParam, errs.Code(err))
}

func TestErrorSampling(t *testing.T) {
	dir := initTemp(t, "info")

	for range 25 {
		L().Error("数据库写入失败", "module", "site")
	}
	recs := readLines(t, dir, FilePanel)
	assert.Len(t, recs, 10, "同 msg+module 的 error 每分钟最多 10 条")

	L().Error("数据库写入失败", "module", "cluster")
	assert.Len(t, readLines(t, dir, FilePanel), 11, "module 不同则独立计数")
}

func TestErrAttrs(t *testing.T) {
	assert.Nil(t, ErrAttrs(nil))

	err := errs.Wrap(os.ErrPermission, errs.CodeInternal, "读取失败").WithField("path", "/etc/shadow")
	attrs := ErrAttrs(err)
	m := map[string]any{}
	for i := 0; i+1 < len(attrs); i += 2 {
		m[attrs[i].(string)] = attrs[i+1]
	}
	assert.Equal(t, errs.CodeInternal, m["code"])
	assert.Equal(t, "permission denied", m["err"], "err 取 Detail")
	assert.Equal(t, "/etc/shadow", m["path"])
}

func TestFallbackLoggerBeforeInit(t *testing.T) {
	assert.NotNil(t, L(), "Init 之前也必须可用，避免早期日志丢失")
	assert.NotNil(t, FromContext(context.Background()))
}
