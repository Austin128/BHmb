package site

import (
	"embed"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/novapanel/novapanel/internal/model"
	"github.com/novapanel/novapanel/internal/pkg/errs"
)

// rewriteFS 打包内置伪静态规则库（docs/07 7.8.1）。
//
//go:embed rewrite/*.conf
var rewriteFS embed.FS

// RewriteTemplate 是一条可选的内置规则。
type RewriteTemplate struct {
	Code    string `json:"code"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

// rewriteNames 给内置规则一个展示名，顺序即下拉框顺序。
var rewriteNames = []struct {
	Code string
	Name string
}{
	{"none", "不启用"},
	{"wordpress", "WordPress"},
	{"thinkphp", "ThinkPHP 单入口"},
	{"thinkphp_multi", "ThinkPHP 多入口"},
	{"laravel", "Laravel"},
	{"yii2", "Yii2"},
	{"codeigniter", "CodeIgniter"},
	{"typecho", "Typecho"},
	{"discuz", "Discuz! X3"},
	{"dedecms", "织梦 CMS"},
	{"empirecms", "帝国 CMS"},
	{"spa", "单页应用（history）"},
}

// RewriteTemplates 返回内置规则库。内容缺失的模板直接跳过，
// 避免因为漏打包一个文件导致整个接口报错。
func RewriteTemplates() []RewriteTemplate {
	out := make([]RewriteTemplate, 0, len(rewriteNames))
	for _, it := range rewriteNames {
		if it.Code == model.RewriteTemplateNone {
			out = append(out, RewriteTemplate{Code: it.Code, Name: it.Name, Content: ""})
			continue
		}
		b, err := rewriteFS.ReadFile(path.Join("rewrite", it.Code+".conf"))
		if err != nil {
			continue
		}
		out = append(out, RewriteTemplate{Code: it.Code, Name: it.Name, Content: string(b)})
	}
	return out
}

// RewriteTemplateContent 返回指定内置模板的正文。
func RewriteTemplateContent(code string) (string, bool) {
	if code == model.RewriteTemplateNone {
		return "", true
	}
	for _, t := range RewriteTemplates() {
		if t.Code == code {
			return t.Content, true
		}
	}
	return "", false
}

// 伪静态正文的规模上限（docs/07 7.8.2）。
const (
	rewriteMaxBytes = 64 * 1024
	rewriteMaxLines = 500
	rewriteMaxDepth = 5
)

// rewriteAllowedDirectives 是指令白名单。proxy_pass 仅在站点已开启反代时放行，
// 因此不在这个集合里，由 validateRewrite 按站点类型单独判断。
var rewriteAllowedDirectives = map[string]bool{
	"location": true, "rewrite": true, "if": true, "set": true, "return": true,
	"try_files": true, "add_header": true, "expires": true, "deny": true, "allow": true,
	"index": true, "error_page": true, "internal": true, "break": true,
	"access_log": true, "log_not_found": true, "autoindex": true, "default_type": true,
	"limit_except": true, "types": true, "charset": true, "etag": true,
}

// rewriteDeniedDirectives 是显式黑名单，命中直接拒绝并给出明确原因。
var rewriteDeniedDirectives = map[string]bool{
	"listen": true, "server_name": true, "root": true, "alias": true, "include": true,
	"load_module": true, "perl": true, "user": true, "pid": true, "server": true,
	"http": true, "events": true, "upstream": true,
	"ssl_certificate": true, "ssl_certificate_key": true,
}

var (
	// 取一行里的首个 token 作为指令名；注释与空行提前跳过。
	directiveRe = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)`)
	// 灾难性回溯的典型形态：嵌套量词。
	nestedQuantifierRe = regexp.MustCompile(`\([^)]*[+*]\)[+*]`)
)

