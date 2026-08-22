package file

import (
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/novapanel/novapanel/internal/model"
	"github.com/novapanel/novapanel/internal/pkg/errs"
	"github.com/novapanel/novapanel/internal/service/file/pathguard"
)

// 分片上传的边界值（docs/05 5.8.4）。分片落在服务端自管的临时目录，
// 不写进用户目录，避免上传中断在站点目录留下垃圾。
const (
	defaultChunkSize = 8 << 20
	minChunkSize     = 64 << 10
	maxChunkSize     = 64 << 20
	// maxChunks 限制单次会话的分片数，防止客户端用极小分片打爆 inode。
	maxChunks = 20000
	// uploadTTL 为会话有效期，与 docs/05 的 400007 语义一致。
	uploadTTL = 24 * time.Hour
	// uploadIDBytes 决定 uploadId 的随机长度，形如 up_<24 hex>。
	uploadIDBytes = 12
	metaFileName  = "meta.json"
	partSuffix    = ".part"
)

// uploadMeta 为会话元数据，落盘在会话目录下的 meta.json。
// 分片是否到位由目录里的 <index>.part 体现，因此除 init 外不再改写该文件，
// 并发上传分片时无需加锁。
type uploadMeta struct {
	UploadID    string `json:"uploadId"`
	Dir         string `json:"dir"`
	Filename    string `json:"filename"`
	Size        int64  `json:"size"`
	ChunkSize   int64  `json:"chunkSize"`
	TotalChunks int    `json:"totalChunks"`
	Conflict    string `json:"conflict"`
	Hash        string `json:"hash,omitempty"`
	CreatedAt   int64  `json:"createdAt"`
}

// expireAtMs 为会话失效时刻（Unix 毫秒），内部判定过期用。
func (m *uploadMeta) expireAtMs() int64 {
	return m.CreatedAt + int64(uploadTTL/time.Millisecond)
}

// expireAtRFC3339 按 docs/05 5.2.5 的业务时间约定给前端返回可读时间。
func (m *uploadMeta) expireAtRFC3339() string {
	return time.UnixMilli(m.expireAtMs()).Format(time.RFC3339)
}

func (m *uploadMeta) expired(now time.Time) bool {
	return now.UnixMilli() > m.expireAtMs()
}

// chunkLen 返回第 index 片的期望字节数，末片通常短于 chunkSize。
func (m *uploadMeta) chunkLen(index int) int64 {
	if index < 0 || index >= m.TotalChunks {
		return 0
	}
	if index == m.TotalChunks-1 {
		return m.Size - m.ChunkSize*int64(m.TotalChunks-1)
	}
	return m.ChunkSize
}

// MissingChunksError 在合并时缺片返回。包的 *errs.Error 让统一错误层仍按 400006 落码，
// handler 可再取出 Missing 回填 data.missing 供前端只重传缺失分片。
type MissingChunksError struct {
	Err      *errs.Error
	UploadID string
	Missing  []int
}

func (e *MissingChunksError) Error() string { return e.Err.Error() }

func (e *MissingChunksError) Unwrap() error { return e.Err }

// ChunkRequest 为单个分片的服务层入参。Checksum 形如 md5:<hex> 或 sha256:<hex>，
// 为空时只校验长度。
type ChunkRequest struct {
	UploadID string
	Index    int
	Size     int64
	Checksum string
	Src      io.Reader
}

