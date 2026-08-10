// Package datarule 提供基于 GORM 插件的数据权限控制能力。
// 通过注册数据域（Domain）与规则提供者（RuleProvider），
// 在 GORM Query 回调中自动注入行级数据过滤条件（WHERE 子句与 Omit 字段）。
package datarule

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// DataRulePlugin GORM 插件，在 gorm:query 阶段自动注入数据权限规则。
// 通过拦截 Query 回调，根据用户上下文和数据域配置动态追加 WHERE 条件和 Omit 字段。
type DataRulePlugin struct {
	provider RuleProvider // 规则查询接口，用于获取用户的数据权限规则
}

// NewDataRulePlugin 创建数据规则插件实例，需要传入已初始化的规则提供者。
func NewDataRulePlugin(provider RuleProvider) *DataRulePlugin {
	return &DataRulePlugin{provider: provider}
}

// Name 返回插件名称，用于 GORM 插件注册标识。
func (p *DataRulePlugin) Name() string {
	return "datarule"
}

// Initialize 注册 GORM Query 阶段回调（在 gorm:query 之前插入 datarule:before_query）。
// 如果 provider 未设置则返回错误。
func (p *DataRulePlugin) Initialize(db *gorm.DB) error {
	if p.provider == nil {
		return fmt.Errorf("datarule RuleProvider 未设置")
	}
	return db.Callback().Query().Before("gorm:query").Register("datarule:before_query", p.beforeQuery)
}

// beforeQuery GORM Query 阶段的前置回调，作为 gorm:query 钩子在每次查询前执行。
// 主要步骤：
//  1. 从 context 中提取用户上下文（UserContext），非业务查询直接跳过。
//  2. 根据当前查询的表名匹配已注册的数据域（Domain），未匹配则跳过。
//  3. 通过 provider 查询该用户在指定数据域下的所有规则。
//  4. 合并所有命中主体的限制规则。
//  5. 注入 Omit 字段屏蔽不需要的列。
//  6. 将规则中的条件组转换为 WHERE 子句并追加到查询中。
func (p *DataRulePlugin) beforeQuery(db *gorm.DB) {
	// 1. 获取用户上下文
	uc := GetUserContext(db.Statement.Context)
	if uc == nil {
		return // 非业务查询不做拦截
	}

	// 2. 确定数据域
	tableName := db.Statement.Table
	if tableName == "" {
		// 尝试从模型获取表名
		if stmt := db.Statement; stmt.Model != nil {
			if tn, ok := getTableName(stmt.Model); ok && tn != "" {
				tableName = tn
			}
		}
	}

	domain := resolveDomain(tableName)
	if domain == "" {
		return // 未注册数据域，不拦截
	}

	// 3. 查询规则
	rules, err := p.provider.GetRules(db.Statement.Context, uc, domain)
	if err != nil {
		db.AddError(err)
		return
	}
	if len(rules) == 0 {
		return
	}

	// 4. 合并所有命中规则，行条件保持 AND，字段屏蔽取并集。
	merged := mergeRules(rules)

	// 5. 注入 Omit 字段
	if len(merged.OmitFields) > 0 {
		db.Statement.Omits = merged.OmitFields
	}

	// 6. 注入 WHERE 条件
	for _, group := range merged.ConditionGroups {
		condition, ok := buildConditions(group, uc)
		if !ok {
			continue
		}
		db.Statement.AddClause(clause.Where{Exprs: []clause.Expression{
			clause.Expr{SQL: "(" + condition.Query + ")", Vars: condition.Args},
		}})
	}
}

// getTableName 通过 TableName() 接口获取模型的数据库表名。
// 如果模型未实现 TableName() 接口则返回空字符串和 false。
func getTableName(model interface{}) (string, bool) {
	if t, ok := model.(interface{ TableName() string }); ok {
		return t.TableName(), true
	}
	return "", false
}

// resolveDomain 根据数据库表名在已注册的数据域中查找对应的业务域标识。
// 未找到匹配则返回空字符串。
func resolveDomain(tableName string) string {
	for _, cfg := range registeredDomains {
		if cfg.TableName == tableName {
			return cfg.Domain
		}
	}
	return ""
}

