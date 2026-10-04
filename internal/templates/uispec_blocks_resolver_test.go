package templates

import "context"

// fixedResolver 是 uispec.Resolver 的测试实现（片段测试只关心渲染，取数走预置数据）。
type fixedResolver struct {
	data map[string]any
}

func (r *fixedResolver) Resolve(_ context.Context, source string, _ map[string]string, _ int) (any, error) {
	return r.data[source], nil
}
