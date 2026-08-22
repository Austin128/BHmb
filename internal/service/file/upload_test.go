package file

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/novapanel/novapanel/internal/model"
	"github.com/novapanel/novapanel/internal/pkg/errs"
)

func TestUploadWritesFileAndRejectsOversize(t *testing.T) {
	svc, work := newTestService(t)
	svc.cfg.MaxUploadSize = 16

	entry, err := svc.Upload(UploadRequest{
		Dir:  work,
		Name: "a.txt",
		Size: 5,
		Src:  strings.NewReader("hello"),
	})
	if err != nil {
		t.Fatalf("上传：%v", err)
	}
	if entry.Path != filepath.Join(work, "a.txt") || entry.Size != 5 {
		t.Fatalf("上传结果不符：%+v", entry)
	}

	// 声明大小超限时必须在读流之前就拒绝
	_, err = svc.Upload(UploadRequest{Dir: work, Name: "big.bin", Size: 99, Src: strings.NewReader("x")})
	if code := errs.Code(err); code != errs.CodeFileTooLarge {
		t.Fatalf("期望 %d，得到 %v", errs.CodeFileTooLarge, err)
	}

	// 谎报 Content-Length：声明合法但实际超限，写入过程必须兜底拦下
	_, err = svc.Upload(UploadRequest{
		Dir:  work,
		Name: "liar.bin",
		Size: 1,
		Src:  strings.NewReader(strings.Repeat("x", 64)),
	})
	if code := errs.Code(err); code != errs.CodeFileTooLarge {
		t.Fatalf("期望 %d，得到 %v", errs.CodeFileTooLarge, err)
	}
	if _, err := os.Stat(filepath.Join(work, "liar.bin")); !os.IsNotExist(err) {
		t.Fatal("超限上传不得留下残留文件")
	}
}

func TestUploadConflictPolicies(t *testing.T) {
	svc, work := newTestService(t)
	mustWrite(t, filepath.Join(work, "a.txt"), "old")

	if _, err := svc.Upload(UploadRequest{Dir: work, Name: "a.txt", Size: 3, Src: strings.NewReader("new")}); errs.Code(err) != errs.CodeFileExists {
		t.Fatalf("默认策略应拒绝同名，得到 %v", err)
	}

	renamed, err := svc.Upload(UploadRequest{
		Dir: work, Name: "a.txt", Size: 3, Conflict: model.FileConflictRename, Src: strings.NewReader("new"),
	})
	if err != nil {
		t.Fatalf("自动改名：%v", err)
	}
	if filepath.Base(renamed.Path) == "a.txt" {
		t.Fatalf("改名策略必须换新名：%s", renamed.Path)
	}

	if _, err := svc.Upload(UploadRequest{
		Dir: work, Name: "a.txt", Size: 3, Conflict: model.FileConflictOverwrite, Src: strings.NewReader("new"),
	}); err != nil {
		t.Fatalf("覆盖：%v", err)
	}
	data, err := os.ReadFile(filepath.Join(work, "a.txt"))
	if err != nil || string(data) != "new" {
		t.Fatalf("覆盖后内容不符：%q %v", data, err)
	}
}

func TestUploadRejectsEscapeAndBadName(t *testing.T) {
	svc, work := newTestService(t)

	if _, err := svc.Upload(UploadRequest{Dir: "/etc", Name: "a.txt", Src: strings.NewReader("x")}); errs.Code(err) != errs.CodePathEscape {
		t.Fatalf("白名单外目录应拒绝，得到 %v", err)
	}
	// 文件名里的路径分隔符会被 filepath.Base 剥掉，穿越不成立但仍需落在目标目录内
	entry, err := svc.Upload(UploadRequest{Dir: work, Name: "../evil.txt", Size: 1, Src: strings.NewReader("x")})
	if err != nil {
		t.Fatalf("上传：%v", err)
	}
	if entry.Path != filepath.Join(work, "evil.txt") {
		t.Fatalf("上传必须落在目标目录内：%s", entry.Path)
	}

	if _, err := svc.Upload(UploadRequest{Dir: work, Name: "..", Src: strings.NewReader("x")}); errs.Code(err) != errs.CodeInvalidParam {
		t.Fatalf("非法文件名应拒绝，得到 %v", err)
	}
}

func TestOpenForDownload(t *testing.T) {
	svc, work := newTestService(t)
	mustWrite(t, filepath.Join(work, "a.txt"), "hello")

	f, fi, err := svc.OpenForDownload(filepath.Join(work, "a.txt"))
	if err != nil {
		t.Fatalf("打开下载：%v", err)
	}
	defer func() { _ = f.Close() }()
	if fi.Size() != 5 {
		t.Fatalf("大小不符：%d", fi.Size())
	}
	data, err := io.ReadAll(f)
	if err != nil || string(data) != "hello" {
		t.Fatalf("内容不符：%q %v", data, err)
	}

	if _, _, err := svc.OpenForDownload(work); errs.Code(err) != errs.CodeInvalidParam {
		t.Fatalf("目录应提示改用打包下载，得到 %v", err)
	}
	if _, _, err := svc.OpenForDownload(filepath.Join(work, "missing")); errs.Code(err) != errs.CodeFileNotFound {
		t.Fatalf("缺失文件应返回 %d，得到 %v", errs.CodeFileNotFound, err)
	}
	if _, _, err := svc.OpenForDownload("/etc/passwd"); errs.Code(err) != errs.CodePathEscape {
		t.Fatalf("白名单外应拒绝，得到 %v", err)
	}
}

