//go:build linux || darwin

package sysinfo

import "syscall"

// fillUsage 用 statfs 填充容量。Total 用 Blocks，Free 用 Bavail（非 root 可用块），
// Used 取 Blocks-Bfree：这样已用量包含 root 预留块，与 df 的口径一致。
func fillUsage(m *MountPoint) bool {
	var st syscall.Statfs_t
	if err := syscall.Statfs(m.Path, &st); err != nil {
		return false
	}
	bsize := uint64(st.Bsize)
	if bsize == 0 {
		return false
	}
	m.Total = uint64(st.Blocks) * bsize
	m.Free = uint64(st.Bavail) * bsize
	if st.Blocks >= st.Bfree {
		m.Used = (uint64(st.Blocks) - uint64(st.Bfree)) * bsize
	}
	m.UsedPercent = percent(m.Used, m.Used+m.Free)
	return true
}
