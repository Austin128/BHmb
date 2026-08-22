// Package sysinfo 采集主机与进程运行信息，供总览页与运维页展示。
// 采集全部走 Linux 的 /proc 与 statfs，不引入第三方依赖；
// 非 Linux（开发机）只能拿到进程侧信息，主机字段留零值由前端标注为不可用。
package sysinfo

import (
	"strconv"
	"strings"
)

// 解析函数一律只吃字符串、只吐结构体，方便用固定样本做单测，
// 不必依赖运行环境真的有 /proc。

// MemStat 为内存与交换区用量，单位字节。
type MemStat struct {
	Total     uint64 `json:"total"`
	Available uint64 `json:"available"`
	Used      uint64 `json:"used"`
	Buffers   uint64 `json:"buffers"`
	Cached    uint64 `json:"cached"`
	SwapTotal uint64 `json:"swapTotal"`
	SwapUsed  uint64 `json:"swapUsed"`
	// UsedPercent 为 Used/Total 的百分比，保留一位小数。
	UsedPercent float64 `json:"usedPercent"`
}

// parseMeminfo 解析 /proc/meminfo。字段单位是 kB，缺字段按 0 处理。
// Used 采用 Total-Available（与 free -m 的 used 语义一致），
// 不用 Total-Free，否则页缓存会被算成已用，看起来像内存快满了。
func parseMeminfo(text string) MemStat {
	kv := map[string]uint64{}
	for _, line := range strings.Split(text, "\n") {
		name, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		v, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue
		}
		if len(fields) > 1 && strings.EqualFold(fields[1], "kB") {
			v *= 1024
		}
		kv[name] = v
	}

	m := MemStat{
		Total:     kv["MemTotal"],
		Available: kv["MemAvailable"],
		Buffers:   kv["Buffers"],
		Cached:    kv["Cached"],
		SwapTotal: kv["SwapTotal"],
	}
	// 老内核没有 MemAvailable，退回 Free+Buffers+Cached 估算
	if m.Available == 0 && m.Total > 0 {
		m.Available = kv["MemFree"] + m.Buffers + m.Cached
	}
	if m.Total > m.Available {
		m.Used = m.Total - m.Available
	}
	if m.SwapTotal > kv["SwapFree"] {
		m.SwapUsed = m.SwapTotal - kv["SwapFree"]
	}
	m.UsedPercent = percent(m.Used, m.Total)
	return m
}

// LoadStat 为系统平均负载。
type LoadStat struct {
	Load1  float64 `json:"load1"`
	Load5  float64 `json:"load5"`
	Load15 float64 `json:"load15"`
}

// parseLoadavg 解析 /proc/loadavg 的前三个字段。
func parseLoadavg(text string) LoadStat {
	f := strings.Fields(text)
	var out LoadStat
	if len(f) > 0 {
		out.Load1, _ = strconv.ParseFloat(f[0], 64)
	}
	if len(f) > 1 {
		out.Load5, _ = strconv.ParseFloat(f[1], 64)
	}
	if len(f) > 2 {
		out.Load15, _ = strconv.ParseFloat(f[2], 64)
	}
	return out
}

// cpuTimes 为 /proc/stat 首行的累计 jiffies。
type cpuTimes struct {
	total uint64
	idle  uint64
}

// parseCPUStat 解析 /proc/stat 的 "cpu" 汇总行。
// idle 包含 iowait：等 IO 时 CPU 同样是空闲的，算进使用率会虚高。
func parseCPUStat(text string) (cpuTimes, bool) {
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		var out cpuTimes
		for i, f := range strings.Fields(line)[1:] {
			v, err := strconv.ParseUint(f, 10, 64)
			if err != nil {
				continue
			}
			out.total += v
			if i == 3 || i == 4 { // idle, iowait
				out.idle += v
			}
		}
		return out, out.total > 0
	}
	return cpuTimes{}, false
}

// cpuUsage 由两次采样算使用率；采样间隔内没有推进则返回 0。
func cpuUsage(prev, cur cpuTimes) float64 {
	if cur.total <= prev.total {
		return 0
	}
	deltaTotal := cur.total - prev.total
	var deltaIdle uint64
	if cur.idle > prev.idle {
		deltaIdle = cur.idle - prev.idle
	}
	if deltaIdle > deltaTotal {
		return 0
	}
	return round1(float64(deltaTotal-deltaIdle) / float64(deltaTotal) * 100)
}

// CPUInfo 为 CPU 静态信息与使用率。
type CPUInfo struct {
	Model string `json:"model"`
	// Cores 为逻辑核数。
	Cores        int     `json:"cores"`
	UsagePercent float64 `json:"usagePercent"`
	LoadStat
}

