package datarule

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func (p *DataRulePlugin) beforeUpdate(db *gorm.DB) {
	scope, ok := p.resolveRuleScope(db)
	if !ok {
		return
	}
	applyOmitFields(db, scope.merged)
	applyRowConditions(db, scope.merged, scope.user)
}

func (p *DataRulePlugin) beforeDelete(db *gorm.DB) {
	scope, ok := p.resolveRuleScope(db)
	if !ok {
		return
	}
	applyRowConditions(db, scope.merged, scope.user)
}

func (p *DataRulePlugin) afterUpdate(db *gorm.DB) {
	rejectMissingAffectedRows(db, "更新")
}

func (p *DataRulePlugin) afterDelete(db *gorm.DB) {
	rejectMissingAffectedRows(db, "删除")
}

func affectedRows(db *gorm.DB) int64 {
	if db.Statement != nil && db.Statement.DB != nil {
		return db.Statement.RowsAffected
	}
	return db.RowsAffected
}

func rejectMissingAffectedRows(db *gorm.DB, action string) {
	if db.Error != nil {
		return
	}
	if db.DryRun || !scopeApplied(db) {
		return
	}
	if affectedRows(db) != 0 {
		return
	}
	db.AddError(fmt.Errorf("数据权限拒绝：没有可%s的行（可能不存在或不在你的可见范围内）", action))
}

func (p *DataRulePlugin) beforeCreate(db *gorm.DB) {
	scope, ok := p.resolveRuleScope(db)
	if !ok {
		return
	}
	if len(scope.merged.ConditionGroups) == 0 {
		return
	}

	records, err := createRecords(db.Statement.Dest, db.Statement.Schema)
	if err != nil {
		db.AddError(fmt.Errorf("数据权限拒绝：无法校验将要创建的数据（%s）", err.Error()))
		return
	}

	for i, record := range records {
		for _, group := range scope.merged.ConditionGroups {
			matched, evalErr := evalGroup(record, group, scope.user)
			if evalErr != nil {
				db.AddError(fmt.Errorf("数据权限拒绝：无法校验第 %d 条将要创建的数据（%s）", i+1, evalErr.Error()))
				return
			}
			if !matched {
				db.AddError(fmt.Errorf("数据权限拒绝：第 %d 条将要创建的数据不满足当前用户的行级规则", i+1))
				return
			}
		}
	}
}

type recordValues map[string]any

func createRecords(dest any, sch *schema.Schema) ([]recordValues, error) {
	if dest == nil {
		return nil, fmt.Errorf("Create 的目标为 nil")
	}

	rv := reflect.ValueOf(dest)
	for rv.Kind() == reflect.Ptr || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return nil, fmt.Errorf("Create 的目标为 nil 指针")
		}
		rv = rv.Elem()
	}

	switch rv.Kind() {
	case reflect.Struct:
		return []recordValues{structRecord(rv)}, nil

	case reflect.Slice, reflect.Array:

		records := make([]recordValues, 0, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			item := rv.Index(i)
			for item.Kind() == reflect.Ptr || item.Kind() == reflect.Interface {
				if item.IsNil() {
					return nil, fmt.Errorf("第 %d 条数据为 nil 指针", i+1)
				}
				item = item.Elem()
			}
			if item.Kind() != reflect.Struct {
				return nil, fmt.Errorf("第 %d 条数据不是结构体（%s）", i+1, item.Kind())
			}
			records = append(records, structRecord(item))
		}
		return records, nil

	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String {
			return nil, fmt.Errorf("Create 的 map 键必须是字符串，实际为 %s", rv.Type().Key().Kind())
		}
		record := make(recordValues, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			key := iter.Key().String()
			column := key

			if sch != nil {
				if field := sch.LookUpField(key); field != nil && field.DBName != "" {
					column = field.DBName
				}
			}
			record[column] = iter.Value().Interface()
		}
		return []recordValues{record}, nil

	default:
		return nil, fmt.Errorf("Create 的目标形状不支持（%s）", rv.Kind())
	}
}

func structRecord(rv reflect.Value) recordValues {
	record := make(recordValues, rv.NumField())
	collectStructFields(rv, record)
	return record
}

