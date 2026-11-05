package addtocart

import (
	"errors"
	"fmt"
	"go_wp/pkg/builder/core"
	"strings"
	"testing"
)

type stubContent map[string]string

func (s stubContent) ResolveString(field string) (string, error) {
	if v, ok := s[field]; ok {
		return v, nil
	}
	return "", fmt.Errorf("未知字段 %q", field)
}

const (
	optionsJSON  = "[{\"key\":\"color\",\"name\":\"颜色\",\"values\":[{\"key\":\"red\",\"label\":\"红色\"},{\"key\":\"blue\",\"label\":\"蓝色\"}]}]"
	variantsJSON = "[" +
		"{\"id\":\"v-red\",\"sku\":\"SKU-RED\",\"price\":\"99.00\",\"comparePrice\":\"129.00\",\"enabled\":true,\"options\":{\"color\":\"red\"}}," +
		"{\"id\":\"v-blue\",\"sku\":\"SKU-BLUE\",\"price\":\"89.00\",\"enabled\":true,\"options\":{\"color\":\"blue\"}}," +
		"{\"id\":\"v-off\",\"sku\":\"SKU-OFF\",\"price\":\"79.00\",\"enabled\":false,\"options\":{\"color\":\"blue\"}}" +
		"]"
)

func baseProps() *Props { return &Props{} }
func baseContent() stubContent {
	return stubContent{"product.options": optionsJSON, "product.variants": variantsJSON}
}

func TestBuildViewBuyerChoosesOnlyCurrentProductVariants(t *testing.T) {
	v, err := BuildView(&Props{ShowQuantity: true}, baseContent(), "proj-1", "zh")
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Rows) != 2 || !v.ChooseVariant || !v.ShowQuantity {
		t.Fatalf("多规格应由买家在当前表单选择: %+v", v)
	}
	if v.Rows[0].VariantID != "v-red" || v.Rows[1].VariantID != "v-blue" || !strings.Contains(v.Rows[1].Label, "蓝色") {
		t.Fatalf("规格只应来自当前商品的启用项: %+v", v.Rows)
	}
	if v.Action != CartAddPath || v.ProjectID != "proj-1" {
		t.Fatalf("加购动作固定: %+v", v)
	}
}
func TestBuildViewOneVariantIsAutomatic(t *testing.T) {
	v, err := BuildView(baseProps(), stubContent{"product.options": "[]", "product.variants": `[{"id":"only","enabled":true,"sku":"DEFAULT"}]`}, "proj-1", "")
	if err != nil || v.ChooseVariant || len(v.Rows) != 1 || v.Rows[0].VariantID != "only" {
		t.Fatalf("唯一规格应自动使用: %+v %v", v, err)
	}
}
func TestLegacyProductWiringRequiresExplicitConversion(t *testing.T) {
	for _, raw := range []string{`{"productSource":"specific","productId":"old"}`, `{"variantId":"old"}`, `{"currency":"¥"}`, `{"variantMode":"perVariant"}`, `{"source":"product","optionsField":"product.options"}`} {
		if _, err := core.DecodeProps[Props]([]byte(raw)); err == nil || !strings.Contains(err.Error(), "改为当前商品加购") {
			t.Fatalf("旧接线不能静默继续执行: %s %v", raw, err)
		}
	}
}

func TestBuildViewEmptyVariantsIsUnavailable(t *testing.T) {
	for _, raw := range []string{"", "[]", `[{"id":"off","enabled":false}]`} {
		content := stubContent{"product.options": optionsJSON, "product.variants": raw}
		view, err := BuildView(baseProps(), content, "proj-1", "")
		if err != nil || len(view.Rows) != 0 || view.UnavailableText == "" {
			t.Fatalf("无可购买规格应可编译禁用态: %+v %v", view, err)
		}
		attrs, _ := view.DeclareFeatures()
		if len(attrs) != 0 {
			t.Fatalf("禁用购买不能启用私有读写增强: %v", attrs)
		}
		view.ApplyI18n(func(key, fallback string) string {
			if key == TextKeyUnavailable {
				return "This product is currently unavailable."
			}
			return fallback
		})
		if view.UnavailableText != "This product is currently unavailable." {
			t.Fatal("禁用提示必须跟随编译语言")
		}
	}
}

