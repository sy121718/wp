package migrations_test

import (
	"testing"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

func TestMailEmptyStatesSeedPreservesOperatorValues(t *testing.T) {
	const version = "441-i18n-mail-empty-states"
	var seed migrations.Seed
	for _, item := range migrations.AllSeeds() {
		if item.Version == version {
			seed = item
			break
		}
	}
	if seed.Version == "" {
		t.Fatal("441 邮件空态词条未自注册")
	}
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	// These historical seeds establish the exact values that 441 is allowed to update.
	for _, item := range migrations.AllSeeds() {
		switch item.Version {
		case "190-i18n-seed-marketing", "232-i18n-seed-admin-pages-round4", "437-i18n-mail-empty-descriptions":
			for _, stmt := range migrations.SplitStatements(item.SQL) {
				if err := db.Exec(stmt).Error; err != nil {
					t.Fatalf("初始化 %s: %v", item.Version, err)
				}
			}
		}
	}
	const custom = "Operator custom text"
	if err := db.Exec("UPDATE sys_i18n SET item_value = ? WHERE item_key = ? AND lang = ?", custom, "admin.mail.templates.empty", "en-US").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time) VALUES (?, ?, ?, 200, 'admin', 'operator', 1, now(), now())", "admin.mail.marketing.contacts.empty.initial.title", "zh-CN", custom).Error; err != nil {
		t.Fatal(err)
	}
	var applied int64
	if err := db.Raw(seed.ConditionSQL).Scan(&applied).Error; err != nil || applied != 0 {
		t.Fatalf("执行前判定 %d，错误 %v", applied, err)
	}
	for round := 1; round <= 2; round++ {
		if err := migrations.RunSeeds(db); err != nil {
			t.Fatalf("第 %d 轮重跑全部 seed: %v", round, err)
		}
		if err := db.Raw(seed.ConditionSQL).Scan(&applied).Error; err != nil || applied != 1 {
			t.Fatalf("第 %d 轮判定 %d，错误 %v", round, applied, err)
		}
		for _, historical := range migrations.AllSeeds() {
			if historical.Version != "190-i18n-seed-marketing" && historical.Version != "232-i18n-seed-admin-pages-round4" {
				continue
			}
			if err := db.Raw(historical.ConditionSQL).Scan(&applied).Error; err != nil || applied != 1 {
				t.Fatalf("第 %d 轮旧 seed %s 判定 %d，错误 %v", round, historical.Version, applied, err)
			}
		}
		// 下面那两条键的中英终值不是 441 写的，而是比它晚的收口迁移改写的：
		//   522 —— 邮件拆页后 `.empty`（筛选无结果）改指页头「导入联系人」抽屉；
		//   526 —— 联系人 CRUD 落地后重写 `.initial`（无筛选空态）描述。
		// 所以断言终值之前，先确认这两条迁移确实把判据翻转成了「已完成」：
		// 否则「期望值」就只是现实的镜子 —— 谁把 522 / 526 删了，用例照样绿。
		// （本用例的库里只有 `.initial.title` zh-CN 被改成运营值，不影响这两条判据。）
		for _, version := range []string{
			"522-mail-split-i18n-fixes",
			"526-mail-contact-crud-i18n",
		} {
			superseding := findSeedByVersion(t, version)
			if err := db.Raw(superseding.ConditionSQL).Scan(&applied).Error; err != nil || applied != 1 {
				t.Fatalf("第 %d 轮 %s 判定 %d，错误 %v；下面的终值由它收口", round, version, applied, err)
			}
		}
		for _, tc := range []struct{ key, lang, want, by string }{
			// by = 终值出处：`441` 表示这一轮仍由 441 写入/保持的值。
			{"admin.mail.templates.empty", "zh-CN", "新建邮件模板，填写主题与正文。", "441"},
			{"admin.mail.templates.empty", "en-US", custom, "441（运营值）"},
			{"admin.mail.marketing.contacts.empty.initial.title", "zh-CN", custom, "441（运营值，后继迁移不得覆盖）"},
			{"admin.mail.marketing.contacts.empty.initial.title", "en-US", "No contacts yet", "441"},
			{"admin.mail.marketing.contacts.empty.initial", "zh-CN",
				"新建一位联系人手工加一条，或用「导入联系人」批量导入 CSV —— 名单建立起来才能按标签圈人群群发。", "526"},
			{"admin.mail.marketing.contacts.empty.initial", "en-US",
				"Add one contact by hand, or import a CSV in bulk — the list has to exist before you can target people by tag.", "526"},
			{"admin.mail.marketing.contacts.empty", "zh-CN",
				"可调整筛选条件，或点右上角「导入联系人」批量导入。", "522"},
			{"admin.mail.marketing.contacts.empty", "en-US",
				`Adjust the filters, or use "Import contacts" at the top right to bulk import.`, "522"},
		} {
			var got string
			if err := db.Raw("SELECT item_value FROM sys_i18n WHERE item_key = ? AND lang = ?", tc.key, tc.lang).Scan(&got).Error; err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("第 %d 轮 %s/%s = %q，期望 %q（终值出处 %s）", round, tc.key, tc.lang, got, tc.want, tc.by)
			}
		}
	}
}
