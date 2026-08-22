package v1_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/novapanel/novapanel/internal/model"
	"github.com/novapanel/novapanel/internal/pkg/errs"
)

// authed 给请求带上 Bearer 令牌。
func authed(req *http.Request, token string) *http.Request {
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

func fileQuery(path string, extra ...string) string {
	q := url.Values{}
	q.Set("path", path)
	for i := 0; i+1 < len(extra); i += 2 {
		q.Set(extra[i], extra[i+1])
	}
	return "?" + q.Encode()
}

func TestFileListAndStat(t *testing.T) {
	a := newApp(t)
	token, _ := a.login(t)
	require.NoError(t, os.WriteFile(filepath.Join(a.fileRoot, "index.html"), []byte("<h1>hi</h1>"), 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(a.fileRoot, "logs"), 0o755))

	rec, env := a.do(t, authed(httptest.NewRequest(http.MethodGet,
		"/api/v1/file/list"+fileQuery(a.fileRoot), nil), token))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Zero(t, env.Code)

	var list model.FileListResponse
	require.NoError(t, json.Unmarshal(env.Data, &list))
	assert.Equal(t, a.fileRoot, list.Path)
	assert.Equal(t, []string{a.fileRoot}, list.AllowRoots)
	require.Len(t, list.Items, 2)
	assert.Equal(t, "logs", list.Items[0].Name, "目录应排在文件之前")
	assert.True(t, list.Items[0].IsDir)
	assert.Equal(t, "index.html", list.Items[1].Name)
	assert.Equal(t, "text/html", list.Items[1].Mime)
	assert.Equal(t, "0644", list.Items[1].ModeOctal)

	_, env = a.do(t, authed(httptest.NewRequest(http.MethodGet,
		"/api/v1/file/stat"+fileQuery(filepath.Join(a.fileRoot, "index.html")), nil), token))
	require.Zero(t, env.Code)
	var stat model.FileEntry
	require.NoError(t, json.Unmarshal(env.Data, &stat))
	assert.EqualValues(t, 11, stat.Size)
}

// TestFileRejectsPathEscape 确认穿越与黑名单路径在 API 层返回文档约定的错误码与状态码。
func TestFileRejectsPathEscape(t *testing.T) {
	a := newApp(t)
	token, _ := a.login(t)
	require.NoError(t, os.Mkdir(filepath.Join(a.fileRoot, "secret"), 0o755))

	cases := []struct {
		name   string
		path   string
		code   int
		status int
	}{
		{"穿越到白名单外", filepath.Join(a.fileRoot, "../../etc"), errs.CodePathEscape, http.StatusForbidden},
		{"白名单外的绝对路径", "/etc", errs.CodePathEscape, http.StatusForbidden},
		{"黑名单目录", filepath.Join(a.fileRoot, "secret"), errs.CodeProtectedPath, http.StatusForbidden},
		// 相对路径属参数问题（docs/08 TC09 的 ErrNotAbs），不归入路径逃逸
		{"相对路径", "www/logs", errs.CodeInvalidParam, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec, env := a.do(t, authed(httptest.NewRequest(http.MethodGet,
				"/api/v1/file/list"+fileQuery(tc.path), nil), token))
			assert.Equal(t, tc.status, rec.Code)
			assert.Equal(t, tc.code, env.Code)
			// 错误信息不得回显宿主上的解析细节
			assert.NotContains(t, env.Message, "/etc/passwd")
		})
	}
}

func TestFileContentIfMatch(t *testing.T) {
	a := newApp(t)
	token, _ := a.login(t)
	path := filepath.Join(a.fileRoot, "app.conf")
	require.NoError(t, os.WriteFile(path, []byte("port=80\n"), 0o644))

	_, env := a.do(t, authed(httptest.NewRequest(http.MethodGet,
		"/api/v1/file/content"+fileQuery(path), nil), token))
	require.Zero(t, env.Code)
	var content model.FileContentResponse
	require.NoError(t, json.Unmarshal(env.Data, &content))
	require.NotEmpty(t, content.ETag)

	// 用 If-Match 头替代请求体 etag
	req := jsonReq(t, http.MethodPut, "/api/v1/file/content",
		`{"path":"`+path+`","content":"port=8080\n"}`)
	req.Header.Set("If-Match", content.ETag)
	rec, env := a.do(t, authed(req, token))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Zero(t, env.Code)

	// 重放同一个 If-Match 必须冲突，返回 409
	req = jsonReq(t, http.MethodPut, "/api/v1/file/content",
		`{"path":"`+path+`","content":"port=9090\n"}`)
	req.Header.Set("If-Match", content.ETag)
	rec, env = a.do(t, authed(req, token))
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Equal(t, errs.CodeFileChanged, env.Code)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "port=8080\n", string(data))
}

