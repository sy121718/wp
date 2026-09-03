// Package templates — embed.FS 适配 jet.Loader。
//
// 组件模板用 go:embed 打进二进制，构建期不依赖文件路径。
// 适配 jet.Loader 接口（Exists + Open），复用 io/fs 语义。
package templates

import (
	"io"
	"io/fs"
)

// embedLoader 把 embed.FS 适配为 jet.Loader。
// paths 为 embed 根目录下的文件（含子目录），用于 Exists 判断。
type embedLoader struct {
	fsys  fs.FS
	paths map[string]bool
}

// newEmbedLoader 从 embed.FS 与文件清单构建 loader。
func newEmbedLoader(fsys fs.FS, files []string) *embedLoader {
	paths := make(map[string]bool, len(files))
	for _, f := range files {
		paths[f] = true
	}
	return &embedLoader{fsys: fsys, paths: paths}
}

// Exists 判断模板路径是否存在于 embed 清单。
func (l *embedLoader) Exists(templatePath string) bool {
	return l.paths[templatePath]
}

// Open 返回模板内容；调用方负责关闭。
func (l *embedLoader) Open(templatePath string) (io.ReadCloser, error) {
	return l.fsys.Open(templatePath)
}
