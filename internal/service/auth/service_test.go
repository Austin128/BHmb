package auth

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/novapanel/novapanel/internal/model"
	"github.com/novapanel/novapanel/internal/pkg/config"
	"github.com/novapanel/novapanel/internal/pkg/errs"
	"github.com/novapanel/novapanel/internal/rbac"
	"github.com/novapanel/novapanel/internal/repository"
	"github.com/novapanel/novapanel/internal/repository/migrate"
	"github.com/novapanel/novapanel/internal/repository/seed"
	"github.com/novapanel/novapanel/internal/security"
	"github.com/novapanel/novapanel/migrations"
)

const testPassword = "Novapanel1!"

// fixedTime 为测试基准时刻。取当前时间对齐到秒：断言只依赖相对时长，
// 同时保证签发的 accessToken 在真实时钟下仍处于有效期内。
var fixedTime = time.Now().UTC().Truncate(time.Second)

type stubTwoFA struct {
	err  error
	code string
}

func (s *stubTwoFA) Verify(_ context.Context, _ int64, _ string, code string) error {
	s.code = code
	return s.err
}

type fixture struct {
	svc      *Service
	db       *gorm.DB
	users    repository.UserRepository
	sessions repository.SessionRepository
	revokes  *security.MemoryRevocationStore
	twoFA    *stubTwoFA
	hasher   *security.Hasher
	now      time.Time
	nextID   int64
}

// newFixture 用文件型 SQLite 搭建真实仓储，避免 mock 与 SQL 行为脱节。
func newFixture(t *testing.T, tune func(cfg *Config)) *fixture {
	t.Helper()
	db, err := repository.Open(&config.DatabaseConfig{
		Driver:          repository.DriverSQLite,
		Path:            filepath.Join(t.TempDir(), "nova.db"),
		MaxOpenConns:    4,
		MaxIdleConns:    2,
		ConnMaxLifetime: time.Hour,
	}, 200*time.Millisecond)
	require.NoError(t, err)
	t.Cleanup(func() { _ = repository.Close(db) })

	ctx := context.Background()
	_, err = migrate.New(db, migrations.FS, repository.Dialect(db)).Up(ctx)
	require.NoError(t, err)
	require.NoError(t, seed.New(db).Run(ctx))

	hasher := security.NewHasher(4) // 测试用低 cost
	hash, err := hasher.Hash(testPassword)
	require.NoError(t, err)
	_, err = seed.New(db).EnsureAdmin(ctx, "admin", hash)
	require.NoError(t, err)

	tokens, err := security.NewTokenIssuer(make([]byte, 32), 15*time.Minute)
	require.NoError(t, err)
	revokes := security.NewMemoryRevocationStore()
	twoFA := &stubTwoFA{}

	cfg := Config{
		LoginFailLimit: 3,
		LockDuration:   15 * time.Minute,
		RefreshTTL:     7 * 24 * time.Hour,
		RememberTTL:    30 * 24 * time.Hour,
	}
	if tune != nil {
		tune(&cfg)
	}

	f := &fixture{
		db:       db,
		users:    repository.NewUserRepository(db),
		sessions: repository.NewSessionRepository(db),
		revokes:  revokes,
		twoFA:    twoFA,
		hasher:   hasher,
		now:      fixedTime,
		nextID:   90000,
	}
	svc, err := New(Deps{
		Users:    f.users,
		Roles:    repository.NewRoleRepository(db),
		Sessions: f.sessions,
		Hasher:   hasher,
		Tokens:   tokens,
		Revokes:  revokes,
		TwoFA:    twoFA,
	}, cfg)
	require.NoError(t, err)

	svc.now = func() time.Time { return f.now }
	svc.nextID = func() int64 { f.nextID++; return f.nextID }
	f.svc = svc
	return f
}

// adminID 返回内置管理员主键。
func (f *fixture) adminID(t *testing.T) int64 {
	t.Helper()
	u, err := f.users.FindByUsername(context.Background(), rbac.DefaultTenantID, "admin")
	require.NoError(t, err)
	return u.ID
}

