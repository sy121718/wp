package feature

import (
	"testing"

	"gorm.io/gorm"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

// 本文件锁 713 这条 seed：AI 新建供应商成功后的手动刷新入口文案。
//
// 放在 feature 包（与 712 同理）：按 AI 精确包可运行，不被 public/migrations 里
// 其他域的 register 测试编译问题拖住。
const aiCreateRefreshVersion = "713-ai-create-refresh-i18n"

func findAICreateRefreshSeed(t *testing.T) migrations.Seed {
	t.Helper()
	for _, seed := range migrations.AllSeeds() {
		if seed.Version == aiCreateRefreshVersion {
			return seed
		}
	}
	t.Fatalf("种子 %s 未注册（register.go 里漏了 registerAICreateRefreshI18n？）", aiCreateRefreshVersion)
	return migrations.Seed{}
}

// TestAICreateRefreshSeedPrepares 按 public 规则做持久化 PREPARE 校验：
// 新 SQL 上线前必须能准备成功，否则那条迁移会在执行时才炸。
func TestAICreateRefreshSeedPrepares(t *testing.T) {
	seed := findAICreateRefreshSeed(t)
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	if err := db.Connection(func(conn *gorm.DB) error {
		if err := conn.Exec("PREPARE ai_create_refresh_seed AS " + seed.SQL).Error; err != nil {
			return err
		}
		return conn.Exec("DEALLOCATE ai_create_refresh_seed").Error
	}); err != nil {
		t.Fatal(err)
	}
}

// TestAICreateRefreshSeedCoversBothLanguages 钉住 RunSeeds 之后的中英两元组，
// 并用门槛求值证明它**精确枚举两个元组**（缺 en 时必须变 0）。
func TestAICreateRefreshSeedCoversBothLanguages(t *testing.T) {
	seed := findAICreateRefreshSeed(t)
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
		Where("item_key = ?", "admin.ai.create.refresh").
		Order("lang").
		Scan(&rows).Error; err != nil {
		t.Fatalf("读词条失败：%v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("应恰有 2 条（zh-CN / en-US），实际 %d 条：%+v", len(rows), rows)
	}
	want := map[string]string{
		"zh-CN": "刷新页面查看新供应商",
		"en-US": "Refresh the page to see the new provider",
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

	var native string
	if err := db.Raw(seed.ConditionSQL).Scan(&native).Error; err != nil {
		t.Fatalf("求值门槛失败：%v", err)
	}
	if native != "1" {
		t.Fatalf("两条都在时门槛应为 1，实际 %q", native)
	}
	if err := db.Exec("DELETE FROM sys_i18n WHERE item_key = ? AND lang = ?",
		"admin.ai.create.refresh", "en-US").Error; err != nil {
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
