// Command novactl 是 NovaPanel 的运维命令行工具：迁移、口令重置与配置校验。
// 它与面板进程共享同一套 internal 包，保证行为一致。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"gorm.io/gorm"

	"github.com/novapanel/novapanel/internal/pkg/config"
	"github.com/novapanel/novapanel/internal/pkg/errs"
	"github.com/novapanel/novapanel/internal/rbac"
	"github.com/novapanel/novapanel/internal/repository"
	"github.com/novapanel/novapanel/internal/repository/migrate"
	"github.com/novapanel/novapanel/internal/repository/seed"
	"github.com/novapanel/novapanel/internal/security"
	"github.com/novapanel/novapanel/migrations"
)

var (
	version   = "dev"
	commit    = "none"
	buildTime = "unknown"
)

const defaultConfigPath = "/opt/novapanel/conf/panel.yaml"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "错误：%v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return errs.New(errs.CodeInvalidParam, "缺少子命令")
	}

	switch args[0] {
	case "migrate":
		return cmdMigrate(args[1:])
	case "seed":
		return cmdSeed(args[1:])
	case "passwd":
		return cmdPasswd(args[1:])
	case "config":
		return cmdConfig(args[1:])
	case "version", "-v", "--version":
		fmt.Printf("novactl %s (commit %s, built %s)\n", version, commit, buildTime)
		return nil
	case "help", "-h", "--help":
		usage()
		return nil
	default:
		usage()
		return errs.Newf(errs.CodeInvalidParam, "未知子命令：%s", args[0])
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `novactl <命令> [参数]

命令：
  migrate up              执行全部未应用的迁移
  migrate down [-step N]  回滚最近 N 个迁移（默认 1）
  migrate status          列出迁移状态
  seed                    幂等写入角色与权限种子数据
  passwd -u <用户名>      重置用户口令（不传 -p 时生成随机口令）
  config check            校验配置文件
  version                 打印版本

公共参数：
  -c <path>               配置文件路径（默认 `+defaultConfigPath+`）
`)
}

// openDB 加载配置并打开数据库连接，返回释放函数。
func openDB(configPath string) (*config.Config, *gorm.DB, func(), error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, nil, nil, err
	}
	db, err := repository.Open(&cfg.Database, cfg.Database.SlowThreshold)
	if err != nil {
		return nil, nil, nil, err
	}
	return cfg, db, func() { _ = repository.Close(db) }, nil
}

func cmdMigrate(args []string) error {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	configPath := fs.String("c", defaultConfigPath, "配置文件路径")
	step := fs.Int("step", 1, "回滚步数，仅 down 有效")
	if err := parse(fs, args); err != nil {
		return err
	}
	action := fs.Arg(0)
	if action == "" {
		return errs.New(errs.CodeInvalidParam, "用法：novactl migrate up|down|status")
	}

	_, db, closeDB, err := openDB(*configPath)
	if err != nil {
		return err
	}
	defer closeDB()

	ctx := context.Background()
	m := migrate.New(db, migrations.FS, repository.Dialect(db))

	switch action {
	case "up":
		applied, err := m.Up(ctx)
		if err != nil {
			return err
		}
		if len(applied) == 0 {
			fmt.Println("没有待执行的迁移")
			return nil
		}
		for _, mg := range applied {
			fmt.Printf("已应用 %d_%s\n", mg.Version, mg.Name)
		}
		return nil
	case "down":
		// 回滚会删表丢数据，因此要求显式确认
		if os.Getenv("NOVA_CONFIRM_DOWN") != "yes" {
			return errs.New(errs.CodeNeedConfirm,
				"回滚会删除表与数据，确认后请设置 NOVA_CONFIRM_DOWN=yes 重新执行")
		}
		rolled, err := m.Down(ctx, *step)
		if err != nil {
			return err
		}
		for _, mg := range rolled {
			fmt.Printf("已回滚 %d_%s\n", mg.Version, mg.Name)
		}
		return nil
	case "status":
		list, err := m.Status(ctx)
		if err != nil {
			return err
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "版本\t名称\t状态\t应用时间")
		for _, s := range list {
			state := "待应用"
			applied := "-"
			if s.Applied {
				state = "已应用"
				applied = s.AppliedAt.Local().Format(time.RFC3339)
			}
			fmt.Fprintf(w, "%d\t%s\t%s\t%s\n", s.Version, s.Name, state, applied)
		}
		return w.Flush()
	default:
		return errs.Newf(errs.CodeInvalidParam, "未知迁移动作：%s", action)
	}
}

