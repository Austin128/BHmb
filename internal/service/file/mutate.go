package file

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/novapanel/novapanel/internal/model"
	"github.com/novapanel/novapanel/internal/pkg/errs"
	"github.com/novapanel/novapanel/internal/service/file/pathguard"
)

// renameSuffixLimit 为自动改名时尝试的最大序号，超过即视为冲突。
const renameSuffixLimit = 1000

// CreateDir 新建目录。父目录必须已存在且可写，目标已存在返回 400003。
func (s *Service) CreateDir(req model.FileCreateRequest) (*model.FileEntry, error) {
	_, full, err := s.guard.ResolveParent(req.Path, pathguard.ModeWrite)
	if err != nil {
		return nil, err
	}
	perm, err := parseMode(req.Mode, 0o755)
	if err != nil {
		return nil, err
	}
	if err := os.Mkdir(full, perm); err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, errs.ErrFileExists
		}
		return nil, wrapFSError(err)
	}
	// Mkdir 受 umask 影响，显式 chmod 保证与请求一致
	if err := os.Chmod(full, perm); err != nil {
		return nil, wrapFSError(err)
	}
	return s.entryOf(full)
}

// CreateFile 新建空文件。
func (s *Service) CreateFile(req model.FileCreateRequest) (*model.FileEntry, error) {
	_, full, err := s.guard.ResolveParent(req.Path, pathguard.ModeWrite)
	if err != nil {
		return nil, err
	}
	perm, err := parseMode(req.Mode, 0o644)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(full, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, errs.ErrFileExists
		}
		return nil, wrapFSError(err)
	}
	if err := f.Close(); err != nil {
		return nil, wrapFSError(err)
	}
	if err := os.Chmod(full, perm); err != nil {
		return nil, wrapFSError(err)
	}
	return s.entryOf(full)
}

// Rename 同目录重命名。newName 只允许文件名，禁止借此跨目录移动。
func (s *Service) Rename(req model.FileRenameRequest) (*model.FileEntry, error) {
	src, err := s.guard.Resolve(req.Path, pathguard.ModeWrite)
	if err != nil {
		return nil, err
	}
	if err := s.ensureNotRoot(src); err != nil {
		return nil, err
	}
	name, err := validFileName(req.NewName)
	if err != nil {
		return nil, err
	}

	dst := filepath.Join(filepath.Dir(src), name)
	if dst == src {
		return s.entryOf(src)
	}
	if _, err := s.guard.Resolve(dst, pathguard.ModeWrite); err != nil {
		return nil, err
	}
	if _, err := os.Lstat(dst); err == nil {
		return nil, errs.ErrFileExists
	}
	if err := os.Rename(src, dst); err != nil {
		return nil, wrapFSError(err)
	}
	return s.entryOf(dst)
}

// Move 批量移动。dest 必须是已存在的目录，冲突策略见 model.FileConflict*。
func (s *Service) Move(req model.FileTransferRequest) (*model.FileBatchResponse, error) {
	return s.transfer(req, false)
}

// Copy 批量复制。目录递归复制，符号链接按链接本身复制。
func (s *Service) Copy(req model.FileTransferRequest) (*model.FileBatchResponse, error) {
	return s.transfer(req, true)
}

func (s *Service) transfer(req model.FileTransferRequest, isCopy bool) (*model.FileBatchResponse, error) {
	destDir, err := s.guard.Resolve(req.Dest, pathguard.ModeWrite)
	if err != nil {
		return nil, err
	}
	fi, err := os.Stat(destDir)
	if err != nil {
		return nil, wrapFSError(err)
	}
	if !fi.IsDir() {
		return nil, errs.Newf(errs.CodeInvalidParam, "目标必须是目录")
	}
	conflict := req.Conflict
	if conflict == "" {
		conflict = model.FileConflictReject
	}

	out := &model.FileBatchResponse{Results: make([]model.FileOpResult, 0, len(req.Paths))}
	for _, raw := range req.Paths {
		res := model.FileOpResult{Path: raw}
		dst, err := s.transferOne(raw, destDir, conflict, isCopy)
		if err != nil {
			res.Code, res.Message = errs.Code(err), errs.Message(err)
			out.Failed++
		} else {
			res.OK, res.Dest = true, dst
			out.Succeeded++
		}
		out.Results = append(out.Results, res)
	}
	return out, nil
}

