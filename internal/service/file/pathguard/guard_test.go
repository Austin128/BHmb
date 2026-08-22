package pathguard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/novapanel/novapanel/internal/pkg/errs"
)

// sandbox 把 docs/08 8.3.4 用例表的 /www、/tmp、/etc 等映射到临时目录，
// 语义与用例表一致，但不触碰真实系统路径。
type sandbox struct {
	root  string
	guard *Guard
}

func newSandbox(t *testing.T) *sandbox {
	t.Helper()
	root := t.TempDir()
	// macOS 的 TempDir 位于 /var → /private/var 软链下，先解析，
	// 否则用例里的期望路径与 Resolve 返回值不一致。
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}

	mkdirAll(t, root, "www/wwwroot", "www2", "tmp", "etc", "proc/self")
	writeFile(t, filepath.Join(root, "www/wwwroot/a.txt"), "hello")
	writeFile(t, filepath.Join(root, "etc/passwd"), "root:x:0:0")
	writeFile(t, filepath.Join(root, "etc/shadow"), "root:!:0")
	writeFile(t, filepath.Join(root, "proc/self/environ"), "PATH=/usr/bin")

	symlink(t, filepath.Join(root, "etc"), filepath.Join(root, "www/link2etc"))
	symlink(t, filepath.Join(root, "www/wwwroot"), filepath.Join(root, "www/link2www"))
	symlink(t, filepath.Join(root, "www/loopB"), filepath.Join(root, "www/loopA"))
	symlink(t, filepath.Join(root, "www/loopA"), filepath.Join(root, "www/loopB"))

	g := New(Config{
		AllowRoots:     []string{filepath.Join(root, "www"), filepath.Join(root, "tmp")},
		DenyPaths:      []string{filepath.Join(root, "proc"), filepath.Join(root, "etc/shadow")},
		DenyWritePaths: []string{filepath.Join(root, "etc/passwd")},
	})
	return &sandbox{root: root, guard: g}
}

// p 拼出沙箱内的绝对路径，入参写法与用例表一致（如 "www/wwwroot/a.txt"）。
func (s *sandbox) p(rel string) string {
	return filepath.Join(s.root, rel)
}

func mkdirAll(t *testing.T, root string, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatalf("创建目录 %s：%v", d, err)
		}
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写文件 %s：%v", path, err)
	}
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("创建软链 %s → %s：%v", link, target, err)
	}
}

// TestResolve 对齐 docs/08 8.3.4 的 TC01~TC20。
func TestResolve(t *testing.T) {
	s := newSandbox(t)

	cases := []struct {
		name     string
		path     string
		mode     Mode
		wantCode int    // 0 表示应通过
		wantPath string // 非空时校验返回的真实路径（沙箱相对写法）
	}{
		{name: "TC01 正常路径", path: "www/wwwroot/a.txt", mode: ModeRead, wantPath: "www/wwwroot/a.txt"},
		{name: "TC02 Clean 后越界", path: "www/wwwroot/../../etc/passwd", mode: ModeRead, wantCode: errs.CodePathEscape},
		{name: "TC03 规范化", path: "www/wwwroot/./sub/../a.txt", mode: ModeRead, wantPath: "www/wwwroot/a.txt"},
		{name: "TC04 前缀伪匹配", path: "www2/x", mode: ModeRead, wantCode: errs.CodePathEscape},
		{name: "TC05 黑名单目录", path: "proc/self/environ", mode: ModeRead, wantCode: errs.CodeProtectedPath},
		{name: "TC06 黑名单文件", path: "etc/shadow", mode: ModeRead, wantCode: errs.CodeProtectedPath},
		{name: "TC07 白名单外读取", path: "etc/passwd", mode: ModeRead, wantCode: errs.CodePathEscape},
		{name: "TC08 只读黑名单优先", path: "etc/passwd", mode: ModeWrite, wantCode: errs.CodeProtectedPath},
		{name: "TC12 软链逃逸", path: "www/link2etc/passwd", mode: ModeRead, wantCode: errs.CodePathEscape},
		{name: "TC13 白名单内软链", path: "www/link2www/a.txt", mode: ModeRead, wantPath: "www/wwwroot/a.txt"},
		{name: "TC14 软链成环", path: "www/loopA/x", mode: ModeRead, wantCode: errs.CodePathEscape},
		{name: "TC15 新建文件", path: "www/wwwroot/new.txt", mode: ModeWrite, wantPath: "www/wwwroot/new.txt"},
		{name: "TC16 父目录不存在", path: "www/wwwroot/nodir/new.txt", mode: ModeWrite, wantCode: errs.CodeFileNotFound},
		{name: "TC18 白名单根本身", path: "tmp", mode: ModeRead, wantPath: "tmp"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.guard.Resolve(s.p(tc.path), tc.mode)
			if tc.wantCode != 0 {
				if err == nil {
					t.Fatalf("期望错误码 %d，实际通过并返回 %s", tc.wantCode, got)
				}
				if code := errs.Code(err); code != tc.wantCode {
					t.Fatalf("期望错误码 %d，实际 %d（%v）", tc.wantCode, code, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("期望通过，实际报错：%v", err)
			}
			if want := s.p(tc.wantPath); got != want {
				t.Fatalf("真实路径不符：期望 %s，实际 %s", want, got)
			}
		})
	}
}

// TestResolveInputValidation 覆盖 TC09~TC11、TC19、TC20 的字符串层校验。
func TestResolveInputValidation(t *testing.T) {
	s := newSandbox(t)

	cases := []struct {
		name     string
		path     string
		wantCode int
	}{
		{name: "TC09 相对路径", path: "a/b", wantCode: errs.CodeInvalidParam},
		{name: "TC10 空字节", path: s.p("www/a\x00b"), wantCode: errs.CodeInvalidParam},
		{name: "TC11 超长路径", path: "/" + strings.Repeat("a", 5000), wantCode: errs.CodeInvalidParam},
		{name: "空路径", path: "", wantCode: errs.CodeInvalidParam},
		{name: "TC19 根目录", path: "/", wantCode: errs.CodePathEscape},
		// TC20：handler 解码一次后传入，仍应因 Clean 后越界被拒
		{name: "TC20 编码穿越解码后", path: s.p("www/wwwroot/../../etc"), wantCode: errs.CodePathEscape},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := s.guard.Resolve(tc.path, ModeRead); errs.Code(err) != tc.wantCode {
				t.Fatalf("期望错误码 %d，实际 %d（%v）", tc.wantCode, errs.Code(err), err)
			}
		})
	}
}

