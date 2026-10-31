package builder

import (
	"strings"
	"testing"

	product "go_wp/internal/builder/components/product"
	productcard "go_wp/internal/builder/components/productcard"
	productlist "go_wp/internal/builder/components/productlist"
	"go_wp/internal/builder/core"
)

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
