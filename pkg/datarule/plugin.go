package datarule

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type DataRulePlugin struct {
	provider RuleProvider
}

func NewDataRulePlugin(provider RuleProvider) *DataRulePlugin {
	return &DataRulePlugin{provider: provider}
}

func (p *DataRulePlugin) Name() string {
	return "datarule"
}

const (
	callbackBeforeQuery  = "datarule:before_query"
	callbackBeforeCreate = "datarule:before_create"
	callbackBeforeUpdate = "datarule:before_update"
	callbackAfterUpdate  = "datarule:after_update"
	callbackBeforeDelete = "datarule:before_delete"
	callbackAfterDelete  = "datarule:after_delete"
)

const appliedScopeKey = "datarule:applied"

func (p *DataRulePlugin) Initialize(db *gorm.DB) error {
	if p.provider == nil {
		return fmt.Errorf("datarule RuleProvider 未设置")
	}

	if err := db.Callback().Query().Before("gorm:query").Register(callbackBeforeQuery, p.beforeQuery); err != nil {
		return err
	}

	if err := db.Callback().Update().Before("gorm:update").Register(callbackBeforeUpdate, p.beforeUpdate); err != nil {
		return err
	}
	if err := db.Callback().Update().After("gorm:update").Register(callbackAfterUpdate, p.afterUpdate); err != nil {
		return err
	}

	if err := db.Callback().Delete().Before("gorm:delete").Register(callbackBeforeDelete, p.beforeDelete); err != nil {
		return err
	}
	if err := db.Callback().Delete().After("gorm:delete").Register(callbackAfterDelete, p.afterDelete); err != nil {
		return err
	}

	if err := db.Callback().Create().Before("gorm:create").Register(callbackBeforeCreate, p.beforeCreate); err != nil {
		return err
	}

	return nil
}

type ruleScope struct {
	merged RuleConfig
	user   *UserContext
}

func (p *DataRulePlugin) resolveRuleScope(db *gorm.DB) (ruleScope, bool) {

	uc := GetUserContext(db.Statement.Context)
	if uc == nil {
		return ruleScope{}, false
	}

	tableName := db.Statement.Table
	if tableName == "" {
		if stmt := db.Statement; stmt.Model != nil {
			if tn, ok := getTableName(stmt.Model); ok && tn != "" {
				tableName = tn
			}
		}
	}

	domain := resolveDomain(tableName)
	if domain == "" {
		return ruleScope{}, false
	}

	rules, err := p.provider.GetRules(db.Statement.Context, uc, domain)
	if err != nil {
		db.AddError(err)
		return ruleScope{}, false
	}
	if len(rules) == 0 {
		return ruleScope{}, false
	}

	db.Statement.Settings.Store(appliedScopeKey, true)
	return ruleScope{merged: mergeRules(rules), user: uc}, true
}

func scopeApplied(db *gorm.DB) bool {
	if db.Statement == nil {
		return false
	}
	_, ok := db.Statement.Settings.Load(appliedScopeKey)
	return ok
}

func applyOmitFields(db *gorm.DB, merged RuleConfig) {
	if len(merged.OmitFields) > 0 {
		db.Statement.Omits = merged.OmitFields
	}
}

func applyRowConditions(db *gorm.DB, merged RuleConfig, uc *UserContext) {
	dialect := dialectOf(db)
	for _, group := range merged.ConditionGroups {
		condition, ok := buildConditions(group, uc, dialect)
		if !ok {
			continue
		}
		db.Statement.AddClause(clause.Where{Exprs: []clause.Expression{
			clause.Expr{SQL: "(" + condition.Query + ")", Vars: condition.Args},
		}})
	}
}

func (p *DataRulePlugin) beforeQuery(db *gorm.DB) {
	scope, ok := p.resolveRuleScope(db)
	if !ok {
		return
	}

	applyOmitFields(db, scope.merged)
	applyRowConditions(db, scope.merged, scope.user)
}

func dialectOf(db *gorm.DB) string {
	if db != nil && db.Dialector != nil {
		if name := strings.TrimSpace(db.Dialector.Name()); name != "" {
			return strings.ToLower(name)
		}
	}
	return "postgres"
}

func quoteField(dialect, field string) string {
	if dialect == "mysql" {
		return "`" + field + "`"
	}
	return `"` + field + `"`
}

func getTableName(model interface{}) (string, bool) {
	if t, ok := model.(interface{ TableName() string }); ok {
		return t.TableName(), true
	}
	return "", false
}

func resolveDomain(tableName string) string {

	for _, cfg := range domainsSnapshot() {
		if cfg.TableName == tableName {
			return cfg.Domain
		}
	}
	return ""
}

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

func buildConditions(group ConditionGroup, uc *UserContext, dialect string) (FilterCondition, bool) {
	queries := make([]string, 0, len(group.Conditions))
	args := make([]any, 0, len(group.Conditions))
	for _, item := range group.Conditions {
		condition, ok := buildCondition(item, uc, dialect)
		if !ok {
			continue
		}
		queries = append(queries, condition.Query)
		args = append(args, condition.Args...)
	}
	if len(queries) == 0 {
		return FilterCondition{}, false
	}

	logic := LogicAnd
	if strings.EqualFold(group.Logic, LogicOr) {
		logic = LogicOr
	}
	return FilterCondition{
		Query: strings.Join(queries, " "+logic+" "),
		Args:  args,
	}, true
}

func buildCondition(c Condition, uc *UserContext, dialect string) (FilterCondition, bool) {
	fieldName := escapeField(c.Field)
	if fieldName == "" || !validOp(c.Op) {
		return FilterCondition{}, false
	}

	field := quoteField(dialect, fieldName)
	value := strings.TrimSpace(c.Value)

	if m := deptScopeRe.FindStringSubmatch(value); len(m) >= 2 {
		switch m[1] {
		case "SELF":
			return FilterCondition{Query: field + " = ?", Args: []any{uc.DeptID}}, true
		case "SELF_AND_CHILDREN":
			return deptScopeCondition(dialect, field, uc)
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

func deptScopeCondition(dialect, field string, uc *UserContext) (FilterCondition, bool) {

	if len(uc.DeptSubtreeIDs) > 0 {
		placeholders := make([]string, len(uc.DeptSubtreeIDs))
		args := make([]any, len(uc.DeptSubtreeIDs))
		for i, id := range uc.DeptSubtreeIDs {
			placeholders[i] = "?"
			args[i] = id
		}
		return FilterCondition{
			Query: field + " IN (" + strings.Join(placeholders, ",") + ")",
			Args:  args,
		}, true
	}

	deptID := fmt.Sprintf("%d", uc.DeptID)
	if dialect == "mysql" {
		return FilterCondition{
			Query: field + " IN (SELECT " + quoteField(dialect, "id") + " FROM sys_dept WHERE " +
				"CONCAT(',', " + quoteField(dialect, "ancestors") + ", ',') LIKE CONCAT('%,', ?, ',%') OR " +
				quoteField(dialect, "id") + " = ?)",
			Args: []any{deptID, uc.DeptID},
		}, true
	}
	return FilterCondition{
		Query: field + " IN (SELECT " + quoteField(dialect, "id") + " FROM sys_dept WHERE " +
			"(',' || " + quoteField(dialect, "ancestors") + " || ',') LIKE ('%,' || ? || ',%') OR " +
			quoteField(dialect, "id") + " = ?)",
		Args: []any{deptID, uc.DeptID},
	}, true
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

func escapeField(field string) string {
	for _, r := range field {
		if !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') && r != '_' {
			return ""
		}
	}
	return field
}
