package sysinfo

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// 采集入口。/proc 只在 Linux 存在，非 Linux 读失败即留零值，
// 由 Snapshot.HostMetricsAvailable 告诉前端「这些指标本平台采不到」，
// 而不是显示成 0% 让人误判。
const (
	procStat     = "/proc/stat"
	procMeminfo  = "/proc/meminfo"
	procLoadavg  = "/proc/loadavg"
	procCPUInfo  = "/proc/cpuinfo"
	procMounts   = "/proc/mounts"
	procNetDev   = "/proc/net/dev"
	procUptime   = "/proc/uptime"
	procOSRelese = "/etc/os-release"
	procKernel   = "/proc/sys/kernel/osrelease"
	procSelfStat = "/proc/self/status"
)

// cpuSampleMinGap 为两次 CPU 采样的最小间隔。间隔太短算出来的使用率抖动很大，
// 距上次采样不足该间隔时就地睡一小会儿再采第二次。
const (
	cpuSampleMinGap  = 900 * time.Millisecond
	cpuSampleFallbck = 120 * time.Millisecond
)

// Build 为编译期版本信息，由上层注入。
type Build struct {
	Version   string
	Commit    string
	BuildTime string
}

// Host 为主机静态信息与开机时长。
type Host struct {
	Hostname string `json:"hostname"`
	OS       string `json:"os"`
	// OSName 为发行版名称，如 Debian GNU/Linux 12 (bookworm)。
	OSName string `json:"osName"`
	Kernel string `json:"kernel"`
	Arch   string `json:"arch"`
	// UptimeSeconds 为主机开机时长；BootTime 为推算出的开机时刻。
	UptimeSeconds int64      `json:"uptimeSeconds"`
	BootTime      *time.Time `json:"bootTime,omitempty"`
	Now           time.Time  `json:"now"`
	Timezone      string     `json:"timezone"`
}

// Panel 为面板进程自身的运行信息。
type Panel struct {
	Version       string    `json:"version"`
	Commit        string    `json:"commit,omitempty"`
	BuildTime     string    `json:"buildTime,omitempty"`
	GoVersion     string    `json:"goVersion"`
	PID           int       `json:"pid"`
	StartedAt     time.Time `json:"startedAt"`
	UptimeSeconds int64     `json:"uptimeSeconds"`
	Goroutines    int       `json:"goroutines"`
	// HeapAllocBytes 为 Go 堆上的存活对象大小；RSSBytes 为进程常驻内存（Linux 才有）。
	HeapAllocBytes uint64 `json:"heapAllocBytes"`
	RSSBytes       uint64 `json:"rssBytes"`
	NumCPU         int    `json:"numCpu"`
	DataDir        string `json:"dataDir,omitempty"`
}

// Snapshot 为一次采集结果。
type Snapshot struct {
	Host    Host           `json:"host"`
	CPU     CPUInfo        `json:"cpu"`
	Memory  MemStat        `json:"memory"`
	Disks   []MountPoint   `json:"disks"`
	Network []NetInterface `json:"network"`
	Panel   Panel          `json:"panel"`
	// HostMetricsAvailable 为 false 时 CPU/内存/磁盘/网络均无意义（非 Linux 平台）。
	HostMetricsAvailable bool `json:"hostMetricsAvailable"`
}

// Collector 采集主机与进程信息。CPU 使用率需要两次采样，因此持有上一次的累计值。
type Collector struct {
	build     Build
	startedAt time.Time
	dataDir   string

	mu       sync.Mutex
	prev     cpuTimes
	prevAt   time.Time
	prevOK   bool
	cpuModel string
	cpuCores int
	cpuOnce  sync.Once
}

// NewCollector 构造采集器。startedAt 为进程启动时刻，dataDir 用于展示数据目录所在磁盘。
func NewCollector(build Build, startedAt time.Time, dataDir string) *Collector {
	if build.Version == "" {
		build.Version = "dev"
	}
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	return &Collector{build: build, startedAt: startedAt, dataDir: dataDir}
}

// Collect 采集一次快照。任何单项采集失败都不影响其余字段，只留零值。
func (c *Collector) Collect() Snapshot {
	now := time.Now()
	mem := parseMeminfo(readFile(procMeminfo))

	snap := Snapshot{
		Host:                 c.host(now),
		CPU:                  c.cpu(),
		Memory:               mem,
		Disks:                c.disks(),
		Network:              parseNetDev(readFile(procNetDev)),
		Panel:                c.panel(now),
		HostMetricsAvailable: runtime.GOOS == "linux" && mem.Total > 0,
	}
	return snap
}

