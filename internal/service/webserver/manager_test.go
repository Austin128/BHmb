package webserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/novapanel/novapanel/internal/pkg/config"
	"github.com/novapanel/novapanel/internal/pkg/errs"
)

// fakeRunner 记录调用并按调用者设定的规则返回失败，避免测试依赖宿主机 Nginx。
type fakeRunner struct {
	mu    sync.Mutex
	calls []string

	versionOut string
	dumpOut    string
	// testErr / reloadErr 为真时对应命令失败。
	testErr   bool
	reloadErr bool
	// testErrOnce 只让第一次 -t 失败，用于验证「回滚后复检通过」的分支。
	testErrOnce bool
	testCount   int
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) (Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	call := strings.TrimSpace(name + " " + strings.Join(args, " "))
	f.calls = append(f.calls, call)

	switch {
	case len(args) == 1 && args[0] == "-v":
		return Result{Stderr: f.versionOut}, nil
	case len(args) == 1 && args[0] == "-T":
		return Result{Stdout: f.dumpOut}, nil
	case len(args) == 1 && args[0] == "-t":
		f.testCount++
		if f.testErr || (f.testErrOnce && f.testCount == 1) {
			return Result{Stderr: "nginx: [emerg] unknown directive \"lisen\""}, assert.AnError
		}
		return Result{Stderr: "syntax is ok\ntest is successful"}, nil
	default:
		// 其余命令按 reload 处理：既可能是 nginx -s reload，也可能是自定义命令。
		if f.reloadErr {
			return Result{Stderr: "reload failed: no such process"}, assert.AnError
		}
		return Result{Stdout: "reloaded"}, nil
	}
}

func (f *fakeRunner) callsOf(sub string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if strings.Contains(c, sub) {
			n++
		}
	}
	return n
}

// fakeBin 造一个可执行的假 nginx，让 Detect 的 os.Stat 与权限检查能通过。
func fakeBin(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0o755))
	return p
}

type env struct {
	m      *Manager
	runner *fakeRunner
	cfg    config.WebsiteConfig
}

func newEnv(t *testing.T, mutate func(*config.WebsiteConfig), runner *fakeRunner) *env {
	t.Helper()
	base := t.TempDir()
	cfg := config.WebsiteConfig{
		Enabled:    true,
		ServerType: "auto",
		NginxBin:   fakeBin(t, base, "nginx"),
		VhostDir:   filepath.Join(base, "vhost"),
		WwwRoot:    filepath.Join(base, "wwwroot"),
		LogDir:     filepath.Join(base, "wwwlogs"),
	}
	if mutate != nil {
		mutate(&cfg)
	}
	if runner == nil {
		runner = &fakeRunner{versionOut: "nginx version: nginx/1.24.0"}
	}
	return &env{m: NewManager(cfg, runner), runner: runner, cfg: cfg}
}

func TestDetectReportsMissingWebServer(t *testing.T) {
	e := newEnv(t, func(c *config.WebsiteConfig) {
		c.NginxBin = filepath.Join(t.TempDir(), "absent-nginx")
	}, nil)

	_, err := e.m.Detect(context.Background())
	require.Error(t, err)
	assert.Equal(t, errs.CodeUnsupported, errs.Code(err))

	st := e.m.Status(context.Background())
	assert.False(t, st.Installed)
	assert.Contains(t, st.Message, "未检测到 Nginx")
	assert.Equal(t, e.cfg.VhostDir, st.VhostDir, "未安装也要回显托管目录，前端才能给出 include 指引")
	assert.Nil(t, st.VhostIncluded, "探测不到引擎时 include 状态是未知，不能报告为 false")
}

func TestDetectParsesVersionAndCachesResult(t *testing.T) {
	e := newEnv(t, nil, &fakeRunner{versionOut: "nginx version: openresty/1.25.3.1"})

	eng, err := e.m.Detect(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "openresty", eng.Kind, "输出里出现 openresty 时应纠正引擎类型")
	assert.Equal(t, "1.25.3.1", eng.Version)

	_, err = e.m.Detect(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, e.runner.callsOf("-v"), "缓存期内不应重复 fork 探测进程")
}

func TestStatusDetectsMissingInclude(t *testing.T) {
	e := newEnv(t, nil, &fakeRunner{versionOut: "nginx version: nginx/1.24.0", dumpOut: "http { include /etc/nginx/conf.d/*.conf; }"})

	st := e.m.Status(context.Background())
	require.True(t, st.Installed)
	assert.True(t, st.ConfigOK)
	require.NotNil(t, st.VhostIncluded)
	assert.False(t, *st.VhostIncluded)
	assert.Contains(t, st.Message, "主配置未 include")
}

