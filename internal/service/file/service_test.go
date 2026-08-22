package file

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/novapanel/novapanel/internal/model"
	"github.com/novapanel/novapanel/internal/pkg/errs"
	"github.com/novapanel/novapanel/internal/service/file/pathguard"
)

// newTestService 把白名单限制在临时目录内，保证测试不会碰到宿主真实路径。
func newTestService(t *testing.T) (*Service, string) {
	t.Helper()
	root := t.TempDir()
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}
	work := filepath.Join(root, "www")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatalf("准备工作目录：%v", err)
	}

	guard := pathguard.New(pathguard.Config{
		AllowRoots: []string{work},
		DenyPaths:  []string{filepath.Join(work, "secret")},
	})
	svc, err := New(guard, Config{
		MaxEditSize:      1 << 20,
		MaxSearchResults: 50,
		// 分片会话隔离在各自的临时目录，避免用例之间互相看到残留会话
		UploadTempDir: filepath.Join(root, "upload-tmp"),
	})
	if err != nil {
		t.Fatalf("构造服务：%v", err)
	}
	return svc, work
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("建目录：%v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写文件：%v", err)
	}
}

func TestListSortAndHidden(t *testing.T) {
	svc, work := newTestService(t)
	mustWrite(t, filepath.Join(work, "b.txt"), "bb")
	mustWrite(t, filepath.Join(work, "a.txt"), "a")
	mustWrite(t, filepath.Join(work, ".hidden"), "h")
	if err := os.Mkdir(filepath.Join(work, "sub"), 0o755); err != nil {
		t.Fatalf("建子目录：%v", err)
	}

	res, err := svc.List(model.FileListRequest{Path: work})
	if err != nil {
		t.Fatalf("列目录：%v", err)
	}
	if res.Paged {
		t.Fatalf("小目录应全量返回")
	}
	// 目录优先，其余按名称升序；隐藏文件默认不返回
	got := names(res.Items)
	want := []string{"sub", "a.txt", "b.txt"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("期望 %v，实际 %v", want, got)
	}

	res, err = svc.List(model.FileListRequest{Path: work, ShowHidden: true})
	if err != nil {
		t.Fatalf("列目录（含隐藏）：%v", err)
	}
	if len(res.Items) != 4 {
		t.Fatalf("含隐藏文件应为 4 项，实际 %d", len(res.Items))
	}

	res, err = svc.List(model.FileListRequest{Path: work, Sort: model.FileSortSize, Order: model.FileOrderDesc})
	if err != nil {
		t.Fatalf("按大小排序：%v", err)
	}
	if res.Items[0].Name != "sub" {
		t.Fatalf("目录优先不受排序方向影响，实际首项 %s", res.Items[0].Name)
	}
	if res.Items[1].Name != "b.txt" {
		t.Fatalf("降序时较大文件应在前，实际 %s", res.Items[1].Name)
	}
}

func TestListPaginationByCursor(t *testing.T) {
	svc, work := newTestService(t)
	for i := 0; i < 12; i++ {
		mustWrite(t, filepath.Join(work, "f"+strconv.Itoa(i)+".txt"), "x")
	}

	first, err := svc.List(model.FileListRequest{Path: work, PageSize: 5, Cursor: ""})
	if err != nil {
		t.Fatalf("首页：%v", err)
	}
	// 未超过全量阈值且无游标时一次返回全部
	if first.Paged || len(first.Items) != 12 {
		t.Fatalf("小目录应全量返回，实际 paged=%v items=%d", first.Paged, len(first.Items))
	}

	// 显式带游标即进入分页模式，用于超大目录续读
	cursor := encodeCursor("f0.txt")
	page, err := svc.List(model.FileListRequest{Path: work, PageSize: 5, Cursor: cursor})
	if err != nil {
		t.Fatalf("游标页：%v", err)
	}
	if !page.Paged || len(page.Items) != 5 {
		t.Fatalf("游标模式应返回 5 项，实际 paged=%v items=%d", page.Paged, len(page.Items))
	}
	if page.Items[0].Name == "f0.txt" {
		t.Fatalf("游标应从上一页末项之后开始")
	}
	if page.NextCursor == "" {
		t.Fatalf("仍有剩余条目时应返回 nextCursor")
	}
}

func TestListRejectsDenyAndOutOfRoot(t *testing.T) {
	svc, work := newTestService(t)
	if err := os.MkdirAll(filepath.Join(work, "secret"), 0o755); err != nil {
		t.Fatalf("建目录：%v", err)
	}

	if _, err := svc.List(model.FileListRequest{Path: filepath.Join(work, "secret")}); errs.Code(err) != errs.CodeProtectedPath {
		t.Fatalf("黑名单目录应返回 %d，实际 %v", errs.CodeProtectedPath, err)
	}
	if _, err := svc.List(model.FileListRequest{Path: filepath.Dir(work)}); errs.Code(err) != errs.CodePathEscape {
		t.Fatalf("白名单外目录应返回 %d，实际 %v", errs.CodePathEscape, err)
	}
}

