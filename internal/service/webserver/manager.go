package webserver

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/novapanel/novapanel/internal/pkg/config"
	"github.com/novapanel/novapanel/internal/pkg/errs"
)

// Engine 描述探测到的 Web 服务。
type Engine struct {
	// Kind 取 nginx / openresty。
	Kind string `json:"kind"`
	// Bin 为可执行文件绝对路径。
	Bin string `json:"bin"`
	// Version 形如 1.25.3，探测失败时为空。
	Version string `json:"version"`
}

// Status 为建站模块的运行前提检查结果，前端据此提示用户，而不是让操作失败在下发阶段。
type Status struct {
	// Installed 为真表示找到了可执行的 Nginx/OpenResty。
	Installed bool   `json:"installed"`
	Kind      string `json:"kind"`
	Bin       string `json:"bin"`
	Version   string `json:"version"`
	// VhostDir 为面板托管的站点配置目录。
	VhostDir string `json:"vhostDir"`
	// VhostIncluded 为真表示主配置已 include VhostDir，站点配置才会真正生效。
	// nginx -T 不可用时为 nil，表示未知，不做失败判定。
	VhostIncluded *bool `json:"vhostIncluded"`
	// ConfigOK 为最近一次 nginx -t 结果。
	ConfigOK bool `json:"configOK"`
	// Message 为不可用原因或校验输出摘要。
	Message string `json:"message"`
	// WwwRoot 与 LogDir 回显给前端做默认值提示。
	WwwRoot string `json:"wwwRoot"`
	LogDir  string `json:"logDir"`
}

// Manager 管理 vhost 目录与 Web 服务命令。所有方法可并发调用，
// 下发通过互斥锁串行化：nginx -t 校验的是全局配置，并发替换会互相污染判定结果。
type Manager struct {
	cfg    config.WebsiteConfig
	runner Runner

	mu sync.Mutex

	cacheMu sync.Mutex
	engine  *Engine
	// cachedAt 用于短期缓存探测结果，避免每次列表请求都 fork 进程。
	cachedAt time.Time
}

// nginx 版本形如 "nginx version: openresty/1.25.3.1"。
var versionRe = regexp.MustCompile(`([0-9]+(?:\.[0-9]+)+)`)

// detectCacheTTL 为引擎探测缓存时长，装完 Nginx 后最迟 30 秒被面板感知。
const detectCacheTTL = 30 * time.Second

// NewManager 构造管理器。runner 为空时使用真实命令执行器。
func NewManager(cfg config.WebsiteConfig, runner Runner) *Manager {
	if runner == nil {
		runner = NewExecRunner(0)
	}
	return &Manager{cfg: cfg, runner: runner}
}

// Config 返回建站配置副本，供服务层读取目录约定。
func (m *Manager) Config() config.WebsiteConfig { return m.cfg }

// candidates 返回按优先级排列的可执行文件候选。显式配置优先，其次按 server_type 决定顺序。
func (m *Manager) candidates() []struct{ kind, bin string } {
	type cand = struct{ kind, bin string }
	if m.cfg.NginxBin != "" {
		kind := m.cfg.ServerType
		if kind == "" || kind == "auto" {
			kind = "nginx"
			if strings.Contains(m.cfg.NginxBin, "openresty") {
				kind = "openresty"
			}
		}
		return []cand{{kind, m.cfg.NginxBin}}
	}

	openresty := []cand{
		{"openresty", "/usr/local/openresty/nginx/sbin/nginx"},
		{"openresty", "/usr/local/openresty/bin/openresty"},
	}
	nginx := []cand{
		{"nginx", "/usr/local/nginx/sbin/nginx"},
		{"nginx", "/usr/sbin/nginx"},
		{"nginx", "/usr/bin/nginx"},
		{"nginx", "/opt/homebrew/bin/nginx"},
		{"nginx", "/usr/local/bin/nginx"},
	}
	// PATH 查找放在最后：显式路径更能反映面板安装脚本装的那一份。
	if p, err := exec.LookPath("nginx"); err == nil {
		nginx = append(nginx, cand{"nginx", p})
	}

	switch m.cfg.ServerType {
	case "nginx":
		return nginx
	case "openresty":
		return openresty
	default:
		return append(openresty, nginx...)
	}
}

