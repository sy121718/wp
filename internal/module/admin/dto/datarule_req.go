package admindto

import "go_wp/pkg/datarule"

type RuleListReq struct {
	Page   int    `form:"page" json:"page"`
	Limit  int    `form:"limit" json:"limit"`
	Domain string `form:"domain" json:"domain"`
	Status *int   `form:"status" json:"status"`
}

func (r *RuleListReq) GetPage() int {

	if r.Page < 1 {
		return 1
	}
	return r.Page
}

func (r *RuleListReq) GetLimit() int {

	if r.Limit < 1 {
		return 10
	}
	if r.Limit > 100 {
		return 100
	}
	return r.Limit
}

type RuleDetailReq struct {
	ID uint64 `form:"id" json:"id" binding:"required" validate:"required"`
}

type RuleConfigDTO struct {
	OmitFields []string `json:"omit_fields" binding:"omitempty,dive,max=64" validate:"omitempty,dive,max=64"`

	ConditionGroups []RuleConditionGroupDTO `json:"condition_groups" binding:"omitempty,dive" validate:"omitempty,dive"`
}

type RuleConditionGroupDTO struct {
	Logic      string             `json:"logic" binding:"required,oneof=AND OR" validate:"required,oneof=AND OR"`
	Conditions []RuleConditionDTO `json:"conditions" binding:"omitempty,dive" validate:"omitempty,dive"`
}

type RuleConditionDTO struct {
	Field string `json:"field" binding:"required,max=64" validate:"required,max=64"`

	Op    string `json:"op" binding:"required,oneof=EQ NEQ GT GTE LT LTE IN NOT_IN LIKE NOT_LIKE BETWEEN" validate:"required,oneof=EQ NEQ GT GTE LT LTE IN NOT_IN LIKE NOT_LIKE BETWEEN"`
	Value string `json:"value" binding:"max=512" validate:"max=512"`
}

func (c RuleConfigDTO) ToRuleConfig() datarule.RuleConfig {
	groups := make([]datarule.ConditionGroup, 0, len(c.ConditionGroups))
	for _, group := range c.ConditionGroups {
		conditions := make([]datarule.Condition, 0, len(group.Conditions))
		for _, condition := range group.Conditions {
			conditions = append(conditions, datarule.Condition{
				Field: condition.Field,
				Op:    condition.Op,
				Value: condition.Value,
			})
		}
		groups = append(groups, datarule.ConditionGroup{Logic: group.Logic, Conditions: conditions})
	}
	return datarule.RuleConfig{OmitFields: c.OmitFields, ConditionGroups: groups}
}

type RuleCreateReq struct {
	RuleName string        `json:"rule_name" form:"rule_name" binding:"required,max=100" validate:"required,max=100"`
	Domain   string        `json:"domain" form:"domain" binding:"required,max=50" validate:"required,max=50"`
	Config   RuleConfigDTO `json:"config" form:"config" binding:"required" validate:"required"`
	Status   int           `json:"status" form:"status" binding:"oneof=0 1" validate:"oneof=0 1"`
	Remark   string        `json:"remark" form:"remark" binding:"omitempty,max=200" validate:"omitempty,max=200"`
}

type RuleUpdateReq struct {
	ID       uint64        `json:"id" form:"id" binding:"required" validate:"required"`
	RuleName string        `json:"rule_name" form:"rule_name" binding:"required,max=100" validate:"required,max=100"`
	Domain   string        `json:"domain" form:"domain" binding:"required,max=50" validate:"required,max=50"`
	Config   RuleConfigDTO `json:"config" form:"config" binding:"required" validate:"required"`
	Status   int           `json:"status" form:"status" binding:"oneof=0 1" validate:"oneof=0 1"`
	Remark   string        `json:"remark" form:"remark" binding:"omitempty,max=200" validate:"omitempty,max=200"`
}

type RuleDeleteReq struct {
	IDs []uint64 `json:"ids" form:"ids" binding:"required,min=1" validate:"required,min=1"`
}

type RuleSchemaDetailReq struct {
	Domain string `form:"domain" json:"domain" binding:"required" validate:"required"`
}

type RuleAssignmentListReq struct {
	RuleID uint64 `form:"rule_id" json:"rule_id" binding:"required" validate:"required"`
}

type RuleAssignmentSaveReq struct {
	RuleID      uint64               `json:"rule_id" form:"rule_id" binding:"required" validate:"required"`
	Assignments []RuleAssignmentItem `json:"assignments" form:"assignments"`
}

type RuleAssignmentItem struct {
	TargetType  int    `json:"target_type" binding:"required,oneof=1 2 3" validate:"required,oneof=1 2 3"`
	TargetID    uint64 `json:"target_id" binding:"required" validate:"required"`
	TargetScope int    `json:"target_scope" binding:"omitempty,oneof=1 2" validate:"omitempty,oneof=1 2"`
}
