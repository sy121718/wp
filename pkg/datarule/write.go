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

// ── 写路径：Update / Delete 的行级保护 ──────────────────────────────────────

// beforeUpdate Update 的 Before 回调，注入**与 Query 完全相同**的行级条件与字段屏蔽。
//
// 为什么不另写一套：行级条件的语义（AND/OR 组合、dept.scope 的方向、方言引用符）在
// buildConditions / deptScopeCondition 里只有一份实现，Update 通过复用
// resolveRuleScope + applyRowConditions 直接拿到同一份结果。另写一套的后果是两套语义
// 慢慢漂移，表现为「列表里看不到、直接调接口却能改」—— 那正是插件只挂 Query 时的缺口。
//
// 字段级写保护复用同一份 OmitFields：GORM 在 UPDATE 语义下把 Omits 解释为
// 「不更新该列」（见 applyOmitFields 的说明）。DELETE 没有字段概念，所以 beforeDelete 不注入。
func (p *DataRulePlugin) beforeUpdate(db *gorm.DB) {
	scope, ok := p.resolveRuleScope(db)
	if !ok {
		return
	}
	applyOmitFields(db, scope.merged)
	applyRowConditions(db, scope.merged, scope.user)
}

// beforeDelete Delete 的 Before 回调，注入与 Query 完全相同的行级条件。
func (p *DataRulePlugin) beforeDelete(db *gorm.DB) {
	scope, ok := p.resolveRuleScope(db)
	if !ok {
		return
	}
	applyRowConditions(db, scope.merged, scope.user)
}

// afterUpdate Update 执行后的 0 行判定。
func (p *DataRulePlugin) afterUpdate(db *gorm.DB) {
	rejectMissingAffectedRows(db, "更新")
}

// afterDelete Delete 执行后的 0 行判定。
func (p *DataRulePlugin) afterDelete(db *gorm.DB) {
	rejectMissingAffectedRows(db, "删除")
}

// affectedRows 返回本语句的影响行数。
//
// gorm.Statement 内嵌 *DB，所以 db.Statement.RowsAffected 实际读的是
// Statement.DB.RowsAffected；gorm 的 Session(&Session{Initialized:true}) 会把 Statement.DB
// 重新绑定到新实例（Save 的 fallthrough 走的正是这条路），因此这条路与 db.RowsAffected
// 在回调内**始终指向同一个 DB 实例**。Statement 缺失时回退到回调拿到的 db ——
// 不为一行计数把语句打成 panic。
func affectedRows(db *gorm.DB) int64 {
	if db.Statement != nil && db.Statement.DB != nil {
		return db.Statement.RowsAffected
	}
	return db.RowsAffected
}

// rejectMissingAffectedRows 是 Update / Delete 共用的 0 行判定。
//
// 判定成立时 AddError 返回明确的中文错误，例如：
//
//	数据权限拒绝：没有可更新的行（可能不存在或不在你的可见范围内）
//
// **0 行拒绝是安全的**：行级 WHERE 已经写在 UPDATE/DELETE 语句里，没权限的行根本不参与
// 匹配，所以即使报错发生在语句执行之后，也不可能改错行 —— 报错只是如实告诉调用方
// 「什么都没改成」。**因此这里不需要事务**：没有半截状态需要回滚，加事务只会让一次
// 只读判定多占一条连接（gorm 默认事务回滚的结果也一样，0 行本来就什么都没改）。
//
// **方言边界（别把它当成通用结论）**：
//   - PostgreSQL 的 UPDATE 计数的是**匹配行数** —— 值没变化也算 1，所以
//     「0 行 = 无权限或不存在」在本项目主库（PostgreSQL）上成立；
//   - MySQL 的 affected_rows 只计数**实际改变**的行，同一条语句在 MySQL 上对
//     「值未变化」的合法更新会返回 0，这里会**误报**。MySQL 只是历史兼容，这是已知代价。
//
// 三个前提：
//  1. DryRun 下语句根本没有执行，RowsAffected 恒为 0，此时报错只会凭空制造失败；
//     DryRun 本身不写库，没有需要保护的对象。
//  2. 插件必须**参与过**这条语句（见 appliedScopeKey）：未注册数据域的表、对该域
//     没有任何规则的用户，插件从没介入过，此时 0 行与权限无关 —— 报「数据权限拒绝」
//     是错的，还会把全站「更新/删除一个不存在的 id」的正常业务错误改写成权限错误。
//  3. 「SET 子句里一列都不剩」也会走到这里：被屏蔽的列是 Update 目标里唯一的列时，
//     gorm 的 ConvertToAssignments 返回空集合，gorm:update 直接提前返回、不做 UPDATE。
//     此时报错同样是如实的（一个字节都没改），只是文案说的是行而不是列。
func rejectMissingAffectedRows(db *gorm.DB, action string) {
	if db.Error != nil {
		return // 语句本身已经失败了，不要覆盖原始错误
	}
	if db.DryRun || !scopeApplied(db) {
		return
	}
	if affectedRows(db) != 0 {
		return
	}
	db.AddError(fmt.Errorf("数据权限拒绝：没有可%s的行（可能不存在或不在你的可见范围内）", action))
}

