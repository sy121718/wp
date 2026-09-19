// Package datarule 提供基于 GORM 插件的数据权限控制能力。
// 通过注册数据域（Domain）与规则提供者（RuleProvider），把「谁能看、谁能改」下沉到 GORM 回调链。
//
// 覆盖范围（读写两路，改这个包之前请先读完这段）：
//
//   - Query（读）：行级读保护 —— 注入 WHERE；字段级读屏蔽 —— 注入 Omits，
//     GORM 在 SELECT 语义下把它解释为「不查询该列」。
//   - Create（写）：值校验。Create 没有 WHERE 可以注入，语义是 CASL 式的
//     「检查将要创建的对象」：从 db.Statement.Dest 反射取值，逐条与条件组比对，
//     不满足即拒绝落库（批量创建逐元素校验）。**这是行为变更** ——
//     在此之前 Create 完全不受数据权限约束。求值失败（字段在 Dest 上取不到、
//     类型不认识、Dest 形状不支持）一律 fail-closed 拒绝，绝不静默放过。
//   - Update（写）：行级写保护 —— 注入与 Query **完全相同**的行条件（与 Query
//     共用同一份条件构造，不另写一套，否则两套语义会漂移）；字段级写屏蔽 ——
//     复用同一份 OmitFields，GORM 在 UPDATE 语义下把 Omits 解释为「不更新该列」。
//     语句执行后影响行数为 0 时返回明确错误（原因与方言边界见 beforeUpdate / afterUpdate 的注释）。
//   - Delete（写）：行级写保护 —— 同样复用 Query 的行条件构造；执行后 0 行同样报错。
//     DELETE 没有字段概念，因此不注入 Omits。
//
// 规则取不到（provider 报错）→ 直接 AddError 并终止该回调（与 Query 路一致）；
// 规则集为空表示「该用户在该域没有任何限制」→ 放行，这是既有语义。
//
// **Query 之外的路径没有任何兜底**：本插件只在 GORM 的 Query / Create / Update / Delete
// 回调链上生效。裸 SQL（db.Raw / db.Exec）、未经 context 传入 UserContext 的调用
// （GetUserContext 返回 nil）、以及**未注册数据域的表**，全部不经过这里 —— 它们在数据权限
// 意义上等于「不受约束」。要保护一张表：先在本包注册它的数据域，再保证写它的语句走 GORM
// 回调链且 context 里带着 UserContext。
package datarule

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// DataRulePlugin GORM 插件：按用户上下文与数据域配置，在 GORM 读、写两路回调上注入数据权限约束。
// 读路（Query）注入行级 WHERE 与字段级 Omits；写路：Create 做值校验、
// Update/Delete 注入同一份行条件并做 0 行判定、Update 另外复用 Omits 做字段级写屏蔽。
// 完整覆盖范围与「Query 之外没有兜底」的边界见包注释。
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

// GORM 回调名。集中声明，避免注册侧与测试清理侧写不一致的字符串常量。
const (
	callbackBeforeQuery  = "datarule:before_query"
	callbackBeforeCreate = "datarule:before_create"
	callbackBeforeUpdate = "datarule:before_update"
	callbackAfterUpdate  = "datarule:after_update"
	callbackBeforeDelete = "datarule:before_delete"
	callbackAfterDelete  = "datarule:after_delete"
)

// appliedScopeKey db.Statement.Settings 里的标记：本语句已被数据权限接管
// （Before 阶段解析出了该域对该用户的有效规则）。After 阶段据此决定是否做 0 行检查。
//
// 为什么需要这个标记：0 行检查的**唯一**目的是把「没权限，所以什么也没改成」如实报出来。
// 对未注册数据域的表、对该域没有任何规则的用户，插件从头到尾没有参与这条语句，
// 此时再报「数据权限拒绝」既是错的（与权限无关），也会把全站「更新/删除一个不存在的 id」
// 的正常业务错误改写成权限错误。所以「插件参与了这条语句」才是报错的前提。
const appliedScopeKey = "datarule:applied"

