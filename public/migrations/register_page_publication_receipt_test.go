package migrations_test

// register_page_publication_receipt_test.go —— 迁移 701（激活账本成功身份）的落地判据。
//
// 判据必须能区分三种「看起来跑过了」：
//   · 列不存在          —— 激活写回执 id 时当场报列不存在，只出现在真实发布路径上；
//   · 列型/可空性错     —— id 比较退化或历史行写不进 NULL，同样只在发布时才炸；
//   · 该列有默认值      —— 0 会被读成「回执 0 的成功尝试」，而 0 不是回执。
//
// 三条一起判，所以这里既断言 CheckSQL 在真实迁移库上返回 1（完整落地），
// 也断言「判据自己认得出来」这些偏差（否则一个恒返回 1 的 CheckSQL 也能过）。

import (
	"testing"

	"gorm.io/gorm"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

// 注意：本文件的 DDL 一律写 public.<表> 限定名。
//
// 理由：测试库的连接 search_path 是 "public,ext_shared"（见 pgtest），而 CheckSQL 用的是
// current_schema() —— 它按 search_path 第一项解析，恰好是 public，所以两者一致。但夹具里
// 若写裸表名，构造偏差时依赖的就是同一份隐式假设；显式限定名把这个假设写死成事实，
// 判据被改坏时（例如有人把 current_schema() 改成别的解析）用例会明确失败而不是碰巧通过。

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

// checkReady 用迁移器同一约定执行 CheckSQL：含 ? 时只传表名一个参数。
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
	// 生产迁移库上该列已经存在：判据必须认可「完整落地」，否则每次启动都要重跑。
	if got := receiptCheckReady(t, db, m); got != 1 {
		t.Fatalf("生产迁移库上 701 判据应返回 1，实际 %d", got)
	}
	// 幂等：重复执行不报错，执行后判据仍为 1。
	for i := 0; i < 2; i++ {
		if err := db.Exec(m.SQL).Error; err != nil {
			t.Fatalf("第 %d 次重跑 701 失败: %v", i+1, err)
		}
	}
	if got := receiptCheckReady(t, db, m); got != 1 {
		t.Fatalf("重跑后 701 判据应仍为 1，实际 %d", got)
	}
}

// TestPagePublicationReceiptCheckRejectsDrift 判据本身必须认得偏差（不是恒真）。
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
			// 复原后再跑一次迁移 SQL，判据必须回到 1：证明偏差是可修的、不是判据噪声。
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
