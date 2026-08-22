package handler

import (
	"errors"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/novapanel/novapanel/internal/api/v1/response"
	"github.com/novapanel/novapanel/internal/model"
	"github.com/novapanel/novapanel/internal/pkg/errs"
	filesvc "github.com/novapanel/novapanel/internal/service/file"
)

// File 为文件管理处理器。路径校验全部由 service 内的 pathguard 完成，
// 处理器只做参数绑定与响应封装。
type File struct {
	svc *filesvc.Service
}

// NewFile 构造文件处理器。
func NewFile(svc *filesvc.Service) *File {
	return &File{svc: svc}
}

// Roots 处理 GET /api/v1/file/roots，返回可访问的根目录，供前端目录树初始化。
func (h *File) Roots(c *gin.Context) {
	response.OK(c, gin.H{"allowRoots": h.svc.AllowRoots()})
}

// List 处理 GET /api/v1/file/list。
func (h *File) List(c *gin.Context) {
	var req model.FileListRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		response.Fail(c, bindError(err))
		return
	}
	res, err := h.svc.List(req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// Stat 处理 GET /api/v1/file/stat。
func (h *File) Stat(c *gin.Context) {
	var req model.FileStatRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		response.Fail(c, bindError(err))
		return
	}
	res, err := h.svc.Stat(req.Path)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// Content 处理 GET /api/v1/file/content，返回文本内容与 ETag。
func (h *File) Content(c *gin.Context) {
	var req model.FileContentRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		response.Fail(c, bindError(err))
		return
	}
	res, err := h.svc.ReadContent(req.Path)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// Save 处理 PUT /api/v1/file/content。
func (h *File) Save(c *gin.Context) {
	var req model.FileSaveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, bindError(err))
		return
	}
	// 允许用标准 If-Match 头代替请求体里的 etag，便于通用 HTTP 客户端调用
	if req.ETag == "" {
		req.ETag = c.GetHeader("If-Match")
	}
	res, err := h.svc.SaveContent(req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// CreateDir 处理 POST /api/v1/file/dir。
func (h *File) CreateDir(c *gin.Context) {
	h.create(c, h.svc.CreateDir)
}

// CreateFile 处理 POST /api/v1/file/file。
func (h *File) CreateFile(c *gin.Context) {
	h.create(c, h.svc.CreateFile)
}

func (h *File) create(c *gin.Context, fn func(model.FileCreateRequest) (*model.FileEntry, error)) {
	var req model.FileCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, bindError(err))
		return
	}
	res, err := fn(req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// Rename 处理 POST /api/v1/file/rename。
func (h *File) Rename(c *gin.Context) {
	var req model.FileRenameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, bindError(err))
		return
	}
	res, err := h.svc.Rename(req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// Move 处理 POST /api/v1/file/move。
func (h *File) Move(c *gin.Context) {
	h.transfer(c, h.svc.Move)
}

// Copy 处理 POST /api/v1/file/copy。
func (h *File) Copy(c *gin.Context) {
	h.transfer(c, h.svc.Copy)
}

func (h *File) transfer(c *gin.Context, fn func(model.FileTransferRequest) (*model.FileBatchResponse, error)) {
	var req model.FileTransferRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, bindError(err))
		return
	}
	res, err := fn(req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// Delete 处理 DELETE /api/v1/file/items。批量操作按逐项结果返回，
// 整体仍是 200，由前端根据 failed 展示部分失败。
func (h *File) Delete(c *gin.Context) {
	var req model.FileDeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, bindError(err))
		return
	}
	res, err := h.svc.Delete(req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// Permission 处理 PUT /api/v1/file/permission。
func (h *File) Permission(c *gin.Context) {
	var req model.FilePermissionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, bindError(err))
		return
	}
	res, err := h.svc.Permission(req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// Compress 处理 POST /api/v1/file/compress。
func (h *File) Compress(c *gin.Context) {
	var req model.FileCompressRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, bindError(err))
		return
	}
	res, err := h.svc.Compress(req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// Decompress 处理 POST /api/v1/file/decompress。
func (h *File) Decompress(c *gin.Context) {
	var req model.FileDecompressRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, bindError(err))
		return
	}
	res, err := h.svc.Decompress(req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// Search 处理 GET /api/v1/file/search。
func (h *File) Search(c *gin.Context) {
	var req model.FileSearchRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		response.Fail(c, bindError(err))
		return
	}
	res, err := h.svc.Search(req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// Du 处理 GET /api/v1/file/du。
func (h *File) Du(c *gin.Context) {
	var req model.FileDuRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		response.Fail(c, bindError(err))
		return
	}
	res, err := h.svc.Du(req.Path)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// Upload 处理 POST /api/v1/file/upload（multipart/form-data）。
// 字段：path 为目标目录，file 为文件，conflict 为冲突策略。
func (h *File) Upload(c *gin.Context) {
	dir := strings.TrimSpace(c.PostForm("path"))
	if dir == "" {
		response.Fail(c, errs.Newf(errs.CodeInvalidParam, "缺少目标目录"))
		return
	}
	fh, err := c.FormFile("file")
	if err != nil {
		response.Fail(c, errs.Newf(errs.CodeInvalidParam, "缺少上传文件"))
		return
	}
	src, err := fh.Open()
	if err != nil {
		response.Fail(c, errs.Newf(errs.CodeInvalidParam, "上传文件无法读取"))
		return
	}
	defer func() { _ = src.Close() }()

	res, err := h.svc.Upload(filesvc.UploadRequest{
		Dir:      dir,
		Name:     fh.Filename,
		Size:     fh.Size,
		Conflict: c.PostForm("conflict"),
		Src:      src,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// UploadInit 处理 POST /api/v1/file/upload/init，建立分片上传会话。
func (h *File) UploadInit(c *gin.Context) {
	var req model.FileUploadInitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, bindError(err))
		return
	}
	res, err := h.svc.InitUpload(req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// UploadChunk 处理 POST /api/v1/file/upload/chunk（multipart/form-data）。
// 字段：uploadId、index、checksum（可选，md5:<hex> 或 sha256:<hex>）、chunk。
// gin 解析 multipart 时超过内存阈值的部分会落到系统临时文件，分片本身不驻留内存。
func (h *File) UploadChunk(c *gin.Context) {
	uploadID := strings.TrimSpace(c.PostForm("uploadId"))
	if uploadID == "" {
		response.Fail(c, errs.Newf(errs.CodeInvalidParam, "缺少 uploadId"))
		return
	}
	index, err := strconv.Atoi(strings.TrimSpace(c.PostForm("index")))
	if err != nil {
		response.Fail(c, errs.Newf(errs.CodeInvalidParam, "index 必须是整数"))
		return
	}
	fh, err := c.FormFile("chunk")
	if err != nil {
		response.Fail(c, errs.Newf(errs.CodeInvalidParam, "缺少分片数据"))
		return
	}
	src, err := fh.Open()
	if err != nil {
		response.Fail(c, errs.Newf(errs.CodeInvalidParam, "分片数据无法读取"))
		return
	}
	defer func() { _ = src.Close() }()

	res, err := h.svc.UploadChunk(filesvc.ChunkRequest{
		UploadID: uploadID,
		Index:    index,
		Size:     fh.Size,
		Checksum: strings.TrimSpace(c.PostForm("checksum")),
		Src:      src,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// UploadStatus 处理 GET /api/v1/file/upload/status，供断点续传对齐进度。
func (h *File) UploadStatus(c *gin.Context) {
	var req model.FileUploadStatusRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		response.Fail(c, bindError(err))
		return
	}
	res, err := h.svc.UploadStatus(req.UploadID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// UploadComplete 处理 POST /api/v1/file/upload/complete，合并分片并落地。
// 缺片时按 400006 返回，并在 data.missing 给出待重传的分片序号。
func (h *File) UploadComplete(c *gin.Context) {
	var req model.FileUploadCompleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, bindError(err))
		return
	}
	res, err := h.svc.CompleteUpload(req)
	if err != nil {
		var missing *filesvc.MissingChunksError
		if errors.As(err, &missing) {
			response.FailWith(c, errs.Code(err), errs.Message(err), model.FileUploadMissingData{
				UploadID: missing.UploadID, Missing: missing.Missing,
			})
			return
		}
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// UploadAbort 处理 DELETE /api/v1/file/upload/:uploadId，放弃会话并清理分片。
func (h *File) UploadAbort(c *gin.Context) {
	if err := h.svc.AbortUpload(strings.TrimSpace(c.Param("uploadId"))); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{})
}

// Download 处理 GET /api/v1/file/download。
// 这是唯一不走统一信封的接口（docs/05 5.2 例外），由 ServeContent 直接输出，
// 因此天然支持 Range 断点续传与条件请求。
func (h *File) Download(c *gin.Context) {
	var req model.FileStatRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		response.Fail(c, bindError(err))
		return
	}
	f, fi, err := h.svc.OpenForDownload(req.Path)
	if err != nil {
		response.Fail(c, err)
		return
	}
	defer func() { _ = f.Close() }()

	name := filepath.Base(fi.Name())
	// 同时给出 ASCII 回退与 RFC 5987 编码名，兼顾老客户端与中文文件名
	c.Header("Content-Disposition", "attachment; filename=\""+sanitizeASCIIName(name)+"\"; filename*=UTF-8''"+url.PathEscape(name))
	c.Header("Content-Type", "application/octet-stream")
	c.Header("X-Content-Type-Options", "nosniff")
	http.ServeContent(c.Writer, c.Request, name, fi.ModTime(), f)
}

// sanitizeASCIIName 生成 Content-Disposition 的 ASCII 回退名，
// 去掉引号、反斜杠与控制字符，避免头部注入。
func sanitizeASCIIName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r < 0x20 || r == 0x7F:
			continue
		case r == '"' || r == '\\':
			b.WriteByte('_')
		case r > 0x7F:
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}
	out := b.String()
	if out == "" {
		return "download"
	}
	return out
}
