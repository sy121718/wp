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
	"reflect"
	"strings"

	"gorm.io/gorm/schema"
)

// 实体字段 tag 名：声明该字段可在数据权限规则中配置。
const entityFieldTag = "datarule"

// DomainFromEntity 由实体字段上的 datarule tag 派生数据域声明。
//
// tag 语法（分号分隔键值对，值内以逗号分隔列表，与 gorm tag 并列写在字段上）：
//
//	DeptID uint64 `gorm:"column:dept_id" datarule:"label=所属部门;ops=EQ,NEQ,IN,NOT_IN"`
//
// 约定：
//   - 没有 datarule tag 的字段不进白名单 —— 可配置字段必须显式声明（fail-closed），
//     「字段白名单靠另抄一份」正是漂移的来源（抄错列名/类型都不会有人发现）；
//   - label 与 ops 都必填，ops 的每一项都必须被引擎支持（IsSupportedOp）；
//   - 列名取 gorm 的 column 标签，缺省按 gorm 命名策略推导（DeptID → dept_id）；
//   - 表名必须由实体实现 TableName() 提供，不猜 gorm 默认表名 —— 表名错一个字符，
//     resolveDomain 就永不命中，该表的行级过滤会静默失效；
//   - 任何一处不合法都返回 error（装配期 fail-fast，不允许白名单悄悄降级）。
func DomainFromEntity(domain, label string, entity any) (DomainConfig, error) {
	if entity == nil {
		return DomainConfig{}, fmt.Errorf("datarule: 数据域 %s 的实体为 nil", domain)
	}

	typ := reflect.TypeOf(entity)
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return DomainConfig{}, fmt.Errorf("datarule: 数据域 %s 的实体必须是结构体，实际为 %s", domain, typ.Kind())
	}

	table, ok := entity.(interface{ TableName() string })
	if !ok || strings.TrimSpace(table.TableName()) == "" {
		return DomainConfig{}, fmt.Errorf("datarule: 数据域 %s 的实体 %s 必须实现 TableName() 并返回非空表名", domain, typ.Name())
	}

	naming := schema.NamingStrategy{}
	whiteList := make([]FieldDef, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		raw, tagged := field.Tag.Lookup(entityFieldTag)
		if !tagged {
			continue
		}
		if !field.IsExported() {
			return DomainConfig{}, fmt.Errorf("datarule: 数据域 %s 的字段 %s 非导出字段不能声明为可配置字段", domain, field.Name)
		}

		fieldLabel, ops, err := parseEntityFieldTag(raw)
		if err != nil {
			return DomainConfig{}, fmt.Errorf("datarule: 数据域 %s 的字段 %s %w", domain, field.Name, err)
		}
		whiteList = append(whiteList, FieldDef{
			Field:     columnNameOf(field, naming),
			Label:     fieldLabel,
			Operators: ops,
		})
	}

	return DomainConfig{
		Domain:      domain,
		DomainLabel: label,
		TableName:   strings.TrimSpace(table.TableName()),
		WhiteList:   whiteList,
	}, nil
}

// parseEntityFieldTag 解析实体字段的 datarule tag：label=用户名;ops=EQ,NEQ,LIKE。
// 未知键、缺 label、缺 ops、不支持的操作符一律报错（不静默忽略）。
func parseEntityFieldTag(raw string) (label string, ops []string, err error) {
	seen := make(map[string]bool)
	for _, part := range strings.Split(raw, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, value, found := strings.Cut(part, "=")
		if !found {
			return "", nil, fmt.Errorf("datarule tag 片段 %q 缺少 =", part)
		}
		switch strings.TrimSpace(key) {
		case "label":
			label = strings.TrimSpace(value)
		case "ops":
			for _, op := range strings.Split(value, ",") {
				op = strings.ToUpper(strings.TrimSpace(op))
				if op == "" {
					continue
				}
				if !IsSupportedOp(op) {
					return "", nil, fmt.Errorf("datarule tag 使用了不支持的操作符 %q（支持：%s）", op, strings.Join(Operators, ", "))
				}
				if !seen[op] {
					seen[op] = true
					ops = append(ops, op)
				}
			}
		default:
			return "", nil, fmt.Errorf("datarule tag 出现未知键 %q（只支持 label 与 ops）", strings.TrimSpace(key))
		}
	}
	if label == "" {
		return "", nil, fmt.Errorf("datarule tag 缺少 label")
	}
	if len(ops) == 0 {
		return "", nil, fmt.Errorf("datarule tag 缺少 ops")
	}
	return label, ops, nil
}

// columnNameOf 取实体字段的列名：优先 gorm 的 column 标签，缺省按 gorm 命名策略推导。
func columnNameOf(field reflect.StructField, naming schema.NamingStrategy) string {
	if tag, ok := field.Tag.Lookup("gorm"); ok {
		for _, part := range strings.Split(tag, ";") {
			key, value, found := strings.Cut(part, ":")
			if found && strings.TrimSpace(key) == "column" {
				if column := strings.TrimSpace(value); column != "" {
					return column
				}
			}
		}
	}
	return naming.ColumnName("", field.Name)
}