func (f *fixture) enableTwoFA(t *testing.T, userID int64) {
	t.Helper()
	require.NoError(t, f.db.Model(&model.SysUser{}).
		Where("id = ?", userID).
		Update("two_factor_enabled", true).Error)
}

func (f *fixture) login(t *testing.T) *model.LoginResult {
	t.Helper()
	res, err := f.svc.Login(context.Background(), model.LoginRequest{
		Username: "admin", Password: testPassword,
	}, model.LoginMeta{ClientIP: "10.0.0.1", UserAgent: "go-test", DeviceType: "web"})
	require.NoError(t, err)
	require.NotNil(t, res)
	return res
}

func TestLoginSuccess(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, nil)

	res := f.login(t)
	assert.NotEmpty(t, res.AccessToken)
	assert.Equal(t, model.TokenTypeBearer, res.TokenType)
	assert.EqualValues(t, 900, res.ExpiresIn)
	assert.Equal(t, []string{model.RoleSuperAdmin}, res.Roles)
	assert.Equal(t, []string{model.PermissionAll}, res.Permissions, "超管返回通配权限")
	assert.True(t, res.User.IsSuper)
	assert.True(t, res.User.MustChangePassword)
	assert.Equal(t, defaultThemePreference, res.User.Theme)
	assert.NotEmpty(t, res.RefreshToken)
	assert.Equal(t, 7*24*time.Hour, res.RefreshTTL)

	// 会话落库且只存哈希
	sess, err := f.sessions.FindByRefreshHash(ctx, security.HashRefreshToken(res.RefreshToken))
	require.NoError(t, err)
	assert.Equal(t, model.SessionStatusActive, sess.Status)
	assert.Equal(t, "10.0.0.1", sess.ClientIP)
	assert.NotEqual(t, res.RefreshToken, sess.RefreshTokenHash)
	assert.Len(t, sess.RefreshTokenHash, 64)
	assert.Equal(t, f.now.Add(7*24*time.Hour), sess.RefreshExpireAt.UTC())

	// claims 与会话对应
	claims, err := f.svc.tokens.Parse(res.AccessToken)
	require.NoError(t, err)
	assert.Equal(t, sess.ID, claims.SessionID)
	assert.Equal(t, sess.JTI, claims.ID)
	uid, err := claims.UserID()
	require.NoError(t, err)
	assert.Equal(t, res.User.ID, uid)

	// 登录成功刷新风控字段
	u, err := f.users.FindByID(ctx, res.User.ID)
	require.NoError(t, err)
	require.NotNil(t, u.LastLoginAt)
	assert.Equal(t, "10.0.0.1", u.LastLoginIP)
	assert.Zero(t, u.LoginFailCount)
}

func TestLoginRememberExtendsRefreshTTL(t *testing.T) {
	f := newFixture(t, nil)
	res, err := f.svc.Login(context.Background(), model.LoginRequest{
		Username: "admin", Password: testPassword, Remember: true,
	}, model.LoginMeta{ClientIP: "10.0.0.2"})
	require.NoError(t, err)
	assert.Equal(t, 30*24*time.Hour, res.RefreshTTL)
}

func TestLoginRejectsBadInput(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, nil)

	_, err := f.svc.Login(ctx, model.LoginRequest{Username: "  ", Password: ""}, model.LoginMeta{})
	require.Error(t, err)
	assert.Equal(t, errs.CodeInvalidParam, errs.Code(err))

	_, err = f.svc.Login(ctx, model.LoginRequest{Username: "ghost", Password: testPassword}, model.LoginMeta{})
	require.Error(t, err)
	assert.Equal(t, errs.CodeBadCredentials, errs.Code(err), "用户不存在与密码错误必须同码")
}

