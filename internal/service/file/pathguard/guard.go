// Package pathguard 是文件模块所有路径的唯一入口（docs/08 8.3）。
// 判定优先级：deny_paths（读写全禁）> deny_write_paths（可读不可写）>
// allow_roots（前缀放行）> 默认拒绝。解析后再做软链与硬链二次校验，
// 因此调用方拿到的返回值是可以直接交给 os 包使用的真实路径。
package pathguard

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/novapanel/novapanel/internal/pkg/errs"
)

// defaultMaxPathLen 与 Linux PATH_MAX 对齐。
const defaultMaxPathLen = 4096

// Mode 区分读写，写操作额外受 deny_write_paths 约束。
type Mode int

const (
	// ModeRead 为读取（含列目录、下载）。
	ModeRead Mode = iota
	// ModeWrite 为写入（含新建、改名、删除、改权限）。
	ModeWrite
)

// Config 为守卫配置，对应 conf/panel.yaml 的 file 段。
type Config struct {
	AllowRoots     []string
	DenyPaths      []string
	DenyWritePaths []string
	// FollowSymlink 为真时允许软链指向白名单外（仍受 deny 约束），默认关闭。
	FollowSymlink bool
	MaxPathLen    int
}

// Guard 执行路径判定。构造后只读，可被并发使用。
type Guard struct {
	allowRoots     []string
	denyPaths      []string
	denyWritePaths []string
	followSymlink  bool
	maxPathLen     int
}

// New 构造守卫。allowRoots 为空表示不放行任何路径（默认拒绝），
// 这是有意的保守取值：配置缺失时文件模块整体不可用，而不是暴露整个根文件系统。
func New(cfg Config) *Guard {
	g := &Guard{
		allowRoots:     normalizeList(cfg.AllowRoots),
		denyPaths:      normalizeList(cfg.DenyPaths),
		denyWritePaths: normalizeList(cfg.DenyWritePaths),
		followSymlink:  cfg.FollowSymlink,
		maxPathLen:     cfg.MaxPathLen,
	}
	if g.maxPathLen <= 0 {
		g.maxPathLen = defaultMaxPathLen
	}
	return g
}

// AllowRoots 返回规范化后的白名单根，供前端展示可访问范围。
func (g *Guard) AllowRoots() []string {
	out := make([]string, len(g.allowRoots))
	copy(out, g.allowRoots)
	return out
}

// Resolve 校验并返回真实路径。所有文件接口必须先经过这里。
func (g *Guard) Resolve(raw string, mode Mode) (string, error) {
	clean, err := g.precheck(raw)
	if err != nil {
		return "", err
	}

	// 1. 黑名单优先，命中即拒（即使同时在白名单内）
	if hit(clean, g.denyPaths) {
		return "", errs.ErrProtectedPath
	}
	if mode == ModeWrite && hit(clean, g.denyWritePaths) {
		return "", errs.Newf(errs.CodeProtectedPath, "%s 为只读路径", clean)
	}

	// 2. 白名单前缀校验（字符串层，先挡掉明显越界）
	if !underAnyRoot(clean, g.allowRoots) {
		return "", errs.ErrPathEscape
	}

	// 3. 解析软链后二次校验，防「白名单内软链指向白名单外」
	real, err := evalExisting(clean)
	if err != nil {
		return "", err
	}
	if real != clean {
		if !g.followSymlink && !underAnyRoot(real, g.allowRoots) {
			return "", errs.Newf(errs.CodePathEscape, "符号链接目标越出允许范围")
		}
		if hit(real, g.denyPaths) {
			return "", errs.ErrProtectedPath
		}
		if mode == ModeWrite && hit(real, g.denyWritePaths) {
			return "", errs.Newf(errs.CodeProtectedPath, "符号链接目标为只读路径")
		}
	}

	// 4. 硬链检查：多引用的普通文件若不在白名单内，禁止写入与下载
	if err := g.checkHardlink(real); err != nil {
		return "", err
	}
	return real, nil
}

// ResolveParent 用于「目标尚不存在」的场景（新建、上传、解压目标）：
// 校验父目录可写，返回父目录真实路径与拼接后的完整路径。
func (g *Guard) ResolveParent(raw string, mode Mode) (parent, full string, err error) {
	clean, err := g.precheck(raw)
	if err != nil {
		return "", "", err
	}
	dir, base := filepath.Split(clean)
	if base == "" || base == "." || base == ".." {
		return "", "", errs.Newf(errs.CodeInvalidParam, "文件名不合法")
	}
	parent, err = g.Resolve(filepath.Clean(dir), mode)
	if err != nil {
		return "", "", err
	}
	return parent, filepath.Join(parent, base), nil
}

// precheck 做与文件系统无关的字符串层校验。
func (g *Guard) precheck(raw string) (string, error) {
	if raw == "" {
		return "", errs.Newf(errs.CodeInvalidParam, "路径不能为空")
	}
	if strings.ContainsRune(raw, '\x00') {
		return "", errs.Newf(errs.CodeInvalidParam, "路径含非法字符")
	}
	if len(raw) > g.maxPathLen {
		return "", errs.Newf(errs.CodeInvalidParam, "路径超长")
	}
	if !filepath.IsAbs(raw) {
		return "", errs.Newf(errs.CodeInvalidParam, "必须为绝对路径")
	}
	return filepath.Clean(raw), nil
}

func (g *Guard) checkHardlink(real string) error {
	st, err := os.Lstat(real)
	if err != nil {
		return nil // 目标不存在交由上层按业务语义处理
	}
	if !st.Mode().IsRegular() {
		return nil
	}
	n, ok := hardLinkCount(st)
	if !ok || n <= 1 {
		return nil
	}
	if !underAnyRoot(real, g.allowRoots) {
		return errs.Newf(errs.CodePathEscape, "检测到可疑硬链接")
	}
	return nil
}

// normalizeList 清理、去重、解析软链并按长度降序排序，
// 保证前缀比对先命中更精确的规则。规则本身位于软链上时（如 macOS 的 /var → /private/var，
// 或把 /www 挂到数据盘的软链），不解析会导致 Resolve 的第 3 步误判越界。
func normalizeList(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, p := range in {
		p = strings.TrimSpace(p)
		if p == "" || !filepath.IsAbs(p) {
			continue
		}
		p = filepath.Clean(p)
		if real, err := filepath.EvalSymlinks(p); err == nil {
			p = real
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	return out
}

// underAnyRoot 用「相等或前缀 + 分隔符」比对，避免 /www2 被 /www 放行。
func underAnyRoot(p string, roots []string) bool {
	for _, r := range roots {
		if p == r || strings.HasPrefix(p, r+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

func hit(p string, list []string) bool {
	return underAnyRoot(p, list)
}

// evalExisting 解析已存在部分的软链；尾段允许尚不存在（新建场景），
// 但父目录必须存在，否则按越界处理交给上层返回明确错误。
func evalExisting(p string) (string, error) {
	real, err := filepath.EvalSymlinks(p)
	if err == nil {
		return real, nil
	}
	if !os.IsNotExist(err) {
		// ELOOP、ENAMETOOLONG、EACCES 等一律按逃逸处理，不泄漏底层错误细节
		return "", errs.Newf(errs.CodePathEscape, "路径无法安全解析")
	}

	dir, base := filepath.Split(p)
	realDir, err := filepath.EvalSymlinks(filepath.Clean(dir))
	if err != nil {
		if os.IsNotExist(err) {
			return "", errs.ErrFileNotFound
		}
		return "", errs.Newf(errs.CodePathEscape, "路径无法安全解析")
	}
	return filepath.Join(realDir, base), nil
}
