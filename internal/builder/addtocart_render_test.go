package builder

import (
	"errors"
	"go_wp/internal/templates"
	"go_wp/pkg/builder/core"
	json "go_wp/pkg/jsonx"
	"strings"
	"testing"
)

type addToCartResolver map[string]string

func (r addToCartResolver) ResolveString(field string) (string, error) { return r[field], nil }
func addToCartDoc(t *testing.T, props map[string]any) *Page {
	t.Helper()
	raw, err := json.Marshal(props)
	if err != nil {
		t.Fatal(err)
	}
	return &Page{Settings: PageSettings{Layout: PageLayout{Mode: LayoutFull}}, Root: []*core.Node{{ID: "buy", Type: "core.addToCart", Props: raw}}}
}
func TestAddToCartCompilesBuyerChoiceForCurrentProduct(t *testing.T) {
	set, err := templates.NewEmbeddedComponentSet()
	if err != nil {
		t.Fatal(err)
	}
	res := addToCartResolver{"product.options": `[{"key":"color","name":"颜色","values":[{"key":"red","label":"红色"},{"key":"blue","label":"蓝色"}]}]`, "product.variants": `[{"id":"v-red","enabled":true,"options":{"color":"red"}},{"id":"v-blue","enabled":true,"options":{"color":"blue"}},{"id":"v-off","enabled":false}]`}
	page := addToCartDoc(t, map[string]any{"label": "马上买", "showQuantity": true})
	compiled, err := Compile(page, WithComponentSet(set), WithContentResolver(res), WithProjectID("proj-9"), WithArchiveEntity("product", "product-current"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`data-component-instance="buy"`, `data-write-url="/api/site/cart/add"`, `data-write-handler="commerce.cart"`, `name="projectId" value="proj-9"`, `<select name="variantId" aria-label="请选择规格" required>`, `<option value="">请选择规格</option>`, `value="v-red"`, `value="v-blue"`, `name="quantity"`, `>马上买<`} {
		if !strings.Contains(compiled.HTML, want) {
			t.Errorf("缺少 %s: %s", want, compiled.HTML)
		}
	}
	if strings.Contains(compiled.HTML, "v-off") || strings.Count(compiled.HTML, `<form `) != 1 {
		t.Fatalf("当前商品仅一个表单，停用规格不得进入: %s", compiled.HTML)
	}
}
func TestAddToCartRequiresProductArchiveEvenWithArbitraryContent(t *testing.T) {
	set, err := templates.NewEmbeddedComponentSet()
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"", "article"} {
		page := addToCartDoc(t, map[string]any{})
		opts := []CompileOption{WithComponentSet(set), WithProjectID("p"), WithContentResolver(addToCartResolver{"product.options": "[]", "product.variants": `[{"id":"other","enabled":true}]`}), WithArchiveEntity(kind, "other")}
		preview, err := Compile(page, append(opts, WithCompileMode(CompileModePreview))...)
		if err != nil || !strings.Contains(preview.HTML, `data-sky-incomplete="true"`) {
			t.Fatalf("非商品上下文应局部提示: %v", err)
		}
		if _, err := Compile(page, append(opts, WithCompileMode(CompileModePublish))...); !errors.Is(err, core.ErrIncompleteNode) {
			t.Fatalf("发布必须拒绝非当前商品上下文: %v", err)
		}
	}
}
func TestAddToCartRejectsRetiredProductWiring(t *testing.T) {
	set, err := templates.NewEmbeddedComponentSet()
	if err != nil {
		t.Fatal(err)
	}
	for _, props := range []map[string]any{{"productSource": "specific", "productId": "another"}, {"variantId": "fixed"}, {"currency": "¥"}} {
		if _, err := Compile(addToCartDoc(t, props), WithComponentSet(set), WithCompileMode(CompileModePreview)); err == nil || !strings.Contains(err.Error(), "改为当前商品加购") {
			t.Fatalf("旧接线必须明确清理: %v", err)
		}
	}
}

func TestAddToCartUnavailableCurrentProductKeepsPublishedPage(t *testing.T) {
	set, err := templates.NewEmbeddedComponentSet()
	if err != nil {
		t.Fatal(err)
	}
	for _, variants := range []string{`[]`, `[{"id":"off","enabled":false}]`} {
		page := addToCartDoc(t, map[string]any{"showQuantity": true})
		page.Root = append([]*core.Node{{ID: "context", Type: "core.heading", Props: json.RawMessage(`{"text":"合法当前商品仍显示"}`)}}, page.Root...)
		compiled, err := Compile(page, WithComponentSet(set), WithCompileMode(CompileModePublish), WithProjectID("proj-9"), WithArchiveEntity("product", "product-current"), WithContentResolver(addToCartResolver{"product.options": "[]", "product.variants": variants}))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"合法当前商品仍显示", `type="button" disabled`, `role="status">当前商品暂不可购买</p>`} {
			if !strings.Contains(compiled.HTML, want) {
				t.Fatalf("禁用态缺少%s: %s", want, compiled.HTML)
			}
		}
		for _, bad := range []string{`<form `, `data-write=`, `data-site-read=`, `data-requires-js`, `sky-incomplete`, `name="variantId"`, `name="quantity"`} {
			if strings.Contains(compiled.HTML, bad) {
				t.Fatalf("不可购商品不得输出%s: %s", bad, compiled.HTML)
			}
		}
	}
}
