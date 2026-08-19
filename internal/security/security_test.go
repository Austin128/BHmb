package security

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/novapanel/novapanel/internal/pkg/errs"
)

func TestHasherRoundTrip(t *testing.T) {
	h := NewHasher(4) // 测试用低 cost，生产由配置提供 12
	hash, err := h.Hash("Str0ng-Passw!")
	require.NoError(t, err)
	assert.NotContains(t, hash, "Str0ng-Passw!", "哈希不得包含明文")
	assert.True(t, h.Verify(hash, "Str0ng-Passw!"))
	assert.False(t, h.Verify(hash, "wrong-password"))
}

func TestNewHasherClampsCost(t *testing.T) {
	assert.Equal(t, 12, NewHasher(0).Cost())
	assert.Equal(t, 12, NewHasher(99).Cost())
	assert.Equal(t, 12, NewHasher(12).Cost())
}

func TestNeedsRehash(t *testing.T) {
	low := NewHasher(4)
	hash, err := low.Hash("Str0ng-Passw!")
	require.NoError(t, err)

	assert.True(t, NewHasher(12).NeedsRehash(hash), "低 cost 哈希应提示升级")
	assert.False(t, low.NeedsRehash(hash))
	assert.False(t, low.NeedsRehash("not-a-bcrypt-hash"))
}

func TestHashRejectsOverlongPassword(t *testing.T) {
	h := NewHasher(4)
	_, err := h.Hash(string(make([]byte, PasswordMaxLen+1)))
	require.Error(t, err)
	assert.Equal(t, errs.CodeWeakPassword, errs.Code(err))
}

func TestCheckStrength(t *testing.T) {
	cases := []struct {
		name     string
		password string
		ok       bool
	}{
		{"合规三类", "Novapanel1!", true},
		{"合规无符号", "NovaPanel2026", true},
		{"太短", "Ab1!", false},
		{"仅小写数字", "novapanel1234", false},
		{"常见弱口令", "Administrator", false},
		{"超长", string(make([]byte, PasswordMaxLen+1)), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := CheckStrength(c.password)
			if c.ok {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Equal(t, errs.CodeWeakPassword, errs.Code(err))
		})
	}
}

func TestMasterKeyLoadOrCreate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "master.key")

	key, err := LoadOrCreateMasterKey(path)
	require.NoError(t, err)
	require.Len(t, key, masterKeyLen)

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "主密钥文件必须是 0600")

	again, err := LoadOrCreateMasterKey(path)
	require.NoError(t, err)
	assert.Equal(t, key, again, "已存在时必须复用同一密钥")

	_, err = LoadOrCreateMasterKey("")
	require.Error(t, err)
	assert.Equal(t, errs.CodeInvalidParam, errs.Code(err))

	short := filepath.Join(t.TempDir(), "short.key")
	require.NoError(t, os.WriteFile(short, []byte("too-short"), 0o600))
	_, err = LoadOrCreateMasterKey(short)
	require.Error(t, err)
	assert.Equal(t, errs.CodeInvalidParam, errs.Code(err))
}

func TestDeriveKeyIsolatesPurposes(t *testing.T) {
	master, err := LoadOrCreateMasterKey(filepath.Join(t.TempDir(), "master.key"))
	require.NoError(t, err)

	jwtKey, err := DeriveKey(master, "novapanel/jwt", 32)
	require.NoError(t, err)
	dataKey, err := DeriveKey(master, "novapanel/data", 32)
	require.NoError(t, err)

	assert.Len(t, jwtKey, 32)
	assert.NotEqual(t, jwtKey, dataKey, "不同用途必须派生出不同子密钥")
	assert.NotEqual(t, master, jwtKey)

	same, err := DeriveKey(master, "novapanel/jwt", 32)
	require.NoError(t, err)
	assert.Equal(t, jwtKey, same, "同 info 必须可复现")

	_, err = DeriveKey(nil, "x", 32)
	require.Error(t, err)
}

func newIssuer(t *testing.T) *TokenIssuer {
	t.Helper()
	iss, err := NewTokenIssuer(make([]byte, 32), 15*time.Minute)
	require.NoError(t, err)
	return iss
}

func TestTokenIssuerSignParse(t *testing.T) {
	iss := newIssuer(t)
	now := time.Now().UTC().Truncate(time.Second)
	pwdAt := now.Add(-24 * time.Hour)

	jti, err := NewJTI()
	require.NoError(t, err)
	require.Len(t, jti, 32)

	token, exp, err := iss.Sign(1874923847293847, 1, 555, jti, []string{"super_admin"}, pwdAt, now)
	require.NoError(t, err)
	assert.Equal(t, now.Add(15*time.Minute), exp)

	claims, err := iss.Parse(token)
	require.NoError(t, err)

	uid, err := claims.UserID()
	require.NoError(t, err)
	assert.EqualValues(t, 1874923847293847, uid)
	assert.EqualValues(t, 1, claims.TenantID)
	assert.EqualValues(t, 555, claims.SessionID)
	assert.Equal(t, []string{"super_admin"}, claims.Roles)
	assert.Equal(t, pwdAt.Unix(), claims.PwdUpdatedAt)
	assert.Equal(t, jti, claims.ID)
	assert.Equal(t, Issuer, claims.Issuer)
}