func TestFileUploadAndDownloadRange(t *testing.T) {
	a := newApp(t)
	token, _ := a.login(t)

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	require.NoError(t, mw.WriteField("path", a.fileRoot))
	part, err := mw.CreateFormFile("file", "报表.csv")
	require.NoError(t, err)
	_, err = part.Write([]byte("a,b,c\n1,2,3\n"))
	require.NoError(t, err)
	require.NoError(t, mw.Close())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/file/upload", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec, env := a.do(t, authed(req, token))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Zero(t, env.Code)

	var entry model.FileEntry
	require.NoError(t, json.Unmarshal(env.Data, &entry))
	assert.Equal(t, "报表.csv", entry.Name)
	assert.Equal(t, "0644", entry.ModeOctal)

	// 下载走 ServeContent，不套统一信封
	dl := authed(httptest.NewRequest(http.MethodGet,
		"/api/v1/file/download"+fileQuery(entry.Path), nil), token)
	dl.Header.Set("Range", "bytes=2-4")
	rec = httptest.NewRecorder()
	a.handlerT.ServeHTTP(rec, dl)
	assert.Equal(t, http.StatusPartialContent, rec.Code)
	assert.Equal(t, "b,c", rec.Body.String())
	assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	cd := rec.Header().Get("Content-Disposition")
	assert.Contains(t, cd, "filename*=UTF-8''", "中文名必须给出 RFC 5987 形式")
	assert.NotContains(t, strings.SplitN(cd, "filename*=", 2)[0], "报表", "ASCII 回退名不应含非 ASCII 字符")

	// 同名再上传，默认策略必须拒绝
	body.Reset()
	mw = multipart.NewWriter(&body)
	require.NoError(t, mw.WriteField("path", a.fileRoot))
	part, err = mw.CreateFormFile("file", "报表.csv")
	require.NoError(t, err)
	_, err = part.Write([]byte("x"))
	require.NoError(t, err)
	require.NoError(t, mw.Close())
	req = httptest.NewRequest(http.MethodPost, "/api/v1/file/upload", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec, env = a.do(t, authed(req, token))
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Equal(t, errs.CodeFileExists, env.Code)
}

func TestFileBatchDeleteReportsPerItem(t *testing.T) {
	a := newApp(t)
	token, _ := a.login(t)
	ok := filepath.Join(a.fileRoot, "a.txt")
	require.NoError(t, os.WriteFile(ok, []byte("a"), 0o644))
	missing := filepath.Join(a.fileRoot, "missing.txt")

	rec, env := a.do(t, authed(jsonReq(t, http.MethodDelete, "/api/v1/file/items",
		`{"paths":["`+ok+`","`+missing+`","/etc/passwd"]}`), token))
	require.Equal(t, http.StatusOK, rec.Code, "批量操作整体成功，逐项给出结果")
	require.Zero(t, env.Code)

	var res model.FileBatchResponse
	require.NoError(t, json.Unmarshal(env.Data, &res))
	assert.Equal(t, 1, res.Succeeded)
	assert.Equal(t, 2, res.Failed)
	require.Len(t, res.Results, 3)
	assert.True(t, res.Results[0].OK)
	assert.Equal(t, errs.CodeFileNotFound, res.Results[1].Code)
	assert.Equal(t, errs.CodePathEscape, res.Results[2].Code)

	_, err := os.Stat(ok)
	assert.True(t, os.IsNotExist(err), "已删除的文件不应存在")
}

func TestFileRequiresAuth(t *testing.T) {
	a := newApp(t)
	rec, env := a.do(t, httptest.NewRequest(http.MethodGet,
		"/api/v1/file/list"+fileQuery(a.fileRoot), nil))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, errs.CodeUnauthorized, env.Code)
}

// TestFileEnforcesPermissionPoints 校验文件模块按权限点分组：只读访客能浏览，
// 但改权限（file:permission:update）与删除（file:file:delete）必须被拒。
func TestFileEnforcesPermissionPoints(t *testing.T) {
	a := newApp(t)
	token := a.readonlyToken(t)
	target := filepath.Join(a.fileRoot, "readme.txt")
	require.NoError(t, os.WriteFile(target, []byte("hi"), 0o644))

	rec, env := a.do(t, authed(httptest.NewRequest(http.MethodGet,
		"/api/v1/file/list"+fileQuery(a.fileRoot), nil), token))
	require.Equal(t, http.StatusOK, rec.Code, "只读访客持有 file:file:list")
	require.Zero(t, env.Code)

	rec, env = a.do(t, authed(jsonReq(t, http.MethodPut, "/api/v1/file/permission",
		`{"path":"`+target+`","mode":"0777"}`), token))
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, errs.CodeForbidden, env.Code)

	rec, env = a.do(t, authed(jsonReq(t, http.MethodDelete, "/api/v1/file/items",
		`{"paths":["`+target+`"]}`), token))
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, errs.CodeForbidden, env.Code)

	info, err := os.Stat(target)
	require.NoError(t, err, "被拒的请求不得改动磁盘")
	assert.Equal(t, os.FileMode(0o644), info.Mode().Perm())
}
