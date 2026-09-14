package pipeline

// compile_plugins.go — 插件组件集与 CompileOption（page / presentation 共用，EDT-003）。

import (
	"context"
	"strings"

	"go_wp/internal/builder"
	plugincontract "go_wp/internal/module/plugin/contract"
	"go_wp/internal/templates"

	"github.com/CloudyKit/jet/v6"
)

// PluginAssemblyPort 启用插件装配查询（plugin 模块契约）。
type PluginAssemblyPort interface {
	EnabledAssembly(ctx context.Context) (*plugincontract.Assembly, error)
}

// LoadPluginAssembly 查询启用插件的编译素材；失败或未注入时返回 nil（不阻断构建）。
func LoadPluginAssembly(ctx context.Context, plugins PluginAssemblyPort) *plugincontract.Assembly {
	if plugins == nil {
		return nil
	}
	asm, err := plugins.EnabledAssembly(ctx)
	if err != nil {
		return nil
	}
	return asm
}

// ComponentSetWithPlugins 内置 embed 组件集 + 可选插件 CompositeSet 与 WithPluginResolver / ExtraCSS。
func ComponentSetWithPlugins(asm *plugincontract.Assembly) (set *jet.Set, opts []builder.CompileOption, err error) {
	set, err = templates.NewEmbeddedComponentSet()
	if err != nil {
		return nil, nil, err
	}
	if asm != nil && len(asm.PluginFS) > 0 {
		set, err = templates.NewCompositeSet(asm.PluginFS)
		if err != nil {
			return nil, nil, err
		}
		opts = append(opts, builder.WithPluginResolver(plugincontract.AssemblyResolver(asm)))
		if len(asm.ExtraCSS) > 0 {
			opts = append(opts, builder.WithExtraCSS(strings.Join(asm.ExtraCSS, "\n\n")))
		}
	}
	return set, opts, nil
}
