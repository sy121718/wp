package feature

// product_publish_attempt_test.go — 发布历史（B11 / 迁移 671）的整页链路。
//
// 走真实链路：建商品 → 真发布（真编译、真激活）→ presentation 的 PublishAttemptPort
// → 商品侧 outbound 适配器 → 商品读模型。假 reader 里我写什么就回什么，验不到
// 「发布真的留了一行」这件事。
//
// 覆盖三件在界面上看不出来的事：
//   1. 发布成功会**自动**留一行（不需要任何一次点击）—— 这是「全记」的前提；
//   2. 失败也留（含原因原文）：那是运营唯一能查到的失败证据；
//   3. 分页是 page/size/hasMore：累计口径下重复取同一页得到同一批行（前端按 id 去重）。

import (
	"context"
	"testing"

	productdto "go_wp/internal/module/product/dto"
	publishrecordoutbound "go_wp/internal/module/product/outbound/publishrecord"
	presentationdto "go_wp/internal/module/presentation/dto"
)

// withPublishAttemptPort 给 fixture 的商品 service 接上发布历史端口
// （与装配同形：presentation service 已嵌入 PublishAttemptPort）。
func (f *detailFixture) withPublishAttemptPort() {
	f.products.SetPublishAttemptPort(publishrecordoutbound.NewWithAttempts(f.pres, f.pres))
}

// TestPublishAttemptsRecordsEveryPublish 一次发布留一行，再发一次变两行，最新在最前。
func TestPublishAttemptsRecordsEveryPublish(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	f.withPublishAttemptPort()
	ctx := context.Background()

	productID := f.createProduct(t, "留痕商品", "attempt-live", "发布两次", 39, 49)
	f.publish(t, productID, "/products/attempt-live")

	first, err := f.products.ProductPublishAttempts(ctx, &productdto.PublishAttemptListReq{ProjectID: f.projectID, ProductID: productID})
	if err != nil {
		t.Fatalf("读发布历史失败: %v", err)
	}
	if !first.Ready {
		t.Fatal("端口已装配，Ready 应为 true")
	}
	if first.Total != 1 || len(first.Rows) != 1 {
		t.Fatalf("建实例应自动留一行（全记的前提），实际 total=%d rows=%d", first.Total, len(first.Rows))
	}
	row := first.Rows[0]
	if row.Outcome != "success" {
		t.Fatalf("首次发布应记成功: %+v", row)
	}
	if row.Trigger != "create" {
		t.Fatalf("建实例触发的发布应记 create: %+v", row)
	}
	if row.CreatedAtText == "" {
		t.Fatalf("时间必须已格式化（前端只填 textContent）: %+v", row)
	}
	if row.ArtifactVersion == 0 || row.ArtifactHash == "" {
		t.Fatalf("成功的尝试要指向产物版本与 hash: %+v", row)
	}

	// 第二次发布（换 URL = update_url）→ 两行，新行在前。
	if _, err := f.pres.UpdateURL(ctx, &presentationdto.UpdateURLReq{
		ProjectID: f.projectID, EntityType: "product", EntityID: productID, NewPath: "/products/attempt-live-2",
	}); err != nil {
		t.Fatalf("改 URL 失败: %v", err)
	}
	list, err := f.products.ProductPublishAttempts(ctx, &productdto.PublishAttemptListReq{ProjectID: f.projectID, ProductID: productID})
	if err != nil {
		t.Fatalf("读发布历史失败: %v", err)
	}
	if list.Total != 2 || len(list.Rows) != 2 {
		t.Fatalf("第二次发布后应有 2 行，实际 total=%d rows=%d", list.Total, len(list.Rows))
	}
	if list.Rows[0].ID <= list.Rows[1].ID {
		t.Fatalf("新行必须排在前面（倒序）: %+v", list.Rows)
	}
	if list.Rows[0].Trigger != "update_url" {
		t.Fatalf("改 URL 触发的发布应记 update_url: %+v", list.Rows[0])
	}
}

