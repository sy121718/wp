package builder

// field_binding_collection_test.go — 集合组件按**集合源**实体类型校验（审计 EDT-004 收口）。
//
// 背景：分类归档模板（entity_type = product_category）里放一个 core.productList，
// 它绑的是 product.* —— 列表渲染的是商品集合，与模板实体不是同一个数据源。
// 旧口径（"声明的实体类型必须等于模板实体类型"）把这种绑定一律判成跨数据源而拒绝，
// 于是分类归档页在架构上根本列不出商品。
//
// 本文件守三条线，缺一条都会让改动变成"直接放行"：
//  1. 集合组件（声明了集合源）→ 按集合源实体类型校验，跨源绑定放行；
//  2. **非集合组件**绑跨源字段 → 仍然拒绝（这是新旧行为的分界线，
//     防止后来人为了省事把"属于哪个数据源"这条判定整个删掉）；
//  3. 集合组件**没选集合源** → 沿用模板实体类型，仍然拒绝；
//  4. 集合组件子树（cardstack 的子节点模板按集合项渲染）同属该集合源的作用域。

import (
	"context"
	"strings"
	"testing"

	cardstack "go_wp/internal/builder/components/cardstack"
	productcard "go_wp/internal/builder/components/productcard"
	productlist "go_wp/internal/builder/components/productlist"
	"go_wp/internal/builder/core"
)

// stubEntitySource 最小实体来源：只给一个类型 + 白名单。
type stubEntitySource struct {
	entityType string
	fields     []string
}

func (s stubEntitySource) EntityType() string       { return s.entityType }
func (s stubEntitySource) FieldWhitelist() []string { return append([]string(nil), s.fields...) }
func (s stubEntitySource) ResolverFor(context.Context, string) (core.ContentResolver, error) {
	return nil, nil
}

// stubRegistry 只用商品域 + 内容域的两个类型，避免依赖真实模块装配。
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

// productListDocOf 一份只含 core.productList 的文档（集合源可指定）。
func productListDocOf(source string) string {
	props := `"titleField":"item.name","imageField":"item.images","linkField":"item.url"`
	if source != "" {
		props = `"collectionSource":"` + source + `",` + props
	}
	return `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"pl1","type":"` + productlist.Type + `","props":{` + props + `}}]}`
}

// productCardDoc 一份只含 core.productCard 的文档（**非集合组件**）。
func productCardDoc() string {
	return `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"pc1","type":"` + productcard.Type + `","props":{"titleField":"product.name","imageField":"product.images"}}]}`
}

// categoryRefDoc 一份「集合组件（cardstack）+ 子节点商品卡」的文档：
// 集合模式下子节点是**集合项模板**，子节点绑定与集合源同源。
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

// TestValidateFieldRefsCollectionSource 集合组件按集合源实体类型校验。
func TestValidateFieldRefsCollectionSource(t *testing.T) {
	reg := stubRegistry(t)
	cases := []struct {
		name       string
		doc        string
		entityType string
		wantErr    string // 空 = 期望通过
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

// TestCollectFieldRefsCarriesCollectionSource 收集阶段必须把集合源带到引用上 ——
// 带不上时校验会退回模板实体类型，归档模板又被拒（改了校验却看不出效果）。
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
	// 非集合组件不带集合源（否则等于对所有绑定放行）。
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

// TestCollectionEntityType 集合源标识 → 实体类型的推导只有一处。
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