func collectStructFields(rv reflect.Value, record recordValues) {
	naming := schema.NamingStrategy{}
	typ := rv.Type()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.PkgPath != "" && !field.Anonymous {
			continue
		}
		value := rv.Field(i)
		if field.Anonymous {
			inner := value
			for inner.Kind() == reflect.Ptr {
				if inner.IsNil() {
					inner = reflect.Value{}
					break
				}
				inner = inner.Elem()
			}
			if inner.IsValid() && inner.Kind() == reflect.Struct {
				collectStructFields(inner, record)
				continue
			}
		}
		if !value.CanInterface() {
			continue
		}
		column := columnNameOf(field, naming)
		if column == "" || column == "-" {
			continue
		}
		record[column] = value.Interface()
	}
}

func evalGroup(record recordValues, group ConditionGroup, uc *UserContext) (bool, error) {
	logic := LogicAnd
	if strings.EqualFold(group.Logic, LogicOr) {
		logic = LogicOr
	}

	evaluated := false
	for _, condition := range group.Conditions {
		matched, enforceable, err := evalCondition(record, condition, uc)
		if err != nil {
			return false, err
		}
		if !enforceable {
			continue
		}
		evaluated = true
		if logic == LogicOr && matched {
			return true, nil
		}
		if logic == LogicAnd && !matched {
			return false, nil
		}
	}
	if !evaluated {
		return true, nil
	}

	return logic == LogicAnd, nil
}

func evalCondition(record recordValues, c Condition, uc *UserContext) (matched bool, enforceable bool, err error) {
	fieldName := escapeField(c.Field)
	if fieldName == "" || !validOp(c.Op) {
		return false, false, nil
	}
	if uc == nil {
		uc = &UserContext{}
	}

	value := strings.TrimSpace(c.Value)
	if m := deptScopeRe.FindStringSubmatch(value); len(m) >= 2 {
		switch m[1] {
		case "SELF":
			return matchDeptID(record, fieldName, uc.DeptID)
		case "SELF_AND_CHILDREN":
			return matchDeptSubtree(record, fieldName, uc)
		default:
			return false, false, nil
		}
	}

	got, scalarErr := recordScalar(record, fieldName)
	if scalarErr != nil {
		return false, true, scalarErr
	}

	if got.kind == scalarNull {
		return false, true, nil
	}

	op := strings.ToUpper(c.Op)
	switch op {
	case "EQ", "NEQ", "GT", "GTE", "LT", "LTE":
		cmp, ok := scalarCompare(got, value)
		if !ok {
			return false, true, fmt.Errorf("字段 %s 的值 %v 无法与 %q 比较", fieldName, got.raw, value)
		}
		switch op {
		case "EQ":
			return cmp == 0, true, nil
		case "NEQ":
			return cmp != 0, true, nil
		case "GT":
			return cmp > 0, true, nil
		case "GTE":
			return cmp >= 0, true, nil
		case "LT":
			return cmp < 0, true, nil
		default:
			return cmp <= 0, true, nil
		}

	case "LIKE", "NOT_LIKE":
		text, ok := got.text()
		if !ok {
			return false, true, fmt.Errorf("字段 %s 的值 %v 无法做 LIKE 匹配", fieldName, got.raw)
		}
		hit := likeMatch(text, value)
		if op == "NOT_LIKE" {
			hit = !hit
		}
		return hit, true, nil

	case "IN", "NOT_IN":
		values := splitConditionValues(value)
		if len(values) == 0 {
			return false, false, nil
		}
		hit := false
		for _, candidate := range values {
			cmp, ok := scalarCompare(got, candidate)
			if !ok {
				return false, true, fmt.Errorf("字段 %s 的值 %v 无法与 %q 比较", fieldName, got.raw, candidate)
			}
			if cmp == 0 {
				hit = true
				break
			}
		}
		if op == "NOT_IN" {
			hit = !hit
		}
		return hit, true, nil

	case "BETWEEN":
		values := splitConditionValues(value)
		if len(values) != 2 {
			return false, false, nil
		}
		low, okLow := scalarCompare(got, values[0])
		high, okHigh := scalarCompare(got, values[1])
		if !okLow || !okHigh {
			return false, true, fmt.Errorf("字段 %s 的值 %v 无法与区间 [%s, %s] 比较", fieldName, got.raw, values[0], values[1])
		}
		return low >= 0 && high <= 0, true, nil
	}

	return false, false, nil
}

