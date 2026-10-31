package feature

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	productdto "go_wp/internal/module/product/dto"
	publishrecordoutbound "go_wp/internal/module/product/outbound/publishrecord"
)

func (f *detailFixture) withPublishRecordPort() {
	f.products.SetPublishRecordPort(publishrecordoutbound.New(f.pres))
}

func TestPublishRecordPageShowsCurrentState(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	f.withPublishRecordPort()
	ctx := context.Background()

	liveID := f.createProduct(t, "已发布商品", "pub-live", "上线了", 99, 129)
	plainID := f.createProduct(t, "素商品", "pub-none", "没实例", 9, 9)

	f.publish(t, liveID, "/products/pub-live")

	view, err := f.products.QueryPublishRecords(ctx, &productdto.ProductPublishRecordPageReq{ProjectID: f.projectID})
	if err != nil {
		t.Fatalf("发布记录页读模型失败: %v", err)
	}
	if !view.Available {
		t.Fatal("端口已装配，Ready 应为 true")
	}
	if view.Total != 2 || len(view.Records) != 2 {
		t.Fatalf("两个商品都应有一行，实际 total=%d rows=%d", view.Total, len(view.Records))
	}
	byID := map[string]productdto.PublishRecordRowResp{}
	for _, r := range view.Records {
		byID[r.ProductID] = r
	}

	live, ok := byID[liveID]
	if !ok {
		t.Fatalf("已发布商品必须在列表里：%+v", view.Records)
	}
	if !live.HasInstance || !live.Online {
		t.Fatalf("已发布商品应 HasInstance+Online：%+v", live)
	}
	if live.PublishedAt.IsZero() || live.ArtifactVersion == 0 || live.ArtifactHash == "" {
		t.Fatalf("已发布商品应带发布时间 / 产物版本 / hash：%+v", live)
	}
	if len(live.Langs) != 1 || live.Langs[0].OnlinePath != "/products/pub-live" {
		t.Fatalf("应有且只有一条语言的线上路径 /products/pub-live：%+v", live.Langs)
	}
	if live.Langs[0].Lang == "" {
		t.Fatalf("语言行必须带语言码：%+v", live.Langs[0])
	}

	plain, ok := byID[plainID]
	if !ok {
		t.Fatalf("没有实例的商品也必须在列表里（否则看不见「还没发布」的那一批）：%+v", view.Records)
	}
	if plain.HasInstance || plain.Online || len(plain.Langs) != 0 {
		t.Fatalf("没有实例的商品应是空档：%+v", plain)
	}

	active, err := f.products.QueryPublishRecords(ctx, &productdto.ProductPublishRecordPageReq{ProjectID: f.projectID, Status: "active"})
	if err != nil {
		t.Fatalf("active 筛选失败: %v", err)
	}
	if active.Total != 1 || len(active.Records) != 1 || active.Records[0].ProductID != liveID {
		t.Fatalf("active 只应留下已上线的那一行：total=%d %+v", active.Total, active.Records)
	}
	none, err := f.products.QueryPublishRecords(ctx, &productdto.ProductPublishRecordPageReq{ProjectID: f.projectID, Status: "none"})
	if err != nil {
		t.Fatalf("none 筛选失败: %v", err)
	}
	if none.Total != 1 || len(none.Records) != 1 || none.Records[0].ProductID != plainID {
		t.Fatalf("none 只应留下没有实例的那一行：total=%d %+v", none.Total, none.Records)
	}

	kw, err := f.products.QueryPublishRecords(ctx, &productdto.ProductPublishRecordPageReq{ProjectID: f.projectID, Keyword: "素商品"})
	if err != nil {
		t.Fatalf("关键词筛选失败: %v", err)
	}
	if len(kw.Records) != 1 || kw.Records[0].ProductID != plainID {
		t.Fatalf("关键词应只命中一个商品：%+v", kw.Records)
	}
}

