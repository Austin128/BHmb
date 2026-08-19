package security

import (
	"context"
	"sync"
	"time"
)

// RevocationStore 维护 accessToken 吊销名单（docs/12 12.7.5）。
// 吊销粒度有两种：jti 单条（登出、踢单个会话）与用户级水位线（改密、踢全部会话）。
type RevocationStore interface {
	// Revoke 将单个 jti 加入名单，ttl 通常取该令牌的剩余有效期。
	Revoke(ctx context.Context, jti string, ttl time.Duration) error
	// Revoked 判断 jti 是否已被吊销。
	Revoked(ctx context.Context, jti string) (bool, error)
	// RevokeUser 写用户级水位线：签发时间早于 before 的令牌一律失效。
	RevokeUser(ctx context.Context, userID int64, before time.Time) error
	// UserWatermark 返回用户级水位线，零值表示未设置。
	UserWatermark(ctx context.Context, userID int64) (time.Time, error)
}

// MemoryRevocationStore 是单实例内存实现，进程重启即清空。
// 重启后仍能保证安全：accessToken 有效期只有 15 分钟，且刷新链路会回查 sys_session 状态。
// 多控制面实例部署需替换为 bbolt / Redis 实现（M1 交付）。
type MemoryRevocationStore struct {
	mu         sync.RWMutex
	jtis       map[string]time.Time // jti -> 过期时间
	watermarks map[int64]time.Time
}

// NewMemoryRevocationStore 构造内存吊销名单。
func NewMemoryRevocationStore() *MemoryRevocationStore {
	return &MemoryRevocationStore{
		jtis:       make(map[string]time.Time),
		watermarks: make(map[int64]time.Time),
	}
}

// Revoke 加入名单并顺带清理过期条目。
func (s *MemoryRevocationStore) Revoke(_ context.Context, jti string, ttl time.Duration) error {
	if jti == "" {
		return nil
	}
	if ttl <= 0 {
		ttl = time.Minute
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, exp := range s.jtis {
		if exp.Before(now) {
			delete(s.jtis, k)
		}
	}
	s.jtis[jti] = now.Add(ttl)
	return nil
}

// Revoked 查询单条吊销状态。
func (s *MemoryRevocationStore) Revoked(_ context.Context, jti string) (bool, error) {
	s.mu.RLock()
	exp, ok := s.jtis[jti]
	s.mu.RUnlock()
	if !ok {
		return false, nil
	}
	if exp.Before(time.Now()) {
		s.mu.Lock()
		delete(s.jtis, jti)
		s.mu.Unlock()
		return false, nil
	}
	return true, nil
}

// RevokeUser 抬高用户级水位线，只允许向前推进。
func (s *MemoryRevocationStore) RevokeUser(_ context.Context, userID int64, before time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cur, ok := s.watermarks[userID]; ok && cur.After(before) {
		return nil
	}
	s.watermarks[userID] = before
	return nil
}

// UserWatermark 返回用户级水位线。
func (s *MemoryRevocationStore) UserWatermark(_ context.Context, userID int64) (time.Time, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.watermarks[userID], nil
}
