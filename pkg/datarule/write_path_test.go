package datarule

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	"gorm.io/gorm"
)

// 写路径单元测试（DryRun + 纯函数；真库用例见 public/test/admin/unit/datarule_write_db_test.go）。
//
// 这里覆盖的是「插件生成了什么 SQL / 给出了什么判定」，不覆盖「库里真的变了没有」——
// 后者只有真库能证明。

// writeProbeRecord 写路径测试用实体。表名与 plugin_test.go 的 protectedRecord 区分开：
// 域按表名匹配，两个测试文件不能抢同一个表名。
type writeProbeRecord struct {
	ID       uint64
	Status   int
	Username string
	DeptID   uint64
}

func (writeProbeRecord) TableName() string { return "protected_record_write" }

// writeProbeDomain 写路径测试域标识。
const writeProbeDomain = "TEST_WRITE"

// newWriteProbeDB 注册写路径测试域并返回挂了插件的 DryRun DB。
func newWriteProbeDB(t *testing.T, provider testRuleProvider) *gorm.DB {
	t.Helper()
	if err := RegisterDomain(DomainConfig{Domain: writeProbeDomain, TableName: "protected_record_write"}); err != nil {
		t.Fatalf("注册测试域失败：%v", err)
	}
	db, err := openTestDB()
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用，跳过写路径测试：%v", err)
	}
	if err = db.Use(NewDataRulePlugin(provider)); err != nil {
		t.Fatalf("注册插件失败：%v", err)
	}
	return db
}

// writeProbeCtx 构造测试用用户上下文：本部门 7，子树 [7,8]（**向下**的可见范围）。
func writeProbeCtx() context.Context {
	return context.WithValue(context.Background(), UserContextKey{},
		&UserContext{UserID: 9, DeptID: 7, DeptSubtreeIDs: []uint64{7, 8}})
}

// whereFragment 截出 DryRun 生成 SQL 的 WHERE 之后部分，并把 pg 的 $1/$2 占位符
// 归一化为 ?，用于比较「三条路径注入的是不是同一份条件构造」。
func whereFragment(sql string) string {
	index := strings.Index(sql, " WHERE ")
	if index < 0 {
		return ""
	}
	fragment := sql[index+len(" WHERE "):]
	if returning := strings.Index(fragment, " RETURNING"); returning >= 0 {
		fragment = fragment[:returning]
	}
	return regexp.MustCompile(`\$\d+`).ReplaceAllString(strings.TrimSpace(fragment), "?")
}

// writeProbeConditions 一组同时含 AND 组合与多类型操作符的行条件。
func writeProbeConditions() []RuleConfig {
	return []RuleConfig{{
		ConditionGroups: []ConditionGroup{{
			Logic: "AND",
			Conditions: []Condition{
				{Field: "status", Op: "IN", Value: "1,2"},
				{Field: "username", Op: "LIKE", Value: "%admin%"},
			},
		}},
	}}
}

