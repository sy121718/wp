package model

// order_purchase_model.go — 「某访客是否买过某商品」（商品评论差异化规则的只读事实查询）。
//
// 为什么这条归订单模块：**「买过」的口径由订单模块独占解释** —— 哪些状态算钱进来了
// 由 paidStatuses 决定（名单只声明一次，见 order_model.go）。商品侧读不到 orders /
// order_items（表隔离），所以这个问题只能由订单侧回答。
//
// 形态与 order_spent_model.go 的 SpentTotalsByProject 同源：同一套 rls.InProjectScope
// 工程作用域 + 同一份 paidStatuses 状态名单。两边各写一份状态名单的失败模式是
//「某次顺手加了个状态之后，累计消费变了、买过判定没变」，都不报错、只在对账时才发现。

import (
	"context"
	"strings"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

// HasPurchasedProduct 判定某访客在本工程下是否买过某商品。
//
// 三段式（子查询 + IN + Limit 1）而不是一次 EXISTS：
//
//   - **子查询而不是两段查询**：先取订单 id 再查明细，会在订单多的客户上把成百上千个 id
//     拉进内存再拼 IN 列表；这里一次往返、由 PG 做 semi-join。
//   - **Limit(1) 而不是 Count**：外层只关心「有没有」，数出全部命中行是白干
//     （同一商品买过多次就有多行）。
//   - **工程过滤落在 orders 上**（order_items 没有工程列）—— 这不是漏了一层隔离：
//     工程归属由订单决定，明细行随其所属订单进入工程。子查询里的 orders 同样受
//     迁移 215 的 FORCE 策略压制，rls.InProjectScope 设的 app.project_id 对它生效。
//
// 入参形状由 service 入口校验（见 order_purchase.go），这里不重复判：
// 非法 uuid 到 PG 会报类型语法错，那是调用方该在入口挡住的事，不该沉到仓储层。
func (m *OrderModel) HasPurchasedProduct(ctx context.Context, projectID string, userID uint64, productID string) (purchased bool, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		// 状态名单与累计消费同源（paidStatuses，见 order_model.go）：
		// 两边各写一份的失败模式是「某次顺手加了个状态之后，累计消费变了、买过判定没变」，
		// 都不报错、只在对账时才发现。
		orderIDs := tx.Model(&OrderEntity{}).
			Select("id").
			Where("project_id = ?", projectID).
			Where("user_id = ?", userID).
			Where("status = ANY(string_to_array(?, ',')::text[])", strings.Join(paidStatuses, ","))

		var hit []string
		if err := tx.Model(&OrderItemEntity{}).
			Select("id").
			Where("product_id = ?", productID).
			Where("order_id IN (?)", orderIDs).
			Limit(1).
			Find(&hit).Error; err != nil {
			return err
		}
		purchased = len(hit) > 0
		return nil
	})
	if err != nil {
		return false, err
	}
	return purchased, nil
}
