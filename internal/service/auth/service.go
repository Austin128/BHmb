// Package auth 实现登录、二次验证、令牌刷新、登出与当前用户信息装载。
// 该包是认证业务的唯一决策点：handler 只做参数绑定与 Cookie 读写，
// 仓储只做数据存取，所有状态判定（锁定、停用、会话可用性、轮换）都在这里。
package auth

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/novapanel/novapanel/internal/model"
	"github.com/novapanel/novapanel/internal/pkg/errs"
	"github.com/novapanel/novapanel/internal/pkg/idgen"
	"github.com/novapanel/novapanel/internal/rbac"
	"github.com/novapanel/novapanel/internal/repository"
	"github.com/novapanel/novapanel/internal/security"
)

// defaultThemePreference 为 M0 的固定主题偏好。
// sys_user 尚无 theme 列，个人偏好持久化随「用户偏好设置」里程碑落地，
// 现阶段前端本地保存主题，后端只回一个安全默认值。
const defaultThemePreference = "auto"

// Config 为认证服务的策略参数，来源于 SecurityConfig。
type Config struct {
	// TenantID 为单租户部署下的租户主键。
	TenantID int64
	// RefreshTTL 为普通登录的 refreshToken 有效期。
	RefreshTTL time.Duration
	// RememberTTL 为勾选「记住我」后的 refreshToken 有效期。
	RememberTTL time.Duration
	// LoginFailLimit 为触发账号锁定的连续失败次数，<=0 表示不锁定。
	LoginFailLimit int
	// LockDuration 为锁定时长。
	LockDuration time.Duration
	// SessionMaxPerUser 为单用户并发会话上限，<=0 表示不限制。
	SessionMaxPerUser int
	// TwoFAChallengeTTL 为一阶段票据有效期，默认 5 分钟。
	TwoFAChallengeTTL time.Duration
}

func (c *Config) withDefaults() {
	if c.TenantID <= 0 {
		c.TenantID = rbac.DefaultTenantID
	}
	if c.RefreshTTL <= 0 {
		c.RefreshTTL = 7 * 24 * time.Hour
	}
	if c.RememberTTL <= 0 {
		c.RememberTTL = 30 * 24 * time.Hour
	}
	if c.LockDuration <= 0 {
		c.LockDuration = 15 * time.Minute
	}
	if c.TwoFAChallengeTTL <= 0 {
		c.TwoFAChallengeTTL = 5 * time.Minute
	}
}

// Service 为认证服务。
type Service struct {
	users    repository.UserRepository
	roles    repository.RoleRepository
	sessions repository.SessionRepository
	hasher   *security.Hasher
	tokens   *security.TokenIssuer
	revokes  security.RevocationStore
	twoFA    TwoFAVerifier
	chals    ChallengeStore
	cfg      Config

	// now 与 nextID 便于测试注入确定性时钟与主键。
	now    func() time.Time
	nextID func() int64
}

// Deps 汇总构造依赖，避免过长参数列表。
type Deps struct {
	Users    repository.UserRepository
	Roles    repository.RoleRepository
	Sessions repository.SessionRepository
	Hasher   *security.Hasher
	Tokens   *security.TokenIssuer
	Revokes  security.RevocationStore
	// TwoFA 为空时使用「未绑定」实现：开启 2FA 的账号会明确收到 110016。
	TwoFA TwoFAVerifier
	// Challenges 为空时使用进程内实现。
	Challenges ChallengeStore
}

// New 构造认证服务。
func New(d Deps, cfg Config) (*Service, error) {
	if d.Users == nil || d.Roles == nil || d.Sessions == nil {
		return nil, errs.New(errs.CodeInternal, "认证服务缺少仓储依赖")
	}
	if d.Hasher == nil || d.Tokens == nil {
		return nil, errs.New(errs.CodeInternal, "认证服务缺少安全依赖")
	}
	if d.Revokes == nil {
		d.Revokes = security.NewMemoryRevocationStore()
	}
	if d.TwoFA == nil {
		d.TwoFA = UnboundTwoFAVerifier{}
	}
	if d.Challenges == nil {
		d.Challenges = NewMemoryChallengeStore()
	}
	cfg.withDefaults()
	return &Service{
		users:    d.Users,
		roles:    d.Roles,
		sessions: d.Sessions,
		hasher:   d.Hasher,
		tokens:   d.Tokens,
		revokes:  d.Revokes,
		twoFA:    d.TwoFA,
		chals:    d.Challenges,
		cfg:      cfg,
		now:      func() time.Time { return time.Now().UTC() },
		nextID:   idgen.NextID,
	}, nil
}

