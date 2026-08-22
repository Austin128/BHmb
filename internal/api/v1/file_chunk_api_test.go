package v1_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/novapanel/novapanel/internal/model"
	"github.com/novapanel/novapanel/internal/pkg/errs"
)

// chunkReq 组装 POST /file/upload/chunk 的 multipart 请求。
func chunkReq(t *testing.T, token, uploadID string, index int, data []byte, checksum string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	require.NoError(t, mw.WriteField("uploadId", uploadID))
	require.NoError(t, mw.WriteField("index", strconv.Itoa(index)))
	if checksum != "" {
		require.NoError(t, mw.WriteField("checksum", checksum))
	}
	part, err := mw.CreateFormFile("chunk", "blob")
	require.NoError(t, err)
	_, err = part.Write(data)
	require.NoError(t, err)
	require.NoError(t, mw.Close())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/file/upload/chunk", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return authed(req, token)
}

// TestFileChunkUploadFlow 跑通 init → chunk → complete，并覆盖缺片与秒传。
func TestFileChunkUploadFlow(t *testing.T) {
	a := newApp(t)
	token, _ := a.login(t)

	payload := bytes.Repeat([]byte("np"), 96*1024) // 192 KiB，按 64 KiB 切成 3 片
	sum := sha256.Sum256(payload)
	hash := "sha256:" + hex.EncodeToString(sum[:])
	chunkSize := 64 * 1024

	rec, env := a.do(t, authed(jsonReq(t, http.MethodPost, "/api/v1/file/upload/init",
		`{"path":"`+a.fileRoot+`","filename":"pkg.bin","size":`+strconv.Itoa(len(payload))+
			`,"chunkSize":`+strconv.Itoa(chunkSize)+`,"hash":"`+hash+`"}`), token))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Zero(t, env.Code)

	var init model.FileUploadInitResponse
	require.NoError(t, json.Unmarshal(env.Data, &init))
	require.Equal(t, 3, init.TotalChunks)
	require.NotEmpty(t, init.UploadID)
	assert.False(t, init.QuickUpload)
	assert.NotEmpty(t, init.ExpireAt, "expireAt 按 RFC3339 返回")

	// 先只传首尾两片，留出缺片
	_, env = a.do(t, chunkReq(t, token, init.UploadID, 0, payload[:chunkSize], ""))
	require.Zero(t, env.Code)
	_, env = a.do(t, chunkReq(t, token, init.UploadID, 2, payload[2*chunkSize:], ""))
	require.Zero(t, env.Code)

	rec, env = a.do(t, authed(httptest.NewRequest(http.MethodGet,
		"/api/v1/file/upload/status?uploadId="+init.UploadID, nil), token))
	require.Equal(t, http.StatusOK, rec.Code)
	var status model.FileUploadStatusResponse
	require.NoError(t, json.Unmarshal(env.Data, &status))
	assert.Equal(t, []int{0, 2}, status.UploadedChunks)
	assert.Equal(t, []int{1}, status.MissingChunks)

	// 缺片时 complete 返回 400006，并在 data.missing 指明待重传分片
	rec, env = a.do(t, authed(jsonReq(t, http.MethodPost, "/api/v1/file/upload/complete",
		`{"uploadId":"`+init.UploadID+`"}`), token))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, errs.CodeChunkChecksum, env.Code)
	var miss model.FileUploadMissingData
	require.NoError(t, json.Unmarshal(env.Data, &miss))
	assert.Equal(t, []int{1}, miss.Missing)

	// 校验和不匹配的分片必须被拒
	rec, env = a.do(t, chunkReq(t, token, init.UploadID, 1,
		payload[chunkSize:2*chunkSize], "sha256:"+hex.EncodeToString(make([]byte, 32))))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, errs.CodeChunkChecksum, env.Code)

	// 带正确校验和补齐缺片后合并
	good := sha256.Sum256(payload[chunkSize : 2*chunkSize])
	_, env = a.do(t, chunkReq(t, token, init.UploadID, 1,
		payload[chunkSize:2*chunkSize], "sha256:"+hex.EncodeToString(good[:])))
	require.Zero(t, env.Code)

	rec, env = a.do(t, authed(jsonReq(t, http.MethodPost, "/api/v1/file/upload/complete",
		`{"uploadId":"`+init.UploadID+`","hash":"`+hash+`"}`), token))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Zero(t, env.Code)
	var done model.FileUploadCompleteResponse
	require.NoError(t, json.Unmarshal(env.Data, &done))
	assert.Equal(t, filepath.Join(a.fileRoot, "pkg.bin"), done.Path)
	assert.EqualValues(t, len(payload), done.Size)
	assert.Equal(t, hash, done.Hash)

	got, err := os.ReadFile(done.Path)
	require.NoError(t, err)
	assert.True(t, bytes.Equal(payload, got), "合并结果应与原文件一致")

	// 会话已消费，状态查询应报 404
	rec, env = a.do(t, authed(httptest.NewRequest(http.MethodGet,
		"/api/v1/file/upload/status?uploadId="+init.UploadID, nil), token))
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, errs.CodeFileNotFound, env.Code)

	// 同名同哈希再 init 命中秒传，无需再传分片
	_, env = a.do(t, authed(jsonReq(t, http.MethodPost, "/api/v1/file/upload/init",
		`{"path":"`+a.fileRoot+`","filename":"pkg.bin","size":`+strconv.Itoa(len(payload))+
			`,"hash":"`+hash+`"}`), token))
	require.Zero(t, env.Code)
	var quick model.FileUploadInitResponse
	require.NoError(t, json.Unmarshal(env.Data, &quick))
	assert.True(t, quick.QuickUpload)
	require.NotNil(t, quick.Entry)
	assert.Equal(t, "pkg.bin", quick.Entry.Name)
}

