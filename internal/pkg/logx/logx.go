// Package logx 封装 log/slog + lumberjack，提供结构化日志、脱敏、
// 运行时级别热调整与 error 采样。字段命名遵循 docs/03 3.9.2。
package logx

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/novapanel/novapanel/internal/pkg/ctxkey"
	"github.com/novapanel/novapanel/internal/pkg/errs"
)

// 日志文件名。audit.log 独立留存，不受界面操作影响。
const (
	FilePanel  = "panel.log"
	FileAccess = "access.log"
	FileAudit  = "audit.log"
)

// Options 为日志初始化参数，与 config.LogConfig 一一对应。
type Options struct {
	Level      string
	Dir        string
	MaxSizeMB  int
	MaxBackups int
	MaxAgeDays int
	Compress   bool
	Console    bool
	// Dev 为开发模式：使用文本 handler 输出到控制台，便于本地阅读。
	Dev bool
}

// Logger 持有面板、访问与审计三类日志器及其底层写入器。
type Logger struct {
	panel  *slog.Logger
	access *slog.Logger
	audit  *slog.Logger
	level  *slog.LevelVar
	closer []io.Closer
}

var (
	mu      sync.RWMutex
	current = fallback()
)

// fallback 在 Init 之前提供 stderr 输出，避免早期日志丢失。
func fallback() *Logger {
	lv := new(slog.LevelVar)
	lv.Set(slog.LevelInfo)
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lv})
	l := slog.New(h)
	return &Logger{panel: l, access: l, audit: l, level: lv}
}

// Init 按配置创建日志器并设为全局当前实例。重复调用会关闭旧写入器。
func Init(o Options) (*Logger, error) {
	lv := new(slog.LevelVar)
	level, err := ParseLevel(o.Level)
	if err != nil {
		return nil, err
	}
	lv.Set(level)

	if o.Dir == "" {
		return nil, errs.New(errs.CodeInvalidParam, "日志目录不能为空")
	}
	if err := os.MkdirAll(o.Dir, 0o750); err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "日志目录创建失败").WithField("dir", o.Dir)
	}

	lg := &Logger{level: lv}
	newRotator := func(name string) *lumberjack.Logger {
		r := &lumberjack.Logger{
			Filename:   filepath.Join(o.Dir, name),
			MaxSize:    o.MaxSizeMB,
			MaxBackups: o.MaxBackups,
			MaxAge:     o.MaxAgeDays,
			Compress:   o.Compress,
			LocalTime:  false,
		}
		lg.closer = append(lg.closer, r)
		return r
	}

	panelWriters := []io.Writer{newRotator(FilePanel)}
	if o.Console {
		panelWriters = append(panelWriters, os.Stdout)
	}
	panelOut := io.MultiWriter(panelWriters...)

	opts := &slog.HandlerOptions{Level: lv, AddSource: true, ReplaceAttr: replaceAttr}
	var panelHandler slog.Handler
	if o.Dev {
		panelHandler = slog.NewTextHandler(panelOut, opts)
	} else {
		panelHandler = slog.NewJSONHandler(panelOut, opts)
	}
	lg.panel = slog.New(newSampler(panelHandler, 10, time.Minute))
	lg.access = slog.New(slog.NewJSONHandler(newRotator(FileAccess), &slog.HandlerOptions{Level: slog.LevelInfo, ReplaceAttr: replaceAttr}))
	lg.audit = slog.New(slog.NewJSONHandler(newRotator(FileAudit), &slog.HandlerOptions{Level: slog.LevelInfo, ReplaceAttr: replaceAttr}))

	mu.Lock()
	old := current
	current = lg
	mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	return lg, nil
}

// Close 关闭底层文件写入器。
func (l *Logger) Close() error {
	var firstErr error
	for _, c := range l.closer {
		if err := c.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// ParseLevel 将字符串解析为 slog 级别。
func ParseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug, nil
	case "", "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, errs.Newf(errs.CodeInvalidParam, "非法日志级别 %q，仅支持 debug/info/warn/error", s)
	}
}

// SetLevel 运行时热调整日志级别，供 PATCH /api/v1/system/log-level 使用。
func SetLevel(s string) error {
	level, err := ParseLevel(s)
	if err != nil {
		return err
	}
	mu.RLock()
	lv := current.level
	mu.RUnlock()
	lv.Set(level)
	return nil
}

// Level 返回当前日志级别的小写名称。
func Level() string {
	mu.RLock()
	defer mu.RUnlock()
	return strings.ToLower(current.level.Level().String())
}

// L 返回面板日志器。
func L() *slog.Logger {
	mu.RLock()
	defer mu.RUnlock()
	return current.panel
}

// Access 返回访问日志器。
func Access() *slog.Logger {
	mu.RLock()
	defer mu.RUnlock()
	return current.access
}

// Audit 返回审计日志器。
func Audit() *slog.Logger {
	mu.RLock()
	defer mu.RUnlock()
	return current.audit
}

// FromContext 返回自动注入链路字段的日志器：traceId、userId、tenantId、nodeId。
func FromContext(ctx context.Context) *slog.Logger {
	return L().With(ctxAttrs(ctx)...)
}

// AccessFromContext 返回带链路字段的访问日志器。
func AccessFromContext(ctx context.Context) *slog.Logger {
	return Access().With(ctxAttrs(ctx)...)
}

// AuditFromContext 返回带链路字段的审计日志器。
func AuditFromContext(ctx context.Context) *slog.Logger {
	return Audit().With(ctxAttrs(ctx)...)
}

func ctxAttrs(ctx context.Context) []any {
	attrs := make([]any, 0, 8)
	if id := ctxkey.TraceID(ctx); id != "" {
		attrs = append(attrs, "traceId", id)
	}
	if id := ctxkey.UserID(ctx); id != 0 {
		attrs = append(attrs, "userId", id)
	}
	attrs = append(attrs, "tenantId", ctxkey.TenantID(ctx))
	if id := ctxkey.NodeID(ctx); id != 0 {
		attrs = append(attrs, "nodeId", id)
	}
	return attrs
}

// ErrAttrs 生成 error 级别必需的 code 与 err 字段，err 取 Detail 优先。
func ErrAttrs(err error) []any {
	if err == nil {
		return nil
	}
	code := errs.Code(err)
	detail := err.Error()
	if e, ok := errs.As(err); ok && e.Detail != "" {
		detail = e.Detail
	}
	attrs := []any{"code", code, "err", detail}
	if e, ok := errs.As(err); ok {
		for k, v := range e.Fields {
			attrs = append(attrs, k, v)
		}
	}
	return attrs
}
