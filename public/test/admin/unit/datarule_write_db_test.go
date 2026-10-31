package unit

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

type writeCaseEnv struct {
	env       *env
	userCtx   context.Context
	visible   uint64
	invisible uint64
}

func deptOneRuleConfig() admindto.RuleConfigDTO {
	return admindto.RuleConfigDTO{
		ConditionGroups: []admindto.RuleConditionGroupDTO{{
			Logic:      "AND",
			Conditions: []admindto.RuleConditionDTO{{Field: "dept_id", Op: "EQ", Value: "1"}},
		}},
	}
}

func setupWriteCase(t *testing.T) *writeCaseEnv {
	t.Helper()
	return setupWriteCaseWith(t, deptOneRuleConfig(), &datarule.UserContext{UserID: 100})
}

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

func adminStatus(t *testing.T, e *env, id uint64) int {
	t.Helper()
	var row adminmodel.AdminEntity
	if err := e.db.Where("id = ?", id).First(&row).Error; err != nil {
		t.Fatalf("读取管理员行失败：%v", err)
	}
	return row.Status
}

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

func adminExists(t *testing.T, e *env, id uint64) bool {
	t.Helper()
	var count int64
	if err := e.db.Model(&adminmodel.AdminEntity{}).Where("id = ?", id).Count(&count).Error; err != nil {
		t.Fatalf("统计管理员行失败：%v", err)
	}
	return count > 0
}

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

	plain := c.env.db.Model(&adminmodel.AdminEntity{}).Updates(map[string]any{"status": 8})
	if !errors.Is(plain.Error, gorm.ErrMissingWhereClause) {
		t.Fatalf("无 UserContext 时应当由 gorm 判定缺少 WHERE，实际：%v", plain.Error)
	}
}

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

	okRow := newAdmin(1)
	if err := c.env.db.WithContext(c.userCtx).Create(okRow).Error; err != nil {
		t.Fatalf("本部门的新行应当可以创建：%v", err)
	}
	if !adminExists(t, c.env, okRow.ID) {
		t.Fatal("放行的行应当真的落库")
	}

	badRow := newAdmin(2)
	err := c.env.db.WithContext(c.userCtx).Create(badRow).Error
	if err == nil || !strings.Contains(err.Error(), "数据权限拒绝") {
		t.Fatalf("越界的新行应当被拒绝，实际：%v", err)
	}
	if adminExists(t, c.env, badRow.ID) {
		t.Fatal("被拒绝的行不应落库")
	}

	first, second := newAdmin(1), newAdmin(2)
	err = c.env.db.WithContext(c.userCtx).Create(&[]*adminmodel.AdminEntity{first, second}).Error
	if err == nil || !strings.Contains(err.Error(), "第 2 条") {
		t.Fatalf("批量创建应当逐元素校验并指出越界的那一条，实际：%v", err)
	}
	if adminExists(t, c.env, first.ID) || adminExists(t, c.env, second.ID) {
		t.Fatal("批量创建被拒绝时整批都不应落库")
	}

	mapErr := c.env.db.WithContext(c.userCtx).Model(&adminmodel.AdminEntity{}).
		Create(map[string]any{"username": uniq("mapcreate")}).Error
	if mapErr == nil || !strings.Contains(mapErr.Error(), "数据权限拒绝") || !strings.Contains(mapErr.Error(), "dept_id") {
		t.Fatalf("条件字段取不到时必须 fail-closed 拒绝，实际：%v", mapErr)
	}
}
