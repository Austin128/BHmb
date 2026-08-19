package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/novapanel/novapanel/internal/model"
	"github.com/novapanel/novapanel/internal/pkg/errs"
)

// SessionRepository 管理登录会话。仓储层只接收 refreshToken 的哈希，
// 明文 refreshToken 不得离开 service 层。
type SessionRepository interface {
	Create(ctx context.Context, s *model.SysSession) error
	FindByID(ctx context.Context, id int64) (*model.SysSession, error)
	FindByRefreshHash(ctx context.Context, hash string) (*model.SysSession, error)
	FindByJTI(ctx context.Context, jti string) (*model.SysSession, error)
	// Rotate 轮换会话凭证：更新 jti、refresh 哈希与两个过期时间。
	Rotate(ctx context.Context, id int64, jti, refreshHash string, accessExpireAt, refreshExpireAt, now time.Time) error
	Touch(ctx context.Context, id int64, at time.Time) error
	Revoke(ctx context.Context, id int64, reason string, at time.Time) error
	RevokeAllOfUser(ctx context.Context, userID int64, reason string, at time.Time) (int64, error)
	// ActiveOfUser 返回该用户仍有效的会话，按最后活跃时间升序，便于淘汰最旧会话。
	ActiveOfUser(ctx context.Context, userID int64, now time.Time) ([]model.SysSession, error)
	// DeleteExpiredBefore 物理清理过期会话（会话表不软删）。
	DeleteExpiredBefore(ctx context.Context, before time.Time) (int64, error)
}

type sessionRepo struct {
	db *gorm.DB
}

// NewSessionRepository 构造会话仓储。
func NewSessionRepository(db *gorm.DB) SessionRepository { return &sessionRepo{db: db} }

func (r *sessionRepo) Create(ctx context.Context, s *model.SysSession) error {
	if err := r.db.WithContext(ctx).Create(s).Error; err != nil {
		return errs.Wrap(err, errs.CodeInternal, "会话创建失败")
	}
	return nil
}

func (r *sessionRepo) FindByID(ctx context.Context, id int64) (*model.SysSession, error) {
	var s model.SysSession
	if err := r.db.WithContext(ctx).First(&s, "id = ?", id).Error; err != nil {
		return nil, wrapFind(err, "会话")
	}
	return &s, nil
}

func (r *sessionRepo) FindByRefreshHash(ctx context.Context, hash string) (*model.SysSession, error) {
	var s model.SysSession
	if err := r.db.WithContext(ctx).First(&s, "refresh_token_hash = ?", hash).Error; err != nil {
		return nil, wrapFind(err, "会话")
	}
	return &s, nil
}

func (r *sessionRepo) FindByJTI(ctx context.Context, jti string) (*model.SysSession, error) {
	var s model.SysSession
	if err := r.db.WithContext(ctx).First(&s, "jti = ?", jti).Error; err != nil {
		return nil, wrapFind(err, "会话")
	}
	return &s, nil
}

func (r *sessionRepo) Rotate(ctx context.Context, id int64, jti, refreshHash string, accessExpireAt, refreshExpireAt, now time.Time) error {
	res := r.db.WithContext(ctx).Model(&model.SysSession{}).
		Where("id = ? AND status = ?", id, model.SessionStatusActive).
		Updates(map[string]any{
			"jti":                jti,
			"refresh_token_hash": refreshHash,
			"access_expire_at":   accessExpireAt,
			"refresh_expire_at":  refreshExpireAt,
			"last_active_at":     now,
			"updated_at":         now,
		})
	if res.Error != nil {
		return errs.Wrap(res.Error, errs.CodeInternal, "会话轮换失败")
	}
	if res.RowsAffected == 0 {
		return errs.New(errs.CodeRefreshInvalid, "刷新凭证无效")
	}
	return nil
}

func (r *sessionRepo) Touch(ctx context.Context, id int64, at time.Time) error {
	err := r.db.WithContext(ctx).Model(&model.SysSession{}).
		Where("id = ?", id).
		Updates(map[string]any{"last_active_at": at, "updated_at": at}).Error
	if err != nil {
		return errs.Wrap(err, errs.CodeInternal, "会话活跃时间更新失败")
	}
	return nil
}

func (r *sessionRepo) Revoke(ctx context.Context, id int64, reason string, at time.Time) error {
	err := r.db.WithContext(ctx).Model(&model.SysSession{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":        model.SessionStatusRevoked,
			"revoke_reason": reason,
			"updated_at":    at,
		}).Error
	if err != nil {
		return errs.Wrap(err, errs.CodeInternal, "会话吊销失败")
	}
	return nil
}

func (r *sessionRepo) RevokeAllOfUser(ctx context.Context, userID int64, reason string, at time.Time) (int64, error) {
	res := r.db.WithContext(ctx).Model(&model.SysSession{}).
		Where("user_id = ? AND status = ?", userID, model.SessionStatusActive).
		Updates(map[string]any{
			"status":        model.SessionStatusRevoked,
			"revoke_reason": reason,
			"updated_at":    at,
		})
	if res.Error != nil {
		return 0, errs.Wrap(res.Error, errs.CodeInternal, "批量吊销会话失败")
	}
	return res.RowsAffected, nil
}

func (r *sessionRepo) ActiveOfUser(ctx context.Context, userID int64, now time.Time) ([]model.SysSession, error) {
	var list []model.SysSession
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND status = ? AND refresh_expire_at > ?", userID, model.SessionStatusActive, now).
		Order("last_active_at ASC").
		Find(&list).Error
	if err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "会话列表查询失败")
	}
	return list, nil
}

func (r *sessionRepo) DeleteExpiredBefore(ctx context.Context, before time.Time) (int64, error) {
	res := r.db.WithContext(ctx).
		Where("refresh_expire_at < ?", before).
		Delete(&model.SysSession{})
	if res.Error != nil {
		return 0, errs.Wrap(res.Error, errs.CodeInternal, "过期会话清理失败")
	}
	return res.RowsAffected, nil
}
