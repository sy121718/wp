package migrations_test

import (
	"testing"

	"gorm.io/gorm"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

func pagePublicationReceiptMigration(t *testing.T) migrations.Migration {
	t.Helper()
	for _, m := range migrations.All() {
		if m.Version == "701-page-publication-receipt" {
			if m.SQL == "" {
				t.Fatal("701 迁移缺少 SQL")
			}
			if m.TableName != "page_publications" {
				t.Fatalf("701 迁移的 TableName 应为 page_publications，实际 %q", m.TableName)
			}
			return m
		}
	}
	t.Fatal("缺少 701-page-publication-receipt 迁移注册")
	return migrations.Migration{}
}

func receiptCheckReady(t *testing.T, db *gorm.DB, m migrations.Migration) int64 {
	t.Helper()
	var ready int64
	if err := db.Raw(m.CheckSQL, m.TableName).Scan(&ready).Error; err != nil {
		t.Fatalf("执行 701 CheckSQL 失败: %v", err)
	}
	return ready
}

func TestPagePublicationReceiptMigrationIsIdempotent(t *testing.T) {
	m := pagePublicationReceiptMigration(t)
	db := support.NewMigratedPGTestDB(t)

	if got := receiptCheckReady(t, db, m); got != 1 {
		t.Fatalf("生产迁移库上 701 判据应返回 1，实际 %d", got)
	}

	for i := 0; i < 2; i++ {
		if err := db.Exec(m.SQL).Error; err != nil {
			t.Fatalf("第 %d 次重跑 701 失败: %v", i+1, err)
		}
	}
	if got := receiptCheckReady(t, db, m); got != 1 {
		t.Fatalf("重跑后 701 判据应仍为 1，实际 %d", got)
	}
}

func TestPagePublicationReceiptCheckRejectsDrift(t *testing.T) {
	m := pagePublicationReceiptMigration(t)
	db := support.NewMigratedPGTestDB(t)
	apply := func(t *testing.T, stmt string) {
		t.Helper()
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("构造偏差失败(%s): %v", stmt, err)
		}
	}
	cases := []struct {
		name   string
		drift  string
		revert string
	}{
		{
			name:   "列被删",
			drift:  "ALTER TABLE public.page_publications DROP COLUMN receipt_id",
			revert: "ALTER TABLE public.page_publications ADD COLUMN receipt_id bigint",
		},
		{
			name:   "列型变成 text",
			drift:  "ALTER TABLE public.page_publications ALTER COLUMN receipt_id TYPE text USING receipt_id::text",
			revert: "ALTER TABLE public.page_publications ALTER COLUMN receipt_id TYPE bigint USING receipt_id::bigint",
		},
		{
			name:   "被加上 NOT NULL（历史行写不进 NULL）",
			drift:  "ALTER TABLE public.page_publications ALTER COLUMN receipt_id SET NOT NULL",
			revert: "ALTER TABLE public.page_publications ALTER COLUMN receipt_id DROP NOT NULL",
		},
		{
			name:   "被加上默认值（0 会被读成回执 0）",
			drift:  "ALTER TABLE public.page_publications ALTER COLUMN receipt_id SET DEFAULT 0",
			revert: "ALTER TABLE public.page_publications ALTER COLUMN receipt_id DROP DEFAULT",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			apply(t, c.drift)
			if got := receiptCheckReady(t, db, m); got != 0 {
				t.Fatalf("%s：判据应返回 0（要求重跑迁移），实际 %d", c.name, got)
			}

			apply(t, c.revert)
			if err := db.Exec(m.SQL).Error; err != nil {
				t.Fatalf("%s 复原后重跑迁移失败: %v", c.name, err)
			}
			if got := receiptCheckReady(t, db, m); got != 1 {
				t.Fatalf("%s 复原后判据应返回 1，实际 %d", c.name, got)
			}
		})
	}
}
