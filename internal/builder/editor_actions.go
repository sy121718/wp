package builder

import "go_wp/internal/builder/core"

// EditorActionSpec 仅搬运动作声明；目标合法性仍由发布编译器判断。
type EditorActionSpec struct {
	Key          string            `json:"key"`
	AllowedKinds []core.ActionKind `json:"allowedKinds"`
}

// ComponentActions 为每次工作台请求返回独立副本，避免编辑元信息污染全局注册。
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