func TestLoginLocksAfterFailures(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, nil)
	bad := model.LoginRequest{Username: "admin", Password: "wrong-password"}

	for i := 0; i < 2; i++ {
		_, err := f.svc.Login(ctx, bad, model.LoginMeta{})
		require.Error(t, err)
		assert.Equal(t, errs.CodeBadCredentials, errs.Code(err))
	}

	_, err := f.svc.Login(ctx, bad, model.LoginMeta{})
	require.Error(t, err)
	assert.Equal(t, errs.CodeAccountLocked, errs.Code(err), "达到上限应锁定")

	// 锁定期内即使密码正确也拒绝
	_, err = f.svc.Login(ctx, model.LoginRequest{Username: "admin", Password: testPassword}, model.LoginMeta{})
	require.Error(t, err)
	assert.Equal(t, errs.CodeAccountLocked, errs.Code(err))

	// 锁定到期后恢复
	f.now = fixedTime.Add(16 * time.Minute)
	_, err = f.svc.Login(ctx, model.LoginRequest{Username: "admin", Password: testPassword}, model.LoginMeta{})
	require.NoError(t, err)
}

func TestLoginRejectsDisabledAccount(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, nil)
	require.NoError(t, f.db.Model(&model.SysUser{}).
		Where("id = ?", f.adminID(t)).
		Update("status", model.UserStatusDisabled).Error)

	_, err := f.svc.Login(ctx, model.LoginRequest{Username: "admin", Password: testPassword}, model.LoginMeta{})
	require.Error(t, err)
	assert.Equal(t, errs.CodeAccountDisabled, errs.Code(err))
}

func TestLoginWithTwoFAReturnsChallengeOnly(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, nil)
	f.enableTwoFA(t, f.adminID(t))

	res, err := f.svc.Login(ctx, model.LoginRequest{Username: "admin", Password: testPassword, Remember: true}, model.LoginMeta{ClientIP: "10.0.0.3"})
	require.NoError(t, err)
	require.NotNil(t, res.TwoFA)
	assert.Empty(t, res.AccessToken, "一阶段不得下发 accessToken")
	assert.Empty(t, res.RefreshToken)
	assert.EqualValues(t, 300, res.TwoFA.ExpiresIn)
	assert.Equal(t, []string{model.TwoFAMethodTOTP, model.TwoFAMethodRecovery}, res.TwoFA.Methods)
	assert.Contains(t, res.TwoFA.TwoFAToken, "tfa_")

	// 未创建任何会话
	var n int64
	require.NoError(t, f.db.Model(&model.SysSession{}).Count(&n).Error)
	assert.Zero(t, n)

	// 二阶段成功后继承 remember 选项
	second, err := f.svc.VerifyTwoFA(ctx, model.TwoFAVerifyRequest{
		TwoFAToken: res.TwoFA.TwoFAToken, Method: model.TwoFAMethodTOTP, Code: "123456",
	}, model.LoginMeta{ClientIP: "10.0.0.3"})
	require.NoError(t, err)
	assert.NotEmpty(t, second.AccessToken)
	assert.Equal(t, 30*24*time.Hour, second.RefreshTTL)
	assert.Equal(t, "123456", f.twoFA.code)
	assert.True(t, second.User.TwoFABound)

	// 票据一次性
	_, err = f.svc.VerifyTwoFA(ctx, model.TwoFAVerifyRequest{
		TwoFAToken: res.TwoFA.TwoFAToken, Method: model.TwoFAMethodTOTP, Code: "123456",
	}, model.LoginMeta{})
	require.Error(t, err)
	assert.Equal(t, errs.Code2FAInvalid, errs.Code(err))
}

