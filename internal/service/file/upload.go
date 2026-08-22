package file

import (
	"io"
	"os"
	"path/filepath"

	"github.com/novapanel/novapanel/internal/model"
	"github.com/novapanel/novapanel/internal/pkg/errs"
	"github.com/novapanel/novapanel/internal/service/file/pathguard"
)

// UploadRequest 为单文件上传的服务层入参。handler 负责解析 multipart，
// 服务层只关心「目标目录 + 文件名 + 数据流」。
type UploadRequest struct {
	Dir      string
	Name     string
	Size     int64
	Conflict string
	Src      io.Reader
}

// Upload 把上传流写入目标目录。写入前先按声明大小做上限校验，
// 写入时再用 LimitReader 兜底，防止客户端谎报 Content-Length。
func (s *Service) Upload(req UploadRequest) (*model.FileEntry, error) {
	dir, err := s.guard.Resolve(req.Dir, pathguard.ModeWrite)
	if err != nil {
		return nil, err
	}
	fi, err := os.Stat(dir)
	if err != nil {
		return nil, wrapFSError(err)
	}
	if !fi.IsDir() {
		return nil, errs.Newf(errs.CodeInvalidParam, "上传目标必须是目录")
	}

	name, err := validFileName(filepath.Base(req.Name))
	if err != nil {
		return nil, err
	}
	if req.Size > s.cfg.MaxUploadSize {
		return nil, errs.Newf(errs.CodeFileTooLarge, "文件超过上传上限 %s", humanSize(s.cfg.MaxUploadSize))
	}

	target := filepath.Join(dir, name)
	if _, err := s.guard.Resolve(target, pathguard.ModeWrite); err != nil {
		return nil, err
	}
	conflict := req.Conflict
	if conflict == "" {
		conflict = model.FileConflictReject
	}
	target, err = s.applyConflict(target, conflict)
	if err != nil {
		return nil, err
	}

	// 先落临时文件再改名，避免上传中断在目标位置留下不完整文件
	tmp, err := os.CreateTemp(dir, ".upload-*")
	if err != nil {
		return nil, wrapFSError(err)
	}
	tmpName := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}

	written, err := io.Copy(tmp, io.LimitReader(req.Src, s.cfg.MaxUploadSize+1))
	if err != nil {
		cleanup()
		return nil, wrapFSError(err)
	}
	if written > s.cfg.MaxUploadSize {
		cleanup()
		return nil, errs.Newf(errs.CodeFileTooLarge, "文件超过上传上限 %s", humanSize(s.cfg.MaxUploadSize))
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return nil, wrapFSError(err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return nil, wrapFSError(err)
	}
	// CreateTemp 建出的是 0600，改为常规文件权限，便于站点进程读取
	if err := os.Chmod(tmpName, 0o644); err != nil {
		_ = os.Remove(tmpName)
		return nil, wrapFSError(err)
	}
	if err := os.Rename(tmpName, target); err != nil {
		_ = os.Remove(tmpName)
		return nil, wrapFSError(err)
	}
	return s.entryOf(target)
}
