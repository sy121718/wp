// Package dataruledto datarule 模块数据传输对象。
package dataruledto

import "go_wp/pkg/datarule"

// RuleItem 规则列表项。
type RuleItem struct {
	ID         uint64 `json:"id"`
	RuleName   string `json:"rule_name"`
	Domain     string `json:"domain"`
	Status     int    `json:"status"`
	Remark     string `json:"remark"`
	CreateTime string `json:"create_time"`
	UpdateTime string `json:"update_time"`
}

// ListResp 列表查询响应。
type ListResp struct {
	Total int64      `json:"total"`
	List  []RuleItem `json:"list"`
}

// DetailResp 规则详情。
type DetailResp struct {
	ID         uint64              `json:"id"`
	RuleName   string              `json:"rule_name"`
	Domain     string              `json:"domain"`
	Config     datarule.RuleConfig `json:"config"`
	Status     int                 `json:"status"`
	Remark     string              `json:"remark"`
	CreateBy   uint64              `json:"create_by"`
	CreateTime string              `json:"create_time"`
	UpdateBy   uint64              `json:"update_by"`
	UpdateTime string              `json:"update_time"`
}

// DomainItem 已注册 domain 简要信息。
type DomainItem struct {
	Domain      string `json:"domain"`
	DomainLabel string `json:"domain_label"`
	TableName   string `json:"table_name"`
}

// DomainDetail domain 详情含字段白名单。
type DomainDetail struct {
	Domain      string     `json:"domain"`
	DomainLabel string     `json:"domain_label"`
	TableName   string     `json:"table_name"`
	Fields      []FieldDef `json:"fields"`
}

// FieldDef 字段定义。
type FieldDef struct {
	Field     string   `json:"field"`
	Label     string   `json:"label"`
	DataType  string   `json:"data_type"`
	Operators []string `json:"operators"`
}

// AssignmentResp 分配记录。
type AssignmentResp struct {
	ID          uint64 `json:"id"`
	RuleID      uint64 `json:"rule_id"`
	TargetType  int    `json:"target_type"`
	TargetID    uint64 `json:"target_id"`
	TargetScope int    `json:"target_scope"`
	CreateTime  string `json:"create_time"`
}

// AssignmentListResp 分配列表响应。
type AssignmentListResp struct {
	List []AssignmentResp `json:"list"`
}