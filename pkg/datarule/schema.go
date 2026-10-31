package datarule

import (
	"fmt"
	"reflect"
	"strings"

	"gorm.io/gorm/schema"
)

const entityFieldTag = "datarule"

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
