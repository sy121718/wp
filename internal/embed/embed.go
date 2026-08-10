// Package frontend 提供编译进二进制的前端静态资源。
package frontend

import (
	"embed"
	"io/fs"
)

// files 包含前端构建产物；all: 前缀确保占位文件也可用于本地编译。
//
//go:embed all:dist
var files embed.FS

// DistFS 返回以前端构建目录为根的只读文件系统。
func DistFS() (fs.FS, error) {
	return fs.Sub(files, "dist")
}
