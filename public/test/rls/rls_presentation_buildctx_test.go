package rlstest

// rls_presentation_buildctx_test.go — 构建上下文里的工程 id（DB-009 第四批）。
//
// 背景：presentation 的 renderHTML 在 core.Compile **之前**就把 ctx 交给实体字段源
//（s.registry.ResolverFor(buildCtx, ...)），而 core.WithBuildProjectID 只在 Compile 内部
// 才补上。原先 buildCtx 只有 WithBuildLang，于是这个「Compile 之前的上下文」里没有工程 id。
//
// 本文件钉住的是这个值的**作用**：工程 id 决定内容译文的作用域 ——
// product 的实体字段源在 Compile 之前就是用它构造取词器
//（internal/module/product/service/entity_source_translate.go 的
// i18n.NewContentTranslatorScoped(ctx, core.BuildProjectID(ctx), ...)）。
// 没有工程 id 时本工程的译文取不到，发布产物里这些字段会回落原文。
//
// 全程非超级角色：sys_translation 带全局策略（project_id IS NULL 或 = 当前）。
// 第七批收口后「工程 id」是**充分**条件 —— 取词路径内部会为它建立会话作用域，
// 所以本文件断言的不再是「还需要作用域」，而是「带上工程 id 就够了」。

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"go_wp/internal/builder/core"
	"go_wp/pkg/i18n"
	"go_wp/pkg/rls"
)

// TestRLS_BuildCtxProjectIDScopesContentTranslation 工程 id 决定内容译文作用域。
func TestRLS_BuildCtxProjectIDScopesContentTranslation(t *testing.T) {
	db, role := rlsFixture(t)
	resetProjects(t, db, role)
	ctx := context.Background()
	pA := uuid.NewString()
	seedProject(t, db, pA, "工程 A")

	const src = "夏季衬衫"
	const ctxName = "product.name"
	hash := i18n.ContentHash(src)
	// 一条**本工程专属**译文（sys_translation.project_id 非空）。
	if err := rls.InProjectScope(ctx, db, pA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO sys_translation (project_id, source_hash, context, lang, source_text, target_text, engine)
			VALUES (?, ?, ?, 'en-US', ?, 'Summer Shirt', 'manual')`,
			pA, hash, ctxName, src).Error
	}); err != nil {
		t.Fatalf("写入本工程译文失败: %v", err)
	}

	// 组合 1：**构建期的真实情形** —— 没有工程作用域、ctx 里也没有工程 id。
	// sys_translation 的策略是「project_id IS NULL 或 = 当前会话变量」，未设变量时只剩全局行。
	plain := i18n.NewContentTranslatorScoped(ctx, "", i18n.NewDBContentStore(db), "en-US", []string{hash})
	if got := plain.TranslateContent(src, ctxName); got != src {
		t.Fatalf("无作用域、无工程 id 时应回落原文，实际 %q", got)
	}

	// 组合 2：只补上工程 id —— **不再需要调用方手工建立作用域**（DB-009 第七批）。
	//
	// 第四批在这里记下的结论是「还不够：作用域不建立时策略照样过滤掉本工程行」，
	// 那条缺口已在第七批收口：有工程上下文的取词路径（content_store.go 的
	// loadContentTargets）自己在事务内设 app.project_id，所以只带工程 id 就能命中本工程行。
	// 本组合因此从「缺口对照」变成正面断言 —— 若有人把取词路径里的作用域摘掉，这里立刻红。
	onlyID := i18n.NewContentTranslatorScoped(ctx, pA, i18n.NewDBContentStore(db), "en-US", []string{hash})
	if got := onlyID.TranslateContent(src, ctxName); got != "Summer Shirt" {
		t.Fatalf("有工程 id 时应命中本工程译文（作用域由取词路径内部建立），实际 %q", got)
	}

	// 组合 3：工程 id + 作用域同时具备 → 命中本工程译文。
	// 构建期的正确形态：service 从 core.BuildProjectID(ctx) 取到工程 id，model 方法内部
	// 用 rls.InProjectScope 建立作用域（page 侧已是这个形状）。
	err := rls.InProjectScope(ctx, db, pA, func(tx *gorm.DB) error {
		scoped := i18n.NewContentTranslatorScoped(ctx, pA, i18n.NewDBContentStore(tx), "en-US", []string{hash})
		if got := scoped.TranslateContent(src, ctxName); got != "Summer Shirt" {
			t.Fatalf("工程 id + 作用域应命中本工程译文，实际 %q", got)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("工程作用域内取词失败: %v", err)
	}

	// 语义护栏：presentation 的 buildCtx 走的正是同一对 API。
	buildCtx := core.WithBuildProjectID(core.WithBuildLang(context.Background(), "en-US"), pA)
	if got := core.BuildProjectID(buildCtx); got != pA {
		t.Fatalf("buildCtx 应带工程 id，实际 %q", got)
	}
	if got := core.BuildLang(buildCtx); got != "en-US" {
		t.Fatalf("buildCtx 应保留语言，实际 %q", got)
	}
}
