package rlstest

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"go_wp/pkg/builder/core"
	"go_wp/pkg/i18n"
	"go_wp/pkg/rls"
)

func TestRLS_BuildCtxProjectIDScopesContentTranslation(t *testing.T) {
	db, role := rlsFixture(t)
	resetProjects(t, db, role)
	ctx := context.Background()
	pA := uuid.NewString()
	seedProject(t, db, pA, "工程 A")

	const src = "夏季衬衫"
	const ctxName = "product.name"
	hash := i18n.ContentHash(src)

	if err := rls.InProjectScope(ctx, db, pA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO sys_translation (project_id, source_hash, context, lang, source_text, target_text, engine)
			VALUES (?, ?, ?, 'en-US', ?, 'Summer Shirt', 'manual')`,
			pA, hash, ctxName, src).Error
	}); err != nil {
		t.Fatalf("写入本工程译文失败: %v", err)
	}

	plain := i18n.NewContentTranslatorScoped(ctx, "", i18n.NewDBContentStore(db), "en-US", []string{hash})
	if got := plain.TranslateContent(src, ctxName); got != src {
		t.Fatalf("无作用域、无工程 id 时应回落原文，实际 %q", got)
	}

	onlyID := i18n.NewContentTranslatorScoped(ctx, pA, i18n.NewDBContentStore(db), "en-US", []string{hash})
	if got := onlyID.TranslateContent(src, ctxName); got != "Summer Shirt" {
		t.Fatalf("有工程 id 时应命中本工程译文（作用域由取词路径内部建立），实际 %q", got)
	}

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

	buildCtx := core.WithBuildProjectID(core.WithBuildLang(context.Background(), "en-US"), pA)
	if got := core.BuildProjectID(buildCtx); got != pA {
		t.Fatalf("buildCtx 应带工程 id，实际 %q", got)
	}
	if got := core.BuildLang(buildCtx); got != "en-US" {
		t.Fatalf("buildCtx 应保留语言，实际 %q", got)
	}
}
