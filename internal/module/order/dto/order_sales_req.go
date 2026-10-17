package orderdto

// SalesOverviewReq 销售概览的请求。
//
// 时间用**字符串**而不是 time.Time：同一份结构既服务后台原生表单（form）也要服务 JSON 接口，
// 表单里的日期天生是文本。解析只发生在 service，非法格式在那里统一报错。
//
// From / To 都是 YYYY-MM-DD、**含当天**；两者都必须给 —— 留空则走「静默默认窗口」，
// 那是「我看到的是哪一段」说不清楚的一类数字，宁可让调用方显式传。
type SalesOverviewReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
	From      string `form:"from" json:"from"`
	To        string `form:"to" json:"to"`
	// Status 订单状态筛选（白名单：paid / shipped / completed 的任意组合，逗号分隔）；
	// 空 = 全部计入消费的状态（与「净销售额」同口径的那三个）。
	//
	// 这里**刻意不给 cancelled / refunded**：本页所有数字都是「卖了多少钱」，
	// 把取消单算进来会得到一个没人要的口径（那属于「下了多少单」，订单列表页已经在回答）。
	Status string `form:"status" json:"status"`
	// Monthly 趋势回看的月数（1~24，默认 6）：趋势图看的是「近半年卖得怎么样」，
	// 而卡片看的是筛选区间 —— 两者口径不同才会问出不同的问题，
	// 若趋势也跟着 from/to 走，选一个「今天」就只剩一个点。
	Monthly int `form:"monthly" json:"monthly"`
}
