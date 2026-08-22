// Package app 完成控制面的依赖装配与生命周期管理：
// 配置 → 日志 → 数据库 → 迁移 → 种子 → 安全组件 → 服务 → 路由 → HTTP 服务器。
// cmd 下的可执行文件只负责解析参数并调用本包，不做任何业务装配。
package app

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	v1 "github.com/novapanel/novapanel/internal/api/v1"
	"github.com/novapanel/novapanel/internal/api/v1/handler"
	"github.com/novapanel/novapanel/internal/api/v1/middleware"
	"github.com/novapanel/novapanel/internal/pkg/config"
	"github.com/novapanel/novapanel/internal/pkg/errs"
	"github.com/novapanel/novapanel/internal/pkg/logx"
	"github.com/novapanel/novapanel/internal/repository"
	"github.com/novapanel/novapanel/internal/repository/migrate"
	"github.com/novapanel/novapanel/internal/repository/seed"
	"github.com/novapanel/novapanel/internal/security"
	"github.com/novapanel/novapanel/internal/service/auth"
	filesvc "github.com/novapanel/novapanel/internal/service/file"
	"github.com/novapanel/novapanel/internal/service/file/pathguard"
	"github.com/novapanel/novapanel/internal/web"
	"github.com/novapanel/novapanel/migrations"
)

// jwtKeyPurpose 为访问令牌签名密钥的 HKDF 用途标签，与加密用子密钥隔离。
const jwtKeyPurpose = "novapanel/jwt/access"

// envInitialAdminPassword 允许安装脚本注入初始管理员口令，避免口令出现在命令行参数里。
const envInitialAdminPassword = "NOVA_INITIAL_ADMIN_PASSWORD"

// defaultAdminUsername 为初始管理员用户名。
const defaultAdminUsername = "admin"

// App 持有运行期依赖。
type App struct {
	cfg    *config.Config
	build  handler.BuildInfo
	logger *logx.Logger
	db     *gorm.DB
	engine *gin.Engine
	server *http.Server
	auth   *auth.Service
}

// New 按配置装配应用。返回后必须调用 Close 释放数据库与日志写入器。
func New(cfg *config.Config, build handler.BuildInfo) (*App, error) {
	if cfg == nil {
		return nil, errs.New(errs.CodeInvalidParam, "配置不能为空")
	}
	a := &App{cfg: cfg, build: build}

	logger, err := logx.Init(logx.Options{
		Level:      cfg.Log.Level,
		Dir:        cfg.Log.Dir,
		MaxSizeMB:  cfg.Log.MaxSizeMB,
		MaxBackups: cfg.Log.MaxBackups,
		MaxAgeDays: cfg.Log.MaxAgeDays,
		Compress:   cfg.Log.Compress,
		Console:    cfg.Log.Console,
	})
	if err != nil {
		return nil, err
	}
	a.logger = logger

	if err := a.initDatabase(); err != nil {
		_ = a.Close()
		return nil, err
	}
	if err := a.initHTTP(); err != nil {
		_ = a.Close()
		return nil, err
	}
	return a, nil
}

func (a *App) initDatabase() error {
	ctx := context.Background()

	db, err := repository.Open(&a.cfg.Database, a.cfg.Database.SlowThreshold)
	if err != nil {
		return err
	}
	a.db = db

	migrator := migrate.New(db, migrations.FS, repository.Dialect(db))
	if a.cfg.Database.AutoMigrate {
		applied, err := migrator.Up(ctx)
		if err != nil {
			return err
		}
		logx.L().Info("数据库迁移完成", slog.Int("applied", len(applied)))
	} else {
		pending, err := migrator.Pending(ctx)
		if err != nil {
			return err
		}
		if pending > 0 {
			// 关闭自动迁移时缺失迁移必须显式失败：带着旧表结构启动会在运行期报难以定位的列错误
			return errs.Newf(errs.CodeInternal, "存在 %d 个未执行的迁移，请先执行 novactl migrate up", pending)
		}
	}

	if err := seed.New(db).Run(ctx); err != nil {
		return err
	}
	return a.ensureAdmin(ctx)
}

