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

	// DeptSubtreeIDs 当前用户所属部门的子树 id 列表（含自身与全部子孙部门），可空。
	//
	// **方向语义（别再把两个方向搞混）**：
	//   - 子树 = **向下**展开的「我能看到哪些部门的数据」，dept.scope:SELF_AND_CHILDREN 用的是它；
	//   - 祖先链 = **向上**的「我隶属于哪些上级部门」，它只用于匹配「分配给上级部门的规则」，
	//     而那段匹配已经在 admin 的规则快照里用 dept_ancestors 做完（不进本结构）。
	//
	// 拿祖先列表去填 IN (...) 会把可见范围**放大到上级部门**（方向相反，后果是越权）；
	// 反过来把子树当成「我的上级」也一样错。要哪个方向，看条件表达式的语义。
	//
	// 本字段非空时 deptScopeCondition 直接生成 field IN (?,?,...)（走索引、无子查询）；
	// 为空时回退到既有的 sys_dept 子查询 —— 既有调用方与测试的行为逐字不变。
	DeptSubtreeIDs []uint64
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
