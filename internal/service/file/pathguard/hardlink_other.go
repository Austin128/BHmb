//go:build !unix

package pathguard

import "io/fs"

// hardLinkCount 在非 unix 平台无法获取链接数，返回不可用让调用方跳过该检查。
func hardLinkCount(fs.FileInfo) (uint64, bool) {
	return 0, false
}
