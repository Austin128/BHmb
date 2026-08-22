package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/novapanel/novapanel/internal/model"
	"github.com/novapanel/novapanel/internal/pkg/errs"
)

// UserRepository 提供用户读取与登录风控字段更新。
type UserRepository interface {
	FindByID(ctx context.Context, id int64) (*model.SysUser, error)
	FindByUsername(ctx context.Context, tenantID int64, username string) (*model.SysUser, error)
	Count(ctx context.Context) (int64, error)
	// List 按创建时间分页列出本租户用户，返回当页数据与总数。
	List(ctx context.Context, tenantID int64, offset, limit int) ([]model.SysUser, int64, error)
	// MarkLoginSuccess 登录成功后清零失败计数并记录来源。
	MarkLoginSuccess(ctx context.Context, id int64, at time.Time, ip string) error
	// MarkLoginFailure 累加失败次数，达到上限时写入锁定截止时间，返回累计次数。
	MarkLoginFailure(ctx context.Context, id int64, limit int, lockFor time.Duration, now time.Time) (int, error)
	UpdatePassword(ctx context.Context, id int64, passwordHash string, at time.Time) error
}

type userRepo struct {
	db *gorm.DB
}

// NewUserRepository 构造用户仓储。
func NewUserRepository(db *gorm.DB) UserRepository { return &userRepo{db: db} }

func (r *userRepo) FindByID(ctx context.Context, id int64) (*model.SysUser, error) {
	var u model.SysUser
	if err := r.db.WithContext(ctx).First(&u, "id = ?", id).Error; err != nil {
		return nil, wrapFind(err, "用户")
	}
	return &u, nil
}

func (r *userRepo) FindByUsername(ctx context.Context, tenantID int64, username string) (*model.SysUser, error) {
	var u model.SysUser
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND username = ?", tenantID, username).
		First(&u).Error
	if err != nil {
		return nil, wrapFind(err, "用户")
	}
	return &u, nil
}

func (r *userRepo) Count(ctx context.Context) (int64, error) {
	var n int64
	if err := r.db.WithContext(ctx).Model(&model.SysUser{}).Count(&n).Error; err != nil {
		return 0, errs.Wrap(err, errs.CodeInternal, "用户数量统计失败")
	}
	return n, nil
}

// List 先数总数再取当页：页码越界时前端仍需要 total 才能回退到最后一页。
func (r *userRepo) List(ctx context.Context, tenantID int64, offset, limit int) ([]model.SysUser, int64, error) {
	q := r.db.WithContext(ctx).Model(&model.SysUser{}).Where("tenant_id = ?", tenantID)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, errs.Wrap(err, errs.CodeInternal, "用户数量统计失败")
	}
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	var list []model.SysUser
	if err := q.Order("id ASC").Offset(offset).Limit(limit).Find(&list).Error; err != nil {
		return nil, 0, errs.Wrap(err, errs.CodeInternal, "用户列表查询失败")
	}
	return list, total, nil
}

func (r *userRepo) MarkLoginSuccess(ctx context.Context, id int64, at time.Time, ip string) error {
	err := r.db.WithContext(ctx).Model(&model.SysUser{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"last_login_at":    at,
			"last_login_ip":    ip,
			"login_fail_count": 0,
			"locked_until":     nil,
			"updated_at":       at,
		}).Error
	if err != nil {
		return errs.Wrap(err, errs.CodeInternal, "登录状态更新失败")
	}
	return nil
}

func (r *userRepo) MarkLoginFailure(ctx context.Context, id int64, limit int, lockFor time.Duration, now time.Time) (int, error) {
	count := 0
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var u model.SysUser
		if err := tx.WithContext(ctx).First(&u, "id = ?", id).Error; err != nil {
			return err
		}
		count = u.LoginFailCount + 1
		updates := map[string]any{"login_fail_count": count, "updated_at": now}
		if limit > 0 && count >= limit {
			until := now.Add(lockFor)
			updates["locked_until"] = until
			updates["login_fail_count"] = 0 // 锁定后重新计数，避免解锁即再次锁定
		}
		return tx.WithContext(ctx).Model(&model.SysUser{}).Where("id = ?", id).Updates(updates).Error
	})
	if err != nil {
		return count, errs.Wrap(err, errs.CodeInternal, "登录失败计数更新失败")
	}
	return count, nil
}

func (r *userRepo) UpdatePassword(ctx context.Context, id int64, passwordHash string, at time.Time) error {
	err := r.db.WithContext(ctx).Model(&model.SysUser{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"password_hash":        passwordHash,
			"password_updated_at":  at,
			"must_change_password": false,
			"updated_at":           at,
		}).Error
	if err != nil {
		return errs.Wrap(err, errs.CodeInternal, "密码更新失败")
	}
	return nil
}

// wrapFind 把 GORM 的记录不存在统一成 errs.ErrNotFound，其余归为内部错误。
func wrapFind(err error, what string) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errs.Wrapf(err, errs.CodeNotFound, "%s不存在", what)
	}
	return errs.Wrapf(err, errs.CodeInternal, "%s查询失败", what)
}
