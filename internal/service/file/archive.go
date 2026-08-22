package file

import (
	"archive/tar"
	"archive/zip"
	"compress/bzip2"
	"compress/gzip"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/novapanel/novapanel/internal/model"
	"github.com/novapanel/novapanel/internal/pkg/errs"
	"github.com/novapanel/novapanel/internal/service/file/pathguard"
)

// maxDecompressRatio 限制单个条目的解压膨胀比，抵御 zip bomb。
// 超过该比例时中止并按压缩包损坏处理。
const maxDecompressRatio = 200

// Compress 把多个路径打包到 dest。format 为空时按 dest 后缀推断。
func (s *Service) Compress(req model.FileCompressRequest) (*model.FileEntry, error) {
	_, dest, err := s.guard.ResolveParent(req.Dest, pathguard.ModeWrite)
	if err != nil {
		return nil, err
	}
	format, err := archiveFormat(req.Format, dest)
	if err != nil {
		return nil, err
	}
	if _, err := os.Lstat(dest); err == nil {
		return nil, errs.ErrFileExists
	}

	// 先校验全部源路径，避免打包到一半才发现越界
	srcs := make([]string, 0, len(req.Paths))
	for _, raw := range req.Paths {
		full, err := s.guard.Resolve(raw, pathguard.ModeRead)
		if err != nil {
			return nil, err
		}
		srcs = append(srcs, full)
	}

	f, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, wrapFSError(err)
	}
	writeErr := func() error {
		defer func() { _ = f.Close() }()
		if format == model.ArchiveZip {
			return writeZip(f, srcs)
		}
		return writeTar(f, srcs, format == model.ArchiveTarGz)
	}()
	if writeErr != nil {
		_ = os.Remove(dest) // 失败不留半截压缩包
		return nil, writeErr
	}
	return s.entryOf(dest)
}

// Decompress 解压到 dest 目录。条目路径经过 pathguard 校验，
// 因此 ../ 穿越（zip slip）会被拒绝。
func (s *Service) Decompress(req model.FileDecompressRequest) (*model.FileEntry, error) {
	src, err := s.guard.Resolve(req.Path, pathguard.ModeRead)
	if err != nil {
		return nil, err
	}
	dest, err := s.guard.Resolve(req.Dest, pathguard.ModeWrite)
	if err != nil {
		return nil, err
	}
	// 守卫允许尾段尚不存在（父目录必须存在），此处按需创建目标目录
	switch fi, statErr := os.Stat(dest); {
	case statErr == nil && !fi.IsDir():
		return nil, errs.Newf(errs.CodeInvalidParam, "解压目标必须是目录")
	case statErr == nil:
	case os.IsNotExist(statErr):
		if err := os.Mkdir(dest, 0o755); err != nil {
			return nil, wrapFSError(err)
		}
	default:
		return nil, wrapFSError(statErr)
	}

	switch detectArchive(src) {
	case model.ArchiveZip:
		err = s.extractZip(src, dest)
	case model.ArchiveTarGz:
		err = s.extractTar(src, dest, "gzip")
	case model.ArchiveTarBz2:
		err = s.extractTar(src, dest, "bzip2")
	case model.ArchiveTar:
		err = s.extractTar(src, dest, "")
	default:
		return nil, errs.ErrArchiveUnsupported
	}
	if err != nil {
		return nil, err
	}
	return s.entryOf(dest)
}

// archiveFormat 解析显式格式或按后缀推断。
func archiveFormat(format, dest string) (string, error) {
	if format != "" {
		return format, nil
	}
	name := strings.ToLower(dest)
	switch {
	case strings.HasSuffix(name, ".zip"):
		return model.ArchiveZip, nil
	case strings.HasSuffix(name, ".tar.gz"), strings.HasSuffix(name, ".tgz"):
		return model.ArchiveTarGz, nil
	case strings.HasSuffix(name, ".tar"):
		return model.ArchiveTar, nil
	default:
		return "", errs.ErrArchiveUnsupported
	}
}

// detectArchive 先看后缀，再读文件头（docs/05 400008 的判定顺序）。
func detectArchive(path string) string {
	name := strings.ToLower(path)
	switch {
	case strings.HasSuffix(name, ".zip"):
		return model.ArchiveZip
	case strings.HasSuffix(name, ".tar.gz"), strings.HasSuffix(name, ".tgz"):
		return model.ArchiveTarGz
	case strings.HasSuffix(name, ".tar.bz2"), strings.HasSuffix(name, ".tbz2"):
		return model.ArchiveTarBz2
	case strings.HasSuffix(name, ".tar"):
		return model.ArchiveTar
	}

	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	head := make([]byte, 4)
	if n, _ := f.Read(head); n < 4 {
		return ""
	}
	switch {
	case head[0] == 'P' && head[1] == 'K':
		return model.ArchiveZip
	case head[0] == 0x1F && head[1] == 0x8B:
		return model.ArchiveTarGz
	case head[0] == 'B' && head[1] == 'Z' && head[2] == 'h':
		return model.ArchiveTarBz2
	}
	return ""
}

func writeZip(w io.Writer, srcs []string) error {
	zw := zip.NewWriter(w)
	for _, src := range srcs {
		base := filepath.Dir(src)
		err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // 读不到的条目跳过，不让整个打包失败
			}
			rel, err := filepath.Rel(base, p)
			if err != nil {
				return nil
			}
			fi, err := d.Info()
			if err != nil {
				return nil
			}
			if d.IsDir() {
				_, err = zw.Create(rel + "/")
				return err
			}
			if !fi.Mode().IsRegular() {
				return nil // 符号链接与设备文件不入包
			}
			hdr, err := zip.FileInfoHeader(fi)
			if err != nil {
				return err
			}
			hdr.Name, hdr.Method = rel, zip.Deflate
			zf, err := zw.CreateHeader(hdr)
			if err != nil {
				return err
			}
			in, err := os.Open(p)
			if err != nil {
				return nil
			}
			defer func() { _ = in.Close() }()
			_, err = io.Copy(zf, in)
			return err
		})
		if err != nil {
			_ = zw.Close()
			return wrapFSError(err)
		}
	}
	if err := zw.Close(); err != nil {
		return wrapFSError(err)
	}
	return nil
}

