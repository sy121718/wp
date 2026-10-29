package feature

// 购物车已使用数据与显式动作；旧槽位换绑不能把无关JSON组件标记待重建。

import (
	"context"
	json "go_wp/pkg/jsonx"
	"testing"
	"time"

	pagedto "go_wp/internal/module/page/dto"
	pagemodel "go_wp/internal/module/page/model"
)

// slotCartDocument 不含任何槽位或显式页面目标。
const slotCartDocument = `{"settings":{"layout":{"mode":"full"}},"root":[{"type":"core.cartIcon","id":"cart-1","props":{}}]}`

// slotPlainDocument 完全不碰系统页面槽位。
const slotPlainDocument = `{"settings":{"layout":{"mode":"full"}},"root":[{"type":"core.text","id":"t1","props":{"text":"PLAIN-PAGE"}}]}`

func TestSiteSlotChangeDoesNotAffectJSONCart(t *testing.T) {
	db, svc, projectID := newPageService(t)
	ctx := context.Background()

	// page_dependencies 由生产迁移建（迁移 174 已把 site_slot 写进 CHECK 约束的真实枚举）。
	// 手抄建表把枚举冻结在测试里，迁移扩枚举时这里看不到任何变化。

	// 四个无内容目标的 kind（home / search / archive / notFound），互不冲突。
	create := func(kind, path, doc string) *pagedto.PageResp {
		t.Helper()
		page, err := svc.Create(ctx, &pagedto.CreateReq{
			ProjectID: projectID, Kind: kind, ContentTargetType: "none",
			DraftPath: path, DraftDocument: json.RawMessage(doc),
		})
		if err != nil {
			t.Fatalf("创建页面 %s 失败: %v", path, err)
		}
		detail, err := svc.Detail(ctx, &pagedto.DetailReq{ProjectID: projectID, ID: page.ID})
		if err != nil {
			t.Fatalf("查询页面 %s 失败: %v", path, err)
		}
		return detail
	}

	withCart := create("home", "/slot-cart-page", slotCartDocument)
	plain := create("search", "/slot-plain-page", slotPlainDocument)
	cartTarget := create("archive", "/slot-cart-target", slotPlainDocument)
	otherTarget := create("notFound", "/slot-other-target", slotPlainDocument)

	// 先建绑定，再构建：依赖是构建期写入 page_dependencies 的。
	if err := svc.BindSiteSlot(ctx, &pagedto.SiteSlotBindReq{
		ProjectID: projectID, Slot: "cart", PageID: cartTarget.ID,
	}); err != nil {
		t.Fatalf("绑定购物车槽位失败: %v", err)
	}
	for _, id := range []string{withCart.ID, plain.ID} {
		if _, err := svc.Build(ctx, &pagedto.BuildReq{ID: id}); err != nil {
			t.Fatalf("构建页面 %s 失败: %v", id, err)
		}
	}
	// 前置断言：构建之后两个页面都应是干净的，否则后面的断言测不出任何东西。
	for _, d := range []*pagedto.PageResp{withCart, plain} {
		fresh, err := svc.Detail(ctx, &pagedto.DetailReq{ProjectID: projectID, ID: d.ID})
		if err != nil {
			t.Fatalf("复查页面失败: %v", err)
		}
		if fresh.Stale {
			t.Fatalf("构建后页面不应仍为待重建（前置条件不成立，用例无意义）: %s", d.ID)
		}
	}

	// 旧表仍在过渡期，但JSON组件不再消费它。
	if err := svc.BindSiteSlot(ctx, &pagedto.SiteSlotBindReq{
		ProjectID: projectID, Slot: "cart", PageID: otherTarget.ID,
	}); err != nil {
		t.Fatalf("换绑购物车槽位失败: %v", err)
	}

	after, err := svc.Detail(ctx, &pagedto.DetailReq{ProjectID: projectID, ID: withCart.ID})
	if err != nil {
		t.Fatalf("查询消费槽位的页面失败: %v", err)
	}
	if after.Stale {
		t.Fatalf("JSON购物车不依赖旧槽位，不能被旧换绑标记待重建")
	}
	b, err := svc.Detail(ctx, &pagedto.DetailReq{ProjectID: projectID, ID: plain.ID})
	if err != nil {
		t.Fatalf("查询未消费槽位的页面失败: %v", err)
	}
	if b.Stale {
		t.Fatalf("未渲染任何槽位链接的页面不应被标记待重建")
	}
	// 过渡期仍存在旧活跃产物：仅其真实登记的依赖应失效。
	if after.StagedArtifactID == nil {
		t.Fatal("构建必须产生暂存产物以验证真实依赖")
	}
	model := pagemodel.NewPageModel(db)
	if err := model.ReplaceDependencies(ctx, projectID, withCart.ID, *after.StagedArtifactID, []pagemodel.DependencyEntity{{
		PageID: withCart.ID, ArtifactID: *after.StagedArtifactID, DependencyKind: "site_slot", DependencyKey: "cart", LastChecked: time.Now().UTC(),
	}}); err != nil {
		t.Fatal(err)
	}
	if err := svc.BindSiteSlot(ctx, &pagedto.SiteSlotBindReq{ProjectID: projectID, Slot: "cart", PageID: cartTarget.ID}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id    string
		stale bool
	}{{withCart.ID, true}, {plain.ID, false}} {
		detail, err := svc.Detail(ctx, &pagedto.DetailReq{ProjectID: projectID, ID: tc.id})
		if err != nil || detail.Stale != tc.stale {
			t.Fatalf("旧依赖只应影响命中产物: id=%s want=%t detail=%+v err=%v", tc.id, tc.stale, detail, err)
		}
	}
}
