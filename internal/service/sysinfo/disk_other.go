//go:build !linux && !darwin

package sysinfo

// fillUsage 在没有 statfs 的平台上直接放弃采集容量，页面会显示为不可用。
func fillUsage(*MountPoint) bool { return false }
