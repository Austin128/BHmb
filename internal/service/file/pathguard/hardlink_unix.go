//go:build unix

package pathguard

import (
	"io/fs"
	"syscall"
)

// hardLinkCount 读取 inode 的硬链接数。Nlink 在各平台宽度不同，统一转 uint64。
func hardLinkCount(info fs.FileInfo) (uint64, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return uint64(st.Nlink), true
}