func (c *Collector) host(now time.Time) Host {
	h := Host{
		OS:            runtime.GOOS,
		OSName:        parseOSRelease(readFile(procOSRelese)),
		Kernel:        strings.TrimSpace(readFile(procKernel)),
		Arch:          runtime.GOARCH,
		UptimeSeconds: parseUptime(readFile(procUptime)),
		Now:           now,
		Timezone:      now.Format("-07:00"),
	}
	if name, err := os.Hostname(); err == nil {
		h.Hostname = name
	}
	if h.OSName == "" {
		h.OSName = runtime.GOOS
	}
	if h.UptimeSeconds > 0 {
		boot := now.Add(-time.Duration(h.UptimeSeconds) * time.Second)
		h.BootTime = &boot
	}
	if tz, _ := now.Zone(); tz != "" {
		h.Timezone = tz + " " + h.Timezone
	}
	return h
}

// cpu 取型号、核数与使用率。型号与核数只需读一次。
func (c *Collector) cpu() CPUInfo {
	c.cpuOnce.Do(func() {
		c.cpuModel, c.cpuCores = parseCPUInfo(readFile(procCPUInfo))
	})
	out := CPUInfo{Model: c.cpuModel, Cores: c.cpuCores, LoadStat: parseLoadavg(readFile(procLoadavg))}
	if out.Cores == 0 {
		out.Cores = runtime.NumCPU()
	}
	if out.Model == "" {
		out.Model = runtime.GOARCH
	}
	out.UsagePercent = c.cpuPercent()
	return out
}

// cpuPercent 用上一次采样算增量；间隔过短或首次调用则原地补一次短采样。
func (c *Collector) cpuPercent() float64 {
	cur, ok := parseCPUStat(readFile(procStat))
	if !ok {
		return 0
	}
	now := time.Now()

	c.mu.Lock()
	prev, prevAt, prevOK := c.prev, c.prevAt, c.prevOK
	c.prev, c.prevAt, c.prevOK = cur, now, true
	c.mu.Unlock()

	if prevOK && now.Sub(prevAt) >= cpuSampleMinGap {
		return cpuUsage(prev, cur)
	}

	time.Sleep(cpuSampleFallbck)
	second, ok := parseCPUStat(readFile(procStat))
	if !ok {
		return 0
	}
	c.mu.Lock()
	c.prev, c.prevAt, c.prevOK = second, time.Now(), true
	c.mu.Unlock()
	return cpuUsage(cur, second)
}

// disks 填充各挂载点用量，并保证数据目录所在磁盘一定在列表里。
func (c *Collector) disks() []MountPoint {
	mounts := parseMounts(readFile(procMounts))
	out := make([]MountPoint, 0, len(mounts))
	for _, m := range mounts {
		if fillUsage(&m) && m.Total > 0 {
			out = append(out, m)
		}
	}
	if c.dataDir != "" && !coversPath(out, c.dataDir) {
		dir := MountPoint{Path: filepath.Clean(c.dataDir)}
		if fillUsage(&dir) && dir.Total > 0 {
			out = append(out, dir)
		}
	}
	return out
}

func (c *Collector) panel(now time.Time) Panel {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return Panel{
		Version:        c.build.Version,
		Commit:         c.build.Commit,
		BuildTime:      c.build.BuildTime,
		GoVersion:      runtime.Version(),
		PID:            os.Getpid(),
		StartedAt:      c.startedAt,
		UptimeSeconds:  int64(now.Sub(c.startedAt).Seconds()),
		Goroutines:     runtime.NumGoroutine(),
		HeapAllocBytes: ms.HeapAlloc,
		RSSBytes:       parseSelfRSS(readFile(procSelfStat)),
		NumCPU:         runtime.NumCPU(),
		DataDir:        c.dataDir,
	}
}

// parseSelfRSS 从 /proc/self/status 取 VmRSS，单位字节。
func parseSelfRSS(text string) uint64 {
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(line, "VmRSS:") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 2 {
			return 0
		}
		v, err := strconv.ParseUint(f[1], 10, 64)
		if err != nil {
			return 0
		}
		return v * 1024
	}
	return 0
}

// coversPath 判断已收集的挂载点里是否已经包含该路径所在的挂载点。
func coversPath(mounts []MountPoint, path string) bool {
	path = filepath.Clean(path)
	for _, m := range mounts {
		if m.Path == path || (m.Path != "/" && strings.HasPrefix(path, m.Path+"/")) || m.Path == "/" {
			return true
		}
	}
	return false
}

// readFile 读文本文件，失败返回空串：采集器的所有字段都允许缺失。
func readFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}
