package file

import (
	"encoding/base64"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/novapanel/novapanel/internal/model"
	"github.com/novapanel/novapanel/internal/pkg/errs"
	"github.com/novapanel/novapanel/internal/service/file/pathguard"
)

// 列目录与遍历的保护性上限（docs/08 8.2.1）。
const (
	// fullListThreshold 以下的目录一次性全量返回，避免小目录多一次请求。
	fullListThreshold = 2000
	// degradedThreshold 以上的目录停止精确统计与探测，只做分页返回。
	degradedThreshold = 100000
	defaultPageSize   = 500
	maxPageSize       = 2000
	// duMaxEntries 限制目录统计遍历的条目数，防止超大目录长时间占用请求。
	duMaxEntries       = 200000
	defaultSearchDepth = 6
)

// Config 为文件服务的大小与数量限制，由 app 层从 config.FileConfig 转换而来。
type Config struct {
	MaxEditSize      int64
	MaxUploadSize    int64
	MaxListEntries   int
	MaxSearchResults int
	// UploadTempDir 存放分片上传的会话与分片，由服务端自管，不落在用户目录。
	UploadTempDir string
}

func (c *Config) withDefaults() {
	if c.MaxEditSize <= 0 {
		c.MaxEditSize = 4 << 20
	}
	if c.MaxUploadSize <= 0 {
		c.MaxUploadSize = 2 << 30
	}
	if c.MaxListEntries <= 0 {
		c.MaxListEntries = 5000
	}
	if c.MaxSearchResults <= 0 {
		c.MaxSearchResults = 1000
	}
	if c.UploadTempDir == "" {
		c.UploadTempDir = filepath.Join(os.TempDir(), "novapanel-upload")
	}
}

// Service 是文件管理的业务入口。构造后只读，可并发使用。
type Service struct {
	guard *pathguard.Guard
	cfg   Config
	ids   *idCache
}

// New 构造文件服务。guard 为必填，缺失时文件模块不可用。
func New(guard *pathguard.Guard, cfg Config) (*Service, error) {
	if guard == nil {
		return nil, errs.Newf(errs.CodeInternal, "文件服务缺少路径守卫")
	}
	cfg.withDefaults()
	return &Service{guard: guard, cfg: cfg, ids: newIDCache()}, nil
}

// AllowRoots 暴露白名单根，供前端目录树渲染。
func (s *Service) AllowRoots() []string {
	return s.guard.AllowRoots()
}

// List 返回目录列表。策略见 docs/08 8.2.1：小目录全量、大目录游标分页、超大目录降级。
func (s *Service) List(req model.FileListRequest) (*model.FileListResponse, error) {
	dir, err := s.guard.Resolve(req.Path, pathguard.ModeRead)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(dir)
	if err != nil {
		return nil, wrapFSError(err)
	}
	if !info.IsDir() {
		return nil, errs.Newf(errs.CodeInvalidParam, "目标不是目录")
	}

	// ReadDir 只取名称与类型，不触发逐条 lstat，超大目录首屏才有可接受的延迟
	all, err := os.ReadDir(dir)
	if err != nil {
		return nil, wrapFSError(err)
	}

	degraded := len(all) > degradedThreshold
	entries := filterEntries(all, req)
	sortEntries(entries, req.Sort, req.Order)

	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}

	resp := &model.FileListResponse{
		Path:       dir,
		Parent:     s.parentOf(dir),
		Total:      int64(len(entries)),
		Degraded:   degraded,
		AllowRoots: s.guard.AllowRoots(),
	}
	if degraded {
		resp.Total = -1
	}

	start := 0
	if req.Cursor != "" {
		start = cursorIndex(entries, req.Cursor)
	}
	paged := degraded || len(entries) > fullListThreshold || req.Cursor != ""
	end := len(entries)
	if paged && start+pageSize < end {
		end = start + pageSize
	}
	if start > len(entries) {
		start = len(entries)
	}
	page := entries[start:end]

	resp.Paged = paged
	resp.Items = make([]model.FileEntry, 0, len(page))
	for _, de := range page {
		full := filepath.Join(dir, de.Name())
		fi, err := os.Lstat(full)
		if err != nil {
			continue // 列举期间被删除的条目直接跳过，不让整页失败
		}
		resp.Items = append(resp.Items, s.buildEntry(full, fi))
	}
	if paged && end < len(entries) {
		resp.NextCursor = encodeCursor(entries[end-1].Name())
	}
	return resp, nil
}

// Stat 返回单个文件或目录的元数据，MIME 走完整探测。
func (s *Service) Stat(path string) (*model.FileEntry, error) {
	full, err := s.guard.Resolve(path, pathguard.ModeRead)
	if err != nil {
		return nil, err
	}
	fi, err := os.Lstat(full)
	if err != nil {
		return nil, wrapFSError(err)
	}
	e := s.buildEntry(full, fi)
	if !e.IsDir && !e.LinkBroken {
		e.Mime = detectMime(full, e.Ext)
	}
	return &e, nil
}

