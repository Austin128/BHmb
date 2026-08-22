package security

import (
	"strings"

	"github.com/novapanel/novapanel/internal/pkg/errs"
)

// 用户名策略（与口令策略同为青垣面板默认值，改动需同步 docs/12）：
// 3-32 位、必须以字母开头、只允许 ASCII 字母数字与 . _ -，且不以标点结尾。
// 限制成 ASCII 是为了避免全角字符、同形字带来的登录歧义；
// 上限远低于 sys_user.username 的 VARCHAR(64)，留出前后缀余量。
const (
	UsernameMinLen = 3
	UsernameMaxLen = 32
)

// reservedUsernames 是与系统语义容易混淆的名字，禁止占用。
// 注意不含 admin：初始管理员就叫 admin，允许继续使用与改名。
var reservedUsernames = map[string]struct{}{
	"system":    {},
	"anonymous": {},
	"nobody":    {},
	"novapanel": {},
	"qingyuan":  {},
}

// CheckUsername 校验用户名格式，不合法时返回 CodeInvalidParam。
func CheckUsername(name string) error {
	if name != strings.TrimSpace(name) {
		return errs.New(errs.CodeInvalidParam, "用户名首尾不能有空白字符")
	}
	if len(name) < UsernameMinLen || len(name) > UsernameMaxLen {
		return errs.Newf(errs.CodeInvalidParam, "用户名需 %d-%d 位", UsernameMinLen, UsernameMaxLen)
	}
	if _, ok := reservedUsernames[strings.ToLower(name)]; ok {
		return errs.Newf(errs.CodeInvalidParam, "用户名 %s 为保留名，请换一个", name)
	}

	var prevPunct bool
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
			prevPunct = false
		case c >= '0' && c <= '9':
			if i == 0 {
				return errs.New(errs.CodeInvalidParam, "用户名必须以字母开头")
			}
			prevPunct = false
		case c == '.' || c == '_' || c == '-':
			if i == 0 {
				return errs.New(errs.CodeInvalidParam, "用户名必须以字母开头")
			}
			if prevPunct {
				return errs.New(errs.CodeInvalidParam, "用户名中的 . _ - 不能连续出现")
			}
			if i == len(name)-1 {
				return errs.New(errs.CodeInvalidParam, "用户名不能以 . _ - 结尾")
			}
			prevPunct = true
		default:
			return errs.New(errs.CodeInvalidParam, "用户名只能包含字母、数字与 . _ -")
		}
	}
	return nil
}

// SameUsername 判断两个用户名是否视为同一个。
// SQLite 默认大小写不敏感、MySQL 取决于排序规则、Postgres 大小写敏感，
// 各驱动行为不一致，因此统一按不区分大小写判定，避免出现 Admin 与 admin 并存。
func SameUsername(a, b string) bool { return strings.EqualFold(a, b) }
