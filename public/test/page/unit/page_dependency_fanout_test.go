package unit

import (
	"context"
	json "go_wp/pkg/jsonx"
	publish "go_wp/pkg/publish"
	"testing"

	blockdto "go_wp/internal/module/block/dto"
	blockmodel "go_wp/internal/module/block/model"
	blockservice "go_wp/internal/module/block/service"
	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const headerBlockDocument = `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"hdr-1","type":"core.text","props":{"mode":"plaintext","text":"页眉"}}]}`

func contentBoundPage(t *testing.T, svc pagecontract.PageService, projectID, path, entityID string) string {
	t.Helper()
	created, err := svc.Create(context.Background(), &pagedto.CreateReq{
		ProjectID: projectID, Kind: "article", ContentTargetType: "article",
		ContentTargetID: &entityID, DraftPath: path, DraftDocument: []byte(pageDocument),
	})
	if err != nil {
		t.Fatalf("创建内容绑定页面(%s)失败: %v", path, err)
	}
	return created.ID
}

func TestPageDependencyPreciseFanout(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()

	entityA, entityB := uuid.NewString(), uuid.NewString()
	pageA := contentBoundPage(t, svc, projectID, "/products/a", entityA)
	pageB := contentBoundPage(t, svc, projectID, "/products/b", entityB)

	pageHome := createPage(t, svc, projectID, "/home", pageDocument).ID

	for _, id := range []string{pageA, pageB, pageHome} {
		buildAndPublish(t, svc, id)
	}

	assertDependencyRow(t, db, pageA, publish.DepKindDirectContent, "article:"+entityA)
	assertDependencyRow(t, db, pageB, publish.DepKindDirectContent, "article:"+entityB)
	if n := countDependencyRows(t, db, pageHome, publish.DepKindDirectContent); n != 0 {
		t.Fatalf("首页不应声明 direct_content 依赖，实际 %d 条", n)
	}

	ids, err := svc.MarkStaleByDependency(ctx, publish.DepKindDirectContent, "article:"+entityA)
	if err != nil {
		t.Fatalf("MarkStaleByDependency 失败: %v", err)
	}
	if len(ids) != 1 || ids[0] != pageA {
		t.Fatalf("受影响页面应只有 %s，实际 %v", pageA, ids)
	}
	if !pageStale(t, db, pageA) {
		t.Fatalf("依赖命中的页面应 stale=true")
	}
	for _, other := range []string{pageB, pageHome} {
		if pageStale(t, db, other) {
			t.Fatalf("无关页面 %s 被误标记 stale（全站标记退化）", other)
		}
	}

	again, err := svc.MarkStaleByDependency(ctx, publish.DepKindDirectContent, "article:"+entityA)
	if err != nil {
		t.Fatalf("重复标记失败: %v", err)
	}
	if len(again) != 1 || again[0] != pageA {
		t.Fatalf("重复标记应仍只命中 %s，实际 %v", pageA, again)
	}

	none, err := svc.MarkStaleByDependency(ctx, publish.DepKindDirectContent, "article:"+uuid.NewString())
	if err != nil {
		t.Fatalf("未命中查询失败: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("未声明依赖的键不应命中页面，实际 %v", none)
	}
}

func TestPageDependencyBlockAndI18nKeys(t *testing.T) {
	db, svc, projects, projectID := newPageService(t)
	ctx := context.Background()

	header, err := blockservice.NewService(blockmodel.NewBlockModel(db), projects).Create(ctx, &blockdto.CreateReq{
		ProjectID: projectID, Name: "页眉块", Kind: "header",
		Document: json.RawMessage(headerBlockDocument),
	})
	if err != nil {
		t.Fatalf("创建页眉块失败: %v", err)
	}
	blockID := header.ID
	doc := `{"settings":{"layout":{"mode":"full"},"structure":{"headerBlockId":"` + blockID + `"}},"root":[]}`
	page := createPage(t, svc, projectID, "/with-header", doc).ID
	other := createPage(t, svc, projectID, "/plain", pageDocument).ID
	buildAndPublish(t, svc, page)
	buildAndPublish(t, svc, other)

	assertDependencyRow(t, db, page, publish.DepKindBlock, "block:"+blockID)
	assertDependencyRow(t, db, page, publish.DepKindI18N, publish.I18NDependencyKey)
	if n := countDependencyRows(t, db, other, publish.DepKindBlock); n != 0 {
		t.Fatalf("未绑定块的页面不应有 block 依赖，实际 %d 条", n)
	}

	ids, err := svc.MarkStaleByDependency(ctx, publish.DepKindBlock, "block:"+blockID)
	if err != nil {
		t.Fatalf("块依赖反查失败: %v", err)
	}
	if len(ids) != 1 || ids[0] != page {
		t.Fatalf("块变更应只命中绑定该块的页面 %s，实际 %v", page, ids)
	}
	if pageStale(t, db, other) {
		t.Fatalf("未绑定块的页面被误标记 stale")
	}
}

func TestFanoutPrecisionVersusWholeSiteMarking(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()

	entityA := uuid.NewString()
	pageA := contentBoundPage(t, svc, projectID, "/cmp/a", entityA)
	pageB := contentBoundPage(t, svc, projectID, "/cmp/b", uuid.NewString())
	pageHome := createPage(t, svc, projectID, "/cmp/home", pageDocument).ID
	all := []string{pageA, pageB, pageHome}
	for _, id := range all {
		buildAndPublish(t, svc, id)
	}

	if err := svc.MarkStaleForI18n(ctx); err != nil {
		t.Fatalf("MarkStaleForI18n 失败: %v", err)
	}
	wholeSite := 0
	for _, id := range all {
		if pageStale(t, db, id) {
			wholeSite++
		}
	}
	if wholeSite != 3 {
		t.Fatalf("全站标记应命中 3 个页面，实际 %d", wholeSite)
	}

	if err := db.Exec("UPDATE pages SET stale = false").Error; err != nil {
		t.Fatalf("复位 stale 失败: %v", err)
	}
	ids, err := svc.MarkStaleByDependency(ctx, publish.DepKindDirectContent, "article:"+entityA)
	if err != nil {
		t.Fatalf("精确标记失败: %v", err)
	}
	precise := 0
	for _, id := range all {
		if pageStale(t, db, id) {
			precise++
		}
	}
	if len(ids) != 1 || precise != 1 {
		t.Fatalf("精确标记应命中 1 个页面，实际返回 %v / 标记 %d", ids, precise)
	}
	if !pageStale(t, db, pageA) {
		t.Fatalf("精确标记应命中依赖该内容的页面 %s", pageA)
	}
	t.Logf("全站标记命中 %d 个页面 → 精确 fan-out 命中 %d 个页面（无关页面 %s/%s 未被触碰）",
		wholeSite, precise, pageB, pageHome)
}

func TestMarkStaleForI18nStillWholeSite(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()

	pageA := createPage(t, svc, projectID, "/a", pageDocument).ID
	pageB := createPage(t, svc, projectID, "/b", pageDocument).ID
	buildAndPublish(t, svc, pageA)
	buildAndPublish(t, svc, pageB)
	if pageStale(t, db, pageA) || pageStale(t, db, pageB) {
		t.Fatalf("发布后应 stale=false")
	}

	if err := svc.MarkStaleForI18n(ctx); err != nil {
		t.Fatalf("MarkStaleForI18n 失败: %v", err)
	}
	for _, id := range []string{pageA, pageB} {
		if !pageStale(t, db, id) {
			t.Fatalf("MarkStaleForI18n 应把全部页面标记 stale，%s 未标记", id)
		}
	}
}

func assertDependencyRow(t *testing.T, db *gorm.DB, pageID, kind, key string) {
	t.Helper()
	if n := countDependencyRows(t, db, pageID, kind, key); n == 0 {
		t.Fatalf("页面 %s 缺少依赖记录 (%s, %s)", pageID, kind, key)
	}
}

func countDependencyRows(t *testing.T, db *gorm.DB, pageID, kind string, key ...string) int64 {
	t.Helper()
	q := `SELECT COUNT(*) FROM page_dependencies d JOIN pages p ON p.id = d.page_id
		WHERE d.page_id = ? AND d.dependency_kind = ?
		AND d.artifact_id IN (p.active_artifact_id, p.staged_artifact_id)`
	args := []any{pageID, kind}
	if len(key) > 0 {
		q += " AND d.dependency_key = ?"
		args = append(args, key[0])
	}
	var n int64
	if err := db.Raw(q, args...).Scan(&n).Error; err != nil {
		t.Fatalf("统计依赖记录失败: %v", err)
	}
	return n
}

func pageStale(t *testing.T, db *gorm.DB, pageID string) bool {
	t.Helper()
	var stale bool
	if err := db.Raw("SELECT stale FROM pages WHERE id = ?", pageID).Scan(&stale).Error; err != nil {
		t.Fatalf("读取 stale 失败: %v", err)
	}
	return stale
}
