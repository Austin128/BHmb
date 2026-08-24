package site

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/novapanel/novapanel/internal/pkg/errs"
)

// builtin 打包内置 vhost 模板，使二进制无需随包分发模板目录。
//
//go:embed tmpl/*/*.tmpl
var builtin embed.FS

// RenderCtx 是 vhost 模板的唯一入参。字段全部为渲染所需的成品值，
// 模板里不做业务判断，避免同一逻辑在模板与服务层各写一遍。
type RenderCtx struct {
	Name        string
	Type        string
	GeneratedAt string
	// ServerNames 为 server_name 列表，主域名在前。
	ServerNames []string
	// ListenPorts 为去重后的 HTTP 监听端口。
	ListenPorts []int
	Root        string
	IndexFiles  []string

	AccessLogEnabled bool
	ErrorLogEnabled  bool
	AccessLog        string
	ErrorLog         string

	// ProxyTarget 为 proxy_pass 目标，ProxyHostHeader 为回源 Host 头（含 $host 变量形式）。
	ProxyTarget     string
	ProxyHostHeader string
	ProxyWebSocket  bool

	// 以下字段来自站点设置（docs/07 7.10.1），全部为可直接输出的成品值。
	// Redirects 为已翻译成 nginx 语句的重定向规则。
	Redirects []RenderRedirect
	// AllowIPs / DenyIPs 为访问限制名单，DenyAllOthers 为白名单模式下的兜底 deny all。
	AllowIPs      []string
	DenyIPs       []string
	DenyAllOthers bool
	// DenyUAPattern 为拼好的 User-Agent 拦截正则，空表示不启用。
	DenyUAPattern string
	// AntiLeech 为空表示不启用防盗链。
	AntiLeech *RenderAntiLeech
	// CacheExtPattern 与 CacheExpires 同时非空时才输出静态缓存 location。
	CacheExtPattern string
	CacheExpires    string
	// AutoIndex 为真时开启目录浏览。
	AutoIndex bool
	// LimitRate / LimitRateAfter 为 nginx 带宽限制值，如 512k。
	LimitRate      string
	LimitRateAfter string
	// RewriteInclude 为伪静态片段绝对路径，空表示无伪静态。
	RewriteInclude string
}

// RenderRedirect 是一条已翻译好的重定向语句。
type RenderRedirect struct {
	Path      string
	Statement string
	Remark    string
}

// RenderAntiLeech 是防盗链渲染参数。
type RenderAntiLeech struct {
	ExtPattern      string
	ValidReferers   []string
	ReturnCode      int
	ReturnDirective string
	// Expires 非空时把静态缓存指令并入防盗链 location，避免正则 location 重叠。
	Expires string
}

// Renderer 渲染 vhost 配置。overrideDir 下的同名模板覆盖内置模板，
// 便于管理员按站点规范定制而无需改二进制。
type Renderer struct {
	tpl *template.Template
}

var funcMap = template.FuncMap{
	// joinStr 用于 server_name 与 index 这类空格分隔列表。
	"joinStr": strings.Join,
}

// NewRenderer 解析内置模板，并在 overrideDir 存在同名模板时覆盖。
func NewRenderer(overrideDir string) (*Renderer, error) {
	t := template.New("vhost").Option("missingkey=error").Funcs(funcMap)
	t, err := t.ParseFS(builtin, "tmpl/*/*.tmpl")
	if err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "内置 vhost 模板解析失败")
	}
	if overrideDir != "" {
		// 覆盖目录缺失或没有模板都是正常情况，只在解析出错时报错。
		matches, _ := filepath.Glob(filepath.Join(overrideDir, "*", "*.tmpl"))
		if len(matches) > 0 {
			t2, err := t.ParseFiles(matches...)
			if err != nil {
				return nil, errs.Wrapf(err, errs.CodeInvalidParam, "自定义 vhost 模板解析失败（目录 %s）", overrideDir)
			}
			t = t2
		}
	}
	return &Renderer{tpl: t}, nil
}

// TemplateName 返回站点类型对应的模板名。
func TemplateName(siteType string) string { return siteType + ".conf.tmpl" }

// Render 渲染指定站点类型的配置，返回正文与 SHA-256 指纹（用于幂等下发判定）。
func (r *Renderer) Render(siteType string, ctx *RenderCtx) (string, string, error) {
	name := TemplateName(siteType)
	if r.tpl.Lookup(name) == nil {
		return "", "", errs.Newf(errs.CodeUnsupported, "暂不支持的站点类型：%s", siteType)
	}
	var buf bytes.Buffer
	if err := r.tpl.ExecuteTemplate(&buf, name, ctx); err != nil {
		return "", "", errs.Wrapf(err, errs.CodeInternal, "渲染站点模板 %s 失败", name)
	}
	sum := sha256.Sum256(buf.Bytes())
	return buf.String(), hex.EncodeToString(sum[:]), nil
}

// SupportedTypes 返回当前已有模板的站点类型，供前端只展示真正能创建的类型。
func (r *Renderer) SupportedTypes() []string {
	var out []string
	for _, t := range r.tpl.Templates() {
		n := t.Name()
		if strings.HasSuffix(n, ".conf.tmpl") {
			out = append(out, strings.TrimSuffix(n, ".conf.tmpl"))
		}
	}
	return out
}

// hashOf 计算配置正文的 SHA-256 指纹，与 Render 返回的指纹算法一致，
// 手工编辑与模板渲染两条路径因此可以互相比对是否发生实质变更。
func hashOf(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// String 便于日志与测试断言。
func (c *RenderCtx) String() string {
	return fmt.Sprintf("site=%s type=%s domains=%v ports=%v", c.Name, c.Type, c.ServerNames, c.ListenPorts)
}
