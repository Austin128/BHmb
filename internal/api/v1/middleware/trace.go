// Package middleware 提供 HTTP 中间件链：链路标识、恢复、访问日志、认证与鉴权。
// 顺序约定（docs/05 5.3）：Trace → Recovery → AccessLog → Auth → Authz。
package middleware

import (
	"github.com/gin-gonic/gin"

	"github.com/novapanel/novapanel/internal/pkg/ctxkey"
	"github.com/novapanel/novapanel/internal/pkg/idgen"
)

// 链路相关的请求/响应头。
const (
	HeaderTraceID   = "X-Trace-Id"
	HeaderRequestID = "X-Request-Id"
)

// Trace 生成 26 位 ULID 作为 traceId，并把合法的客户端 X-Request-Id 单独保留。
// 客户端不可指定 traceId：否则可伪造链路、污染日志聚合。
func Trace() gin.HandlerFunc {
	return func(c *gin.Context) {
		traceID := idgen.NewULID()

		requestID := c.GetHeader(HeaderRequestID)
		if !idgen.IsRequestID(requestID) {
			requestID = "" // 非法值一律丢弃，防止日志与响应头注入
		}

		ctx := ctxkey.WithTraceID(c.Request.Context(), traceID)
		if requestID != "" {
			ctx = ctxkey.WithRequestID(ctx, requestID)
			c.Header(HeaderRequestID, requestID)
		}
		c.Request = c.Request.WithContext(ctx)

		c.Header(HeaderTraceID, traceID)
		c.Next()
	}
}
