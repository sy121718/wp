package rlstest

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"go_wp/pkg/i18n"
	"go_wp/pkg/rls"
)

const translationContext = "core.button.text"

func seedTranslationRow(t *testing.T, db *gorm.DB, projectID, sourceText, target string) {
	t.Helper()
	ctx := context.Background()
	write := func(h *gorm.DB) error {
		var pid any
		if strings.TrimSpace(projectID) != "" {
			pid = projectID
		}
		return h.Exec(insertTranslationSQL,
			pid, i18n.ContentHash(sourceText), translationContext, sourceText, target).Error
	}
	var err error
	if strings.TrimSpace(projectID) == "" {
		err = write(db)
	} else {
		err = rls.InProjectScope(ctx, db, projectID, write)
	}
	if err != nil {
		t.Fatalf("准备译文行失败（RLS 生效时写入必须经作用域）: %v", err)
	}
}

const insertTranslationSQL = "INSERT INTO sys_translation " +
	"(project_id, source_hash, context, lang, source_text, target_text, engine) " +
	"VALUES (?, ?, ?, 'en-US', ?, ?, 'manual')"

func TestRLS_ContentStoreScopedByProject(t *testing.T) {
	db, role := rlsFixture(t)
	resetProjects(t, db, role)
	ctx := context.Background()

	pA, pB := uuid.NewString(), uuid.NewString()
	seedProject(t, db, pA, "工程 A")
	seedProject(t, db, pB, "工程 B")

	const src = "加入购物车"
	seedTranslationRow(t, db, "", src, "Add to cart（全局）")
	seedTranslationRow(t, db, pA, src, "Add to cart（A 自己的）")

	hashes := []string{i18n.ContentHash(src)}
	key := i18n.ContentIndexKey(i18n.ContentHash(src), translationContext)

	storeA := i18n.NewDBContentStoreForProject(db, pA)
	gotA, err := storeA.LoadTargets(ctx, "en-US", hashes)
	if err != nil {
		t.Fatalf("工程 A 取词失败: %v", err)
	}
	if gotA[key] != "Add to cart（A 自己的）" {
		t.Fatalf("工程 A 应取到自己的译文（工程行优先），实际 %q", gotA[key])
	}

	storeB := i18n.NewDBContentStoreForProject(db, pB)
	gotB, err := storeB.LoadTargets(ctx, "en-US", hashes)
	if err != nil {
		t.Fatalf("工程 B 取词失败: %v", err)
	}
	if gotB[key] != "Add to cart（全局）" {
		t.Fatalf("工程 B 未覆盖时应回落全局行，实际 %q", gotB[key])
	}

	if gotB[key] == "Add to cart（A 自己的）" {
		t.Fatalf("工程 B 读到了工程 A 的译文行（跨工程泄漏）")
	}

	unscoped, err := i18n.NewDBContentStore(db).LoadTargets(ctx, "en-US", hashes)
	if err != nil {
		t.Fatalf("全局取词失败: %v", err)
	}
	if unscoped[key] != "Add to cart（全局）" {
		t.Fatalf("无工程上下文应只取到全局行，实际 %q", unscoped[key])
	}
}

func TestRLS_ContentWriterUpsertScopedByProject(t *testing.T) {
	db, role := rlsFixture(t)
	resetProjects(t, db, role)
	ctx := context.Background()

	pA, pB := uuid.NewString(), uuid.NewString()
	seedProject(t, db, pA, "工程 A")
	seedProject(t, db, pB, "工程 B")

	writer := i18n.NewContentWriter(db)
	const srcScoped = "立即购买"
	const srcGlobal = "联系客服"

	written, err := writer.Upsert(ctx, []i18n.ContentWriteItem{
		{
			ProjectID: pA, SourceHash: i18n.ContentHash(srcScoped), Context: translationContext,
			Lang: "en-US", SourceText: srcScoped, TargetText: "Buy now", Engine: i18n.ContentEngineManual,
		},
		{
			SourceHash: i18n.ContentHash(srcGlobal), Context: translationContext,
			Lang: "en-US", SourceText: srcGlobal, TargetText: "Contact us", Engine: i18n.ContentEngineManual,
		},
	})
	if err != nil {
		t.Fatalf("写入译文失败（RLS 生效时写入必须承工程作用域）: %v", err)
	}
	if written != 2 {
		t.Fatalf("应写入 2 条，实际 %d", written)
	}

	hashesScoped := []string{i18n.ContentHash(srcScoped)}
	keyScoped := i18n.ContentIndexKey(i18n.ContentHash(srcScoped), translationContext)

	gotA, err := writer.LoadTargetsForProject(ctx, pA, "en-US", hashesScoped)
	if err != nil {
		t.Fatalf("工程 A 读回失败: %v", err)
	}
	if gotA[keyScoped] != "Buy now" {
		t.Fatalf("工程 A 应读回自己刚写的译文，实际 %q", gotA[keyScoped])
	}

	gotB, err := writer.LoadTargetsForProject(ctx, pB, "en-US", hashesScoped)
	if err != nil {
		t.Fatalf("工程 B 读回失败: %v", err)
	}
	if _, ok := gotB[keyScoped]; ok {
		t.Fatalf("工程 B 不应读到工程 A 的译文行，实际 %v", gotB)
	}

	if _, err = writer.Upsert(ctx, []i18n.ContentWriteItem{{
		ProjectID: pA, SourceHash: i18n.ContentHash(srcScoped), Context: translationContext,
		Lang: "en-US", SourceText: srcScoped, TargetText: "Buy it now", Engine: i18n.ContentEngineManual,
	}}); err != nil {
		t.Fatalf("覆盖更新失败: %v", err)
	}

	var rows int64
	if err = rls.InProjectScope(ctx, db, pA, func(tx *gorm.DB) error {
		return tx.Table("sys_translation").
			Where("project_id = ? AND source_hash = ? AND context = ?",
				pA, i18n.ContentHash(srcScoped), translationContext).Count(&rows).Error
	}); err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if rows != 1 {
		t.Fatalf("同键重复保存应仍只有 1 行，实际 %d 行", rows)
	}
}

func TestRLS_ContentWriterLoadDetailsScopedByProject(t *testing.T) {
	db, role := rlsFixture(t)
	resetProjects(t, db, role)
	ctx := context.Background()

	pA := uuid.NewString()
	seedProject(t, db, pA, "工程 A")

	const src = "库存不足"
	seedTranslationRow(t, db, "", src, "Out of stock（全局）")
	seedTranslationRow(t, db, pA, src, "Insufficient stock（A 自己的）")

	hashes := []string{i18n.ContentHash(src)}
	key := i18n.ContentIndexKey(i18n.ContentHash(src), translationContext)
	writer := i18n.NewContentWriter(db)

	details, err := writer.LoadDetailsForProject(ctx, pA, "en-US", hashes)
	if err != nil {
		t.Fatalf("读工程译文明细失败: %v", err)
	}
	if details[key].TargetText != "Insufficient stock（A 自己的）" {
		t.Fatalf("工程 A 的明细应是自己的译法，实际 %q", details[key].TargetText)
	}
	if details[key].Engine != i18n.ContentEngineManual {
		t.Fatalf("engine 应随明细一并返回，实际 %q", details[key].Engine)
	}

	global, err := writer.LoadDetails(ctx, "en-US", hashes)
	if err != nil {
		t.Fatalf("读全局译文明细失败: %v", err)
	}
	if global[key].TargetText != "Out of stock（全局）" {
		t.Fatalf("无工程上下文应取全局行，实际 %q", global[key].TargetText)
	}
}
