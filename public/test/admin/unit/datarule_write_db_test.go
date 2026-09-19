package unit

// datarule_write_db_test.go —— 数据权限**写路径**的真库用例。
//
// 单元用例（pkg/datarule/write_path_test.go）只能证明「插件生成了什么 SQL」；
// 这里证明「库里真的没被改 / 只改了可见行」—— 这个结论只有真库能给。
//
// 覆盖：
//  1. Update / Delete 的行级写保护：越界行报错，且**数据一行都没动**；
//  2. 有规则时**不带 WHERE** 的 Update 被允许，且只影响可见行（gorm 的
//     ErrMissingWhereClause 检查在 gorm:update / gorm:delete 主回调内部、在插件 Before 之后）；
//  3. Update 的字段级写保护：被屏蔽的列改完读回原值未变；
//  4. Create 的行级校验：命中放行、越界拒绝、批量整批拒绝；
//  5. Create 的 fail-closed：条件字段在 Dest 上取不到时拒绝；
//  6. 插件没参与时不做 0 行判定（与权限无关的空结果不会被改写成权限错误）。

import (
	"context"
	"errors"
	"strings"
	"testing"

	admindto "go_wp/internal/module/admin/dto"
	adminmodel "go_wp/internal/module/admin/model"
	"go_wp/pkg/datarule"

	"gorm.io/gorm"
)

// writeCaseEnv 写路径真库用例环境：规则已分配、插件已挂、两行部门不同的探针数据已就绪。
type writeCaseEnv struct {
	env       *env
	userCtx   context.Context
	visible   uint64 // dept_id = 1，落在规则可见范围内
	invisible uint64 // dept_id = 2，不在可见范围内
}

// deptOneRuleConfig 只允许部门 1 的行可见的规则配置。
func deptOneRuleConfig() admindto.RuleConfigDTO {
	return admindto.RuleConfigDTO{
		ConditionGroups: []admindto.RuleConditionGroupDTO{{
			Logic:      "AND",
			Conditions: []admindto.RuleConditionDTO{{Field: "dept_id", Op: "EQ", Value: "1"}},
		}},
	}
}

// setupWriteCase 用默认规则（部门 1 可见）与默认用户上下文（UserID=100）装配环境。
func setupWriteCase(t *testing.T) *writeCaseEnv {
	t.Helper()
	return setupWriteCaseWith(t, deptOneRuleConfig(), &datarule.UserContext{UserID: 100})
}

// setupWriteCaseWith 装配写路径真库环境。
//
// 顺序很关键：规则创建 / 分配、以及两行探针数据都必须**在挂插件之前**完成 ——
// 挂上插件后 Create 也会被校验，dept.scope:SELF 这类规则会把准备数据的那一步自己拦掉。
func setupWriteCaseWith(t *testing.T, config admindto.RuleConfigDTO, uc *datarule.UserContext) *writeCaseEnv {
	t.Helper()
	e := setupEnv(t)
	ctx := context.Background()

	ruleID := ruleCreate(t, e, "写路径规则"+uniq(""), "ADMIN", adminmodel.RuleStatusEnabled, config)
	if err := e.svc.RuleAssignmentSave(ctx, ruleAssignmentForUser(ruleID, uc.UserID)); err != nil {
		t.Fatalf("分配规则失败：%v", err)
	}

	visible := createWriteProbeAdmin(t, e, 1, uniq("vis"))
	invisible := createWriteProbeAdmin(t, e, 2, uniq("inv"))

	datarule.SetProvider(e.svc)
	if err := datarule.RegisterPluginWithDB(e.db); err != nil {
		t.Fatalf("注册 datarule 插件失败：%v", err)
	}

	return &writeCaseEnv{
		env:       e,
		userCtx:   context.WithValue(ctx, datarule.UserContextKey{}, uc),
		visible:   visible,
		invisible: invisible,
	}
}

// createWriteProbeAdmin 直接落库一个指定部门的管理员（绕过 service，避免受权限影响）。
func createWriteProbeAdmin(t *testing.T, e *env, deptID uint64, username string) uint64 {
	t.Helper()
	email := username + "@example.com"
	row := &adminmodel.AdminEntity{
		Username: username,
		Password: "probe-password",
		Email:    &email,
		DeptID:   deptID,
		Status:   adminmodel.AdminStatusActive,
	}
	if err := e.db.Create(row).Error; err != nil {
		t.Fatalf("准备管理员行失败：%v", err)
	}
	return row.ID
}

// adminStatus 读取某行当前状态（用不带 UserContext 的句柄，绕过数据权限读屏蔽）。
func adminStatus(t *testing.T, e *env, id uint64) int {
	t.Helper()
	var row adminmodel.AdminEntity
	if err := e.db.Where("id = ?", id).First(&row).Error; err != nil {
		t.Fatalf("读取管理员行失败：%v", err)
	}
	return row.Status
}

