package model

// 本文件为文件管理链路的请求/响应 DTO，字段与 docs/05 5.7.4、5.8.4 及
// docs/08 8.2.1～8.2.2 对应。所有 path 均为绝对路径，由 pathguard 统一校验。

// 排序字段与方向的合法取值。
const (
	FileSortName  = "name"
	FileSortSize  = "size"
	FileSortMtime = "mtime"
	FileSortType  = "type"

	FileOrderAsc  = "asc"
	FileOrderDesc = "desc"
)

// 冲突策略：拒绝、覆盖、自动改名。
const (
	FileConflictReject    = "reject"
	FileConflictOverwrite = "overwrite"
	FileConflictRename    = "rename"
)

// 支持的压缩格式。
const (
	ArchiveZip    = "zip"
	ArchiveTarGz  = "tar.gz"
	ArchiveTar    = "tar"
	ArchiveTarBz2 = "tar.bz2"
	ArchiveTarXz  = "tar.xz"
)

// FileEntry 为单个文件或目录的元数据（docs/08 8.2.2）。
type FileEntry struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	IsDir     bool   `json:"isDir"`
	Size      int64  `json:"size"`
	Mode      string `json:"mode"`
	ModeOctal string `json:"modeOctal"`
	UID       int    `json:"uid"`
	GID       int    `json:"gid"`
	Owner     string `json:"owner"`
	Group     string `json:"group"`
	// Mtime 为 Unix 毫秒。
	Mtime int64 `json:"mtime"`
	// LinkTarget 仅符号链接非空。
	LinkTarget string `json:"linkTarget,omitempty"`
	LinkBroken bool   `json:"linkBroken,omitempty"`
	IsSymlink  bool   `json:"isSymlink"`
	Mime       string `json:"mime"`
	Ext        string `json:"ext"`
	Icon       string `json:"icon"`
	Writable   bool   `json:"writable"`
}

// FileListRequest 为 GET /file/list 的查询参数。
type FileListRequest struct {
	Path       string `form:"path" binding:"required,max=4096"`
	PageSize   int    `form:"pageSize" binding:"omitempty,min=1,max=2000"`
	Cursor     string `form:"cursor" binding:"omitempty,max=4096"`
	Sort       string `form:"sort" binding:"omitempty,oneof=name size mtime type"`
	Order      string `form:"order" binding:"omitempty,oneof=asc desc"`
	Keyword    string `form:"keyword" binding:"omitempty,max=255"`
	ShowHidden bool   `form:"showHidden"`
	OnlyDir    bool   `form:"onlyDir"`
}

// FileListResponse 为目录列表结果。小目录 paged=false 一次返回全部，
// 大目录降级为游标分页（docs/08 8.2.1）。
type FileListResponse struct {
	Path string `json:"path"`
	// Parent 为上级目录，已在白名单内时非空，供前端「返回上一级」使用。
	Parent     string      `json:"parent"`
	Items      []FileEntry `json:"items"`
	Total      int64       `json:"total"`
	Paged      bool        `json:"paged"`
	Degraded   bool        `json:"degraded"`
	NextCursor string      `json:"nextCursor,omitempty"`
	// AllowRoots 让前端知道可访问范围，用于目录树根节点渲染。
	AllowRoots []string `json:"allowRoots,omitempty"`
}

// FileStatRequest 为 GET /file/stat 的查询参数。
type FileStatRequest struct {
	Path string `form:"path" binding:"required,max=4096"`
}

// FileContentRequest 为 GET /file/content 的查询参数。
type FileContentRequest struct {
	Path string `form:"path" binding:"required,max=4096"`
}

// FileContentResponse 为文本读取结果，ETag 用于保存时的乐观锁。
type FileContentResponse struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	Size      int64  `json:"size"`
	ETag      string `json:"etag"`
	Mime      string `json:"mime"`
	Truncated bool   `json:"truncated"`
}