func writeTar(w io.Writer, srcs []string, gzipped bool) error {
	var closers []io.Closer
	if gzipped {
		gw := gzip.NewWriter(w)
		closers = append(closers, gw)
		w = gw
	}
	tw := tar.NewWriter(w)

	for _, src := range srcs {
		base := filepath.Dir(src)
		err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			fi, err := d.Info()
			if err != nil {
				return nil
			}
			if !fi.IsDir() && !fi.Mode().IsRegular() {
				return nil
			}
			rel, err := filepath.Rel(base, p)
			if err != nil {
				return nil
			}
			hdr, err := tar.FileInfoHeader(fi, "")
			if err != nil {
				return err
			}
			hdr.Name = filepath.ToSlash(rel)
			if fi.IsDir() {
				hdr.Name += "/"
			}
			if err := tw.WriteHeader(hdr); err != nil {
				return err
			}
			if fi.IsDir() {
				return nil
			}
			in, err := os.Open(p)
			if err != nil {
				return nil
			}
			defer func() { _ = in.Close() }()
			_, err = io.Copy(tw, in)
			return err
		})
		if err != nil {
			_ = tw.Close()
			return wrapFSError(err)
		}
	}
	if err := tw.Close(); err != nil {
		return wrapFSError(err)
	}
	for i := len(closers) - 1; i >= 0; i-- {
		if err := closers[i].Close(); err != nil {
			return wrapFSError(err)
		}
	}
	return nil
}

func (s *Service) extractZip(src, dest string) error {
	zr, err := zip.OpenReader(src)
	if err != nil {
		return errs.ErrArchiveCorrupt
	}
	defer func() { _ = zr.Close() }()

	for _, zf := range zr.File {
		target, err := s.safeJoin(dest, zf.Name)
		if err != nil {
			return err
		}
		if zf.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return wrapFSError(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return wrapFSError(err)
		}
		rc, err := zf.Open()
		if err != nil {
			return errs.ErrArchiveCorrupt
		}
		limit := int64(zf.CompressedSize64)*maxDecompressRatio + (1 << 20)
		err = writeLimited(target, rc, zf.Mode().Perm(), limit)
		_ = rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) extractTar(src, dest, compression string) error {
	f, err := os.Open(src)
	if err != nil {
		return wrapFSError(err)
	}
	defer func() { _ = f.Close() }()

	var r io.Reader = f
	switch compression {
	case "gzip":
		gr, err := gzip.NewReader(f)
		if err != nil {
			return errs.ErrArchiveCorrupt
		}
		defer func() { _ = gr.Close() }()
		r = gr
	case "bzip2":
		r = bzip2.NewReader(f)
	}

	// 以压缩包体积推算总解压上限，避免高压缩比炸弹撑满磁盘
	fi, err := f.Stat()
	if err != nil {
		return wrapFSError(err)
	}
	remaining := fi.Size()*maxDecompressRatio + (1 << 20)

	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return errs.ErrArchiveCorrupt
		}
		target, err := s.safeJoin(dest, hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, fs.FileMode(hdr.Mode).Perm()); err != nil {
				return wrapFSError(err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return wrapFSError(err)
			}
			if err := writeLimited(target, tr, fs.FileMode(hdr.Mode).Perm(), remaining); err != nil {
				return err
			}
			remaining -= hdr.Size
			if remaining <= 0 {
				return errs.Newf(errs.CodeArchiveCorrupt, "解压内容超出安全上限")
			}
		default:
			// 符号链接、硬链接、设备文件不还原，防止借解压绕过路径白名单
			continue
		}
	}
}

// safeJoin 把压缩包内的相对路径拼到目标目录，并再过一次路径守卫，
// 从而同时挡住 zip slip 与「解压到白名单外」两类问题。
func (s *Service) safeJoin(dest, name string) (string, error) {
	cleaned := filepath.Clean(filepath.FromSlash(name))
	if cleaned == "." || cleaned == string(os.PathSeparator) {
		return dest, nil
	}
	if filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, "..") {
		return "", errs.ErrPathEscape
	}
	target := filepath.Join(dest, cleaned)
	if target != dest && !strings.HasPrefix(target, dest+string(os.PathSeparator)) {
		return "", errs.ErrPathEscape
	}
	if _, err := s.guard.Resolve(filepath.Dir(target), pathguard.ModeWrite); err != nil {
		return "", err
	}
	return target, nil
}

// writeLimited 带上限地落盘，超限即删除半成品并按压缩包异常返回。
func writeLimited(target string, r io.Reader, perm fs.FileMode, limit int64) error {
	if perm == 0 {
		perm = 0o644
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm)
	if err != nil {
		return wrapFSError(err)
	}
	written, err := io.Copy(out, io.LimitReader(r, limit+1))
	closeErr := out.Close()
	if err != nil {
		_ = os.Remove(target)
		return wrapFSError(err)
	}
	if closeErr != nil {
		_ = os.Remove(target)
		return wrapFSError(closeErr)
	}
	if written > limit {
		_ = os.Remove(target)
		return errs.Newf(errs.CodeArchiveCorrupt, "解压内容超出安全上限")
	}
	return nil
}