// ── 写路径：Create 的行级校验 ──────────────────────────────────────────────

// beforeCreate Create 的 Before 回调：校验「将要创建的对象」是否落在该用户的行级范围内。
//
// **这是行为变更**：插件此前只挂 Query，Create 完全不受数据权限约束；此后受约束。
//
// 为什么不是注入 WHERE：Create 的目标行还不存在，没有可用的行条件。所以正确语义是
// CASL 那种「检查将要创建的对象」—— 从 db.Statement.Dest 反射取值，与合并后的条件组
// 逐条比对，不满足即拒绝落库。
//
// **fail-closed**：条件涉及的字段在 Dest 上取不到值（字段不存在 / 类型不认识 /
// Dest 形状不支持）一律 AddError 拒绝，绝不静默放过 ——「看起来加了保护、实际什么都没拦」
// 比不做更危险。批量创建（结构体切片，元素可以是结构体或结构体指针）逐元素校验，
// 任何一条不满足都拒绝整批（此时语句还没执行，AddError 让 gorm 回滚该事务）。
func (p *DataRulePlugin) beforeCreate(db *gorm.DB) {
	scope, ok := p.resolveRuleScope(db)
	if !ok {
		return
	}
	if len(scope.merged.ConditionGroups) == 0 {
		return // 只有字段屏蔽、没有行级条件，Create 无需取值
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

// recordValues 一条待创建记录的字段快照：数据库列名 → 字段值。
type recordValues map[string]any

// createRecords 把 Create 的 Dest 拆成一组字段快照。
//
// 支持的结构形状（都是 gorm 的 Create 实际接受的形状）：
//   - 结构体 / 指向结构体的指针（单条）；
//   - 结构体切片 / 数组，元素可以是结构体或结构体指针（批量，逐元素校验）；
//   - map[string]any（键按 Statement.Schema 归一化为数据库列名）。
//
// 之外的形状（数字、字符串、nil 指针、键不是字符串的 map、元素不是结构体的切片……）
// 一律返回 error，调用方据此 fail-closed 拒绝 —— 不允许「形状不认识就放行」。
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
		// 空切片：没有要写的行（gorm 也不会产生语句），放行。
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
			// gorm 接受 Go 字段名与数据库列名两种键，这里统一归一化为列名。
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

// structRecord 把结构体转成「数据库列名 → 值」的快照。
// 列名走与数据域白名单**同一份**推导（columnNameOf：gorm column 标签优先，缺省按命名策略），
// 否则白名单里的列名与这里的取值键会各说各话。
func structRecord(rv reflect.Value) recordValues {
	record := make(recordValues, rv.NumField())
	collectStructFields(rv, record)
	return record
}

// collectStructFields 递归收集结构体字段（含匿名内嵌结构体，与 gorm 的扁平化一致）。
// 只收可导出的字段；列名为 "-" 的字段跳过（它不可能出现在白名单里）。
func collectStructFields(rv reflect.Value, record recordValues) {
	naming := schema.NamingStrategy{}
	typ := rv.Type()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.PkgPath != "" && !field.Anonymous {
			continue // 未导出字段
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

// ── 值求值：把条件组套在一条待创建记录上 ────────────────────────────────────

// evalGroup 按组内 Logic（AND/OR）对一条记录求值。
// 与 Query 路共用同一份「哪些条件不产生约束」的判定：不可执行的条件一律跳过
// （见 evalCondition 的说明）；组内一条可执行条件都不剩时，本组不产生约束（返回 true）。
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
		return true, nil // 本组不产生约束
	}
	// AND：全部命中（中途没返回 false）；OR：无一命中（命中会提前返回 true）。
	return logic == LogicAnd, nil
}

// evalCondition 在一条待创建记录上求值单条条件。
//
// 返回值：
//   - enforceable=false：这条条件在引擎里本来就不产生任何约束，调用方应当**跳过**它。
//     判定与 Query 路的 buildCondition 完全对齐（非法字段名、不支持的操作符、
//     dept.scope:ALL/CUSTOM、IN/NOT_IN 值为空、BETWEEN 参数个数不是 2）—— 否则会出现
//     「一条不限制读的规则却挡住了写」这种两套语义各说各话的情况；
//   - err != nil：条件涉及的字段在记录上取不到值，或值类型不认识 / 不可比较 ——
//     调用方必须 fail-closed 拒绝；
//   - 否则 matched 表示记录是否满足这条条件。
//
// dept.scope:* 的求值方向与 Query 路完全一致：SELF 用 uc.DeptID，SELF_AND_CHILDREN 用
// uc.DeptSubtreeIDs（**向下**的可见范围：本部门 + 全部子孙，见 types.go 的方向语义说明），
// 并且与 Query 路一样**忽略操作符**（该引用表达式自带比较语义）。
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
		default: // ALL / CUSTOM：Query 路不产生约束，这里同样跳过
			return false, false, nil
		}
	}

	got, scalarErr := recordScalar(record, fieldName)
	if scalarErr != nil {
		return false, true, scalarErr
	}
	// SQL 语义：NULL 参与任何比较的结果都是 NULL（不命中），所以一律不满足。
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
		default: // LTE
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

