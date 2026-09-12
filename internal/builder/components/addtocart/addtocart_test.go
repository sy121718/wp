package addtocart

// addtocart_test.go — 加购组件的校验与视图测试。
//
// 覆盖的是**不变量**而不是「函数能跑通」：
//   · 没有变体来源 / 规格维度的组件在保存期就被拒（否则产物里会有一个点了报错的按钮）；
//   · 变体标签与价格与规格选择器同源（复用 product 包的解析，测试用同一份 JSON 形状）；
//   · 未启用的变体不出现在可加购列表里（停用的规格不能买）；
//   · 没有启用变体时留提示而不是让整页构建失败（那是数据状态，不是配置错误）；
//   · 购物车容器选择器写错时回退默认值（错的选择器会让 HTMX 静默不刷新）。

import (
	"fmt"
	"strings"
	"testing"
)

// stubContent 按字段名返回值的解析器替身（只实现 ResolveString —— 那就是契约的全部）。
type stubContent map[string]string

func (s stubContent) ResolveString(field string) (string, error) {
	if v, ok := s[field]; ok {
		return v, nil
	}
	return "", fmt.Errorf("未知字段 %q", field)
}

// 样例数据：形状与 product.options / product.variants 一致（三个变体，一个未启用）。
const (
	optionsJSON  = "[{\"key\":\"color\",\"name\":\"颜色\",\"values\":[{\"key\":\"red\",\"label\":\"红色\"},{\"key\":\"blue\",\"label\":\"蓝色\"}]}]"
	variantsJSON = "[" +
		"{\"id\":\"v-red\",\"sku\":\"SKU-RED\",\"price\":\"99.00\",\"comparePrice\":\"129.00\",\"enabled\":true,\"options\":{\"color\":\"red\"}}," +
		"{\"id\":\"v-blue\",\"sku\":\"SKU-BLUE\",\"price\":\"89.00\",\"enabled\":true,\"options\":{\"color\":\"blue\"}}," +
		"{\"id\":\"v-off\",\"sku\":\"SKU-OFF\",\"price\":\"79.00\",\"enabled\":false,\"options\":{\"color\":\"blue\"}}" +
		"]"
)

// baseProps 一份齐备的 Props（各用例只在它上面改一处）。
func baseProps() *Props {
	return &Props{
		Source:        "product",
		OptionsField:  "product.options",
		VariantsField: "product.variants",
	}
}

func baseContent() stubContent {
	return stubContent{"product.options": optionsJSON, "product.variants": variantsJSON}
}

// TestValidateExtra 保存期校验：缺任一槽位都拒绝。
func TestValidateExtra(t *testing.T) {
	tests := []struct {
		name    string
		props   *Props
		wantErr bool
	}{
		{"齐备合法", baseProps(), false},
		{"缺规格维度字段", &Props{VariantsField: "product.variants"}, true},
		{"缺变体组合字段", &Props{OptionsField: "product.options"}, true},
		{"两者都缺", &Props{}, true},
		{"只填空格不算声明", &Props{OptionsField: "  ", VariantsField: "product.variants"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateExtra(tt.props, "n1")
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateExtra(%+v) err=%v, wantErr=%v", tt.props, err, tt.wantErr)
			}
		})
	}
}

// TestBuildViewSingleMode 单变体模式：只输出一行，取第一个启用变体，不带规格标签。
func TestBuildViewSingleMode(t *testing.T) {
	v, err := BuildView(baseProps(), baseContent(), "proj-1")
	if err != nil {
		t.Fatalf("BuildView 失败: %v", err)
	}
	if v.Notice != "" {
		t.Fatalf("不该有提示: %q", v.Notice)
	}
	if len(v.Rows) != 1 {
		t.Fatalf("单变体模式应只有一行，实际 %d", len(v.Rows))
	}
	row := v.Rows[0]
	if row.VariantID != "v-red" {
		t.Fatalf("应取第一个启用变体，实际 %q", row.VariantID)
	}
	if row.Label != "" {
		t.Fatalf("单变体模式不该输出规格标签，实际 %q", row.Label)
	}
	if !strings.Contains(row.Price, "99.00") {
		t.Fatalf("价格应带货币符号与数值，实际 %q", row.Price)
	}
	if v.Action != CartAddPath || v.Target != defaultCartTarget || v.ProjectID != "proj-1" {
		t.Fatalf("表单基础字段不对: %+v", v)
	}
}

// TestBuildViewPerVariantMode 逐变体模式：每个启用变体一行，带规格标签。
func TestBuildViewPerVariantMode(t *testing.T) {
	p := baseProps()
	p.VariantMode = ModePerVariant
	p.ShowQuantity = true
	v, err := BuildView(p, baseContent(), "proj-1")
	if err != nil {
		t.Fatalf("BuildView 失败: %v", err)
	}
	if len(v.Rows) != 2 {
		t.Fatalf("未启用的变体不该出现（应 2 行），实际 %d：%+v", len(v.Rows), v.Rows)
	}
	if v.Rows[0].Label == "" || v.Rows[1].Label == "" {
		t.Fatalf("逐变体模式必须输出规格标签，否则「红色」与「蓝色」两行长得一样: %+v", v.Rows)
	}
	if !strings.Contains(v.Rows[0].Label, "红色") || !strings.Contains(v.Rows[1].Label, "蓝色") {
		t.Fatalf("规格标签应含属性值展示名: %+v", v.Rows)
	}
	for _, row := range v.Rows {
		if row.VariantID == "v-off" {
			t.Fatal("已停用的变体不能出现在可加购列表里")
		}
	}
	if !v.ShowQuantity {
		t.Fatal("数量输入开关应透传")
	}
}

