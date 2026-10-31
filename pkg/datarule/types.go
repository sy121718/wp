package datarule

import "context"

type RuleConfig struct {
	OmitFields      []string         `json:"omit_fields"`
	ConditionGroups []ConditionGroup `json:"condition_groups"`
}

const (
	LogicAnd = "AND"
	LogicOr  = "OR"
)

type ConditionGroup struct {
	Logic      string      `json:"logic"`
	Conditions []Condition `json:"conditions"`
}

type Condition struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value string `json:"value"`
}

type FilterCondition struct {
	Query string
	Args  []any
}

type UserContext struct {
	UserID  uint64
	DeptID  uint64
	IsAdmin int
	Roles   []string

	DeptSubtreeIDs []uint64
}

type DomainConfig struct {
	Domain      string
	DomainLabel string
	TableName   string
	WhiteList   []FieldDef
}

type FieldDef struct {
	Field     string   `json:"field"`
	Label     string   `json:"label"`
	Operators []string `json:"operators"`
}

type RuleProvider interface {
	GetRules(ctx context.Context, user *UserContext, domain string) ([]RuleConfig, error)
}