// mergeRules 合并多条规则配置。
// OmitFields 去重合并，ConditionGroups 直接拼接（保留所有条件组）。
func mergeRules(rules []RuleConfig) RuleConfig {
	merged := RuleConfig{}
	seenOmit := map[string]bool{}
	for _, r := range rules {
		for _, f := range r.OmitFields {
			if !seenOmit[f] {
				merged.OmitFields = append(merged.OmitFields, f)
				seenOmit[f] = true
			}
		}
		merged.ConditionGroups = append(merged.ConditionGroups, r.ConditionGroups...)
	}
	return merged
}

// buildConditions 将条件组中的有效条件合并为一个参数化 SQL 条件。
func buildConditions(group ConditionGroup, uc *UserContext) (FilterCondition, bool) {
	queries := make([]string, 0, len(group.Conditions))
	args := make([]any, 0, len(group.Conditions))
	for _, item := range group.Conditions {
		condition, ok := buildCondition(item, uc)
		if !ok {
			continue
		}
		queries = append(queries, condition.Query)
		args = append(args, condition.Args...)
	}
	if len(queries) == 0 {
		return FilterCondition{}, false
	}

	logic := "AND"
	if strings.EqualFold(group.Logic, "OR") {
		logic = "OR"
	}
	return FilterCondition{
		Query: strings.Join(queries, " "+logic+" "),
		Args:  args,
	}, true
}

// buildCondition 将单条过滤条件转换为参数化 SQL 与参数列表。
func buildCondition(c Condition, uc *UserContext) (FilterCondition, bool) {
	fieldName := escapeField(c.Field)
	if fieldName == "" || !validOp(c.Op) {
		return FilterCondition{}, false
	}

	field := "`" + fieldName + "`"
	value := strings.TrimSpace(c.Value)

	if m := deptScopeRe.FindStringSubmatch(value); len(m) >= 2 {
		switch m[1] {
		case "SELF":
			return FilterCondition{Query: field + " = ?", Args: []any{uc.DeptID}}, true
		case "SELF_AND_CHILDREN":
			return FilterCondition{
				Query: field + " IN (SELECT id FROM sys_dept WHERE ancestors LIKE ? OR id = ?)",
				Args:  []any{fmt.Sprintf("%%%d%%", uc.DeptID), uc.DeptID},
			}, true
		case "ALL":
			return FilterCondition{}, false
		default:
			return FilterCondition{}, false
		}
	}

	scalar := func(operator string) (FilterCondition, bool) {
		return FilterCondition{Query: field + " " + operator + " ?", Args: []any{value}}, true
	}

	switch strings.ToUpper(c.Op) {
	case "EQ":
		return scalar("=")
	case "NEQ":
		return scalar("!=")
	case "GT":
		return scalar(">")
	case "GTE":
		return scalar(">=")
	case "LT":
		return scalar("<")
	case "LTE":
		return scalar("<=")
	case "LIKE":
		return scalar("LIKE")
	case "NOT_LIKE":
		return scalar("NOT LIKE")
	case "IN", "NOT_IN":
		values := splitConditionValues(value)
		if len(values) == 0 {
			return FilterCondition{}, false
		}
		operator := "IN"
		if strings.EqualFold(c.Op, "NOT_IN") {
			operator = "NOT IN"
		}
		return FilterCondition{Query: field + " " + operator + " ?", Args: []any{values}}, true
	case "BETWEEN":
		values := splitConditionValues(value)
		if len(values) != 2 {
			return FilterCondition{}, false
		}
		return FilterCondition{Query: field + " BETWEEN ? AND ?", Args: []any{values[0], values[1]}}, true
	default:
		return FilterCondition{}, false
	}
}

func splitConditionValues(value string) []string {
	parts := strings.Split(value, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			values = append(values, trimmed)
		}
	}
	return values
}

// escapeField 过滤字段名，只允许字母、数字和下划线，防止 SQL 注入。
// 如果字段名包含非法字符则返回空字符串。
func escapeField(field string) string {
	for _, r := range field {
		if !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') && r != '_' {
			return ""
		}
	}
	return field
}
