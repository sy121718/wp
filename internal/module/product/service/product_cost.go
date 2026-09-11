// product_cost.go — 变体成本价写回端口实现（issue #18）。
//
// 依赖方向：inventory → product。库存模块登记入库后（**库存变动已提交**）经本端口
// 把采购单价 / 生产手工成本价写进 product_variants.cost_price。
//
// 商品模块只是这个业务列的**所有者**（表在自己这边，跨模块写只能走自己的入口）：
// 入库登记不参与商品模块的任何状态判断，本方法也只动 cost_price 一列。
package productservice

import (
	"context"
	"errors"
	"strings"
	"time"

	productenums "go_wp/internal/module/product/enums"
)

// UpdateVariantCost 写变体成本价（只动 cost_price 一列）。
func (s *Service) UpdateVariantCost(ctx context.Context, variantID string, cost float64) (err error) {
	if strings.TrimSpace(variantID) == "" {
		return errors.New(productenums.ErrInvalidParam)
	}
	return s.m.UpdateVariantCost(ctx, variantID, cost, time.Now().UTC())
}
