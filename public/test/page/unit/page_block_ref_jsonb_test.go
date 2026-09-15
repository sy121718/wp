// 068 迁移（PG 特性优化第一批）的回归测试。
//
// 覆盖两件事：
//
//	① 等价性：JSONB 路径查询（jsonb_path_query_array + @>）替代旧写法
//	   draft_document::text LIKE '%"blockId": "<id>"%' 后，各类文档形态上
//	   结果集必须完全一致（含嵌套引用、structure 绑定、字符串字面量干扰、
//	   数字型 blockId、软删页面）；
//	② 索引有效：迁移 068 的三个索引被 planner 真实选中（BitmapOr），
//	   守住「page_model.go 的表达式与 068 的索引表达式一致」这条约束 ——
//	   任一侧改动而另一侧未同步时本测试失败（索引静默失效的护栏）。
//
// 表结构与 068 索引都由**生产迁移**建立（support.NewMigratedPGTestDB），不再手抄 DDL：
// 迁移改了索引表达式，这里立刻能发现，而不是守着与生产无关的本地副本。
// PG 不可用时 t.Skip。
package unit

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	pagemodel "go_wp/internal/module/page/model"

	"go_wp/public/test/support"
	"gorm.io/gorm"
)

// oldBlockRefCond 旧写法（改前实现），仅用于等价性对照，不参与生产代码。
const oldBlockRefCond = `draft_document::text LIKE '%"blockId": "' || ?::text || '"%'`

// newBlockRefCond 新写法（与 page_model.go 的 blockRefMatchCond 相同）。
const newBlockRefCond = `jsonb_path_query_array(draft_document, '$.**.blockId') @> jsonb_build_array(?::text)`

// blockRefTailCond structure 页眉/页脚分支（新旧写法共用，语义不变）。
const blockRefTailCond = ` OR draft_document->'settings'->'structure'->>'headerBlockId' = ?::text
	OR draft_document->'settings'->'structure'->>'footerBlockId' = ?::text`

// blockRefDocCase 一个页面文档样本。
type blockRefDocCase struct {
	name    string
	doc     string
	deleted bool
}

// blockRefDocCases 覆盖 blockId 可能出现的全部形态。
func blockRefDocCases(target, other string) []blockRefDocCase {
	return []blockRefDocCase{
		{
			name: "顶层 globalref 引用",
			doc:  fmt.Sprintf(`{"settings":{},"root":[{"id":"n1","type":"core.globalref","props":{"blockId":"%s"}}]}`, target),
		},
		{
			name: "容器内两层嵌套引用",
			doc:  fmt.Sprintf(`{"settings":{},"root":[{"id":"n1","type":"core.section","props":{},"children":[{"id":"n2","type":"core.globalref","props":{"blockId":"%s"}}]}]}`, target),
		},
		{
			name: "容器内三层嵌套引用",
			doc:  fmt.Sprintf(`{"settings":{},"root":[{"id":"n1","type":"core.section","props":{},"children":[{"id":"n2","type":"core.container","props":{},"children":[{"id":"n3","type":"core.globalref","props":{"blockId":"%s"}}]}]}]}`, target),
		},
		{
			name: "settings.structure 页眉绑定",
			doc:  fmt.Sprintf(`{"settings":{"structure":{"headerBlockId":"%s"}},"root":[]}`, target),
		},
		{
			name: "settings.structure 页脚绑定",
			doc:  fmt.Sprintf(`{"settings":{"structure":{"footerBlockId":"%s"}},"root":[]}`, target),
		},
		{
			name: "同页多处引用",
			doc:  fmt.Sprintf(`{"settings":{},"root":[{"id":"n1","type":"core.globalref","props":{"blockId":"%s"}},{"id":"n2","type":"core.globalref","props":{"blockId":"%s"}}]}`, target, other),
		},
		{
			name: "文本值内字面量（不得命中）",
			doc:  fmt.Sprintf(`{"settings":{},"root":[{"id":"t1","type":"core.text","props":{"content":"注意 \"blockId\": \"%s\" 只是字面量"}}]}`, target),
		},
		{
			name: "数字型 blockId（不得按字符串命中）",
			doc:  `{"settings":{},"root":[{"id":"d1","type":"core.globalref","props":{"blockId":12345}}]}`,
		},
		{
			name: "引用其他块",
			doc:  fmt.Sprintf(`{"settings":{},"root":[{"id":"n1","type":"core.globalref","props":{"blockId":"%s"}}]}`, other),
		},
		{
			name: "空文档",
			doc:  `{"settings":{},"root":[]}`,
		},
		{
			name:    "软删页面引用（不得命中）",
			doc:     fmt.Sprintf(`{"settings":{},"root":[{"id":"n1","type":"core.globalref","props":{"blockId":"%s"}}]}`, target),
			deleted: true,
		},
	}
}

