package builder

// addtocart_render_test.go — 加购组件的**产物级**验证。
//
// 组件包的测试覆盖 BuildView 与样式编译；模板对不对只能在编译产物里看：
// Jet 里一个写错的变量通常渲染成空串而不是报错 —— 那正是「组件测试全绿、
// 线上按钮点了没反应」的成因。所以这里断言的是产物 HTML 里真的有什么。

import (
	"encoding/json"
	"strings"
	"testing"

	"go_wp/internal/templates"
)

// addToCartResolver 按字段名给值的桩解析器（字段语义由 cart/product 模块测试覆盖，
// 这里只验证「值能进到产物里」）。
type addToCartResolver map[string]string

func (r addToCartResolver) ResolveString(field string) (string, error) { return r[field], nil }

// addToCartDoc 含一个加购节点的最小文档。
func addToCartDoc(t *testing.T, props map[string]any) *Page {
	t.Helper()
	doc, err := json.Marshal(map[string]any{
		"settings": map[string]any{"layout": map[string]any{"mode": "full"}},
		"root":     []any{map[string]any{"id": "buy", "type": "core.addToCart", "props": props}},
	})
	if err != nil {
		t.Fatalf("序列化文档失败: %v", err)
	}
	page, err := ParsePage(doc)
	if err != nil {
		t.Fatalf("文档解析失败: %v", err)
	}
	return page
}

// TestAddToCartCompilesToWorkingForm 编译产物里必须是一个能真正提交的表单。
func TestAddToCartCompilesToWorkingForm(t *testing.T) {
	set, err := templates.NewComponentSet("../templates/components")
	if err != nil {
		t.Fatalf("NewComponentSet: %v", err)
	}
	page := addToCartDoc(t, map[string]any{
		"source":        "product",
		"optionsField":  "product.options",
		"variantsField": "product.variants",
		"variantMode":   "single",
		"showQuantity":  true,
		"buttonText":    "马上买",
		"cartTarget":    "#cart",
	})
	res := addToCartResolver{
		"product.options":  `[{"key":"color","name":"颜色","values":[{"key":"red","label":"红色"},{"key":"blue","label":"蓝色"}]}]`,
		"product.variants": `[{"id":"v-red","sku":"SKU-RED","price":"99.00","enabled":true,"options":{"color":"red"}},{"id":"v-blue","sku":"SKU-BLUE","price":"89.00","enabled":true,"options":{"color":"blue"}},{"id":"v-off","sku":"SKU-OFF","price":"79.00","enabled":false,"options":{"color":"blue"}}]`,
	}
	compiled, err := Compile(page, WithComponentSet(set), WithContentResolver(res), WithProjectID("proj-9"))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	doc, err := RenderDocument(compiled)
	if err != nil {
		t.Fatalf("组装文档失败: %v", err)
	}

	for _, want := range []string{
		`action="/_fragments/cartAdd"`,    // 提交端点
		`hx-post="/_fragments/cartAdd"`,   // HTMX 路径
		`hx-target="#cart"`,               // 购物车容器（默认值真的进了产物）
		`name="projectId" value="proj-9"`, // 工程 id 来自构建上下文
		`name="variantId" value="v-red"`,  // 变体 id 烘进 hidden input
		`name="quantity"`,                 // 数量输入（showQuantity=true）
		`type="submit"`,
		`>马上买<`, // 作者自定义的按钮文字
		`99.00`, // 与规格选择器同源的价格
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("产物缺少 %q；实际产物：%s", want, doc)
		}
	}
	// 已停用的变体不能出现在可加购列表里（停用的规格不能买）。
	if strings.Contains(doc, "v-off") {
		t.Errorf("已停用变体不该进产物；实际产物：%s", doc)
	}
}

// TestAddToCartPerVariantCompilesAllEnabledVariants 逐变体模式：每个启用变体一个表单。
func TestAddToCartPerVariantCompilesAllEnabledVariants(t *testing.T) {
	set, err := templates.NewComponentSet("../templates/components")
	if err != nil {
		t.Fatalf("NewComponentSet: %v", err)
	}
	page := addToCartDoc(t, map[string]any{
		"source":        "product",
		"optionsField":  "product.options",
		"variantsField": "product.variants",
		"variantMode":   "perVariant",
	})
	res := addToCartResolver{
		"product.options":  `[{"key":"color","name":"颜色","values":[{"key":"red","label":"红色"},{"key":"blue","label":"蓝色"}]}]`,
		"product.variants": `[{"id":"v-red","sku":"SKU-RED","price":"99.00","enabled":true,"options":{"color":"red"}},{"id":"v-blue","sku":"SKU-BLUE","price":"89.00","enabled":true,"options":{"color":"blue"}}]`,
	}
	compiled, err := Compile(page, WithComponentSet(set), WithContentResolver(res), WithProjectID("proj-9"))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	doc, err := RenderDocument(compiled)
	if err != nil {
		t.Fatalf("组装文档失败: %v", err)
	}
	if got := strings.Count(doc, `type="submit"`); got != 2 {
		t.Errorf("逐变体模式应输出 2 个加购按钮，实际 %d；实际产物：%s", got, doc)
	}
	for _, want := range []string{"v-red", "v-blue", "红色", "蓝色"} {
		if !strings.Contains(doc, want) {
			t.Errorf("产物缺少 %q（规格标签与变体 id 都要有，否则两行长得一样）；实际产物：%s", want, doc)
		}
	}
}

// TestAddToCartWithoutProjectIDStaysCompilable 缺工程 id 时降级为提示而不是构建失败。
//
// 这条守的是「一个按钮坏了不能整页发布不了」：pipeline.DefaultCompile 明确不带工程 id。
func TestAddToCartWithoutProjectIDStaysCompilable(t *testing.T) {
	set, err := templates.NewComponentSet("../templates/components")
	if err != nil {
		t.Fatalf("NewComponentSet: %v", err)
	}
	page := addToCartDoc(t, map[string]any{
		"source":        "product",
		"optionsField":  "product.options",
		"variantsField": "product.variants",
	})
	res := addToCartResolver{"product.options": "[]", "product.variants": "[]"}
	compiled, err := Compile(page, WithComponentSet(set), WithContentResolver(res))
	if err != nil {
		t.Fatalf("缺工程 id 不该让整页编译失败: %v", err)
	}
	doc, err := RenderDocument(compiled)
	if err != nil {
		t.Fatalf("组装文档失败: %v", err)
	}
	if !strings.Contains(doc, "暂不可用") {
		t.Errorf("缺工程 id 时应渲染可见提示；实际产物：%s", doc)
	}
}
