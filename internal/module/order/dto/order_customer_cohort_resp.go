package orderdto

// CustomerCohortCell 矩阵里的一格。
//
// 空的格**不出现在 Cells 里**（而不是给一个 0）：第 3 个月还没到与第 3 个月没人来
// 是两件完全不同的事，前者显示成 0 会让运营以为「三个月后客户全跑了」。
type CustomerCohortCell struct {
	// MonthIndex 相对月序号（0 = 该群的首单当月）。
	MonthIndex int `json:"monthIndex"`
	// MonthLabel 该格的年月（"2026年10月"）。
	MonthLabel string `json:"monthLabel"`
	// ActiveCustomers 该月下过消费单的人数。
	ActiveCustomers int64 `json:"activeCustomers"`
	// RetentionRatePct 占本群人数的百分比（保留一位小数）；本群人数为 0 时为 0。
	RetentionRatePct float64 `json:"retentionRatePct"`
	// RetentionLabel 展示用串（"42.9%"）：百分比格式化在 service 发生一次。
	RetentionLabel string `json:"retentionLabel"`
	// Reached 该月是否已经过去（false = 还没到，页面上显示为空而不是 0%）。
	//
	// 「还没到这个月」与「这个月没人来」必须能分开：混在一起时最新几个月的留存率
	// 全是 0%，读起来像断崖式流失，而真实原因是时间还没到。
	Reached bool `json:"reached"`
}

// CustomerCohortRow 矩阵的一行（一个群）。
type CustomerCohortRow struct {
	// CohortMonth 首单所在月（YYYY-MM）。
	CohortMonth string `json:"cohortMonth"`
	// CohortLabel 展示用月份（"2026年10月"）。
	CohortLabel string `json:"cohortLabel"`
	// CohortSize 该群人数（分母）。
	CohortSize int64 `json:"cohortSize"`
	// Cells 该群的各月留存（稀疏；未到达的月份缺席）。
	Cells []CustomerCohortCell `json:"cells"`
}

// CustomerCohortResp 群组留存矩阵。
//
// Customers 是所有群的人数之和，且**必须等于客户概览的「新客」数**（同一个分群判据，
// 测试钉住）—— 这两个数字出现在不同页面，只有在口径真的同源时才会一直相等。
type CustomerCohortResp struct {
	ProjectID string `json:"projectId"`
	From      string `json:"from"`
	To        string `json:"to"`
	// Months 实际展示的列数（＝最长的那一行）。
	Months int `json:"months"`
	// Cohorts 群数。
	Cohorts int64 `json:"cohorts"`
	// Customers 各群人数之和。
	Customers int64 `json:"customers"`
	// Rows 按群月份升序。
	Rows []CustomerCohortRow `json:"rows"`
}
