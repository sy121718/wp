package unit

import (
	"context"
	publish "go_wp/pkg/publish"
	"testing"
	"time"

	contentdto "go_wp/internal/module/content/dto"
	contentmodel "go_wp/internal/module/content/model"
	contentservice "go_wp/internal/module/content/service"
	pagecontract "go_wp/internal/module/page/contract"

	"gorm.io/gorm"
)

func newContentFanoutPageService(t *testing.T) (*gorm.DB, pagecontract.PageService, *contentservice.Service, string) {
	t.Helper()
	db, svc, _, projectID := newPageService(t)
	contentSvc := contentservice.NewService(contentmodel.NewModel(db))
	fanout := publish.NewFanout()
	fanout.SetSyncRebuild(true)
	fanout.Register(publish.SourceTypePage, svc)
	fanout.SetRebuilder(publish.SourceTypePage, svc)
	contentSvc.SetDependencyInvalidator(fanout)
	return db, svc, contentSvc, projectID
}

func pageUpdatedAt(t *testing.T, db *gorm.DB, pageID string) time.Time {
	t.Helper()
	var at time.Time
	if err := db.Raw("SELECT update_time FROM pages WHERE id = ?", pageID).Scan(&at).Error; err != nil {
		t.Fatalf("读取 pages.update_time 失败: %v", err)
	}
	return at
}

func publicationUpdatedAt(t *testing.T, db *gorm.DB, pageID, lang string) time.Time {
	t.Helper()
	var at *time.Time
	if err := db.Raw("SELECT update_time FROM page_publications WHERE page_id = ? AND lang = ?", pageID, lang).Scan(&at).Error; err != nil {
		t.Fatalf("读取 page_publications.update_time 失败: %v", err)
	}
	if at == nil {
		return time.Time{}
	}
	return *at
}

func countArtifactRows(t *testing.T, db *gorm.DB, pageID string) int64 {
	t.Helper()
	var n int64
	if err := db.Raw("SELECT COUNT(*) FROM page_artifacts WHERE page_id = ?", pageID).Scan(&n).Error; err != nil {
		t.Fatalf("统计产物行失败: %v", err)
	}
	return n
}

func TestContentChangeAutoPublishChain(t *testing.T) {
	db, svc, contentSvc, projectID := newContentFanoutPageService(t)
	ctx := context.Background()

	entityA, err := contentSvc.Create(ctx, &contentdto.CreateReq{
		EntityType: "article", Slug: "auto-a", Data: map[string]any{"title": "自动发布 A"},
	})
	if err != nil {
		t.Fatalf("创建实体 A 失败: %v", err)
	}
	entityB, err := contentSvc.Create(ctx, &contentdto.CreateReq{
		EntityType: "article", Slug: "auto-b", Data: map[string]any{"title": "自动发布 B"},
	})
	if err != nil {
		t.Fatalf("创建实体 B 失败: %v", err)
	}
	pageA := contentBoundPage(t, svc, projectID, "/auto/a", entityA.ID)
	pageB := contentBoundPage(t, svc, projectID, "/auto/b", entityB.ID)
	pageHome := createPage(t, svc, projectID, "/auto/home", pageDocument).ID
	for _, id := range []string{pageA, pageB, pageHome} {
		buildAndPublish(t, svc, id)
	}

	before := map[string]time.Time{
		pageA:    pageUpdatedAt(t, db, pageA),
		pageB:    pageUpdatedAt(t, db, pageB),
		pageHome: pageUpdatedAt(t, db, pageHome),
	}
	pubA := publicationUpdatedAt(t, db, pageA, "zh-CN")
	artifactsA := countArtifactRows(t, db, pageA)
	time.Sleep(5 * time.Millisecond)

	if _, err = contentSvc.Update(ctx, &contentdto.UpdateReq{
		ID: entityA.ID, Data: map[string]any{"title": "自动发布 A v2"},
	}); err != nil {
		t.Fatalf("更新实体 A 失败: %v", err)
	}

	if now := pageUpdatedAt(t, db, pageA); !now.After(before[pageA]) {
		t.Fatalf("受影响页面未重建：pages.update_time 未前进（%v → %v）", before[pageA], now)
	}
	if now := publicationUpdatedAt(t, db, pageA, "zh-CN"); !now.After(pubA) {
		t.Fatalf("受影响页面未重新发布：page_publications.update_time 未前进（%v → %v）", pubA, now)
	}

	if pageStale(t, db, pageA) {
		t.Fatalf("自动重建成功后 stale 应为 false")
	}

	if now := pageUpdatedAt(t, db, pageB); !now.Equal(before[pageB]) {
		t.Fatalf("无关页面 B 被误重建（update_time %v → %v）", before[pageB], now)
	}
	if now := pageUpdatedAt(t, db, pageHome); !now.Equal(before[pageHome]) {
		t.Fatalf("无关页面 home 被误重建（update_time %v → %v）", before[pageHome], now)
	}

	if n := countArtifactRows(t, db, pageA); n != artifactsA {
		t.Fatalf("产物字节未变时不应新增产物行（%d → %d）", artifactsA, n)
	}
}

func TestContentChangeFanoutAffectedSet(t *testing.T) {
	_, svc, contentSvc, projectID := newContentFanoutPageService(t)
	ctx := context.Background()

	entity, err := contentSvc.Create(ctx, &contentdto.CreateReq{
		EntityType: "article", Slug: "fanout-set", Data: map[string]any{"title": "扇出集合"},
	})
	if err != nil {
		t.Fatalf("创建实体失败: %v", err)
	}
	page := contentBoundPage(t, svc, projectID, "/fanout/set", entity.ID)
	other := createPage(t, svc, projectID, "/fanout/other", pageDocument).ID
	buildAndPublish(t, svc, page)
	buildAndPublish(t, svc, other)

	ids, err := svc.MarkStaleByDependency(ctx, publish.DepKindContentCollection, publish.ContentCollectionKey("article").Key)
	if err != nil {
		t.Fatalf("集合键反查失败: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("无产物声明集合依赖时不应命中页面，实际 %v", ids)
	}

	ids, err = svc.MarkStaleByDependency(ctx, publish.DepKindDirectContent, "article:"+entity.ID)
	if err != nil {
		t.Fatalf("反查失败: %v", err)
	}
	if len(ids) != 1 || ids[0] != page {
		t.Fatalf("受影响集合应只含 %s，实际 %v", page, ids)
	}
}