// setupBlockRefPages 造数据：blockRefDocCases 样本与 2 万行无关页面
// （让 planner 在真实规模下选索引）。pages 表与迁移 068 的三个索引都来自生产迁移，
// 索引表达式与查询表达式漂移时 TestPageBlockReferenceIndexUsed 直接失败。
func setupBlockRefPages(t *testing.T, target, other string) (*gorm.DB, pagemodel.Model) {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	// pages.project_id 有外键 → projects(id)：先补一条真实工程行，样本页面共用。
	const blockRefProjectID = "9f2c1d40-0000-4000-8000-000000000068"
	support.SeedProjectRow(t, db, blockRefProjectID, "块引用回归站点")

	// 2 万行无关页面：draft_document 结构同真实文档，blockId 为无关值。
	if err := db.Exec(`INSERT INTO pages
		(id, project_id, kind, content_target_type, draft_path, draft_document, draft_version, stale, created_at, updated_at)
		SELECT gen_random_uuid(), $1::uuid, 'home', 'none', '/p/' || i,
		       jsonb_build_object('settings', '{}'::jsonb, 'root', jsonb_build_array(
		           jsonb_build_object('id','n'||i,'type','core.section','props','{}'::jsonb,'children', jsonb_build_array(
		               jsonb_build_object('id','n'||i||'b','type','core.globalref','props', jsonb_build_object('blockId','blk-'||(i%97))))))),
		       1, false, now(), now()
		  FROM generate_series(1, 20000) AS i`, blockRefProjectID).Error; err != nil {
		t.Fatalf("造无关页面失败: %v", err)
	}

	for _, c := range blockRefDocCases(target, other) {
		deleted := "NULL"
		if c.deleted {
			deleted = "now()"
		}
		stmt := fmt.Sprintf(`INSERT INTO pages
			(id, project_id, kind, content_target_type, draft_path, draft_document, draft_version, stale, deleted_at, created_at, updated_at)
			VALUES (gen_random_uuid(), $3::uuid, 'home', 'none', $1::text, $2::jsonb, 1, false, %s, now(), now())`, deleted)
		if err := db.Exec(stmt, "/case/"+c.name, c.doc, blockRefProjectID).Error; err != nil {
			t.Fatalf("插入样本页面(%s)失败: %v", c.name, err)
		}
	}
	if err := db.Exec("ANALYZE pages").Error; err != nil {
		t.Fatalf("ANALYZE 失败: %v", err)
	}
	return db, *pagemodel.NewPageModel(db)
}

// queryIDs 执行「条件 + id 排序」查询，返回命中 id 集合。
func queryIDs(t *testing.T, db *gorm.DB, cond string, args ...any) []string {
	t.Helper()
	var ids []string
	if err := db.Raw("SELECT id::text FROM pages WHERE deleted_at IS NULL AND ("+cond+") ORDER BY id::text", args...).
		Scan(&ids).Error; err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	sort.Strings(ids)
	return ids
}

// TestPageBlockReferenceJSONBEquivalence 新旧查询结果集必须逐一致。
func TestPageBlockReferenceJSONBEquivalence(t *testing.T) {
	const target, other = "blk-target-0001", "blk-other-0002"
	db, m := setupBlockRefPages(t, target, other)
	ctx := context.Background()

	for _, blockID := range []string{target, other, "12345", "blk-target-0001-miss", ""} {
		t.Run("blockID="+blockID, func(t *testing.T) {
			oldIDs := queryIDs(t, db, oldBlockRefCond+blockRefTailCond, blockID, blockID, blockID)
			newIDs := queryIDs(t, db, newBlockRefCond+blockRefTailCond, blockID, blockID, blockID)
			if strings.Join(oldIDs, ",") != strings.Join(newIDs, ",") {
				t.Fatalf("结果集不一致 | 旧(LIKE)=%v | 新(JSONB)=%v", oldIDs, newIDs)
			}
			count, err := m.CountBlockReference(ctx, blockID)
			if err != nil {
				t.Fatalf("CountBlockReference 失败: %v", err)
			}
			if int(count) != len(oldIDs) {
				t.Fatalf("CountBlockReference=%d 与旧查询命中 %d 不一致", count, len(oldIDs))
			}
			t.Logf("blockID=%q 命中 %d 条，新旧结果集一致", blockID, len(oldIDs))
		})
	}
}

