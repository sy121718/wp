package builder

// product_field_slots_test.go — 商品域组件的字段槽位接集合元数据下拉（审计 EDT-006）。
//
// 此前这三个组件的槽位是自由文本：作者要手写 product.priceRange 这样的路径，
// 写错（prodct.priceRange / product.price_range）要等到保存或构建才报错。
// 现在槽位声明为 bindingfield，选项来自与 ValidateFieldRefs 同一份字段白名单。
//
// 本用例钉住的是**前缀白名单**：详情页组件只该列 product.*，卡片与列表两种作用域都列。
// 前缀给错不会报错，只会让编辑器里出现用不了的选项（选了之后构建期才拒绝），
// 所以必须由测试守住，而不是靠人记得。

import (
	"strings"
	"testing"

	product "go_wp/internal/builder/components/product"
	productcard "go_wp/internal/builder/components/productcard"
	productlist "go_wp/internal/builder/components/productlist"
	"go_wp/internal/builder/core"
)

// slotControls 取出组件中所有字段槽位控件（key 以 Field 结尾的）。
func slotControls(t *testing.T, spec any) map[string]core.Control {
	t.Helper()
	controls, err := core.ParseControls(spec)
	if err != nil {
		t.Fatalf("解析控件 schema 失败: %v", err)
	}
	out := map[string]core.Control{}
	for _, c := range controls {
		if strings.HasSuffix(c.Key, "Field") {
			out[c.Key] = c
		}
	}
	if len(out) == 0 {
		t.Fatalf("未找到任何字段槽位控件（key 以 Field 结尾）")
	}
	return out
}

// TestProductComponentSlotsAreProductPrefixed core.product 的槽位只接受 product.* 前缀。
func TestProductComponentSlotsAreProductPrefixed(t *testing.T) {
	slots := slotControls(t, (&product.Component{}).PropsSpec())
	for key, c := range slots {
		if c.Kind != core.ControlBindingField {
			t.Fatalf("槽位 %s 应使用 bindingfield 控件，实际 %s", key, c.Kind)
		}
		if len(c.Prefixes) != 1 || c.Prefixes[0] != "product" {
			t.Fatalf("槽位 %s 的 prefixes 应为 [product]（详情页绑当前实体，item.* 在此无含义），实际 %v", key, c.Prefixes)
		}
	}
}

// TestProductCardAndListSlotsAcceptBothScopes 卡片与列表同时服务两个作用域。
//
// 卡片在集合里展开时绑 item.*（那一行的数据），在详情页当推荐位时绑 product.*（当前实体），
// 因此两种前缀都要列 —— 只列一种会让另一半场景在编辑器里配不出来。
func TestProductCardAndListSlotsAcceptBothScopes(t *testing.T) {
	for name, spec := range map[string]any{
		"productCard": (&productcard.Component{}).PropsSpec(),
		"productList": (&productlist.Component{}).PropsSpec(),
	} {
		t.Run(name, func(t *testing.T) {
			for key, c := range slotControls(t, spec) {
				if c.Kind != core.ControlBindingField {
					t.Fatalf("槽位 %s 应使用 bindingfield 控件，实际 %s", key, c.Kind)
				}
				if len(c.Prefixes) != 2 || c.Prefixes[0] != "item" || c.Prefixes[1] != "product" {
					t.Fatalf("槽位 %s 的 prefixes 应为 [item product]，实际 %v", key, c.Prefixes)
				}
			}
		})
	}
}

// TestPrefixesOnlyOnFieldBindingControls prefixes 用在别的控件上必须报错。
//
// 静默忽略的后果是「配了但没生效」——正是 prefixes 要消除的那类问题。
func TestPrefixesOnlyOnFieldBindingControls(t *testing.T) {
	type badProps struct {
		Broken string `json:"broken" ct:"string,prefixes=item,label=文案"`
	}
	if _, err := core.ParseControls(&badProps{}); err == nil {
		t.Fatalf("prefixes 用在 string 控件上应报错")
	} else if !strings.Contains(err.Error(), "prefixes") {
		t.Fatalf("错误信息应指出 prefixes，实际: %v", err)
	}
}
