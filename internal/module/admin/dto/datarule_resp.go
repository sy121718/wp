package admindto

// RuleItem 数据规则列表项。
type RuleItem struct {
	ID         uint64 `json:"id"`
	RuleName   string `json:"rule_name"`
	Domain     string `json:"domain"`
	Status     int    `json:"status"`
	Remark     string `json:"remark"`
	CreateTime string `json:"create_time"`
	UpdateTime string `json:"update_time"`
}

// RuleListResp 列表查询响应。
type RuleListResp struct {
	Total int64      `json:"total"`
	List  []RuleItem `json:"list"`
}

// RuleDetailResp 数据规则详情。
type RuleDetailResp struct {
	ID         uint64        `json:"id"`
	RuleName   string        `json:"rule_name"`
	Domain     string        `json:"domain"`
	Config     RuleConfigDTO `json:"config"`
	Status     int           `json:"status"`
	Remark     string        `json:"remark"`
	CreateBy   uint64        `json:"create_by"`
	CreateTime string        `json:"create_time"`
	UpdateBy   uint64        `json:"update_by"`
	UpdateTime string        `json:"update_time"`
}

// RuleDomainItem 已注册 domain 简要信息。
type RuleDomainItem struct {
	Domain      string `json:"domain"`
	DomainLabel string `json:"domain_label"`
	TableName   string `json:"table_name"`
}

// RuleDomainDetail domain 详情含字段白名单。
type RuleDomainDetail struct {
	Domain      string         `json:"domain"`
	DomainLabel string         `json:"domain_label"`
	TableName   string         `json:"table_name"`
	Fields      []RuleFieldDef `json:"fields"`
}

// RuleFieldDef 字段定义（数据域白名单项）。
// 不含数据类型：字段类型不是「能不能配」的判据，且它的权威来源是数据库 catalog，
// 抄进声明只会在列型变化时静默漂移（曾经的 data_type 就与真实列型对不上）。
type RuleFieldDef struct {
	Field     string   `json:"field"`
	Label     string   `json:"label"`
	Operators []string `json:"operators"`
}

// RuleAssignmentResp 分配记录。
type RuleAssignmentResp struct {
	ID          uint64 `json:"id"`
	RuleID      uint64 `json:"rule_id"`
	TargetType  int    `json:"target_type"`
	TargetID    uint64 `json:"target_id"`
	TargetScope int    `json:"target_scope"`
	CreateTime  string `json:"create_time"`
}

// RuleAssignmentListResp 分配列表响应。
type RuleAssignmentListResp struct {
	List []RuleAssignmentResp `json:"list"`
}
