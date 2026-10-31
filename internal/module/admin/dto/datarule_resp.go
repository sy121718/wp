package admindto

type RuleItem struct {
	ID         uint64 `json:"id"`
	RuleName   string `json:"rule_name"`
	Domain     string `json:"domain"`
	Status     int    `json:"status"`
	Remark     string `json:"remark"`
	CreateTime string `json:"create_time"`
	UpdateTime string `json:"update_time"`
}

type RuleListResp struct {
	Total int64      `json:"total"`
	List  []RuleItem `json:"list"`
}

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

type RuleDomainItem struct {
	Domain      string `json:"domain"`
	DomainLabel string `json:"domain_label"`
	TableName   string `json:"table_name"`
}

type RuleDomainDetail struct {
	Domain      string         `json:"domain"`
	DomainLabel string         `json:"domain_label"`
	TableName   string         `json:"table_name"`
	Fields      []RuleFieldDef `json:"fields"`
}

type RuleFieldDef struct {
	Field     string   `json:"field"`
	Label     string   `json:"label"`
	Operators []string `json:"operators"`
}

type RuleAssignmentResp struct {
	ID          uint64 `json:"id"`
	RuleID      uint64 `json:"rule_id"`
	TargetType  int    `json:"target_type"`
	TargetID    uint64 `json:"target_id"`
	TargetScope int    `json:"target_scope"`
	CreateTime  string `json:"create_time"`
}

type RuleAssignmentListResp struct {
	List []RuleAssignmentResp `json:"list"`
}
