package rlstest

// rls_i18n_content_write_scope_test.go — sys_translation 读写路径的工程作用域（DB-009 第七批）。
//
// 缺口形状是**扫描盲区的盲区**：句柄来自**函数参数**（db *gorm.DB），既不是 model 的 m.db，
// 也不是 DB(ctx)/RevisionDB(ctx) 这类具名方法，两种 grep 都扫不到它。
//
//   · pkg/i18n/content_store.go 的 loadContentTargets：有工程上下文时只把工程 id 填进 SQL 的
//     project_id = $3，**没有设会话变量**。sys_translation 带 FORCE 策略，未设 app.project_id
//     时策略只放行 project_id IS NULL 的全局行 —— 本工程自己的译文读不到。构建期取词于是
//     一律回退全局译法（甚至回退原文），而这条路径**连日志都没有**：取词失败同样回退原文，
//     两者在外部表现上完全一样。
//
//   · pkg/i18n/content_write.go 的 Upsert：写工程行同样没设会话变量。RLS 的 INSERT 违规是
//     **报错**而不是静默 0 行，所以症状是工作台「保存译文失败」。
//
// 断言都具备失败能力：把 model/写入函数里的作用域摘掉，本文件的用例必须变红（实测见报告）。

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

// seedTranslationRow 经**显式作用域**写一行译文；projectID 为空即全局行（project_id IS NULL）。
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

// TestRLS_ContentStoreScopedByProject 构建期取词必须看得见本工程的译文行。
//
// 改造前：工程行的 project_id 条件写在 SQL 里，而策略谓词读的是**会话变量** ——
// 变量没设，工程行一行都不可见，取词静默回落全局译法。
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

	// 1) 工程 A 取词：工程行优先。
	storeA := i18n.NewDBContentStoreForProject(db, pA)
	gotA, err := storeA.LoadTargets(ctx, "en-US", hashes)
	if err != nil {
		t.Fatalf("工程 A 取词失败: %v", err)
	}
	if gotA[key] != "Add to cart（A 自己的）" {
		t.Fatalf("工程 A 应取到自己的译文（工程行优先），实际 %q", gotA[key])
	}

	// 2) 工程 B 取词：本工程没有覆盖 → 回落全局行。
	storeB := i18n.NewDBContentStoreForProject(db, pB)
	gotB, err := storeB.LoadTargets(ctx, "en-US", hashes)
	if err != nil {
		t.Fatalf("工程 B 取词失败: %v", err)
	}
	if gotB[key] != "Add to cart（全局）" {
		t.Fatalf("工程 B 未覆盖时应回落全局行，实际 %q", gotB[key])
	}

	// 3) 隔离：B 绝不能读到 A 的译法（谓词写反 / 作用域没生效时这里红）。
	if gotB[key] == "Add to cart（A 自己的）" {
		t.Fatalf("工程 B 读到了工程 A 的译文行（跨工程泄漏）")
	}

	// 4) 缺口对照：**无工程上下文**的 store 只看得见全局行 —— 这正是改造前
	//    「有工程上下文但没设会话变量」那条路径的可见集合，两者逐字相同。
	unscoped, err := i18n.NewDBContentStore(db).LoadTargets(ctx, "en-US", hashes)
	if err != nil {
		t.Fatalf("全局取词失败: %v", err)
	}
	if unscoped[key] != "Add to cart（全局）" {
		t.Fatalf("无工程上下文应只取到全局行，实际 %q", unscoped[key])
	}
}

// TestRLS_ContentWriterUpsertScopedByProject 工作台写入必须能写本工程的行。
//
// 改造前：无会话变量 ⇒ 策略 WITH CHECK 拒绝工程行 ⇒ Upsert 直接报错
// （sys_translation 的 INSERT 违规是报错，不是静默 0 行）。
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

	// 1) 一行工程行 + 一行全局行，同一批写入：整批成功（分组后各在正确作用域下执行）。
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

	// 2) 工程 A 读回自己的行。
	gotA, err := writer.LoadTargetsForProject(ctx, pA, "en-US", hashesScoped)
	if err != nil {
		t.Fatalf("工程 A 读回失败: %v", err)
	}
	if gotA[keyScoped] != "Buy now" {
		t.Fatalf("工程 A 应读回自己刚写的译文，实际 %q", gotA[keyScoped])
	}

	// 3) 别的工程读不到 A 的行（不是全局行，也没有自己的覆盖）。
	gotB, err := writer.LoadTargetsForProject(ctx, pB, "en-US", hashesScoped)
	if err != nil {
		t.Fatalf("工程 B 读回失败: %v", err)
	}
	if _, ok := gotB[keyScoped]; ok {
		t.Fatalf("工程 B 不应读到工程 A 的译文行，实际 %v", gotB)
	}

	// 4) 覆盖更新仍走 ON CONFLICT（同键重复保存只更新不新增）。
	if _, err = writer.Upsert(ctx, []i18n.ContentWriteItem{{
		ProjectID: pA, SourceHash: i18n.ContentHash(srcScoped), Context: translationContext,
		Lang: "en-US", SourceText: srcScoped, TargetText: "Buy it now", Engine: i18n.ContentEngineManual,
	}}); err != nil {
		t.Fatalf("覆盖更新失败: %v", err)
	}
	// 统计也要在作用域内跑：不设 app.project_id 时策略让任何查询恒 0 行（fail closed），
	// 那会让这条断言失去判据 —— 它本身就得先能看见行。
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

// TestRLS_ContentWriterLoadDetailsScopedByProject 工作台的明细读取（含 engine / update_time）。
//
// 这条路径同时服务「工作台展示」与「保存前的是否变化判定」：读不到本工程行时，
// 展示的是全局译法，判定则会把每一行都当成已变更。
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

	// 无工程上下文：全局行（既有语义，未改造的调用点行为不变）。
	global, err := writer.LoadDetails(ctx, "en-US", hashes)
	if err != nil {
		t.Fatalf("读全局译文明细失败: %v", err)
	}
	if global[key].TargetText != "Out of stock（全局）" {
		t.Fatalf("无工程上下文应取全局行，实际 %q", global[key].TargetText)
	}
}
