// Package templates — 插件模板的复合加载器（docs/06-plugin-system.md §7）。
//
// 命名空间合并（终态设计，强于 overlay 回退）：内置模板与插件模板路径空间
// 不相交——内置为 "{name}.jet"，插件为 "plugin/{pluginID}/{file}.jet"——
// 插件永远不可能覆盖内置模板（比"先到先得"更强的安全基线）。
// CompositeLoader 持有内置 embed loader + 启用插件的 fs.FS 映射，
// 按路径前缀路由：plugin/{pid}/{rest} → 插件包根下 components/{rest}。
package templates

import (
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"

	"github.com/CloudyKit/jet/v6"
)

// PluginFS 单个启用插件的模板文件系统视图。
type PluginFS struct {
	// ID 插件 ID（manifest.id，路由键）。
	ID string
	// FS 插件包根目录（components/ 位于其下；os.DirFS(解包目录)）。
	FS fs.FS
}

// compositeLoader 命名空间复合加载器：内置 embed + 插件路由。
type compositeLoader struct {
	base    jetLoader
	plugins map[string]fs.FS
}

// jetLoader 最小加载器接口（embedLoader 实现同一形态）。
type jetLoader interface {
	Exists(templatePath string) bool
	Open(templatePath string) (io.ReadCloser, error)
}

// pluginPathPrefix 插件模板命名空间前缀。
const pluginPathPrefix = "plugin/"

// componentsDir 插件包内组件模板目录。
const componentsDir = "components"

// newCompositeLoader 构建复合加载器（base 为内置 embed loader）。
func newCompositeLoader(base jetLoader, plugins []PluginFS) *compositeLoader {
	m := make(map[string]fs.FS, len(plugins))
	for _, p := range plugins {
		m[p.ID] = p.FS
	}
	return &compositeLoader{base: base, plugins: m}
}

// route 插件命名空间路径 → 目标 fs 与包内相对路径。
// "plugin/marketing/card.jet" → (marketing 的 FS, "components/card.jet")。
// jet 传入的模板路径可能带前导斜杠（path.Join 产物），先规范化（与
// embedLoader.normalize 一致：去前导斜杠）。
func (l *compositeLoader) route(templatePath string) (fs.FS, string, bool) {
	clean := path.Clean(path.Join("/", templatePath))[1:]
	rel, ok := strings.CutPrefix(clean, pluginPathPrefix)
	if !ok {
		return nil, "", false
	}
	pid, rest, ok := strings.Cut(rel, "/")
	if !ok || pid == "" || rest == "" {
		return nil, "", false
	}
	f, ok := l.plugins[pid]
	if !ok {
		return nil, "", false
	}
	// rest 已由白名单文件名约束（plugincomp.templateFileRe），
	// 此处再清路径防穿越兜底（Clean 后必须仍在 components 下）。
	inner := path.Join(componentsDir, path.Clean("/" + rest)[1:])
	if inner == componentsDir || strings.HasPrefix(inner, componentsDir+"/") == false {
		return nil, "", false
	}
	return f, inner, true
}

// Exists 见 jet.Loader。
func (l *compositeLoader) Exists(templatePath string) bool {
	if f, inner, ok := l.route(templatePath); ok {
		st, err := fs.Stat(f, inner)
		return err == nil && !st.IsDir()
	}
	return l.base.Exists(templatePath)
}

// Open 见 jet.Loader。
func (l *compositeLoader) Open(templatePath string) (io.ReadCloser, error) {
	if f, inner, ok := l.route(templatePath); ok {
		return f.Open(inner)
	}
	return l.base.Open(templatePath)
}

// NewCompositeSet 构建含插件模板的组件模板 Set（内置 embed + 启用插件）。
//
// plugins 为当前启用的插件集（装配层按 registry 构建）；每次构建任务调用
// 产生独立 Set（插件模板按任务快照，确定性：同一插件版本集 → 同一模板内容）。
func NewCompositeSet(plugins []PluginFS) (*jet.Set, error) {
	base, err := newEmbeddedComponentLoader()
	if err != nil {
		return nil, fmt.Errorf("内置组件模板加载失败: %w", err)
	}
	set := jet.NewSet(
		newCompositeLoader(base, plugins),
		jet.WithTemplateNameExtensions([]string{"", ".jet"}),
	)
	injectGlobals(set)
	return set, nil
}