func TestTokenIssuerRejectsExpiredAndTampered(t *testing.T) {
	iss := newIssuer(t)
	past := time.Now().UTC().Add(-time.Hour)

	expired, _, err := iss.Sign(1, 1, 1, "jti", nil, past, past)
	require.NoError(t, err)
	_, err = iss.Parse(expired)
	require.Error(t, err)
	assert.Equal(t, errs.CodeTokenExpired, errs.Code(err))

	other, err := NewTokenIssuer([]byte("another-32-byte-long-secret-key!"), time.Minute)
	require.NoError(t, err)
	valid, _, err := iss.Sign(1, 1, 1, "jti", nil, time.Now(), time.Now().UTC())
	require.NoError(t, err)
	_, err = other.Parse(valid)
	require.Error(t, err, "换密钥后签名校验必须失败")
	assert.Equal(t, errs.CodeUnauthorized, errs.Code(err))

	_, err = iss.Parse("not.a.jwt")
	require.Error(t, err)
	assert.Equal(t, errs.CodeUnauthorized, errs.Code(err))
}

func TestNewTokenIssuerValidatesInput(t *testing.T) {
	_, err := NewTokenIssuer(make([]byte, 16), time.Minute)
	require.Error(t, err)
	assert.Equal(t, errs.CodeInvalidParam, errs.Code(err))

	_, err = NewTokenIssuer(make([]byte, 32), 0)
	require.Error(t, err)
	assert.Equal(t, errs.CodeInvalidParam, errs.Code(err))

	iss := newIssuer(t)
	_, _, err = iss.Sign(1, 1, 1, "", nil, time.Now(), time.Now())
	require.Error(t, err)
	assert.Equal(t, errs.CodeInvalidParam, errs.Code(err))
}

func TestRefreshTokenHashing(t *testing.T) {
	token, hash, err := NewRefreshToken()
	require.NoError(t, err)
	assert.Len(t, token, 43, "32 字节 base64url 无填充为 43 字符")
	assert.Len(t, hash, 64, "SHA-256 十六进制为 64 字符")
	assert.NotEqual(t, token, hash)
	assert.Equal(t, hash, HashRefreshToken(token), "哈希必须可复现")

	other, otherHash, err := NewRefreshToken()
	require.NoError(t, err)
	assert.NotEqual(t, token, other)
	assert.NotEqual(t, hash, otherHash)
}

func TestMemoryRevocationStore(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryRevocationStore()

	revoked, err := s.Revoked(ctx, "jti-1")
	require.NoError(t, err)
	assert.False(t, revoked)

	require.NoError(t, s.Revoke(ctx, "jti-1", time.Minute))
	revoked, err = s.Revoked(ctx, "jti-1")
	require.NoError(t, err)
	assert.True(t, revoked)

	require.NoError(t, s.Revoke(ctx, "jti-2", -time.Minute)) // 非正 ttl 回落为 1 分钟
	revoked, err = s.Revoked(ctx, "jti-2")
	require.NoError(t, err)
	assert.True(t, revoked)

	// 直接注入过期条目，验证读取时清理
	s.jtis["jti-3"] = time.Now().Add(-time.Second)
	revoked, err = s.Revoked(ctx, "jti-3")
	require.NoError(t, err)
	assert.False(t, revoked)
	assert.NotContains(t, s.jtis, "jti-3")

	require.NoError(t, s.Revoke(ctx, "", time.Minute))
}

func TestMemoryRevocationStoreWatermark(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryRevocationStore()

	wm, err := s.UserWatermark(ctx, 7)
	require.NoError(t, err)
	assert.True(t, wm.IsZero())

	now := time.Now().UTC()
	require.NoError(t, s.RevokeUser(ctx, 7, now))
	wm, err = s.UserWatermark(ctx, 7)
	require.NoError(t, err)
	assert.Equal(t, now, wm)

	require.NoError(t, s.RevokeUser(ctx, 7, now.Add(-time.Hour)))
	wm, err = s.UserWatermark(ctx, 7)
	require.NoError(t, err)
	assert.Equal(t, now, wm, "水位线只允许向前推进")

	forward := now.Add(time.Hour)
	require.NoError(t, s.RevokeUser(ctx, 7, forward))
	wm, err = s.UserWatermark(ctx, 7)
	require.NoError(t, err)
	assert.Equal(t, forward, wm)
}
