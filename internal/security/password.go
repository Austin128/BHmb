package security

import (
	"crypto/rand"
	"math/big"
	"strings"
	"unicode"

	"golang.org/x/crypto/bcrypt"

	"github.com/novapanel/novapanel/internal/pkg/errs"
)

// 口令策略（规范文档未固化数值，此处为青垣面板默认，改动需同步 docs/internal/conventions.md）：
// 最短 10 位、最长 72 位（bcrypt 上限），且至少覆盖 小写/大写/数字/符号 四类中的三类。
const (
	PasswordMinLen = 10
	PasswordMaxLen = 72
)

// weakPasswords 为明显弱口令，命中即拒绝。
var weakPasswords = map[string]struct{}{
	"password":      {},
	"passw0rd":      {},
	"12345678":      {},
	"123456789":     {},
	"1234567890":    {},
	"qwertyuiop":    {},
	"adminadmin":    {},
	"novapanel":     {},
	"letmein123":    {},
	"iloveyou123":   {},
	"administrator": {},
}

// Hasher 使用 bcrypt 哈希口令。cost 由 security.bcrypt_cost 提供，默认 12。
type Hasher struct {
	cost int
}

// NewHasher 构造 Hasher；cost 超出 bcrypt 允许范围时回落到 bcrypt.DefaultCost。
func NewHasher(cost int) *Hasher {
	if cost < bcrypt.MinCost || cost > bcrypt.MaxCost {
		cost = 12
	}
	return &Hasher{cost: cost}
}

// Cost 返回实际使用的 bcrypt cost。
func (h *Hasher) Cost() int { return h.cost }

// Hash 生成口令哈希。调用方必须先通过 CheckStrength 校验强度。
func (h *Hasher) Hash(password string) (string, error) {
	if len(password) > PasswordMaxLen {
		return "", errs.Newf(errs.CodeWeakPassword, "密码长度不能超过 %d 位", PasswordMaxLen)
	}
	b, err := bcrypt.GenerateFromPassword([]byte(password), h.cost)
	if err != nil {
		return "", errs.Wrap(err, errs.CodeInternal, "密码哈希失败")
	}
	return string(b), nil
}

// Verify 校验口令。为避免用户枚举，调用方应对失败统一返回 110004。
func (h *Hasher) Verify(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// NeedsRehash 判断既有哈希的 cost 是否低于当前配置，登录成功后可顺带升级。
func (h *Hasher) NeedsRehash(hash string) bool {
	cost, err := bcrypt.Cost([]byte(hash))
	if err != nil {
		return false
	}
	return cost < h.cost
}

// randomPasswordAlphabet 去掉了 O/0、l/1/I 等易混淆字符，便于安装报告中人工抄录。
const randomPasswordAlphabet = "abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789!@#$%^&*-_=+"

// RandomPassword 生成满足强度策略的随机口令，用于首次启动的初始管理员账号。
// 生成失败属于熵源故障，必须向上冒泡而不是回落到弱口令。
func RandomPassword(length int) (string, error) {
	if length < PasswordMinLen {
		length = PasswordMinLen
	}
	if length > PasswordMaxLen {
		length = PasswordMaxLen
	}
	alphabet := []byte(randomPasswordAlphabet)
	limit := big.NewInt(int64(len(alphabet)))

	// 最多重试若干次直到通过强度校验：随机串偶发缺少某类字符
	for attempt := 0; attempt < 16; attempt++ {
		buf := make([]byte, length)
		for i := range buf {
			n, err := rand.Int(rand.Reader, limit)
			if err != nil {
				return "", errs.Wrap(err, errs.CodeInternal, "随机口令生成失败")
			}
			buf[i] = alphabet[n.Int64()]
		}
		candidate := string(buf)
		if CheckStrength(candidate) == nil {
			return candidate, nil
		}
	}
	return "", errs.New(errs.CodeInternal, "随机口令生成失败：多次未满足强度策略")
}

// CheckStrength 校验口令强度，不满足时返回 110001。
func CheckStrength(password string) error {
	if len(password) < PasswordMinLen {
		return errs.Newf(errs.CodeWeakPassword, "密码至少 %d 位", PasswordMinLen)
	}
	if len(password) > PasswordMaxLen {
		return errs.Newf(errs.CodeWeakPassword, "密码长度不能超过 %d 位", PasswordMaxLen)
	}
	if _, ok := weakPasswords[strings.ToLower(password)]; ok {
		return errs.New(errs.CodeWeakPassword, "密码过于常见，请更换")
	}

	var lower, upper, digit, symbol bool
	for _, r := range password {
		switch {
		case unicode.IsLower(r):
			lower = true
		case unicode.IsUpper(r):
			upper = true
		case unicode.IsDigit(r):
			digit = true
		default:
			symbol = true
		}
	}
	classes := 0
	for _, ok := range []bool{lower, upper, digit, symbol} {
		if ok {
			classes++
		}
	}
	if classes < 3 {
		return errs.New(errs.CodeWeakPassword, "密码需包含小写字母、大写字母、数字、符号中的至少三类")
	}
	return nil
}