func TestVerifyTwoFAFailures(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, nil)
	f.enableTwoFA(t, f.adminID(t))

	_, err := f.svc.VerifyTwoFA(ctx, model.TwoFAVerifyRequest{TwoFAToken: "tfa_unknown", Method: model.TwoFAMethodTOTP, Code: "000000"}, model.LoginMeta{})
	require.Error(t, err)
	assert.Equal(t, errs.Code2FAInvalid, errs.Code(err))

	res, err := f.svc.Login(ctx, model.LoginRequest{Username: "admin", Password: testPassword}, model.LoginMeta{})
	require.NoError(t, err)
	f.twoFA.err = errs.New(errs.Code2FAInvalid, "动态码错误")
	_, err = f.svc.VerifyTwoFA(ctx, model.TwoFAVerifyRequest{
		TwoFAToken: res.TwoFA.TwoFAToken, Method: model.TwoFAMethodTOTP, Code: "999999",
	}, model.LoginMeta{})
	require.Error(t, err)
	assert.Equal(t, errs.Code2FAInvalid, errs.Code(err))

	// 票据过期
	res2, err := f.svc.Login(ctx, model.LoginRequest{Username: "admin", Password: testPassword}, model.LoginMeta{})
	require.NoError(t, err)
	f.now = fixedTime.Add(6 * time.Minute)
	f.twoFA.err = nil
	_, err = f.svc.VerifyTwoFA(ctx, model.TwoFAVerifyRequest{
		TwoFAToken: res2.TwoFA.TwoFAToken, Method: model.TwoFAMethodTOTP, Code: "123456",
	}, model.LoginMeta{})
	require.Error(t, err)
	assert.Equal(t, errs.Code2FAInvalid, errs.Code(err))
}

func TestDefaultTwoFAVerifierRejectsUnbound(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, nil)
	f.svc.twoFA = UnboundTwoFAVerifier{} // 默认实现：M0 未提供 2FA 密钥存储
	f.enableTwoFA(t, f.adminID(t))

	res, err := f.svc.Login(ctx, model.LoginRequest{Username: "admin", Password: testPassword}, model.LoginMeta{})
	require.NoError(t, err)
	_, err = f.svc.VerifyTwoFA(ctx, model.TwoFAVerifyRequest{
		TwoFAToken: res.TwoFA.TwoFAToken, Method: model.TwoFAMethodTOTP, Code: "123456",
	}, model.LoginMeta{})
	require.Error(t, err)
	assert.Equal(t, errs.Code2FANotBound, errs.Code(err))
}

func TestRefreshRotatesCredentials(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, nil)
	first := f.login(t)
	oldSess, err := f.sessions.FindByRefreshHash(ctx, security.HashRefreshToken(first.RefreshToken))
	require.NoError(t, err)

	f.now = fixedTime.Add(5 * time.Minute)
	res, err := f.svc.Refresh(ctx, first.RefreshToken, model.LoginMeta{ClientIP: "10.0.0.1"})
	require.NoError(t, err)
	assert.NotEmpty(t, res.AccessToken)
	assert.NotEqual(t, first.AccessToken, res.AccessToken)
	assert.NotEqual(t, first.RefreshToken, res.RefreshToken)
	assert.EqualValues(t, 900, res.ExpiresIn)
	assert.Equal(t, 7*24*time.Hour, res.RefreshTTL)

	// 同一会话被复用，jti 已轮换
	newSess, err := f.sessions.FindByRefreshHash(ctx, security.HashRefreshToken(res.RefreshToken))
	require.NoError(t, err)
	assert.Equal(t, oldSess.ID, newSess.ID)
	assert.NotEqual(t, oldSess.JTI, newSess.JTI)

	// 旧 accessToken 的 jti 进吊销名单
	revoked, err := f.revokes.Revoked(ctx, oldSess.JTI)
	require.NoError(t, err)
	assert.True(t, revoked)

	// 旧 refreshToken 立即失效
	_, err = f.svc.Refresh(ctx, first.RefreshToken, model.LoginMeta{})
	require.Error(t, err)
	assert.Equal(t, errs.CodeRefreshInvalid, errs.Code(err))
}

func TestRefreshRejectsEmptyAndUnknown(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, nil)

	for _, token := range []string{"", "   ", "not-a-real-token"} {
		_, err := f.svc.Refresh(ctx, token, model.LoginMeta{})
		require.Error(t, err)
		assert.Equal(t, errs.CodeRefreshInvalid, errs.Code(err))
	}
}