// adminPhone 读取某行电话。
func adminPhone(t *testing.T, e *env, id uint64) string {
	t.Helper()
	var row adminmodel.AdminEntity
	if err := e.db.Where("id = ?", id).First(&row).Error; err != nil {
		t.Fatalf("读取管理员行失败：%v", err)
	}
	if row.Phone == nil {
		return ""
	}
	return *row.Phone
}

// adminExists 判断某行是否还在（不带 UserContext）。
func adminExists(t *testing.T, e *env, id uint64) bool {
	t.Helper()
	var count int64
	if err := e.db.Model(&adminmodel.AdminEntity{}).Where("id = ?", id).Count(&count).Error; err != nil {
		t.Fatalf("统计管理员行失败：%v", err)
	}
	return count > 0
}

// TestDataRuleUpdateRejectsInvisibleRow Update：越界行被拒绝且一行没改；可见行照常。
func TestDataRuleUpdateRejectsInvisibleRow(t *testing.T) {
	c := setupWriteCase(t)

	err := c.env.db.WithContext(c.userCtx).Model(&adminmodel.AdminEntity{}).
		Where("id = ?", c.invisible).Updates(map[string]any{"status": 3}).Error
	if err == nil || !strings.Contains(err.Error(), "数据权限拒绝") {
		t.Fatalf("不可见的行应当被写路径拒绝，实际：%v", err)
	}
	if got := adminStatus(t, c.env, c.invisible); got != adminmodel.AdminStatusActive {
		t.Fatalf("被拒绝的更新不应改动数据，status=%d", got)
	}

	if err = c.env.db.WithContext(c.userCtx).Model(&adminmodel.AdminEntity{}).
		Where("id = ?", c.visible).Updates(map[string]any{"status": 3}).Error; err != nil {
		t.Fatalf("可见行应当可以更新：%v", err)
	}
	if got := adminStatus(t, c.env, c.visible); got != 3 {
		t.Fatalf("可见行应当被更新为 3，实际 %d", got)
	}
}

// TestDataRuleUpdateZeroRowsTellsCaller 插件参与且 0 行时如实报错（可能是越界，也可能是不存在）；
// 插件**没参与**（没有 UserContext）时同样的 0 行结果不报权限错误。
func TestDataRuleUpdateZeroRowsTellsCaller(t *testing.T) {
	c := setupWriteCase(t)

	err := c.env.db.WithContext(c.userCtx).Model(&adminmodel.AdminEntity{}).
		Where("id = ?", uint64(99999999)).Updates(map[string]any{"status": 3}).Error
	if err == nil || !strings.Contains(err.Error(), "数据权限拒绝") || !strings.Contains(err.Error(), "没有可更新的行") {
		t.Fatalf("插件参与且 0 行时应当明确告知什么都没改成，实际：%v", err)
	}

	plain := c.env.db.Model(&adminmodel.AdminEntity{}).
		Where("id = ?", uint64(99999999)).Updates(map[string]any{"status": 3})
	if plain.Error != nil {
		t.Fatalf("插件未参与时 0 行不应报权限错误，实际：%v", plain.Error)
	}
	if plain.RowsAffected != 0 {
		t.Fatalf("前置条件不成立：这一行不该存在，RowsAffected=%d", plain.RowsAffected)
	}
}

// TestDataRuleDeleteRejectsInvisibleRow Delete 同样受行级写保护。
func TestDataRuleDeleteRejectsInvisibleRow(t *testing.T) {
	c := setupWriteCase(t)

	err := c.env.db.WithContext(c.userCtx).Where("id = ?", c.invisible).
		Delete(&adminmodel.AdminEntity{}).Error
	if err == nil || !strings.Contains(err.Error(), "数据权限拒绝") {
		t.Fatalf("不可见的行应当无法删除，实际：%v", err)
	}
	if !adminExists(t, c.env, c.invisible) {
		t.Fatal("被拒绝的删除不应真的删掉数据")
	}

	if err = c.env.db.WithContext(c.userCtx).Where("id = ?", c.visible).
		Delete(&adminmodel.AdminEntity{}).Error; err != nil {
		t.Fatalf("可见行应当可以删除：%v", err)
	}
	if adminExists(t, c.env, c.visible) {
		t.Fatal("可见行应当已被删除")
	}
}

// TestDataRuleUpdateWithoutWhereTouchesOnlyVisibleRows 有规则时，**不带 WHERE** 的 Update：
// 既不会被 gorm 判成无条件全表更新，也只影响可见行。
func TestDataRuleUpdateWithoutWhereTouchesOnlyVisibleRows(t *testing.T) {
	c := setupWriteCase(t)

	res := c.env.db.WithContext(c.userCtx).Model(&adminmodel.AdminEntity{}).
		Updates(map[string]any{"status": 9})
	if res.Error != nil {
		t.Fatalf("有规则时，不带 WHERE 的 Update 应当被允许（条件由插件注入），实际：%v", res.Error)
	}
	if errors.Is(res.Error, gorm.ErrMissingWhereClause) {
		t.Fatal("注入条件后不应再被判为缺少 WHERE")
	}
	if got := adminStatus(t, c.env, c.visible); got != 9 {
		t.Fatalf("可见行应当被更新为 9，实际 %d", got)
	}
	if got := adminStatus(t, c.env, c.invisible); got != adminmodel.AdminStatusActive {
		t.Fatalf("不可见行不应被这条语句改动，status=%d", got)
	}

	// 反证：插件不介入时，gorm 的原判定照旧生效。
	plain := c.env.db.Model(&adminmodel.AdminEntity{}).Updates(map[string]any{"status": 8})
	if !errors.Is(plain.Error, gorm.ErrMissingWhereClause) {
		t.Fatalf("无 UserContext 时应当由 gorm 判定缺少 WHERE，实际：%v", plain.Error)
	}
}