// InitUpload 建立分片上传会话。命中同名同哈希的既有文件时直接秒传，
// 命中未过期的同参数会话时复用，从而支持刷新页面后继续断点续传。
func (s *Service) InitUpload(req model.FileUploadInitRequest) (*model.FileUploadInitResponse, error) {
	dir, err := s.guard.Resolve(req.Path, pathguard.ModeWrite)
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
	name, err := validFileName(filepath.Base(req.Filename))
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

	chunkSize := clampChunkSize(req.ChunkSize)
	total := int((req.Size + chunkSize - 1) / chunkSize)
	if total > maxChunks {
		return nil, errs.Newf(errs.CodeInvalidParam,
			"分片数 %d 超过上限 %d，请调大 chunkSize", total, maxChunks)
	}
	conflict := req.Conflict
	if conflict == "" {
		conflict = model.FileConflictReject
	}

	// 秒传：目标已存在且整文件哈希一致时，不必再传一遍
	if req.Hash != "" {
		same, err := sameFileHash(target, req.Size, req.Hash)
		if err != nil {
			return nil, err
		}
		if same {
			entry, err := s.entryOf(target)
			if err != nil {
				return nil, err
			}
			return &model.FileUploadInitResponse{
				ChunkSize: chunkSize, TotalChunks: total, UploadedChunks: []int{},
				QuickUpload: true, Entry: entry,
			}, nil
		}
	}

	root, err := s.uploadRoot()
	if err != nil {
		return nil, err
	}
	s.gcUploads(root)

	// 复用可续传的既有会话，避免刷新页面后从 0 重传
	if meta, ok := s.findResumable(root, dir, name, req.Size, chunkSize, conflict); ok {
		uploaded, err := uploadedChunks(filepath.Join(root, meta.UploadID), meta.TotalChunks)
		if err != nil {
			return nil, err
		}
		return &model.FileUploadInitResponse{
			UploadID: meta.UploadID, ChunkSize: meta.ChunkSize, TotalChunks: meta.TotalChunks,
			UploadedChunks: uploaded, ExpireAt: meta.expireAtRFC3339(),
		}, nil
	}

	id, err := newUploadID()
	if err != nil {
		return nil, err
	}
	meta := &uploadMeta{
		UploadID: id, Dir: dir, Filename: name, Size: req.Size, ChunkSize: chunkSize,
		TotalChunks: total, Conflict: conflict, Hash: req.Hash,
		CreatedAt: time.Now().UnixMilli(),
	}
	sessionDir := filepath.Join(root, id)
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		return nil, wrapFSError(err)
	}
	if err := writeMeta(sessionDir, meta); err != nil {
		_ = os.RemoveAll(sessionDir)
		return nil, err
	}
	return &model.FileUploadInitResponse{
		UploadID: id, ChunkSize: chunkSize, TotalChunks: total,
		UploadedChunks: []int{}, ExpireAt: meta.expireAtRFC3339(),
	}, nil
}

// UploadChunk 落盘单个分片。先写临时文件再改名，重传同一分片天然幂等。
func (s *Service) UploadChunk(req ChunkRequest) (*model.FileUploadChunkResponse, error) {
	sessionDir, meta, err := s.loadSession(req.UploadID)
	if err != nil {
		return nil, err
	}
	if req.Index < 0 || req.Index >= meta.TotalChunks {
		return nil, errs.Newf(errs.CodeInvalidParam, "分片序号超出范围 0~%d", meta.TotalChunks-1)
	}
	want := meta.chunkLen(req.Index)
	if req.Size > 0 && req.Size != want {
		return nil, errs.ErrChunkChecksum.WithDetail("分片 %d 长度 %d，期望 %d", req.Index, req.Size, want)
	}

	sum, err := newChecksum(req.Checksum)
	if err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(sessionDir, fmt.Sprintf("%d-*.tmp", req.Index))
	if err != nil {
		return nil, wrapFSError(err)
	}
	tmpName := tmp.Name()
	discard := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}

	var dst io.Writer = tmp
	if sum != nil {
		dst = io.MultiWriter(tmp, sum.hash)
	}
	// 多读 1 字节以识别「实际长度超过期望」的客户端
	written, err := io.Copy(dst, io.LimitReader(req.Src, want+1))
	if err != nil {
		discard()
		return nil, wrapFSError(err)
	}
	if written != want {
		discard()
		return nil, errs.ErrChunkChecksum.WithDetail("分片 %d 实收 %d 字节，期望 %d", req.Index, written, want)
	}
	if sum != nil && !sum.matches() {
		discard()
		return nil, errs.ErrChunkChecksum.WithDetail("分片 %d 校验和不匹配", req.Index)
	}
	if err := tmp.Sync(); err != nil {
		discard()
		return nil, wrapFSError(err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return nil, wrapFSError(err)
	}
	if err := os.Rename(tmpName, partPath(sessionDir, req.Index)); err != nil {
		_ = os.Remove(tmpName)
		return nil, wrapFSError(err)
	}

	uploaded, err := uploadedChunks(sessionDir, meta.TotalChunks)
	if err != nil {
		return nil, err
	}
	return &model.FileUploadChunkResponse{
		UploadID: meta.UploadID, Index: req.Index, Received: written,
		UploadedCount: len(uploaded), TotalChunks: meta.TotalChunks,
	}, nil
}

