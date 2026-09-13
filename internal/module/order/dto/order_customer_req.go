package orderdto

// order_customer_req.go — 后台「按客户看订单」的只读请求（客户管理页用）。

// CustomerOrderSummaryReq 按「工程 + 客户」取订单聚合事实。
//
// 两个参数都必填：订单是工程维度的（不同工程的单互不相干，混在一起算出来的
// 「累计消费」没有对应的账），UserID=0 也一律拒绝 —— 那意味着「没有客户」，
// 而不是「所有客户」；后者会让这条只读方法变成拉全站汇总的口子。
type CustomerOrderSummaryReq struct {
	ProjectID string
	UserID    uint64
}
