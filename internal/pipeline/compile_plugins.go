package pipeline

import (
	"context"
	"strings"

	"go_wp/internal/builder"
	plugincontract "go_wp/internal/module/plugin/contract"
	"go_wp/internal/templates"

	"github.com/CloudyKit/jet/v6"
)

type PluginAssemblyPort interface {
	EnabledAssembly(ctx context.Context) (*plugincontract.Assembly, error)
}

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

func ComponentSetWithPlugins(asm *plugincontract.Assembly) (set *jet.Set, opts []builder.CompileOption, err error) {
	set, err = templates.NewEmbeddedComponentSet()
	if err != nil {
		return nil, nil, err
	}
	if asm != nil && len(asm.PluginFS) > 0 {

		set, err = templates.NewCompositeSetCached(asm.Fingerprint, asm.PluginFS)
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
