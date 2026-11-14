package templates

import (
	"fmt"

	"github.com/CloudyKit/jet/v6"

	"go_wp/pkg/builder/source"
	jetpkg "go_wp/pkg/view/jet"
)

// PluginFS 是插件的模板源（应用侧沿用 builder 的中立形状）。
type PluginFS = source.PluginFS

// 插件模板查找与有界缓存的机制已收进 pkg/view/jet；
// 这里只保留「应用的内置组件模板源」这一应用事实。

// NewCompositeSet 用应用的内置组件模板源构造组件模板 Set。
func NewCompositeSet(plugins []PluginFS) (*jet.Set, error) {
	base, err := embeddedComponentLoader()
	if err != nil {
		return nil, err
	}
	return jetpkg.NewJetSetFromLoader(jetpkg.NewCompositeLoader(base, toJetPluginFS(plugins)), false, []string{"", ".jet"}), nil
}

// NewCompositeSetCached 按 fingerprint 复用 Set；fingerprint 必须覆盖模板源、
// 插件集合与版本，只在最近两代内复用。
func NewCompositeSetCached(fingerprint string, plugins []PluginFS) (*jet.Set, error) {
	base, err := embeddedComponentLoader()
	if err != nil {
		return nil, err
	}
	return jetpkg.NewCompositeSetCached(fingerprint, base, toJetPluginFS(plugins), []string{"", ".jet"})
}

// ResetCompositeSetCache 清空公共缓存。
func ResetCompositeSetCache() { jetpkg.ResetCompositeSetCache() }

// embeddedComponentLoader 返回应用内置组件模板的 loader。
//
// 必须合并构建器登记的组件自带模板（core.container / core.card 等）：集中目录里只有
// 少量公共片段，真正的组件模板由各组件包 core.RegisterTemplate 登记。
// 插件装配走的是这条路径，漏掉它会让含插件模板的页面以「获取组件模板失败」编译中断。
func embeddedComponentLoader() (*jetpkg.FSLoader, error) {
	loader, err := newEmbeddedComponentLoader()
	if err != nil {
		return nil, fmt.Errorf("内置组件模板加载失败: %w", err)
	}
	return withOwnedTemplates(loader), nil
}

// toJetPluginFS 把应用的插件模板源交给公共 loader。
func toJetPluginFS(plugins []PluginFS) []jetpkg.PluginFS {
	if len(plugins) == 0 {
		return nil
	}
	out := make([]jetpkg.PluginFS, 0, len(plugins))
	for _, p := range plugins {
		out = append(out, jetpkg.PluginFS{ID: p.ID, FS: p.FS})
	}
	return out
}
