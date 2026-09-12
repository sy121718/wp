// Package templates — embed.FS 适配 jet.Loader。
//
// 组件模板用 go:embed 打进二进制，构建期不依赖文件路径。
// 适配 jet.Loader 接口（Exists + Open），复用 io/fs 语义。
//
// 模板有两个来源：
//  1. 集中目录 internal/templates/components/*.jet（存量，仍在）；
//  2. **组件自带** —— 组件目录里 //go:embed xxx.jet 后经 core.RegisterTemplate 注册（就近放置）。
//
// 组件自带的优先：它就在组件旁边，最知道自己在渲染什么。迁移期两者同名时给一条警告，
// 提示该把集中目录里那份删掉（否则读者会以为改那份有用）。
package templates

import (
	"bytes"
	"io"
	"io/fs"
	"path"

	"go_wp/internal/builder/core"
	"go_wp/pkg/logger"
)

// embedLoader 把 embed.FS 适配为 jet.Loader。
// paths 为 embed 根目录下的文件（含子目录），用于 Exists 判断。
type embedLoader struct {
	fsys  fs.FS
	paths map[string]bool
	// inline 组件自带模板（文件名 → 源码），优先于 fsys。
	inline map[string][]byte
}

// newEmbedLoader 从 fs.FS 与文件清单构建 loader。
//
// **组件自带模板在这里统一合并**：所有构造点（embed 单例 / 目录 / composite）都经过本函数，
// 放在这里才不会漏。此前放在上层的 newEmbeddedComponentLoader，结果走「从目录加载」的路径
// （测试与部分装配）拿不到组件自带模板 —— 表现为构建期 template not found。
func newEmbedLoader(fsys fs.FS, files []string) *embedLoader {
	paths := make(map[string]bool, len(files))
	for _, f := range files {
		paths[f] = true
	}
	return (&embedLoader{fsys: fsys, paths: paths, inline: map[string][]byte{}}).addOwnedTemplates()
}

// addOwnedTemplates 合并组件自带的模板（core 注册表）。
//
// 模板名与文件名对齐：注册名 form 对应 form.jet（jet 的模板名扩展名已配 .jet）。
func (l *embedLoader) addOwnedTemplates() *embedLoader {
	for _, name := range core.OwnedTemplateNames() {
		src, _ := core.OwnedTemplate(name)
		file := name + ".jet"
		if l.paths[file] {
			// 迁移期正常现象：模板刚从集中目录搬到组件目录。
			// 留一条痕，避免后来者「改了集中目录那份却看不出为什么没生效」。
			logger.Scene("build").With("template", file).
				Warn("模板同时存在于集中目录与组件目录，以组件目录为准（建议删除集中目录那份）")
		}
		l.inline[file] = []byte(src)
	}
	return l
}

// normalize 把 jet 传入的模板路径规范化：去前导斜杠（jet 经 path.Join 后形如 /text.jet），
// 与 embed 清单中的相对键对齐。
func (l *embedLoader) normalize(templatePath string) string {
	return path.Clean(path.Join("/", templatePath))[1:]
}

// Exists 判断模板路径是否存在于清单（组件自带优先）。
func (l *embedLoader) Exists(templatePath string) bool {
	name := l.normalize(templatePath)
	if _, ok := l.inline[name]; ok {
		return true
	}
	return l.paths[name]
}

// Open 返回模板内容；调用方负责关闭。
func (l *embedLoader) Open(templatePath string) (io.ReadCloser, error) {
	name := l.normalize(templatePath)
	if src, ok := l.inline[name]; ok {
		return io.NopCloser(bytes.NewReader(src)), nil
	}
	return l.fsys.Open(name)
}
