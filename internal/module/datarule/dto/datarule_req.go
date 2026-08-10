// Package dataruledto datarule 模块数据传输对象。
package dataruledto

import "go_wp/pkg/datarule"

// ListReq 规则列表查询。
type ListReq struct {
	Page   int    `form:"page" json:"page"`
	Limit  int    `form:"limit" json:"limit"`
	Domain string `form:"domain" json:"domain"`
	Status *int   `form:"status" json:"status"`
}

func (r *ListReq) GetPage() int {
	// 页码最小为 1
	if r.Page < 1 {
		return 1
	}
	return r.Page
}

func (r *ListReq) GetLimit() int {
	// 每页条数限制在 1~100 之间
	if r.Limit < 1 {
		return 10
	}
	if r.Limit > 100 {
		return 100
	}
	return r.Limit
}

// DetailReq 规则详情查询。
type DetailReq struct {
	ID uint64 `form:"id" json:"id" binding:"required" validate:"required"`
}

// CreateReq 新建规则。
type CreateReq struct {
	RuleName string              `json:"rule_name" binding:"required,max=100" validate:"required,max=100"`
	Domain   string              `json:"domain" binding:"required,max=50" validate:"required,max=50"`
	Config   datarule.RuleConfig `json:"config" binding:"required" validate:"required"`
	Status   int                 `json:"status" binding:"oneof=0 1" validate:"oneof=0 1"`
	Remark   string              `json:"remark" binding:"omitempty,max=200" validate:"omitempty,max=200"`
}

// UpdateReq 更新规则。
type UpdateReq struct {
	ID       uint64              `json:"id" binding:"required" validate:"required"`
	RuleName string              `json:"rule_name" binding:"required,max=100" validate:"required,max=100"`
	Domain   string              `json:"domain" binding:"required,max=50" validate:"required,max=50"`
	Config   datarule.RuleConfig `json:"config" binding:"required" validate:"required"`
	Status   int                 `json:"status" binding:"oneof=0 1" validate:"oneof=0 1"`
	Remark   string              `json:"remark" binding:"omitempty,max=200" validate:"omitempty,max=200"`
}

// DeleteReq 批量删除规则。
type DeleteReq struct {
	IDs []uint64 `json:"ids" binding:"required,min=1" validate:"required,min=1"`
}

// SchemaDetailReq 查询 domain 详情。
type SchemaDetailReq struct {
	Domain string `form:"domain" json:"domain" binding:"required" validate:"required"`
}

// AssignmentListReq 查询规则分配列表。
type AssignmentListReq struct {
	RuleID uint64 `form:"rule_id" json:"rule_id" binding:"required" validate:"required"`
}

// AssignmentSaveReq 批量保存规则分配。
type AssignmentSaveReq struct {
	RuleID      uint64           `json:"rule_id" binding:"required" validate:"required"`
	Assignments []AssignmentItem `json:"assignments"`
}

// AssignmentItem 分配项。
type AssignmentItem struct {
	TargetType  int    `json:"target_type" binding:"required,oneof=1 2 3" validate:"required,oneof=1 2 3"`
	TargetID    uint64 `json:"target_id" binding:"required" validate:"required"`
	TargetScope int    `json:"target_scope" binding:"omitempty,oneof=1 2" validate:"omitempty,oneof=1 2"`
}