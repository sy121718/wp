package feature

import (
	"testing"

	"gorm.io/gorm"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

// 本文件锁 712 这条 seed：AI 候选弹窗「没有追加权限」的界面词条。
//
// 放在 feature 包而不是 public/migrations：那条 seed 属 ai 域，跑在这里能按 AI 精确包
// 验证（public/migrations 的用例编译依赖其他域的 register 测试，本批不该被它拖住）。
const aiPickerNoPermissionVersion = "712-ai-picker-no-permission-i18n"

// findAIPickerNoPermissionSeed 按版本号取本批 seed。
func findAIPickerNoPermissionSeed(t *testing.T) migrations.Seed {
	t.Helper()
	for _, seed := range migrations.AllSeeds() {
		if seed.Version == aiPickerNoPermissionVersion {
			return seed
		}
	}
	t.Fatalf("种子 %s 未注册（register.go 里漏了 registerAIPickerNoPermissionI18n？）", aiPickerNoPermissionVersion)
	return migrations.Seed{}
}

// TestAIPickerNoPermissionSeedPrepares 按 public 规则做持久化 PREPARE 校验：
// 新 SQL 上线前必须能准备成功，否则那条迁移会在执行时才炸。
func TestAIPickerNoPermissionSeedPrepares(t *testing.T) {
	seed := findAIPickerNoPermissionSeed(t)
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	if err := db.Connection(func(conn *gorm.DB) error {
		if err := conn.Exec("PREPARE ai_picker_no_permission_seed AS " + seed.SQL).Error; err != nil {
			return err
		}
		return conn.Exec("DEALLOCATE ai_picker_no_permission_seed").Error
	}); err != nil {
		t.Fatal(err)
	}
}

// TestAIPickerNoPermissionSeedCoversBothLanguages 钉住 RunSeeds 之后的中英两元组。
//
// 这条词条是「没有追加权限」的界面说明，没有它用户看到的是归口文案，看不出是权限问题。
// 两个语言缺失都不能被门槛放过 —— 只数 zh-CN 一条时 en-US 缺了门槛照样成立，
// 英文界面就永远缺词。
func TestAIPickerNoPermissionSeedCoversBothLanguages(t *testing.T) {
	seed := findAIPickerNoPermissionSeed(t)
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("跑 seed 失败：%v", err)
	}

	type row struct {
		Lang  string
		Value string
	}
	var rows []row
	if err := db.Table("sys_i18n").
		Select("lang, item_value AS value").
		Where("item_key = ?", "admin.ai.picker.noPermission").
		Order("lang").
		Scan(&rows).Error; err != nil {
		t.Fatalf("读词条失败：%v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("应恰有 2 条（zh-CN / en-US），实际 %d 条：%+v", len(rows), rows)
	}

	want := map[string]string{
		"zh-CN": "没有追加模型的权限。",
		"en-US": "You do not have permission to add models.",
	}
	for _, r := range rows {
		expect, ok := want[r.Lang]
		if !ok {
			t.Fatalf("出现了预期之外的语种 %q", r.Lang)
		}
		if r.Value != expect {
			t.Errorf("%s 文案 = %q，想要 %q", r.Lang, r.Value, expect)
		}
	}

	// 门槛必须精确枚举这两个元组：把 en-US 删掉之后门槛要变 0，
	// 否则「补 en 的那次迁移」永远不会再执行。
	var native string
	if err := db.Raw(seed.ConditionSQL).Scan(&native).Error; err != nil {
		t.Fatalf("求值门槛失败：%v", err)
	}
	if native != "1" {
		t.Fatalf("两条都在时门槛应为 1，实际 %q", native)
	}
	if err := db.Exec("DELETE FROM sys_i18n WHERE item_key = ? AND lang = ?",
		"admin.ai.picker.noPermission", "en-US").Error; err != nil {
		t.Fatalf("删 en-US 失败：%v", err)
	}
	native = ""
	if err := db.Raw(seed.ConditionSQL).Scan(&native).Error; err != nil {
		t.Fatalf("求值门槛失败：%v", err)
	}
	if native != "0" {
		t.Fatalf("缺 en-US 时门槛必须为 0（只数 zh 一条会让它假成立），实际 %q", native)
	}
}
