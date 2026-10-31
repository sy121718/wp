package workbenchhttp

import (
	"maps"
	"sync"

	json "go_wp/pkg/jsonx"

	"go_wp/internal/builder"
	plugincontract "go_wp/internal/module/plugin/contract"
)

var componentSchemaSnapshot = sync.OnceValues(func() (map[string]json.RawMessage, error) {
	return builder.ComponentSchemas()
})

var componentSchemaSnapshotFunc = componentSchemaSnapshot

func componentsForCanvas(asm *plugincontract.Assembly) (map[string]json.RawMessage, error) {
	base, err := componentSchemaSnapshotFunc()
	if err != nil {
		return nil, err
	}
	out := maps.Clone(base)
	if out == nil {

		out = map[string]json.RawMessage{}
	}
	if asm == nil {
		return out, nil
	}
	for t, data := range asm.InspectorSchemas {
		out[t] = data
	}
	return out, nil
}
