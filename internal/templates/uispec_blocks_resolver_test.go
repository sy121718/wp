package templates

import "context"

type fixedResolver struct {
	data map[string]any
}

func (r *fixedResolver) Resolve(_ context.Context, source string, _ map[string]string, _ int) (any, error) {
	return r.data[source], nil
}