func (s *Service) transferOne(raw, destDir, conflict string, isCopy bool) (string, error) {
	src, err := s.guard.Resolve(raw, pathguard.ModeWrite)
	if err != nil {
		return "", err
	}
	if err := s.ensureNotRoot(src); err != nil {
		return "", err
	}
	// 阻断「把目录移动/复制到自身或自身子目录」这类会造成递归的操作
	if destDir == src || strings.HasPrefix(destDir, src+string(os.PathSeparator)) {
		return "", errs.Newf(errs.CodeInvalidParam, "目标目录不能是源目录自身或其子目录")
	}

	dst := filepath.Join(destDir, filepath.Base(src))
	dst, err = s.applyConflict(dst, conflict)
	if err != nil {
		return "", err
	}
	if isCopy {
		if err := s.copyTree(src, dst); err != nil {
			return "", err
		}
		return dst, nil
	}
	if err := os.Rename(src, dst); err != nil {
		// 跨设备移动 rename 会失败，回退为「复制 + 删除」
		if isCrossDevice(err) {
			if err := s.copyTree(src, dst); err != nil {
				return "", err
			}
			if err := os.RemoveAll(src); err != nil {
				return "", wrapFSError(err)
			}
			return dst, nil
		}
		return "", wrapFSError(err)
	}
	return dst, nil
}

// applyConflict 按策略处理目标已存在：拒绝、覆盖（先删）、自动加序号。
func (s *Service) applyConflict(dst, conflict string) (string, error) {
	if _, err := os.Lstat(dst); err != nil {
		if os.IsNotExist(err) {
			return dst, nil
		}
		return "", wrapFSError(err)
	}
	switch conflict {
	case model.FileConflictOverwrite:
		if err := os.RemoveAll(dst); err != nil {
			return "", wrapFSError(err)
		}
		return dst, nil
	case model.FileConflictRename:
		return uniqueName(dst)
	default:
		return "", errs.ErrFileExists
	}
}

// uniqueName 生成 name(1).ext 形式的候选名。
func uniqueName(dst string) (string, error) {
	dir, base := filepath.Split(dst)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	for i := 1; i <= renameSuffixLimit; i++ {
		candidate := filepath.Join(dir, stem+"("+strconv.Itoa(i)+")"+ext)
		if _, err := os.Lstat(candidate); os.IsNotExist(err) {
			return candidate, nil
		}
	}
	return "", errs.ErrFileExists
}

// copyTree 递归复制。符号链接复制链接本身，避免顺着链接把目标内容也复制进来。
func (s *Service) copyTree(src, dst string) error {
	fi, err := os.Lstat(src)
	if err != nil {
		return wrapFSError(err)
	}
	switch {
	case fi.Mode()&fs.ModeSymlink != 0:
		target, err := os.Readlink(src)
		if err != nil {
			return wrapFSError(err)
		}
		if err := os.Symlink(target, dst); err != nil {
			return wrapFSError(err)
		}
		return nil
	case fi.IsDir():
		if err := os.MkdirAll(dst, fi.Mode().Perm()); err != nil {
			return wrapFSError(err)
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return wrapFSError(err)
		}
		for _, de := range entries {
			if err := s.copyTree(filepath.Join(src, de.Name()), filepath.Join(dst, de.Name())); err != nil {
				return err
			}
		}
		return os.Chmod(dst, fi.Mode().Perm())
	case fi.Mode().IsRegular():
		return copyFile(src, dst, fi.Mode().Perm())
	default:
		// 设备文件、套接字等特殊文件不复制，避免产生不可预期的宿主状态
		return errs.Newf(errs.CodeUnsupported, "不支持复制该类型文件")
	}
}

// Delete 批量删除。白名单根本身与非空判定交由 RemoveAll，逐项返回结果。
func (s *Service) Delete(req model.FileDeleteRequest) (*model.FileBatchResponse, error) {
	out := &model.FileBatchResponse{Results: make([]model.FileOpResult, 0, len(req.Paths))}
	for _, raw := range req.Paths {
		res := model.FileOpResult{Path: raw}
		if err := s.deleteOne(raw); err != nil {
			res.Code, res.Message = errs.Code(err), errs.Message(err)
			out.Failed++
		} else {
			res.OK = true
			out.Succeeded++
		}
		out.Results = append(out.Results, res)
	}
	return out, nil
}

