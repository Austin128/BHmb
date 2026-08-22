package sysinfo

import (
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 解析函数用固定样本测，不依赖运行环境是否有 /proc，
// 这样 macOS 开发机与 Linux CI 得到的结论一致。

const meminfoSample = `MemTotal:        4025492 kB
MemFree:          213456 kB
MemAvailable:    2903216 kB
Buffers:           88128 kB
Cached:          1930112 kB
SwapCached:            0 kB
SwapTotal:       1048572 kB
SwapFree:         999996 kB
`

func TestParseMeminfo(t *testing.T) {
	m := parseMeminfo(meminfoSample)
	assert.Equal(t, uint64(4025492)*1024, m.Total)
	assert.Equal(t, uint64(2903216)*1024, m.Available)
	// used 取 total-available，页缓存不算已用
	assert.Equal(t, (uint64(4025492)-uint64(2903216))*1024, m.Used)
	assert.Equal(t, uint64(1048572-999996)*1024, m.SwapUsed)
	assert.InDelta(t, 27.9, m.UsedPercent, 0.15)
}

func TestParseMeminfoOldKernelWithoutMemAvailable(t *testing.T) {
	m := parseMeminfo("MemTotal: 1000 kB\nMemFree: 200 kB\nBuffers: 100 kB\nCached: 300 kB\n")
	// 没有 MemAvailable 时用 free+buffers+cached 估算
	assert.Equal(t, uint64(600)*1024, m.Available)
	assert.Equal(t, uint64(400)*1024, m.Used)
}

func TestParseMeminfoEmpty(t *testing.T) {
	m := parseMeminfo("")
	assert.Zero(t, m.Total)
	assert.Zero(t, m.UsedPercent)
}

func TestParseLoadavg(t *testing.T) {
	l := parseLoadavg("0.52 0.31 0.19 1/321 4242\n")
	assert.InDelta(t, 0.52, l.Load1, 0.001)
	assert.InDelta(t, 0.31, l.Load5, 0.001)
	assert.InDelta(t, 0.19, l.Load15, 0.001)

	assert.Equal(t, LoadStat{}, parseLoadavg(""))
}

func TestParseCPUStatAndUsage(t *testing.T) {
	prev, ok := parseCPUStat("cpu  100 0 100 800 0 0 0 0 0 0\ncpu0 1 2 3 4\n")
	require.True(t, ok)
	assert.Equal(t, uint64(1000), prev.total)
	assert.Equal(t, uint64(800), prev.idle)

	cur, ok := parseCPUStat("cpu  150 0 150 900 0 0 0 0 0 0\n")
	require.True(t, ok)
	assert.Equal(t, uint64(1200), cur.total)
	// 增量 200，其中 idle 100 → 忙 100/200
	assert.InDelta(t, 50.0, cpuUsage(prev, cur), 0.1)

	_, ok = parseCPUStat("intr 12345\n")
	assert.False(t, ok)
}

func TestCPUUsageEdgeCases(t *testing.T) {
	same := cpuTimes{total: 1000, idle: 500}
	assert.Zero(t, cpuUsage(same, same), "累计值没推进时不应算出使用率")
	assert.Zero(t, cpuUsage(cpuTimes{total: 2000, idle: 1000}, same), "计数器回绕时返回 0 而不是负数")
	// 全程空闲
	assert.Zero(t, cpuUsage(cpuTimes{total: 100, idle: 100}, cpuTimes{total: 200, idle: 200}))
}

func TestParseCPUInfo(t *testing.T) {
	model, cores := parseCPUInfo(`processor	: 0
model name	: Intel(R) Xeon(R) CPU E5-2680 v4 @ 2.40GHz
processor	: 1
model name	: Intel(R) Xeon(R) CPU E5-2680 v4 @ 2.40GHz
`)
	assert.Equal(t, "Intel(R) Xeon(R) CPU E5-2680 v4 @ 2.40GHz", model)
	assert.Equal(t, 2, cores)

	// ARM 板子没有 model name，回落到 Hardware
	model, cores = parseCPUInfo("processor\t: 0\nHardware\t: BCM2835\n")
	assert.Equal(t, "BCM2835", model)
	assert.Equal(t, 1, cores)
}

func TestParseMountsFiltersPseudoAndDuplicates(t *testing.T) {
	mounts := parseMounts(`sysfs /sys sysfs rw 0 0
proc /proc proc rw 0 0
/dev/vda1 / ext4 rw,relatime 0 0
tmpfs /dev/shm tmpfs rw 0 0
/dev/vdb1 /data\040disk xfs rw 0 0
/dev/vda1 /var/lib/docker ext4 rw 0 0
`)
	require.Len(t, mounts, 2)
	assert.Equal(t, "/", mounts[0].Path)
	assert.Equal(t, "ext4", mounts[0].FSType)
	// 八进制转义要还原成空格
	assert.Equal(t, "/data disk", mounts[1].Path)
	assert.Equal(t, "xfs", mounts[1].FSType)
}

func TestParseNetDev(t *testing.T) {
	nics := parseNetDev(`Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo: 1234 12 0 0 0 0 0 0 1234 12 0 0 0 0 0 0
  eth0: 5000 50 0 0 0 0 0 0 6000 60 0 0 0 0 0 0
  eth1: 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0
`)
	require.Len(t, nics, 1)
	assert.Equal(t, "eth0", nics[0].Name)
	assert.Equal(t, uint64(5000), nics[0].BytesRecv)
	assert.Equal(t, uint64(6000), nics[0].BytesSent)
	assert.Equal(t, uint64(60), nics[0].PacketsSent)
}

func TestParseUptimeAndOSRelease(t *testing.T) {
	assert.Equal(t, int64(3600), parseUptime("3600.42 7200.11\n"))
	assert.Zero(t, parseUptime("bad"))

	assert.Equal(t, "Debian GNU/Linux 12 (bookworm)",
		parseOSRelease("NAME=\"Debian GNU/Linux\"\nPRETTY_NAME=\"Debian GNU/Linux 12 (bookworm)\"\n"))
	// 没有 PRETTY_NAME 时退回 NAME
	assert.Equal(t, "Alpine Linux", parseOSRelease("NAME=\"Alpine Linux\"\nID=alpine\n"))
	assert.Empty(t, parseOSRelease(""))
}

func TestParseSelfRSS(t *testing.T) {
	assert.Equal(t, uint64(20480)*1024, parseSelfRSS("Name:\tnovapanel\nVmRSS:\t   20480 kB\n"))
	assert.Zero(t, parseSelfRSS("Name:\tnovapanel\n"))
}

func TestPercentAndRound(t *testing.T) {
	assert.Zero(t, percent(10, 0), "总量为 0 时不能除零")
	assert.InDelta(t, 33.3, percent(1, 3), 0.001)
	assert.InDelta(t, 66.7, percent(2, 3), 0.001)
}

func TestCoversPath(t *testing.T) {
	mounts := []MountPoint{{Path: "/data"}}
	assert.True(t, coversPath(mounts, "/data/novapanel"))
	assert.False(t, coversPath(mounts, "/opt/novapanel"))
	// 有根挂载点时任何路径都已被覆盖
	assert.True(t, coversPath([]MountPoint{{Path: "/"}}, "/opt/novapanel"))
}

func TestCollectAlwaysReturnsUsableSnapshot(t *testing.T) {
	c := NewCollector(Build{Version: "v1.2.3", Commit: "abc123"}, time.Now().Add(-time.Minute), t.TempDir())
	snap := c.Collect()

	assert.Equal(t, "v1.2.3", snap.Panel.Version)
	assert.Equal(t, "abc123", snap.Panel.Commit)
	assert.Equal(t, runtime.Version(), snap.Panel.GoVersion)
	assert.Positive(t, snap.Panel.PID)
	assert.GreaterOrEqual(t, snap.Panel.UptimeSeconds, int64(59))
	assert.Positive(t, snap.Panel.Goroutines)
	assert.NotEmpty(t, snap.Host.OS)
	assert.NotEmpty(t, snap.Host.Arch)
	assert.Positive(t, snap.CPU.Cores, "取不到 /proc/cpuinfo 时也要回落到 runtime.NumCPU")
	assert.NotEmpty(t, snap.CPU.Model)
	assert.NotNil(t, snap.Disks)
	assert.NotNil(t, snap.Network)
	// 使用率永远落在合法区间，避免前端进度条越界
	assert.GreaterOrEqual(t, snap.CPU.UsagePercent, 0.0)
	assert.LessOrEqual(t, snap.CPU.UsagePercent, 100.0)
	assert.GreaterOrEqual(t, snap.Memory.UsedPercent, 0.0)
	assert.LessOrEqual(t, snap.Memory.UsedPercent, 100.0)

	if runtime.GOOS == "linux" {
		assert.True(t, snap.HostMetricsAvailable)
		assert.Positive(t, snap.Memory.Total)
		assert.Positive(t, snap.Host.UptimeSeconds)
	} else {
		assert.False(t, snap.HostMetricsAvailable, "非 Linux 必须明确标记主机指标不可用")
	}
}

func TestCollectDataDirDiskIncludedOnUnix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows 不采集容量")
	}
	c := NewCollector(Build{}, time.Now(), t.TempDir())
	snap := c.Collect()
	require.NotEmpty(t, snap.Disks, "临时目录所在磁盘至少应被采集到一条")
	assert.Positive(t, snap.Disks[0].Total)
	assert.LessOrEqual(t, snap.Disks[0].UsedPercent, 100.0)
}

func TestCollectTwiceUsesPreviousSample(t *testing.T) {
	c := NewCollector(Build{}, time.Now(), "")
	first := c.Collect()
	second := c.Collect()
	// 两次都必须给出合法值；第二次会走「上次采样间隔不足 → 就地补采」分支
	assert.GreaterOrEqual(t, first.CPU.UsagePercent, 0.0)
	assert.GreaterOrEqual(t, second.CPU.UsagePercent, 0.0)
	assert.LessOrEqual(t, second.CPU.UsagePercent, 100.0)
}