// TestFileChunkUploadAbortAndExpiry 覆盖放弃会话与非法 uploadId。
func TestFileChunkUploadAbortAndExpiry(t *testing.T) {
	a := newApp(t)
	token, _ := a.login(t)

	_, env := a.do(t, authed(jsonReq(t, http.MethodPost, "/api/v1/file/upload/init",
		`{"path":"`+a.fileRoot+`","filename":"tmp.bin","size":16}`), token))
	require.Zero(t, env.Code)
	var init model.FileUploadInitResponse
	require.NoError(t, json.Unmarshal(env.Data, &init))

	rec, env := a.do(t, authed(httptest.NewRequest(http.MethodDelete,
		"/api/v1/file/upload/"+init.UploadID, nil), token))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Zero(t, env.Code)

	// 放弃后分片会话即失效
	rec, env = a.do(t, chunkReq(t, token, init.UploadID, 0, []byte("0123456789abcdef"), ""))
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, errs.CodeFileNotFound, env.Code)

	// 伪造的 uploadId 不得穿越到会话目录之外
	rec, env = a.do(t, authed(httptest.NewRequest(http.MethodGet,
		"/api/v1/file/upload/status?uploadId=up_..%2F..%2Fetc", nil), token))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, errs.CodeInvalidParam, env.Code)
}

// TestFileChunkUploadRequiresCreatePermission 确认分片链路统一按 file:file:create 鉴权。
func TestFileChunkUploadRequiresCreatePermission(t *testing.T) {
	a := newApp(t)
	token := a.readonlyToken(t)

	for _, tc := range []struct {
		name string
		req  *http.Request
	}{
		{"init", jsonReq(t, http.MethodPost, "/api/v1/file/upload/init",
			`{"path":"`+a.fileRoot+`","filename":"x.bin","size":8}`)},
		{"complete", jsonReq(t, http.MethodPost, "/api/v1/file/upload/complete",
			`{"uploadId":"up_000000000000000000000000"}`)},
		{"status", httptest.NewRequest(http.MethodGet,
			"/api/v1/file/upload/status?uploadId=up_000000000000000000000000", nil)},
		{"abort", httptest.NewRequest(http.MethodDelete,
			"/api/v1/file/upload/up_000000000000000000000000", nil)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec, env := a.do(t, authed(tc.req, token))
			assert.Equal(t, http.StatusForbidden, rec.Code)
			assert.Equal(t, errs.CodeForbidden, env.Code)
		})
	}

	rec, env := a.do(t, chunkReq(t, token, "up_000000000000000000000000", 0, []byte("x"), ""))
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, errs.CodeForbidden, env.Code)

	entries, err := os.ReadDir(a.fileRoot)
	require.NoError(t, err)
	assert.Empty(t, entries, "被拒的分片上传不得在目标目录留下痕迹")
}
