package site

import (
	"fmt"
	"net"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/novapanel/novapanel/internal/model"
	"github.com/novapanel/novapanel/internal/pkg/errs"
)

// 设置项的规模上限，避免单站点生成出体积失控的配置。
const (
	maxRedirects       = 20
	maxAccessIPs       = 100
	maxDenyUserAgents  = 30
	maxAntiLeechExt    = 30
	maxAntiLeechRefers = 50
	maxRunPathDepth    = 8
)

// cacheExtensions 是静态缓存默认覆盖的扩展名，顺序固定以保证渲染结果可哈希比对。
var cacheExtensions = []string{
	"js", "css", "png", "jpg", "jpeg", "gif", "webp", "avif", "ico",
	"svg", "woff", "woff2", "ttf", "eot", "mp4", "webm", "mp3",
}

var (
	extRe     = regexp.MustCompile(`^[A-Za-z0-9]{1,10}$`)
	refererRe = regexp.MustCompile(`^\*?[A-Za-z0-9._-]+$`)
	// 运行目录只允许简单的路径片段，避免 .. 逃逸与空格导致的配置歧义。
	runPathSegRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
	// UA 片段禁止的字符：会破坏 if 语句结构或注入新指令。
	uaForbidden = `"';{}$\`
)

// NormalizeSettings 校验并归一化站点设置。返回值可直接序列化入库，
// 所有渲染期需要的判断都在这里完成，模板与渲染函数不再做业务校验。
func NormalizeSettings(in model.SiteSettings, siteType string) (model.SiteSettings, error) {
	out := model.DefaultSiteSettings()

	runPath, err := normalizeRunPath(in.RunPath)
	if err != nil {
		return out, err
	}
	out.RunPath = runPath
	out.AutoIndex = in.AutoIndex

	switch in.CacheProfile {
	case "", model.CacheProfileNone:
		out.CacheProfile = model.CacheProfileNone
	case model.CacheProfileHour, model.CacheProfileDay, model.CacheProfileMonth, model.CacheProfileYear:
		out.CacheProfile = in.CacheProfile
	default:
		return out, errs.Newf(errs.CodeInvalidParam, "不支持的缓存档位：%s", in.CacheProfile)
	}

	al, err := normalizeAntiLeech(in.AntiLeech)
	if err != nil {
		return out, err
	}
	out.AntiLeech = al

	ac, err := normalizeAccessControl(in.AccessControl)
	if err != nil {
		return out, err
	}
	out.AccessControl = ac

	rd, err := normalizeRedirects(in.Redirects, siteType)
	if err != nil {
		return out, err
	}
	out.Redirects = rd
	return out, nil
}

// normalizeRunPath 归一化运行目录：统一为 /sub/dir 形式，空表示使用站点根。
func normalizeRunPath(in string) (string, error) {
	s := strings.TrimSpace(in)
	if s == "" || s == "/" {
		return "", nil
	}
	s = strings.ReplaceAll(s, "\\", "/")
	if !strings.HasPrefix(s, "/") {
		s = "/" + s
	}
	cleaned := path.Clean(s)
	if cleaned == "/" {
		return "", nil
	}
	segs := strings.Split(strings.TrimPrefix(cleaned, "/"), "/")
	if len(segs) > maxRunPathDepth {
		return "", errs.Newf(errs.CodeInvalidParam, "运行目录层级不能超过 %d 级", maxRunPathDepth)
	}
	for _, seg := range segs {
		if seg == ".." || !runPathSegRe.MatchString(seg) {
			return "", errs.Newf(errs.CodeInvalidParam, "运行目录含非法片段：%s", seg)
		}
	}
	return "/" + strings.Join(segs, "/"), nil
}

// normalizeAntiLeech 校验防盗链设置。
func normalizeAntiLeech(in model.AntiLeechSetting) (model.AntiLeechSetting, error) {
	out := model.AntiLeechSetting{ReturnCode: 403}
	if !in.Enabled {
		return out, nil
	}
	switch in.ReturnCode {
	case 0, 403:
		out.ReturnCode = 403
	case 404, 444:
		out.ReturnCode = in.ReturnCode
	default:
		return out, errs.Newf(errs.CodeInvalidParam, "防盗链返回码只支持 403/404/444，当前为 %d", in.ReturnCode)
	}

	exts := dedupeStrings(in.Extensions, strings.ToLower)
	if len(exts) == 0 {
		return out, errs.New(errs.CodeInvalidParam, "防盗链需要至少指定一个文件扩展名")
	}
	if len(exts) > maxAntiLeechExt {
		return out, errs.Newf(errs.CodeInvalidParam, "防盗链扩展名不能超过 %d 个", maxAntiLeechExt)
	}
	for _, e := range exts {
		if !extRe.MatchString(e) {
			return out, errs.Newf(errs.CodeInvalidParam, "非法扩展名：%s", e)
		}
	}

	refs := dedupeStrings(in.AllowedReferers, strings.ToLower)
	if len(refs) > maxAntiLeechRefers {
		return out, errs.Newf(errs.CodeInvalidParam, "放行来源不能超过 %d 个", maxAntiLeechRefers)
	}
	for _, r := range refs {
		if !refererRe.MatchString(r) {
			return out, errs.Newf(errs.CodeInvalidParam, "非法放行来源：%s", r)
		}
	}

	out.Enabled = true
	out.Extensions = exts
	out.AllowedReferers = refs
	out.AllowEmptyReferer = in.AllowEmptyReferer
	return out, nil
}

// normalizeAccessControl 校验访问限制：IP 支持单地址与 CIDR，UA 为受限正则片段。
func normalizeAccessControl(in model.AccessControlSetting) (model.AccessControlSetting, error) {
	out := model.AccessControlSetting{}
	if !in.Enabled {
		return out, nil
	}
	allow, err := normalizeIPList(in.AllowIPs, "放行")
	if err != nil {
		return out, err
	}
	deny, err := normalizeIPList(in.DenyIPs, "拒绝")
	if err != nil {
		return out, err
	}
	uas := dedupeStrings(in.DenyUserAgents, strings.TrimSpace)
	if len(uas) > maxDenyUserAgents {
		return out, errs.Newf(errs.CodeInvalidParam, "User-Agent 规则不能超过 %d 条", maxDenyUserAgents)
	}
	for _, ua := range uas {
		if strings.ContainsAny(ua, uaForbidden) {
			return out, errs.Newf(errs.CodeInvalidParam, "User-Agent 规则含非法字符：%s", ua)
		}
		if len(ua) > 128 {
			return out, errs.New(errs.CodeInvalidParam, "单条 User-Agent 规则不能超过 128 字符")
		}
		if _, err := regexp.Compile(ua); err != nil {
			return out, errs.Newf(errs.CodeInvalidParam, "User-Agent 规则不是合法正则：%s", ua)
		}
	}
	if len(allow) == 0 && len(deny) == 0 && len(uas) == 0 {
		return out, errs.New(errs.CodeInvalidParam, "启用访问限制时至少需要一条规则")
	}
	out.Enabled = true
	out.AllowIPs = allow
	out.DenyIPs = deny
	out.DenyUserAgents = uas
	return out, nil
}

// normalizeIPList 校验 IP / CIDR 列表。
func normalizeIPList(in []string, label string) ([]string, error) {
	list := dedupeStrings(in, strings.TrimSpace)
	if len(list) > maxAccessIPs {
		return nil, errs.Newf(errs.CodeInvalidParam, "%s名单不能超过 %d 条", label, maxAccessIPs)
	}
	for _, item := range list {
		if strings.Contains(item, "/") {
			if _, _, err := net.ParseCIDR(item); err != nil {
				return nil, errs.Newf(errs.CodeInvalidParam, "%s名单中的 %s 不是合法 CIDR", label, item)
			}
			continue
		}
		if net.ParseIP(item) == nil {
			return nil, errs.Newf(errs.CodeInvalidParam, "%s名单中的 %s 不是合法 IP", label, item)
		}
	}
	return list, nil
}

// normalizeRedirects 校验重定向列表。目标必须是 http/https 绝对地址，
// 且不能指向面板自身端口以外的本机回环（避免把面板暴露成站点内容）。
func normalizeRedirects(in []model.RedirectRule, siteType string) ([]model.RedirectRule, error) {
	out := make([]model.RedirectRule, 0, len(in))
	if len(in) > maxRedirects {
		return nil, errs.Newf(errs.CodeInvalidParam, "重定向规则不能超过 %d 条", maxRedirects)
	}
	seen := make(map[string]struct{}, len(in))
	for i := range in {
		r := in[i]
		p := strings.TrimSpace(r.Path)
		if p == "" {
			p = "/"
		}
		if !strings.HasPrefix(p, "/") {
			return nil, errs.Newf(errs.CodeInvalidParam, "重定向路径必须以 / 开头：%s", p)
		}
		if strings.ContainsAny(p, " \t\"'{};$\\") {
			return nil, errs.Newf(errs.CodeInvalidParam, "重定向路径含非法字符：%s", p)
		}
		if p != "/" {
			p = strings.TrimRight(p, "/")
		}
		if _, ok := seen[p]; ok {
			return nil, errs.Newf(errs.CodeInvalidParam, "重定向路径重复：%s", p)
		}
		seen[p] = struct{}{}

		target := strings.TrimSpace(r.Target)
		u, err := url.Parse(target)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return nil, errs.Newf(errs.CodeInvalidParam, "重定向目标必须是 http/https 绝对地址：%s", target)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return nil, errs.Newf(errs.CodeInvalidParam, "重定向目标仅支持 http/https：%s", target)
		}
		if strings.ContainsAny(target, " \t\"'{};\\") {
			return nil, errs.Newf(errs.CodeInvalidParam, "重定向目标含非法字符：%s", target)
		}
		code := r.Code
		if code == 0 {
			code = model.RedirectCodePermanent
		}
		if code != model.RedirectCodePermanent && code != model.RedirectCodeTemporary {
			return nil, errs.Newf(errs.CodeInvalidParam, "重定向状态码只支持 301/302，当前为 %d", code)
		}
		remark := strings.TrimSpace(r.Remark)
		if len(remark) > 255 {
			return nil, errs.New(errs.CodeInvalidParam, "重定向备注不能超过 255 字符")
		}
		out = append(out, model.RedirectRule{
			Enabled:   r.Enabled,
			Path:      p,
			Target:    strings.TrimRight(target, "/"),
			Code:      code,
			KeepPath:  r.KeepPath,
			KeepQuery: r.KeepQuery,
			Remark:    remark,
		})
	}
	// 反代站的整站重定向会让 proxy_pass 永远不生效，属于配置错误而非期望行为。
	if siteType == model.SiteTypeProxy {
		for _, r := range out {
			if r.Enabled && r.Path == "/" {
				return nil, errs.New(errs.CodeInvalidParam, "反向代理站不能配置整站重定向，否则代理不会生效")
			}
		}
	}
	return out, nil
}

// NormalizeRateLimit 校验限速设置。
func NormalizeRateLimit(in model.RateLimitSetting) (model.RateLimitSetting, error) {
	out := model.RateLimitSetting{}
	if !in.Enabled {
		return out, nil
	}
	if in.BandwidthKB <= 0 {
		return out, errs.New(errs.CodeInvalidParam, "启用限速时带宽必须大于 0 KB/s")
	}
	if in.BandwidthKB > 1024*1024 {
		return out, errs.New(errs.CodeInvalidParam, "带宽上限不能超过 1048576 KB/s")
	}
	if in.BurstKB < 0 {
		return out, errs.New(errs.CodeInvalidParam, "突发阈值不能为负数")
	}
	if in.BurstKB > 1024*1024 {
		return out, errs.New(errs.CodeInvalidParam, "突发阈值不能超过 1048576 KB")
	}
	out.Enabled = true
	out.BandwidthKB = in.BandwidthKB
	out.BurstKB = in.BurstKB
	return out, nil
}

// applySettings 把归一化后的设置写入渲染上下文。
// 这里承担全部条件判断，模板只负责按字段输出文本。
func applySettings(ctx *RenderCtx, root string, s model.SiteSettings, rl model.RateLimitSetting, rewritePath string) {
	if s.RunPath != "" {
		// 运行目录已校验为安全片段，直接拼到站点根之后。
		ctx.Root = path.Join(root, s.RunPath)
	}
	ctx.AutoIndex = s.AutoIndex

	for _, r := range s.Redirects {
		if !r.Enabled {
			continue
		}
		ctx.Redirects = append(ctx.Redirects, buildRedirect(r))
	}

	if s.AccessControl.Enabled {
		ctx.AllowIPs = s.AccessControl.AllowIPs
		ctx.DenyIPs = s.AccessControl.DenyIPs
		// AllowIPs 非空意味着白名单模式，必须补 deny all 才能生效。
		ctx.DenyAllOthers = len(s.AccessControl.AllowIPs) > 0
		if len(s.AccessControl.DenyUserAgents) > 0 {
			ctx.DenyUAPattern = strings.Join(s.AccessControl.DenyUserAgents, "|")
		}
	}

	cacheExts := cacheExtensions
	if s.AntiLeech.Enabled {
		ctx.AntiLeech = &RenderAntiLeech{
			ExtPattern:      strings.Join(s.AntiLeech.Extensions, "|"),
			ValidReferers:   antiLeechReferers(s.AntiLeech),
			ReturnCode:      s.AntiLeech.ReturnCode,
			ReturnDirective: antiLeechReturn(s.AntiLeech.ReturnCode),
		}
		// 防盗链与缓存都靠扩展名正则 location 匹配，重叠会让后声明的那个永不生效，
		// 因此缓存扩展集合要剔除防盗链已接管的扩展，并把缓存指令并入防盗链块。
		cacheExts = subtractStrings(cacheExtensions, s.AntiLeech.Extensions)
	}
	if expires, ok := model.CacheProfileExpires[s.CacheProfile]; ok {
		ctx.CacheExpires = expires
		if len(cacheExts) > 0 {
			ctx.CacheExtPattern = strings.Join(cacheExts, "|")
		}
		if ctx.AntiLeech != nil {
			ctx.AntiLeech.Expires = expires
		}
	}

	if rl.Enabled {
		ctx.LimitRate = fmt.Sprintf("%dk", rl.BandwidthKB)
		if rl.BurstKB > 0 {
			ctx.LimitRateAfter = fmt.Sprintf("%dk", rl.BurstKB)
		}
	}
	ctx.RewriteInclude = rewritePath
}

// buildRedirect 把一条规则翻译成 nginx 语句。
// KeepQuery 为假时在替换串末尾补 ?，这是 nginx 丢弃原查询串的写法。
func buildRedirect(r model.RedirectRule) RenderRedirect {
	flag := "permanent"
	if r.Code == model.RedirectCodeTemporary {
		flag = "redirect"
	}
	suffix := "?"
	if r.KeepQuery {
		suffix = ""
	}
	prefix := r.Path
	if prefix == "/" {
		prefix = ""
	}
	pattern := "^" + regexp.QuoteMeta(prefix)
	replacement := r.Target
	if r.KeepPath {
		pattern += "(.*)$"
		replacement += "$1"
	} else {
		pattern += "(?:/.*)?$"
	}
	return RenderRedirect{
		Path:      r.Path,
		Statement: fmt.Sprintf("rewrite %s %s%s %s;", pattern, replacement, suffix, flag),
		Remark:    r.Remark,
	}
}

// antiLeechReferers 组装 valid_referers 参数。
// server_names 让站点自身域名始终有效；none/blocked 只在允许空 Referer 时加入。
func antiLeechReferers(s model.AntiLeechSetting) []string {
	out := make([]string, 0, len(s.AllowedReferers)+3)
	if s.AllowEmptyReferer {
		out = append(out, "none", "blocked")
	}
	out = append(out, "server_names")
	out = append(out, s.AllowedReferers...)
	return out
}

// antiLeechReturn 生成命中盗链时的动作。444 是 Nginx 的直接断开连接。
func antiLeechReturn(code int) string {
	return fmt.Sprintf("return %d;", code)
}

// dedupeStrings 归一化 + 去重 + 去空，保持稳定顺序（排序）以便配置哈希稳定。
func dedupeStrings(in []string, norm func(string) string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		v := norm(strings.TrimSpace(raw))
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// subtractStrings 返回 base 中不在 remove 内的元素，比较不区分大小写。
func subtractStrings(base, remove []string) []string {
	skip := make(map[string]struct{}, len(remove))
	for _, r := range remove {
		skip[strings.ToLower(r)] = struct{}{}
	}
	out := make([]string, 0, len(base))
	for _, b := range base {
		if _, ok := skip[strings.ToLower(b)]; ok {
			continue
		}
		out = append(out, b)
	}
	return out
}