// TestPageMarkStaleForBlockJSONBEquivalence 改写后的 UPDATE 命中集合必须与旧写法一致。
func TestPageMarkStaleForBlockJSONBEquivalence(t *testing.T) {
	const target, other = "blk-target-0001", "blk-other-0002"
	db, m := setupBlockRefPages(t, target, other)
	ctx := context.Background()

	for _, blockID := range []string{target, other, "blk-target-0001-miss"} {
		t.Run("blockID="+blockID, func(t *testing.T) {
			expect := queryIDs(t, db, oldBlockRefCond+blockRefTailCond, blockID, blockID, blockID)
			if err := db.Exec("UPDATE pages SET stale = false").Error; err != nil {
				t.Fatalf("重置 stale 失败: %v", err)
			}
			if err := m.MarkStaleForBlock(ctx, blockID); err != nil {
				t.Fatalf("MarkStaleForBlock 失败: %v", err)
			}
			var got []string
			if err := db.Raw("SELECT id::text FROM pages WHERE stale ORDER BY id::text").Scan(&got).Error; err != nil {
				t.Fatalf("查询 stale 页面失败: %v", err)
			}
			if strings.Join(expect, ",") != strings.Join(got, ",") {
				t.Fatalf("标记集合不一致 | 旧(LIKE)期望=%v | 实际标记=%v", expect, got)
			}
			t.Logf("blockID=%q 标记 %d 条，与旧查询命中集合一致", blockID, len(got))
		})
	}
}

// TestPageBlockReferenceIndexUsed 三个索引必须被 planner 选中（BitmapOr）。
// 索引表达式与查询表达式任一侧改动而未同步时，本测试失败。
func TestPageBlockReferenceIndexUsed(t *testing.T) {
	const target, other = "blk-target-0001", "blk-other-0002"
	db, _ := setupBlockRefPages(t, target, other)

	plan := explainBlockRef(t, db, newBlockRefCond+blockRefTailCond, target, target, target)
	for _, idx := range []string{"idx_pages_blockref", "idx_pages_structure_header", "idx_pages_structure_footer"} {
		if !strings.Contains(plan, idx) {
			t.Fatalf("索引 %s 未被使用，索引与查询表达式可能已漂移 | EXPLAIN: %s", idx, plan)
		}
	}
	if !strings.Contains(plan, "BitmapOr") {
		t.Fatalf("未走 BitmapOr 合并三个索引，OR 形态可能退化为全表扫 | EXPLAIN: %s", plan)
	}
	t.Logf("EXPLAIN（完整 OR 形态）: %s", plan)

	// 反证：旧写法无法走块引用索引（LIKE 无索引可用）。
	oldPlan := explainBlockRef(t, db, oldBlockRefCond+blockRefTailCond, target, target, target)
	if strings.Contains(oldPlan, "idx_pages_blockref") {
		t.Fatalf("旧 LIKE 写法不应命中块引用索引 | EXPLAIN: %s", oldPlan)
	}
	t.Logf("旧写法 EXPLAIN（无索引可用）: %s", oldPlan)
}

// explainBlockRef 取查询的执行计划（COSTS OFF 便于断言索引名）。
func explainBlockRef(t *testing.T, db *gorm.DB, cond string, args ...any) string {
	t.Helper()
	var lines []string
	q := "EXPLAIN (COSTS OFF) SELECT id FROM pages WHERE deleted_at IS NULL AND (" + cond + ")"
	if err := db.Raw(q, args...).Scan(&lines).Error; err != nil {
		t.Fatalf("EXPLAIN 失败: %v", err)
	}
	return strings.Join(lines, " ; ")
}
