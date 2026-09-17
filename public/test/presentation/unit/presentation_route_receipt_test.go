// presentation_route_receipt_test.go — 多语言发布的路由登记失败必须失败而非静默成功（审计 AR2-004）。
//
// 缺陷原状：publishAllLangs 里 registerRoute 失败只写一条 Warn 然后继续，函数照常
// 返回成功 —— URL 文件已可访问而 page_routes 缺行，后续占用预检 / 回滚 / 删除 / GC
// 依据错误的路由表决策，留下无法管理的线上路径。
//
// 本用例做三件事：
//  1. 注入「路由登记失败」（Activate 返回错误），断言发布**不再静默成功**；
//  2. 断言留痕的是可恢复状态：访问面已激活 + 发布回执 pending + 实例无活跃指针；
//  3. 恢复故障后跑启动恢复，断言路由被幂等补齐、回执结案，且重复执行不产生重复行。
package unit

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	contentdto "go_wp/internal/module/content/dto"
	presentationdto "go_wp/internal/module/presentation/dto"
	pubcontract "go_wp/internal/module/publication/contract"
	"go_wp/pkg/i18n"
)

// routeFaultInjector 路由契约的故障注入包装：Activate 可被切换为「总是失败」。
// 其余方法（含发布回执的登记 / 结案 / 扫描）逐字透传 —— 注入点只落在路由登记这一步，
// 这样「访问面已激活、路由未登记」才是真实场景而不是人为拼接的中间态。
type routeFaultInjector struct {
	pubcontract.PublicationService
	mu       sync.Mutex
	failing  bool
	failPath string
	calls    int
}

func (r *routeFaultInjector) Activate(ctx context.Context,
	req *pubcontract.ActivateReq) (*pubcontract.RouteResp, error) {
	r.mu.Lock()
	fail := r.failing || (r.failPath != "" && req != nil && strings.TrimSpace(req.Path) == r.failPath)
	r.calls++
	r.mu.Unlock()
	if fail {
		return nil, errors.New("注入故障：路由登记唯一冲突")
	}
	return r.PublicationService.Activate(ctx, req)
}

func (r *routeFaultInjector) setFailing(v bool) {
	r.mu.Lock()
	r.failing = v
	r.mu.Unlock()
}

// setFailPath 只让指定路径的登记失败（多语言用例里模拟「某一种语言发布失败」）。
func (r *routeFaultInjector) setFailPath(path string) {
	r.mu.Lock()
	r.failPath = path
	r.mu.Unlock()
}

// enableTwoLangs 写入 zh-CN（默认）+ en-US 两语言清单。
func enableTwoLangs(t *testing.T, f *presFixture) {
	t.Helper()
	if err := f.db.Exec(
		"INSERT INTO project_locales (project_id, lang, sort_order, is_default, enabled, create_time, update_time) VALUES (?, ?, 0, true, true, now(), now()), (?, ?, 1, false, true, now(), now())",
		f.projectID, "zh-CN", f.projectID, "en-US").Error; err != nil {
		t.Fatalf("写入语言清单失败: %v", err)
	}
}

// TestPresentationRouteRegisterFailureIsRecoverable 路由登记失败 → 发布失败 + 可恢复状态 + 恢复补齐。
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
	i18n.SetSiteLangURLMode(i18n.SiteLangURLModeDefaultPlain)
	t.Cleanup(func() { i18n.SetSiteLangURLMode(i18n.SiteLangURLModeDefaultPlain) })
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

	// 故障注入：第一个语言的 registerRoute 就失败（访问面切换在它之前已经发生）。
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

	// 1) 访问面确实已切换：默认语言文件可读（这正是「不能静默成功」的由来）。
	if html := activeHTML(t, logicalPath); html == "" {
		t.Fatal("默认语言产物应已在访问面上激活")
	}
	// 2) 路由账本缺行。
	if n := countActiveRoutes(t, f, logicalPath); n != 0 {
		t.Fatalf("路由登记已失败，page_routes 不应有 active 行，实际 %d", n)
	}
	// 3) 留痕的是可恢复状态：回执 pending。
	if pending, committed := receiptStates(t, f, instID); pending != 1 || committed != 0 {
		t.Fatalf("应恰好一条 pending 回执，实际 pending=%d committed=%d", pending, committed)
	}
	// 实例活跃指针不在本用例的断言范围：AR2-004 只管「路由登记失败不再被吞掉」，
	// 「指针只在整批成功后才前进」是 AR2-003 的收口（见 presentation_i18n_partial_test.go）。

	// 恢复：故障消除后启动恢复按访问面实际指向补齐路由登记。
	recovered, rolledBack, rerr := f.pres.RecoverPendingPublications(ctx)
	if rerr != nil {
		t.Fatalf("发布回执恢复失败: %v", rerr)
	}
	if recovered != 1 || rolledBack != 0 {
		t.Fatalf("应补齐 1 条回执，实际 recovered=%d rolledBack=%d", recovered, rolledBack)
	}
	if n := countActiveRoutes(t, f, logicalPath); n != 1 {
		t.Fatalf("恢复后该路径应有且只有 1 条 active 路由行，实际 %d", n)
	}
	// 恢复分两段：先按访问面证据补齐这条回执，再因为语言账本没铺满而重跑整批
	// （AR2-003 的批次收敛），因此 committed 会多于 1 条 —— 关键是没有 pending 残留。
	if pending, committed := receiptStates(t, f, instID); pending != 0 || committed < 1 {
		t.Fatalf("恢复后不应再有未结案回执，实际 pending=%d committed=%d", pending, committed)
	}

	// 幂等：重复执行恢复不产生重复路由行，也不再有任何待处理回执。
	recovered, rolledBack, rerr = f.pres.RecoverPendingPublications(ctx)
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

// countActiveRoutes 该路径上归属本实例的 active 路由行数。
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

// receiptStates 该实例的访问面切换回执状态计数（pending / committed）。
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
