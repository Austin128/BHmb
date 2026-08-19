package errs

import (
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "重写 golden 文件")

const goldenPath = "testdata/status-mapping.golden"

// TestHTTPStatusGolden 固定「错误码 → HTTP 状态码」映射，防止误改导致前端行为漂移。
func TestHTTPStatusGolden(t *testing.T) {
	codes := Codes()
	sort.Ints(codes)

	var b strings.Builder
	b.WriteString("# 由 go test ./internal/pkg/errs -update 生成，禁止手工编辑\n")
	b.WriteString("# 格式：<错误码> <HTTP状态码>\n")
	for _, c := range codes {
		fmt.Fprintf(&b, "%d %d\n", c, HTTPStatus(c))
	}
	got := b.String()

	if *update {
		require.NoError(t, os.MkdirAll(filepath.Dir(goldenPath), 0o755))
		require.NoError(t, os.WriteFile(goldenPath, []byte(got), 0o644))
		return
	}

	want, err := os.ReadFile(goldenPath)
	require.NoError(t, err, "golden 文件缺失，请执行 go test ./internal/pkg/errs -update")
	assert.Equal(t, string(want), got, "错误码映射与 golden 不一致：新增错误码需同步 golden 与 05 号文档")
}

func TestHTTPStatusFallback(t *testing.T) {
	assert.Equal(t, http.StatusOK, HTTPStatus(0))
	assert.Equal(t, http.StatusForbidden, HTTPStatus(119999), "11 段未登记错误码兜底 403")
	assert.Equal(t, http.StatusForbidden, HTTPStatus(129999), "12 段未登记错误码兜底 403")
	assert.Equal(t, http.StatusBadRequest, HTTPStatus(999999), "其他段兜底 400")
}

func TestNewAndNewf(t *testing.T) {
	e := New(CodeConflict, "冲突")
	assert.Equal(t, CodeConflict, e.Code)
	assert.Equal(t, "冲突", e.Message)
	assert.Empty(t, e.Detail)

	f := Newf(CodeInvalidParam, "端口 %d 不合法", 70000)
	assert.Equal(t, "端口 70000 不合法", f.Message)
}

func TestWrap(t *testing.T) {
	root := errors.New("dial tcp: connection refused")

	e := Wrap(root, CodeInternal, "查询失败")
	assert.Equal(t, CodeInternal, e.Code)
	assert.Equal(t, root.Error(), e.Detail)
	assert.ErrorIs(t, e, root, "Unwrap 必须保留底层错误")

	assert.Equal(t, New(CodeInternal, "x").Code, Wrap(nil, CodeInternal, "x").Code, "cause 为 nil 时等价 New")

	again := Wrap(e, CodeInternal, "再包一层")
	assert.Same(t, e, again, "同错误码不重复包装")

	other := Wrap(e, CodeConflict, "状态冲突")
	assert.Equal(t, CodeConflict, other.Code)
	assert.ErrorIs(t, other, root)
}

func TestCodeAndMessage(t *testing.T) {
	assert.Equal(t, 0, Code(nil))
	assert.Equal(t, CodeInternal, Code(errors.New("裸错误")), "非 *Error 一律按内部错误处理")
	assert.Equal(t, CodeNotFound, Code(ErrNotFound))
	assert.Equal(t, "服务内部错误", Message(errors.New("裸错误")))
	assert.Equal(t, "资源不存在", Message(ErrNotFound))

	wrapped := fmt.Errorf("service: %w", ErrNotFound)
	assert.Equal(t, CodeNotFound, Code(wrapped), "被标准库包装后仍可取码")
}

func TestIsByCode(t *testing.T) {
	err := ErrNotFound.WithDetail("site id=%d", 42)
	assert.ErrorIs(t, err, ErrNotFound, "按错误码比较")
	assert.NotErrorIs(t, err, ErrConflict)
	assert.NotErrorIs(t, errors.New("裸错误"), ErrNotFound)
}

func TestWithDetailAndFieldAreCopies(t *testing.T) {
	base := ErrInvalidParam
	d := base.WithDetail("port=%d", 0)
	assert.Empty(t, base.Detail, "原哨兵错误不可被污染")
	assert.Equal(t, "port=0", d.Detail)

	f1 := base.WithField("a", 1)
	f2 := f1.WithField("b", 2)
	assert.Nil(t, base.Fields)
	assert.Len(t, f1.Fields, 1)
	assert.Len(t, f2.Fields, 2)
	assert.Equal(t, 1, f2.Fields["a"])
}

func TestErrorString(t *testing.T) {
	assert.Contains(t, New(CodeInternal, "内部错误").Error(), "code=100005")
	assert.Contains(t, Wrap(errors.New("boom"), CodeInternal, "内部错误").Error(), "cause=boom")
}

func TestAs(t *testing.T) {
	e, ok := As(fmt.Errorf("wrap: %w", ErrForbidden))
	require.True(t, ok)
	assert.Equal(t, CodeForbidden, e.Code)

	_, ok = As(errors.New("裸错误"))
	assert.False(t, ok)
}
