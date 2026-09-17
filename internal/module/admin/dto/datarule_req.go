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

// RuleConfigDTO 数据规则配置的请求/响应形状（JSON 与 pkg/datarule.RuleConfig 一致）。
//
// 形状与取值范围留在 dto、由 binding tag 声明式校验：「结构合法」随请求一起被拒，
// 进不到 service；「这个字段/操作符属不属于该数据域」需要域声明，才由 service 判定。
// 直接复用 datarule.RuleConfig 会让请求结构随引擎类型走，改引擎就改协议，也挂不上 tag。
type RuleConfigDTO struct {
	// OmitFields 查询结果中需要屏蔽的列名，每项都必须是该数据域白名单内的字段。
	OmitFields []string `json:"omit_fields" binding:"omitempty,dive,max=64" validate:"omitempty,dive,max=64"`
	// ConditionGroups 过滤条件组，组间为 AND。
	ConditionGroups []RuleConditionGroupDTO `json:"condition_groups" binding:"omitempty,dive" validate:"omitempty,dive"`
}

// RuleConditionGroupDTO 条件组：组内按 Logic 组合，组间为 AND。
type RuleConditionGroupDTO struct {
	// Logic 只允许 AND / OR（引擎侧对无法识别的取值会静默降级为 AND，这里直接拒绝）。
	Logic      string             `json:"logic" binding:"required,oneof=AND OR" validate:"required,oneof=AND OR"`
	Conditions []RuleConditionDTO `json:"conditions" binding:"omitempty,dive" validate:"omitempty,dive"`
}

// RuleConditionDTO 单条过滤条件。
type RuleConditionDTO struct {
	Field string `json:"field" binding:"required,max=64" validate:"required,max=64"`
	// Op 的候选集与 pkg/datarule.Operators 一致；「该字段是否允许这个操作符」由 service 按域白名单判定。
	Op    string `json:"op" binding:"required,oneof=EQ NEQ GT GTE LT LTE IN NOT_IN LIKE NOT_LIKE BETWEEN" validate:"required,oneof=EQ NEQ GT GTE LT LTE IN NOT_IN LIKE NOT_LIKE BETWEEN"`
	Value string `json:"value" binding:"max=512" validate:"max=512"`
}

// ToRuleConfig 转为引擎类型（service 侧校验/归一化后落库的唯一入口）。
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

// RuleCreateReq 新建数据规则。
type RuleCreateReq struct {
	RuleName string        `json:"rule_name" binding:"required,max=100" validate:"required,max=100"`
	Domain   string        `json:"domain" binding:"required,max=50" validate:"required,max=50"`
	Config   RuleConfigDTO `json:"config" binding:"required" validate:"required"`
	Status   int           `json:"status" binding:"oneof=0 1" validate:"oneof=0 1"`
	Remark   string        `json:"remark" binding:"omitempty,max=200" validate:"omitempty,max=200"`
}

// RuleUpdateReq 更新数据规则。
type RuleUpdateReq struct {
	ID       uint64        `json:"id" binding:"required" validate:"required"`
	RuleName string        `json:"rule_name" binding:"required,max=100" validate:"required,max=100"`
	Domain   string        `json:"domain" binding:"required,max=50" validate:"required,max=50"`
	Config   RuleConfigDTO `json:"config" binding:"required" validate:"required"`
	Status   int           `json:"status" binding:"oneof=0 1" validate:"oneof=0 1"`
	Remark   string        `json:"remark" binding:"omitempty,max=200" validate:"omitempty,max=200"`
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