func TestContentETagOptimisticLock(t *testing.T) {
	svc, work := newTestService(t)
	path := filepath.Join(work, "app.conf")
	mustWrite(t, path, "port=80\n")

	got, err := svc.ReadContent(path)
	if err != nil {
		t.Fatalf("读取内容：%v", err)
	}
	if got.Content != "port=80\n" {
		t.Fatalf("内容不符：%q", got.Content)
	}

	// ETag 匹配时保存成功
	saved, err := svc.SaveContent(model.FileSaveRequest{Path: path, Content: "port=8080\n", ETag: got.ETag, CreateBackup: true})
	if err != nil {
		t.Fatalf("保存：%v", err)
	}
	if saved.ETag == got.ETag {
		t.Fatalf("内容变化后 ETag 应变化")
	}

	// 用旧 ETag 再保存必须冲突
	if _, err := svc.SaveContent(model.FileSaveRequest{Path: path, Content: "x", ETag: got.ETag}); errs.Code(err) != errs.CodeFileChanged {
		t.Fatalf("陈旧 ETag 应返回 %d，实际 %v", errs.CodeFileChanged, err)
	}
	// 文件已存在却不带 ETag，同样按冲突处理，避免误覆盖
	if _, err := svc.SaveContent(model.FileSaveRequest{Path: path, Content: "x"}); errs.Code(err) != errs.CodeFileChanged {
		t.Fatalf("缺失 ETag 应返回 %d，实际 %v", errs.CodeFileChanged, err)
	}

	// 备份文件已生成
	entries, err := os.ReadDir(work)
	if err != nil {
		t.Fatalf("读目录：%v", err)
	}
	var found bool
	for _, de := range entries {
		if strings.HasPrefix(de.Name(), ".app.conf.bak.") {
			found = true
		}
	}
	if !found {
		t.Fatalf("createBackup 应生成备份文件")
	}
}

func TestContentRejectsBinaryAndOversize(t *testing.T) {
	svc, work := newTestService(t)

	bin := filepath.Join(work, "app.bin")
	if err := os.WriteFile(bin, []byte{0x7F, 'E', 'L', 'F', 0x00, 0x01}, 0o644); err != nil {
		t.Fatalf("写二进制：%v", err)
	}
	if _, err := svc.ReadContent(bin); errs.Code(err) != errs.CodeNotUTF8 {
		t.Fatalf("二进制应返回 %d，实际 %v", errs.CodeNotUTF8, err)
	}

	svc.cfg.MaxEditSize = 4
	big := filepath.Join(work, "big.txt")
	mustWrite(t, big, "0123456789")
	if _, err := svc.ReadContent(big); errs.Code(err) != errs.CodeFileTooLarge {
		t.Fatalf("超限应返回 %d，实际 %v", errs.CodeFileTooLarge, err)
	}
}

