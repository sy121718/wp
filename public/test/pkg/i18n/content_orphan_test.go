package i18n_test

// content_orphan_test.go — sys_translation 孤儿行的检出与显式清理（审计 I18N-024）
// 在真实 PostgreSQL 上的验证。
//
// 覆盖：
//  1. 判据 A（hash 与原文不符）在没有候选集合时即可检出；
//  2. 判据 B（源引用消失）只在提供候选集合后生效；
//  3. 全局行（project_id IS NULL）永不进入检出/清理范围；
//  4. 清理是显式操作：dry run 不删任何行；apply 才删，且返回「检出 N 行、清理 M 行」；
//  5. 删除只命中 (source_hash, context, lang) 元组，不波及同原文的其它语境、
//     其它语言的译文，也不波及别的工程；
//  6. 空候选集合按「未提供」处理（不启用判据 B），工程 id 为空直接拒绝。

import (
	"errors"
	"strings"
	"testing"

	"go_wp/pkg/i18n"
)

// TestContentOrphanScanAndPurge 孤儿检出与清理的完整链路。
func TestContentOrphanScanAndPurge(t *testing.T) {
	db := newProjectScopeDB(t)
	insertTestProject(t, db, scopeProjectA, "工程A")
	insertTestProject(t, db, scopeProjectB, "工程B")
	ctx := t.Context()
	writer := i18n.NewContentWriter(db)

	const btnContext = "core.button.text"
	const headContext = "core.heading.text"
	keepSrc, dropSrc, brokenSrc, globalSrc := "继续购物", "临时下架文案", "历史旧文案", "全局文案"
	keepKey := i18n.ContentIndexKey(i18n.ContentHash(keepSrc), btnContext)

	// 1) 工程 A：一条仍在候选里的行、一条失引行。
	if _, err := writer.Upsert(ctx, []i18n.ContentWriteItem{
		{ProjectID: scopeProjectA, SourceHash: i18n.ContentHash(keepSrc), Context: btnContext, Lang: "en-US", SourceText: keepSrc, TargetText: "Continue shopping"},
		{ProjectID: scopeProjectA, SourceHash: i18n.ContentHash(dropSrc), Context: headContext, Lang: "en-US", SourceText: dropSrc, TargetText: "Sold out soon"},
	}); err != nil {
		t.Fatalf("写入工程 A 译文失败: %v", err)
	}
	// 2) 破损行：hash 与原文不符。写入口会拒绝这种行（第一条硬约束），
	//    因此这里直接 INSERT 构造历史脏数据 —— 正是判据 A 要捡出来的东西。
	brokenHash := strings.Repeat("0", 64)
	if err := db.Exec("INSERT INTO sys_translation (project_id, source_hash, context, lang, source_text, target_text) VALUES (?::uuid, ?, ?, ?, ?, ?)",
		scopeProjectA, brokenHash, headContext, "en-US", brokenSrc, "Legacy text").Error; err != nil {
		t.Fatalf("构造破损行失败: %v", err)
	}
	// 3) 全局行 + 工程 B 的同 key 行（都不该被工程 A 的清理波及）。
	if _, err := writer.Upsert(ctx, []i18n.ContentWriteItem{
		{SourceHash: i18n.ContentHash(globalSrc), Context: btnContext, Lang: "en-US", SourceText: globalSrc, TargetText: "Global text"},
		{ProjectID: scopeProjectB, SourceHash: i18n.ContentHash(keepSrc), Context: btnContext, Lang: "en-US", SourceText: keepSrc, TargetText: "B continue"},
		{ProjectID: scopeProjectA, SourceHash: i18n.ContentHash(keepSrc), Context: btnContext, Lang: "zh-TW", SourceText: keepSrc, TargetText: "繼續購物"},
	}); err != nil {
		t.Fatalf("写入对照行失败: %v", err)
	}

	// 4) 没有候选集合：只跑判据 A（安全默认）。
	brokens, err := writer.ScanOrphans(ctx, i18n.OrphanScope{ProjectID: scopeProjectA})
	if err != nil {
		t.Fatalf("扫描孤儿失败: %v", err)
	}
	if brokens.KeptKeys {
		t.Fatal("未提供候选集合时不应启用判据 B")
	}
	if brokens.Broken != 1 || brokens.Unreferenced != 0 || brokens.Orphans() != 1 {
		t.Fatalf("只应检出 1 条 hash 破损行，实际 broken=%d unreferenced=%d", brokens.Broken, brokens.Unreferenced)
	}
	if len(brokens.Rows) != 1 || brokens.Rows[0].Reason != i18n.OrphanReasonBrokenHash {
		t.Fatalf("破损行判据应为 %s，实际 %+v", i18n.OrphanReasonBrokenHash, brokens.Rows)
	}

	// 5) 提供候选集合：破损 + 失引，共 2 条。
	scope := i18n.OrphanScope{ProjectID: scopeProjectA, Keep: map[string]bool{keepKey: true}}
	report, err := writer.ScanOrphans(ctx, scope)
	if err != nil {
		t.Fatalf("带候选集合扫描失败: %v", err)
	}
	if !report.KeptKeys {
		t.Fatal("提供了候选集合时应启用判据 B")
	}
	if report.Broken != 1 || report.Unreferenced != 1 || report.Orphans() != 2 {
		t.Fatalf("应检出 2 条孤儿（1 破损 + 1 失引），实际 broken=%d unreferenced=%d", report.Broken, report.Unreferenced)
	}
	// 全局行与「仍在候选里」的行都不在检出结果中。
	for _, row := range report.Rows {
		if row.SourceText == globalSrc || (row.SourceText == keepSrc && row.Lang == "en-US") {
			t.Fatalf("不该被检出的行出现在结果里: %+v", row)
		}
	}

	// 6) dry run：检出 2 行、清理 0 行，且库里一行不少。
	totalBefore := countTranslationRows(t, db)
	dry, err := writer.PurgeOrphans(ctx, scope, false)
	if err != nil {
		t.Fatalf("dry run 失败: %v", err)
	}
	if !dry.DryRun || dry.Detected != 2 || dry.Deleted != 0 {
		t.Fatalf("dry run 应为「检出 2 行、清理 0 行」，实际 %+v", dry)
	}
	if text := dry.OrphanPurgeText(); !strings.Contains(text, "检出 2 行") || !strings.Contains(text, "清理 0 行") {
		t.Fatalf("dry run 文案缺少计数: %q", text)
	}
	if after := countTranslationRows(t, db); after != totalBefore {
		t.Fatalf("dry run 不应删除任何行: %d -> %d", totalBefore, after)
	}

	// 7) 显式 apply：清理 2 行，其余行原样保留。
	applied, err := writer.PurgeOrphans(ctx, scope, true)
	if err != nil {
		t.Fatalf("执行清理失败: %v", err)
	}
	if applied.DryRun || applied.Detected != 2 || applied.Deleted != 2 {
		t.Fatalf("应「检出 2 行、清理 2 行」，实际 %+v", applied)
	}
	if text := applied.OrphanPurgeText(); !strings.Contains(text, "检出 2 行") || !strings.Contains(text, "清理 2 行") {
		t.Fatalf("清理文案缺少计数: %q", text)
	}
	// 全局行 + 工程 A 的 en-US 在册行 + 工程 A 的 zh-TW 行 + 工程 B 的行 = 4。
	if left := countTranslationRows(t, db); left != 4 {
		t.Fatalf("清理后应剩 4 行（全局 / 在册 / 另一语言 / 另一工程），实际 %d", left)
	}
	if got := loadOneProject(t, writer, scopeProjectA, i18n.ContentHash(keepSrc)); got != "Continue shopping" {
		t.Fatalf("在册译文不该被清掉，实际 %q", got)
	}
	if got := loadOneProject(t, writer, scopeProjectB, i18n.ContentHash(keepSrc)); got != "B continue" {
		t.Fatalf("别的工程的译文不该被清掉，实际 %q", got)
	}

	// 8) 边界：空候选集合不启用判据 B；工程 id 为空直接拒绝。
	emptyKeep, err := writer.ScanOrphans(ctx, i18n.OrphanScope{ProjectID: scopeProjectA, Keep: map[string]bool{}})
	if err != nil {
		t.Fatalf("空候选集合扫描失败: %v", err)
	}
	if emptyKeep.KeptKeys || emptyKeep.Unreferenced != 0 {
		t.Fatalf("空候选集合应被视为「未提供」（否则会清空整个工程），实际 %+v", emptyKeep)
	}
	if _, err := writer.ScanOrphans(ctx, i18n.OrphanScope{}); !errors.Is(err, i18n.ErrOrphanProjectEmpty) {
		t.Fatalf("未指定工程应返回 ErrOrphanProjectEmpty，实际 %v", err)
	}
	if _, err := writer.PurgeOrphans(ctx, i18n.OrphanScope{}, true); !errors.Is(err, i18n.ErrOrphanProjectEmpty) {
		t.Fatalf("未指定工程不得执行清理，实际 %v", err)
	}
}