// FileSaveRequest 为 PUT /file/content 的请求体。
// ETag 为空表示新建文件；非空时与当前内容不一致返回 400011。
type FileSaveRequest struct {
	Path         string `json:"path" binding:"required,max=4096"`
	Content      string `json:"content"`
	ETag         string `json:"etag" binding:"omitempty,max=128"`
	CreateBackup bool   `json:"createBackup"`
}

// FileSaveResponse 返回保存后的新 ETag。
type FileSaveResponse struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
	ETag string `json:"etag"`
}

// FileCreateRequest 为 POST /file/dir 与 POST /file/file 的请求体。
type FileCreateRequest struct {
	Path string `json:"path" binding:"required,max=4096"`
	// Mode 为八进制权限串，如 0755；为空时目录用 0755、文件用 0644。
	Mode string `json:"mode" binding:"omitempty,max=5"`
}

// FileRenameRequest 为 POST /file/rename 的请求体。
type FileRenameRequest struct {
	Path    string `json:"path" binding:"required,max=4096"`
	NewName string `json:"newName" binding:"required,max=255"`
}

// FileTransferRequest 为 POST /file/move 与 POST /file/copy 的请求体。
type FileTransferRequest struct {
	Paths    []string `json:"paths" binding:"required,min=1,max=500,dive,required,max=4096"`
	Dest     string   `json:"dest" binding:"required,max=4096"`
	Conflict string   `json:"conflict" binding:"omitempty,oneof=reject overwrite rename"`
}

// FileDeleteRequest 为 DELETE /file/items 的请求体。
type FileDeleteRequest struct {
	Paths []string `json:"paths" binding:"required,min=1,max=500,dive,required,max=4096"`
}

// FilePermissionRequest 为 PUT /file/permission 的请求体。
type FilePermissionRequest struct {
	Path      string `json:"path" binding:"required,max=4096"`
	Mode      string `json:"mode" binding:"omitempty,max=5"`
	Owner     string `json:"owner" binding:"omitempty,max=64"`
	Group     string `json:"group" binding:"omitempty,max=64"`
	Recursive bool   `json:"recursive"`
}

// FileCompressRequest 为 POST /file/compress 的请求体。
type FileCompressRequest struct {
	Paths  []string `json:"paths" binding:"required,min=1,max=500,dive,required,max=4096"`
	Dest   string   `json:"dest" binding:"required,max=4096"`
	Format string   `json:"format" binding:"omitempty,oneof=zip tar.gz tar"`
}

// FileDecompressRequest 为 POST /file/decompress 的请求体。
type FileDecompressRequest struct {
	Path string `json:"path" binding:"required,max=4096"`
	Dest string `json:"dest" binding:"required,max=4096"`
}

// FileSearchRequest 为 GET /file/search 的查询参数。
type FileSearchRequest struct {
	Path     string `form:"path" binding:"required,max=4096"`
	Keyword  string `form:"keyword" binding:"required,max=255"`
	MaxDepth int    `form:"maxDepth" binding:"omitempty,min=1,max=20"`
	Limit    int    `form:"limit" binding:"omitempty,min=1,max=1000"`
}

// FileSearchResponse 为搜索结果，Truncated 表示命中数超过上限被截断。
type FileSearchResponse struct {
	Items     []FileEntry `json:"items"`
	Total     int64       `json:"total"`
	Truncated bool        `json:"truncated"`
}

// FileDuRequest 为 GET /file/du 的查询参数。
type FileDuRequest struct {
	Path string `form:"path" binding:"required,max=4096"`
}

// FileDuResponse 为目录占用统计结果。
type FileDuResponse struct {
	Path  string `json:"path"`
	Size  int64  `json:"size"`
	Files int64  `json:"files"`
	Dirs  int64  `json:"dirs"`
	// Truncated 为真表示遍历条目数触达上限，结果为下限估算值。
	Truncated bool `json:"truncated"`
}

