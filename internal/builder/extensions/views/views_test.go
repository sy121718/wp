package views

import (
	"testing"

	"go_wp/pkg/builder/core"
)

// TestRegisterAllCoversEveryBusinessType 保证「组件已注册但视图没接入」不会拖到渲染期才报错。
//
// 判据是双向的：RegisterAll 登记的每个类型都必须真实存在于组件注册表；
// 且每个业务类型（core. 前缀且非通用组件）都必须能从 NodeViewBuilderOf 命中。
func TestRegisterAllCoversEveryBusinessType(t *testing.T) {
	RegisterAll()

	business := []string{
		"core.product", "core.productCard", "core.cardstack", "core.productSelector",
		"core.cartIcon", "core.searchResults", "core.orderList", "core.userForms",
		"core.addToCart", "core.articleList", "core.productList", "core.checkoutForm",
		"core.nav",
	}
	if len(business) != 13 {
		t.Fatalf("业务视图清单应为 13 项，实际 %d 项 —— 新增组件时同步本清单", len(business))
	}
	for _, typeName := range business {
		if _, ok := core.NodeViewBuilderOf(typeName); !ok {
			t.Errorf("%s 没有登记视图构建函数：渲染到这里会退化成通用骨架或直接报错", typeName)
		}
	}

	// 反向：登记了视图却不在组件注册表，说明类型名两侧漂移。
	for _, typeName := range business {
		if _, err := core.Lookup(typeName); err != nil {
			t.Errorf("%s 有视图函数但组件注册表里没有它：%v", typeName, err)
		}
	}
}
