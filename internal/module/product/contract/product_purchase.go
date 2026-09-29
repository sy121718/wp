// product_purchase.go — 访客购买事实的只读端口（「买过才能评」这条评论规则的输入）。
//
// 为什么端口定义在 product 而不是 order：需要这个答案的是**商品侧的产品规则**
// （商品评论必须买过）。答案是订单域的事实，但问题的提出方在商品侧 ——
// 同型先例 membershipcontract.PurchaseSource（消费方定义接口、订单侧实现）。
//
// 收窄到一条是非题：product 拿不到订单的读写能力，只能问「有没有这回事」。
// 越权防护靠接口形状，不靠调用方自觉（同 VariantAvailabilityPort / DependencyInvalidator）。
package productcontract

import "context"

// PurchaseChecker 访客购买事实的只读端口（**订单侧实现**；装配期注入）。
//
// 未注入时的行为由使用方决定：评论规则的实现方（product 的 commentcontract.EntityPolicy）
// 在未注入时**放行** —— 判据见 commentcontract.EntityPolicy 的注释（它是产品策略，
// 不是安全边界；未注入即拒绝会把「装配漏了一行」表现成「商品评论全发不出去」）。
type PurchaseChecker interface {
	// HasPurchasedProduct 某访客在本工程下是否买过某商品。
	//
	// 「什么算买过」（哪些订单状态计入）由**订单侧**定义并负责 —— 它才是那个
	// 知道钱有没有进来的领域（与 PurchaseSource.SpentTotalsByUser 同源：
	// 计入消费的状态名单只声明一次）。
	//
	// 返回值语义要分清：
	//   - (false, nil) 是**正常结论**（确实没买过），不是错误；
	//   - (false, err) 是判定失败（查询故障），调用方应记日志归口，
	//     不要把它当成「没买过」—— 一次数据库抖动会让所有评论被拒，
	//     而用户看到的是一句无从解释的拒绝。
	HasPurchasedProduct(ctx context.Context, projectID string, userID uint64, productID string) (purchased bool, err error)
}
