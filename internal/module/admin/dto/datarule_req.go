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

// Config 在两个基础表单里**不由列表抽屉提交**：新建从空配置开始，
// 更新沿用库里已有配置（要改配置请去编辑页）。所以它不是这两个端点的必填字段；
// 标成 required 只会让字段门禁与真实提交形状对不上（gin 对结构体的 required
// 也是恒真，拦不住任何东西）。
type RuleCreateReq struct {
	RuleName string        `json:"rule_name" form:"rule_name" binding:"required,max=100" validate:"required,max=100"`
	Domain   string        `json:"domain" form:"domain" binding:"required,max=50" validate:"required,max=50"`
	Config   RuleConfigDTO `json:"config" form:"config"`
	Status   int           `json:"status" form:"status" binding:"oneof=0 1" validate:"oneof=0 1"`
	Remark   string        `json:"remark" form:"remark" binding:"omitempty,max=200" validate:"omitempty,max=200"`
}

type RuleUpdateReq struct {
	ID       uint64        `json:"id" form:"id" binding:"required" validate:"required"`
	RuleName string        `json:"rule_name" form:"rule_name" binding:"required,max=100" validate:"required,max=100"`
	Domain   string        `json:"domain" form:"domain" binding:"required,max=50" validate:"required,max=50"`
	Config   RuleConfigDTO `json:"config" form:"config"`
	Status   int           `json:"status" form:"status" binding:"oneof=0 1" validate:"oneof=0 1"`
	Remark   string        `json:"remark" form:"remark" binding:"omitempty,max=200" validate:"omitempty,max=200"`

	// 条件编辑器的线上字段：组状态与条件行都是静态平行数组（同名控件按出现顺序多值）。
	// 它们由控制器在绑定后按顺序装配成 Config，DTO 只声明线上形状，不做业务解析。
	ConfigEditor string   `json:"config_editor" form:"config_editor"`
	OmitFields   []string `json:"omit_fields" form:"omit_fields"`
	GroupLogic   []string `json:"groupLogic" form:"groupLogic"`
	CondGroup    []string `json:"condGroup" form:"condGroup"`
	CondField    []string `json:"condField" form:"condField"`
	CondOp       []string `json:"condOp" form:"condOp"`
	CondValue    []string `json:"condValue" form:"condValue"`
}

type RuleDeleteReq struct {
	ID  uint64   `json:"id" form:"id"`
	IDs []uint64 `json:"ids" form:"ids"`
}

// IDList 返回本次要处理的主键集合：单条传 id，批量传 ids，两者都传时以 ids 为准。
func (r *RuleDeleteReq) IDList() []uint64 {
	if len(r.IDs) > 0 {
		return r.IDs
	}
	if r.ID == 0 {
		return nil
	}
	return []uint64{r.ID}
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
