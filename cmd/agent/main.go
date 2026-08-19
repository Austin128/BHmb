// Command agent 是 NovaPanel 的节点代理。
// M0 里程碑只交付控制面单机能力，Agent 的 gRPC 双向流、mTLS 注册与任务执行
// 在「多节点集群与 Agent」里程碑落地（docs/10）。此处保留可编译的入口骨架，
// 明确以非零退出码拒绝运行，避免给出「已支持集群」的错觉。
package main

import (
	"flag"
	"fmt"
	"os"
)

var (
	version   = "dev"
	commit    = "none"
	buildTime = "unknown"
)

func main() {
	showVersion := flag.Bool("version", false, "打印版本信息后退出")
	flag.Parse()

	if *showVersion {
		fmt.Printf("nova-agent %s (commit %s, built %s)\n", version, commit, buildTime)
		return
	}

	fmt.Fprintln(os.Stderr, "nova-agent 尚未实现：当前里程碑仅提供控制面单机能力，请参见 docs/10-多节点集群与Agent.md")
	os.Exit(69) // EX_UNAVAILABLE：服务不可用，便于 systemd 与安装脚本区分「未实现」与「启动失败」
}
