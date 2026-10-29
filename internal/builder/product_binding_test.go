package builder

import (
	"context"
	"errors"
	"strings"
	"testing"

	"go_wp/internal/builder/core"
	"go_wp/internal/builder/source"
	productdto "go_wp/internal/module/product/dto"
	"go_wp/internal/templates"
	json "go_wp/pkg/jsonx"
)

const bindingProductID = "8ab20021-5608-4d9b-98ca-8ba772189b08"
const bindingVariantID = "883170e5-50ba-46c6-b041-55b700a741a6"

type bindingProductSource struct {
	data *productdto.PublicProductOptionsResp
	err  error
}

func (s bindingProductSource) ResolveCollection(context.Context, string, map[string]string) ([]map[string]any, error) {
	return nil, nil
}
func (s bindingProductSource) CollectionSchemas(context.Context) ([]source.CollectionSchema, error) {
	return nil, nil
}
func (s bindingProductSource) CollectionFilterOptions(context.Context, string, string) (source.CollectionFilterOptions, error) {
	return source.CollectionFilterOptions{}, nil
}
func (s bindingProductSource) PublicProductOptions(context.Context, *productdto.PublicProductOptionsReq) (*productdto.PublicProductOptionsResp, error) {
	return s.data, s.err
}

func productBindingPage(kind, props string) *Page {
	return &Page{Settings: PageSettings{Layout: PageLayout{Mode: LayoutFull}}, Root: []*core.Node{
		{ID: "kept", Type: "core.heading", Props: json.RawMessage(`{"text":"完整页面保留"}`)},
		{ID: "cart", Name: "商品配置", Type: kind, Props: json.RawMessage(props)},
	}}
}

func TestProductBindingPreviewOnlyToleratesIncomplete(t *testing.T) {
	set, err := templates.NewEmbeddedComponentSet()
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"core.addToCart", "core.productSelector"} {
		for _, props := range []string{`{}`, `{"productSource":"specific"}`} {
			page := productBindingPage(kind, props)
			compiled, err := Compile(page, WithCompileMode(CompileModePreview), WithComponentSet(set))
			if err != nil {
				t.Fatalf("%s未配置商品不应阻断预览: %v", kind, err)
			}
			if !strings.Contains(compiled.HTML, "完整页面保留") || !strings.Contains(compiled.HTML, `data-sky-id="cart"`) || strings.Count(compiled.HTML, `data-sky-incomplete="true"`) != 1 {
				t.Fatalf("只应占位未配置节点: %s", compiled.HTML)
			}
			if _, err = Compile(page, WithCompileMode(CompileModePublish), WithComponentSet(set)); !errors.Is(err, core.ErrIncompleteNode) {
				t.Fatalf("发布应拒绝未配置商品: %v", err)
			}
		}
	}
}

func TestProductBindingSpecificProductAndVariant(t *testing.T) {
	set, err := templates.NewEmbeddedComponentSet()
	if err != nil {
		t.Fatal(err)
	}
	data := &productdto.PublicProductOptionsResp{ID: bindingProductID, Name: "设计杯", Variants: []productdto.PublicProductVariantOptionResp{{ID: bindingVariantID, SKU: "CUP", Price: 2900, Options: map[string]string{}}}}
	src := bindingProductSource{data: data}
	props := `{"productSource":"specific","productId":"` + bindingProductID + `","variantId":"` + bindingVariantID + `"}`
	opts := []CompileOption{WithCompileMode(CompileModePublish), WithComponentSet(set), WithProjectID(bindingProductID), WithProductDataSource(src)}
	compiled, err := Compile(productBindingPage("core.addToCart", props), opts...)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(compiled.HTML, `value="`+bindingVariantID+`"`) || strings.Contains(compiled.HTML, "sky-incomplete") {
		t.Fatalf("指定商品应冻结正确规格: %s", compiled.HTML)
	}
	// 数据错误与失效引用不能被预览容错吞掉。
	cases := []struct {
		name, props string
		src         bindingProductSource
	}{
		{"未知商品", props, bindingProductSource{err: errors.New("商品不存在")}},
		{"规格不归属", strings.ReplaceAll(props, bindingVariantID, "f2b5c28c-aa2c-43b2-95d6-36dfc8ac2674"), src},
		{"非法商品ID", `{"productSource":"specific","productId":"not-a-uuid"}`, src},
		{"非法字段", `{"optionsField":"product.costPrice"}`, src},
		{"未完成仍拒绝非法字段", `{"productSource":"specific","optionsField":"product.costPrice"}`, src},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Compile(productBindingPage("core.addToCart", tc.props), WithCompileMode(CompileModePreview), WithComponentSet(set), WithProjectID(bindingProductID), WithProductDataSource(tc.src))
			if err == nil || errors.Is(err, core.ErrIncompleteNode) {
				t.Fatalf("必须明确拒绝非法或失效引用: %v", err)
			}
		})
	}
}
