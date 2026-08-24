package site_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/novapanel/novapanel/internal/model"
	"github.com/novapanel/novapanel/internal/pkg/errs"
)

// writeLog 往站点访问日志里写入若干行，模拟 Nginx 持续追加。
func writeLog(t *testing.T, path string, lines []string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644))
}

func TestLogsReturnsTailInOrder(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	detail, err := e.svc.Create(ctx, e.actor, staticReq("logtail", "logtail.example.com"))
	require.NoError(t, err)

	all := make([]string, 0, 500)
	for i := 1; i <= 500; i++ {
		all = append(all, fmt.Sprintf("line-%d", i))
	}
	writeLog(t, filepath.Join(e.cfg.LogDir, "logtail.log"), all)

	out, err := e.svc.Logs(ctx, e.actor, detail.ID, model.SiteLogAccess, 100)
	require.NoError(t, err)
	assert.True(t, out.Exists)
	assert.True(t, out.Enabled)
	require.Len(t, out.Lines, 100)
	assert.Equal(t, "line-401", out.Lines[0], "应返回尾部且保持时间顺序")
	assert.Equal(t, "line-500", out.Lines[99])
	assert.True(t, out.Truncated, "文件比窗口长时要告诉前端只显示了尾部")
	assert.Positive(t, out.Size)
	assert.Positive(t, out.ModifiedAt)

	// 行数不足时全量返回，且不标记截断。
	writeLog(t, filepath.Join(e.cfg.LogDir, "logtail.log"), []string{"only-one"})
	out, err = e.svc.Logs(ctx, e.actor, detail.ID, model.SiteLogAccess, 100)
	require.NoError(t, err)
	assert.Equal(t, []string{"only-one"}, out.Lines)
	assert.False(t, out.Truncated)
}

func TestLogsMissingFileIsNotAnError(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	detail, err := e.svc.Create(ctx, e.actor, staticReq("fresh", "fresh.example.com"))
	require.NoError(t, err)

	// 新站点还没有请求，日志文件不存在，这属于正常状态。
	out, err := e.svc.Logs(ctx, e.actor, detail.ID, model.SiteLogError, 0)
	require.NoError(t, err)
	assert.False(t, out.Exists)
	assert.Empty(t, out.Lines)
	assert.Equal(t, filepath.Join(e.cfg.LogDir, "fresh.error.log"), out.Path)
	assert.Zero(t, out.ModifiedAt)
}

func TestLogsRejectsUnknownTypeAndPathOutsideLogDir(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	detail, err := e.svc.Create(ctx, e.actor, staticReq("guard", "guard.example.com"))
	require.NoError(t, err)

	_, err = e.svc.Logs(ctx, e.actor, detail.ID, "syslog", 10)
	require.Error(t, err)
	assert.Equal(t, errs.CodeInvalidParam, errs.Code(err))

	// 记录里的日志路径被改到日志目录之外时必须拒绝，
	// 否则该接口会退化成任意文件读取。
	outside := filepath.Join(t.TempDir(), "passwd")
	require.NoError(t, os.WriteFile(outside, []byte("secret\n"), 0o644))
	set, err := e.svc.Settings(ctx, e.actor, detail.ID)
	require.NoError(t, err)
	require.NotEmpty(t, set.AccessLogPath)
	e.mutateSite(t, detail.ID, func(s *model.WebSite) { s.AccessLogPath = outside })

	_, err = e.svc.Logs(ctx, e.actor, detail.ID, model.SiteLogAccess, 10)
	require.Error(t, err)
	assert.Equal(t, errs.CodeInvalidParam, errs.Code(err))
}

func TestClearLogTruncatesInPlace(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	detail, err := e.svc.Create(ctx, e.actor, staticReq("clean", "clean.example.com"))
	require.NoError(t, err)

	path := filepath.Join(e.cfg.LogDir, "clean.log")
	writeLog(t, path, []string{"a", "b", "c"})
	before, err := os.Stat(path)
	require.NoError(t, err)

	out, err := e.svc.ClearLog(ctx, e.actor, detail.ID, model.SiteLogAccess)
	require.NoError(t, err)
	assert.Empty(t, out.Lines)
	assert.Zero(t, out.Size)

	after, err := os.Stat(path)
	require.NoError(t, err)
	assert.True(t, after.Mode().IsRegular())
	assert.Zero(t, after.Size())
	// 必须是截断而不是删除重建：Nginx 已持有的文件描述符指向同一 inode，
	// 删除会让后续日志写进一个不可见的文件。
	assert.True(t, os.SameFile(before, after), "清空应保留原文件（inode 不变）")
}

func TestClearLogMissingFileSucceeds(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	detail, err := e.svc.Create(ctx, e.actor, staticReq("noop", "noop.example.com"))
	require.NoError(t, err)

	out, err := e.svc.ClearLog(ctx, e.actor, detail.ID, model.SiteLogError)
	require.NoError(t, err)
	assert.False(t, out.Exists)
}

func TestLogsHandlesByteWindowEdges(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	detail, err := e.svc.Create(ctx, e.actor, staticReq("huge", "huge.example.com"))
	require.NoError(t, err)
	path := filepath.Join(e.cfg.LogDir, "huge.log")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))

	// 单行就超过 512KiB 字节窗口（Lua 堆栈这类日志会这样），
	// 且结尾没有换行符：不能因为「丢掉开头半行」把唯一一行也丢空。
	require.NoError(t, os.WriteFile(path, []byte(strings.Repeat("x", 600*1024)), 0o644))
	out, err := e.svc.Logs(ctx, e.actor, detail.ID, model.SiteLogAccess, 100)
	require.NoError(t, err)
	require.Len(t, out.Lines, 1)
	assert.Len(t, out.Lines[0], 512*1024, "只返回窗口内的尾部字节")
	assert.True(t, out.Truncated)

	// 超过 2000 行的请求被压到上限，而不是把整个文件读进内存。
	all := make([]string, 0, 3000)
	for i := 1; i <= 3000; i++ {
		all = append(all, fmt.Sprintf("l%d", i))
	}
	writeLog(t, path, all)
	out, err = e.svc.Logs(ctx, e.actor, detail.ID, model.SiteLogAccess, 9999)
	require.NoError(t, err)
	require.Len(t, out.Lines, 2000)
	assert.Equal(t, "l3000", out.Lines[1999])
	assert.True(t, out.Truncated)
}

func TestLogsRespectTenantBoundary(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	detail, err := e.svc.Create(ctx, e.actor, staticReq("mine", "mine.example.com"))
	require.NoError(t, err)
	writeLog(t, filepath.Join(e.cfg.LogDir, "mine.log"), []string{"x"})

	other := e.actor
	other.TenantID = 2
	_, err = e.svc.Logs(ctx, other, detail.ID, model.SiteLogAccess, 10)
	require.Error(t, err)
	assert.Equal(t, errs.CodeSiteNotFound, errs.Code(err))

	_, err = e.svc.ClearLog(ctx, other, detail.ID, model.SiteLogAccess)
	require.Error(t, err)
	assert.Equal(t, errs.CodeSiteNotFound, errs.Code(err))
}