// FileUploadInitRequest 为 POST /file/upload/init 的请求体。
// Hash 为整文件 sha256（形如 sha256:<hex>），命中同名同哈希文件时走秒传。
type FileUploadInitRequest struct {
	Path      string `json:"path" binding:"required,max=4096"`
	Filename  string `json:"filename" binding:"required,max=255"`
	Size      int64  `json:"size" binding:"required,min=1"`
	ChunkSize int64  `json:"chunkSize" binding:"omitempty,min=1"`
	Hash      string `json:"hash" binding:"omitempty,max=128"`
	Conflict  string `json:"conflict" binding:"omitempty,oneof=reject overwrite rename"`
}

// FileUploadInitResponse 返回会话与续传信息。QuickUpload 为真时无需再传分片。
type FileUploadInitResponse struct {
	UploadID       string `json:"uploadId"`
	ChunkSize      int64  `json:"chunkSize"`
	TotalChunks    int    `json:"totalChunks"`
	UploadedChunks []int  `json:"uploadedChunks"`
	// ExpireAt 为 RFC3339 时间（docs/05 5.2.5），会话过期后需重新 init。
	ExpireAt    string     `json:"expireAt"`
	QuickUpload bool       `json:"quickUpload"`
	Entry       *FileEntry `json:"entry,omitempty"`
}

// FileUploadChunkResponse 为单个分片落盘后的进度回执。
type FileUploadChunkResponse struct {
	UploadID      string `json:"uploadId"`
	Index         int    `json:"index"`
	Received      int64  `json:"received"`
	UploadedCount int    `json:"uploadedCount"`
	TotalChunks   int    `json:"totalChunks"`
}

// FileUploadStatusRequest 为 GET /file/upload/status 的查询参数。
type FileUploadStatusRequest struct {
	UploadID string `form:"uploadId" binding:"required,max=64"`
}

// FileUploadStatusResponse 供断点续传前对齐进度。
type FileUploadStatusResponse struct {
	UploadID       string `json:"uploadId"`
	Path           string `json:"path"`
	Filename       string `json:"filename"`
	Size           int64  `json:"size"`
	ChunkSize      int64  `json:"chunkSize"`
	TotalChunks    int    `json:"totalChunks"`
	UploadedChunks []int  `json:"uploadedChunks"`
	MissingChunks  []int  `json:"missingChunks"`
	ExpireAt       string `json:"expireAt"`
}

// FileUploadCompleteRequest 为 POST /file/upload/complete 的请求体。
type FileUploadCompleteRequest struct {
	UploadID string `json:"uploadId" binding:"required,max=64"`
	// Hash 非空时对合并结果做整文件校验，不匹配返回 400006。
	Hash string `json:"hash" binding:"omitempty,max=128"`
}

// FileUploadCompleteResponse 为合并结果。Hash 仅在请求带哈希时回显。
type FileUploadCompleteResponse struct {
	Entry      FileEntry `json:"entry"`
	Path       string    `json:"path"`
	Size       int64     `json:"size"`
	Hash       string    `json:"hash,omitempty"`
	DurationMs int64     `json:"durationMs"`
}

// FileUploadMissingData 为缺片时随 400006 返回的补充数据。
type FileUploadMissingData struct {
	UploadID string `json:"uploadId"`
	Missing  []int  `json:"missing"`
}

// FileOpResult 为批量操作的逐项结果，部分成功时返回。
type FileOpResult struct {
	Path    string `json:"path"`
	OK      bool   `json:"ok"`
	Code    int    `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
	// Dest 在移动、复制、压缩场景下给出最终落地路径（自动改名时可与请求不同）。
	Dest string `json:"dest,omitempty"`
}

// FileBatchResponse 为批量操作汇总。
type FileBatchResponse struct {
	Succeeded int            `json:"succeeded"`
	Failed    int            `json:"failed"`
	Results   []FileOpResult `json:"results"`
}
