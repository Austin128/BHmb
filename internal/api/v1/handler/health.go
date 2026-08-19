package handler

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/novapanel/novapanel/internal/api/v1/response"
	"github.com/novapanel/novapanel/internal/repository"
)

// BuildInfo 为编译期注入的版本信息。
type BuildInfo struct {
	Version   string
	Commit    string
	BuildTime string
}

// HealthStatus 为 /api/v1/health 的响应体。
type HealthStatus struct {
	Status        string        `json:"status"`
	Version       string        `json:"version"`
	Commit        string        `json:"commit,omitempty"`
	BuildTime     string        `json:"buildTime,omitempty"`
	UptimeSeconds int64         `json:"uptimeSeconds"`
	Database      DatabaseCheck `json:"database"`
}

// DatabaseCheck 为数据库探活结果。
type DatabaseCheck struct {
	Driver string `json:"driver"`
	OK     bool   `json:"ok"`
	// Error 只在探活失败时出现，内容为驱动返回的错误摘要。
	Error string `json:"error,omitempty"`
}

// Health 为健康检查处理器。该接口不需要认证，因此不返回任何配置或拓扑细节。
type Health struct {
	db        *gorm.DB
	build     BuildInfo
	startedAt time.Time
}

// NewHealth 构造健康检查处理器。
func NewHealth(db *gorm.DB, build BuildInfo, startedAt time.Time) *Health {
	if build.Version == "" {
		build.Version = "dev"
	}
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	return &Health{db: db, build: build, startedAt: startedAt}
}

// Check 处理 GET /api/v1/health。数据库不可用时仍返回 200 信封，
// 由 status=degraded 与 database.ok 体现，便于探针区分「进程活着」与「依赖异常」。
func (h *Health) Check(c *gin.Context) {
	out := HealthStatus{
		Status:        "ok",
		Version:       h.build.Version,
		Commit:        h.build.Commit,
		BuildTime:     h.build.BuildTime,
		UptimeSeconds: int64(time.Since(h.startedAt).Seconds()),
		Database:      DatabaseCheck{Driver: repository.Dialect(h.db), OK: true},
	}

	if err := h.pingDB(c.Request.Context()); err != nil {
		out.Status = "degraded"
		out.Database.OK = false
		out.Database.Error = err.Error()
	}
	response.OK(c, out)
}

func (h *Health) pingDB(ctx context.Context) error {
	if h.db == nil {
		return errNoDatabase
	}
	sqlDB, err := h.db.DB()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return sqlDB.PingContext(ctx)
}

// errNoDatabase 用于未装配数据库的场景（例如仅启动 API 的诊断模式）。
var errNoDatabase = errNoDB{}

type errNoDB struct{}

func (errNoDB) Error() string { return "database not configured" }
