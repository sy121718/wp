package admindto

import "go_wp/pkg/datarule"

// RuleListReq 数据规则列表查询。
type RuleListReq struct {
	Page   int    `form:"page" json:"page"`
	Limit  int    `form:"limit" json:"limit"`
	Domain string `form:"domain" json:"domain"`
	Status *int   `form:"status" json:"status"`
}

func (r *RuleListReq) GetPage() int {
	// 页码最小为 1
	if r.Page < 1 {
		return 1
	}
	return r.Page
}

func (r *RuleListReq) GetLimit() int {
	// 每页条数限制在 1~100 之间
	if r.Limit < 1 {
		return 10
	}
	if r.Limit > 100 {
		return 100
	}
	return r.Limit
}

// RuleDetailReq 数据规则详情查询。
type RuleDetailReq struct {
	ID uint64 `form:"id" json:"id" binding:"required" validate:"required"`
}

// RuleCreateReq 新建数据规则。
type RuleCreateReq struct {
	RuleName string              `json:"rule_name" binding:"required,max=100" validate:"required,max=100"`
	Domain   string              `json:"domain" binding:"required,max=50" validate:"required,max=50"`
	Config   datarule.RuleConfig `json:"config" binding:"required" validate:"required"`
	Status   int                 `json:"status" binding:"oneof=0 1" validate:"oneof=0 1"`
	Remark   string              `json:"remark" binding:"omitempty,max=200" validate:"omitempty,max=200"`
}

// RuleUpdateReq 更新数据规则。
type RuleUpdateReq struct {
	ID       uint64              `json:"id" binding:"required" validate:"required"`
	RuleName string              `json:"rule_name" binding:"required,max=100" validate:"required,max=100"`
	Domain   string              `json:"domain" binding:"required,max=50" validate:"required,max=50"`
	Config   datarule.RuleConfig `json:"config" binding:"required" validate:"required"`
	Status   int                 `json:"status" binding:"oneof=0 1" validate:"oneof=0 1"`
	Remark   string              `json:"remark" binding:"omitempty,max=200" validate:"omitempty,max=200"`
}

// RuleDeleteReq 批量删除数据规则。
type RuleDeleteReq struct {
	IDs []uint64 `json:"ids" binding:"required,min=1" validate:"required,min=1"`
}

// RuleSchemaDetailReq 查询 domain 详情。
type RuleSchemaDetailReq struct {
	Domain string `form:"domain" json:"domain" binding:"required" validate:"required"`
}

// RuleAssignmentListReq 查询规则分配列表。
type RuleAssignmentListReq struct {
	RuleID uint64 `form:"rule_id" json:"rule_id" binding:"required" validate:"required"`
}

// RuleAssignmentSaveReq 批量保存规则分配。
type RuleAssignmentSaveReq struct {
	RuleID      uint64               `json:"rule_id" binding:"required" validate:"required"`
	Assignments []RuleAssignmentItem `json:"assignments"`
}

// RuleAssignmentItem 分配项。
type RuleAssignmentItem struct {
	TargetType  int    `json:"target_type" binding:"required,oneof=1 2 3" validate:"required,oneof=1 2 3"`
	TargetID    uint64 `json:"target_id" binding:"required" validate:"required"`
	TargetScope int    `json:"target_scope" binding:"omitempty,oneof=1 2" validate:"omitempty,oneof=1 2"`
}
