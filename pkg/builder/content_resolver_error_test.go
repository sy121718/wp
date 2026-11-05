package builder

import (
	"errors"
	"testing"

	"go_wp/internal/templates"
	"go_wp/pkg/builder/core"
	json "go_wp/pkg/jsonx"
)

func TestCompilePreservesContentResolverRequired(t *testing.T) {
	set, err := templates.NewEmbeddedComponentSet()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		typeName string
		props    string
	}{
		{"core.heading", `{"binding":{"field":"post.title"}}`},
		{"core.text", `{"binding":{"field":"post.title"}}`},
		{"core.image", `{"binding":{"field":"post.image"}}`},
		{"core.gallery", `{"binding":{"field":"post.images"}}`},
		{"core.button", `{"text":"打开","action":"link","binding":{"field":"post.url"}}`},
		{"core.product", `{"titleField":"product.name"}`},
		{"core.productCard", `{"titleField":"product.name"}`},
	}
	for _, tc := range cases {
		t.Run(tc.typeName, func(t *testing.T) {
			page := &Page{
				Settings: PageSettings{Layout: PageLayout{Mode: LayoutFull}},
				Root: []*core.Node{{
					ID: "bound-node", Type: tc.typeName, Props: json.RawMessage(tc.props),
				}},
			}
			_, err := Compile(page, WithComponentSet(set), WithCompileMode(CompileModePreview))
			if !errors.Is(err, core.ErrContentResolverRequired) {
				t.Fatalf("缺少内容解析器必须拒绝编译并保留错误链事实，实际: %v", err)
			}
		})
	}
}