func TestStatusReportsIncludedVhost(t *testing.T) {
	base := t.TempDir()
	vhost := filepath.Join(base, "vhost")
	e := newEnv(t, func(c *config.WebsiteConfig) { c.VhostDir = vhost }, &fakeRunner{
		versionOut: "nginx version: nginx/1.24.0",
		dumpOut:    "http { include " + vhost + "/*.conf; }",
	})

	st := e.m.Status(context.Background())
	require.NotNil(t, st.VhostIncluded)
	assert.True(t, *st.VhostIncluded)
	assert.Empty(t, st.Message)
}

func TestDeployWritesConfigAndReloads(t *testing.T) {
	e := newEnv(t, nil, nil)
	ctx := context.Background()

	require.NoError(t, e.m.Deploy(ctx, "shop", "server { listen 80; }"))

	target := e.m.VhostPath("shop")
	b, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "server { listen 80; }", string(b))

	info, err := os.Stat(target)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), info.Mode().Perm(), "配置对 Nginx 可读、对其他人不可写")

	dirInfo, err := os.Stat(e.cfg.VhostDir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o750), dirInfo.Mode().Perm())

	assert.Equal(t, 1, e.runner.callsOf("-t"))
	assert.Equal(t, 1, e.runner.callsOf("-s reload"))

	// 目录里不应残留原子替换用的临时文件。
	entries, err := os.ReadDir(e.cfg.VhostDir)
	require.NoError(t, err)
	for _, ent := range entries {
		assert.False(t, strings.HasPrefix(ent.Name(), ".nova-vhost-"), "临时文件必须被清理：%s", ent.Name())
	}
}

func TestDeployUsesCustomReloadCommand(t *testing.T) {
	e := newEnv(t, func(c *config.WebsiteConfig) { c.ReloadCmd = "/bin/systemctl reload nginx" }, nil)

	require.NoError(t, e.m.Deploy(context.Background(), "shop", "server { }"))
	assert.Equal(t, 1, e.runner.callsOf("systemctl reload nginx"))
	assert.Zero(t, e.runner.callsOf("-s reload"), "配置了自定义命令就不应再走 nginx -s reload")
}

func TestDeployRemovesNewFileWhenSyntaxCheckFails(t *testing.T) {
	e := newEnv(t, nil, &fakeRunner{versionOut: "nginx version: nginx/1.24.0", testErr: true})
	ctx := context.Background()

	err := e.m.Deploy(ctx, "shop", "lisen 80;")
	require.Error(t, err)
	assert.Equal(t, errs.CodeNginxTestFailed, errs.Code(err))
	be, ok := errs.As(err)
	require.True(t, ok)
	assert.Contains(t, be.Detail, "unknown directive", "错误详情要带上 nginx 的原始诊断")

	_, statErr := os.Stat(e.m.VhostPath("shop"))
	assert.True(t, os.IsNotExist(statErr), "新站点校验失败后不得留下配置文件")
	assert.Zero(t, e.runner.callsOf("-s reload"), "校验失败不得 reload")
}

func TestDeployRestoresPreviousConfigWhenSyntaxCheckFails(t *testing.T) {
	e := newEnv(t, nil, &fakeRunner{versionOut: "nginx version: nginx/1.24.0"})
	ctx := context.Background()

	require.NoError(t, e.m.Deploy(ctx, "shop", "server { # v1 }"))

	e.runner.mu.Lock()
	e.runner.testErr = true
	e.runner.mu.Unlock()

	err := e.m.Deploy(ctx, "shop", "server { # broken")
	require.Error(t, err)
	assert.Contains(t, errs.Message(err), "已回滚")

	b, readErr := os.ReadFile(e.m.VhostPath("shop"))
	require.NoError(t, readErr)
	assert.Equal(t, "server { # v1 }", string(b), "失败后必须恢复上一版本内容")

	// 被替换掉的旧配置应归档，便于人工排查。
	history, err := os.ReadDir(filepath.Join(e.cfg.VhostDir, "history", "shop"))
	require.NoError(t, err)
	assert.NotEmpty(t, history)
}

func TestDeployReportsHostConfigBrokenBeforeChange(t *testing.T) {
	e := newEnv(t, nil, &fakeRunner{versionOut: "nginx version: nginx/1.24.0", testErr: true})

	err := e.m.Deploy(context.Background(), "shop", "server { }")
	require.Error(t, err)
	assert.Contains(t, errs.Message(err), "宿主机原有配置本身校验未通过",
		"回滚后复检仍失败时要指向主配置，避免用户误判是本次改动")
}

