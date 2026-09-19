// product_cost.go — 变体成本价写回端口实现（issue #18；留痕见 issue #19）。
//
// 依赖方向：inventory → product。库存模块登记入库后（**库存变动已提交**）经本端口
// 把采购单价 / 生产手工成本价写进 product_variants.cost_price。
//
// 商品模块只是这个业务列的**所有者**（表在自己这边，跨模块写只能走自己的入口）：
// 入库登记不参与商品模块的任何状态判断，本方法也只动 cost_price 一列。
//
// issue #19 起这次回写也进主数据变更记录：成本价是定价工具的成本口径，属于主数据。
// 留痕的改前 / 改后快照只差 cost_price 一列，故不需要二次读库；
// operatorID 由调用方（入库登记）从会话带下来 —— 记的是「谁登记了这次入库」。
package productservice

import (
	"context"
	"errors"
	"strings"
	"time"

	masterdatacontract "go_wp/internal/module/masterdata/contract"
	masterdataenums "go_wp/internal/module/masterdata/enums"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
	"go_wp/pkg/rls"

	"gorm.io/gorm"
)

// UpdateVariantCost 写变体成本价（只动 cost_price 一列）并留痕。
func (s *Service) UpdateVariantCost(ctx context.Context, projectID, variantID string, cost float64, operatorID string) (err error) {
	if strings.TrimSpace(variantID) == "" {
		return errors.New(productenums.ErrInvalidParam)
	}
	// 留痕端口未注入（纯商品单测路径）时不读库、不写记录，只做成本价回写本身。
	var (
		v        *productmodel.VariantEntity
		resolved string
	)
	if s.changes != nil {
		if v, err = s.m.GetVariant(ctx, variantID); err != nil {
			return mapNotFound(err)
		}
		// 作用域用调用方（入库登记）给的工程：按变体反查所属商品要走 products 的 RLS。
		if resolved, err = s.variantProjectID(ctx, v, projectID); err != nil {
			return err
		}
	}
	now := time.Now().UTC()
	if s.changes == nil || v == nil {
		// 留痕端口未注入（纯商品单测路径）：只做成本价回写本身。
		return s.m.UpdateVariantCost(ctx, variantID, cost, now)
	}
	before := variantChangeSnapshot(v, nil)
	after := variantChangeSnapshot(v, nil)
	after["cost_price"] = masterdatacontract.FormatPrice(cost)
	// 成本价回写与它的变更记录**同事务**（AGENTS.md「写操作的事务与回滚」）：
	// 成本价是定价工具的成本口径，改了却没留下这一笔就是错账，只能靠对账发现。
	// 留痕端口未注入时 recordChangesTx 空转（与原先的 recordChanges 一致）。
	return s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, resolved); serr != nil {
			return serr
		}
		if uerr := s.m.UpdateVariantCostTx(ctx, tx, variantID, cost, now); uerr != nil {
			return uerr
		}
		return s.recordChangesTx(ctx, tx, variantChangeInput(resolved, v, masterdataenums.ActionUpdate,
			masterdataenums.OriginReceipt, operatorID, before, after))
	})
}
