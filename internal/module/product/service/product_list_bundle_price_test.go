package productservice_test

// product_list_bundle_price_test.go — 列表里的容器价：页面与 API 必须同源。
//
// 审查 P1：m.List 的 Select 里没有 default_price，而 bundle 容器**只有**这一个对外价格
// （applyPriceRange 的 bundle 分支只认它）。页面路径有补偿（product_row.go 的
// applyRowFacts 单独补这一列），API / MCP 出口没有 —— 于是同一次列表里
// 页面显示容器价、接口返回 0。

import (
	"testing"

	"github.com/google/uuid"

	productdto "go_wp/internal/module/product/dto"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectcontract "go_wp/internal/module/project/contract"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/public/test/support"

	"context"
)

func TestListReturnsBundleContainerPrice(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "容器价")

	if err := db.Exec(`INSERT INTO products
		(id, project_id, name, slug, sku_code, status, type, sort, default_price, create_time, update_time)
		VALUES (?, ?, '容器商品', ?, ?, 'published', 'bundle', 1, 88.50, now(), now())`,
		uuid.NewString(), projectID, "p-"+uuid.NewString()[:8], "SKU-B-"+uuid.NewString()[:8]).Error; err != nil {
		t.Fatalf("造数据失败：%v", err)
	}

	svc := productservice.NewService(productmodel.NewModel(db), bundlePriceProjectStub{projectID: projectID})
	list, err := svc.List(context.Background(), &productdto.ListReq{ProjectID: projectID})
	if err != nil {
		t.Fatalf("列表失败：%v", err)
	}
	if len(list) != 1 {
		t.Fatalf("应返回 1 条，实际 %d", len(list))
	}
	if list[0].DefaultPrice == nil {
		t.Fatal("容器商品的 DefaultPrice 为空 —— m.List 的 Select 漏了 default_price 列")
	}
	if *list[0].DefaultPrice != 88.50 {
		t.Fatalf("DefaultPrice = %v，期望 88.50", *list[0].DefaultPrice)
	}
	if list[0].PriceMin != 88.50 || list[0].PriceMax != 88.50 {
		t.Fatalf("价格区间 = %v~%v，期望 88.50~88.50（bundle 分支只认容器价）",
			list[0].PriceMin, list[0].PriceMax)
	}
}

type bundlePriceProjectStub struct {
	projectcontract.ProjectService
	projectID string
}

func (s bundlePriceProjectStub) List(context.Context) ([]projectdto.ProjectResp, error) {
	return []projectdto.ProjectResp{{ID: s.projectID}}, nil
}
