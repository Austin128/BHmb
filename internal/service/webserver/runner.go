// Package webserver 封装宿主机 Web 服务（Nginx / OpenResty）的探测、配置校验与热加载。
// 面板只写 vhost 目录下的站点配置，主配置由安装脚本 include，因此本包不修改系统主配置。
package webserver

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"time"
)

// Result 为一次命令执行的输出。stderr 单独保留，因为 nginx -t 的诊断信息走 stderr。
type Result struct {
	Stdout string
	Stderr string
}

// Combined 返回优先展示 stderr 的合并输出，便于把诊断信息回传前端。
func (r Result) Combined() string {
	out := strings.TrimSpace(r.Stderr)
	if out == "" {
		out = strings.TrimSpace(r.Stdout)
	}
	return out
}

// Runner 执行外部命令。抽成接口是为了让站点服务在没有 Nginx 的开发机与 CI 上也能被完整测试。
type Runner interface {
	Run(ctx context.Context, name string, args ...string) (Result, error)
}

// ExecRunner 是基于 os/exec 的默认实现，单条命令超时上限为 Timeout。
type ExecRunner struct {
	Timeout time.Duration
}

// NewExecRunner 构造默认执行器，未指定超时时取 20 秒：
// nginx -t 在大量站点时可能耗时，但不应无限期挂住请求。
func NewExecRunner(timeout time.Duration) *ExecRunner {
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	return &ExecRunner{Timeout: timeout}
}

// Run 执行命令并捕获输出。参数以 argv 数组传递，不经过 shell，避免命令注入。
func (e *ExecRunner) Run(ctx context.Context, name string, args ...string) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, e.Timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // name 来自配置探测结果，args 由本包构造
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return Result{Stdout: stdout.String(), Stderr: stderr.String()}, err
}
