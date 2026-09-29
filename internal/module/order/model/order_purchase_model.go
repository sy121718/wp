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

// hasPurchasedProductSQL 一条 EXISTS 子查询判定「买过没」。
//
// 为什么是子查询而不是「先取订单 id、再查明细」两段：后者在订单多的客户上会把
// 成百上千个 id 拉进内存再拼 IN 列表，而这里一次往返、由 PG 做 semi-join。
//
// 为什么是 EXISTS 而不是 JOIN：外层只关心「有没有」，EXISTS 命中即短路；
// JOIN 会把命中的行全部展开（同一商品买过多次就有多行）再丢弃。
//
// 工程过滤落在 orders 上（order_items 没有工程列）—— 这不是漏了一层隔离：
// 工程归属由订单决定，明细行随其所属订单进入工程。子查询里的 orders 同样受
// 迁移 215 的 FORCE 策略压制，rls.InProjectScope 设的 app.project_id 对它生效。
//
// 参数顺序：product_id、project_id、user_id、状态名单（CSV）。
const hasPurchasedProductSQL = `SELECT EXISTS (
	SELECT 1
	FROM order_items oi
	WHERE oi.product_id = ?
	  AND oi.order_id IN (
	      SELECT o.id
	      FROM orders o
	      WHERE o.project_id = ?
	        AND o.user_id = ?
	        AND o.status = ANY(string_to_array(?, ',')::text[])
	  )
) AS found`

// HasPurchasedProduct 判定某访客在本工程下是否买过某商品。
//
// 入参形状由 service 入口校验（见 order_purchase.go），这里不重复判：
// 非法 uuid 到 PG 会报类型语法错，那是调用方该在入口挡住的事，不该沉到仓储层。
func (m *OrderModel) HasPurchasedProduct(ctx context.Context, projectID string, userID uint64, productID string) (purchased bool, err error) {
	var row struct {
		Found bool `gorm:"column:found"`
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Raw(hasPurchasedProductSQL,
			productID, projectID, userID, strings.Join(paidStatuses, ",")).
			Scan(&row).Error
	})
	if err != nil {
		return false, err
	}
	return row.Found, nil
}
