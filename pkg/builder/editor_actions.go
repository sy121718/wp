package builder

import "go_wp/pkg/builder/core"

type EditorActionSpec struct {
	Key          string            `json:"key"`
	AllowedKinds []core.ActionKind `json:"allowedKinds"`
}

func ComponentActions() map[string][]EditorActionSpec {
	out := make(map[string][]EditorActionSpec)
	for _, name := range core.Types() {
		component, err := core.Lookup(name)
		if err != nil {
			continue
		}
		provider, ok := component.(core.ActionsProvider)
		if !ok {
			continue
		}
		for _, action := range provider.Actions() {
			out[name] = append(out[name], EditorActionSpec{Key: action.Key, AllowedKinds: append([]core.ActionKind(nil), action.AllowedKinds...)})
		}
	}
	return out
}
