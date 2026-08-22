package handler

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/novapanel/novapanel/internal/api/v1/response"
	"github.com/novapanel/novapanel/internal/repository"
	"github.com/novapanel/novapanel/internal/service/sysinfo"
)

// System 提供主机与面板运行信息，供总览页与运维页展示。
type System struct {
	collector *sysinfo.Collector
	db        *gorm.DB
}

// NewSystem 构造系统信息处理器。
func NewSystem(collector *sysinfo.Collector, db *gorm.DB) *System {
	return &System{collector: collector, db: db}
}

// OverviewResult 为总览接口响应。
type OverviewResult struct {
	sysinfo.Snapshot
	Database DatabaseStat `json:"database"`
}

// DatabaseStat 为数据库探活与连接池状态。
// 连接池细节只在受权限保护的总览接口里给，公开的 health 接口仍只回驱动与可用性。
type DatabaseStat struct {
	Driver          string `json:"driver"`
	OK              bool   `json:"ok"`
	Error           string `json:"error,omitempty"`
	OpenConnections int    `json:"openConnections"`
	InUse           int    `json:"inUse"`
	Idle            int    `json:"idle"`
}

// Overview 处理 GET /api/v1/system/overview。
// 采集失败的单项以零值返回，整体不报错：监控页不该因为一个字段读不到就整页空白。
func (h *System) Overview(c *gin.Context) {
	out := OverviewResult{Snapshot: h.collector.Collect()}
	out.Database = h.databaseCheck()
	response.OK(c, out)
}

// databaseCheck 与健康接口口径保持一致：Ping 通即视为可用。
func (h *System) databaseCheck() DatabaseStat {
	check := DatabaseStat{Driver: repository.Dialect(h.db), OK: true}
	sqlDB, err := h.db.DB()
	if err != nil {
		check.OK = false
		check.Error = err.Error()
		return check
	}
	if err := sqlDB.Ping(); err != nil {
		check.OK = false
		check.Error = err.Error()
		return check
	}
	stats := sqlDB.Stats()
	check.OpenConnections = stats.OpenConnections
	check.InUse = stats.InUse
	check.Idle = stats.Idle
	return check
}
