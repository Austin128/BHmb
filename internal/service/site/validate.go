package site

import (
	"net"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/novapanel/novapanel/internal/pkg/errs"
)

// 站点名同时作为 vhost 文件名与默认目录名，因此只允许小写字母、数字、下划线和短横线。
var siteNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{1,62}$`)

// 域名正则取自 docs/05 5.4：支持一级泛域名前缀，不允许纯数字顶级域。
var domainRe = regexp.MustCompile(`^(\*\.)?([a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,63}$`)

// 面板自身的端口不允许被站点占用或反代，避免把管理入口暴露成站点。
const panelPortDefault = 34567

// ValidateName 校验站点标识。
func ValidateName(name string) error {
	if !siteNameRe.MatchString(name) {
		return errs.New(errs.CodeInvalidParam,
			"站点标识需 2-63 位，仅允许小写字母、数字、下划线和短横线，且以字母或数字开头")
	}
	return nil
}

// NormalizeDomain 归一化域名：去空白、转小写、去末尾点。localhost 与 IP 直连也允许，
// 因为内网站点与临时调试确实会用它们。
func NormalizeDomain(d string) (string, error) {
	d = strings.ToLower(strings.TrimSpace(d))
	d = strings.TrimSuffix(d, ".")
	if d == "" {
		return "", errs.New(errs.CodeDomainInvalid, "域名不能为空")
	}
	if len(d) > 253 {
		return "", errs.New(errs.CodeDomainInvalid, "域名总长不能超过 253 个字符")
	}
	if d == "localhost" || net.ParseIP(d) != nil {
		return d, nil
	}
	if !domainRe.MatchString(d) {
		return "", errs.Newf(errs.CodeDomainInvalid, "域名格式不合法：%s", d)
	}
	if strings.Contains(strings.TrimPrefix(d, "*."), "*") {
		return "", errs.New(errs.CodeDomainInvalid, "泛域名只允许 *. 作为最左一级")
	}
	return d, nil
}

// ValidatePort 校验站点监听端口，并拒绝占用面板端口。
func ValidatePort(port, panelPort int) error {
	if port < 1 || port > 65535 {
		return errs.Newf(errs.CodeInvalidParam, "端口需在 1-65535 之间，当前 %d", port)
	}
	if panelPort == 0 {
		panelPort = panelPortDefault
	}
	if port == panelPort {
		return errs.Newf(errs.CodePortOccupied, "端口 %d 是面板管理端口，不能用于站点", port)
	}
	return nil
}

// ValidateRoot 校验站点根目录：必须是绝对路径、清理后无 .. 且落在允许的父目录下。
// allowedRoots 为空表示不限制父目录（由文件模块的白名单继续兜底）。
func ValidateRoot(root string, allowedRoots []string) (string, error) {
	if root == "" {
		return "", errs.New(errs.CodeInvalidParam, "站点根目录不能为空")
	}
	if !filepath.IsAbs(root) {
		return "", errs.New(errs.CodeInvalidParam, "站点根目录必须是绝对路径")
	}
	clean := filepath.Clean(root)
	if clean == "/" {
		return "", errs.New(errs.CodeInvalidParam, "站点根目录不能是根分区 /")
	}
	if len(allowedRoots) == 0 {
		return clean, nil
	}
	for _, base := range allowedRoots {
		base = filepath.Clean(base)
		if clean == base || strings.HasPrefix(clean, base+string(filepath.Separator)) {
			return clean, nil
		}
	}
	return "", errs.Newf(errs.CodeInvalidParam,
		"站点根目录必须位于 %s 之内", strings.Join(allowedRoots, "、"))
}

// ValidateProxyTarget 校验反代目标。禁止指向面板自身，否则等于把管理后台无鉴权地代理出去。
func ValidateProxyTarget(raw string, panelPort int) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errs.New(errs.CodeInvalidParam, "反向代理目标地址不能为空")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", errs.New(errs.CodeInvalidParam, "反向代理目标需形如 http://127.0.0.1:3000")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", errs.New(errs.CodeInvalidParam, "反向代理目标仅支持 http 或 https")
	}
	if panelPort == 0 {
		panelPort = panelPortDefault
	}
	host := u.Hostname()
	portStr := u.Port()
	port := 80
	if u.Scheme == "https" {
		port = 443
	}
	if portStr != "" {
		p, convErr := strconv.Atoi(portStr)
		if convErr != nil || p < 1 || p > 65535 {
			return "", errs.New(errs.CodeInvalidParam, "反向代理目标端口不合法")
		}
		port = p
	}
	if isLoopback(host) && port == panelPort {
		return "", errs.Newf(errs.CodeInvalidParam,
			"不能把站点反向代理到面板自身（%s:%d）", host, panelPort)
	}
	// 去掉多余的末尾斜杠：proxy_pass 带路径与不带路径语义不同，这里统一为无路径形式。
	u.Path = strings.TrimSuffix(u.Path, "/")
	return u.String(), nil
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// ParseIndexFiles 归一化默认文档列表，去重并保留顺序。
func ParseIndexFiles(raw []string, siteType string) []string {
	if len(raw) == 0 {
		if siteType == "php" {
			return []string{"index.php", "index.html", "index.htm"}
		}
		return []string{"index.html", "index.htm"}
	}
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, f := range raw {
		f = strings.TrimSpace(f)
		// 默认文档只能是文件名，带路径分隔符会让 Nginx 解析出预期之外的目标。
		if f == "" || strings.ContainsAny(f, "/\\ ") {
			continue
		}
		if _, ok := seen[f]; ok {
			continue
		}
		seen[f] = struct{}{}
		out = append(out, f)
	}
	if len(out) == 0 {
		return ParseIndexFiles(nil, siteType)
	}
	return out
}
