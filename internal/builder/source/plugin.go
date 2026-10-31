package source

import "io/fs"

type PluginPropControl struct {
	Kind string `json:"kind"`

	Label string `json:"label,omitempty"`

	Default any `json:"default,omitempty"`

	Options []string `json:"options,omitempty"`

	MaxLen int `json:"maxlen,omitempty"`
}

type CollectionBinding struct {
	Source string

	Fields []string

	Filter map[string]string
}

type StyleSink interface {
	Add(breakpoint, selector string, decls []string)

	AddHover(selector string, decls []string)

	AddHoverNone(selector string, decls []string)

	AddActive(selector string, decls []string)

	AddContainer(condition, selector string, decls []string)

	AddThemeQuery(containerName, prop, value, selector string, decls []string)

	AddStyleQuery(containerName, prop, value, selector string, decls []string)
}

type PluginComponentSpec struct {
	Type string

	Label string

	Template string

	Props map[string]PluginPropControl

	CompileStyles func(nodeID string, props map[string]any, b StyleSink) error

	HasChildren bool

	Collection *CollectionBinding
}

type PluginResolver interface {
	LookupPluginComponent(typeName string) (*PluginComponentSpec, bool)
}

type PluginFS struct {
	ID string

	FS fs.FS
}