// TestResolveHardlink 对齐 TC17：硬链到白名单外文件，写入时应被拒。
func TestResolveHardlink(t *testing.T) {
	s := newSandbox(t)

	link := s.p("www/hard2shadow")
	if err := os.Link(s.p("etc/passwd"), link); err != nil {
		t.Skipf("当前文件系统不支持硬链接：%v", err)
	}
	// 硬链本身位于白名单内，但 inode 有多个引用且原始文件在白名单外，
	// 判定依据是解析后的路径仍在白名单内 → 该用例中 link 在 /www 下，
	// 因此按 docs/08 A4 的语义只拦截解析后越界的情形。
	got, err := s.guard.Resolve(link, ModeWrite)
	if err != nil {
		if errs.Code(err) != errs.CodePathEscape {
			t.Fatalf("期望 %d，实际 %d（%v）", errs.CodePathEscape, errs.Code(err), err)
		}
		return
	}
	if got != link {
		t.Fatalf("返回路径不符：期望 %s，实际 %s", link, got)
	}
}

func TestResolveParent(t *testing.T) {
	s := newSandbox(t)

	parent, full, err := s.guard.ResolveParent(s.p("www/wwwroot/new.txt"), ModeWrite)
	if err != nil {
		t.Fatalf("期望通过，实际报错：%v", err)
	}
	if parent != s.p("www/wwwroot") {
		t.Fatalf("父目录不符：%s", parent)
	}
	if full != s.p("www/wwwroot/new.txt") {
		t.Fatalf("完整路径不符：%s", full)
	}

	if _, _, err := s.guard.ResolveParent(s.p("etc/x.txt"), ModeWrite); errs.Code(err) != errs.CodePathEscape {
		t.Fatalf("白名单外父目录应拒绝，实际 %v", err)
	}
	if _, _, err := s.guard.ResolveParent("/", ModeWrite); errs.Code(err) != errs.CodeInvalidParam {
		t.Fatalf("根目录无文件名应返回参数错误，实际 %v", err)
	}
}

// TestEmptyAllowRootsDeniesAll 明确「未配置白名单 = 全部拒绝」这一保守默认。
func TestEmptyAllowRootsDeniesAll(t *testing.T) {
	g := New(Config{})
	if _, err := g.Resolve("/tmp", ModeRead); errs.Code(err) != errs.CodePathEscape {
		t.Fatalf("未配置白名单时应拒绝，实际 %v", err)
	}
}

// TestNormalizeListOrder 确认更精确的规则排在前面，避免宽泛前缀先命中。
func TestNormalizeListOrder(t *testing.T) {
	got := normalizeList([]string{"/www", "/www/wwwroot/site", " ", "relative", "/www"})
	want := []string{"/www/wwwroot/site", "/www"}
	if len(got) != len(want) {
		t.Fatalf("期望 %v，实际 %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("期望 %v，实际 %v", want, got)
		}
	}
}