func recordScalar(record recordValues, column string) (scalarValue, error) {
	raw, ok := record[column]
	if !ok {
		return scalarValue{}, fmt.Errorf("字段 %s 不在将要创建的数据上", column)
	}
	got, err := toScalar(raw)
	if err != nil {
		return scalarValue{}, fmt.Errorf("字段 %s 的值无法解析：%s", column, err.Error())
	}
	return got, nil
}

func matchDeptID(record recordValues, column string, deptID uint64) (bool, bool, error) {
	got, err := recordScalar(record, column)
	if err != nil {
		return false, true, err
	}
	if got.kind != scalarNumber {
		return false, true, fmt.Errorf("字段 %s 的值 %v 不是数值，无法与部门 id 比较", column, got.raw)
	}
	return got.num == float64(deptID), true, nil
}

func matchDeptSubtree(record recordValues, column string, uc *UserContext) (bool, bool, error) {
	got, err := recordScalar(record, column)
	if err != nil {
		return false, true, err
	}
	if got.kind != scalarNumber {
		return false, true, fmt.Errorf("字段 %s 的值 %v 不是数值，无法与部门范围比较", column, got.raw)
	}
	ids := uc.DeptSubtreeIDs
	if len(ids) == 0 {
		ids = []uint64{uc.DeptID}
	}
	for _, id := range ids {
		if got.num == float64(id) {
			return true, true, nil
		}
	}
	return false, true, nil
}

type scalarKind int

const (
	scalarNull scalarKind = iota
	scalarNumber
	scalarString
	scalarBool
)

type scalarValue struct {
	kind scalarKind
	num  float64
	str  string
	raw  any
}

func (s scalarValue) text() (string, bool) {
	switch s.kind {
	case scalarString:
		return s.str, true
	case scalarNumber:
		return strconv.FormatFloat(s.num, 'f', -1, 64), true
	case scalarBool:
		return s.str, true
	default:
		return "", false
	}
}

func toScalar(v any) (scalarValue, error) {
	if v == nil {
		return scalarValue{kind: scalarNull}, nil
	}

	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Ptr || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return scalarValue{kind: scalarNull}, nil
		}
		rv = rv.Elem()
	}

	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return scalarValue{kind: scalarNumber, num: float64(rv.Int()), raw: v}, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return scalarValue{kind: scalarNumber, num: float64(rv.Uint()), raw: v}, nil
	case reflect.Float32, reflect.Float64:
		return scalarValue{kind: scalarNumber, num: rv.Float(), raw: v}, nil
	case reflect.String:
		return scalarValue{kind: scalarString, str: rv.String(), raw: v}, nil
	case reflect.Bool:
		return scalarValue{kind: scalarBool, str: strconv.FormatBool(rv.Bool()), raw: v}, nil
	case reflect.Struct:

		if _, isTime := rv.Interface().(time.Time); isTime {
			return scalarValue{}, fmt.Errorf("字段类型 time.Time 的行级写校验尚未实现（字典序与 SQL 时间语义不等价，详见 write.go 的 toScalar 注释）")
		}
		if s, ok := rv.Interface().(fmt.Stringer); ok {
			return scalarValue{kind: scalarString, str: s.String(), raw: v}, nil
		}
	}

	return scalarValue{}, fmt.Errorf("不支持的类型 %s", rv.Type())
}

func scalarCompare(got scalarValue, want string) (int, bool) {
	want = strings.TrimSpace(want)
	switch got.kind {
	case scalarNumber:
		n, err := strconv.ParseFloat(want, 64)
		if err != nil {
			return 0, false
		}
		switch {
		case got.num < n:
			return -1, true
		case got.num > n:
			return 1, true
		default:
			return 0, true
		}
	case scalarString:
		return strings.Compare(got.str, want), true
	case scalarBool:
		b, err := strconv.ParseBool(want)
		if err != nil {
			return 0, false
		}
		return strings.Compare(got.str, strconv.FormatBool(b)), true
	default:
		return 0, false
	}
}

func likeMatch(subject, pattern string) bool {
	s := []rune(subject)
	p := []rune(pattern)

	si, pi := 0, 0
	star, mark := -1, 0
	for si < len(s) {
		switch {
		case pi < len(p) && (p[pi] == '_' || p[pi] == s[si]):
			si++
			pi++
		case pi < len(p) && p[pi] == '%':
			star = pi
			mark = si
			pi++
		case star >= 0:
			pi = star + 1
			mark++
			si = mark
		default:
			return false
		}
	}
	for pi < len(p) && p[pi] == '%' {
		pi++
	}
	return pi == len(p)
}