// Du 递归统计目录占用。达到遍历上限时返回 Truncated=true，结果为下限值。
func (s *Service) Du(path string) (*model.FileDuResponse, error) {
	full, err := s.guard.Resolve(path, pathguard.ModeRead)
	if err != nil {
		return nil, err
	}

	out := &model.FileDuResponse{Path: full}
	visited := 0
	walkErr := filepath.WalkDir(full, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			// 无权限的子目录跳过，不中断整体统计
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		visited++
		if visited > duMaxEntries {
			out.Truncated = true
			return filepath.SkipAll
		}
		if d.IsDir() {
			if p != full {
				out.Dirs++
			}
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		if fi.Mode().IsRegular() {
			out.Files++
			out.Size += fi.Size()
		}
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, filepath.SkipAll) {
		return nil, wrapFSError(walkErr)
	}
	return out, nil
}

// Search 在给定目录下按名称递归检索，深度与结果数受限以保证响应时间可控。
func (s *Service) Search(req model.FileSearchRequest) (*model.FileSearchResponse, error) {
	root, err := s.guard.Resolve(req.Path, pathguard.ModeRead)
	if err != nil {
		return nil, err
	}
	keyword := strings.ToLower(strings.TrimSpace(req.Keyword))
	if keyword == "" {
		return nil, errs.Newf(errs.CodeInvalidParam, "关键词不能为空")
	}

	limit := req.Limit
	if limit <= 0 || limit > s.cfg.MaxSearchResults {
		limit = s.cfg.MaxSearchResults
	}
	maxDepth := req.MaxDepth
	if maxDepth <= 0 {
		maxDepth = defaultSearchDepth
	}
	rootDepth := strings.Count(root, string(os.PathSeparator))

	out := &model.FileSearchResponse{Items: make([]model.FileEntry, 0, 32)}
	walkErr := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() && strings.Count(p, string(os.PathSeparator))-rootDepth >= maxDepth {
			return fs.SkipDir
		}
		if p == root || !strings.Contains(strings.ToLower(d.Name()), keyword) {
			return nil
		}
		if len(out.Items) >= limit {
			out.Truncated = true
			return filepath.SkipAll
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		out.Items = append(out.Items, s.buildEntry(p, fi))
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, filepath.SkipAll) {
		return nil, wrapFSError(walkErr)
	}
	out.Total = int64(len(out.Items))
	return out, nil
}

// parentOf 返回仍在白名单内的上级目录，越界或已是根时返回空串。
func (s *Service) parentOf(dir string) string {
	parent := filepath.Dir(dir)
	if parent == dir {
		return ""
	}
	if _, err := s.guard.Resolve(parent, pathguard.ModeRead); err != nil {
		return ""
	}
	return parent
}

// filterEntries 按隐藏文件、仅目录与关键词过滤。
func filterEntries(all []os.DirEntry, req model.FileListRequest) []os.DirEntry {
	keyword := strings.ToLower(strings.TrimSpace(req.Keyword))
	out := make([]os.DirEntry, 0, len(all))
	for _, de := range all {
		name := de.Name()
		if !req.ShowHidden && strings.HasPrefix(name, ".") {
			continue
		}
		if req.OnlyDir && !de.IsDir() {
			continue
		}
		if keyword != "" && !strings.Contains(strings.ToLower(name), keyword) {
			continue
		}
		out = append(out, de)
	}
	return out
}

// sortEntries 目录始终优先于文件，其后按指定字段排序。
// size 与 mtime 需要 Info()，失败时按零值处理，保证排序稳定不 panic。
func sortEntries(entries []os.DirEntry, field, order string) {
	desc := order == model.FileOrderDesc
	less := func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.IsDir() != b.IsDir() {
			return a.IsDir()
		}
		switch field {
		case model.FileSortSize:
			return infoSize(a) < infoSize(b)
		case model.FileSortMtime:
			return infoMtime(a) < infoMtime(b)
		case model.FileSortType:
			ea, eb := filepath.Ext(a.Name()), filepath.Ext(b.Name())
			if ea != eb {
				return ea < eb
			}
		}
		return strings.ToLower(a.Name()) < strings.ToLower(b.Name())
	}
	sort.SliceStable(entries, func(i, j int) bool {
		// 目录优先规则不受 order 影响，只有同类条目之间才反转
		a, b := entries[i], entries[j]
		if a.IsDir() != b.IsDir() {
			return a.IsDir()
		}
		if desc {
			return less(j, i)
		}
		return less(i, j)
	})
}

func infoSize(de os.DirEntry) int64 {
	fi, err := de.Info()
	if err != nil {
		return 0
	}
	return fi.Size()
}

func infoMtime(de os.DirEntry) int64 {
	fi, err := de.Info()
	if err != nil {
		return 0
	}
	return fi.ModTime().UnixNano()
}

// encodeCursor 生成不透明游标。内容为上一页最后一项的名称，
// 因此对任意排序方式都能在新一轮排序结果中重新定位。
func encodeCursor(name string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(name))
}

// cursorIndex 解析游标并返回下一页起点。游标失效（条目已被删除）时从头开始，
// 宁可重复一页也不返回错误中断浏览。
func cursorIndex(entries []os.DirEntry, cursor string) int {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0
	}
	name := string(raw)
	for i, de := range entries {
		if de.Name() == name {
			return i + 1
		}
	}
	return 0
}

// wrapFSError 把文件系统错误翻译为业务错误码，避免把宿主路径细节泄漏给前端。
func wrapFSError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, os.ErrNotExist):
		return errs.ErrFileNotFound
	case errors.Is(err, os.ErrExist):
		return errs.ErrFileExists
	case errors.Is(err, os.ErrPermission):
		return errs.ErrFilePermission
	case isNoSpace(err):
		return errs.ErrDiskFull
	default:
		return errs.Wrap(err, errs.CodeInternal, "文件操作失败")
	}
}
