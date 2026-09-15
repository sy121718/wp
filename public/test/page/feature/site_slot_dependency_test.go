package feature

// site_slot_dependency_test.go — 槽位换绑的精确失效范围（审计 VIS-006）。
//
// 此前 page_slot.go 的绑定/解绑无条件 MarkStaleForProject（整个工程）——
// 大站点上「改一次结算页绑定」等于全站重建。精确范围只有构建期才知道：
// 页面文档里没有「我用了购物车槽位」这种声明，那是组件渲染时取的值。
//
// 本用例的关键设计：两个页面，一个渲染购物车图标（消费 cart 槽位），一个纯文本。
// 只建一个页面时，「精确标记」与「全量标记」看起来是一样的。

import (
	"context"
	"encoding/json"
	"testing"

	pagedto "go_wp/internal/module/page/dto"
)

// slotCartDocument 渲染时会取 cart 槽位路径。
const slotCartDocument = `{"settings":{"layout":{"mode":"full"}},"root":[{"type":"core.cartIcon","id":"cart-1","props":{}}]}`

// slotPlainDocument 完全不碰系统页面槽位。
const slotPlainDocument = `{"settings":{"layout":{"mode":"full"}},"root":[{"type":"core.text","id":"t1","props":{"text":"PLAIN-PAGE"}}]}`

func TestSiteSlotChangeMarksOnlyReferencingPages(t *testing.T) {
	_, svc, projectID := newPageService(t)
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

	// 换绑购物车槽位：只有真的把该槽位路径烘进产物的页面需要重建。
	if err := svc.BindSiteSlot(ctx, &pagedto.SiteSlotBindReq{
		ProjectID: projectID, Slot: "cart", PageID: otherTarget.ID,
	}); err != nil {
		t.Fatalf("换绑购物车槽位失败: %v", err)
	}

	after, err := svc.Detail(ctx, &pagedto.DetailReq{ProjectID: projectID, ID: withCart.ID})
	if err != nil {
		t.Fatalf("查询消费槽位的页面失败: %v", err)
	}
	if !after.Stale {
		t.Fatalf("渲染过购物车链接的页面应被标记待重建")
	}
	b, err := svc.Detail(ctx, &pagedto.DetailReq{ProjectID: projectID, ID: plain.ID})
	if err != nil {
		t.Fatalf("查询未消费槽位的页面失败: %v", err)
	}
	if b.Stale {
		t.Fatalf("未渲染任何槽位链接的页面不应被标记待重建（这正是本次改造要消除的全量重建）")
	}
}