// UploadStatus 返回已到位与缺失的分片，供断点续传对齐进度。
func (s *Service) UploadStatus(uploadID string) (*model.FileUploadStatusResponse, error) {
	sessionDir, meta, err := s.loadSession(uploadID)
	if err != nil {
		return nil, err
	}
	uploaded, err := uploadedChunks(sessionDir, meta.TotalChunks)
	if err != nil {
		return nil, err
	}
	return &model.FileUploadStatusResponse{
		UploadID: meta.UploadID, Path: meta.Dir, Filename: meta.Filename, Size: meta.Size,
		ChunkSize: meta.ChunkSize, TotalChunks: meta.TotalChunks,
		UploadedChunks: uploaded, MissingChunks: missingOf(uploaded, meta.TotalChunks),
		ExpireAt: meta.expireAtRFC3339(),
	}, nil
}

// CompleteUpload 按序合并分片。缺片时返回 MissingChunksError（400006）。
// 合并期间用 meta.json 改名做互斥，重复调用的第二次会得到会话不存在。
func (s *Service) CompleteUpload(req model.FileUploadCompleteRequest) (*model.FileUploadCompleteResponse, error) {
	started := time.Now()
	sessionDir, meta, err := s.loadSession(req.UploadID)
	if err != nil {
		return nil, err
	}
	uploaded, err := uploadedChunks(sessionDir, meta.TotalChunks)
	if err != nil {
		return nil, err
	}
	if missing := missingOf(uploaded, meta.TotalChunks); len(missing) > 0 {
		return nil, &MissingChunksError{
			Err:      errs.ErrChunkChecksum.WithDetail("缺少 %d 个分片", len(missing)),
			UploadID: meta.UploadID,
			Missing:  missing,
		}
	}

	// 目标目录的可写性在合并时重新校验：init 之后白名单可能已被收紧
	dir, err := s.guard.Resolve(meta.Dir, pathguard.ModeWrite)
	if err != nil {
		return nil, err
	}
	target := filepath.Join(dir, meta.Filename)
	if _, err := s.guard.Resolve(target, pathguard.ModeWrite); err != nil {
		return nil, err
	}

	// 改名成功者独占合并，避免并发 complete 把同一批分片合两次
	lock := filepath.Join(sessionDir, metaFileName+".merging")
	if err := os.Rename(filepath.Join(sessionDir, metaFileName), lock); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errs.ErrFileNotFound.WithDetail("上传会话 %s 不存在或正在合并", req.UploadID)
		}
		return nil, wrapFSError(err)
	}

	wantHash := req.Hash
	if wantHash == "" {
		wantHash = meta.Hash
	}
	entry, sum, err := s.mergeParts(sessionDir, dir, target, meta, wantHash)
	if err != nil {
		// 合并失败时把锁还原，前端可修正后重试而不必重传全部分片
		_ = os.Rename(lock, filepath.Join(sessionDir, metaFileName))
		return nil, err
	}
	_ = os.RemoveAll(sessionDir)

	resp := &model.FileUploadCompleteResponse{
		Entry: *entry, Path: entry.Path, Size: entry.Size,
		DurationMs: time.Since(started).Milliseconds(),
	}
	if wantHash != "" {
		resp.Hash = sum
	}
	return resp, nil
}

// AbortUpload 放弃会话并清理分片。会话已不存在时按幂等处理，直接返回成功。
func (s *Service) AbortUpload(uploadID string) error {
	if err := validUploadID(uploadID); err != nil {
		return err
	}
	root, err := s.uploadRoot()
	if err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Join(root, uploadID)); err != nil {
		return wrapFSError(err)
	}
	return nil
}

