package productdto

// product_publish_record_req.go — 发布记录页的取数入参（B9）。
//
// 只读一屏：没有写字段。筛选三件（关键词 / 状态 / 页码）与商品列表页同口径 ——
// 同一个后台里两个列表的筛选语义不一致是最容易被当成 bug 的一种漂移。

// ProductPublishRecordPageReq 发布记录页的查询串。
type ProductPublishRecordPageReq struct {
	// ProjectID 工程 id；空串 = 取工程清单的第一个（与商品列表页一致）。
	ProjectID string `form:"project"`
	// Keyword 按商品名 / SKU 模糊筛（复用商品列表的关键词口径）。
	Keyword string `form:"keyword"`
	// Status 发布状态筛选：空 / all = 全部；active = 已上线；draft = 有实例未上线；none = 无实例。
	Status string `form:"status"`
	Page   int    `form:"page"`
}
