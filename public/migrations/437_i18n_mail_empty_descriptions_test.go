package migrations_test

import (
	"testing"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

const mailEmptyDescriptionVersion = "437-i18n-mail-empty-descriptions"

var mailEmptyDescriptions = map[string]map[string]string{
	"admin.mail.marketing.contacts.empty": {
		"zh-CN": "可调整筛选条件，或在下方折叠区批量导入联系人。",
		"en-US": "Adjust the filters, or import contacts in the section below.",
	},
	"admin.mail.marketing.campaigns.empty": {
		"zh-CN": "新建活动后点「启动群发」开始发送。",
		"en-US": "Create a campaign, then click Start sending.",
	},
	"admin.mail.campaign.links_empty": {
		"zh-CN": "启动群发后，有收件人点击邮件链接才会显示排行。",
		"en-US": "Start sending; the ranking appears after recipients click links in the email.",
	},
	"admin.mail.campaign.recipients_empty": {
		"zh-CN": "启动群发后，收件人会分批进入投递队列。",
		"en-US": "Start sending to add recipients to the delivery queue in batches.",
	},
	"admin.mail.automation.empty": {
		"zh-CN": "先新建一条，比如「新订阅 → 等 1 天 → 发欢迎邮件 → 打上 welcomed 标签」。",
		"en-US": "Create a flow, for example: new subscriber → wait 1 day → send a welcome email → tag them welcomed.",
	},
	"admin.mail.automation.runs.empty": {
		"zh-CN": "流程启用后，满足触发条件的人会自动进入。",
		"en-US": "Once a flow is enabled, people who match its trigger enter automatically.",
	},
}

func TestMailEmptyDescriptionSeedPreservesOperatorChanges(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	var target migrations.Seed
	for _, s := range migrations.AllSeeds() {
		switch s.Version {
		case "190-i18n-seed-marketing", "232-i18n-seed-admin-pages-round4":
			for _, stmt := range migrations.SplitStatements(s.SQL) {
				if err := db.Exec(stmt).Error; err != nil {
					t.Fatalf("初始化历史词条 %s: %v", s.Version, err)
				}
			}
		case mailEmptyDescriptionVersion:
			target = s
		}
	}
	if target.Version == "" {
		t.Fatalf("迁移 %s 未注册", mailEmptyDescriptionVersion)
	}
	keys := []string{
		"admin.mail.marketing.contacts.empty", "admin.mail.marketing.campaigns.empty",
		"admin.mail.campaign.links_empty", "admin.mail.campaign.recipients_empty",
		"admin.mail.automation.empty", "admin.mail.automation.runs.empty",
	}
	old := map[string]map[string]string{}
	for _, key := range keys {
		old[key] = map[string]string{}
		for _, lang := range []string{"zh-CN", "en-US"} {
			var value string
			if err := db.Raw("SELECT item_value FROM sys_i18n WHERE item_key = ? AND lang = ?", key, lang).Scan(&value).Error; err != nil {
				t.Fatalf("读取旧值 %s/%s: %v", key, lang, err)
			}
			old[key][lang] = value
		}
	}
	const custom = "运营手动改写，不应覆盖"
	if err := db.Exec("UPDATE sys_i18n SET item_value = ? WHERE item_key = ? AND lang = ?", custom, keys[0], "zh-CN").Error; err != nil {
		t.Fatal(err)
	}
	var applied int64
	if err := db.Raw(target.ConditionSQL).Scan(&applied).Error; err != nil || applied != 0 {
		t.Fatalf("修正前判据 = %d, err = %v；仍有旧值应执行", applied, err)
	}
	for _, stmt := range migrations.SplitStatements(target.SQL) {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("执行迁移失败: %v", err)
		}
	}
	for _, key := range keys {
		for _, lang := range []string{"zh-CN", "en-US"} {
			var got string
			if err := db.Raw("SELECT item_value FROM sys_i18n WHERE item_key = ? AND lang = ?", key, lang).Scan(&got).Error; err != nil {
				t.Fatal(err)
			}
			want := mailEmptyDescriptions[key][lang]
			if key == keys[0] && lang == "zh-CN" {
				want = custom
			}
			if got != want {
				t.Errorf("%s/%s: %q → %q，期望 %q", key, lang, old[key][lang], got, want)
			}
		}
	}
	if err := db.Raw(target.ConditionSQL).Scan(&applied).Error; err != nil || applied != 1 {
		t.Fatalf("修正后判据 = %d, err = %v；应跳过重跑", applied, err)
	}
	// Re-running historical seeds cannot restore their old values; the new seed is idempotent.
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("重跑全部 seed 失败: %v", err)
	}
	for _, key := range keys {
		for _, lang := range []string{"zh-CN", "en-US"} {
			var got string
			if err := db.Raw("SELECT item_value FROM sys_i18n WHERE item_key = ? AND lang = ?", key, lang).Scan(&got).Error; err != nil {
				t.Fatal(err)
			}
			want := mailEmptyDescriptions[key][lang]
			if key == keys[0] && lang == "zh-CN" {
				want = custom
			}
			if got != want {
				t.Errorf("重跑 seed 后 %s/%s = %q，期望 %q", key, lang, got, want)
			}
		}
	}
}
