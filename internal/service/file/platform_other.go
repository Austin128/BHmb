//go:build !unix

package file

import (
	"io/fs"

	"github.com/novapanel/novapanel/internal/pkg/errs"
)

// 非 unix 平台不提供属主与权限位语义，文件模块在这些平台上只支持基础读写。
// 面板本身只在 Linux 部署，这里的实现仅为保证跨平台可编译与本地开发可运行。

func ownership(fs.FileInfo) (uid, gid int, ok bool) {
	return 0, 0, false
}

func writable(string) bool {
	return true
}

// isNoSpace 在非 unix 平台无法可靠区分 ENOSPC，统一按普通失败处理。
func isNoSpace(error) bool {
	return false
}

// isCrossDevice 在非 unix 平台不做跨设备回退，直接把 rename 失败上报。
func isCrossDevice(error) bool {
	return false
}

func resolveOwner(name string) (int, error) {
	if name == "" {
		return -1, nil
	}
	return -1, errs.Newf(errs.CodeUnsupported, "当前平台不支持修改属主")
}

func resolveGroup(name string) (int, error) {
	if name == "" {
		return -1, nil
	}
	return -1, errs.Newf(errs.CodeUnsupported, "当前平台不支持修改属组")
}