// Login 校验口令并签发令牌；账号开启 2FA 时只返回一阶段票据。
func (s *Service) Login(ctx context.Context, req model.LoginRequest, meta model.LoginMeta) (*model.LoginResult, error) {
	username := strings.TrimSpace(req.Username)
	if username == "" || req.Password == "" {
		return nil, errs.New(errs.CodeInvalidParam, "用户名与密码不能为空")
	}
	now := s.now()

	user, err := s.users.FindByUsername(ctx, s.cfg.TenantID, username)
	if err != nil {
		if errs.Code(err) == errs.CodeNotFound {
			// 不区分「用户不存在」与「密码错误」，避免账号枚举
			return nil, errs.ErrBadCredentials
		}
		return nil, err
	}

	if user.IsLocked(now) {
		return nil, errs.Newf(errs.CodeAccountLocked, "账号已锁定，请于 %s 后重试", user.LockedUntil.Format(time.RFC3339))
	}
	if err := s.checkStatus(user); err != nil {
		return nil, err
	}

	if !s.hasher.Verify(user.PasswordHash, req.Password) {
		return nil, s.onBadPassword(ctx, user.ID, now)
	}

	if user.TwoFactorEnabled {
		return s.issueChallenge(ctx, user, req.Remember, meta, now)
	}
	return s.issueLogin(ctx, user, req.Remember, meta, now)
}

// VerifyTwoFA 消费一阶段票据并校验动态码，成功后返回与登录一致的结构。
func (s *Service) VerifyTwoFA(ctx context.Context, req model.TwoFAVerifyRequest, meta model.LoginMeta) (*model.LoginResult, error) {
	now := s.now()
	ch, err := s.chals.Consume(ctx, strings.TrimSpace(req.TwoFAToken), now)
	if err != nil {
		return nil, err
	}
	if err := s.twoFA.Verify(ctx, ch.UserID, req.Method, req.Code); err != nil {
		return nil, err
	}

	user, err := s.users.FindByID(ctx, ch.UserID)
	if err != nil {
		return nil, err
	}
	if user.IsLocked(now) {
		return nil, errs.ErrAccountLocked
	}
	if err := s.checkStatus(user); err != nil {
		return nil, err
	}
	return s.issueLogin(ctx, user, ch.Remember, meta, now)
}