func TestPublishRecordPageNormalizesEmptyAndOutOfRange(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	f.withPublishRecordPort()
	productID := f.createProduct(t, "分页商品", "pub-page-bounds", "未发布", 9, 9)
	for _, tc := range []struct {
		name, status, keyword string
		page                  int
		wantID                string
	}{
		{name: "状态筛选空集", status: "active", page: 999},
		{name: "关键词空集", keyword: "no-match-product", page: 999},
		{name: "非空越界夹末页", status: "none", page: 999, wantID: productID},
		{name: "负页码归首页", status: "none", page: -1, wantID: productID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view, err := f.products.QueryPublishRecords(context.Background(), &productdto.ProductPublishRecordPageReq{
				ProjectID: f.projectID, Status: tc.status, Keyword: tc.keyword, Page: tc.page,
			})
			if err != nil {
				t.Fatal(err)
			}
			if view.Page != 1 || view.Limit != 20 || !view.Available || view.Records == nil {
				t.Fatalf("分页事实未归一: %+v", view)
			}
			if view.Total != int64(len(view.Records)) {
				t.Fatalf("单页 total/rows 不一致: %+v", view)
			}
			if tc.wantID == "" {
				if view.Total != 0 {
					t.Fatalf("空筛选返回记录: %+v", view)
				}
			} else if view.Total != 1 || view.Records[0].ProductID != tc.wantID {
				t.Fatalf("末页商品丢失: %+v", view)
			}
		})
	}
}

func TestPublishRecordPageClampsToActualLastPage(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	f.withPublishRecordPort()
	for i := 0; i < 21; i++ {
		f.createProduct(t, fmt.Sprintf("分页商品%d", i), fmt.Sprintf("pub-many-%d", i), "未发布", 9, 9)
	}
	ctx := context.Background()
	first, err := f.products.QueryPublishRecords(ctx, &productdto.ProductPublishRecordPageReq{ProjectID: f.projectID, Status: "none", Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	last, err := f.products.QueryPublishRecords(ctx, &productdto.ProductPublishRecordPageReq{ProjectID: f.projectID, Status: "none", Page: 2})
	if err != nil {
		t.Fatal(err)
	}
	if first.Total != 21 || last.Total != first.Total || len(first.Records) != 20 || len(last.Records) != 1 || last.Page != 2 {
		t.Fatalf("多页事实不一致: first=%+v last=%+v", first, last)
	}
	for _, row := range first.Records {
		if row.ProductID == last.Records[0].ProductID {
			t.Fatal("跨页重复商品")
		}
	}
	for _, page := range []int{999, int(^uint(0) >> 1)} {
		got, err := f.products.QueryPublishRecords(ctx, &productdto.ProductPublishRecordPageReq{ProjectID: f.projectID, Status: "none", Page: page})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, last) {
			t.Fatalf("越界结果与末页不等: page=%d got=%+v last=%+v", page, got, last)
		}
	}
}

func TestPublishRecordPageNotWired(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	f.createProduct(t, "素商品", "pub-none2", "没实例", 9, 9)
	view, err := f.products.QueryPublishRecords(context.Background(),
		&productdto.ProductPublishRecordPageReq{ProjectID: f.projectID})
	if err != nil {
		t.Fatalf("未装配档不该报错（那是部署侧提示，不是读失败）: %v", err)
	}
	if view.Available {
		t.Fatal("没接端口时 Ready 必须是 false")
	}
	if len(view.Records) != 0 {
		t.Fatalf("未装配时不该编造行：%+v", view.Records)
	}
}

func TestPublishRecordPageUnknownProject(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	f.withPublishRecordPort()
	_, err := f.products.QueryPublishRecords(context.Background(),
		&productdto.ProductPublishRecordPageReq{ProjectID: "does-not-exist"})
	if err == nil {
		t.Fatal("未知工程应报错")
	}
}