// ValidateRewrite 校验伪静态正文：指令白/黑名单 → 结构 → 规模。
// 真实语法由后续 nginx -t 兜底，这里只做毫秒级的静态检查，
// 保证明显非法的内容不会被写进 vhost 目录。
func ValidateRewrite(content, siteType string) error {
	if len(content) > rewriteMaxBytes {
		return errs.Newf(errs.CodeRewriteInvalid, "伪静态规则超过 %dKB 上限", rewriteMaxBytes/1024)
	}
	lines := strings.Split(content, "\n")
	if len(lines) > rewriteMaxLines {
		return errs.Newf(errs.CodeRewriteInvalid, "伪静态规则超过 %d 行上限", rewriteMaxLines)
	}

	depth, maxDepth := 0, 0
	for i, raw := range lines {
		line := strings.TrimSpace(stripRewriteComment(raw))
		if line == "" {
			continue
		}
		for _, ch := range line {
			switch ch {
			case '{':
				depth++
				if depth > maxDepth {
					maxDepth = depth
				}
			case '}':
				depth--
				if depth < 0 {
					return errs.Newf(errs.CodeRewriteInvalid, "第 %d 行花括号不配对", i+1)
				}
			}
		}
		if err := checkRewriteDirective(line, siteType, i+1); err != nil {
			return err
		}
	}
	if depth != 0 {
		return errs.New(errs.CodeRewriteInvalid, "花括号不配对，请检查规则结构")
	}
	if maxDepth > rewriteMaxDepth {
		return errs.Newf(errs.CodeRewriteInvalid, "嵌套深度 %d 超过上限 %d", maxDepth, rewriteMaxDepth)
	}
	if nestedQuantifierRe.MatchString(content) {
		return errs.New(errs.CodeRewriteInvalid, "规则含嵌套量词正则（如 (a+)+），可能触发灾难性回溯，请改写")
	}
	return nil
}

// checkRewriteDirective 检查单行首个 token 是否被允许。
func checkRewriteDirective(line, siteType string, lineNo int) error {
	// 纯 '}' 或以 '}' 结尾的收尾行没有指令。
	if line == "}" || line == "{" {
		return nil
	}
	m := directiveRe.FindStringSubmatch(line)
	if m == nil {
		// 以变量或正则开头（如 ~* \.php$ {）出现在 location 参数续行里，交给 nginx -t。
		return nil
	}
	name := strings.ToLower(m[1])
	if rewriteDeniedDirectives[name] {
		return errs.Newf(errs.CodeRewriteInvalid, "第 %d 行含禁止指令 %s", lineNo, name)
	}
	// 反代站的 location / 由 proxy_pass 占用，伪静态再声明会导致 duplicate location。
	if name == "location" && siteType == model.SiteTypeProxy {
		return errs.Newf(errs.CodeRewriteInvalid, "第 %d 行：反向代理站的伪静态只支持 rewrite/if/set/return 语句", lineNo)
	}
	if name == "proxy_pass" {
		if siteType != model.SiteTypeProxy {
			return errs.Newf(errs.CodeRewriteInvalid, "第 %d 行 proxy_pass 仅反向代理站可用", lineNo)
		}
		return nil
	}
	if strings.HasPrefix(name, "lua") || strings.HasPrefix(name, "proxy_") {
		return errs.Newf(errs.CodeRewriteInvalid, "第 %d 行含禁止指令 %s", lineNo, name)
	}
	if !rewriteAllowedDirectives[name] {
		return errs.Newf(errs.CodeRewriteInvalid, "第 %d 行指令 %s 不在允许列表内", lineNo, name)
	}
	return nil
}

// stripRewriteComment 去掉行尾注释，# 出现在引号内时保留。
func stripRewriteComment(line string) string {
	inQuote := byte(0)
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case inQuote != 0:
			if c == inQuote {
				inQuote = 0
			}
		case c == '"' || c == '\'':
			inQuote = c
		case c == '#':
			return line[:i]
		}
	}
	return line
}

// RewriteTemplateCodes 返回内置模板代码，供参数校验与前端下拉使用。
func RewriteTemplateCodes() []string {
	out := make([]string, 0, len(rewriteNames)+1)
	for _, it := range rewriteNames {
		out = append(out, it.Code)
	}
	out = append(out, model.RewriteTemplateCustom)
	sort.Strings(out)
	return out
}