// recordScalar 从记录快照里取出某个列的标量值。
// 取不到（列不在快照里）返回 error —— 这是 fail-closed 的主要落点。
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

// matchDeptID 校验记录的部门列是否等于给定部门 id（dept.scope:SELF）。
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

// matchDeptSubtree 校验记录的部门列是否落在「本部门及全部子部门」内。
//
// 方向是**向下**的：来源是 UserContext.DeptSubtreeIDs（本部门 + 全部子孙），不是祖先链 ——
// 拿祖先链当可见范围会把范围放大到上级部门，方向相反。
// 子树未知（未注入部门解析器 / 部门为 0）时退化为**只含本部门**：比真实可见范围更窄，
// 方向安全 —— 宁可多拒一条，也不因为「子树没算出来」而静默放过一条。
//
// **与 Query 路的已知差异（唯一一处读写不等价，别在别处「对齐」掉它）**：
// DeptSubtreeIDs 为空时，Query 路会回退到 sys_dept 子查询、仍能算出真实子树，
// 而这里退化为本部门。于是同一份规则下可能出现「列表里看得见子部门的数据，
// 却建不出一行子部门的记录」。这是**有意的取舍**：读侧算宽了只是多显示，
// 写侧算宽了就是放行 —— 宁可让写侧更窄并在报错里说清，也不要为了读写对称，
// 把一个「还没算出来」的范围当成可见范围。
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

// ── 标量归一化与比较 ────────────────────────────────────────────────────────

type scalarKind int

const (
	scalarNull scalarKind = iota
	scalarNumber
	scalarString
	scalarBool
)

// scalarValue 记录值归一化后的可比较形式。
type scalarValue struct {
	kind scalarKind
	num  float64
	str  string
	raw  any
}

// text 返回标量的文本形式，供 LIKE 使用。
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

// toScalar 把记录上的字段值归一化为标量。
// 支持：整数 / 无符号整数 / 浮点 / 字符串 / 布尔 / time.Time（RFC3339）/
// 其它实现 fmt.Stringer 的类型；指针与接口自动解引用，nil 归一化为 scalarNull。
// 其余类型一律返回 error —— 类型不认识时 fail-closed，不做「猜一个字符串」的兜底。
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
		// 时间列**刻意不支持**（fail-closed，不是遗漏）。
		//
		// 原因不是「懒得写」，而是「字符串化之后是错的」：SQL 里的时间比较是时间语义，
		// 而归一化成字符串后 scalarCompare 走字典序，两者不等价，且**错误方向是放行**：
		// 字段值 2026-09-19T01:00:00+08:00 与条件 GT 2026-09-19 20:00:00 相比，
		// 字典序拿 'T'(0x54) 与 ' '(0x20) 比就判「命中」，而真实时间远早于条件 ——
		// 一个「只允许创建某时刻之后的数据」的规则会静默放行该时刻之前的数据。
		//
		// 当前四个数据域的白名单里没有任何时间列（只有 dept_id / username / email /
		// phone / status），所以这条路径走不到。将来要支持：把条件值解析成 time.Time
		// 做真比较（utils 里有可复用的布局），**不要退回字符串字典序**。
		if _, isTime := rv.Interface().(time.Time); isTime {
			return scalarValue{}, fmt.Errorf("字段类型 time.Time 的行级写校验尚未实现（字典序与 SQL 时间语义不等价，详见 write.go 的 toScalar 注释）")
		}
		if s, ok := rv.Interface().(fmt.Stringer); ok {
			return scalarValue{kind: scalarString, str: s.String(), raw: v}, nil
		}
	}

	return scalarValue{}, fmt.Errorf("不支持的类型 %s", rv.Type())
}

// scalarCompare 把记录值与条件里的字符串值做比较，返回 -1 / 0 / 1。
// 数值字段按数值比较；字符串字段按字典序（与 SQL 里 varchar 列的比较一致）；
// 布尔字段接受 true/false 与 1/0。ok=false 表示两侧不可比（调用方 fail-closed）。
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

// likeMatch 实现 SQL LIKE 的通配语义：% 匹配任意长度、_ 匹配单个字符，其余字符按字面匹配。
// 只处理这两个通配符，不引入方言差异（PG 的 LIKE 默认没有转义字符，MySQL 默认反斜杠转义）——
// 规则里的值是后台表单填的，不期望含转义序列。
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
