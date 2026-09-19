package productservice

// product_type.go — 商品类型（迁移 238）：variant（常规变体商品）/ bundle（捆绑容器）。
//
// 为什么要把「有没有自己的 SKU / 价格放在哪」显式成一个类型：
// 捆绑品此前只是「bundle_items 非空的商品」，系统分不清主体卖自己的 SKU 与容器卖组合。
// 后果是创建路径无差别地给捆绑品生成了一个价 0 的首个变体，而价格区间只从变体派生 ——
// 捆绑商品在列表里显示 0.00（运营看到的现象）。
//
// 定了类型之后的三条不变量：
//   · bundle 不生成首个变体（容器没有自己的 SKU），库存由成员变体按 BOM 扣减；
//   · bundle 只有一个对外价格 = 容器价（products.default_price），创建时必须 > 0；
//   · 成员价不参与任何对外金额与展示（成本合计保留在后台口径）。

import (
	"errors"
	"strings"

	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
)

// normalizeProductType 归一化商品类型：空默认 variant，其余仅接受 variant / bundle。
func normalizeProductType(raw string) (string, error) {
	switch strings.TrimSpace(raw) {
	case "", productmodel.TypeVariant:
		return productmodel.TypeVariant, nil
	case productmodel.TypeBundle:
		return productmodel.TypeBundle, nil
	default:
		return "", errors.New(productenums.ErrProductTypeInvalid)
	}
}