// TestDataRuleUpdateOmitsProtectedColumn 字段级写保护：被屏蔽的列改完读回原值未变。
func TestDataRuleUpdateOmitsProtectedColumn(t *testing.T) {
	config := deptOneRuleConfig()
	config.OmitFields = []string{"phone"}
	c := setupWriteCaseWith(t, config, &datarule.UserContext{UserID: 100})

	phone := "13800000000"
	if err := c.env.db.Model(&adminmodel.AdminEntity{}).
		Where("id = ?", c.visible).Updates(map[string]any{"phone": phone}).Error; err != nil {
		t.Fatalf("预置电话失败：%v", err)
	}

	err := c.env.db.WithContext(c.userCtx).Model(&adminmodel.AdminEntity{}).
		Where("id = ?", c.visible).
		Updates(map[string]any{"phone": "19900000000", "status": 3}).Error
	if err != nil {
		t.Fatalf("可见行应当可以更新（被屏蔽的列由 gorm 从 SET 子句里剔除）：%v", err)
	}
	if got := adminPhone(t, c.env, c.visible); got != phone {
		t.Fatalf("被屏蔽的列不应被改写：期望 %q 实际 %q", phone, got)
	}
	if got := adminStatus(t, c.env, c.visible); got != 3 {
		t.Fatalf("未被屏蔽的列应当照常更新，status=%d", got)
	}
}

// TestDataRuleCreateValidatesValues Create 的行级值校验（含批量与 fail-closed）。
func TestDataRuleCreateValidatesValues(t *testing.T) {
	config := admindto.RuleConfigDTO{
		ConditionGroups: []admindto.RuleConditionGroupDTO{{
			Logic:      "AND",
			Conditions: []admindto.RuleConditionDTO{{Field: "dept_id", Op: "EQ", Value: "dept.scope:SELF"}},
		}},
	}
	c := setupWriteCaseWith(t, config, &datarule.UserContext{UserID: 100, DeptID: 1})

	newAdmin := func(deptID uint64) *adminmodel.AdminEntity {
		username := uniq("create")
		email := username + "@example.com"
		return &adminmodel.AdminEntity{
			Username: username,
			Password: "probe-password",
			Email:    &email,
			DeptID:   deptID,
			Status:   adminmodel.AdminStatusActive,
		}
	}

	// 命中本部门：放行。
	okRow := newAdmin(1)
	if err := c.env.db.WithContext(c.userCtx).Create(okRow).Error; err != nil {
		t.Fatalf("本部门的新行应当可以创建：%v", err)
	}
	if !adminExists(t, c.env, okRow.ID) {
		t.Fatal("放行的行应当真的落库")
	}

	// 越界：拒绝，且不落库。
	badRow := newAdmin(2)
	err := c.env.db.WithContext(c.userCtx).Create(badRow).Error
	if err == nil || !strings.Contains(err.Error(), "数据权限拒绝") {
		t.Fatalf("越界的新行应当被拒绝，实际：%v", err)
	}
	if adminExists(t, c.env, badRow.ID) {
		t.Fatal("被拒绝的行不应落库")
	}

	// 批量：任何一条越界都拒绝整批（第一行也不该被写进去）。
	first, second := newAdmin(1), newAdmin(2)
	err = c.env.db.WithContext(c.userCtx).Create(&[]*adminmodel.AdminEntity{first, second}).Error
	if err == nil || !strings.Contains(err.Error(), "第 2 条") {
		t.Fatalf("批量创建应当逐元素校验并指出越界的那一条，实际：%v", err)
	}
	if adminExists(t, c.env, first.ID) || adminExists(t, c.env, second.ID) {
		t.Fatal("批量创建被拒绝时整批都不应落库")
	}

	// fail-closed：条件字段在 Dest 上取不到值（map 里没有 dept_id）。
	mapErr := c.env.db.WithContext(c.userCtx).Model(&adminmodel.AdminEntity{}).
		Create(map[string]any{"username": uniq("mapcreate")}).Error
	if mapErr == nil || !strings.Contains(mapErr.Error(), "数据权限拒绝") || !strings.Contains(mapErr.Error(), "dept_id") {
		t.Fatalf("条件字段取不到时必须 fail-closed 拒绝，实际：%v", mapErr)
	}
}
