package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"sync"
	"time"

	"github.com/novapanel/novapanel/internal/pkg/errs"
)

// twoFATokenPrefix 为一阶段票据前缀，便于日志与前端识别。
const twoFATokenPrefix = "tfa_"

// Challenge 为登录一阶段票据的载荷。票据一次性使用，过期即废。
type Challenge struct {
	UserID    int64
	TenantID  int64
	Remember  bool
	ClientIP  string
	UserAgent string
	IssuedAt  time.Time
	ExpireAt  time.Time
}

// ChallengeStore 保存一阶段票据。多实例部署需换成 Redis 实现（M1）。
type ChallengeStore interface {
	// Issue 保存票据并返回明文 token。
	Issue(ctx context.Context, ch Challenge) (string, error)
	// Consume 取出并立即失效票据；不存在或过期返回 110010。
	Consume(ctx context.Context, token string, now time.Time) (*Challenge, error)
}

// MemoryChallengeStore 是进程内实现，只存 token 的 SHA-256，避免内存快照泄露明文。
type MemoryChallengeStore struct {
	mu    sync.Mutex
	items map[string]Challenge
}

// NewMemoryChallengeStore 构造进程内票据存储。
func NewMemoryChallengeStore() *MemoryChallengeStore {
	return &MemoryChallengeStore{items: make(map[string]Challenge)}
}

// Issue 生成 32 字节随机票据。
func (s *MemoryChallengeStore) Issue(_ context.Context, ch Challenge) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", errs.Wrap(err, errs.CodeInternal, "二次验证票据生成失败")
	}
	token := twoFATokenPrefix + base64.RawURLEncoding.EncodeToString(b)

	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range s.items { // 顺带清理过期票据
		if v.ExpireAt.Before(ch.IssuedAt) {
			delete(s.items, k)
		}
	}
	s.items[hashToken(token)] = ch
	return token, nil
}

// Consume 校验并一次性消费票据。
func (s *MemoryChallengeStore) Consume(_ context.Context, token string, now time.Time) (*Challenge, error) {
	if token == "" {
		return nil, errs.New(errs.Code2FAInvalid, "二次验证票据无效")
	}
	key := hashToken(token)

	s.mu.Lock()
	ch, ok := s.items[key]
	delete(s.items, key)
	s.mu.Unlock()

	if !ok {
		return nil, errs.New(errs.Code2FAInvalid, "二次验证票据无效或已使用")
	}
	if ch.ExpireAt.Before(now) {
		return nil, errs.New(errs.Code2FAInvalid, "二次验证票据已过期，请重新登录")
	}
	return &ch, nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// TwoFAVerifier 校验 TOTP 或恢复码。真实实现依赖 sys_user_2fa 表（M1 交付）。
type TwoFAVerifier interface {
	Verify(ctx context.Context, userID int64, method, code string) error
}

// UnboundTwoFAVerifier 为默认实现：M0 尚无 2FA 密钥存储，
// 因此对开启了 2FA 的账号明确返回 110016，而不是放行或伪造校验通过。
type UnboundTwoFAVerifier struct{}

// Verify 始终返回「未绑定 2FA」。
func (UnboundTwoFAVerifier) Verify(context.Context, int64, string, string) error {
	return errs.New(errs.Code2FANotBound, "该账号未完成二次验证绑定，请联系管理员")
}
