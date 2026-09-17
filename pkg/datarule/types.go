// Package datarule 提供基于 GORM 插件的数据权限控制能力。
// 通过注册数据域（Domain）与规则提供者（RuleProvider），
// 在 GORM Query 回调中自动注入行级数据过滤条件（WHERE 子句与 Omit 字段）。
package datarule

import "context"

// RuleConfig 一条数据权限规则的配置，包含需要屏蔽的字段列表和条件组列表。
type RuleConfig struct {
	OmitFields      []string         `json:"omit_fields"`      // 需要屏蔽（排除）的字段名列表
	ConditionGroups []ConditionGroup `json:"condition_groups"` // 过滤条件组列表，组间为 AND 关系
}

// 条件组支持的组合逻辑。校验与引擎统一引用这两个常量，不再散落字面量。
const (
	LogicAnd = "AND"
	LogicOr  = "OR"
)

// ConditionGroup 条件组，组内多个条件按 Logic（AND/OR）组合。
// 多个 ConditionGroup 之间为 AND 关系。
type ConditionGroup struct {
	Logic      string      `json:"logic"`      // 条件组合逻辑：AND 或 OR
	Conditions []Condition `json:"conditions"` // 组内包含的过滤条件列表
}

// Condition 单条过滤条件，由字段名、操作符和值组成。
// 值支持普通参数值以及 dept.scope:SELF 等引用表达式。
type Condition struct {
	Field string `json:"field"` // 数据库字段名
	Op    string `json:"op"`    // 操作符：EQ, NEQ, GT, GTE, LT, LTE, IN, NOT_IN, LIKE, NOT_LIKE, BETWEEN
	Value string `json:"value"` // 条件值，支持 dept.scope:SELF 等引用语法
}

// FilterCondition 是可安全注入 ORM 的参数化过滤条件。
type FilterCondition struct {
	Query string
	Args  []any
}

// UserContext 用户身份上下文，由认证中间件解析会话后填充，
// 通过 context.Context 传递给 GORM 插件，供数据权限过滤使用。
type UserContext struct {
	UserID  uint64   // 当前用户 ID
	DeptID  uint64   // 当前用户所属部门 ID
	IsAdmin int      // 是否超级管理员（1 表示是，0 表示否）
	Roles   []string // 用户拥有的角色 code 列表
}

// DomainConfig 数据域注册信息，描述一个业务域对应的数据库表及允许配置的字段白名单。
type DomainConfig struct {
	Domain      string     // 业务域唯一标识，如 ADMIN、ORDER
	DomainLabel string     // 业务域中文名称，用于管理端展示
	TableName   string     // 该域对应的实际数据库表名
	WhiteList   []FieldDef // 允许在规则中配置的字段白名单
}

// FieldDef 数据域中允许配置的字段定义，描述字段的元信息及可用的操作符。
// 字段的可配置性由实体字段上的 datarule tag 声明，经 DomainFromEntity 派生 ——
// 没有 tag 的字段不在白名单里，规则也只能引用白名单内的字段。
// 只保留 label 与 operators：数据类型不是可配置性的判据，且它的权威来源是
// 数据库 catalog（information_schema），照抄进声明只会在列型变化时静默漂移。
type FieldDef struct {
	Field     string   `json:"field"`     // 数据库列名（取自实体 gorm column 标签）
	Label     string   `json:"label"`     // 字段的中文标签，用于前端展示
	Operators []string `json:"operators"` // 该字段允许使用的操作符，必须是引擎支持的操作符
}

// RuleProvider 规则查询接口，由外部模块注入实现。
// 根据完整用户上下文和业务域，返回该用户在该域下应遵守的所有数据权限规则。
type RuleProvider interface {
	// GetRules 查询用户指定数据域下的所有权限规则。
	GetRules(ctx context.Context, user *UserContext, domain string) ([]RuleConfig, error)
}