// mergeParts 把分片顺序写入目标目录的临时文件，再按冲突策略改名落地。
// 临时文件与目标同目录，保证 rename 原子且不跨设备。
func (s *Service) mergeParts(sessionDir, dir, target string, meta *uploadMeta, wantHash string) (*model.FileEntry, string, error) {
	tmp, err := os.CreateTemp(dir, ".upload-*")
	if err != nil {
		return nil, "", wrapFSError(err)
	}
	tmpName := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}

	digest := sha256.New()
	var dst io.Writer = tmp
	if wantHash != "" {
		dst = io.MultiWriter(tmp, digest)
	}
	var total int64
	for i := 0; i < meta.TotalChunks; i++ {
		part, err := os.Open(partPath(sessionDir, i))
		if err != nil {
			cleanup()
			return nil, "", wrapFSError(err)
		}
		n, err := io.Copy(dst, part)
		_ = part.Close()
		if err != nil {
			cleanup()
			return nil, "", wrapFSError(err)
		}
		total += n
	}
	if total != meta.Size {
		cleanup()
		return nil, "", errs.ErrChunkChecksum.WithDetail("合并后大小 %d，声明 %d", total, meta.Size)
	}

	sum := "sha256:" + hex.EncodeToString(digest.Sum(nil))
	if wantHash != "" && !sameHash(wantHash, sum) {
		cleanup()
		return nil, "", errs.ErrChunkChecksum.WithDetail("整文件哈希不匹配")
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return nil, "", wrapFSError(err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return nil, "", wrapFSError(err)
	}
	// CreateTemp 建出的是 0600，改为常规文件权限，便于站点进程读取
	if err := os.Chmod(tmpName, 0o644); err != nil {
		_ = os.Remove(tmpName)
		return nil, "", wrapFSError(err)
	}

	final, err := s.applyConflict(target, meta.Conflict)
	if err != nil {
		_ = os.Remove(tmpName)
		return nil, "", err
	}
	if err := os.Rename(tmpName, final); err != nil {
		_ = os.Remove(tmpName)
		return nil, "", wrapFSError(err)
	}
	entry, err := s.entryOf(final)
	if err != nil {
		return nil, "", err
	}
	return entry, sum, nil
}

// uploadRoot 返回会话根目录。位于服务端自管的临时目录，权限 0700。
func (s *Service) uploadRoot() (string, error) {
	root := s.cfg.UploadTempDir
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", wrapFSError(err)
	}
	return root, nil
}

// loadSession 校验 uploadId 并读出会话元数据，过期会话按 400007 拒绝并清理。
func (s *Service) loadSession(uploadID string) (string, *uploadMeta, error) {
	if err := validUploadID(uploadID); err != nil {
		return "", nil, err
	}
	root, err := s.uploadRoot()
	if err != nil {
		return "", nil, err
	}
	sessionDir := filepath.Join(root, uploadID)
	meta, err := readMeta(sessionDir)
	if err != nil {
		return "", nil, err
	}
	if meta.expired(time.Now()) {
		_ = os.RemoveAll(sessionDir)
		return "", nil, errs.ErrUploadExpired.WithDetail("会话 %s 超过 %s", uploadID, uploadTTL)
	}
	return sessionDir, meta, nil
}

// findResumable 找出参数完全一致且未过期的会话，用于续传复用。
func (s *Service) findResumable(root, dir, name string, size, chunkSize int64, conflict string) (*uploadMeta, bool) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, false
	}
	now := time.Now()
	for _, de := range entries {
		if !de.IsDir() {
			continue
		}
		meta, err := readMeta(filepath.Join(root, de.Name()))
		if err != nil {
			continue
		}
		if meta.Dir == dir && meta.Filename == name && meta.Size == size &&
			meta.ChunkSize == chunkSize && meta.Conflict == conflict && !meta.expired(now) {
			return meta, true
		}
	}
	return nil, false
}

// gcUploads 惰性清理过期会话：只在 init 时顺手做一遍，不额外起后台协程。
func (s *Service) gcUploads(root string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	now := time.Now()
	for _, de := range entries {
		if !de.IsDir() {
			continue
		}
		sessionDir := filepath.Join(root, de.Name())
		meta, err := readMeta(sessionDir)
		if err != nil {
			// meta 不可读的残留目录按创建时间兜底清理
			if fi, statErr := os.Stat(sessionDir); statErr == nil && now.Sub(fi.ModTime()) > uploadTTL {
				_ = os.RemoveAll(sessionDir)
			}
			continue
		}
		if meta.expired(now) {
			_ = os.RemoveAll(sessionDir)
		}
	}
}

func writeMeta(sessionDir string, meta *uploadMeta) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return errs.Wrap(err, errs.CodeInternal, "上传会话序列化失败")
	}
	if err := os.WriteFile(filepath.Join(sessionDir, metaFileName), data, 0o600); err != nil {
		return wrapFSError(err)
	}
	return nil
}

func readMeta(sessionDir string) (*uploadMeta, error) {
	data, err := os.ReadFile(filepath.Join(sessionDir, metaFileName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errs.ErrFileNotFound.WithDetail("上传会话不存在")
		}
		return nil, wrapFSError(err)
	}
	var meta uploadMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "上传会话已损坏")
	}
	if meta.TotalChunks <= 0 || meta.ChunkSize <= 0 {
		return nil, errs.Newf(errs.CodeInternal, "上传会话已损坏")
	}
	return &meta, nil
}

