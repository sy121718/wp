package orderdto

type CustomerGrowthReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
	From      string `form:"from" json:"from"`
	To        string `form:"to" json:"to"`

	// Statuses 为空时按「已成交」默认口径统计；销售概览页显式筛了状态时
	// 传同一份，保证同屏的销售额与新客数回答同一批行。
	Statuses []string `form:"-" json:"-"`
}
