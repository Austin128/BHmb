package logx

import (
	"context"
	"log/slog"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// redactPatterns 命中的字段名（不区分大小写、忽略下划线）其值一律脱敏。
var redactPatterns = []string{
	"password", "passwd", "secret", "token", "accesskey", "secretkey",
	"privatekey", "authorization", "cookie", "totp",
}

// redactMask 为脱敏后的占位值。
const redactMask = "***"

// cliSecretRe 覆盖命令行形式的密码：-pXXXX、--password=XXXX、--token XXXX。
var cliSecretRe = regexp.MustCompile(`(?i)(-p|--password[= ]|--token[= ]|--secret[= ])\S+`)

// ShouldRedact 判断字段名是否需要脱敏。
func ShouldRedact(key string) bool {
	k := strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(key))
	for _, p := range redactPatterns {
		if strings.Contains(k, p) {
			return true
		}
	}
	return false
}

// RedactCommand 对命令行字符串中的密码类参数做正则脱敏。
func RedactCommand(cmd string) string {
	return cliSecretRe.ReplaceAllString(cmd, "${1}"+redactMask)
}

// replaceAttr 统一处理时间（UTC RFC3339Nano）、caller 字段与敏感值脱敏。
func replaceAttr(_ []string, a slog.Attr) slog.Attr {
	switch a.Key {
	case slog.TimeKey:
		if t, ok := a.Value.Any().(time.Time); ok {
			return slog.String("time", t.UTC().Format(time.RFC3339Nano))
		}
	case slog.SourceKey:
		if src, ok := a.Value.Any().(*slog.Source); ok && src != nil {
			return slog.String("caller", shortCaller(src))
		}
	}
	if ShouldRedact(a.Key) {
		return slog.String(a.Key, redactMask)
	}
	if a.Key == "cmd" || a.Key == "command" {
		if s, ok := a.Value.Any().(string); ok {
			return slog.String(a.Key, RedactCommand(s))
		}
	}
	return a
}

// shortCaller 输出 pkg/file.go:line，避免打印构建机绝对路径。
func shortCaller(src *slog.Source) string {
	dir, file := filepath.Split(src.File)
	pkg := filepath.Base(strings.TrimSuffix(dir, string(filepath.Separator)))
	if pkg == "." || pkg == string(filepath.Separator) {
		pkg = ""
	}
	name := file
	if pkg != "" {
		name = pkg + "/" + file
	}
	return name + ":" + itoa(src.Line)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// sampler 对 error 级别日志按 msg+module 采样：同一键在 window 内最多 limit 条，
// 窗口结束时以 suppressed 字段汇总被抑制的条数，避免错误风暴打爆磁盘。
type sampler struct {
	slog.Handler
	limit  int
	window time.Duration

	mu    sync.Mutex
	state map[string]*sampleState
}

type sampleState struct {
	windowStart time.Time
	count       int
	suppressed  int
}

func newSampler(h slog.Handler, limit int, window time.Duration) slog.Handler {
	return &sampler{Handler: h, limit: limit, window: window, state: map[string]*sampleState{}}
}

func (s *sampler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level < slog.LevelError {
		return s.Handler.Handle(ctx, r)
	}

	module := ""
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "module" {
			module = a.Value.String()
			return false
		}
		return true
	})

	key := r.Message + "|" + module
	now := r.Time
	if now.IsZero() {
		now = time.Now()
	}

	s.mu.Lock()
	st, ok := s.state[key]
	if !ok || now.Sub(st.windowStart) >= s.window {
		suppressed := 0
		if ok {
			suppressed = st.suppressed
		}
		st = &sampleState{windowStart: now, count: 1}
		s.state[key] = st
		s.mu.Unlock()
		if suppressed > 0 {
			r.AddAttrs(slog.Int("suppressed", suppressed))
		}
		return s.Handler.Handle(ctx, r)
	}
	st.count++
	if st.count > s.limit {
		st.suppressed++
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()
	return s.Handler.Handle(ctx, r)
}

func (s *sampler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &sampler{Handler: s.Handler.WithAttrs(attrs), limit: s.limit, window: s.window, state: s.state}
}

func (s *sampler) WithGroup(name string) slog.Handler {
	return &sampler{Handler: s.Handler.WithGroup(name), limit: s.limit, window: s.window, state: s.state}
}
