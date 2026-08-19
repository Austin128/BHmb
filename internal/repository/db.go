// Package repository 负责数据库装配与数据访问。上层只依赖本包导出的仓储接口，
// 禁止在 service / handler 中直接使用 *gorm.DB。
package repository

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	sqlitedriver "github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/novapanel/novapanel/internal/pkg/config"
	"github.com/novapanel/novapanel/internal/pkg/errs"
	"github.com/novapanel/novapanel/internal/pkg/logx"
)

// 支持的驱动。
const (
	DriverSQLite   = "sqlite"
	DriverMySQL    = "mysql"
	DriverPostgres = "postgres"
)

// Open 按配置装配 GORM 连接。SQLite 使用纯 Go 驱动（免 CGO），
// 并以 _txlock=immediate 让写事务立即取锁，充当迁移期间的库级互斥。
func Open(cfg *config.DatabaseConfig, slowThreshold time.Duration) (*gorm.DB, error) {
	if cfg == nil {
		return nil, errs.New(errs.CodeInvalidParam, "数据库配置为空")
	}

	gcfg := &gorm.Config{
		Logger:                                   newGormLogger(slowThreshold),
		NowFunc:                                  func() time.Time { return time.Now().UTC() },
		DisableForeignKeyConstraintWhenMigrating: true,
		SkipDefaultTransaction:                   false,
		TranslateError:                           true,
	}

	var (
		db  *gorm.DB
		err error
	)
	switch cfg.Driver {
	case DriverSQLite:
		dsn, derr := sqliteDSN(cfg.Path)
		if derr != nil {
			return nil, derr
		}
		db, err = gorm.Open(sqlitedriver.Open(dsn), gcfg)
	case DriverMySQL:
		db, err = gorm.Open(mysql.Open(cfg.DSN), gcfg)
	case DriverPostgres:
		db, err = gorm.Open(postgres.Open(cfg.DSN), gcfg)
	default:
		return nil, errs.Newf(errs.CodeInvalidParam, "database.driver 仅支持 sqlite/mysql/postgres，当前 %q", cfg.Driver)
	}
	if err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "数据库连接失败").WithField("driver", cfg.Driver)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "获取数据库连接池失败")
	}
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(ctx); err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "数据库连接不可用").WithField("driver", cfg.Driver)
	}
	return db, nil
}

// sqliteDSN 组装 SQLite DSN：WAL 提升读写并发，busy_timeout 缓解写锁竞争，
// foreign_keys 打开外键校验，_txlock=immediate 让事务开始即取写锁。
func sqliteDSN(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errs.New(errs.CodeInvalidParam, "database.path 不能为空")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return "", errs.Wrap(err, errs.CodeInternal, "数据库目录创建失败").WithField("dir", filepath.Dir(path))
	}
	q := url.Values{}
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "synchronous(NORMAL)")
	q.Set("_txlock", "immediate")
	return path + "?" + q.Encode(), nil
}

// Dialect 返回当前连接对应的方言名，用于选择迁移脚本与锁实现。
func Dialect(db *gorm.DB) string {
	switch db.Dialector.Name() {
	case "sqlite":
		return DriverSQLite
	case "mysql":
		return DriverMySQL
	case "postgres":
		return DriverPostgres
	default:
		return db.Dialector.Name()
	}
}

// Close 关闭底层连接池。
func Close(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return errs.Wrap(err, errs.CodeInternal, "获取数据库连接池失败")
	}
	if err := sqlDB.Close(); err != nil {
		return errs.Wrap(err, errs.CodeInternal, "数据库连接关闭失败")
	}
	return nil
}

// gormLogger 把 GORM 日志接入 logx，慢查询按阈值升级为 warn。
type gormLogger struct {
	slowThreshold time.Duration
}

func newGormLogger(slow time.Duration) gormlogger.Interface {
	if slow <= 0 {
		slow = 200 * time.Millisecond
	}
	return &gormLogger{slowThreshold: slow}
}

func (l *gormLogger) LogMode(gormlogger.LogLevel) gormlogger.Interface { return l }

func (l *gormLogger) Info(ctx context.Context, msg string, args ...any) {
	logx.FromContext(ctx).Info("数据库信息", "module", "db", "detail", fmt.Sprintf(msg, args...))
}

func (l *gormLogger) Warn(ctx context.Context, msg string, args ...any) {
	logx.FromContext(ctx).Warn("数据库告警", "module", "db", "detail", fmt.Sprintf(msg, args...))
}

func (l *gormLogger) Error(ctx context.Context, msg string, args ...any) {
	logx.FromContext(ctx).Error("数据库错误", "module", "db", "detail", fmt.Sprintf(msg, args...))
}

func (l *gormLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	cost := time.Since(begin)
	log := logx.FromContext(ctx)
	sql, rows := fc()

	switch {
	case err != nil && !errors.Is(err, gorm.ErrRecordNotFound):
		log.Error("SQL 执行失败", "module", "db", "sql", sql, "rows", rows,
			"costMs", cost.Milliseconds(), "code", errs.CodeInternal, "err", err.Error())
	case cost >= l.slowThreshold:
		log.Warn("慢查询", "module", "db", "sql", sql, "rows", rows, "costMs", cost.Milliseconds())
	default:
		if log.Enabled(ctx, slog.LevelDebug) {
			log.Debug("SQL", "module", "db", "sql", sql, "rows", rows, "costMs", cost.Milliseconds())
		}
	}
}
