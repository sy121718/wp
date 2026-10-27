package feature

// product_publish_record_page_test.go — 发布记录页（B9）的整页链路（真实 PG + 真迁移 + 真发布）。
//
// 这一屏最容易错的地方是「把没发布的商品当成已发布」或反过来 —— 两种都只在真实数据上
// 才看得见（假 reader 里我写什么就回什么）。所以这里走真实链路：建商品 → 建发布实例
//（真编译、真激活）→ 经 presentation 的 PublishRecordPort + outbound 适配器 → 商品读模型。
//
// 三档状态里的「有实例未上线」不在这里造：presentation 没有对外的「只建实例不发」入口
//（CreateInstance 直接激活），造它要绕过 service 直接改表 —— 那验的就不是链路了。
// 那一档由渲染用例（inbound/http/product_publish_record_render_test.go）的形状断言覆盖。

import (
	"context"
	"testing"

	productdto "go_wp/internal/module/product/dto"
	publishrecordoutbound "go_wp/internal/module/product/outbound/publishrecord"
)

// withPublishRecordPort 给 fixture 的商品 service 接上发布记录端口
// （与装配同形：presentation service 已嵌入 PublishRecordPort，outbound 适配器翻成商品侧形状）。
func (f *detailFixture) withPublishRecordPort() {
	f.products.SetPublishRecordPort(publishrecordoutbound.New(f.pres))
}

// TestPublishRecordPageShowsCurrentState 已上线与未建实例两种真实状态，以及它们的状态筛选。
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

	view, err := f.products.ProductPublishRecordPage(ctx, &productdto.ProductPublishRecordPageReq{ProjectID: f.projectID})
	if err != nil {
		t.Fatalf("发布记录页读模型失败: %v", err)
	}
	if !view.Ready {
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

	// 状态筛选按「当前发布状态」而不是商品 status 列：两档各自只留自己的那一行。
	active, err := f.products.ProductPublishRecordPage(ctx, &productdto.ProductPublishRecordPageReq{ProjectID: f.projectID, Status: "active"})
	if err != nil {
		t.Fatalf("active 筛选失败: %v", err)
	}
	if active.Total != 1 || len(active.Records) != 1 || active.Records[0].ProductID != liveID {
		t.Fatalf("active 只应留下已上线的那一行：total=%d %+v", active.Total, active.Records)
	}
	none, err := f.products.ProductPublishRecordPage(ctx, &productdto.ProductPublishRecordPageReq{ProjectID: f.projectID, Status: "none"})
	if err != nil {
		t.Fatalf("none 筛选失败: %v", err)
	}
	if none.Total != 1 || len(none.Records) != 1 || none.Records[0].ProductID != plainID {
		t.Fatalf("none 只应留下没有实例的那一行：total=%d %+v", none.Total, none.Records)
	}

	// 关键词筛选走商品表：命中 slug 也命中名称之外的字段（真实 SQL 才验得到）。
	kw, err := f.products.ProductPublishRecordPage(ctx, &productdto.ProductPublishRecordPageReq{ProjectID: f.projectID, Keyword: "素商品"})
	if err != nil {
		t.Fatalf("关键词筛选失败: %v", err)
	}
	if len(kw.Records) != 1 || kw.Records[0].ProductID != plainID {
		t.Fatalf("关键词应只命中一个商品：%+v", kw.Records)
	}
}

// 端口没接：Ready=false 且不报错（装配缺陷 ≠ 数据读不到），列表为空不伪造行。
func TestPublishRecordPageNotWired(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	f.createProduct(t, "素商品", "pub-none2", "没实例", 9, 9)
	view, err := f.products.ProductPublishRecordPage(context.Background(),
		&productdto.ProductPublishRecordPageReq{ProjectID: f.projectID})
	if err != nil {
		t.Fatalf("未装配档不该报错（那是部署侧提示，不是读失败）: %v", err)
	}
	if view.Ready {
		t.Fatal("没接端口时 Ready 必须是 false")
	}
	if len(view.Records) != 0 {
		t.Fatalf("未装配时不该编造行：%+v", view.Records)
	}
}

// 不存在的工程：400 档（ErrInvalidParam），不是空列表 —— 空列表会被当成「这个站点没商品」。
func TestPublishRecordPageUnknownProject(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	f.withPublishRecordPort()
	_, err := f.products.ProductPublishRecordPage(context.Background(),
		&productdto.ProductPublishRecordPageReq{ProjectID: "does-not-exist"})
	if err == nil {
		t.Fatal("未知工程应报错")
	}
}
