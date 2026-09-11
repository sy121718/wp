// page_auto_publish_test.go — PIPE-3：CMS 内容变更 → 精确 fan-out → 自动重建/发布。
//
// 端到端链路（真实 PG + 真实 content/page service + 真实产物盘）：
//
//	contentSvc.Update → invalidator(pipeline.Fanout)
//	  → page.MarkStaleByDependency（只标记依赖该实体的页面）
//	  → page.RebuildStale（按语言构建；此前已发布的语言自动回写线上）
//
// 断言口径：
//   - 受影响的页面被重建并重新发布（pages/page_publications 的 updated_at 前进）；
//   - 无关页面完全不受影响（时间戳不变、stale 不变）；
//   - 产物字节不变时**不产生新的产物行**（Page 文档不内联实体字段，重建幂等）。
package unit

import (
	"context"
	"testing"
	"time"

	contentdto "go_wp/internal/module/content/dto"
	contentmodel "go_wp/internal/module/content/model"
	contentservice "go_wp/internal/module/content/service"
	pagecontract "go_wp/internal/module/page/contract"
	"go_wp/internal/pipeline"

	"gorm.io/gorm"
)

// newContentFanoutPageService 在 page 测试 schema 上装配「content + fanout + page」。
func newContentFanoutPageService(t *testing.T) (*gorm.DB, pagecontract.PageService, *contentservice.Service, string) {
	t.Helper()
	db, svc, _, projectID := newPageService(t)
	contentSvc := contentservice.NewService(contentmodel.NewModel(db))
	fanout := pipeline.NewFanout()
	fanout.Register(pipeline.SourceTypePage, svc)
	fanout.SetRebuilder(pipeline.SourceTypePage, svc)
	contentSvc.SetDependencyInvalidator(fanout)
	return db, svc, contentSvc, projectID
}

// pageUpdatedAt 读取页面行的 updated_at。
func pageUpdatedAt(t *testing.T, db *gorm.DB, pageID string) time.Time {
	t.Helper()
	var at time.Time
	if err := db.Raw("SELECT updated_at FROM pages WHERE id = ?", pageID).Scan(&at).Error; err != nil {
		t.Fatalf("读取 pages.updated_at 失败: %v", err)
	}
	return at
}

// publicationUpdatedAt 读取该语言发布记录的 updated_at（无记录返回零值）。
func publicationUpdatedAt(t *testing.T, db *gorm.DB, pageID, lang string) time.Time {
	t.Helper()
	var at *time.Time
	if err := db.Raw("SELECT updated_at FROM page_publications WHERE page_id = ? AND lang = ?", pageID, lang).Scan(&at).Error; err != nil {
		t.Fatalf("读取 page_publications.updated_at 失败: %v", err)
	}
	if at == nil {
		return time.Time{}
	}
	return *at
}

// countArtifactRows 统计页面产物行数。
func countArtifactRows(t *testing.T, db *gorm.DB, pageID string) int64 {
	t.Helper()
	var n int64
	if err := db.Raw("SELECT COUNT(*) FROM page_artifacts WHERE page_id = ?", pageID).Scan(&n).Error; err != nil {
		t.Fatalf("统计产物行失败: %v", err)
	}
	return n
}

// TestContentChangeAutoPublishChain 改内容 A → 只重建/重发布页面 A。
func TestContentChangeAutoPublishChain(t *testing.T) {
	db, svc, contentSvc, projectID := newContentFanoutPageService(t)
	ctx := context.Background()

	// 两个内容实体 + 各自绑定页面 + 一个无关页面。
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

	// 内容 A 变更 → 自动链路。
	if _, err = contentSvc.Update(ctx, &contentdto.UpdateReq{
		ID: entityA.ID, Data: map[string]any{"title": "自动发布 A v2"},
	}); err != nil {
		t.Fatalf("更新实体 A 失败: %v", err)
	}

	// 1) 受影响页面被重建并重新发布（时间戳前进）。
	if now := pageUpdatedAt(t, db, pageA); !now.After(before[pageA]) {
		t.Fatalf("受影响页面未重建：pages.updated_at 未前进（%v → %v）", before[pageA], now)
	}
	if now := publicationUpdatedAt(t, db, pageA, "zh-CN"); !now.After(pubA) {
		t.Fatalf("受影响页面未重新发布：page_publications.updated_at 未前进（%v → %v）", pubA, now)
	}
	// 2) 自动重建后 stale 收敛为 false（stale → 重建 → 已重建）。
	if pageStale(t, db, pageA) {
		t.Fatalf("自动重建成功后 stale 应为 false")
	}
	// 3) 无关页面完全不受影响。
	if now := pageUpdatedAt(t, db, pageB); !now.Equal(before[pageB]) {
		t.Fatalf("无关页面 B 被误重建（updated_at %v → %v）", before[pageB], now)
	}
	if now := pageUpdatedAt(t, db, pageHome); !now.Equal(before[pageHome]) {
		t.Fatalf("无关页面 home 被误重建（updated_at %v → %v）", before[pageHome], now)
	}
	// 4) 产物字节不变 → 不新增产物行（重建幂等，无产物膨胀）。
	if n := countArtifactRows(t, db, pageA); n != artifactsA {
		t.Fatalf("产物字节未变时不应新增产物行（%d → %d）", artifactsA, n)
	}
}

// TestContentChangeFanoutAffectedSet 扇出返回的受影响集合精确到页面。
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

	// 集合键（实体新增/删除都会触发）不应命中任何页面：测试 schema 里没有
	// 声明集合绑定的组件，即「没有产物真的依赖该集合」——精确 fan-out 不误伤。
	ids, err := svc.MarkStaleByDependency(ctx, pipeline.DepKindContentCollection, pipeline.ContentCollectionKey("article").Key)
	if err != nil {
		t.Fatalf("集合键反查失败: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("无产物声明集合依赖时不应命中页面，实际 %v", ids)
	}

	// 实体键命中绑定该实体的页面。
	ids, err = svc.MarkStaleByDependency(ctx, pipeline.DepKindDirectContent, "article:"+entity.ID)
	if err != nil {
		t.Fatalf("反查失败: %v", err)
	}
	if len(ids) != 1 || ids[0] != page {
		t.Fatalf("受影响集合应只含 %s，实际 %v", page, ids)
	}
}
