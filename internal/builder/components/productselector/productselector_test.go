package productselector

// productselector_test.go — 规格选择器组件单元测试（issue #26）。
//
// 覆盖：配置期校验（数据源 / 字段路径 / 开关）、字段绑定自报、视图组装（有组合 / 单变体空态）、
// 模板渲染（实时库存片段的变体 id 与兜底文案）。

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"go_wp/internal/builder/core"
	"go_wp/internal/templates"
)

// stubResolver 固定字段值映射的解析器（替代构建期的商品解析器）。
type stubResolver struct {
	values map[string]string
	errOn  string
}

func (s stubResolver) ResolveString(field string) (string, error) {
	if s.errOn != "" && field == s.errOn {
		return "", errors.New("字段越界")
	}
	return s.values[field], nil
}

func propsOf(t *testing.T, kv map[string]any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(kv)
	if err != nil {
		t.Fatalf("props 编码失败: %v", err)
	}
	return b
}

func decodePropsOf(t *testing.T, kv map[string]any) Props {
	t.Helper()
	var p Props
	if err := json.Unmarshal(propsOf(t, kv), &p); err != nil {
		t.Fatalf("props 解码失败: %v", err)
	}
	return p
}

const (
	testOptions  = `[{"key":"color","name":"颜色","values":[{"key":"red","label":"红"},{"key":"blue","label":"蓝"}]}]`
	testVariants = `[{"id":"var-1","sku":"SKU-1","price":"99","comparePrice":"129","enabled":true,"options":{"color":"red"}},` +
		`{"id":"var-2","sku":"SKU-2","price":"109","enabled":true,"options":{"color":"blue"}}]`
)

// TestValidateRequiresProductSource 字段必须来自 product 数据源。
func TestValidateRequiresProductSource(t *testing.T) {
	cases := []struct {
		name string
		kv   map[string]any
		want string
	}{
		{"缺字段名", map[string]any{"optionsField": "product"}, "无效的字段路径"},
		{"跨数据源", map[string]any{"optionsField": "article.options"}, "不属于数据源 product"},
		{"非法开关", map[string]any{"stock": "on"}, "无效的实时可用量开关"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := decodePropsOf(t, c.kv)
			if err := validateExtra(&p, "n1"); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("应报 %q，实际 %v", c.want, err)
			}
		})
	}
	// 空配置取默认值：合法。
	def := decodePropsOf(t, map[string]any{})
	if err := validateExtra(&def, "n1"); err != nil {
		t.Fatalf("默认配置应合法: %v", err)
	}
}

// TestFieldBindings 自报两个商品字段绑定（保存期交注册表白名单校验）。
func TestFieldBindings(t *testing.T) {
	comp := &Component{}
	node := &core.Node{ID: "n1", Type: Type, Props: propsOf(t, map[string]any{})}
	refs, err := comp.FieldBindings(node)
	if err != nil {
		t.Fatalf("收集绑定失败: %v", err)
	}
	want := []string{"options", "variants"}
	if len(refs) != len(want) {
		t.Fatalf("应自报 %d 个绑定，实际 %+v", len(want), refs)
	}
	for i, field := range want {
		if refs[i].EntityType != "product" || refs[i].Field != field {
			t.Fatalf("第 %d 个绑定应为 product.%s，实际 %+v", i, field, refs[i])
		}
	}
}

// TestBuildViewRendersOptionsAndStock 视图组装 + 模板渲染：
// 维度值组、组合行（价格 / 划线价）、每档实时库存片段（变体 id + 兜底文案）。
func TestBuildViewRendersOptionsAndStock(t *testing.T) {
	p := decodePropsOf(t, map[string]any{})
	view, err := BuildView(&p, stubResolver{values: map[string]string{
		"product.options":  testOptions,
		"product.variants": testVariants,
	}})
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	if !view.HasOptions || len(view.OptionGroups) != 1 || len(view.VariantOptions) != 2 {
		t.Fatalf("视图应含 1 个维度 + 2 个组合: %+v", view)
	}
	if view.VariantOptions[0].ID != "var-1" || view.VariantOptions[0].Price != "¥99" {
		t.Fatalf("组合行应带变体 id 与货币前缀价格: %+v", view.VariantOptions[0])
	}

	set, err := templates.NewEmbeddedComponentSet()
	if err != nil {
		t.Fatalf("NewEmbeddedComponentSet: %v", err)
	}
	tpl, err := set.GetTemplate("product_selector")
	if err != nil {
		t.Fatalf("GetTemplate(product_selector): %v", err)
	}
	var buf strings.Builder
	if err := tpl.Execute(&buf, nil, struct {
		Classes  string
		CustomID string
		NodeID   string
		V        View
	}{Classes: "sky-c-s1", NodeID: "s1", V: view}); err != nil {
		t.Fatalf("渲染模板失败: %v", err)
	}
	html := buf.String()
	for _, want := range []string{
		"sky-selector-group",
		"sky-selector-radio",
		"<label class=\"sky-selector-value\" for=\"sky-sel-s1-color-red\">红</label>",
		"/_fragments/productVariantAvailability?variantIds=var-1",
		"hx-trigger=\"load, every 60s\"",
		`aria-live="polite"`,
		StockFallback,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("产物缺少 %q\n%s", want, html)
		}
	}
}

// TestBuildViewSingleVariant 单变体（没有可切换的组合）→ 空态，不输出选择器。
func TestBuildViewSingleVariant(t *testing.T) {
	p := decodePropsOf(t, map[string]any{"emptyText": "该商品暂无可选规格"})
	view, err := BuildView(&p, stubResolver{values: map[string]string{
		"product.options":  `[]`,
		"product.variants": `[{"id":"only","sku":"SKU-1","price":"99","enabled":true,"options":{}}]`,
	}})
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	if view.HasOptions {
		t.Fatalf("单变体不该输出选择器: %+v", view)
	}
	if !view.HasEmpty || view.EmptyText != "该商品暂无可选规格" {
		t.Fatalf("应带空态文案: %+v", view)
	}
}

// TestBuildViewErrors 缺解析器 / 字段越界都要上抛（不静默出空选择器）。
func TestBuildViewErrors(t *testing.T) {
	p := decodePropsOf(t, map[string]any{})
	if _, err := BuildView(&p, nil); err == nil {
		t.Fatalf("缺解析器应报错")
	}
	if _, err := BuildView(&p, stubResolver{errOn: "product.options"}); err == nil {
		t.Fatalf("字段越界应上抛")
	}
}

// TestShowStockOff 关闭实时可用量时不渲染片段容器（但兜底文案与组合行仍在）。
func TestShowStockOff(t *testing.T) {
	p := decodePropsOf(t, map[string]any{"stock": "off"})
	if ShowStock(&p) {
		t.Fatalf("stock=off 应关闭片段")
	}
	view, err := BuildView(&p, stubResolver{values: map[string]string{
		"product.options":  testOptions,
		"product.variants": testVariants,
	}})
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	if view.ShowStock {
		t.Fatalf("视图应标记关闭实时可用量")
	}
}
