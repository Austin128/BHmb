// Package response 实现 docs/05 5.2 的统一响应信封。
// 所有 HTTP 接口（含错误与 202 受理）都必须经由本包输出，
// 唯一例外是文件下载与导出的二进制流。
package response

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/novapanel/novapanel/internal/pkg/ctxkey"
	"github.com/novapanel/novapanel/internal/pkg/errs"
	"github.com/novapanel/novapanel/internal/pkg/logx"
)

// Result 是所有接口的统一信封。
type Result[T any] struct {
	Code      int    `json:"code"`
	Message   string `json:"message"`
	Data      T      `json:"data"`
	TraceID   string `json:"traceId"`
	Timestamp int64  `json:"timestamp"`
}

// PageData 是分页接口的 data 部分。
type PageData[T any] struct {
	List     []T   `json:"list"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"pageSize"`
}

// PageResult 供文档工具引用。
type PageResult[T any] = Result[PageData[T]]

// AsyncData 是 202 响应的 data 部分。
type AsyncData struct {
	TaskID string `json:"taskId"`
	Topic  string `json:"topic"`
}

// messageOK 为成功时的固定文案。
const messageOK = "ok"

// OK 返回成功响应。data 为 nil 时序列化为 null，不改写成 {}。
func OK[T any](c *gin.Context, data T) {
	c.JSON(http.StatusOK, Result[T]{
		Code: 0, Message: messageOK, Data: data,
		TraceID: traceID(c), Timestamp: time.Now().Unix(),
	})
}

// Page 返回分页响应。空列表必须是 []，不能是 null。
func Page[T any](c *gin.Context, list []T, total int64, page, pageSize int) {
	if list == nil {
		list = make([]T, 0)
	}
	OK(c, PageData[T]{List: list, Total: total, Page: page, PageSize: pageSize})
}

// Accepted 返回 202，用于长任务转异步。
func Accepted(c *gin.Context, taskID, topic string) {
	c.JSON(http.StatusAccepted, Result[AsyncData]{
		Code: 0, Message: messageOK, Data: AsyncData{TaskID: taskID, Topic: topic},
		TraceID: traceID(c), Timestamp: time.Now().Unix(),
	})
}

// Fail 把 error 映射为响应：业务错误码 + 用户可见文案 + HTTP 状态码。
// Detail/Fields 属于私有诊断信息，只进日志，绝不进响应体。
func Fail(c *gin.Context, err error) {
	code := errs.Code(err)
	status := errs.HTTPStatus(code)
	body := Result[any]{
		Code: code, Message: errs.Message(err), Data: nil,
		TraceID: traceID(c), Timestamp: time.Now().Unix(),
	}

	if e, ok := errs.As(err); ok {
		// 4xx 属可预期的客户端错误，记 warn；5xx 才是需要告警的服务端故障
		logAt(c, status, slog.Int("code", code),
			slog.String("path", c.FullPath()),
			slog.String("detail", e.Detail),
			slog.Any("fields", e.Fields),
		)
		// 仅参数校验类错误回填逐字段提示，供前端表单定位
		if code == errs.CodeInvalidParam && len(e.Fields) > 0 {
			body.Data = map[string]any{"fields": e.Fields}
		}
	} else {
		logAt(c, status, slog.Int("code", code),
			slog.String("path", c.FullPath()), slog.Any("err", err))
	}

	c.AbortWithStatusJSON(status, body)
}

func logAt(c *gin.Context, status int, attrs ...any) {
	logger := logx.FromContext(c.Request.Context())
	if status >= http.StatusInternalServerError {
		logger.Error("api failed", attrs...)
		return
	}
	logger.Warn("api rejected", attrs...)
}

// FailWith 以指定错误码与业务数据返回失败，用于 110009 这类需要携带 data 的场景。
func FailWith[T any](c *gin.Context, code int, message string, data T) {
	c.AbortWithStatusJSON(errs.HTTPStatus(code), Result[T]{
		Code: code, Message: message, Data: data,
		TraceID: traceID(c), Timestamp: time.Now().Unix(),
	})
}

func traceID(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	return ctxkey.TraceID(c.Request.Context())
}