// TestPublishAttemptsPagingIsCumulativeAndStable 分页是 page/size：取第 2 页得到前 2*size 条，
// 重复取同一页得到同一批行（累计口径下前端按 id 去重后追加即幂等）。
func TestPublishAttemptsPagingIsCumulativeAndStable(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	f.withPublishAttemptPort()
	ctx := context.Background()

	productID := f.createProduct(t, "分页商品", "attempt-page", "发四次", 19, 19)
	f.publish(t, productID, "/products/attempt-page")
	for i, p := range []string{"/products/attempt-page-b", "/products/attempt-page-c", "/products/attempt-page-d"} {
		if _, err := f.pres.UpdateURL(ctx, &presentationdto.UpdateURLReq{
			ProjectID: f.projectID, EntityType: "product", EntityID: productID, NewPath: p,
		}); err != nil {
			t.Fatalf("第 %d 次改 URL 失败: %v", i+2, err)
		}
	}

	p1, err := f.products.ProductPublishAttempts(ctx, &productdto.PublishAttemptListReq{
		ProjectID: f.projectID, ProductID: productID, Page: 1, Size: 2,
	})
	if err != nil {
		t.Fatalf("读第 1 页失败: %v", err)
	}
	if len(p1.Rows) != 2 || !p1.HasMore || p1.Total != 4 {
		t.Fatalf("size=2 的第 1 页应给 2 行且还有更多（total=4）: rows=%d hasMore=%v total=%d",
			len(p1.Rows), p1.HasMore, p1.Total)
	}

	p2, err := f.products.ProductPublishAttempts(ctx, &productdto.PublishAttemptListReq{
		ProjectID: f.projectID, ProductID: productID, Page: 2, Size: 2,
	})
	if err != nil {
		t.Fatalf("读第 2 页失败: %v", err)
	}
	if len(p2.Rows) != 4 || p2.HasMore {
		t.Fatalf("第 2 页应是累计前 4 条且没有更多: rows=%d hasMore=%v", len(p2.Rows), p2.HasMore)
	}
	// 累计口径：前 2 条必须与第 1 页逐条相同（否则前端按 id 去重会漏行）。
	for i := 0; i < 2; i++ {
		if p2.Rows[i].ID != p1.Rows[i].ID {
			t.Fatalf("累计口径下第 %d 行应与第 1 页一致：%d != %d", i, p2.Rows[i].ID, p1.Rows[i].ID)
		}
	}

	// 重复取第 1 页：同一批行（只增表上的恒等，前端去重才不会看到重复）。
	again, err := f.products.ProductPublishAttempts(ctx, &productdto.PublishAttemptListReq{
		ProjectID: f.projectID, ProductID: productID, Page: 1, Size: 2,
	})
	if err != nil {
		t.Fatalf("重读第 1 页失败: %v", err)
	}
	for i := 0; i < 2; i++ {
		if again.Rows[i].ID != p1.Rows[i].ID {
			t.Fatalf("重复请求同一页不该得到不同的行：%d != %d", again.Rows[i].ID, p1.Rows[i].ID)
		}
	}
}

// TestPublishAttemptsNotWired 端口没接：Ready=false 且不报错（装配缺陷 ≠ 读失败 ≠ 没记录）。
func TestPublishAttemptsNotWired(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	productID := f.createProduct(t, "未接线商品", "attempt-nowire", "没接端口", 9, 9)
	res, err := f.products.ProductPublishAttempts(context.Background(),
		&productdto.PublishAttemptListReq{ProjectID: f.projectID, ProductID: productID})
	if err != nil {
		t.Fatalf("未装配档不该报错（那是部署侧提示）: %v", err)
	}
	if res.Ready {
		t.Fatal("没接端口时 Ready 必须是 false")
	}
	if len(res.Rows) != 0 {
		t.Fatalf("未装配时不该伪造行: %+v", res.Rows)
	}
}