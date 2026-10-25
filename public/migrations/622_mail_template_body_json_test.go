package migrations_test

// 622_mail_template_body_json_test.go — 模板可视化正文三列的判定与幂等。
//
// 判据是「三列都在」而不是「表存在」：mail_templates 从 129 起就在，只看表名会让这条
// 迁移对既有库永久跳过 —— 那正是「写了列却没加上」的静默失败形态（本仓 195 踩过）。

import (
	"testing"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

const mailTemplateBodyJSONVersion = "622-mail-template-body-json"

// TestMailTemplateBodyJSONMigration 判定命中 + 三列存在且默认值正确 + 幂等。
func TestMailTemplateBodyJSONMigration(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	var target migrations.Migration
	for _, m := range migrations.All() {
		if m.Version == mailTemplateBodyJSONVersion {
			target = m
			break
		}
	}
	if target.Version == "" {
		t.Fatalf("迁移 %s 未注册", mailTemplateBodyJSONVersion)
	}

	// 判定：迁移跑过之后必须返回 1（三列都在）。
	var applied int64
	if err := db.Raw(target.CheckSQL, target.TableName).Scan(&applied).Error; err != nil {
		t.Fatalf("执行 CheckSQL 失败: %v", err)
	}
	if applied != 1 {
		t.Fatalf("迁移后判据应返回 1（三列都在），实际 %d", applied)
	}

	// 三列的类型与默认值。
	rows, err := db.Raw(`SELECT column_name, is_nullable, coalesce(column_default, '')
		FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'mail_templates'
		  AND column_name IN ('body_json','body_compiled_html','body_compiled_text')
		ORDER BY column_name`).Rows()
	if err != nil {
		t.Fatalf("查列失败: %v", err)
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var name, nullable, def string
		if err := rows.Scan(&name, &nullable, &def); err != nil {
			t.Fatal(err)
		}
		seen[name] = true
		switch name {
		case "body_json":
			if nullable != "YES" {
				t.Errorf("body_json 应可空（代码模式 / 存量模板没有树），实际 nullable=%s", nullable)
			}
		case "body_compiled_html", "body_compiled_text":
			if nullable != "NO" {
				t.Errorf("%s 应 NOT NULL（默认空串），实际 nullable=%s", name, nullable)
			}
			if def == "" {
				t.Errorf("%s 缺默认值：存量行会变成 NULL", name)
			}
		}
	}
	for _, col := range []string{"body_json", "body_compiled_html", "body_compiled_text"} {
		if !seen[col] {
			t.Fatalf("列 %s 不存在", col)
		}
	}

	// 幂等：整条 SQL 在已应用的库上重放不报错、不改动已有行。
	var before int64
	if err := db.Raw("SELECT COUNT(*) FROM mail_templates").Scan(&before).Error; err != nil {
		t.Fatal(err)
	}
	for _, stmt := range migrations.SplitStatements(target.SQL) {
		if stmt == "" {
			continue
		}
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("重放语句失败（迁移必须幂等）: %v\nSQL: %s", err, stmt)
		}
	}
	var after int64
	if err := db.Raw("SELECT COUNT(*) FROM mail_templates").Scan(&after).Error; err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("重放改动了行数：%d → %d", before, after)
	}
}
