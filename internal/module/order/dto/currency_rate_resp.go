package orderdto

// currency_rate.go — 汇率的入参出参（迁移 630，W5）。
//
// Rate 是**文本形式的 decimal**（numeric(18,8) 的同形）：读成 float64 会丢精度，
// 而汇率是财务口径 —— 1e-8 的误差在汇总时会变成可观的金额偏差。换算在 service
// 用 big.Rat 精确算，客户端拿到的也是精确文本。

// GetRateReq 取某币对的当前汇率。
type GetRateReq struct {
	BaseCode  string `form:"baseCode" json:"baseCode"`
	QuoteCode string `form:"quoteCode" json:"quoteCode"`
}

// SetRateReq 覆盖式写汇率。
type SetRateReq struct {
	BaseCode  string `form:"baseCode" json:"baseCode" binding:"required"`
	QuoteCode string `form:"quoteCode" json:"quoteCode" binding:"required"`
	// Rate 正数（可以是 "7.24" / "0.13800000"）—— service 解析成有理数并校验 > 0。
	Rate string `form:"rate" json:"rate" binding:"required"`
}

// RateResp 汇率回执。Rate 为空串表示**未固化**（该币对还没配汇率）。
type RateResp struct {
	BaseCode  string `json:"baseCode"`
	QuoteCode string `json:"quoteCode"`
	Rate      string `json:"rate"`
	RateAt    string `json:"rateAt"`
	Source    string `json:"source"`
}

// ConvertedOverview 折算后的销售概览（迁移 630，W5）。
//
// 「未固化」的订单单独计数（UnconvertedOrders）：折算金额只覆盖有快照的那部分，
// 页面必须能说清「这里面有多少单没折算」—— 否则一个偏小的数字会被当成全量。
type ConvertedOverview struct {
	BaseCurrency string `json:"baseCurrency"`
	// ConvertedSales / ConvertedProfit 折算后的销售额与毛利（**分**，基准币）。
	ConvertedSales  int64 `json:"convertedSales"`
	ConvertedProfit int64 `json:"convertedProfit"`
	// ConvertedOrders 有汇率快照、已折算的订单数。
	ConvertedOrders int64 `json:"convertedOrders"`
	// UnconvertedOrders 没有汇率快照的订单数（汇率未固化）。
	UnconvertedOrders int64 `json:"unconvertedOrders"`
}