func TestRefreshReuseOnRevokedSessionRevokesAll(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, nil)
	first := f.login(t)
	second := f.login(t)

	sess, err := f.sessions.FindByRefreshHash(ctx, security.HashRefreshToken(first.RefreshToken))
	require.NoError(t, err)
	require.NoError(t, f.sessions.Revoke(ctx, sess.ID, model.RevokeReasonAdminRevoke, f.now))

	_, err = f.svc.Refresh(ctx, first.RefreshToken, model.LoginMeta{})
	require.Error(t, err)
	assert.Equal(t, errs.CodeRefreshInvalid, errs.Code(err))

	// 连坐吊销：另一条会话也不可再刷新
	_, err = f.svc.Refresh(ctx, second.RefreshToken, model.LoginMeta{})
	require.Error(t, err)
	assert.Equal(t, errs.CodeRefreshInvalid, errs.Code(err))

	active, err := f.sessions.ActiveOfUser(ctx, f.adminID(t), f.now)
	require.NoError(t, err)
	assert.Empty(t, active)

	wm, err := f.revokes.UserWatermark(ctx, f.adminID(t))
	require.NoError(t, err)
	assert.False(t, wm.IsZero(), "应写入用户级吊销水位线")
}

func TestRefreshRejectsDisabledUser(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, nil)
	res := f.login(t)
	require.NoError(t, f.db.Model(&model.SysUser{}).
		Where("id = ?", f.adminID(t)).
		Update("status", model.UserStatusDisabled).Error)

	_, err := f.svc.Refresh(ctx, res.RefreshToken, model.LoginMeta{})
	require.Error(t, err)
	assert.Equal(t, errs.CodeAccountDisabled, errs.Code(err))
}

func TestLogoutRevokesSessionAndToken(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, nil)
	res := f.login(t)
	claims, err := f.svc.tokens.Parse(res.AccessToken)
	require.NoError(t, err)

	require.NoError(t, f.svc.Logout(ctx, claims.SessionID, claims.ID, claims.ExpiresAt.Time))

	sess, err := f.sessions.FindByID(ctx, claims.SessionID)
	require.NoError(t, err)
	assert.Equal(t, model.SessionStatusRevoked, sess.Status)
	assert.Equal(t, model.RevokeReasonLogout, sess.RevokeReason)

	revoked, err := f.revokes.Revoked(ctx, claims.ID)
	require.NoError(t, err)
	assert.True(t, revoked)

	// 登出后旧 refreshToken 不可用
	_, err = f.svc.Refresh(ctx, res.RefreshToken, model.LoginMeta{})
	require.Error(t, err)
	assert.Equal(t, errs.CodeRefreshInvalid, errs.Code(err))
}

func TestLogoutWithoutSessionStillRevokesJTI(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, nil)

	require.NoError(t, f.svc.Logout(ctx, 0, "orphan-jti", time.Time{}))
	revoked, err := f.revokes.Revoked(ctx, "orphan-jti")
	require.NoError(t, err)
	assert.True(t, revoked)
}

func TestSessionLimitEvictsOldest(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, func(cfg *Config) { cfg.SessionMaxPerUser = 1 })

	first := f.login(t)
	f.now = fixedTime.Add(time.Minute)
	second := f.login(t)

	oldSess, err := f.sessions.FindByRefreshHash(ctx, security.HashRefreshToken(first.RefreshToken))
	require.NoError(t, err)
	assert.Equal(t, model.SessionStatusRevoked, oldSess.Status)
	assert.Equal(t, model.RevokeReasonKickout, oldSess.RevokeReason)

	newSess, err := f.sessions.FindByRefreshHash(ctx, security.HashRefreshToken(second.RefreshToken))
	require.NoError(t, err)
	assert.Equal(t, model.SessionStatusActive, newSess.Status)

	active, err := f.sessions.ActiveOfUser(ctx, f.adminID(t), f.now)
	require.NoError(t, err)
	assert.Len(t, active, 1)
}

