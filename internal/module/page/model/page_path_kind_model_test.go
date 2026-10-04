package pagemodel_test

// page_path_kind_model_test.go — 按线上路径批量反查页面类型的真库行为验证。
//
// 为什么不放在纯逻辑单测里：本方法的价值全在 SQL 谓词上 —— 用 active_path 还是 draft_path、
// deleted_at 谓词、project_id 谓词、IN 列表的构造。任何一条写错都不会报错，只会让
// 「哪些路径是文章页」多几条 / 少几条（文章浏览量统计随之偏移），而这类偏差在纯函数测试里看不见。
//
// 表结构来自**生产迁移**（support.NewMigratedPGTestDB），测试只补真实父行（工程行 + 页面行）。

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	pagemodel "go_wp/internal/module/page/model"
	"go_wp/public/test/support"
)

// insertKindPage 插入一条真实 pages 行（content_target_* 按 kind 满足 pages_content_contract_check）。
//
// activePath 为 nil = 该页没发布过（active_path 是 NULL）；deleted 为真 = 已软删。
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

// pathKindFixture 夹具：工程 A 里四条页面（一条 article、一条 page、一条未发布、一条已删）+ 工程 B 的一条。
type pathKindFixture struct {
	m        *pagemodel.Model
	projectA string
	projectB string
	// articlePath / pagePath 是应命中的两条路径。
	articlePath string
	pagePath    string
	// draftOnlyPath 未发布（active_path 为 NULL）、deletedPath 已软删、otherProjectPath 属于工程 B：
	// 三条都应**查不到**，但原因各不相同。
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
	// 未发布：草稿路径存在，active_path 为 NULL。
	insertKindPage(t, db, uuid.NewString(), fx.projectA, "article", fx.draftOnlyPath, nil, false)
	// 已软删：曾发布过（active_path 有值），但从列表里消失。
	insertKindPage(t, db, uuid.NewString(), fx.projectA, "article", fx.deletedPath, &fx.deletedPath, true)
	// 别的工程：路径与查询值完全相同，但作用域不同。
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
	// 三种「查不到」的原因不同，但结果必须一致：不出现。
	for _, path := range []string{fx.draftOnlyPath, fx.deletedPath, fx.otherProjectPath} {
		if _, ok := kinds[path]; ok {
			t.Errorf("%s 不该出现在结果里（未发布 / 已删除 / 别的工程）", path)
		}
	}
}

func TestKindsOfPathsACrossProjectQueryStaysEmpty(t *testing.T) {
	// 用工程 B 反查工程 A 与工程 B 的路径混合列表：只有工程 B 自己那条应命中。
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

	// 重复 + 前后空白：同一路径只算一次，且空白能被剪掉。
	kinds, err := fx.m.KindsOfPaths(ctx, fx.projectA, []string{
		fx.articlePath, fx.articlePath, "  " + fx.articlePath + "  ", "",
	})
	if err != nil {
		t.Fatalf("反查失败：%v", err)
	}
	if len(kinds) != 1 || kinds[fx.articlePath] != "article" {
		t.Fatalf("重复与空白没有被归一：实得 %+v", kinds)
	}

	// 空集合：返回非 nil 的空 map（调用方直接下标取值即可）。
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
	// 缺工程 id 必须报错，而不是静默回空集：静默空集的表现是「一个路径都认不出来」，
	// 统计页面于是显示 0，而没有任何报错可查。
	if _, err := fx.m.KindsOfPaths(context.Background(), "   ", []string{"/blog/hello"}); err == nil {
		t.Fatal("缺工程 id 应当报错")
	}
}