// parseCPUInfo 从 /proc/cpuinfo 取型号与逻辑核数。
// ARM 平台没有 model name，回落到 Hardware/Processor 字段。
func parseCPUInfo(text string) (model string, cores int) {
	var fallback string
	for _, line := range strings.Split(text, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch key {
		case "processor":
			cores++
		case "model name":
			if model == "" {
				model = value
			}
		case "Hardware", "Model", "cpu model":
			if fallback == "" {
				fallback = value
			}
		}
	}
	if model == "" {
		model = fallback
	}
	return model, cores
}

// MountPoint 为一个挂载点的用量。
type MountPoint struct {
	Device      string  `json:"device"`
	Path        string  `json:"path"`
	FSType      string  `json:"fsType"`
	Total       uint64  `json:"total"`
	Used        uint64  `json:"used"`
	Free        uint64  `json:"free"`
	UsedPercent float64 `json:"usedPercent"`
}

// 伪文件系统不占物理容量，列出来只会淹没真正的磁盘。
var pseudoFS = map[string]struct{}{
	"proc": {}, "sysfs": {}, "devtmpfs": {}, "devpts": {}, "tmpfs": {}, "securityfs": {},
	"cgroup": {}, "cgroup2": {}, "pstore": {}, "efivarfs": {}, "bpf": {}, "debugfs": {},
	"tracefs": {}, "hugetlbfs": {}, "mqueue": {}, "fusectl": {}, "configfs": {}, "ramfs": {},
	"binfmt_misc": {}, "autofs": {}, "squashfs": {}, "overlay": {}, "nsfs": {}, "rpc_pipefs": {},
	"selinuxfs": {}, "fuse.gvfsd-fuse": {}, "fuse.portal": {},
}

// parseMounts 解析 /proc/mounts，过滤伪文件系统与重复挂载的同一设备。
func parseMounts(text string) []MountPoint {
	out := make([]MountPoint, 0, 8)
	seen := map[string]struct{}{}
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		device, path, fsType := f[0], unescapeMount(f[1]), f[2]
		if _, bad := pseudoFS[fsType]; bad {
			continue
		}
		// 同一设备被多处挂载（bind mount、容器内叠加）只保留第一个
		if _, dup := seen[device]; dup {
			continue
		}
		seen[device] = struct{}{}
		out = append(out, MountPoint{Device: device, Path: path, FSType: fsType})
	}
	return out
}

// unescapeMount 还原 /proc/mounts 里的八进制转义（空格 \040 等）。
func unescapeMount(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '\\' && i+3 < len(s) {
			if v, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(v))
				i += 4
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// NetInterface 为单块网卡的累计收发字节数。
type NetInterface struct {
	Name        string `json:"name"`
	BytesRecv   uint64 `json:"bytesRecv"`
	BytesSent   uint64 `json:"bytesSent"`
	PacketsRecv uint64 `json:"packetsRecv"`
	PacketsSent uint64 `json:"packetsSent"`
}

// parseNetDev 解析 /proc/net/dev，跳过 lo 与全零网卡。
func parseNetDev(text string) []NetInterface {
	out := make([]NetInterface, 0, 4)
	for _, line := range strings.Split(text, "\n") {
		name, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		if name == "" || name == "lo" || strings.HasPrefix(name, "veth") {
			continue
		}
		f := strings.Fields(rest)
		if len(f) < 10 {
			continue
		}
		nic := NetInterface{Name: name}
		nic.BytesRecv, _ = strconv.ParseUint(f[0], 10, 64)
		nic.PacketsRecv, _ = strconv.ParseUint(f[1], 10, 64)
		nic.BytesSent, _ = strconv.ParseUint(f[8], 10, 64)
		nic.PacketsSent, _ = strconv.ParseUint(f[9], 10, 64)
		if nic.BytesRecv == 0 && nic.BytesSent == 0 {
			continue
		}
		out = append(out, nic)
	}
	return out
}

// parseUptime 解析 /proc/uptime 的第一个字段，单位秒。
func parseUptime(text string) int64 {
	f := strings.Fields(text)
	if len(f) == 0 {
		return 0
	}
	v, err := strconv.ParseFloat(f[0], 64)
	if err != nil || v < 0 {
		return 0
	}
	return int64(v)
}

// parseOSRelease 从 /etc/os-release 取 PRETTY_NAME，取不到时退回 NAME。
func parseOSRelease(text string) string {
	var name string
	for _, line := range strings.Split(text, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		switch key {
		case "PRETTY_NAME":
			if value != "" {
				return value
			}
		case "NAME":
			if name == "" {
				name = value
			}
		}
	}
	return name
}

func percent(used, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return round1(float64(used) / float64(total) * 100)
}

func round1(v float64) float64 {
	return float64(int64(v*10+0.5)) / 10
}
