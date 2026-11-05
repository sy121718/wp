package admindto

import "go_wp/pkg/utils"

// RuleItem 的 create_time / update_time 是**时刻**（unix 毫秒），
// 显示格式由模板按当前语言与时区决定，后端不预先拼成 "2006-01-02 15:04:05"。
type RuleItem struct {
	ID         uint64          `json:"id"`
	RuleName   string          `json:"rule_name"`
	Domain     string          `json:"domain"`
	Status     int             `json:"status"`
	Remark     string          `json:"remark"`
	CreateTime *utils.JSONTime `json:"create_time"`
	UpdateTime *utils.JSONTime `json:"update_time"`
}

type RuleListResp struct {
	Total int64      `json:"total"`
	List  []RuleItem `json:"list"`
}

type RuleDetailResp struct {
	ID         uint64          `json:"id"`
	RuleName   string          `json:"rule_name"`
	Domain     string          `json:"domain"`
	Config     RuleConfigDTO   `json:"config"`
	Status     int             `json:"status"`
	Remark     string          `json:"remark"`
	CreateBy   uint64          `json:"create_by"`
	CreateTime *utils.JSONTime `json:"create_time"`
	UpdateBy   uint64          `json:"update_by"`
	UpdateTime *utils.JSONTime `json:"update_time"`
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
	ID          uint64          `json:"id"`
	RuleID      uint64          `json:"rule_id"`
	TargetType  int             `json:"target_type"`
	TargetID    uint64          `json:"target_id"`
	TargetScope int             `json:"target_scope"`
	CreateTime  *utils.JSONTime `json:"create_time"`
}

type RuleAssignmentListResp struct {
	List []RuleAssignmentResp `json:"list"`
}