// uploadedChunks 扫描会话目录，返回已到位的分片序号（升序）。
func uploadedChunks(sessionDir string, total int) ([]int, error) {
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		return nil, wrapFSError(err)
	}
	out := make([]int, 0, len(entries))
	for _, de := range entries {
		name := de.Name()
		if de.IsDir() || !strings.HasSuffix(name, partSuffix) {
			continue
		}
		idx, err := strconv.Atoi(strings.TrimSuffix(name, partSuffix))
		if err != nil || idx < 0 || idx >= total {
			continue
		}
		out = append(out, idx)
	}
	sort.Ints(out)
	return out, nil
}

func missingOf(uploaded []int, total int) []int {
	have := make(map[int]struct{}, len(uploaded))
	for _, i := range uploaded {
		have[i] = struct{}{}
	}
	missing := make([]int, 0, total-len(uploaded))
	for i := 0; i < total; i++ {
		if _, ok := have[i]; !ok {
			missing = append(missing, i)
		}
	}
	return missing
}

func partPath(sessionDir string, index int) string {
	return filepath.Join(sessionDir, strconv.Itoa(index)+partSuffix)
}

func clampChunkSize(size int64) int64 {
	switch {
	case size <= 0:
		return defaultChunkSize
	case size < minChunkSize:
		return minChunkSize
	case size > maxChunkSize:
		return maxChunkSize
	default:
		return size
	}
}

// newUploadID 生成不可猜测的会话标识，形如 up_<24 hex>。
func newUploadID() (string, error) {
	buf := make([]byte, uploadIDBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", errs.Wrap(err, errs.CodeInternal, "生成上传标识失败")
	}
	return "up_" + hex.EncodeToString(buf), nil
}

// validUploadID 只接受自己签发的格式，杜绝借 uploadId 做路径穿越。
func validUploadID(id string) error {
	if len(id) != len("up_")+uploadIDBytes*2 || !strings.HasPrefix(id, "up_") {
		return errs.Newf(errs.CodeInvalidParam, "uploadId 不合法")
	}
	if _, err := hex.DecodeString(strings.TrimPrefix(id, "up_")); err != nil {
		return errs.Newf(errs.CodeInvalidParam, "uploadId 不合法")
	}
	return nil
}

// checksum 按客户端声明的算法边写边算。
type checksum struct {
	hash hash.Hash
	want string
}

func (c *checksum) matches() bool {
	return hex.EncodeToString(c.hash.Sum(nil)) == c.want
}

// newChecksum 解析 md5:<hex> 或 sha256:<hex>；为空表示不校验。
func newChecksum(spec string) (*checksum, error) {
	if spec == "" {
		return nil, nil
	}
	algo, want, ok := strings.Cut(strings.ToLower(strings.TrimSpace(spec)), ":")
	if !ok {
		return nil, errs.Newf(errs.CodeInvalidParam, "checksum 需形如 md5:<hex> 或 sha256:<hex>")
	}
	if _, err := hex.DecodeString(want); err != nil {
		return nil, errs.Newf(errs.CodeInvalidParam, "checksum 不是合法的十六进制串")
	}
	switch algo {
	case "md5":
		return &checksum{hash: md5.New(), want: want}, nil
	case "sha256":
		return &checksum{hash: sha256.New(), want: want}, nil
	default:
		return nil, errs.Newf(errs.CodeInvalidParam, "不支持的校验算法 %s", algo)
	}
}

// sameHash 比较两个 sha256 声明，允许对方省略算法前缀。
func sameHash(want, got string) bool {
	w := strings.ToLower(strings.TrimSpace(want))
	g := strings.ToLower(got)
	if !strings.Contains(w, ":") {
		w = "sha256:" + w
	}
	return w == g
}

// sameFileHash 判断目标文件是否已是同一份内容，用于秒传。
// 只在大小一致时才读文件算哈希，避免为不同文件白读一遍。
func sameFileHash(target string, size int64, want string) (bool, error) {
	fi, err := os.Lstat(target)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, wrapFSError(err)
	}
	if !fi.Mode().IsRegular() || fi.Size() != size {
		return false, nil
	}
	f, err := os.Open(target)
	if err != nil {
		return false, wrapFSError(err)
	}
	defer func() { _ = f.Close() }()

	digest := sha256.New()
	if _, err := io.Copy(digest, f); err != nil {
		return false, wrapFSError(err)
	}
	return sameHash(want, "sha256:"+hex.EncodeToString(digest.Sum(nil))), nil
}