// Initialize 注册读、写两路的 GORM 回调。如果 provider 未设置则返回错误。
//
// 读路径的注册位置与改造前**逐字相同**（Query 链、gorm:query 之前），Query 行为不变。
// 写路径：
//   - Update：Before 注入行级 WHERE + 字段级 Omits，After 做 0 行检查；
//   - Delete：Before 注入行级 WHERE，After 做 0 行检查；
//   - Create：Before 对 Dest 做值校验（Create 没有 WHERE 可注入）。
//
// Update/Delete 的 Before 挂在 gorm:update / gorm:delete **之前**、After 挂在**之后**：
// 0 行检查必须发生在 gorm 主回调之后（那时 RowsAffected 才被填上），同时又在
// gorm:commit_or_rollback_transaction 之前 —— 所以这里 AddError 会让 gorm 回滚该语句。
// 对 0 行来说本来就没有改动需要回滚，回滚只是让「什么都没改成」这件事在调用方可见。
//
// 另需确认的一点：gorm 的 ErrMissingWhereClause 检查在 gorm:update / gorm:delete
// **主回调内部**（callbacks.checkMissingWhereConditions）、在我们的 Before 之后读
// Statement.Clauses["WHERE"]，所以这里注入的条件能挡住「无条件全表更新」的误判 ——
// 有规则时，不带 WHERE 的 Update/Delete 是被允许的，且只会命中可见行。
func (p *DataRulePlugin) Initialize(db *gorm.DB) error {
	if p.provider == nil {
		return fmt.Errorf("datarule RuleProvider 未设置")
	}

	// 读路径。
	if err := db.Callback().Query().Before("gorm:query").Register(callbackBeforeQuery, p.beforeQuery); err != nil {
		return err
	}

	// 写路径 - Update。
	if err := db.Callback().Update().Before("gorm:update").Register(callbackBeforeUpdate, p.beforeUpdate); err != nil {
		return err
	}
	if err := db.Callback().Update().After("gorm:update").Register(callbackAfterUpdate, p.afterUpdate); err != nil {
		return err
	}

	// 写路径 - Delete。
	if err := db.Callback().Delete().Before("gorm:delete").Register(callbackBeforeDelete, p.beforeDelete); err != nil {
		return err
	}
	if err := db.Callback().Delete().After("gorm:delete").Register(callbackAfterDelete, p.afterDelete); err != nil {
		return err
	}

	// 写路径 - Create（值校验，没有 WHERE 可注入）。
	if err := db.Callback().Create().Before("gorm:create").Register(callbackBeforeCreate, p.beforeCreate); err != nil {
		return err
	}

	return nil
}

// ruleScope 一次语句级的数据权限解析结果。
type ruleScope struct {
	merged RuleConfig   // 合并后的规则：OmitFields 取并集、条件组全部保留（组间 AND）
	user   *UserContext // 求值 dept.scope:* 引用表达式所需的用户上下文
}

// resolveRuleScope 是 Query / Create / Update / Delete 四路**共用**的规则解析入口。
// 返回 ok=false 表示本语句不受数据权限约束，调用方应当完全不介入：
//   - context 里没有 UserContext（非业务查询）；
//   - 表名未注册数据域（resolveDomain 未命中）；
//   - provider 报错（此时已 AddError，fail-closed 终止本回调）；
//   - 该用户在该域没有任何规则（空规则集 = 无限制，既有语义）。
//
// 提取成共用方法的目的是**语义只有一份**：写路径若另写一套条件构造，两边的 AND/OR 组合、
// 部门范围方向、方言引用符会各自漂移，而这类漂移在测试里表现为「读不到、写却能改」——
// 正是本插件最初只挂 Query 时踩过的坑。
func (p *DataRulePlugin) resolveRuleScope(db *gorm.DB) (ruleScope, bool) {
	// 1. 用户上下文：没有就说明不是业务请求，不做任何拦截。
	uc := GetUserContext(db.Statement.Context)
	if uc == nil {
		return ruleScope{}, false
	}

	// 2. 解析数据域（表名优先取 Statement.Table，缺失时回退模型的 TableName()）。
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
		return ruleScope{}, false // 未注册数据域，不拦截
	}

	// 3. 查询规则。取不到规则一律终止本回调（fail-closed），不允许退化成「无限制」。
	rules, err := p.provider.GetRules(db.Statement.Context, uc, domain)
	if err != nil {
		db.AddError(err)
		return ruleScope{}, false
	}
	if len(rules) == 0 {
		return ruleScope{}, false
	}

	// 4. 合并所有命中规则：行条件保持 AND（逐组注入），字段屏蔽取并集。
	// 同时标记「本语句已被数据权限接管」，供 After 阶段判断是否要做 0 行检查。
	db.Statement.Settings.Store(appliedScopeKey, true)
	return ruleScope{merged: mergeRules(rules), user: uc}, true
}

// scopeApplied 报告 Before 阶段是否已经把数据权限规则挂到了这条语句上。
func scopeApplied(db *gorm.DB) bool {
	if db.Statement == nil {
		return false
	}
	_, ok := db.Statement.Settings.Load(appliedScopeKey)
	return ok
}