// Detect 返回可用引擎。未安装时返回 CodeUnsupported，调用方应转成「请先安装 Web 服务」的提示。
func (m *Manager) Detect(ctx context.Context) (*Engine, error) {
	m.cacheMu.Lock()
	if m.engine != nil && time.Since(m.cachedAt) < detectCacheTTL {
		e := *m.engine
		m.cacheMu.Unlock()
		return &e, nil
	}
	m.cacheMu.Unlock()

	for _, c := range m.candidates() {
		info, err := os.Stat(c.bin)
		if err != nil || info.IsDir() || info.Mode().Perm()&0o111 == 0 {
			continue
		}
		eng := &Engine{Kind: c.kind, Bin: c.bin}
		// nginx -v 把版本写到 stderr，退出码为 0。
		if res, err := m.runner.Run(ctx, c.bin, "-v"); err == nil {
			if mm := versionRe.FindStringSubmatch(res.Combined()); len(mm) > 1 {
				eng.Version = mm[1]
			}
			if strings.Contains(strings.ToLower(res.Combined()), "openresty") {
				eng.Kind = "openresty"
			}
		}
		m.cacheMu.Lock()
		m.engine, m.cachedAt = eng, time.Now()
		m.cacheMu.Unlock()
		e := *eng
		return &e, nil
	}
	return nil, errs.New(errs.CodeUnsupported,
		"未检测到 Nginx 或 OpenResty，请先安装 Web 服务后再创建站点").
		WithField("serverType", m.cfg.ServerType)
}

// Status 汇总运行前提：是否安装、主配置是否 include vhost 目录、当前配置是否通过校验。
func (m *Manager) Status(ctx context.Context) *Status {
	st := &Status{VhostDir: m.cfg.VhostDir, WwwRoot: m.cfg.WwwRoot, LogDir: m.cfg.LogDir}
	eng, err := m.Detect(ctx)
	if err != nil {
		st.Message = errs.Message(err)
		return st
	}
	st.Installed = true
	st.Kind, st.Bin, st.Version = eng.Kind, eng.Bin, eng.Version

	if res, err := m.runner.Run(ctx, eng.Bin, "-t"); err == nil {
		st.ConfigOK = true
	} else {
		st.Message = res.Combined()
	}

	// nginx -T 导出全部生效配置；只要出现 vhost 目录路径，就说明 include 已配好。
	if res, err := m.runner.Run(ctx, eng.Bin, "-T"); err == nil {
		included := strings.Contains(res.Stdout, m.cfg.VhostDir)
		st.VhostIncluded = &included
		if !included && st.Message == "" {
			st.Message = fmt.Sprintf("主配置未 include %s/*.conf，站点配置不会生效", m.cfg.VhostDir)
		}
	}
	return st
}

// VhostPath 返回站点配置文件路径。name 已在服务层做过严格校验，此处只做拼接。
func (m *Manager) VhostPath(name string) string {
	return filepath.Join(m.cfg.VhostDir, name+".conf")
}

// historyDir 存放被替换掉的旧配置，便于人工排查与手工恢复。
func (m *Manager) historyDir(name string) string {
	return filepath.Join(m.cfg.VhostDir, "history", name)
}

// EnsureDirs 创建 vhost、历史、片段与日志目录。首次创建站点前调用。
func (m *Manager) EnsureDirs() error {
	dirs := []string{
		m.cfg.VhostDir,
		filepath.Join(m.cfg.VhostDir, "history"),
		filepath.Join(m.cfg.VhostDir, SnippetRewrite),
		m.cfg.LogDir,
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return errs.Wrapf(err, errs.CodeInternal, "创建目录 %s 失败", dir)
		}
	}
	return nil
}

