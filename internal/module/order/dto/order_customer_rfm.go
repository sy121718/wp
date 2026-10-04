package orderdto

// order_customer_rfm.go — 客户 RFM 分层（分析页 + 客户列表按分段筛选）。
//
// 口径见 model/order_customer_rfm_model.go 的文件头；这里只放对外形状。

// CustomerRfmReq 按「工程 + 区间（+ 可选分段）」取 RFM 明细。
//
// 时间字段约定与 CustomerGrowthReq 一致（YYYY-MM-DD、含当天、两者必填、解析只在 service）。
type CustomerRfmReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
	From      string `form:"from" json:"from"`
	To        string `form:"to" json:"to"`
	// Segment 只看某一段（"vip" / "potential" / "low_value"），空为全部。
	Segment string `form:"segment" json:"segment"`
	Limit   int    `form:"limit" json:"limit"`
	Offset  int    `form:"offset" json:"offset"`
}

// CustomerRfmItem 一个客户的 RFM 明细。
type CustomerRfmItem struct {
	UserID int64 `json:"userId"`
	// LastOrderAt 该客户的**全历史**最后下单日（R 的依据，与窗口无关）。
	LastOrderAt string `json:"lastOrderAt"`
	// RecencyDays 距窗口结束日的天数（越小越近）。
	RecencyDays int `json:"recencyDays"`
	// Frequency 窗口内的订单数。
	Frequency int64 `json:"frequency"`
	// MonetaryCents 窗口内的净消费额（分）。
	MonetaryCents int64 `json:"monetaryCents"`
	// MonetaryLabel 展示串（与其它页面同一套金额格式化）。
	MonetaryLabel string `json:"monetaryLabel"`
	// RScore / FScore / MScore 三个维度的五分位得分（1-5）。
	//
	// 它们是**相对的**：同一个客户在不同区间里得分可能不同 ——
	// 分位是按当次查询的那批人算的。展示时必须带上「分位」两个字，
	// 否则会被读成绝对等级。
	RScore     int `json:"rScore"`
	FScore     int `json:"fScore"`
	MScore     int `json:"mScore"`
	TotalScore int `json:"totalScore"`
	// Segment vip / potential / low_value（与 CRM 的 classifySegment 同阈值）。
	Segment string `json:"segment"`
	// SegmentLabel 展示用分段名（"高价值" / "潜力" / "一般"）。
	SegmentLabel string `json:"segmentLabel"`
}

// CustomerRfmResp RFM 报表：分段计数 + 一页明细。
type CustomerRfmResp struct {
	ProjectID string `json:"projectId"`
	From      string `json:"from"`
	To        string `json:"to"`
	// Customers 参与分层的客户数（窗口内下过单的人）。
	Customers int64 `json:"customers"`
	Vip       int64 `json:"vip"`
	Potential int64 `json:"potential"`
	LowValue  int64 `json:"lowValue"`
	// Total 当前分段过滤下的条数（未过滤时等于 Customers）。
	Total int64             `json:"total"`
	Items []CustomerRfmItem `json:"items"`
}
