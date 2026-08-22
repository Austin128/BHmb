package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"gorm.io/gorm"

	"github.com/novapanel/novapanel/internal/model"
	"github.com/novapanel/novapanel/internal/pkg/errs"
	"github.com/novapanel/novapanel/internal/rbac"
	"github.com/novapanel/novapanel/internal/repository"
)

// cmdUser 处理账号运维：list / unlock / 2fa off。
// 这些操作直接改库，面板进程无需停机；已签发的 accessToken 最长在其 TTL 内仍有效。
func cmdUser(args []string) error {
	fs := flag.NewFlagSet("user", flag.ContinueOnError)
	configPath := fs.String("c", defaultConfigPath, "配置文件路径")
	username := fs.String("u", "", "用户名")
	if err := parse(fs, args); err != nil {
		return err
	}

	action := fs.Arg(0)
	if action == "" {
		return errs.New(errs.CodeInvalidParam, "用法：novactl user list|unlock|2fa off -u <用户名>")
	}

	_, db, closeDB, err := openDB(*configPath)
	if err != nil {
		return err
	}
	defer closeDB()

	ctx := context.Background()
	switch action {
	case "list":
		return userList(ctx, db)
	case "unlock":
		return userUnlock(ctx, db, *username)
	case "2fa":
		if fs.Arg(1) != "off" {
			return errs.New(errs.CodeInvalidParam, "用法：novactl user 2fa off -u <用户名>")
		}
		return userTwoFAOff(ctx, db, *username)
	default:
		return errs.Newf(errs.CodeInvalidParam, "未知账号动作：%s", action)
	}
}

func userList(ctx context.Context, db *gorm.DB) error {
	var users []model.SysUser
	if err := db.WithContext(ctx).Order("id").Find(&users).Error; err != nil {
		return errs.Wrap(err, errs.CodeInternal, "用户查询失败")
	}
	now := time.Now().UTC()
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\t用户名\t状态\t2FA\t锁定\t最后登录\t登录IP")
	for i := range users {
		u := &users[i]
		locked := "-"
		if u.IsLocked(now) {
			locked = u.LockedUntil.Local().Format(time.RFC3339)
		}
		last := "-"
		if u.LastLoginAt != nil {
			last = u.LastLoginAt.Local().Format(time.RFC3339)
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\t%s\n",
			u.ID, u.Username, u.Status, boolText(u.TwoFactorEnabled), locked, last, dashIfEmpty(u.LastLoginIP))
	}
	return w.Flush()
}

// userUnlock 清掉锁定期与失败计数，状态为 locked 的账号一并恢复为 active。
func userUnlock(ctx context.Context, db *gorm.DB, username string) error {
	u, err := findUser(ctx, db, username)
	if err != nil {
		return err
	}
	updates := map[string]any{
		"locked_until":     nil,
		"login_fail_count": 0,
		"updated_at":       time.Now().UTC(),
	}
	if u.Status == model.UserStatusLocked {
		updates["status"] = model.UserStatusActive
	}
	if err := db.WithContext(ctx).Model(&model.SysUser{}).Where("id = ?", u.ID).
		Updates(updates).Error; err != nil {
		return errs.Wrap(err, errs.CodeInternal, "解锁失败")
	}
	fmt.Printf("用户 %s 已解锁（失败计数已清零）\n", username)
	return nil
}

// userTwoFAOff 关闭二次验证，用于管理员丢失动态码设备后自救。
func userTwoFAOff(ctx context.Context, db *gorm.DB, username string) error {
	u, err := findUser(ctx, db, username)
	if err != nil {
		return err
	}
	if !u.TwoFactorEnabled {
		fmt.Printf("用户 %s 未开启二次验证，无需处理\n", username)
		return nil
	}
	if err := db.WithContext(ctx).Model(&model.SysUser{}).Where("id = ?", u.ID).
		Updates(map[string]any{"two_factor_enabled": false, "updated_at": time.Now().UTC()}).Error; err != nil {
		return errs.Wrap(err, errs.CodeInternal, "关闭二次验证失败")
	}
	fmt.Printf("用户 %s 的二次验证已关闭，请登录后尽快重新绑定\n", username)
	return nil
}

// cmdSession 吊销会话：-u 指定用户，--all 吊销全部。
func cmdSession(args []string) error {
	fs := flag.NewFlagSet("session", flag.ContinueOnError)
	configPath := fs.String("c", defaultConfigPath, "配置文件路径")
	username := fs.String("u", "", "用户名")
	all := fs.Bool("all", false, "吊销全部用户的会话")
	if err := parse(fs, args); err != nil {
		return err
	}

	action := fs.Arg(0)
	if action != "list" && action != "revoke" {
		return errs.New(errs.CodeInvalidParam, "用法：novactl session list|revoke [-u <用户名>|-all]")
	}

	_, db, closeDB, err := openDB(*configPath)
	if err != nil {
		return err
	}
	defer closeDB()
	ctx := context.Background()

	if action == "list" {
		return sessionList(ctx, db, *username)
	}
	return sessionRevoke(ctx, db, *username, *all)
}

func sessionList(ctx context.Context, db *gorm.DB, username string) error {
	q := db.WithContext(ctx).Model(&model.SysSession{}).Order("id desc").Limit(100)
	if username != "" {
		u, err := findUser(ctx, db, username)
		if err != nil {
			return err
		}
		q = q.Where("user_id = ?", u.ID)
	}
	var sessions []model.SysSession
	if err := q.Find(&sessions).Error; err != nil {
		return errs.Wrap(err, errs.CodeInternal, "会话查询失败")
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\t用户ID\t状态\t设备\t来源IP\t登录时间")
	for i := range sessions {
		s := &sessions[i]
		fmt.Fprintf(w, "%d\t%d\t%s\t%s\t%s\t%s\n",
			s.ID, s.UserID, s.Status, dashIfEmpty(s.DeviceType),
			dashIfEmpty(s.ClientIP), s.LoginAt.Local().Format(time.RFC3339))
	}
	return w.Flush()
}

func sessionRevoke(ctx context.Context, db *gorm.DB, username string, all bool) error {
	if username == "" && !all {
		return errs.New(errs.CodeInvalidParam, "需指定 -u <用户名> 或 -all")
	}
	q := db.WithContext(ctx).Model(&model.SysSession{}).
		Where("status = ?", model.SessionStatusActive)
	target := "全部用户"
	if username != "" {
		u, err := findUser(ctx, db, username)
		if err != nil {
			return err
		}
		q = q.Where("user_id = ?", u.ID)
		target = username
	}
	res := q.Updates(map[string]any{
		"status":        model.SessionStatusRevoked,
		"revoke_reason": model.RevokeReasonAdminRevoke,
		"updated_at":    time.Now().UTC(),
	})
	if res.Error != nil {
		return errs.Wrap(res.Error, errs.CodeInternal, "会话吊销失败")
	}
	fmt.Printf("已吊销 %s 的 %d 个会话，刷新令牌立即失效\n", target, res.RowsAffected)
	fmt.Println("提示：已签发的 accessToken 会在其剩余寿命内仍可用，需要立刻断开请改口令（novactl passwd）")
	return nil
}

func findUser(ctx context.Context, db *gorm.DB, username string) (*model.SysUser, error) {
	if username == "" {
		return nil, errs.New(errs.CodeInvalidParam, "需指定 -u <用户名>")
	}
	return repository.NewUserRepository(db).FindByUsername(ctx, rbac.DefaultTenantID, username)
}

func boolText(v bool) string {
	if v {
		return "开"
	}
	return "关"
}

func dashIfEmpty(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