// applyOmitFields 注入字段屏蔽列表。
//
// **同一个字段、两种含义**：Query 语义下 GORM 把 Omits 解释为「不 SELECT 该列」
// （字段级读保护）；Update 语义下 callbacks.ConvertToAssignments 经
// Statement.SelectAndOmitColumns 用同一份 Omits 过滤 SET 子句，于是它变成
// 「不 UPDATE 该列」（字段级写保护）。规则里只声明一份 OmitFields，读写两侧各取所需 ——
// 这也是写路径必须复用这里、不能另起一份字段列表的原因。
func applyOmitFields(db *gorm.DB, merged RuleConfig) {
	if len(merged.OmitFields) > 0 {
		db.Statement.Omits = merged.OmitFields
	}
}

// applyRowConditions 注入行级 WHERE 条件，Query / Update / Delete 三路**同一份实现**。
// 组间为 AND（逐组 AddClause），方言决定标识符引用符与部门范围子查询写法。
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

// beforeQuery GORM Query 阶段的前置回调（行级读保护 + 字段级读屏蔽）。
// 改造只是把原来内联的步骤拆成 resolveRuleScope / applyOmitFields / applyRowConditions，
// 执行顺序与注入的 SQL、参数**逐字不变**（既有断言见 plugin_test.go）。
func (p *DataRulePlugin) beforeQuery(db *gorm.DB) {
	scope, ok := p.resolveRuleScope(db)
	if !ok {
		return
	}
	// 5/6. 字段屏蔽 + WHERE 条件，顺序与改造前一致。
	applyOmitFields(db, scope.merged)
	applyRowConditions(db, scope.merged, scope.user)
}

// dialectOf 返回当前查询所属数据库的方言名（postgres/mysql/sqlite/sqlserver...）。
// Dialector 缺失或名称为空时回退 postgres（本项目主库），保证标识符引用符安全。
func dialectOf(db *gorm.DB) string {
	if db != nil && db.Dialector != nil {
		if name := strings.TrimSpace(db.Dialector.Name()); name != "" {
			return strings.ToLower(name)
		}
	}
	return "postgres"
}

// quoteField 按方言为已白名单化的字段名选择标识符引用符：
// mysql 使用反引号；postgres/sqlite/sqlserver 等使用标准双引号（H4：反引号在 PostgreSQL 上非法）。
func quoteField(dialect, field string) string {
	if dialect == "mysql" {
		return "`" + field + "`"
	}
	return `"` + field + `"`
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
	// 读的是不可变快照（engine.go 的 domainsSnapshot），热路径零锁。
	// 此前这里直接遍历包级 map 且未加锁，与 engine.go 注释声称的
	// 「读写一律经 registeredDomainMu」不符 —— 见 registeredDomains 的说明。
	for _, cfg := range domainsSnapshot() {
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
// dialect 决定标识符引用符与部门范围子查询的字符串拼接写法。
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

// buildCondition 将单条过滤条件转换为参数化 SQL 与参数列表。
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

// deptScopeCondition 构建「本部门及全部子部门」过滤条件（H5）。
//
// **方向语义写死在这里**：本函数服务的是「本部门及其子部门」——**向下**展开的可看范围，
// 所以它消费 UserContext.DeptSubtreeIDs（子树），而不是祖先链。祖先链是**向上**的，
// 它的用途是「匹配分配给上级部门的规则」（那段匹配在 admin 的规则快照里用 dept_ancestors
// 完成，不进本结构）。拿祖先列表去填这个 IN 会把可见范围放大到上级部门（越权）。
//
// 两条路径：
//  1. UserContext.DeptSubtreeIDs 非空 → 直接展开 field IN (?,?,...)。这是优化路径：
//     走目标表 dept_id 上的索引，且**完全没有子查询**（旧路径每一次查询都要把 sys_dept
//     整表扫一遍：左边 (',' || ancestors || ',') 是表达式、匹配又在中间，B-tree 用不上）。
//  2. 为空 → 回退下面的子查询，保证既有调用方与既有测试的行为逐字不变。
//
// 回退路径的语义（H5 的原实现）：sys_dept.ancestors 以逗号分隔存储祖先链（如 "0,1,14"）。
// 旧实现 `ancestors LIKE '%{DeptID}%'` 做的是子串匹配：部门 1 会误命中祖先链
// "0,14,41"（"1" 是 "14"/"41" 的子串）。这里对 ancestors 首尾补逗号后整段精确匹配：
//   - PostgreSQL（|| 拼接）：(',' || ancestors || ',') LIKE ('%,' || ? || ',%')
//   - MySQL（CONCAT 拼接）：CONCAT(',', ancestors, ',') LIKE CONCAT('%,', ?, ',%')
//
// 回退路径的 OR id = ? 保留本部门自身命中。
func deptScopeCondition(dialect, field string, uc *UserContext) (FilterCondition, bool) {
	// 优化路径：调用方（中间件 + admin 的部门快照）已把子树算好，这里只做参数绑定。
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
