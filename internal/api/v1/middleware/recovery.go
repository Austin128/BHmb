package middleware

import (
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/novapanel/novapanel/internal/api/v1/response"
	"github.com/novapanel/novapanel/internal/pkg/errs"
	"github.com/novapanel/novapanel/internal/pkg/logx"
)

// Recovery 捕获 panic：记录堆栈到 panel.log，对外只返回 100005，绝不泄露堆栈。
// 连接被对端中断（broken pipe）时不再尝试写响应。
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			if isBrokenPipe(rec) {
				logx.FromContext(c.Request.Context()).Warn("客户端提前断开连接",
					slog.String("path", c.Request.URL.Path))
				c.Abort()
				return
			}
			logx.FromContext(c.Request.Context()).Error("panic recovered",
				slog.Any("panic", rec),
				slog.String("method", c.Request.Method),
				slog.String("path", c.Request.URL.Path),
				slog.String("stack", string(debug.Stack())),
			)
			response.Fail(c, errs.ErrInternal)
		}()
		c.Next()
	}
}

// AccessLog 写访问日志。请求体与查询串中的敏感值由 logx 的脱敏 handler 处理，
// 这里只记录不含凭证的元数据。
func AccessLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		status := c.Writer.Status()
		attrs := []any{
			slog.String("method", c.Request.Method),
			slog.String("path", c.Request.URL.Path),
			slog.String("route", c.FullPath()),
			slog.Int("status", status),
			slog.Int64("durationMs", time.Since(start).Milliseconds()),
			slog.Int("bytes", c.Writer.Size()),
			slog.String("ip", c.ClientIP()),
			slog.String("ua", c.Request.UserAgent()),
		}
		logger := logx.AccessFromContext(c.Request.Context())
		if status >= http.StatusInternalServerError {
			logger.Error("http request", attrs...)
			return
		}
		logger.Info("http request", attrs...)
	}
}

func isBrokenPipe(rec any) bool {
	err, ok := rec.(error)
	if !ok {
		return false
	}
	var ne *net.OpError
	if !errors.As(err, &ne) {
		return false
	}
	msg := strings.ToLower(ne.Error())
	return strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "connection reset by peer") ||
		errors.Is(err, os.ErrDeadlineExceeded)
}
