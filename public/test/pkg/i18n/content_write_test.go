package i18n_test

// content_write_test.go — sys_translation 写入端口（多语言 P5c，docs/06-D §7.8）在真实
// PostgreSQL 上的链路验证。
//
// 覆盖：
//  1. 写入 + 读回（LoadTargets 复用 P5a 读路径；LoadDetails 额外取 engine/update_time）；
//  2. hash 一致性：sha256(source_text) != source_hash → 拒绝且不落库（066 只校验格式）；
//  3. 幂等：同一条重复写入 → 仍一行（ON CONFLICT 更新），update_time 推进；
//  4. 非法输入：空译文 / 非法 engine / 空语言 / 空语境 → 拒绝；
//  5. 批量原子性：一批中任一条非法 → 整体不写（工作台不出现「部分成功」）。
//
// PG 不可用时 t.Skip（与其他功能测试一致）。

import (
	"testing"
	"time"

	"go_wp/pkg/i18n"
	"go_wp/public/test/support"

	"gorm.io/gorm"
)

// newContentWriteDB 建隔离 schema + 跑全量迁移（sys_translation 落库）。
func newContentWriteDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	return db
}

// countTranslationRows 统计表内行数（断言「未落库」）。
func countTranslationRows(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var n int64
	if err := db.Raw("SELECT COUNT(*) FROM sys_translation").Scan(&n).Error; err != nil {
		t.Fatalf("统计行数失败: %v", err)
	}
	return n
}

// TestContentWriterUpsertAndLoad 写入后按 (hash, lang) 读回，engine 与译文一致。
func TestContentWriterUpsertAndLoad(t *testing.T) {
	db := newContentWriteDB(t)
	writer := i18n.NewContentWriter(db)

	srcA, srcB := "了解更多", "关于我们"
	written, err := writer.Upsert(t.Context(), []i18n.ContentWriteItem{
		{SourceHash: i18n.ContentHash(srcA), Context: "core.button.text", Lang: "en-US", SourceText: srcA, TargetText: "Learn more"},
		{SourceHash: i18n.ContentHash(srcB), Context: "core.heading.text", Lang: "en-US", SourceText: srcB, TargetText: "About Us", Engine: i18n.ContentEngineAI},
	})
	if err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if written != 2 {
		t.Fatalf("写入条数应为 2，实际 %d", written)
	}

	targets, err := writer.LoadTargets(t.Context(), "en-US", []string{i18n.ContentHash(srcA), i18n.ContentHash(srcB)})
	if err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	if got := targets[i18n.ContentIndexKey(i18n.ContentHash(srcA), "core.button.text")]; got != "Learn more" {
		t.Fatalf("core.button.text 译文应为 Learn more，实际 %q", got)
	}
	if got := targets[i18n.ContentIndexKey(i18n.ContentHash(srcB), "core.heading.text")]; got != "About Us" {
		t.Fatalf("core.heading.text 译文应为 About Us，实际 %q", got)
	}

	details, err := writer.LoadDetails(t.Context(), "en-US", []string{i18n.ContentHash(srcA), i18n.ContentHash(srcB)})
	if err != nil {
		t.Fatalf("读明细失败: %v", err)
	}
	if d := details[i18n.ContentIndexKey(i18n.ContentHash(srcA), "core.button.text")]; d.Engine != i18n.ContentEngineManual {
		t.Fatalf("未指定 engine 应落 manual，实际 %q", d.Engine)
	}
	if d := details[i18n.ContentIndexKey(i18n.ContentHash(srcB), "core.heading.text")]; d.Engine != i18n.ContentEngineAI {
		t.Fatalf("显式 engine=ai 应保留，实际 %q", d.Engine)
	}
	// 不同语言互不影响。
	other, err := writer.LoadTargets(t.Context(), "ja", []string{i18n.ContentHash(srcA)})
	if err != nil {
		t.Fatalf("读其它语言失败: %v", err)
	}
	if len(other) != 0 {
		t.Fatalf("ja 不应有译文，实际 %+v", other)
	}
}

// TestContentWriterRejectsHashMismatch 原文与 source_hash 不一致 → 拒绝且不落库。
func TestContentWriterRejectsHashMismatch(t *testing.T) {
	db := newContentWriteDB(t)
	writer := i18n.NewContentWriter(db)

	_, err := writer.Upsert(t.Context(), []i18n.ContentWriteItem{{
		SourceHash: i18n.ContentHash("另一段原文"), Context: "core.button.text", Lang: "en-US",
		SourceText: "了解更多", TargetText: "Learn more",
	}})
	if err != i18n.ErrContentHashMismatch {
		t.Fatalf("应返回 ErrContentHashMismatch，实际 %v", err)
	}
	if n := countTranslationRows(t, db); n != 0 {
		t.Fatalf("校验失败不应落库，实际 %d 行", n)
	}
}

