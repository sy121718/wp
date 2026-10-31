package builder

import (
	"context"
	"strings"
	"testing"

	cardstack "go_wp/internal/builder/components/cardstack"
	productcard "go_wp/internal/builder/components/productcard"
	productlist "go_wp/internal/builder/components/productlist"
	"go_wp/internal/builder/core"
)

type stubEntitySource struct {
	entityType string
	fields     []string
}

func (s stubEntitySource) EntityType() string       { return s.entityType }
func (s stubEntitySource) FieldWhitelist() []string { return append([]string(nil), s.fields...) }
func (s stubEntitySource) ResolverFor(context.Context, string) (core.ContentResolver, error) {
	return nil, nil
}

func stubRegistry(t *testing.T) core.EntitySourceRegistry {
	t.Helper()
	reg := core.NewEntitySourceRegistry()
	for _, src := range []stubEntitySource{
		{entityType: "product", fields: []string{"name", "images", "priceRange", "url"}},
		{entityType: "product_category", fields: []string{"name", "slug", "seoTitle"}},
		{entityType: "article", fields: []string{"title", "excerpt"}},
	} {
		if err := reg.Register(src); err != nil {
			t.Fatalf("注册实体类型 %s 失败: %v", src.entityType, err)
		}
	}
	return reg
}

func productListDocOf(source string) string {
	props := `"titleField":"item.name","imageField":"item.images","linkField":"item.url"`
	if source != "" {
		props = `"collectionSource":"` + source + `",` + props
	}
	return `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"pl1","type":"` + productlist.Type + `","props":{` + props + `}}]}`
}

func productCardDoc() string {
	return `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"pc1","type":"` + productcard.Type + `","props":{"titleField":"product.name","imageField":"product.images"}}]}`
}

func categoryRefDoc() string {
	return `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"cs1","type":"` + cardstack.Type + `","props":{"collectionSource":"content:product"},"children":[{"id":"pc1","type":"` + productcard.Type + `","props":{"titleField":"product.name"}}]}]}`
}

func mustParseDoc(t *testing.T, doc string) *Page {
	t.Helper()
	page, err := ParsePage([]byte(doc))
	if err != nil {
		t.Fatalf("解析文档失败: %v", err)
	}
	return page
}

func TestValidateFieldRefsCollectionSource(t *testing.T) {
	reg := stubRegistry(t)
	cases := []struct {
		name       string
		doc        string
		entityType string
		wantErr    string
	}{
		{
			name: "商品集合组件在分类归档模板里放行", doc: productListDocOf("content:product"),
			entityType: "product_category",
		},
		{
			name: "商品集合组件的子节点模板同属集合源", doc: categoryRefDoc(),
			entityType: "product_category",
		},
		{
			name: "集合组件没选集合源仍按模板实体类型拒绝", doc: productListDocOf(""),
			entityType: "product_category", wantErr: "不属于 product_category 数据源",
		},
		{
			name: "非集合组件的跨源绑定仍被拒绝", doc: productCardDoc(),
			entityType: "product_category", wantErr: "不属于 product_category 数据源",
		},
		{
			name: "商品模板里的集合组件照旧放行", doc: productListDocOf("content:product"),
			entityType: "product",
		},
		{
			name: "商品模板里的非集合组件照旧放行", doc: productCardDoc(),
			entityType: "product",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateFieldRefs(mustParseDoc(t, tc.doc), tc.entityType, reg)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("应通过校验，实际报错: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("应被拒绝（%s），实际通过", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("拒绝原因应含 %q，实际: %v", tc.wantErr, err)
			}
		})
	}
}

func TestCollectFieldRefsCarriesCollectionSource(t *testing.T) {
	refs, err := CollectFieldRefs(mustParseDoc(t, categoryRefDoc()))
	if err != nil {
		t.Fatalf("收集字段绑定失败: %v", err)
	}
	if len(refs) == 0 {
		t.Fatalf("应收集到子节点商品卡的字段绑定")
	}
	for _, ref := range refs {
		if ref.CollectionSource != "content:product" {
			t.Fatalf("绑定应带上集合源的标识，实际 %+v", ref)
		}
	}

	plain, err := CollectFieldRefs(mustParseDoc(t, productCardDoc()))
	if err != nil {
		t.Fatalf("收集字段绑定失败: %v", err)
	}
	for _, ref := range plain {
		if ref.CollectionSource != "" {
			t.Fatalf("非集合组件的绑定不该带集合源，实际 %+v", ref)
		}
	}
}

func TestCollectionEntityType(t *testing.T) {
	cases := map[string]string{
		"content:product":   "product",
		"content:article":   "article",
		" content:product":  "product",
		"":                  "",
		"plugin:shop.items": "",
		"content:":          "",
	}
	for in, want := range cases {
		if got := core.CollectionEntityType(in); got != want {
			t.Fatalf("CollectionEntityType(%q) = %q，期望 %q", in, got, want)
		}
	}
}
