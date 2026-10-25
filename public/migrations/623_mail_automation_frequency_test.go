package migrations_test

// 623_mail_automation_frequency_test.go — 频率两列 + 闸门索引的判定与幂等。

import (
	"testing"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

const mailAutomationFrequencyVersion = "623-mail-automation-frequency"

// TestMailAutomationFrequencyMigration 判定命中 + 两列默认值 + 索引存在 + 幂等。
func TestMailAutomationFrequencyMigration(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	var target migrations.Migration
	for _, m := range migrations.All() {
		if m.Version == mailAutomationFrequencyVersion {
			target = m
			break
		}
	}
	if target.Version == "" {
		t.Fatalf("迁移 %s 未注册", mailAutomationFrequencyVersion)
	}

	var applied int64
	if err := db.Raw(target.CheckSQL, target.TableName).Scan(&applied).Error; err != nil {
		t.Fatalf("执行 CheckSQL 失败: %v", err)
	}
	if applied != 1 {
		t.Fatalf("迁移后判据应返回 1（两列 + 索引都在），实际 %d", applied)
	}

	// 默认值：frequency 必须 every_time（改造前的行为），exit_on_reply false。
	rows, err := db.Raw(`SELECT column_name, coalesce(column_default, ''), is_nullable
		FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'mail_automations'
		  AND column_name IN ('trigger_frequency','exit_on_reply')
		ORDER BY column_name`).Rows()
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var name, def, nullable string
		if err := rows.Scan(&name, &def, &nullable); err != nil {
			t.Fatal(err)
		}
		seen[name] = true
		if nullable != "NO" {
			t.Errorf("%s 应 NOT NULL（有默认值），实际 nullable=%s", name, nullable)
		}
		switch name {
		case "trigger_frequency":
			if def == "" || !containsSub(def, "every_time") {
				t.Errorf("trigger_frequency 默认值应含 every_time，实际 %q", def)
			}
		case "exit_on_reply":
			if def == "" || !containsSub(def, "false") {
				t.Errorf("exit_on_reply 默认值应是 false，实际 %q", def)
			}
		}
	}
	for _, col := range []string{"trigger_frequency", "exit_on_reply"} {
		if !seen[col] {
			t.Fatalf("列 %s 不存在", col)
		}
	}

	// 闸门索引。
	var idx int64
	if err := db.Raw(`SELECT COUNT(*) FROM pg_indexes
		WHERE schemaname = current_schema() AND tablename = 'mail_automation_runs'
		  AND indexname = 'idx_mail_automation_runs_automation_contact'`).Scan(&idx).Error; err != nil {
		t.Fatal(err)
	}
	if idx != 1 {
		t.Fatalf("频率闸门索引不存在（会退化成全表扫描）")
	}

	// 幂等：重放整条 SQL 不报错。
	for _, stmt := range migrations.SplitStatements(target.SQL) {
		if stmt == "" {
			continue
		}
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("重放失败（迁移必须幂等）: %v\nSQL: %s", err, stmt)
		}
	}
}

func containsSub(haystack, needle string) bool {
	if len(needle) == 0 {
		return false
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
