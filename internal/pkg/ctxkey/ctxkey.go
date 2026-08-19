// Package ctxkey 定义全链路 context 值的类型化 key。
// 按 docs/03 3.7.3 的白名单，context 仅允许携带 traceID、requestID、
// userID、tenantID、nodeID、locale；其余数据（角色、权限等）不得进入 context。
package ctxkey

import "context"

type key int

const (
	traceIDKey key = iota
	requestIDKey
	userIDKey
	tenantIDKey
	nodeIDKey
	localeKey
)

// DefaultTenantID 为单租户部署下的默认租户。
const DefaultTenantID int64 = 1

// WithTraceID 绑定服务端生成的 traceId。
func WithTraceID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, traceIDKey, id)
}

// TraceID 读取 traceId，缺失返回空串。
func TraceID(ctx context.Context) string { return str(ctx, traceIDKey) }

// WithRequestID 绑定客户端 X-Request-Id（仅回显与关联，不作为 traceId）。
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// RequestID 读取客户端请求 ID。
func RequestID(ctx context.Context) string { return str(ctx, requestIDKey) }

// WithUserID 绑定当前登录用户 ID。
func WithUserID(ctx context.Context, id int64) context.Context {
	return context.WithValue(ctx, userIDKey, id)
}

// UserID 读取当前登录用户 ID，未登录返回 0。
func UserID(ctx context.Context) int64 { return num(ctx, userIDKey) }

// WithTenantID 绑定租户 ID。
func WithTenantID(ctx context.Context, id int64) context.Context {
	return context.WithValue(ctx, tenantIDKey, id)
}

// TenantID 读取租户 ID，缺失返回 DefaultTenantID。
func TenantID(ctx context.Context) int64 {
	if v := num(ctx, tenantIDKey); v != 0 {
		return v
	}
	return DefaultTenantID
}

// WithNodeID 绑定目标节点 ID。
func WithNodeID(ctx context.Context, id int64) context.Context {
	return context.WithValue(ctx, nodeIDKey, id)
}

// NodeID 读取目标节点 ID，非跨节点操作返回 0。
func NodeID(ctx context.Context) int64 { return num(ctx, nodeIDKey) }

// WithLocale 绑定语言标签。
func WithLocale(ctx context.Context, locale string) context.Context {
	return context.WithValue(ctx, localeKey, locale)
}

// Locale 读取语言标签，缺失返回 zh-CN。
func Locale(ctx context.Context) string {
	if v := str(ctx, localeKey); v != "" {
		return v
	}
	return "zh-CN"
}

func str(ctx context.Context, k key) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(k).(string); ok {
		return v
	}
	return ""
}

func num(ctx context.Context, k key) int64 {
	if ctx == nil {
		return 0
	}
	if v, ok := ctx.Value(k).(int64); ok {
		return v
	}
	return 0
}