// Refresh 轮换会话：旧 refreshToken 立即失效，旧 accessToken 的 jti 进吊销名单。
// 已失效凭证被再次使用视为窃取，吊销该用户全部会话并返回 110021。
func (s *Service) Refresh(ctx context.Context, refreshToken string, meta model.LoginMeta) (*model.RefreshResult, error) {
	if strings.TrimSpace(refreshToken) == "" {
		return nil, errs.ErrRefreshInvalid
	}
	now := s.now()

	sess, err := s.sessions.FindByRefreshHash(ctx, security.HashRefreshToken(refreshToken))
	if err != nil {
		if errs.Code(err) == errs.CodeNotFound {
			// 轮换后旧哈希已被覆盖，无法定位用户，只能拒绝
			return nil, errs.ErrRefreshInvalid
		}
		return nil, err
	}
	if !sess.IsUsable(now) {
		// 命中已吊销/已过期会话上的凭证：判定为复用，连坐吊销该用户全部会话
		if _, rerr := s.sessions.RevokeAllOfUser(ctx, sess.UserID, model.RevokeReasonRefreshReuse, now); rerr != nil {
			return nil, rerr
		}
		if rerr := s.revokes.RevokeUser(ctx, sess.UserID, now); rerr != nil {
			return nil, rerr
		}
		return nil, errs.New(errs.CodeRefreshInvalid, "刷新凭证已失效，请重新登录")
	}

	user, err := s.users.FindByID(ctx, sess.UserID)
	if err != nil {
		return nil, err
	}
	if err := s.checkStatus(user); err != nil {
		return nil, err
	}

	roles, err := s.roles.RolesOfUser(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	jti, err := security.NewJTI()
	if err != nil {
		return nil, err
	}
	newRefresh, newHash, err := security.NewRefreshToken()
	if err != nil {
		return nil, err
	}
	refreshTTL := s.refreshTTL(sess.RefreshExpireAt.Sub(sess.LoginAt) > s.cfg.RefreshTTL)
	accessToken, accessExp, err := s.tokens.Sign(user.ID, user.TenantID, sess.ID, jti, roleCodes(roles), pwdUpdatedAt(user), now)
	if err != nil {
		return nil, err
	}
	if err := s.sessions.Rotate(ctx, sess.ID, jti, newHash, accessExp, now.Add(refreshTTL), now); err != nil {
		return nil, err
	}
	// 旧 accessToken 在剩余寿命内失效
	if err := s.revokes.Revoke(ctx, sess.JTI, time.Until(sess.AccessExpireAt)); err != nil {
		return nil, err
	}

	return &model.RefreshResult{
		AccessToken:  accessToken,
		ExpiresIn:    int64(s.tokens.AccessTTL().Seconds()),
		TokenType:    model.TokenTypeBearer,
		RefreshToken: newRefresh,
		RefreshTTL:   refreshTTL,
	}, nil
}

// Logout 吊销当前会话与当前 accessToken。
func (s *Service) Logout(ctx context.Context, sessionID int64, jti string, accessExpireAt time.Time) error {
	now := s.now()
	if sessionID > 0 {
		if err := s.sessions.Revoke(ctx, sessionID, model.RevokeReasonLogout, now); err != nil {
			return err
		}
	}
	ttl := time.Until(accessExpireAt)
	if ttl <= 0 {
		ttl = s.tokens.AccessTTL()
	}
	return s.revokes.Revoke(ctx, jti, ttl)
}

// Profile 返回当前用户、角色、权限点与顶层菜单。
func (s *Service) Profile(ctx context.Context, userID int64) (*model.ProfileResult, error) {
	user, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	roles, err := s.roles.RolesOfUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	perms, err := s.permissionsOf(ctx, user)
	if err != nil {
		return nil, err
	}

	brief := make([]model.RoleBrief, 0, len(roles))
	for _, r := range roles {
		brief = append(brief, model.RoleBrief{Code: r.Code, Name: r.Name, DataScope: r.DataScope})
	}
	return &model.ProfileResult{
		User:        toUserBrief(user),
		Roles:       brief,
		Permissions: perms,
		NodeScope:   []string{}, // 空数组表示可见全部节点
		Menus:       rbac.MenusOf(perms),
	}, nil
}

// checkStatus 校验账号状态是否允许登录。
func (s *Service) checkStatus(u *model.SysUser) error {
	switch u.Status {
	case model.UserStatusActive:
		return nil
	case model.UserStatusDisabled:
		return errs.ErrAccountDisabled
	case model.UserStatusLocked:
		return errs.ErrAccountLocked
	default:
		return errs.New(errs.CodeAccountDisabled, "账号尚未激活")
	}
}

// onBadPassword 累计失败次数并返回对应错误，锁定后直接告知锁定。
func (s *Service) onBadPassword(ctx context.Context, userID int64, now time.Time) error {
	count, err := s.users.MarkLoginFailure(ctx, userID, s.cfg.LoginFailLimit, s.cfg.LockDuration, now)
	if err != nil {
		return err
	}
	if s.cfg.LoginFailLimit > 0 && count >= s.cfg.LoginFailLimit {
		return errs.Newf(errs.CodeAccountLocked, "连续 %d 次密码错误，账号已锁定 %s", count, s.cfg.LockDuration)
	}
	return errs.ErrBadCredentials
}

// issueChallenge 生成一阶段票据。此时不签发任何令牌。
func (s *Service) issueChallenge(ctx context.Context, u *model.SysUser, remember bool, meta model.LoginMeta, now time.Time) (*model.LoginResult, error) {
	token, err := s.chals.Issue(ctx, Challenge{
		UserID:    u.ID,
		Remember:  remember,
		ClientIP:  meta.ClientIP,
		ExpireAt:  now.Add(s.cfg.TwoFAChallengeTTL),
		IssuedAt:  now,
		TenantID:  u.TenantID,
		UserAgent: meta.UserAgent,
	})
	if err != nil {
		return nil, err
	}
	return &model.LoginResult{TwoFA: &model.TwoFAChallenge{
		TwoFAToken: token,
		ExpiresIn:  int64(s.cfg.TwoFAChallengeTTL.Seconds()),
		Methods:    []string{model.TwoFAMethodTOTP, model.TwoFAMethodRecovery},
	}}, nil
}

// issueLogin 写会话、签发令牌并组装登录响应。
func (s *Service) issueLogin(ctx context.Context, u *model.SysUser, remember bool, meta model.LoginMeta, now time.Time) (*model.LoginResult, error) {
	roles, err := s.roles.RolesOfUser(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	perms, err := s.permissionsOf(ctx, u)
	if err != nil {
		return nil, err
	}

	jti, err := security.NewJTI()
	if err != nil {
		return nil, err
	}
	refreshToken, refreshHash, err := security.NewRefreshToken()
	if err != nil {
		return nil, err
	}
	refreshTTL := s.refreshTTL(remember)
	sessionID := s.nextID()

	accessToken, accessExp, err := s.tokens.Sign(u.ID, u.TenantID, sessionID, jti, roleCodes(roles), pwdUpdatedAt(u), now)
	if err != nil {
		return nil, err
	}

	sess := &model.SysSession{
		Base: model.Base{
			ID:        sessionID,
			CreatedAt: now,
			UpdatedAt: now,
			CreatedBy: u.ID,
			TenantID:  u.TenantID,
		},
		UserID:           u.ID,
		JTI:              jti,
		RefreshTokenHash: refreshHash,
		DeviceType:       deviceType(meta.DeviceType),
		UserAgent:        truncate(meta.UserAgent, 512),
		ClientIP:         meta.ClientIP,
		LoginAt:          now,
		LastActiveAt:     now,
		AccessExpireAt:   accessExp,
		RefreshExpireAt:  now.Add(refreshTTL),
		Status:           model.SessionStatusActive,
	}
	if err := s.sessions.Create(ctx, sess); err != nil {
		return nil, err
	}
	if err := s.enforceSessionLimit(ctx, u.ID, sessionID, now); err != nil {
		return nil, err
	}
	if err := s.users.MarkLoginSuccess(ctx, u.ID, now, meta.ClientIP); err != nil {
		return nil, err
	}

	return &model.LoginResult{
		AccessToken:  accessToken,
		ExpiresIn:    int64(s.tokens.AccessTTL().Seconds()),
		TokenType:    model.TokenTypeBearer,
		User:         toUserBrief(u),
		Roles:        roleCodes(roles),
		Permissions:  perms,
		RefreshToken: refreshToken,
		RefreshTTL:   refreshTTL,
	}, nil
}

// enforceSessionLimit 超出并发上限时淘汰最旧会话。
func (s *Service) enforceSessionLimit(ctx context.Context, userID, keepID int64, now time.Time) error {
	if s.cfg.SessionMaxPerUser <= 0 {
		return nil
	}
	active, err := s.sessions.ActiveOfUser(ctx, userID, now)
	if err != nil {
		return err
	}
	for i := 0; len(active)-i > s.cfg.SessionMaxPerUser; i++ {
		if active[i].ID == keepID {
			continue
		}
		if err := s.sessions.Revoke(ctx, active[i].ID, model.RevokeReasonKickout, now); err != nil {
			return err
		}
		if err := s.revokes.Revoke(ctx, active[i].JTI, time.Until(active[i].AccessExpireAt)); err != nil {
			return err
		}
	}
	return nil
}

// permissionsOf 装载权限点；超级管理员直接返回通配。
func (s *Service) permissionsOf(ctx context.Context, u *model.SysUser) ([]string, error) {
	if u.IsSuper {
		return []string{model.PermissionAll}, nil
	}
	codes, err := s.roles.PermissionCodesOfUser(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	if codes == nil {
		codes = []string{}
	}
	return codes, nil
}

func (s *Service) refreshTTL(remember bool) time.Duration {
	if remember {
		return s.cfg.RememberTTL
	}
	return s.cfg.RefreshTTL
}

func toUserBrief(u *model.SysUser) model.UserBrief {
	var avatar *string
	if u.Avatar != "" {
		v := u.Avatar
		avatar = &v
	}
	return model.UserBrief{
		ID:                 u.ID,
		Username:           u.Username,
		Nickname:           u.Nickname,
		Avatar:             avatar,
		Email:              u.Email,
		IsSuper:            u.IsSuper,
		Language:           u.Lang,
		Theme:              defaultThemePreference,
		MustChangePassword: u.MustChangePassword,
		TwoFABound:         u.TwoFactorEnabled,
		LastLoginAt:        u.LastLoginAt,
		LastLoginIP:        u.LastLoginIP,
	}
}

func roleCodes(roles []model.SysRole) []string {
	out := make([]string, 0, len(roles))
	for _, r := range roles {
		out = append(out, r.Code)
	}
	return out
}

// pwdUpdatedAt 返回用于 pwd claim 的时间；从未改密时退回创建时间。
func pwdUpdatedAt(u *model.SysUser) time.Time {
	if u.PasswordUpdatedAt != nil {
		return *u.PasswordUpdatedAt
	}
	return u.CreatedAt
}

func deviceType(v string) string {
	if v == "" {
		return "web"
	}
	return v
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// UserIDFromSubject 解析 JWT sub 为用户主键，供中间件复用。
func UserIDFromSubject(sub string) (int64, error) {
	id, err := strconv.ParseInt(sub, 10, 64)
	if err != nil {
		return 0, errs.Wrap(err, errs.CodeUnauthorized, "登录凭证无效")
	}
	return id, nil
}
