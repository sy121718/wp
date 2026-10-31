package pagemodel_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	pagemodel "go_wp/internal/module/page/model"
	"go_wp/public/test/support"
)

func insertKindPage(t *testing.T, db *gorm.DB, id, projectID, kind, draftPath string, activePath *string, deleted bool) {
	t.Helper()
	targetType := "none"
	var targetID any
	switch kind {
	case "article":
		targetType, targetID = "article", uuid.NewString()
	case "page":
		targetType, targetID = "page", id
	}
	var deletedAt *time.Time
	if deleted {
		at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		deletedAt = &at
	}
	if err := db.Exec(`
		INSERT INTO pages (id, project_id, kind, content_target_type, content_target_id,
			draft_path, active_path, draft_document, draft_version, stale, deleted_at, create_time, update_time)
		VALUES (?, ?, ?, ?, ?, ?, ?, '{}'::jsonb, 1, true, ?, NOW(), NOW())`,
		id, projectID, kind, targetType, targetID, draftPath, activePath, deletedAt).Error; err != nil {
		t.Fatalf("准备页面行失败：%v", err)
	}
}

type pathKindFixture struct {
	m        *pagemodel.Model
	projectA string
	projectB string

	articlePath string
	pagePath    string

	draftOnlyPath    string
	deletedPath      string
	otherProjectPath string
}

func buildPathKindFixture(t *testing.T) *pathKindFixture {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return nil
	}
	fx := &pathKindFixture{
		m:                pagemodel.NewPageModel(db),
		projectA:         uuid.NewString(),
		projectB:         uuid.NewString(),
		articlePath:      "/blog/hello",
		pagePath:         "/about",
		draftOnlyPath:    "/blog/draft-only",
		deletedPath:      "/blog/removed",
		otherProjectPath: "/blog/other",
	}
	support.SeedProjectRow(t, db, fx.projectA, "路径类型工程A")
	support.SeedProjectRow(t, db, fx.projectB, "路径类型工程B")

	insertKindPage(t, db, uuid.NewString(), fx.projectA, "article", fx.articlePath, &fx.articlePath, false)
	insertKindPage(t, db, uuid.NewString(), fx.projectA, "page", fx.pagePath, &fx.pagePath, false)

	insertKindPage(t, db, uuid.NewString(), fx.projectA, "article", fx.draftOnlyPath, nil, false)

	insertKindPage(t, db, uuid.NewString(), fx.projectA, "article", fx.deletedPath, &fx.deletedPath, true)

	insertKindPage(t, db, uuid.NewString(), fx.projectB, "article", fx.otherProjectPath, &fx.otherProjectPath, false)
	return fx
}

func TestKindsOfPathsResolvesPublishedPagesOfThisProject(t *testing.T) {
	fx := buildPathKindFixture(t)
	if fx == nil {
		return
	}
	kinds, err := fx.m.KindsOfPaths(context.Background(), fx.projectA, []string{
		fx.articlePath,
		fx.pagePath,
		fx.draftOnlyPath,
		fx.deletedPath,
		fx.otherProjectPath,
	})
	if err != nil {
		t.Fatalf("反查失败：%v", err)
	}
	if len(kinds) != 2 {
		t.Fatalf("应只命中两条已发布页面，实得 %d 条：%+v", len(kinds), kinds)
	}
	if kinds[fx.articlePath] != "article" {
		t.Errorf("%s 的类型 = %q，期望 article", fx.articlePath, kinds[fx.articlePath])
	}
	if kinds[fx.pagePath] != "page" {
		t.Errorf("%s 的类型 = %q，期望 page", fx.pagePath, kinds[fx.pagePath])
	}

	for _, path := range []string{fx.draftOnlyPath, fx.deletedPath, fx.otherProjectPath} {
		if _, ok := kinds[path]; ok {
			t.Errorf("%s 不该出现在结果里（未发布 / 已删除 / 别的工程）", path)
		}
	}
}

func TestKindsOfPathsACrossProjectQueryStaysEmpty(t *testing.T) {

	fx := buildPathKindFixture(t)
	if fx == nil {
		return
	}
	kinds, err := fx.m.KindsOfPaths(context.Background(), fx.projectB,
		[]string{fx.articlePath, fx.otherProjectPath})
	if err != nil {
		t.Fatalf("反查失败：%v", err)
	}
	if len(kinds) != 1 || kinds[fx.otherProjectPath] != "article" {
		t.Fatalf("工程作用域失效：实得 %+v，期望只含工程 B 的那条", kinds)
	}
}

func TestKindsOfPathsNormalizesInput(t *testing.T) {
	fx := buildPathKindFixture(t)
	if fx == nil {
		return
	}
	ctx := context.Background()

	kinds, err := fx.m.KindsOfPaths(ctx, fx.projectA, []string{
		fx.articlePath, fx.articlePath, "  " + fx.articlePath + "  ", "",
	})
	if err != nil {
		t.Fatalf("反查失败：%v", err)
	}
	if len(kinds) != 1 || kinds[fx.articlePath] != "article" {
		t.Fatalf("重复与空白没有被归一：实得 %+v", kinds)
	}

	empty, err := fx.m.KindsOfPaths(ctx, fx.projectA, nil)
	if err != nil {
		t.Fatalf("空集合不该报错：%v", err)
	}
	if empty == nil || len(empty) != 0 {
		t.Fatalf("空集合应回非 nil 空 map，实得 %#v", empty)
	}
}

func TestKindsOfPathsRequiresProject(t *testing.T) {
	fx := buildPathKindFixture(t)
	if fx == nil {
		return
	}

	if _, err := fx.m.KindsOfPaths(context.Background(), "   ", []string{"/blog/hello"}); err == nil {
		t.Fatal("缺工程 id 应当报错")
	}
}
