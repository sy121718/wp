package productdto

// product_rating_req.go — 商品评分的写入与查询请求（issue #30）。
//
// 评分独立成表后，维护入口也从「商品更新接口的一个字段」变成独立用例：
// 商品更新只管商品自己的列，评分由这里增删 —— 两件事的写入者本来就不是同一类操作。

// AddRatingReq 写入一条商品评分。
type AddRatingReq struct {
	ProductID string `json:"productId" binding:"required"`
	// Score 评分 0~5（含端）；越界即拒绝（数据库还有 CHECK 兜底）。
	Score float64 `json:"score" binding:"required"`
	// Source 来源：空 = manual（运营补录）；将来评论域写 review。
	Source string `json:"source"`
	// OperatorID 操作人：由 inbound 从会话覆盖写入，客户端不可指定。
	OperatorID string `json:"-" form:"-"`
}

// ListRatingsReq 取某商品的评分明细。
type ListRatingsReq struct {
	ProductID string `json:"productId" binding:"required"`
}

// DeleteRatingReq 删除一条评分。
type DeleteRatingReq struct {
	ID string `json:"id" binding:"required"`
}