// TestUpdateAndDeleteReuseQueryRowConditions 写路径复用 Query 那一枪：
// 同一个域、同一个用户，SELECT / UPDATE / DELETE 三条语句注入的 WHERE **结构逐字相同**
// （占位符序号归一化后比较）。另写一套条件构造会让三条语句慢慢漂移，
// 表现为「列表里看不到、直接调接口却能改」。
func TestUpdateAndDeleteReuseQueryRowConditions(t *testing.T) {
	db := newWriteProbeDB(t, testRuleProvider{rules: writeProbeConditions()})
	ctx := writeProbeCtx()

	selectRes := db.WithContext(ctx).Where("id > ?", 0).Find(&[]writeProbeRecord{})
	updateRes := db.WithContext(ctx).Model(&writeProbeRecord{}).Where("id > ?", 0).
		Updates(map[string]any{"status": 3})
	deleteRes := db.WithContext(ctx).Where("id > ?", 0).Delete(&writeProbeRecord{})

	sqls := map[string]string{
		"SELECT": selectRes.Statement.SQL.String(),
		"UPDATE": updateRes.Statement.SQL.String(),
		"DELETE": deleteRes.Statement.SQL.String(),
	}
	fragments := make(map[string]string, len(sqls))
	for name, sql := range sqls {
		fragments[name] = whereFragment(sql)
	}
	base := fragments["SELECT"]
	if base == "" || !strings.Contains(base, "status") {
		t.Fatalf("SELECT 未注入行级条件：%s", sqls["SELECT"])
	}
	for name, fragment := range fragments {
		if fragment != base {
			t.Fatalf("%s 注入的条件与 SELECT 不一致（当前 %s / 基准 %s）", name, fragment, base)
		}
	}
	if !strings.Contains(base, `"status" IN (?,?)`) || !strings.Contains(base, `"username" LIKE ?`) {
		t.Fatalf("注入的 WHERE 结构不符：%s", base)
	}

	// 三条语句绑定的规则参数也必须一致（UPDATE 的 SET 值排在前面，所以只检查规则参数是否都在）。
	results := map[string]*gorm.DB{"SELECT": selectRes, "UPDATE": updateRes, "DELETE": deleteRes}
	for name, res := range results {
		for _, want := range []any{"1", "2", "%admin%"} {
			found := false
			for _, got := range res.Statement.Vars {
				if got == want {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("%s 未绑定规则参数 %v：%#v", name, want, res.Statement.Vars)
			}
		}
	}
}

// TestUpdateAndDeleteWithoutWhereAreNotRejected 确认 gorm 的 ErrMissingWhereClause 检查
// 发生在 gorm:update / gorm:delete **主回调内部**、在插件的 Before **之后**：
// 条件注入完成后，不带 WHERE 的写语句不会被误判为「无条件全表写」；
// 而插件不介入（没有 UserContext）时 gorm 的原判定照旧 —— 反证挡住误判的就是注入的条件。
func TestUpdateAndDeleteWithoutWhereAreNotRejected(t *testing.T) {
	db := newWriteProbeDB(t, testRuleProvider{rules: []RuleConfig{{
		ConditionGroups: []ConditionGroup{{
			Logic:      "AND",
			Conditions: []Condition{{Field: "status", Op: "EQ", Value: "1"}},
		}},
	}}})

	updateRes := db.WithContext(writeProbeCtx()).Model(&writeProbeRecord{}).Updates(map[string]any{"status": 2})
	if updateRes.Error != nil {
		t.Fatalf("有规则时，不带 WHERE 的 Update 应当被允许（条件已由插件注入），实际：%v", updateRes.Error)
	}
	if !strings.Contains(updateRes.Statement.SQL.String(), "WHERE") {
		t.Fatalf("Update 语句里应当有注入的条件：%s", updateRes.Statement.SQL.String())
	}

	deleteRes := db.WithContext(writeProbeCtx()).Model(&writeProbeRecord{}).Delete(&writeProbeRecord{})
	if deleteRes.Error != nil {
		t.Fatalf("有规则时，不带 WHERE 的 Delete 应当被允许（条件已由插件注入），实际：%v", deleteRes.Error)
	}
	if !strings.Contains(deleteRes.Statement.SQL.String(), "WHERE") {
		t.Fatalf("Delete 语句里应当有注入的条件：%s", deleteRes.Statement.SQL.String())
	}

	// 反证：插件不介入时 gorm 的原判定照旧。
	plain := db.Model(&writeProbeRecord{}).Updates(map[string]any{"status": 2})
	if !errors.Is(plain.Error, gorm.ErrMissingWhereClause) {
		t.Fatalf("无 UserContext 时应当由 gorm 判定缺少 WHERE，实际：%v", plain.Error)
	}
	plainDelete := db.Model(&writeProbeRecord{}).Delete(&writeProbeRecord{})
	if !errors.Is(plainDelete.Error, gorm.ErrMissingWhereClause) {
		t.Fatalf("无 UserContext 的 Delete 应当由 gorm 判定缺少 WHERE，实际：%v", plainDelete.Error)
	}
}

// TestUpdateOmitFieldsRemovedFromSetClause 字段级写保护：被屏蔽的列在 SET 子句里根本不出现；
// 同一份 OmitFields 在 SELECT 语义下仍然是「不查该列」。
func TestUpdateOmitFieldsRemovedFromSetClause(t *testing.T) {
	db := newWriteProbeDB(t, testRuleProvider{rules: []RuleConfig{{OmitFields: []string{"username"}}}})
	ctx := writeProbeCtx()

	res := db.WithContext(ctx).Model(&writeProbeRecord{}).Where("id = ?", 1).
		Updates(map[string]any{"status": 2, "username": "hacked"})
	if res.Error != nil {
		t.Fatalf("带 WHERE 的 Update 不应报错：%v", res.Error)
	}
	updateSQL := res.Statement.SQL.String()
	if strings.Contains(updateSQL, "username") {
		t.Fatalf("被屏蔽的列不应出现在 UPDATE 语句里：%s", updateSQL)
	}
	if !strings.Contains(updateSQL, `SET "status"`) {
		t.Fatalf("未被屏蔽的列应当照常更新：%s", updateSQL)
	}

	readSQL := db.WithContext(ctx).Find(&[]writeProbeRecord{}).Statement.SQL.String()
	if strings.Contains(readSQL, "username") {
		t.Fatalf("同一份 OmitFields 在 SELECT 语义下也应屏蔽该列：%s", readSQL)
	}
	if !strings.Contains(readSQL, `"dept_id"`) {
		t.Fatalf("SELECT 应当逐列列出未被屏蔽的字段：%s", readSQL)
	}
}

// TestCreateValidatesDestValues Create 值校验：命中放行、越界拒绝、批量逐元素校验、
// 取值失败 fail-closed、无 UserContext 时不介入。
func TestCreateValidatesDestValues(t *testing.T) {
	db := newWriteProbeDB(t, testRuleProvider{rules: []RuleConfig{{
		ConditionGroups: []ConditionGroup{{
			Logic:      "AND",
			Conditions: []Condition{{Field: "dept_id", Op: "EQ", Value: "dept.scope:SELF_AND_CHILDREN"}},
		}},
	}}})
	ctx := writeProbeCtx() // DeptID=7，子树 [7,8]

	if err := db.WithContext(ctx).Create(&writeProbeRecord{DeptID: 7}).Error; err != nil {
		t.Fatalf("本部门应当放行：%v", err)
	}
	if err := db.WithContext(ctx).Create(&writeProbeRecord{DeptID: 8}).Error; err != nil {
		t.Fatalf("子树内的子部门应当放行：%v", err)
	}
	if err := db.WithContext(ctx).Create(&writeProbeRecord{DeptID: 9}).Error; err == nil ||
		!strings.Contains(err.Error(), "数据权限拒绝") {
		t.Fatalf("不在可见范围内应当被拒绝，实际：%v", err)
	}

	batchErr := db.WithContext(ctx).Create(&[]*writeProbeRecord{{DeptID: 7}, {DeptID: 9}}).Error
	if batchErr == nil || !strings.Contains(batchErr.Error(), "第 2 条") {
		t.Fatalf("批量创建应逐元素校验并指出越界的那一条，实际：%v", batchErr)
	}

	// fail-closed：条件字段在 Dest 上取不到值（map 里没有 dept_id）。
	mapErr := db.WithContext(ctx).Model(&writeProbeRecord{}).Create(map[string]any{"username": "x"}).Error
	if mapErr == nil || !strings.Contains(mapErr.Error(), "dept_id") {
		t.Fatalf("条件字段取不到时必须 fail-closed 拒绝，实际：%v", mapErr)
	}

	// 无 UserContext：插件完全不介入（既有语义）。
	if err := db.Create(&writeProbeRecord{DeptID: 9}).Error; err != nil {
		t.Fatalf("无 UserContext 时 Create 不应被数据权限拦截：%v", err)
	}
}

// TestCreateDeptScopeSelfUsesOwnDeptOnly dept.scope:SELF 只看**本部门**（不是子树）。
func TestCreateDeptScopeSelfUsesOwnDeptOnly(t *testing.T) {
	db := newWriteProbeDB(t, testRuleProvider{rules: []RuleConfig{{
		ConditionGroups: []ConditionGroup{{
			Logic:      "AND",
			Conditions: []Condition{{Field: "dept_id", Op: "EQ", Value: "dept.scope:SELF"}},
		}},
	}}})
	ctx := writeProbeCtx()

	if err := db.WithContext(ctx).Create(&writeProbeRecord{DeptID: 7}).Error; err != nil {
		t.Fatalf("本部门应当放行：%v", err)
	}
	if err := db.WithContext(ctx).Create(&writeProbeRecord{DeptID: 8}).Error; err == nil {
		t.Fatal("SELF 只看本部门，子部门 8 应当被拒绝")
	}
}

// TestCreateWithNoConditionGroupsPasses 规则只有字段屏蔽、没有行级条件时，Create 无需取值。
func TestCreateWithNoConditionGroupsPasses(t *testing.T) {
	db := newWriteProbeDB(t, testRuleProvider{rules: []RuleConfig{{OmitFields: []string{"username"}}}})
	if err := db.WithContext(writeProbeCtx()).Create(&writeProbeRecord{DeptID: 999}).Error; err != nil {
		t.Fatalf("没有行级条件时 Create 不应被拦截：%v", err)
	}
}

// TestCreateRecordsRejectsUnsupportedShapes Dest 形状不认识时必须报错（fail-closed）。
func TestCreateRecordsRejectsUnsupportedShapes(t *testing.T) {
	for name, dest := range map[string]any{
		"nil":           nil,
		"nil 指针":        (*writeProbeRecord)(nil),
		"数字":            42,
		"字符串":           "x",
		"元素非结构体的切片":     []int{1, 2},
		"切片里含 nil 指针元素": []*writeProbeRecord{nil},
		"键不是字符串的 map":   map[int]any{1: "x"},
	} {
		if _, err := createRecords(dest, nil); err == nil {
			t.Fatalf("%s：形状不支持时必须报错（fail-closed）", name)
		}
	}

	records, err := createRecords(&[]*writeProbeRecord{{DeptID: 7, Username: "a"}, {DeptID: 8}}, nil)
	if err != nil || len(records) != 2 {
		t.Fatalf("结构体切片应当支持：records=%v err=%v", records, err)
	}
	if records[0]["dept_id"] != uint64(7) || records[1]["status"] != int(0) {
		t.Fatalf("字段快照不符：%+v", records)
	}

	mapped, err := createRecords(map[string]any{"username": "x"}, nil)
	if err != nil || len(mapped) != 1 || mapped[0]["username"] != "x" {
		t.Fatalf("map 形状应当支持：%+v err=%v", mapped, err)
	}

	empty, err := createRecords([]writeProbeRecord{}, nil)
	if err != nil || len(empty) != 0 {
		t.Fatalf("空切片应当放行：%+v err=%v", empty, err)
	}
}

// TestEvalConditionMirrorsBuildCondition 值求值与 SQL 条件构造必须同进同退：
// buildCondition 认为「这条条件不产生任何约束」时，evalCondition 也必须判它不可执行。
// 两条路一旦分叉，就会出现「一条不限制读的规则却挡住了写」这种两套语义。
func TestEvalConditionMirrorsBuildCondition(t *testing.T) {
	uc := &UserContext{DeptID: 7, DeptSubtreeIDs: []uint64{7, 8}}
	record := recordValues{"status": int(1), "username": "admin", "dept_id": uint64(7)}

	conditions := []Condition{
		{Field: "status", Op: "EQ", Value: "1"},
		{Field: "status", Op: "NEQ", Value: "2"},
		{Field: "status", Op: "GT", Value: "0"},
		{Field: "status", Op: "GTE", Value: "1"},
		{Field: "status", Op: "LT", Value: "2"},
		{Field: "status", Op: "LTE", Value: "1"},
		{Field: "username", Op: "LIKE", Value: "%adm%"},
		{Field: "username", Op: "NOT_LIKE", Value: "%xyz%"},
		{Field: "status", Op: "IN", Value: "1,2"},
		{Field: "status", Op: "NOT_IN", Value: "3,4"},
		{Field: "status", Op: "BETWEEN", Value: "1,3"},
		{Field: "dept_id", Op: "EQ", Value: "dept.scope:SELF"},
		{Field: "dept_id", Op: "EQ", Value: "dept.scope:SELF_AND_CHILDREN"},
		{Field: "status;DROP TABLE sys_admin", Op: "EQ", Value: "1"},
		{Field: "status", Op: "CONTAINS", Value: "1"},
		{Field: "status", Op: "IN", Value: " , "},
		{Field: "status", Op: "BETWEEN", Value: "1"},
		{Field: "dept_id", Op: "EQ", Value: "dept.scope:ALL"},
		{Field: "dept_id", Op: "EQ", Value: "dept.scope:CUSTOM:1,2"},
	}
	for _, condition := range conditions {
		_, buildOK := buildCondition(condition, uc, "postgres")
		_, enforceable, err := evalCondition(record, condition, uc)
		if err != nil {
			t.Fatalf("条件 %+v 不应取值失败：%v", condition, err)
		}
		if buildOK != enforceable {
			t.Fatalf("条件 %+v 两套语义不一致：buildCondition=%v evalCondition=%v", condition, buildOK, enforceable)
		}
	}

	// 取值失败是**预期内的分歧**：SQL 路只认列名合法，值求值路要求 Dest 上真的能取到该列。
	if _, enforceable, err := evalCondition(recordValues{}, Condition{Field: "status", Op: "EQ", Value: "1"}, uc); !enforceable || err == nil {
		t.Fatalf("字段取不到时必须 fail-closed（enforceable=true 且 err!=nil），实际 enforceable=%v err=%v", enforceable, err)
	}
}

// TestEvalGroupLogic 组内 AND/OR 与「组内无可执行条件时不产生约束」。
func TestEvalGroupLogic(t *testing.T) {
	uc := &UserContext{DeptID: 7}
	record := recordValues{"status": int(5), "username": "admin"}

	cases := []struct {
		name  string
		group ConditionGroup
		want  bool
	}{
		{"AND 全部命中", ConditionGroup{Logic: "AND", Conditions: []Condition{
			{Field: "status", Op: "GTE", Value: "5"}, {Field: "username", Op: "LIKE", Value: "%adm%"}}}, true},
		{"AND 有一条不命中", ConditionGroup{Logic: "AND", Conditions: []Condition{
			{Field: "status", Op: "GTE", Value: "5"}, {Field: "username", Op: "EQ", Value: "other"}}}, false},
		{"OR 有一条命中", ConditionGroup{Logic: "OR", Conditions: []Condition{
			{Field: "status", Op: "EQ", Value: "1"}, {Field: "username", Op: "LIKE", Value: "%adm%"}}}, true},
		{"OR 全不命中", ConditionGroup{Logic: "OR", Conditions: []Condition{
			{Field: "status", Op: "EQ", Value: "1"}, {Field: "username", Op: "EQ", Value: "other"}}}, false},
		{"组内条件全不可执行则不产生约束", ConditionGroup{Logic: "AND", Conditions: []Condition{
			{Field: "status", Op: "UNKNOWN", Value: "1"}}}, true},
		{"空条件组不产生约束", ConditionGroup{Logic: "AND"}, true},
	}
	for _, c := range cases {
		got, err := evalGroup(record, c.group, uc)
		if err != nil {
			t.Fatalf("%s：不应报错：%v", c.name, err)
		}
		if got != c.want {
			t.Fatalf("%s：期望 %v 实际 %v", c.name, c.want, got)
		}
	}

	if _, err := evalGroup(recordValues{"status": int(1)}, ConditionGroup{Logic: "AND", Conditions: []Condition{
		{Field: "dept_id", Op: "EQ", Value: "1"}}}, uc); err == nil {
		t.Fatal("组内条件取值失败必须冒泡为错误（fail-closed）")
	}
}

// TestLikeMatch SQL LIKE 的通配语义（% 任意长度、_ 单字符，其余按字面）。
func TestLikeMatch(t *testing.T) {
	cases := []struct {
		subject string
		pattern string
		want    bool
	}{
		{"admin", "%adm%", true},
		{"admin", "adm%", true},
		{"admin", "%min", true},
		{"admin", "admin", true},
		{"admin", "adm_n", true},
		{"admin", "a%n", true},
		{"admin", "%xyz%", false},
		{"admin", "admin_", false},
		{"admin", "_dmin", true},
		{"admin", "admin%admin", false},
		{"", "%", true},
		{"", "_", false},
		{"a%b", "%", true},
	}
	for _, c := range cases {
		if got := likeMatch(c.subject, c.pattern); got != c.want {
			t.Fatalf("likeMatch(%q, %q) 期望 %v 实际 %v", c.subject, c.pattern, c.want, got)
		}
	}
}

// TestToScalar 标量归一化：nil/指针解引用/数值/字符串，以及不认识类型的 fail-closed。
func TestToScalar(t *testing.T) {
	if got, err := toScalar(nil); err != nil || got.kind != scalarNull {
		t.Fatalf("nil 应归一化为 null：%+v err=%v", got, err)
	}
	var nilText *string
	if got, err := toScalar(nilText); err != nil || got.kind != scalarNull {
		t.Fatalf("nil 指针应归一化为 null：%+v err=%v", got, err)
	}
	text := "x"
	if got, err := toScalar(&text); err != nil || got.kind != scalarString || got.str != "x" {
		t.Fatalf("*string 应解引用为字符串：%+v err=%v", got, err)
	}
	if got, err := toScalar(uint64(9)); err != nil || got.kind != scalarNumber || got.num != 9 {
		t.Fatalf("uint64 应归一化为数值：%+v err=%v", got, err)
	}
	if got, err := toScalar(int32(-3)); err != nil || got.num != -3 {
		t.Fatalf("int32 应归一化为数值：%+v err=%v", got, err)
	}
	if _, err := toScalar(struct{ A int }{A: 1}); err == nil {
		t.Fatal("不认识的结构体类型必须 fail-closed 报错")
	}
	if _, err := toScalar([]byte("x")); err == nil {
		t.Fatal("[]byte 类型必须 fail-closed 报错")
	}
}

// TestScopeAppliedOnlyWhenRulesResolved 只有真的解析出规则时才标记「插件已接管」；
// 未注册域 / 无规则 / 没有 UserContext / provider 报错都不标记，
// After 阶段据此不做 0 行判定（避免把与权限无关的空结果改写成权限错误）。
func TestScopeAppliedOnlyWhenRulesResolved(t *testing.T) {
	db := newWriteProbeDB(t, testRuleProvider{rules: writeProbeConditions()})

	withRules := db.WithContext(writeProbeCtx()).Model(&writeProbeRecord{}).Where("id = ?", 1).
		Updates(map[string]any{"status": 1})
	if !scopeApplied(withRules) {
		t.Fatal("解析出规则后应当标记已接管")
	}

	unregistered := db.WithContext(writeProbeCtx()).Table("not_registered_table").
		Where("id = ?", 1).Updates(map[string]any{"status": 1})
	if scopeApplied(unregistered) {
		t.Fatal("未注册数据域的表不应被标记为已接管")
	}

	noUserCtx := db.Model(&writeProbeRecord{}).Where("id = ?", 1).Updates(map[string]any{"status": 1})
	if scopeApplied(noUserCtx) {
		t.Fatal("没有 UserContext 时不应被标记为已接管")
	}

	providerErr := errors.New("规则查询失败")
	failedDB := newWriteProbeDB(t, testRuleProvider{err: providerErr})
	failedTx := failedDB.WithContext(writeProbeCtx()).Model(&writeProbeRecord{}).Where("id = ?", 1).
		Updates(map[string]any{"status": 1})
	if !errors.Is(failedTx.Error, providerErr) {
		t.Fatalf("provider 报错时应当 fail-closed 终止写路径，实际：%v", failedTx.Error)
	}
	if scopeApplied(failedTx) {
		t.Fatal("provider 报错不应标记已接管")
	}
}
