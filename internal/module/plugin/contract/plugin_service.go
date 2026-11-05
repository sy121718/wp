package plugincontract

import (
	"context"

	"go_wp/pkg/builder/source"
	admincontract "go_wp/internal/module/admin/contract"
	plugindto "go_wp/internal/module/plugin/dto"
)

type (
	PluginResp       = plugindto.PluginResp
	ToggleReq        = plugindto.ToggleReq
	UninstallReq     = plugindto.UninstallReq
	DetailReq        = plugindto.DetailReq
	ComponentSummary = plugindto.ComponentSummary
	PresetSummary    = plugindto.PresetSummary
	SchemaInfo       = plugindto.SchemaInfo
	PatrolResp       = plugindto.PatrolResp
)

type PluginService interface {
	Install(ctx context.Context, zipBytes []byte) (res *plugindto.PluginResp, err error)

	List(ctx context.Context) (list []*plugindto.PluginResp, err error)

	Toggle(ctx context.Context, req *plugindto.ToggleReq) (err error)

	Uninstall(ctx context.Context, req *plugindto.UninstallReq) (err error)

	Detail(ctx context.Context, req *plugindto.DetailReq) (res *plugindto.PluginResp, err error)

	PatrolArtifacts(ctx context.Context) (res *plugindto.PatrolResp, err error)

	EnabledAssembly(ctx context.Context) (asm *Assembly, err error)

	AdminAuthz() admincontract.AuthzContextService
}

type Assembly struct {
	Fingerprint string

	PluginFS []source.PluginFS

	Specs map[string]*source.PluginComponentSpec

	InspectorSchemas map[string][]byte

	Components []plugindto.ComponentSummary

	Presets []plugindto.PresetSummary

	ExtraCSS []string
}

type pluginSpecResolver map[string]*source.PluginComponentSpec

func (m pluginSpecResolver) LookupPluginComponent(typeName string) (*source.PluginComponentSpec, bool) {
	spec, ok := m[typeName]
	return spec, ok
}

func AssemblyResolver(a *Assembly) source.PluginResolver {
	if a == nil || len(a.Specs) == 0 {
		return pluginSpecResolver{}
	}
	return pluginSpecResolver(a.Specs)
}
