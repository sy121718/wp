package unit

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"gorm.io/gorm"

	contentdto "go_wp/internal/module/content/dto"
	presentationdto "go_wp/internal/module/presentation/dto"
	pubcontract "go_wp/internal/module/publication/contract"
)

type routeFaultInjector struct {
	pubcontract.PublicationService
	mu       sync.Mutex
	failing  bool
	failPath string
	calls    int
}

func (r *routeFaultInjector) Activate(ctx context.Context,
	req *pubcontract.ActivateReq) (*pubcontract.RouteResp, error) {
	if r.shouldFail(req) {
		return nil, errors.New("注入故障：路由登记唯一冲突")
	}
	return r.PublicationService.Activate(ctx, req)
}

func (r *routeFaultInjector) ActivateTx(ctx context.Context, tx *gorm.DB,
	req *pubcontract.ActivateReq) (*pubcontract.RouteResp, error) {
	if r.shouldFail(req) {
		return nil, errors.New("注入故障：路由登记唯一冲突")
	}
	return r.PublicationService.ActivateTx(ctx, tx, req)
}

func (r *routeFaultInjector) shouldFail(req *pubcontract.ActivateReq) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	fail := r.failing || (r.failPath != "" && req != nil && strings.TrimSpace(req.Path) == r.failPath)
	r.calls++
	return fail
}

func (r *routeFaultInjector) setFailing(v bool) {
	r.mu.Lock()
	r.failing = v
	r.mu.Unlock()
}

func (r *routeFaultInjector) setFailPath(path string) {
	r.mu.Lock()
	r.failPath = path
	r.mu.Unlock()
}

func enableTwoLangs(t *testing.T, f *presFixture) {
	t.Helper()
	if err := f.db.Exec(
		"INSERT INTO project_locales (project_id, lang, sort_order, is_default, enabled, create_time, update_time) VALUES (?, ?, 0, true, true, now(), now()), (?, ?, 1, false, true, now(), now())",
		f.projectID, "zh-CN", f.projectID, "en-US").Error; err != nil {
		t.Fatalf("写入语言清单失败: %v", err)
	}
}

func TestPresentationRouteRegisterFailureIsRecoverable(t *testing.T) {
	injector := &routeFaultInjector{}
	f := newPresFixtureWithRoutes(t, func(routes pubcontract.PublicationService) pubcontract.PublicationService {
		injector.PublicationService = routes
		return injector
	})
	if f == nil {
		return
	}
	ctx := context.Background()
	enableTwoLangs(t, f)
	f.createTemplate(t)

	entity, err := f.content.Create(ctx, &contentdto.CreateReq{
		EntityType: "article", Slug: "route-fault-shirt",
		Data: map[string]any{"title": "路由故障衬衫", "excerpt": "摘要"},
	})
	if err != nil {
		t.Fatalf("创建实体失败: %v", err)
	}
	logicalPath := "/products/route-fault-shirt"

	injector.setFailing(true)
	if _, err = f.pres.CreateInstance(ctx, &presentationdto.CreateInstanceReq{
		ProjectID: f.projectID, EntityType: "article", EntityID: entity.ID, URLPath: logicalPath,
	}); err == nil {
		t.Fatal("路由登记失败时发布必须返回错误（此前只写 Warn 并返回成功）")
	}
	injector.setFailing(false)

	var instID string
	if err = f.db.Raw("SELECT id FROM presentation_instances WHERE project_id = ? AND entity_id = ?",
		f.projectID, entity.ID).Scan(&instID).Error; err != nil || instID == "" {
		t.Fatalf("实例应已建行（发布失败不等于实例不存在）: instID=%q err=%v", instID, err)
	}

	if html := activeHTML(t, logicalPath); html == "" {
		t.Fatal("默认语言产物应已在访问面上激活")
	}

	if n := countActiveRoutes(t, f, logicalPath); n != 0 {
		t.Fatalf("路由登记已失败，page_routes 不应有 active 行，实际 %d", n)
	}

	if pending, committed := receiptStates(t, f, instID); pending != 2 || committed != 0 {
		t.Fatalf("两种语言的回执都应保持 pending，实际 pending=%d committed=%d", pending, committed)
	}

	_, _, rerr := f.pres.RecoverPendingPublications(ctx)
	if rerr != nil {
		t.Fatalf("发布回执恢复失败: %v", rerr)
	}
	if n := countActiveRoutes(t, f, logicalPath); n != 1 {
		t.Fatalf("恢复后该路径应有且只有 1 条 active 路由行，实际 %d", n)
	}
	if rows := publicationRows(t, f, instID); len(rows) != 2 {
		t.Fatalf("整批重建后两种语言都应有账本行，实际 %+v", rows)
	}

	if pending, committed := receiptStates(t, f, instID); pending != 0 || committed < 1 {
		t.Fatalf("恢复后不应再有未结案回执，实际 pending=%d committed=%d", pending, committed)
	}

	recovered, rolledBack, rerr := f.pres.RecoverPendingPublications(ctx)
	if rerr != nil || recovered != 0 || rolledBack != 0 {
		t.Fatalf("重复恢复应为空操作，实际 recovered=%d rolledBack=%d err=%v", recovered, rolledBack, rerr)
	}
	if n := countActiveRoutes(t, f, logicalPath); n != 1 {
		t.Fatalf("重复恢复后路由行数应保持 1，实际 %d", n)
	}
	if pending, committed := receiptStates(t, f, instID); pending != 0 || committed < 1 {
		t.Fatalf("重复恢复后回执状态不应变化，实际 pending=%d committed=%d", pending, committed)
	}
}

func countActiveRoutes(t *testing.T, f *presFixture, path string) int64 {
	t.Helper()
	var n int64
	if err := f.db.Raw(
		"SELECT count(*) FROM page_routes WHERE project_id = ? AND path = ? AND route_kind = 'active'",
		f.projectID, path).Scan(&n).Error; err != nil {
		t.Fatalf("查询路由行失败: %v", err)
	}
	return n
}

func receiptStates(t *testing.T, f *presFixture, instanceID string) (pending, committed int) {
	t.Helper()
	var rows []string
	if err := f.db.Raw(
		"SELECT receipt_state FROM publication_receipts WHERE source_type = 'presentation' AND source_id = ? AND action = 'switch_active' ORDER BY id",
		instanceID).Scan(&rows).Error; err != nil {
		t.Fatalf("查询发布回执失败: %v", err)
	}
	for _, s := range rows {
		switch s {
		case "pending":
			pending++
		case "committed":
			committed++
		}
	}
	return pending, committed
}
