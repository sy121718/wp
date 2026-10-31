package orderdto

type GetRateReq struct {
	BaseCode  string `form:"baseCode" json:"baseCode"`
	QuoteCode string `form:"quoteCode" json:"quoteCode"`
}

type SetRateReq struct {
	BaseCode  string `form:"baseCode" json:"baseCode" binding:"required"`
	QuoteCode string `form:"quoteCode" json:"quoteCode" binding:"required"`

	Rate string `form:"rate" json:"rate" binding:"required"`
}

type RateResp struct {
	BaseCode  string `json:"baseCode"`
	QuoteCode string `json:"quoteCode"`
	Rate      string `json:"rate"`
	RateAt    string `json:"rateAt"`
	Source    string `json:"source"`
}

type ConvertedOverview struct {
	BaseCurrency string `json:"baseCurrency"`

	ConvertedSales  int64 `json:"convertedSales"`
	ConvertedProfit int64 `json:"convertedProfit"`

	ConvertedOrders int64 `json:"convertedOrders"`

	UnconvertedOrders int64 `json:"unconvertedOrders"`
}
