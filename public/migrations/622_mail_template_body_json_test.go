package migrations_test

import (
	"testing"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

const mailTemplateBodyJSONVersion = "622-mail-template-body-json"

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

	var applied int64
	if err := db.Raw(target.CheckSQL, target.TableName).Scan(&applied).Error; err != nil {
		t.Fatalf("执行 CheckSQL 失败: %v", err)
	}
	if applied != 1 {
		t.Fatalf("迁移后判据应返回 1（三列都在），实际 %d", applied)
	}

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
