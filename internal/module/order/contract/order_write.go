package ordercontract

import (
	"context"

	orderdto "go_wp/internal/module/order/dto"
)

// order_write.go — 订单模块的**写入**契约（AI 工具用）。
//
// 只开一条：后台代客建单。判据是「这一条与后台手工建单页是同一件事」——
// CreateAdminOrder 的来源标记固定为 admin，与运营在页面上点「新建订单」落下的
// created_via 一致，所以从流水看不出这单是 AI 建的还是人建的（两者都要留痕的是
// 「后台代建」这件事本身，不是「谁点的鼠标」）。
//
// 刻意**不含**的三组方法，以及它们各自的原因：
//
//	· ChangeStatus / Ship —— 发货会触发库存扣减与通知邮件，是**对已存在订单的
//	  不可逆推进**；用户问「帮我把这单发货」时，正确回答是告诉他去订单页操作，
//	  而不是让模型判断这一单该不该发。
//	· Refund / 退款审批 —— 涉及资金流出，属于必须有第二人复核的动作。
//	· UpdateOrder / DeleteOrder —— 订单是交易凭证，改与删都不该由模型发起。
type OrderWriter interface {
	// CreateAdminOrder 后台代客建单（来源标记 admin）。
	//
	// 总价由服务端算：Subtotal / Total 不接受传入，DiscountTotal 在无券时被忽略，
	// 有券时以服务端试算为准。所以工具层**无法**定价 —— 这是刻意的，
	// 「客户端能定价的接口等于把收银台交给客人自己看」。
	CreateAdminOrder(ctx context.Context, req *orderdto.CreateOrderReq) (res *orderdto.CreateOrderResp, err error)
}
