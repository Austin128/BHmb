package site

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/novapanel/novapanel/internal/model"
	"github.com/novapanel/novapanel/internal/pkg/errs"
)

const (
	// logTailMaxLines 是单次可请求的最大行数。再多前端也渲染不动，
	// 需要全量分析应该走文件管理下载。
	logTailMaxLines = 2000
	// logTailDefaultLines 是不传 lines 时的默认行数。
	logTailDefaultLines = 200
	// logTailMaxBytes 限制从文件尾部读取的字节数，避免单行超长日志（如 Lua 堆栈）
	// 把整个进程内存拖爆。
	logTailMaxBytes = 512 * 1024
)

// Logs 读取站点日志尾部。
// 日志文件由 Nginx 持续追加，因此从文件末尾往前读固定窗口，再按行切分，
// 比顺序扫描整个文件快得多，也不受文件大小影响。
func (s *Service) Logs(ctx context.Context, actor Actor, id int64, kind string, lines int) (*model.SiteLogDTO, error) {
	site, err := s.repo.FindByID(ctx, actor.TenantID, id)
	if err != nil {
		return nil, err
	}

	path, enabled, err := s.logTarget(site, kind)
	if err != nil {
		return nil, err
	}
	if lines <= 0 {
		lines = logTailDefaultLines
	}
	if lines > logTailMaxLines {
		lines = logTailMaxLines
	}

	out := &model.SiteLogDTO{
		SiteID:  site.ID,
		Name:    site.Name,
		Type:    kind,
		Path:    path,
		Enabled: enabled,
		Lines:   []string{},
	}

	st, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			// 日志文件在首次请求到达前不存在，这是正常状态而不是错误：
			// 前端据 exists 提示「暂无日志」，不弹错误。
			return out, nil
		}
		return nil, errs.Wrap(err, errs.CodeInternal, "读取日志文件失败")
	}
	out.Exists = true
	out.Size = st.Size()
	out.ModifiedAt = st.ModTime().UnixMilli()
	if st.Size() == 0 {
		return out, nil
	}

	chunk, partial, err := readTail(path, st.Size(), logTailMaxBytes)
	if err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "读取日志文件失败")
	}
	all := strings.Split(strings.TrimRight(string(chunk), "\n"), "\n")
	if partial && len(all) > 1 {
		// 窗口起点大概率落在某一行中间，丢掉这半行避免展示乱码片段。
		// 但只有一行时不能丢：那是一条超过窗口的超长日志，丢掉界面就全空了，
		// 此时保留被截断的尾部，由 truncated 告知前端内容不完整。
		all = all[1:]
	}
	if len(all) > lines {
		all = all[len(all)-lines:]
		partial = true
	}
	out.Lines = all
	out.Truncated = partial
	return out, nil
}

// ClearLog 清空站点日志。
// 用截断而不是删除：Nginx 已打开的文件描述符在删除后仍指向被摘除的 inode，
// 新日志会写进一个看不见的文件里，必须 reload 才恢复。截断则让写入立即落到同一 inode。
func (s *Service) ClearLog(ctx context.Context, actor Actor, id int64, kind string) (*model.SiteLogDTO, error) {
	site, err := s.repo.FindByID(ctx, actor.TenantID, id)
	if err != nil {
		return nil, err
	}
	path, _, err := s.logTarget(site, kind)
	if err != nil {
		return nil, err
	}
	if err := os.Truncate(path, 0); err != nil && !os.IsNotExist(err) {
		return nil, errs.Wrap(err, errs.CodeInternal, "清空日志失败")
	}
	return s.Logs(ctx, actor, site.ID, kind, logTailDefaultLines)
}

// logTarget 解析日志类型对应的文件路径与开关状态。
// 路径必须落在面板配置的日志目录内：记录里的路径来自数据库，
// 一旦被越权改写就会变成任意文件读取入口，因此这里做一次围栏校验。
func (s *Service) logTarget(site *model.WebSite, kind string) (string, bool, error) {
	var path string
	var enabled bool
	switch kind {
	case model.SiteLogAccess:
		path, enabled = site.AccessLogPath, site.AccessLogEnabled
		if path == "" {
			path = s.accessLogPath(site.Name)
		}
	case model.SiteLogError:
		path, enabled = site.ErrorLogPath, site.ErrorLogEnabled
		if path == "" {
			path = s.errorLogPath(site.Name)
		}
	default:
		return "", false, errs.New(errs.CodeInvalidParam, "日志类型只支持 access 或 error")
	}
	if !withinDir(s.cfg.LogDir, path) {
		return "", false, errs.New(errs.CodeInvalidParam, "日志路径不在面板日志目录内")
	}
	return path, enabled, nil
}

// withinDir 判断 target 是否位于 dir 之内（含 dir 自身）。
func withinDir(dir, target string) bool {
	d, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	t, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(d, t)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// readTail 读取文件尾部最多 max 字节，第二个返回值表示前面还有未读取的内容。
func readTail(path string, size, max int64) ([]byte, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = f.Close() }()

	offset := int64(0)
	partial := false
	if size > max {
		offset = size - max
		partial = true
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, false, err
	}
	buf, err := io.ReadAll(io.LimitReader(f, max))
	if err != nil {
		return nil, false, err
	}
	return buf, partial, nil
}