func (s *Service) deleteOne(raw string) error {
	full, err := s.guard.Resolve(raw, pathguard.ModeWrite)
	if err != nil {
		return err
	}
	if err := s.ensureNotRoot(full); err != nil {
		return err
	}
	if _, err := os.Lstat(full); err != nil {
		return wrapFSError(err)
	}
	if err := os.RemoveAll(full); err != nil {
		return wrapFSError(err)
	}
	return nil
}

// Permission 修改权限与属主，可递归。mode、owner、group 均可单独省略。
func (s *Service) Permission(req model.FilePermissionRequest) (*model.FileEntry, error) {
	full, err := s.guard.Resolve(req.Path, pathguard.ModeWrite)
	if err != nil {
		return nil, err
	}
	if req.Mode == "" && req.Owner == "" && req.Group == "" {
		return nil, errs.Newf(errs.CodeInvalidParam, "至少指定权限、属主或属组之一")
	}

	var perm fs.FileMode
	if req.Mode != "" {
		if perm, err = parseMode(req.Mode, 0); err != nil {
			return nil, err
		}
	}
	uid, err := resolveOwner(req.Owner)
	if err != nil {
		return nil, errs.Newf(errs.CodeInvalidParam, "属主不存在")
	}
	gid, err := resolveGroup(req.Group)
	if err != nil {
		return nil, errs.Newf(errs.CodeInvalidParam, "属组不存在")
	}

	apply := func(p string) error {
		if req.Mode != "" {
			if err := os.Chmod(p, perm); err != nil {
				return wrapFSError(err)
			}
		}
		if uid >= 0 || gid >= 0 {
			if err := os.Chown(p, uid, gid); err != nil {
				return wrapFSError(err)
			}
		}
		return nil
	}

	if !req.Recursive {
		if err := apply(full); err != nil {
			return nil, err
		}
		return s.entryOf(full)
	}
	walkErr := filepath.WalkDir(full, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // 单个子项失败不中断整体递归
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil // 不改符号链接，防止意外改动链接目标的权限
		}
		return apply(p)
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return s.entryOf(full)
}

// ensureNotRoot 禁止对白名单根本身做改名、移动、删除，避免一次操作毁掉整个可访问范围。
func (s *Service) ensureNotRoot(full string) error {
	for _, root := range s.guard.AllowRoots() {
		if full == root {
			return errs.Newf(errs.CodeProtectedPath, "禁止操作根目录 %s", root)
		}
	}
	return nil
}

// entryOf 组装操作结果的元数据。
func (s *Service) entryOf(full string) (*model.FileEntry, error) {
	fi, err := os.Lstat(full)
	if err != nil {
		return nil, wrapFSError(err)
	}
	e := s.buildEntry(full, fi)
	return &e, nil
}

// parseMode 解析八进制权限串（0755、755 均可），空串取默认值。
func parseMode(mode string, def fs.FileMode) (fs.FileMode, error) {
	if strings.TrimSpace(mode) == "" {
		return def, nil
	}
	n, err := strconv.ParseUint(strings.TrimPrefix(mode, "0o"), 8, 32)
	if err != nil || n > 0o7777 {
		return 0, errs.Newf(errs.CodeInvalidParam, "权限格式不合法，应为 0755 这样的八进制")
	}
	return fs.FileMode(n), nil
}

// validFileName 校验文件名，规则见 docs/05 5.4 的文件名约束。
func validFileName(name string) (string, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "", name == ".", name == "..":
		return "", errs.Newf(errs.CodeInvalidParam, "文件名不合法")
	case len(name) > 255:
		return "", errs.Newf(errs.CodeInvalidParam, "文件名超过 255 字节")
	case strings.ContainsAny(name, "/\x00"):
		return "", errs.Newf(errs.CodeInvalidParam, "文件名不能包含斜杠或空字符")
	}
	return name, nil
}