// TestContentWriterIdempotentUpsert 同一条重复写入 → 仍一行，内容更新，update_time 推进。
func TestContentWriterIdempotentUpsert(t *testing.T) {
	db := newContentWriteDB(t)
	writer := i18n.NewContentWriter(db)
	source := "了解更多"
	item := i18n.ContentWriteItem{
		SourceHash: i18n.ContentHash(source), Context: "core.button.text", Lang: "en-US",
		SourceText: source, TargetText: "Learn more",
	}

	if _, err := writer.Upsert(t.Context(), []i18n.ContentWriteItem{item}); err != nil {
		t.Fatalf("首次写入失败: %v", err)
	}
	var firstUpdated time.Time
	if err := db.Raw("SELECT update_time FROM sys_translation").Scan(&firstUpdated).Error; err != nil {
		t.Fatalf("读取 update_time 失败: %v", err)
	}
	time.Sleep(10 * time.Millisecond)

	item.TargetText = "Find out more"
	if _, err := writer.Upsert(t.Context(), []i18n.ContentWriteItem{item}); err != nil {
		t.Fatalf("二次写入失败: %v", err)
	}
	if n := countTranslationRows(t, db); n != 1 {
		t.Fatalf("幂等写入应仍为 1 行，实际 %d 行", n)
	}
	var secondUpdated time.Time
	var target string
	if err := db.Raw("SELECT update_time, target_text FROM sys_translation").Row().Scan(&secondUpdated, &target); err != nil {
		t.Fatalf("读取二次写入结果失败: %v", err)
	}
	if target != "Find out more" {
		t.Fatalf("ON CONFLICT 应覆盖译文，实际 %q", target)
	}
	if !secondUpdated.After(firstUpdated) {
		t.Fatalf("重复写入应推进 update_time（ContentRevision 依赖）: first=%s second=%s", firstUpdated, secondUpdated)
	}
}

// TestContentWriterRejectsInvalid 非法输入拒绝（空译文 / 非法 engine / 空语言 / 空语境）。
func TestContentWriterRejectsInvalid(t *testing.T) {
	db := newContentWriteDB(t)
	writer := i18n.NewContentWriter(db)
	hash := i18n.ContentHash("了解更多")

	for _, tc := range []struct {
		name string
		item i18n.ContentWriteItem
		want error
	}{
		{"空译文", i18n.ContentWriteItem{SourceHash: hash, Context: "core.button.text", Lang: "en-US", SourceText: "了解更多", TargetText: "   "}, i18n.ErrContentTargetEmpty},
		{"非法 engine", i18n.ContentWriteItem{SourceHash: hash, Context: "core.button.text", Lang: "en-US", SourceText: "了解更多", TargetText: "Learn more", Engine: "machine"}, i18n.ErrContentEngineInvalid},
		{"空语言", i18n.ContentWriteItem{SourceHash: hash, Context: "core.button.text", SourceText: "了解更多", TargetText: "Learn more"}, i18n.ErrContentLangEmpty},
		{"空语境", i18n.ContentWriteItem{SourceHash: hash, Lang: "en-US", SourceText: "了解更多", TargetText: "Learn more"}, i18n.ErrContentContextEmpty},
	} {
		if _, err := writer.Upsert(t.Context(), []i18n.ContentWriteItem{tc.item}); err != tc.want {
			t.Fatalf("%s 应返回 %v，实际 %v", tc.name, tc.want, err)
		}
	}
	if n := countTranslationRows(t, db); n != 0 {
		t.Fatalf("非法输入不应落库，实际 %d 行", n)
	}
}

// TestContentWriterBatchAllOrNothing 批量中任一条非法 → 整体不写。
func TestContentWriterBatchAllOrNothing(t *testing.T) {
	db := newContentWriteDB(t)
	writer := i18n.NewContentWriter(db)
	source := "了解更多"

	_, err := writer.Upsert(t.Context(), []i18n.ContentWriteItem{
		{SourceHash: i18n.ContentHash(source), Context: "core.button.text", Lang: "en-US", SourceText: source, TargetText: "Learn more"},
		{SourceHash: i18n.ContentHash("关于我们"), Context: "core.heading.text", Lang: "en-US", SourceText: "被改过的原文", TargetText: "About Us"},
	})
	if err != i18n.ErrContentHashMismatch {
		t.Fatalf("应返回 ErrContentHashMismatch，实际 %v", err)
	}
	if n := countTranslationRows(t, db); n != 0 {
		t.Fatalf("批量写入应整体回滚（先校验后写），实际 %d 行", n)
	}
}

// TestParseContentContext 语境拆解：按最后一个点切分，缺段返回 false。
func TestParseContentContext(t *testing.T) {
	typ, field, ok := i18n.ParseContentContext("core.button.text")
	if !ok || typ != "core.button" || field != "text" {
		t.Fatalf("拆解异常: %q / %q / %v", typ, field, ok)
	}
	if typ, field, ok = i18n.ParseContentContext("product.seoTitle"); !ok || typ != "product" || field != "seoTitle" {
		t.Fatalf("拆解异常: %q / %q / %v", typ, field, ok)
	}
	for _, bad := range []string{"", "core", ".text", "core.", "  "} {
		if _, _, ok := i18n.ParseContentContext(bad); ok {
			t.Fatalf("%q 应拆解失败", bad)
		}
	}
}
