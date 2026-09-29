package orderservice

// order_purchase.go — 「某访客是否买过某商品」的对外出口（商品评论差异化规则用）。
//
// 端口由**消费方**（product 模块）声明（productcontract.PurchaseChecker），
// 实现在这里 —— 同型先例是 membershipcontract.PurchaseSource 的接入方式。

import (
	"context"
	"strings"

	"github.com/google/uuid"

	productcontract "go_wp/internal/module/product/contract"
)

// 编译期断言：本实现满足商品侧声明的购买事实端口。
var _ productcontract.PurchaseChecker = (*Service)(nil)

// HasPurchasedProduct 判定某访客在本工程下是否买过某商品（只读）。
//
// 两条入口校验（不信任调用方）：
//   - userID 为 0 = 没有账号 → 直接 false 且不查库。0 不是有效账号 id（users.id 从 1 起）；
//     「没账号的人买过东西」这个命题本身不成立 —— 访客下单会经 guest 端口开号。
//   - productID 必须是合法 uuid：order_items.product_id 是 uuid 列，非法值会让 PG 报
//     "invalid input syntax for type uuid"，而那条错误在调用方看来像数据库故障、不像参数错误。
//     这里返回 false 而不是 error：不存在这样的商品 id，「买过」就不可能成立 ——
//     把它当故障上报会让一次入参问题变成一条错误日志 + 一句归口文案。
//
// projectID 不在这里判形状：空 / 非法工程在本仓是「查不到行」，返回 false 是正确结论
// （同 SpentTotalsByProject 的判据）。
func (s *Service) HasPurchasedProduct(ctx context.Context, projectID string, userID uint64, productID string) (purchased bool, err error) {
	productID = strings.TrimSpace(productID)
	if userID == 0 || productID == "" {
		return false, nil
	}
	if _, perr := uuid.Parse(productID); perr != nil {
		return false, nil
	}
	return s.orders.HasPurchasedProduct(ctx, strings.TrimSpace(projectID), userID, productID)
}