func TestProfileForNonSuperUser(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, nil)
	now := f.now

	user := model.SysUser{
		Base:         model.Base{ID: 70001, CreatedAt: now, UpdatedAt: now, TenantID: rbac.DefaultTenantID},
		Username:     "ops01",
		Nickname:     "运维一号",
		Email:        "ops01@example.com",
		Avatar:       "/avatar/ops01.png",
		PasswordHash: "$2a$04$hash",
		Status:       model.UserStatusActive,
		Lang:         "zh-CN",
		Timezone:     "Asia/Shanghai",
	}
	require.NoError(t, f.db.Create(&user).Error)
	require.NoError(t, f.db.Create(&model.SysUserRole{
		Relation: model.Relation{ID: 70002, CreatedAt: now, TenantID: rbac.DefaultTenantID},
		UserID:   user.ID,
		RoleID:   rbac.RoleIDOps,
	}).Error)

	profile, err := f.svc.Profile(ctx, user.ID)
	require.NoError(t, err)
	assert.Equal(t, user.ID, profile.User.ID)
	require.NotNil(t, profile.User.Avatar)
	assert.Equal(t, "/avatar/ops01.png", *profile.User.Avatar)
	assert.False(t, profile.User.IsSuper)
	require.Len(t, profile.Roles, 1)
	assert.Equal(t, model.RoleOps, profile.Roles[0].Code)
	assert.Equal(t, model.DataScopeAll, profile.Roles[0].DataScope)
	assert.ElementsMatch(t, rbac.CodesOfRole(model.RoleOps), profile.Permissions)
	assert.NotContains(t, profile.Permissions, model.PermissionAll)
	assert.Equal(t, []string{"dashboard", "user", "ops"}, profile.Menus)
	assert.Empty(t, profile.NodeScope, "空数组表示可见全部节点")
}

func TestProfileSuperAdminAndMissingUser(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, nil)

	profile, err := f.svc.Profile(ctx, f.adminID(t))
	require.NoError(t, err)
	assert.Equal(t, []string{model.PermissionAll}, profile.Permissions)
	assert.Equal(t, rbac.Modules(), profile.Menus)

	_, err = f.svc.Profile(ctx, 987654321)
	require.Error(t, err)
	assert.Equal(t, errs.CodeNotFound, errs.Code(err))
}

func TestNewValidatesDeps(t *testing.T) {
	hasher := security.NewHasher(4)
	tokens, err := security.NewTokenIssuer(make([]byte, 32), time.Minute)
	require.NoError(t, err)

	_, err = New(Deps{Hasher: hasher, Tokens: tokens}, Config{})
	require.Error(t, err)

	_, err = New(Deps{
		Users:    repository.NewUserRepository(nil),
		Roles:    repository.NewRoleRepository(nil),
		Sessions: repository.NewSessionRepository(nil),
	}, Config{})
	require.Error(t, err)

	svc, err := New(Deps{
		Users:    repository.NewUserRepository(nil),
		Roles:    repository.NewRoleRepository(nil),
		Sessions: repository.NewSessionRepository(nil),
		Hasher:   hasher,
		Tokens:   tokens,
	}, Config{})
	require.NoError(t, err)
	assert.Equal(t, rbac.DefaultTenantID, svc.cfg.TenantID, "默认值必须补齐")
	assert.Equal(t, 7*24*time.Hour, svc.cfg.RefreshTTL)
	assert.Equal(t, 30*24*time.Hour, svc.cfg.RememberTTL)
	assert.Equal(t, 15*time.Minute, svc.cfg.LockDuration)
	assert.Equal(t, 5*time.Minute, svc.cfg.TwoFAChallengeTTL)
	assert.NotNil(t, svc.revokes)
	assert.NotNil(t, svc.chals)
}

func TestUserIDFromSubject(t *testing.T) {
	id, err := UserIDFromSubject("1874923847293847")
	require.NoError(t, err)
	assert.EqualValues(t, 1874923847293847, id)

	_, err = UserIDFromSubject("abc")
	require.Error(t, err)
	assert.Equal(t, errs.CodeUnauthorized, errs.Code(err))
}