func TestDeployRestoresConfigWhenReloadFails(t *testing.T) {
	e := newEnv(t, nil, &fakeRunner{versionOut: "nginx version: nginx/1.24.0"})
	ctx := context.Background()

	require.NoError(t, e.m.Deploy(ctx, "shop", "server { # v1 }"))

	e.runner.mu.Lock()
	e.runner.reloadErr = true
	e.runner.mu.Unlock()

	err := e.m.Deploy(ctx, "shop", "server { # v2 }")
	require.Error(t, err)
	assert.Equal(t, errs.CodeExecutorFailed, errs.Code(err))

	b, readErr := os.ReadFile(e.m.VhostPath("shop"))
	require.NoError(t, readErr)
	assert.Equal(t, "server { # v1 }", string(b), "reload 失败也要回到上一版本")
}

func TestRemoveDeletesConfigAndReloads(t *testing.T) {
	e := newEnv(t, nil, nil)
	ctx := context.Background()
	require.NoError(t, e.m.Deploy(ctx, "shop", "server { }"))

	require.NoError(t, e.m.Remove(ctx, "shop"))
	_, statErr := os.Stat(e.m.VhostPath("shop"))
	assert.True(t, os.IsNotExist(statErr))

	// 删除同样归档，误删后还能找回内容。
	history, err := os.ReadDir(filepath.Join(e.cfg.VhostDir, "history", "shop"))
	require.NoError(t, err)
	assert.NotEmpty(t, history)

	// 幂等：文件已不存在时再删一次不报错。
	require.NoError(t, e.m.Remove(ctx, "shop"))
}

func TestRemoveWithoutWebServerStillCleansFile(t *testing.T) {
	e := newEnv(t, nil, nil)
	ctx := context.Background()
	require.NoError(t, e.m.Deploy(ctx, "shop", "server { }"))

	// 模拟 Nginx 被卸载：可执行文件消失且探测缓存失效。
	require.NoError(t, os.Remove(e.cfg.NginxBin))
	stale := NewManager(e.cfg, e.runner)

	require.NoError(t, stale.Remove(ctx, "shop"), "未安装 Web 服务时删除站点不应被阻塞")
	_, statErr := os.Stat(stale.VhostPath("shop"))
	assert.True(t, os.IsNotExist(statErr))
}

func TestRemoveRestoresConfigWhenReloadFails(t *testing.T) {
	e := newEnv(t, nil, nil)
	ctx := context.Background()
	require.NoError(t, e.m.Deploy(ctx, "shop", "server { # v1 }"))

	e.runner.mu.Lock()
	e.runner.reloadErr = true
	e.runner.mu.Unlock()

	err := e.m.Remove(ctx, "shop")
	require.Error(t, err)
	b, readErr := os.ReadFile(e.m.VhostPath("shop"))
	require.NoError(t, readErr)
	assert.Equal(t, "server { # v1 }", string(b), "reload 失败要把配置放回去，避免面板与宿主机状态漂移")
}

func TestTestAndReloadSurfaceEngineErrors(t *testing.T) {
	e := newEnv(t, nil, &fakeRunner{versionOut: "nginx version: nginx/1.24.0"})
	ctx := context.Background()

	out, err := e.m.Test(ctx)
	require.NoError(t, err)
	assert.Contains(t, out, "test is successful")
	require.NoError(t, e.m.Reload(ctx))

	e.runner.mu.Lock()
	e.runner.testErr, e.runner.reloadErr = true, true
	e.runner.mu.Unlock()

	out, err = e.m.Test(ctx)
	require.Error(t, err)
	assert.Equal(t, errs.CodeNginxTestFailed, errs.Code(err))
	assert.Contains(t, out, "unknown directive")

	err = e.m.Reload(ctx)
	require.Error(t, err)
	assert.Equal(t, errs.CodeExecutorFailed, errs.Code(err))
}

func TestDeployIsSerializedAcrossGoroutines(t *testing.T) {
	e := newEnv(t, nil, nil)
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			assert.NoError(t, e.m.Deploy(ctx, "shop", "server { # writer }"))
		}(i)
	}
	wg.Wait()

	// 并发下发同一站点后，文件内容必须是完整的一份，不能是交叉写入的碎片。
	b, err := os.ReadFile(e.m.VhostPath("shop"))
	require.NoError(t, err)
	assert.Equal(t, "server { # writer }", string(b))
}