// SnippetRewrite 是伪静态片段的子目录名。片段放在 vhost 子目录里，
// 主配置的 include <vhostDir>/*.conf 不会匹配子目录，只能由站点配置显式 include，
// 因此不会出现「片段被当成独立 server 块加载」的问题。
const SnippetRewrite = "rewrite"

// SnippetPath 返回片段文件绝对路径。name 已在服务层严格校验。
func (m *Manager) SnippetPath(kind, name string) string {
	return filepath.Join(m.cfg.VhostDir, kind, name+".conf")
}

// ReadSnippet 读取片段内容，第二个返回值表示文件是否存在。
func (m *Manager) ReadSnippet(kind, name string) (string, bool) {
	return readIfExists(m.SnippetPath(kind, name))
}

// WriteSnippet 原子写入片段；content 为空表示删除该片段，
// 这样站点配置里的 include 与文件存在性始终由同一处逻辑决定。
func (m *Manager) WriteSnippet(kind, name, content string) error {
	if strings.TrimSpace(content) == "" {
		return m.RemoveSnippet(kind, name)
	}
	dir := filepath.Join(m.cfg.VhostDir, kind)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return errs.Wrapf(err, errs.CodeInternal, "创建目录 %s 失败", dir)
	}
	return writeAtomic(m.SnippetPath(kind, name), content)
}

// RemoveSnippet 删除片段，文件不存在视为成功。
func (m *Manager) RemoveSnippet(kind, name string) error {
	if err := os.Remove(m.SnippetPath(kind, name)); err != nil && !os.IsNotExist(err) {
		return errs.Wrapf(err, errs.CodeInternal, "删除配置片段 %s 失败", m.SnippetPath(kind, name))
	}
	return nil
}