func TestCopyTreeKeepsStructureAndSkipsSymlink(t *testing.T) {
	svc, work := newTestService(t)
	src := filepath.Join(work, "src")
	mustWrite(t, filepath.Join(src, "deep", "a.txt"), "a")
	mustWrite(t, filepath.Join(src, "b.txt"), "b")
	if err := os.Symlink(filepath.Join(src, "b.txt"), filepath.Join(src, "link")); err != nil {
		t.Fatalf("建软链：%v", err)
	}
	if err := os.Mkdir(filepath.Join(work, "dst"), 0o755); err != nil {
		t.Fatalf("建目标目录：%v", err)
	}

	res, err := svc.Copy(model.FileTransferRequest{Paths: []string{src}, Dest: filepath.Join(work, "dst")})
	if err != nil {
		t.Fatalf("复制目录：%v", err)
	}
	if res.Succeeded != 1 || res.Failed != 0 {
		t.Fatalf("复制结果不符：%+v", res)
	}

	copied := filepath.Join(work, "dst", "src")
	data, err := os.ReadFile(filepath.Join(copied, "deep", "a.txt"))
	if err != nil || string(data) != "a" {
		t.Fatalf("深层文件未复制：%q %v", data, err)
	}
	// 软链原样重建为链接，不跟随目标复制内容；后续访问仍要过白名单校验
	link := filepath.Join(copied, "link")
	li, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("软链未复制：%v", err)
	}
	if li.Mode()&os.ModeSymlink == 0 {
		t.Fatal("软链必须仍是链接，不能被展开为普通文件")
	}
	target, err := os.Readlink(link)
	if err != nil || target != filepath.Join(src, "b.txt") {
		t.Fatalf("软链目标不符：%q %v", target, err)
	}
}

func TestCopyTreeRejectsSpecialFile(t *testing.T) {
	svc, work := newTestService(t)
	if err := os.Mkdir(filepath.Join(work, "dst"), 0o755); err != nil {
		t.Fatalf("建目标目录：%v", err)
	}
	// FIFO 属于特殊文件，复制它没有可预期的语义，必须明确拒绝
	fifo := filepath.Join(work, "pipe")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skipf("当前环境不支持 mkfifo：%v", err)
	}

	res, err := svc.Copy(model.FileTransferRequest{Paths: []string{fifo}, Dest: filepath.Join(work, "dst")})
	if err != nil {
		t.Fatalf("复制：%v", err)
	}
	if res.Failed != 1 || res.Results[0].Code != errs.CodeUnsupported {
		t.Fatalf("期望逐项报 %d，得到 %+v", errs.CodeUnsupported, res.Results)
	}
}

func TestDetectArchiveByMagicWhenExtMissing(t *testing.T) {
	svc, work := newTestService(t)
	mustWrite(t, filepath.Join(work, "a.txt"), "a")

	// 无扩展名的 tar.gz：靠文件头识别
	raw := filepath.Join(work, "blob")
	f, err := os.Create(raw)
	if err != nil {
		t.Fatalf("建文件：%v", err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	body := []byte("hi")
	if err := tw.WriteHeader(&tar.Header{Name: "in.txt", Mode: 0o644, Size: int64(len(body))}); err != nil {
		t.Fatalf("写 tar 头：%v", err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatalf("写 tar 体：%v", err)
	}
	for _, closer := range []io.Closer{tw, gz, f} {
		if err := closer.Close(); err != nil {
			t.Fatalf("关闭：%v", err)
		}
	}

	if got := detectArchive(raw); got != model.ArchiveTarGz {
		t.Fatalf("期望按文件头识别为 %s，得到 %q", model.ArchiveTarGz, got)
	}

	dest := filepath.Join(work, "out")
	if _, err := svc.Decompress(model.FileDecompressRequest{Path: raw, Dest: dest}); err != nil {
		t.Fatalf("解压：%v", err)
	}
	data, err := os.ReadFile(filepath.Join(dest, "in.txt"))
	if err != nil || string(data) != "hi" {
		t.Fatalf("解压内容不符：%q %v", data, err)
	}

	// 既无后缀也无可识别文件头，必须报格式不支持
	if _, err := svc.Decompress(model.FileDecompressRequest{
		Path: filepath.Join(work, "a.txt"), Dest: filepath.Join(work, "out2"),
	}); errs.Code(err) != errs.CodeArchiveUnsupported {
		t.Fatalf("期望 %d，得到 %v", errs.CodeArchiveUnsupported, err)
	}
}

func TestAllowRootsExposedToCaller(t *testing.T) {
	svc, work := newTestService(t)
	roots := svc.AllowRoots()
	if len(roots) != 1 || roots[0] != work {
		t.Fatalf("白名单根不符：%v", roots)
	}
}
