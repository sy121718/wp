package migrations_test

// register_publication_receipt_project_source_test.go —— 迁移 703（回执来源新增 project）的落地判据。
//
// 判据要说的是「source_type 的白名单**恰好**是 page / presentation / project」，
// 而不是「定义里出现过这三个词」。差别在真实事故里是可见的：
//   · 有人把约束放宽成四值（又加了 'site'）—— 包含判据照样命中三条 LIKE，
//     于是这条迁移被判成「已应用」永久跳过，放宽的方向被静默接受；
//   · 有人把约束改成任意文本 —— 回执归属失去封闭性，收敛例程会按来源误领别人的回执。
// 所以这里既断言「完整落地 ⇒ 1」，也断言上面这些偏差「⇒ 0」，最后断言复原后重跑能回到 1
// （判据可修、不是恒真也不是噪声）。
//
// 夹具的 DDL 一律写 public.<表> 限定名：测试库的 search_path 是 "public,ext_shared"，
// CheckSQL 用的是 current_schema()（按 search_path 首项解析，恰好是 public）。
// 显式限定名把这个隐式假设写死成事实 —— 判据被改成别的解析方式时用例会明确失败。

import (
	"strings"
	"testing"

	"gorm.io/gorm"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

// 703 的版本号（与 register_publication_receipt_project_source.go 一致）。
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

// receiptSourceTypeCheckReady 用迁移器同一约定执行 CheckSQL：
// 只在 SQL 里真的出现 ? 时才传表名（migrator.go 的 apply 就是这样绑的）。
// 703 的判据零占位符 —— 这里不传参正是「零占位」那条要求的可执行形式：
// 若哪天有人在判据里塞了个 ?，这个用例会当场报参数个数不符。
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

// TestPublicationReceiptProjectSourceIsApplied 生产迁移库上判据必须认可「完整落地」，且整条 SQL 可重放。
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

// TestPublicationReceiptProjectSourceAcceptsOnlyThreeExactValues 判据必读认得偏差（不是恒真、不是包含判据）。
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
	// 复原 = 本迁移自己的 SQL（不是手抄一份「正确形状」）：判据必须由被测的那条迁移修回来。
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
			// 复原必须挂在 cleanup 上：下面判据失败时 Fatalf 会直接结束子用例，
			// 不先复原就会把「已被改成偏差形状的约束」留给后续子用例，从第二个起全部误判。
			t.Cleanup(func() { revert(t) })
			replaceConstraint(t, c.body)
			if got := receiptSourceTypeCheckReady(t, db, m); got != 0 {
				t.Fatalf("%s：判据应返回 0（必须重跑迁移），实际 %d", c.name, got)
			}
		})
	}
}
