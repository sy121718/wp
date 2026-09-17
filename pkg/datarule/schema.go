// Package datarule 提供基于 GORM 插件的数据权限控制能力。
// 通过注册数据域（Domain）与规则提供者（RuleProvider），
// 在 GORM Query 回调中自动注入行级数据过滤条件（WHERE 子句与 Omit 字段）。
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