// Deploy 原子下发站点配置：备份旧文件 → 原子替换 → nginx -t → reload。
// 任一步失败都恢复旧文件（新站点则删除新文件）并重新校验，确保宿主机配置回到可用状态。
// 说明：nginx -t 校验的是主配置整体，无法只校验尚未 include 的临时文件，
// 因此顺序为「先就位再校验」，失败立即还原，窗口期内不 reload，运行中的服务不受影响。
func (m *Manager) Deploy(ctx context.Context, name, content string) error {
	eng, err := m.Detect(ctx)
	if err != nil {
		return err
	}
	if err := m.EnsureDirs(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	target := m.VhostPath(name)
	old, hadOld := readIfExists(target)

	if err := writeAtomic(target, content); err != nil {
		return err
	}
	if hadOld && old != content {
		m.archive(name, old)
	}

	if res, err := m.runner.Run(ctx, eng.Bin, "-t"); err != nil {
		m.restore(target, old, hadOld)
		// 还原后再校验一次，把「已回滚且宿主机恢复正常」这一事实明确写进错误详情。
		rollbackNote := "已回滚到上一版本"
		if _, err2 := m.runner.Run(ctx, eng.Bin, "-t"); err2 != nil {
			rollbackNote = "已回滚，但宿主机原有配置本身校验未通过，请检查主配置"
		}
		return errs.New(errs.CodeNginxTestFailed, "配置校验未通过，"+rollbackNote).
			WithDetail("%s", truncate(res.Combined(), 2048))
	}

	if err := m.reload(ctx, eng); err != nil {
		m.restore(target, old, hadOld)
		_, _ = m.runner.Run(ctx, eng.Bin, "-s", "reload")
		return err
	}
	return nil
}

// Remove 删除站点配置并重新加载。文件不存在时视为已删除。
func (m *Manager) Remove(ctx context.Context, name string) error {
	eng, err := m.Detect(ctx)
	if err != nil {
		// 未安装 Web 服务时，仍应允许删除数据库记录与残留文件。
		_ = os.Remove(m.VhostPath(name))
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	target := m.VhostPath(name)
	old, hadOld := readIfExists(target)
	if !hadOld {
		return nil
	}
	if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
		return errs.Wrapf(err, errs.CodeInternal, "删除站点配置 %s 失败", target)
	}
	m.archive(name, old)

	if err := m.reload(ctx, eng); err != nil {
		// reload 失败说明主配置存在其它问题，把站点配置放回去避免状态漂移。
		m.restore(target, old, true)
		_, _ = m.runner.Run(ctx, eng.Bin, "-s", "reload")
		return err
	}
	return nil
}

// Test 执行 nginx -t，返回校验输出。
func (m *Manager) Test(ctx context.Context) (string, error) {
	eng, err := m.Detect(ctx)
	if err != nil {
		return "", err
	}
	res, err := m.runner.Run(ctx, eng.Bin, "-t")
	if err != nil {
		return res.Combined(), errs.New(errs.CodeNginxTestFailed, "配置校验未通过").
			WithDetail("%s", truncate(res.Combined(), 2048))
	}
	return res.Combined(), nil
}

// Reload 对外暴露热加载，供「重载 Web 服务」按钮使用。
func (m *Manager) Reload(ctx context.Context) error {
	eng, err := m.Detect(ctx)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.reload(ctx, eng)
}

// reload 优先用配置的自定义命令（例如 systemctl reload nginx），否则用 nginx -s reload。
// 调用方需持有 m.mu。
func (m *Manager) reload(ctx context.Context, eng *Engine) error {
	if cmd := strings.Fields(m.cfg.ReloadCmd); len(cmd) > 0 {
		res, err := m.runner.Run(ctx, cmd[0], cmd[1:]...)
		if err != nil {
			return errs.Wrap(err, errs.CodeExecutorFailed, "Web 服务重载失败").
				WithDetail("%s", truncate(res.Combined(), 1024))
		}
		return nil
	}
	res, err := m.runner.Run(ctx, eng.Bin, "-s", "reload")
	if err != nil {
		return errs.Wrap(err, errs.CodeExecutorFailed, "Web 服务重载失败").
			WithDetail("%s", truncate(res.Combined(), 1024))
	}
	return nil
}

// archive 归档旧配置，失败不影响主流程：归档只是排查辅助。
func (m *Manager) archive(name, content string) {
	if content == "" {
		return
	}
	dir := m.historyDir(name)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return
	}
	file := filepath.Join(dir, time.Now().Format("20060102150405")+".conf")
	_ = os.WriteFile(file, []byte(content), 0o640)
}

// restore 把目标文件恢复到下发前的状态。
func (m *Manager) restore(target, old string, hadOld bool) {
	if !hadOld {
		_ = os.Remove(target)
		return
	}
	_ = writeAtomic(target, old)
}

func readIfExists(path string) (string, bool) {
	b, err := os.ReadFile(path) //nolint:gosec // 路径由面板拼接，name 已校验
	if err != nil {
		return "", false
	}
	return string(b), true
}

// writeAtomic 同目录写临时文件后 rename，保证 Nginx 永远读到完整配置。
func writeAtomic(path, content string) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".nova-vhost-*")
	if err != nil {
		return errs.Wrapf(err, errs.CodeInternal, "创建临时配置文件失败（目录 %s）", dir)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return errs.Wrap(err, errs.CodeInternal, "写入临时配置文件失败")
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return errs.Wrap(err, errs.CodeInternal, "刷新临时配置文件失败")
	}
	if err := tmp.Close(); err != nil {
		return errs.Wrap(err, errs.CodeInternal, "关闭临时配置文件失败")
	}
	// Nginx 以 root 读取配置，0644 允许非 root 的辅助工具查看，但不可写。
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return errs.Wrap(err, errs.CodeInternal, "设置配置文件权限失败")
	}
	if err := os.Rename(tmpName, path); err != nil {
		return errs.Wrapf(err, errs.CodeInternal, "替换配置文件 %s 失败", path)
	}
	return nil
}

func truncate(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
