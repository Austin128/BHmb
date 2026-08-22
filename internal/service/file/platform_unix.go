//go:build unix

package file

import (
	"errors"
	"io/fs"
	"os/user"
	"strconv"
	"syscall"
)

// ownership 从 stat 结果取 uid/gid。非 unix 平台返回 ok=false。
func ownership(info fs.FileInfo) (uid, gid int, ok bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, false
	}
	return int(st.Uid), int(st.Gid), true
}

// writable 以面板运行用户身份判定可写性，用于前端右键菜单可用性。
// 判定失败按不可写处理，宁可少给操作入口也不误导用户。
func writable(path string) bool {
	return syscall.Access(path, writeOK) == nil
}

// writeOK 为 access(2) 的 W_OK。各平台常量值一致（2），但类型不同，
// 这里统一收敛成一个包内常量，避免调用处出现平台差异。
const writeOK = 0x2

// isNoSpace 识别写入时的 ENOSPC，用于返回 400013 而不是笼统的内部错误。
func isNoSpace(err error) bool {
	return errors.Is(err, syscall.ENOSPC)
}

// isCrossDevice 识别跳文件系统的 rename 失败，调用方据此回退到复制 + 删除。
func isCrossDevice(err error) bool {
	return errors.Is(err, syscall.EXDEV)
}

// resolveOwner 把用户名或数字串解析为 uid，空串返回 -1 表示不修改。
func resolveOwner(name string) (int, error) {
	if name == "" {
		return -1, nil
	}
	if id, err := strconv.Atoi(name); err == nil {
		return id, nil
	}
	u, err := user.Lookup(name)
	if err != nil {
		return -1, err
	}
	return strconv.Atoi(u.Uid)
}

// resolveGroup 把组名或数字串解析为 gid，空串返回 -1 表示不修改。
func resolveGroup(name string) (int, error) {
	if name == "" {
		return -1, nil
	}
	if id, err := strconv.Atoi(name); err == nil {
		return id, nil
	}
	g, err := user.LookupGroup(name)
	if err != nil {
		return -1, err
	}
	return strconv.Atoi(g.Gid)
}
