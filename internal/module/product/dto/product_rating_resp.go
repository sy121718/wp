package productdto

// product_rating_resp.go — 商品评分响应（issue #30）。

// RatingItem 一条评分明细。
type RatingItem struct {
	ID        string  `json:"id"`
	ProductID string  `json:"productId"`
	Score     float64 `json:"score"`
	Source    string  `json:"source"`
	CreatedAt string  `json:"createdAt"`
}

// RatingResp 评分写入 / 查询的统一返回：明细 + 该商品的**投影值**。
//
// 带上投影值是为了省一次查询，也让调用方立刻看到「这次写入把平均分变成了多少」——
// 评分这类聚合值的写入者最关心的就是写入后的聚合结果。
type RatingResp struct {
	Items []RatingItem `json:"items"`
	// Rating 平均分；RatingCount 条数；HasRating 为 false 表示一条评分都没有。
	// **「没有评分」与「评分 0」是两回事**，所以空态要单独一个布尔量。
	Rating      float64 `json:"rating"`
	RatingCount int     `json:"ratingCount"`
	HasRating   bool    `json:"hasRating"`
}
