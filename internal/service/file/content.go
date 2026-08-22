package file

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/novapanel/novapanel/internal/model"
	"github.com/novapanel/novapanel/internal/pkg/errs"
	"github.com/novapanel/novapanel/internal/service/file/pathguard"
)

// backupKeep 为在线编辑保存时保留的历史备份份数（docs/05 5.8.4）。
const backupKeep = 5

// ReadContent 读取文本内容并返回 ETag。二进制或超限文件拒绝在线编辑。
func (s *Service) ReadContent(path string) (*model.FileContentResponse, error) {
	full, err := s.guard.Resolve(path, pathguard.ModeRead)
	if err != nil {
		return nil, err
	}
	fi, err := os.Stat(full)
	if err != nil {
		return nil, wrapFSError(err)
	}
	if fi.IsDir() {
		return nil, errs.Newf(errs.CodeInvalidParam, "目标是目录，无法读取内容")
	}
	if fi.Size() > s.cfg.MaxEditSize {
		return nil, errs.Newf(errs.CodeFileTooLarge, "文件超过在线编辑上限 %s", humanSize(s.cfg.MaxEditSize))
	}

	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(full), "."))
	mime := detectMime(full, ext)
	if !isTextMime(mime) {
		return nil, errs.ErrNotUTF8
	}

	data, err := os.ReadFile(full)
	if err != nil {
		return nil, wrapFSError(err)
	}
	return &model.FileContentResponse{
		Path:    full,
		Content: string(data),
		Size:    int64(len(data)),
		ETag:    etagOf(data),
		Mime:    mime,
	}, nil
}

// SaveContent 保存文本内容。写入前用 ETag 做乐观锁：
// 请求 ETag 与磁盘现状不一致时返回 400011，由前端决定覆盖或对比。
func (s *Service) SaveContent(req model.FileSaveRequest) (*model.FileSaveResponse, error) {
	full, err := s.guard.Resolve(req.Path, pathguard.ModeWrite)
	if err != nil {
		return nil, err
	}
	if int64(len(req.Content)) > s.cfg.MaxEditSize {
		return nil, errs.Newf(errs.CodeFileTooLarge, "内容超过在线编辑上限 %s", humanSize(s.cfg.MaxEditSize))
	}

	perm := fs.FileMode(0o644)
	fi, statErr := os.Stat(full)
	switch {
	case statErr == nil:
		if fi.IsDir() {
			return nil, errs.Newf(errs.CodeInvalidParam, "目标是目录，无法写入")
		}
		perm = fi.Mode().Perm()
		current, err := os.ReadFile(full)
		if err != nil {
			return nil, wrapFSError(err)
		}
		// ETag 为空说明前端以为文件不存在，此时磁盘上已有内容同样属于冲突
		if req.ETag == "" || req.ETag != etagOf(current) {
			return nil, errs.ErrFileChanged
		}
		if req.CreateBackup {
			if err := s.backup(full, current, perm); err != nil {
				return nil, err
			}
		}
	case os.IsNotExist(statErr):
		if req.ETag != "" {
			return nil, errs.ErrFileNotFound
		}
	default:
		return nil, wrapFSError(statErr)
	}

	data := []byte(req.Content)
	if err := writeFileAtomic(full, data, perm); err != nil {
		return nil, err
	}
	return &model.FileSaveResponse{Path: full, Size: int64(len(data)), ETag: etagOf(data)}, nil
}

// OpenForDownload 返回已校验路径的只读句柄，交由 handler 用 http.ServeContent
// 输出（天然支持 Range）。调用方负责关闭。
func (s *Service) OpenForDownload(path string) (*os.File, fs.FileInfo, error) {
	full, err := s.guard.Resolve(path, pathguard.ModeRead)
	if err != nil {
		return nil, nil, err
	}
	fi, err := os.Stat(full)
	if err != nil {
		return nil, nil, wrapFSError(err)
	}
	if fi.IsDir() {
		return nil, nil, errs.Newf(errs.CodeInvalidParam, "目录请使用打包下载")
	}
	f, err := os.Open(full)
	if err != nil {
		return nil, nil, wrapFSError(err)
	}
	return f, fi, nil
}

// backup 保存前把原文件复制为 .<name>.bak.<时间戳>，并只保留最近 backupKeep 份。
func (s *Service) backup(full string, content []byte, perm fs.FileMode) error {
	dir, name := filepath.Split(full)
	prefix := "." + name + ".bak."
	dst := filepath.Join(dir, prefix+time.Now().Format("20060102150405"))
	if err := os.WriteFile(dst, content, perm); err != nil {
		return wrapFSError(err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil // 清理失败不影响本次保存
	}
	var backups []string
	for _, de := range entries {
		if !de.IsDir() && strings.HasPrefix(de.Name(), prefix) {
			backups = append(backups, de.Name())
		}
	}
	if len(backups) <= backupKeep {
		return nil
	}
	sort.Strings(backups) // 名称含时间戳，字典序即时间序
	for _, old := range backups[:len(backups)-backupKeep] {
		_ = os.Remove(filepath.Join(dir, old))
	}
	return nil
}

// writeFileAtomic 先写同目录临时文件再 rename，避免写入中断留下半截内容。
// 同目录保证 rename 不跨设备。
func writeFileAtomic(full string, data []byte, perm fs.FileMode) error {
	dir := filepath.Dir(full)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(full)+".tmp*")
	if err != nil {
		return wrapFSError(err)
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName) // rename 成功后此处为空操作
	}()

	if _, err := tmp.Write(data); err != nil {
		return wrapFSError(err)
	}
	if err := tmp.Sync(); err != nil {
		return wrapFSError(err)
	}
	if err := tmp.Close(); err != nil {
		return wrapFSError(err)
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return wrapFSError(err)
	}
	if err := os.Rename(tmpName, full); err != nil {
		return wrapFSError(err)
	}
	return nil
}

// etagOf 用内容哈希作为 ETag，宿主外部改动也能被识别（mtime 精度不可靠）。
func etagOf(data []byte) string {
	sum := sha256.Sum256(data)
	return `"` + hex.EncodeToString(sum[:16]) + `"`
}

// copyFile 复制普通文件并保留权限位。
func copyFile(src, dst string, perm fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return wrapFSError(err)
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return wrapFSError(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return wrapFSError(err)
	}
	if err := out.Close(); err != nil {
		return wrapFSError(err)
	}
	return nil
}

// humanSize 用于错误文案，避免把字节数直接抛给用户。
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	units := []string{"KB", "MB", "GB", "TB"}
	val := float64(n)
	for _, u := range units {
		val /= unit
		if val < unit {
			return fmt.Sprintf("%.0f %s", val, u)
		}
	}
	return fmt.Sprintf("%.0f PB", val/unit)
}
