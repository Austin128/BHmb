// Package web 把已构建的前端资源嵌入二进制，实现单文件分发。
// dist 由 `pnpm build`（vite outDir=../internal/web/dist）直接产出；
// 仓库中只保留 .gitkeep 占位，因此未构建前端时后端仍可编译，
// 此时 Available 返回 false，SPA 路由自动跳过注册，面板仅提供 API。
package web

import (
	"embed"
	"io/fs"
)

// all: 前缀保证 .gitkeep 与 _ 开头的产物也被嵌入，避免空目录导致编译失败。
//
//go:embed all:dist
var embedded embed.FS

// FS 返回以 dist 为根的静态资源文件系统。
func FS() (fs.FS, error) {
	return fs.Sub(embedded, "dist")
}

// MustFS 返回静态资源文件系统，出错即 panic（嵌入目录缺失属编译期问题）。
func MustFS() fs.FS {
	sub, err := FS()
	if err != nil {
		panic(err)
	}
	return sub
}

// Available 判断是否嵌入了真实前端产物（存在 index.html）。
func Available() bool {
	sub, err := FS()
	if err != nil {
		return false
	}
	f, err := sub.Open("index.html")
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}