// ensureAdmin 首次启动创建管理员。口令优先取环境变量，否则生成随机口令并只在此刻输出一次。
func (a *App) ensureAdmin(ctx context.Context) error {
	password := os.Getenv(envInitialAdminPassword)
	generated := password == ""
	if generated {
		p, err := security.RandomPassword(16)
		if err != nil {
			return err
		}
		password = p
	} else if err := security.CheckStrength(password); err != nil {
		return err
	}

	hasher := security.NewHasher(a.cfg.Security.BcryptCost)
	hash, err := hasher.Hash(password)
	if err != nil {
		return err
	}
	created, err := seed.New(a.db).EnsureAdmin(ctx, defaultAdminUsername, hash)
	if err != nil {
		return err
	}
	if !created {
		return nil
	}

	logx.L().Warn("已创建初始管理员账号，请登录后立即修改口令",
		slog.String("username", defaultAdminUsername))
	if generated {
		// 初始口令只在标准输出打印一次，不写入任何日志文件
		fmt.Fprintf(os.Stdout, "初始管理员账号：%s\n初始口令：%s\n请登录后立即修改，此口令不会再次显示。\n",
			defaultAdminUsername, password)
	}
	_ = os.Unsetenv(envInitialAdminPassword)
	return nil
}

func (a *App) initHTTP() error {
	master, err := security.LoadOrCreateMasterKey(a.cfg.Security.MasterKeyFile)
	if err != nil {
		return err
	}
	jwtSecret, err := security.DeriveKey(master, jwtKeyPurpose, 32)
	if err != nil {
		return err
	}
	tokens, err := security.NewTokenIssuer(jwtSecret, a.cfg.Security.JWTAccessTTL)
	if err != nil {
		return err
	}

	users := repository.NewUserRepository(a.db)
	roles := repository.NewRoleRepository(a.db)
	sessions := repository.NewSessionRepository(a.db)
	revokes := security.NewMemoryRevocationStore()

	authSvc, err := auth.New(auth.Deps{
		Users:    users,
		Roles:    roles,
		Sessions: sessions,
		Hasher:   security.NewHasher(a.cfg.Security.BcryptCost),
		Tokens:   tokens,
		Revokes:  revokes,
	}, auth.Config{
		RefreshTTL:        a.cfg.Security.JWTRefreshTTL,
		RememberTTL:       a.cfg.Security.JWTRememberTTL,
		LoginFailLimit:    a.cfg.Security.LoginFailLimit,
		LockDuration:      a.cfg.Security.LoginLockDuration,
		SessionMaxPerUser: a.cfg.Security.SessionMaxPerUser,
	})
	if err != nil {
		return err
	}
	a.auth = authSvc

	opts := v1.Options{
		// Cookie 的 Secure 属性跟随 TLS：明文 HTTP 下设置 Secure 会让浏览器直接丢弃 Cookie
		Auth:   handler.NewAuth(authSvc, a.cfg.Server.TLS.Enabled),
		Health: handler.NewHealth(a.db, a.build, time.Now()),
		AuthMW: middleware.AuthDeps{
			Tokens:     tokens,
			Revokes:    revokes,
			Principals: middleware.NewPrincipalStore(users, roles),
		},
	}
	// 文件管理：白名单为空说明未配置可访问范围，此时整个文件模块不注册，
	// 而不是放行整个根文件系统
	if len(a.cfg.File.AllowRoots) == 0 {
		logx.L().Warn("未配置 file.allow_roots，文件管理模块已禁用")
	} else {
		fileSvc, err := filesvc.New(pathguard.New(pathguard.Config{
			AllowRoots:     a.cfg.File.AllowRoots,
			DenyPaths:      a.cfg.File.DenyPaths,
			DenyWritePaths: a.cfg.File.DenyWritePaths,
			FollowSymlink:  a.cfg.File.FollowSymlink,
			MaxPathLen:     a.cfg.File.MaxPathLen,
		}), filesvc.Config{
			MaxEditSize:      int64(a.cfg.File.MaxEditSizeMB) << 20,
			MaxUploadSize:    int64(a.cfg.File.MaxUploadSizeMB) << 20,
			MaxListEntries:   a.cfg.File.MaxListEntries,
			MaxSearchResults: a.cfg.File.MaxSearchResults,
			UploadTempDir:    a.cfg.File.UploadTempDir,
		})
		if err != nil {
			return err
		}
		opts.File = handler.NewFile(fileSvc)
	}

	if web.Available() {
		opts.StaticFS = web.MustFS()
	} else {
		logx.L().Warn("未嵌入前端产物，仅提供 API（执行 make web-build 后重新编译可内置面板界面）")
	}

	engine, err := v1.NewEngine(opts)
	if err != nil {
		return err
	}
	a.engine = engine

	a.server = &http.Server{
		Addr:              net.JoinHostPort(a.cfg.Server.Host, strconv.Itoa(a.cfg.Server.Port)),
		Handler:           engine,
		ReadTimeout:       a.cfg.Server.ReadTimeout,
		WriteTimeout:      a.cfg.Server.WriteTimeout,
		IdleTimeout:       a.cfg.Server.IdleTimeout,
		ReadHeaderTimeout: 10 * time.Second, // 防慢速头攻击
	}
	if a.cfg.Server.TLS.Enabled {
		a.server.TLSConfig = &tls.Config{MinVersion: tlsMinVersion(a.cfg.Server.TLS.MinVersion)}
		if err := a.ensureTLSCert(); err != nil {
			return err
		}
	}
	return nil
}