func TestMutateLifecycle(t *testing.T) {
	svc, work := newTestService(t)

	dir, err := svc.CreateDir(model.FileCreateRequest{Path: filepath.Join(work, "app"), Mode: "0750"})
	if err != nil {
		t.Fatalf("建目录：%v", err)
	}
	if dir.ModeOctal != "0750" {
		t.Fatalf("权限应为 0750，实际 %s", dir.ModeOctal)
	}
	if _, err := svc.CreateDir(model.FileCreateRequest{Path: filepath.Join(work, "app")}); errs.Code(err) != errs.CodeFileExists {
		t.Fatalf("重复建目录应返回 %d，实际 %v", errs.CodeFileExists, err)
	}

	if _, err := svc.CreateFile(model.FileCreateRequest{Path: filepath.Join(work, "app/index.php")}); err != nil {
		t.Fatalf("建文件：%v", err)
	}
	renamed, err := svc.Rename(model.FileRenameRequest{Path: filepath.Join(work, "app/index.php"), NewName: "main.php"})
	if err != nil {
		t.Fatalf("重命名：%v", err)
	}
	if renamed.Name != "main.php" {
		t.Fatalf("重命名结果不符：%s", renamed.Name)
	}
	// 借 newName 跨目录属于路径穿越，必须拒绝
	if _, err := svc.Rename(model.FileRenameRequest{Path: renamed.Path, NewName: "../evil.php"}); errs.Code(err) != errs.CodeInvalidParam {
		t.Fatalf("含斜杠的新名应返回 %d，实际 %v", errs.CodeInvalidParam, err)
	}

	if err := os.Mkdir(filepath.Join(work, "backup"), 0o755); err != nil {
		t.Fatalf("建目标目录：%v", err)
	}
	cp, err := svc.Copy(model.FileTransferRequest{Paths: []string{renamed.Path}, Dest: filepath.Join(work, "backup")})
	if err != nil || cp.Failed != 0 {
		t.Fatalf("复制失败：err=%v result=%+v", err, cp)
	}
	// 同名再复制，冲突策略 rename 应自动加序号
	cp2, err := svc.Copy(model.FileTransferRequest{
		Paths: []string{renamed.Path}, Dest: filepath.Join(work, "backup"), Conflict: model.FileConflictRename,
	})
	if err != nil || cp2.Failed != 0 {
		t.Fatalf("自动改名复制失败：err=%v result=%+v", err, cp2)
	}
	if filepath.Base(cp2.Results[0].Dest) != "main(1).php" {
		t.Fatalf("自动改名结果不符：%s", cp2.Results[0].Dest)
	}
	// 默认策略必须拒绝覆盖
	cp3, err := svc.Copy(model.FileTransferRequest{Paths: []string{renamed.Path}, Dest: filepath.Join(work, "backup")})
	if err != nil {
		t.Fatalf("复制调用失败：%v", err)
	}
	if cp3.Failed != 1 || cp3.Results[0].Code != errs.CodeFileExists {
		t.Fatalf("默认应冲突拒绝，实际 %+v", cp3.Results)
	}

	mv, err := svc.Move(model.FileTransferRequest{Paths: []string{renamed.Path}, Dest: filepath.Join(work, "backup"), Conflict: model.FileConflictOverwrite})
	if err != nil || mv.Failed != 0 {
		t.Fatalf("移动失败：err=%v result=%+v", err, mv)
	}
	if _, err := os.Lstat(renamed.Path); !os.IsNotExist(err) {
		t.Fatalf("移动后源文件应不存在")
	}

	del, err := svc.Delete(model.FileDeleteRequest{Paths: []string{filepath.Join(work, "backup")}})
	if err != nil || del.Failed != 0 {
		t.Fatalf("删除失败：err=%v result=%+v", err, del)
	}
}

func TestMutateProtectsRootAndSelfNesting(t *testing.T) {
	svc, work := newTestService(t)

	if _, err := svc.Delete(model.FileDeleteRequest{Paths: []string{work}}); err != nil {
		t.Fatalf("删除调用失败：%v", err)
	} else if _, statErr := os.Stat(work); statErr != nil {
		t.Fatalf("白名单根不应被删除：%v", statErr)
	}

	src := filepath.Join(work, "site")
	if err := os.MkdirAll(filepath.Join(src, "inner"), 0o755); err != nil {
		t.Fatalf("建目录：%v", err)
	}
	res, err := svc.Move(model.FileTransferRequest{Paths: []string{src}, Dest: filepath.Join(src, "inner")})
	if err != nil {
		t.Fatalf("移动调用失败：%v", err)
	}
	if res.Failed != 1 || res.Results[0].Code != errs.CodeInvalidParam {
		t.Fatalf("移动到自身子目录应被拒绝，实际 %+v", res.Results)
	}
}

func TestPermission(t *testing.T) {
	svc, work := newTestService(t)
	path := filepath.Join(work, "run.sh")
	mustWrite(t, path, "#!/bin/sh\n")

	got, err := svc.Permission(model.FilePermissionRequest{Path: path, Mode: "0700"})
	if err != nil {
		t.Fatalf("改权限：%v", err)
	}
	if got.ModeOctal != "0700" {
		t.Fatalf("权限应为 0700，实际 %s", got.ModeOctal)
	}
	if _, err := svc.Permission(model.FilePermissionRequest{Path: path}); errs.Code(err) != errs.CodeInvalidParam {
		t.Fatalf("三项全空应返回 %d，实际 %v", errs.CodeInvalidParam, err)
	}
	if _, err := svc.Permission(model.FilePermissionRequest{Path: path, Mode: "999"}); errs.Code(err) != errs.CodeInvalidParam {
		t.Fatalf("非法权限串应返回 %d，实际 %v", errs.CodeInvalidParam, err)
	}
}

