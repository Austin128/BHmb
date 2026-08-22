package file

import (
	"bytes"
	"net/http"
	"os"
	"strings"
)

// 扩展名 → MIME 映射（docs/08 8.2.3 第 1 级）。
// 命中即返回，避免列目录时对每个条目做 IO 探测。
var mimeByExtension = map[string]string{
	// 文本与代码
	"txt": "text/plain", "log": "text/plain", "out": "text/plain", "err": "text/plain",
	"md": "text/markdown", "markdown": "text/markdown",
	"go": "text/x-go", "js": "text/javascript", "mjs": "text/javascript", "cjs": "text/javascript",
	"ts": "text/typescript", "tsx": "text/typescript", "jsx": "text/javascript",
	"vue": "text/x-vue", "py": "text/x-python", "rb": "text/x-ruby", "php": "text/x-php",
	"java": "text/x-java", "rs": "text/x-rust", "c": "text/x-c", "h": "text/x-c",
	"cpp": "text/x-c++", "cc": "text/x-c++", "hpp": "text/x-c++",
	"sh": "text/x-shellscript", "bash": "text/x-shellscript", "zsh": "text/x-shellscript",
	"sql": "application/sql", "lua": "text/x-lua", "pl": "text/x-perl",
	// 配置
	"conf": "text/plain", "cnf": "text/plain", "ini": "text/plain", "env": "text/plain",
	"properties": "text/plain", "yaml": "application/yaml", "yml": "application/yaml",
	"toml": "application/toml", "json": "application/json", "json5": "application/json",
	// 标记与样式
	"html": "text/html", "htm": "text/html", "xml": "application/xml",
	"css": "text/css", "scss": "text/x-scss", "less": "text/x-less",
	// 图片
	"jpg": "image/jpeg", "jpeg": "image/jpeg", "png": "image/png", "gif": "image/gif",
	"webp": "image/webp", "bmp": "image/bmp", "ico": "image/x-icon", "svg": "image/svg+xml",
	// 音视频
	"mp4": "video/mp4", "mkv": "video/x-matroska", "avi": "video/x-msvideo",
	"mov": "video/quicktime", "flv": "video/x-flv", "webm": "video/webm",
	"mp3": "audio/mpeg", "wav": "audio/wav", "flac": "audio/flac", "aac": "audio/aac", "ogg": "audio/ogg",
	// 压缩
	"zip": "application/zip", "tar": "application/x-tar", "gz": "application/gzip",
	"tgz": "application/gzip", "bz2": "application/x-bzip2", "xz": "application/x-xz",
	"7z": "application/x-7z-compressed", "rar": "application/vnd.rar", "zst": "application/zstd",
	// 文档
	"pdf": "application/pdf", "csv": "text/csv",
	"xls": "application/vnd.ms-excel", "xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	"doc": "application/msword", "docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	// 数据库与证书
	"db": "application/vnd.sqlite3", "sqlite": "application/vnd.sqlite3", "mdb": "application/x-msaccess",
	"pem": "application/x-pem-file", "crt": "application/x-x509-ca-cert", "cer": "application/x-x509-ca-cert",
	"key": "application/x-pem-file", "pfx": "application/x-pkcs12",
	// 二进制
	"so": "application/x-sharedlib", "a": "application/x-archive", "o": "application/x-object",
	"exe": "application/vnd.microsoft.portable-executable", "class": "application/java-vm",
	"jar": "application/java-archive", "elf": "application/x-executable",
}

// mimeTextPrefixes 判定「可在线编辑」的 MIME 前缀集合。
var mimeTextPrefixes = []string{"text/", "application/json", "application/yaml", "application/toml", "application/xml", "application/sql"}

// 扩展名 → 图标键映射（docs/08 8.2.4）。
var iconByExtension = map[string]string{}

func init() {
	groups := map[string][]string{
		"file-code":    {"go", "js", "mjs", "cjs", "ts", "tsx", "jsx", "vue", "py", "java", "php", "rs", "c", "h", "cpp", "cc", "hpp", "sh", "bash", "zsh", "rb", "lua", "pl"},
		"file-config":  {"conf", "cnf", "ini", "yaml", "yml", "toml", "env", "properties"},
		"file-json":    {"json", "json5"},
		"file-markup":  {"html", "htm", "xml", "md", "markdown", "css", "scss", "less"},
		"file-image":   {"jpg", "jpeg", "png", "gif", "webp", "bmp", "ico", "svg"},
		"file-video":   {"mp4", "mkv", "avi", "mov", "flv", "webm"},
		"file-audio":   {"mp3", "wav", "flac", "aac", "ogg"},
		"file-archive": {"zip", "tar", "gz", "tgz", "bz2", "xz", "7z", "rar", "zst"},
		"file-pdf":     {"pdf"},
		"file-excel":   {"xls", "xlsx", "csv"},
		"file-word":    {"doc", "docx"},
		"file-db":      {"db", "sqlite", "sql", "mdb"},
		"file-cert":    {"pem", "crt", "cer", "key", "pfx"},
		"file-log":     {"log", "out", "err"},
		"file-binary":  {"elf", "so", "a", "o", "exe", "class", "jar"},
	}
	for icon, exts := range groups {
		for _, ext := range exts {
			iconByExtension[ext] = icon
		}
	}
}

// mimeByExt 只做扩展名查表，未命中返回空串（列目录场景不做磁盘探测）。
func mimeByExt(ext string, isDir bool) string {
	if isDir {
		return "inode/directory"
	}
	return mimeByExtension[ext]
}

// iconOf 返回前端图标键。
func iconOf(ext string, isDir, isSymlink bool) string {
	if isDir {
		if isSymlink {
			return "folder-link"
		}
		return "folder"
	}
	if icon, ok := iconByExtension[ext]; ok {
		return icon
	}
	return "file-unknown"
}

// detectMime 在扩展名未命中时读取文件头做 magic number 探测（docs/08 8.2.3 第 2、3 级）。
// 只在 stat 与在线编辑等单文件场景调用，列目录不触发。
func detectMime(path, ext string) string {
	if m := mimeByExtension[ext]; m != "" {
		return m
	}

	f, err := os.Open(path)
	if err != nil {
		return "application/octet-stream"
	}
	defer func() { _ = f.Close() }()

	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	if n == 0 {
		return "text/plain"
	}
	head := buf[:n]

	// 标准库未覆盖的常见格式补充表
	for _, sig := range []struct {
		magic []byte
		mime  string
	}{
		{[]byte{0x37, 0x7A, 0xBC, 0xAF, 0x27, 0x1C}, "application/x-7z-compressed"},
		{[]byte("Rar!"), "application/vnd.rar"},
		{[]byte{0x28, 0xB5, 0x2F, 0xFD}, "application/zstd"},
		{[]byte{0xCA, 0xFE, 0xBA, 0xBE}, "application/java-vm"},
		{[]byte{0x7F, 'E', 'L', 'F'}, "application/x-executable"},
	} {
		if bytes.HasPrefix(head, sig.magic) {
			return sig.mime
		}
	}

	if m := http.DetectContentType(head); m != "application/octet-stream" {
		return strings.SplitN(m, ";", 2)[0]
	}
	// 无 NUL 字节按文本处理，便于在线编辑无扩展名的配置文件
	if bytes.IndexByte(head, 0x00) < 0 {
		return "text/plain"
	}
	return "application/octet-stream"
}

// isTextMime 判定是否允许在线编辑。
func isTextMime(mime string) bool {
	for _, p := range mimeTextPrefixes {
		if strings.HasPrefix(mime, p) {
			return true
		}
	}
	return false
}
