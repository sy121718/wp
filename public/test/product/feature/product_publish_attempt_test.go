package feature

import (
	"context"
	"testing"

	presentationdto "go_wp/internal/module/presentation/dto"
	productdto "go_wp/internal/module/product/dto"
	publishrecordoutbound "go_wp/internal/module/product/outbound/publishrecord"
)

func (f *detailFixture) withPublishAttemptPort() {
	f.products.SetPublishAttemptPort(publishrecordoutbound.NewWithAttempts(f.pres))
}

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
	if row.CreatedAt.TimeOrZero().IsZero() {
		t.Fatalf("必须返回原始发布时刻，由前端格式化: %+v", row)
	}
	if row.ArtifactVersion == 0 || row.ArtifactHash == "" {
		t.Fatalf("成功的尝试要指向产物版本与 hash: %+v", row)
	}

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

	for i := 0; i < 2; i++ {
		if p2.Rows[i].ID != p1.Rows[i].ID {
			t.Fatalf("累计口径下第 %d 行应与第 1 页一致：%d != %d", i, p2.Rows[i].ID, p1.Rows[i].ID)
		}
	}

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