func TestBuildViewWithoutProjectIsIncomplete(t *testing.T) {
	if _, err := BuildView(baseProps(), baseContent(), "", ""); !errors.Is(err, core.ErrIncompleteNode) {
		t.Fatalf("缺工程应局部占位并拒绝发布: %v", err)
	}
}

func TestBuildViewRequiresContentResolver(t *testing.T) {
	if _, err := BuildView(baseProps(), nil, "proj-1", ""); err == nil {
		t.Fatal("缺内容解析器时应构建失败")
	}
}

func TestBuildViewPropagatesResolveError(t *testing.T) {
	content := stubContent{"product.options": optionsJSON}
	if _, err := BuildView(baseProps(), content, "proj-1", ""); err == nil {
		t.Fatal("变体字段解析失败时应上抛错误")
	}
}

func TestApplyI18n(t *testing.T) {

	v := &View{Label: textFallbackButton}
	asked := map[string]string{}
	v.ApplyI18n(func(key, fallback string) string {
		asked[key] = fallback
		switch key {
		case TextKeyButton:
			return "Add to cart"
		case TextKeyQtyAria:
			return "Quantity"
		case TextKeyVariantAria:
			return "Choose variant"
		}
		t.Errorf("多取了文案键: %q", key)
		return fallback
	})
	if v.Label != "Add to cart" {
		t.Fatalf("默认按钮文字应取译文，实际 %q", v.Label)
	}
	if v.QtyAria != "Quantity" {
		t.Fatalf("数量框无障碍名应取译文，实际 %q（英文站点上读屏器会念中文）", v.QtyAria)
	}
	if len(asked) != 3 {
		t.Fatalf("正常渲染只该取按钮、数量与规格三条文案，实际取了 %d 条: %v", len(asked), asked)
	}

	if asked[TextKeyQtyAria] != textFallbackQtyAria {
		t.Fatalf("数量框文案的兜底值不符，实际 %q", asked[TextKeyQtyAria])
	}

	v2 := &View{Label: "立即抢购"}
	v2.ApplyI18n(func(key, fallback string) string {
		if key == TextKeyButton {
			t.Error("作者自定义的按钮文字不该走界面翻译（它由内容翻译链路负责）")
		}
		if key == TextKeyQtyAria {
			return "Quantity"
		}
		return fallback
	})
	if v2.Label != "立即抢购" {
		t.Fatalf("作者自定义的文案不该被界面翻译覆盖，实际 %q", v2.Label)
	}
	if v2.QtyAria != "Quantity" {
		t.Fatalf("自定义按钮文字不该带走数量框的取词（两件事各自独立），实际 %q", v2.QtyAria)
	}

	v3 := &View{Label: textFallbackButton}
	v3.ApplyI18n(nil)
	if v3.Label != textFallbackButton {
		t.Fatalf("无翻译函数时应保持兜底文案，实际 %q", v3.Label)
	}
	if v3.QtyAria != textFallbackQtyAria {
		t.Fatalf("无翻译函数时数量框应保持兜底文案，实际 %q", v3.QtyAria)
	}

	var v4 *View
	v4.ApplyI18n(nil)
}

func TestEffectiveButtonText(t *testing.T) {
	if got := effectiveLabel(&Props{}); got != defaultLabel {
		t.Fatalf("空按钮文字应回退默认值，实际 %q", got)
	}
	if got := effectiveLabel(&Props{Label: "  "}); got != defaultLabel {
		t.Fatalf("纯空白应回退默认值，实际 %q", got)
	}
	if got := effectiveLabel(&Props{Label: "立刻购买"}); got != "立刻购买" {
		t.Fatalf("自定义文字应原样使用，实际 %q", got)
	}
}
