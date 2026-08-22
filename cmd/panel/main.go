// Command panel 是青垣面板控制面主进程。
// 只解析命令行参数与信号，全部装配逻辑在 internal/app。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/novapanel/novapanel/internal/api/v1/handler"
	"github.com/novapanel/novapanel/internal/app"
	"github.com/novapanel/novapanel/internal/pkg/config"
)

// 版本信息由 -ldflags "-X main.version=..." 注入。
var (
	version   = "dev"
	commit    = "none"
	buildTime = "unknown"
)

func main() {
	var (
		configPath  = flag.String("config", "/opt/novapanel/conf/panel.yaml", "配置文件路径，留空则只用默认值与 NOVA_ 环境变量")
		showVersion = flag.Bool("version", false, "打印版本信息后退出")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("novapanel %s (commit %s, built %s)\n", version, commit, buildTime)
		return
	}

	if err := run(*configPath); err != nil {
		fmt.Fprintf(os.Stderr, "启动失败：%v\n", err)
		os.Exit(1)
	}
}

func run(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	application, err := app.New(cfg, handler.BuildInfo{
		Version:   version,
		Commit:    commit,
		BuildTime: buildTime,
	})
	if err != nil {
		return err
	}
	defer func() { _ = application.Close() }()

	// SIGTERM/SIGINT 触发优雅停机；systemd 停止服务发送的是 SIGTERM
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	return application.Run(ctx)
}