func TestCompressAndDecompress(t *testing.T) {
	svc, work := newTestService(t)
	mustWrite(t, filepath.Join(work, "site/index.html"), "<h1>hi</h1>")
	mustWrite(t, filepath.Join(work, "site/conf/app.ini"), "k=v")

	for _, format := range []string{model.ArchiveZip, model.ArchiveTarGz} {
		name := "site." + strings.ReplaceAll(format, ".", "_")
		dest := filepath.Join(work, name)
		if format == model.ArchiveZip {
			dest += ".zip"
		} else {
			dest += ".tar.gz"
		}
		if _, err := svc.Compress(model.FileCompressRequest{
			Paths: []string{filepath.Join(work, "site")}, Dest: dest, Format: format,
		}); err != nil {
			t.Fatalf("压缩 %s：%v", format, err)
		}

		out := filepath.Join(work, "out-"+format)
		if _, err := svc.Decompress(model.FileDecompressRequest{Path: dest, Dest: out}); err != nil {
			t.Fatalf("解压 %s：%v", format, err)
		}
		data, err := os.ReadFile(filepath.Join(out, "site/index.html"))
		if err != nil {
			t.Fatalf("解压结果缺失：%v", err)
		}
		if string(data) != "<h1>hi</h1>" {
			t.Fatalf("解压内容不符：%q", data)
		}
	}

	if _, err := svc.Compress(model.FileCompressRequest{
		Paths: []string{filepath.Join(work, "site")}, Dest: filepath.Join(work, "x.rar"),
	}); errs.Code(err) != errs.CodeArchiveUnsupported {
		t.Fatalf("未知格式应返回 %d，实际 %v", errs.CodeArchiveUnsupported, err)
	}
}

// TestDecompressRejectsZipSlip 确认压缩包内的 ../ 条目不会写到目标目录之外。
func TestDecompressRejectsZipSlip(t *testing.T) {
	svc, work := newTestService(t)

	archive := filepath.Join(work, "evil.zip")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatalf("建压缩包：%v", err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("../escaped.txt")
	if err != nil {
		t.Fatalf("写条目：%v", err)
	}
	if _, err := w.Write([]byte("pwned")); err != nil {
		t.Fatalf("写内容：%v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("关闭 zip：%v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("关闭文件：%v", err)
	}

	if _, err := svc.Decompress(model.FileDecompressRequest{Path: archive, Dest: filepath.Join(work, "out")}); errs.Code(err) != errs.CodePathEscape {
		t.Fatalf("zip slip 应返回 %d，实际 %v", errs.CodePathEscape, err)
	}
	if _, err := os.Stat(filepath.Join(work, "escaped.txt")); !os.IsNotExist(err) {
		t.Fatalf("越界文件不应被写出")
	}
}

func TestSearchAndDu(t *testing.T) {
	svc, work := newTestService(t)
	mustWrite(t, filepath.Join(work, "site/index.html"), "12345")
	mustWrite(t, filepath.Join(work, "site/conf/nginx.conf"), "1234567890")
	mustWrite(t, filepath.Join(work, "site/deep/a/b/c/target.conf"), "x")

	res, err := svc.Search(model.FileSearchRequest{Path: work, Keyword: ".conf"})
	if err != nil {
		t.Fatalf("搜索：%v", err)
	}
	if len(res.Items) < 2 {
		t.Fatalf("应至少命中 2 项，实际 %d", len(res.Items))
	}

	limited, err := svc.Search(model.FileSearchRequest{Path: work, Keyword: ".conf", Limit: 1})
	if err != nil {
		t.Fatalf("搜索（限量）：%v", err)
	}
	if len(limited.Items) != 1 || !limited.Truncated {
		t.Fatalf("应截断为 1 项，实际 %d truncated=%v", len(limited.Items), limited.Truncated)
	}

	shallow, err := svc.Search(model.FileSearchRequest{Path: work, Keyword: "target", MaxDepth: 2})
	if err != nil {
		t.Fatalf("搜索（限深）：%v", err)
	}
	if len(shallow.Items) != 0 {
		t.Fatalf("深度限制应过滤掉深层命中，实际 %d", len(shallow.Items))
	}

	du, err := svc.Du(filepath.Join(work, "site"))
	if err != nil {
		t.Fatalf("统计：%v", err)
	}
	if du.Files != 3 || du.Size != 16 {
		t.Fatalf("统计结果不符：files=%d size=%d", du.Files, du.Size)
	}
}

func TestStatMimeDetection(t *testing.T) {
	svc, work := newTestService(t)
	// 无扩展名的文本文件应被识别为可编辑文本
	path := filepath.Join(work, "Dockerfile")
	mustWrite(t, path, "FROM debian:12\n")

	got, err := svc.Stat(path)
	if err != nil {
		t.Fatalf("stat：%v", err)
	}
	if !isTextMime(got.Mime) {
		t.Fatalf("应识别为文本，实际 %s", got.Mime)
	}
	if got.Icon != "file-unknown" {
		t.Fatalf("无扩展名应回落到通用图标，实际 %s", got.Icon)
	}
}

func TestNewRequiresGuard(t *testing.T) {
	if _, err := New(nil, Config{}); err == nil {
		t.Fatalf("缺少守卫时应构造失败")
	}
}

func names(items []model.FileEntry) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Name)
	}
	return out
}