func cmdSeed(args []string) error {
	fs := flag.NewFlagSet("seed", flag.ContinueOnError)
	configPath := fs.String("c", defaultConfigPath, "配置文件路径")
	if err := parse(fs, args); err != nil {
		return err
	}
	_, db, closeDB, err := openDB(*configPath)
	if err != nil {
		return err
	}
	defer closeDB()

	if err := seed.New(db).Run(context.Background()); err != nil {
		return err
	}
	fmt.Println("种子数据已就绪")
	return nil
}

func cmdPasswd(args []string) error {
	fs := flag.NewFlagSet("passwd", flag.ContinueOnError)
	configPath := fs.String("c", defaultConfigPath, "配置文件路径")
	username := fs.String("u", "", "用户名")
	password := fs.String("p", "", "新口令，留空则生成随机口令")
	if err := parse(fs, args); err != nil {
		return err
	}
	if *username == "" {
		return errs.New(errs.CodeInvalidParam, "用法：novactl passwd -u <用户名> [-p <口令>]")
	}

	cfg, db, closeDB, err := openDB(*configPath)
	if err != nil {
		return err
	}
	defer closeDB()

	newPassword := *password
	generated := newPassword == ""
	if generated {
		if newPassword, err = security.RandomPassword(16); err != nil {
			return err
		}
	} else if err := security.CheckStrength(newPassword); err != nil {
		return err
	}

	ctx := context.Background()
	users := repository.NewUserRepository(db)
	u, err := users.FindByUsername(ctx, rbac.DefaultTenantID, *username)
	if err != nil {
		return err
	}
	hash, err := security.NewHasher(cfg.Security.BcryptCost).Hash(newPassword)
	if err != nil {
		return err
	}
	// 改密后 pwd 水位线上移，该用户所有旧 accessToken 立即失效
	if err := users.UpdatePassword(ctx, u.ID, hash, time.Now().UTC()); err != nil {
		return err
	}

	fmt.Printf("用户 %s 的口令已重置\n", *username)
	if generated {
		fmt.Printf("新口令：%s\n", newPassword)
	}
	return nil
}

func cmdConfig(args []string) error {
	fs := flag.NewFlagSet("config", flag.ContinueOnError)
	configPath := fs.String("c", defaultConfigPath, "配置文件路径")
	if err := parse(fs, args); err != nil {
		return err
	}
	if fs.Arg(0) != "" && fs.Arg(0) != "check" {
		return errs.Newf(errs.CodeInvalidParam, "未知配置动作：%s", fs.Arg(0))
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	fmt.Printf("配置校验通过：监听 %s:%d，数据库 %s，TLS=%t\n",
		cfg.Server.Host, cfg.Server.Port, cfg.Database.Driver, cfg.Server.TLS.Enabled)
	return nil
}

func parse(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(reorder(args)); err != nil {
		return errs.Wrap(err, errs.CodeInvalidParam, "参数解析失败")
	}
	return nil
}

// reorder 把 flag 提到位置参数之前，使 `novactl migrate up -c path` 与
// `novactl migrate -c path up` 等价（flag 包默认在首个非 flag 参数处停止解析）。
// 本工具的 flag 均需取值（-c/-step/-u/-p），因此可安全吞掉紧随其后的值；
// 若将来新增布尔 flag，必须改为显式 `-flag=true` 写法。
func reorder(args []string) []string {
	flags := make([]string, 0, len(args))
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") {
			rest = append(rest, arg)
			continue
		}
		flags = append(flags, arg)
		if !strings.Contains(arg, "=") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			i++
			flags = append(flags, args[i])
		}
	}
	return append(flags, rest...)
}
