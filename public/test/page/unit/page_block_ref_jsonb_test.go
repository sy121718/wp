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

const blockRefProjectID = "9f2c1d40-0000-4000-8000-000000000068"

const oldBlockRefCond = `draft_document::text LIKE '%"blockId": "' || ?::text || '"%'`

const newBlockRefCond = `jsonb_path_query_array(draft_document, '$.**.blockId') @> jsonb_build_array(?::text)`

const blockRefTailCond = ` OR draft_document->'settings'->'structure'->>'headerBlockId' = ?::text
	OR draft_document->'settings'->'structure'->>'footerBlockId' = ?::text`

type blockRefDocCase struct {
	name    string
	doc     string
	deleted bool
}

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

func setupBlockRefPages(t *testing.T, target, other string) (*gorm.DB, pagemodel.Model) {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)

	support.SeedProjectRow(t, db, blockRefProjectID, "块引用回归站点")

	if err := db.Exec(`INSERT INTO pages
		(id, project_id, kind, content_target_type, draft_path, draft_document, draft_version, stale, create_time, update_time)
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
			(id, project_id, kind, content_target_type, draft_path, draft_document, draft_version, stale, deleted_at, create_time, update_time)
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
			count, err := m.CountBlockReference(ctx, blockRefProjectID, blockID)
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
			if _, err := m.MarkStaleForBlock(ctx, blockRefProjectID, blockID); err != nil {
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

	oldPlan := explainBlockRef(t, db, oldBlockRefCond+blockRefTailCond, target, target, target)
	if strings.Contains(oldPlan, "idx_pages_blockref") {
		t.Fatalf("旧 LIKE 写法不应命中块引用索引 | EXPLAIN: %s", oldPlan)
	}
	t.Logf("旧写法 EXPLAIN（无索引可用）: %s", oldPlan)
}

func explainBlockRef(t *testing.T, db *gorm.DB, cond string, args ...any) string {
	t.Helper()
	var lines []string
	q := "EXPLAIN (COSTS OFF) SELECT id FROM pages WHERE deleted_at IS NULL AND (" + cond + ")"
	if err := db.Raw(q, args...).Scan(&lines).Error; err != nil {
		t.Fatalf("EXPLAIN 失败: %v", err)
	}
	return strings.Join(lines, " ; ")
}
