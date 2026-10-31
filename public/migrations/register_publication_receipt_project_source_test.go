package migrations_test

import (
	"strings"
	"testing"

	"gorm.io/gorm"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

const publicationReceiptSourceTypeProjectVersion = "703-publication-receipt-project-source"

func publicationReceiptProjectSourceMigration(t *testing.T) migrations.Migration {
	t.Helper()
	for _, m := range migrations.All() {
		if m.Version != publicationReceiptSourceTypeProjectVersion {
			continue
		}
		if strings.TrimSpace(m.SQL) == "" {
			t.Fatal("703 迁移缺少 SQL")
		}
		if m.TableName != "publication_receipts" {
			t.Fatalf("703 迁移的 TableName 应为 publication_receipts，实际 %q", m.TableName)
		}
		return m
	}
	t.Fatalf("缺少 %s 迁移注册", publicationReceiptSourceTypeProjectVersion)
	return migrations.Migration{}
}

func receiptSourceTypeCheckReady(t *testing.T, db *gorm.DB, m migrations.Migration) int64 {
	t.Helper()
	if strings.Contains(m.CheckSQL, "?") {
		t.Fatalf("703 判据不应含占位符（迁移器只为表名绑一个参数，对象名须写进字面量）：%s", m.CheckSQL)
	}
	var ready int64
	if err := db.Raw(m.CheckSQL).Scan(&ready).Error; err != nil {
		t.Fatalf("执行 703 CheckSQL 失败: %v", err)
	}
	return ready
}

func TestPublicationReceiptProjectSourceIsApplied(t *testing.T) {
	m := publicationReceiptProjectSourceMigration(t)
	db := support.NewMigratedPGTestDB(t)
	if got := receiptSourceTypeCheckReady(t, db, m); got != 1 {
		t.Fatalf("生产迁移库上 703 判据应返回 1，实际 %d", got)
	}
	for i := 0; i < 2; i++ {
		if err := db.Exec(m.SQL).Error; err != nil {
			t.Fatalf("第 %d 次重跑 703 失败: %v", i+1, err)
		}
	}
	if got := receiptSourceTypeCheckReady(t, db, m); got != 1 {
		t.Fatalf("重跑后 703 判据应仍为 1，实际 %d", got)
	}
}

func TestPublicationReceiptProjectSourceAcceptsOnlyThreeExactValues(t *testing.T) {
	m := publicationReceiptProjectSourceMigration(t)
	db := support.NewMigratedPGTestDB(t)

	const setConstraint = `ALTER TABLE public.publication_receipts DROP CONSTRAINT IF EXISTS publication_receipts_source_type_check`
	apply := func(t *testing.T, stmt string) {
		t.Helper()
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("执行失败(%s): %v", stmt, err)
		}
	}
	replaceConstraint := func(t *testing.T, body string) {
		t.Helper()
		apply(t, setConstraint)
		apply(t, "ALTER TABLE public.publication_receipts ADD CONSTRAINT publication_receipts_source_type_check CHECK ("+body+")")
	}

	revert := func(t *testing.T) {
		t.Helper()
		if err := db.Exec(m.SQL).Error; err != nil {
			t.Fatalf("复原（重跑 703）失败: %v", err)
		}
		if got := receiptSourceTypeCheckReady(t, db, m); got != 1 {
			t.Fatalf("复原后 703 判据应返回 1，实际 %d", got)
		}
	}

	cases := []struct {
		name string
		body string
	}{
		{
			name: "第四值 site（包含判据会漏掉的那种扩宽）",
			body: "source_type IN ('page', 'presentation', 'project', 'site')",
		},
		{
			name: "第四值 padding",
			body: "source_type IN ('page', 'presentation', 'padding', 'project')",
		},
		{
			name: "少一个既有值（只剩 page / project）",
			body: "source_type IN ('page', 'project')",
		},
		{
			name: "改成任意文本（不要白名单）",
			body: "source_type <> ''",
		},
		{
			name: "三值仍在但被放宽：OR TRUE",
			body: "source_type IN ('page', 'presentation', 'project') OR TRUE",
		},
		{
			name: "三值仍在但被放宽：OR IS NULL",
			body: "source_type IN ('page', 'presentation', 'project') OR source_type IS NULL",
		},
		{
			name: "白名单搬到别的列上（判据跟着 conkey 走）",
			body: "action IN ('page', 'presentation', 'project')",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {

			t.Cleanup(func() { revert(t) })
			replaceConstraint(t, c.body)
			if got := receiptSourceTypeCheckReady(t, db, m); got != 0 {
				t.Fatalf("%s：判据应返回 0（必须重跑迁移），实际 %d", c.name, got)
			}
		})
	}
}