// TestBuildViewEmptyVariantsRendersNotice 没有启用变体时留提示，不让整页构建失败。
func TestBuildViewEmptyVariantsRendersNotice(t *testing.T) {
	content := stubContent{"product.options": optionsJSON, "product.variants": ""}
	v, err := BuildView(baseProps(), content, "proj-1")
	if err != nil {
		t.Fatalf("无变体属于数据状态，不该让构建失败: %v", err)
	}
	if v.Notice == "" {
		t.Fatal("无变体时应给出提示，而不是渲染一个点了没反应的按钮")
	}
	if len(v.Rows) != 0 {
		t.Fatalf("无变体时不该有加购行: %+v", v.Rows)
	}
}

// TestBuildViewWithoutProjectIDRendersNotice 缺站点工程 id 时降级为可见提示，不让整页构建失败。
//
// 这条边界要守住：pipeline.DefaultCompile 明确不解析站点级资源（不带工程 id），
// 把它当致命错误会让「草稿冻结一个含加购按钮的页面」直接失败 ——
// 一个按钮坏了整页发布不了，而页面其余部分明明好好的。
func TestBuildViewWithoutProjectIDRendersNotice(t *testing.T) {
	for _, pid := range []string{"", "   "} {
		v, err := BuildView(baseProps(), baseContent(), pid)
		if err != nil {
			t.Fatalf("缺工程 id 不该让构建失败（实际 %v）", err)
		}
		if v.Notice == "" {
			t.Fatal("缺工程 id 时必须给出可见提示，而不是渲染一个点了没反应的按钮")
		}
		if len(v.Rows) != 0 {
			t.Fatalf("不可用时不该输出加购行: %+v", v.Rows)
		}
	}
}

// TestBuildViewRequiresContentResolver 缺内容解析器时报错（字段根本无法解析）。
func TestBuildViewRequiresContentResolver(t *testing.T) {
	if _, err := BuildView(baseProps(), nil, "proj-1"); err == nil {
		t.Fatal("缺内容解析器时应构建失败")
	}
}

// TestBuildViewPropagatesResolveError 字段越界 / 解析失败原样上抛。
func TestBuildViewPropagatesResolveError(t *testing.T) {
	content := stubContent{"product.options": optionsJSON} // 缺 variants 字段
	if _, err := BuildView(baseProps(), content, "proj-1"); err == nil {
		t.Fatal("变体字段解析失败时应上抛错误")
	}
}

// TestEffectiveTargetFallsBack 购物车容器选择器：空白或不像选择器一律回退默认值。
func TestEffectiveTargetFallsBack(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", defaultCartTarget},
		{"   ", defaultCartTarget},
		{"#cart", "#cart"},
		{".cart-host", ".cart-host"},
		{"[data-cart]", "[data-cart]"},
		{"cart", "cart"},
		{"  #my-cart  ", "#my-cart"},
		// 单独一个符号不是选择器：留给 HTMX 只会在控制台留一条警告，页面上什么都不发生。
		{"#", defaultCartTarget},
		{".", defaultCartTarget},
	}
	for _, tt := range tests {
		got := effectiveTarget(&Props{CartTarget: tt.in})
		if got != tt.want {
			t.Errorf("effectiveTarget(%q)=%q, want=%q", tt.in, got, tt.want)
		}
	}
}

// TestApplyI18n 界面文案：默认按钮文字取译文，作者自定义的文案不动。
func TestApplyI18n(t *testing.T) {
	// 默认文案 → 取译文。
	v := &View{ButtonText: textFallbackButton}
	v.ApplyI18n(func(key, fallback string) string {
		if key != TextKeyButton {
			t.Errorf("文案键不对: %q", key)
		}
		return "Add to cart"
	})
	if v.ButtonText != "Add to cart" {
		t.Fatalf("默认按钮文字应取译文，实际 %q", v.ButtonText)
	}

	// 自定义文案 → 不翻译（它属于内容，走 Translatable 那条链路）。
	v2 := &View{ButtonText: "立即抢购"}
	v2.ApplyI18n(func(string, string) string { return "Buy now" })
	if v2.ButtonText != "立即抢购" {
		t.Fatalf("作者自定义的文案不该被界面翻译覆盖，实际 %q", v2.ButtonText)
	}

	// 无翻译函数 → 保持中文兜底，不 panic。
	v3 := &View{ButtonText: textFallbackButton}
	v3.ApplyI18n(nil)
	if v3.ButtonText != textFallbackButton {
		t.Fatalf("无翻译函数时应保持兜底文案，实际 %q", v3.ButtonText)
	}

	// nil 视图不 panic。
	var v4 *View
	v4.ApplyI18n(nil)
}

// TestEffectiveButtonText 按钮文字缺省。
func TestEffectiveButtonText(t *testing.T) {
	if got := effectiveButtonText(&Props{}); got != defaultButtonText {
		t.Fatalf("空按钮文字应回退默认值，实际 %q", got)
	}
	if got := effectiveButtonText(&Props{ButtonText: "  "}); got != defaultButtonText {
		t.Fatalf("纯空白应回退默认值，实际 %q", got)
	}
	if got := effectiveButtonText(&Props{ButtonText: "立刻购买"}); got != "立刻购买" {
		t.Fatalf("自定义文字应原样使用，实际 %q", got)
	}
}

// TestEffectiveMode 未知变体呈现方式兜底为单变体。
func TestEffectiveMode(t *testing.T) {
	if got := effectiveMode(&Props{VariantMode: "weird"}); got != ModeSingle {
		t.Fatalf("未知模式应兜底 single，实际 %q", got)
	}
	if got := effectiveMode(&Props{VariantMode: ModePerVariant}); got != ModePerVariant {
		t.Fatalf("perVariant 应原样保留，实际 %q", got)
	}
}