// ensureTLSCert 在开启 auto_self_signed 且证书缺失时生成自签证书，
// 让首次安装无需准备证书即可通过 HTTPS 访问；已存在的证书不会被覆盖。
func (a *App) ensureTLSCert() error {
	if !a.cfg.Server.TLS.AutoSelfSigned {
		return nil
	}
	created, err := security.EnsureSelfSignedCert(
		a.cfg.Server.TLS.CertFile,
		a.cfg.Server.TLS.KeyFile,
		[]string{a.cfg.Server.Host},
	)
	if err != nil {
		return err
	}
	if created {
		logx.L().Warn("已生成自签 TLS 证书，浏览器会提示不受信任，生产请替换为受信证书",
			slog.String("certFile", a.cfg.Server.TLS.CertFile))
	}
	return nil
}

// Run 启动 HTTP 服务并阻塞，直到 ctx 取消后优雅退出。
func (a *App) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		logx.L().Info("面板启动",
			slog.String("addr", a.server.Addr),
			slog.Bool("tls", a.cfg.Server.TLS.Enabled),
			slog.String("version", a.build.Version),
		)
		var err error
		if a.cfg.Server.TLS.Enabled {
			err = a.server.ListenAndServeTLS(a.cfg.Server.TLS.CertFile, a.cfg.Server.TLS.KeyFile)
		} else {
			logx.L().Warn("TLS 未启用，面板以明文 HTTP 提供服务，请仅在受信网络中使用")
			err = a.server.ListenAndServe()
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- errs.Wrap(err, errs.CodeInternal, "HTTP 服务启动失败")
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return a.shutdown()
	}
}

func (a *App) shutdown() error {
	timeout := a.cfg.Server.ShutdownTimeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	logx.L().Info("收到退出信号，开始优雅停机", slog.Duration("timeout", timeout))
	if err := a.server.Shutdown(ctx); err != nil {
		return errs.Wrap(err, errs.CodeInternal, "优雅停机超时")
	}
	return nil
}

// Handler 返回 HTTP 处理器，供测试与嵌入场景使用。
func (a *App) Handler() http.Handler { return a.engine }

// Close 释放数据库连接与日志写入器。
func (a *App) Close() error {
	var firstErr error
	if a.db != nil {
		if err := repository.Close(a.db); err != nil {
			firstErr = err
		}
	}
	if a.logger != nil {
		if err := a.logger.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func tlsMinVersion(s string) uint16 {
	switch s {
	case "1.3":
		return tls.VersionTLS13
	default:
		return tls.VersionTLS12 // 低于 1.2 的版本不予支持
	}
}
