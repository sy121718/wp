package productservice_test

// product_source_paging_test.go — 集合源分页的 Total 口径。
//
// 审查指出的缺陷：resolveCollection 在「本页空」时直接返回 `offset` 当作总量，
// 依据是「调用方不会凭空跳到第 N 页」。但 ResolveCollectionPage 接受任意 q.Offset
// （访客侧分页），越界请求会让它算出荒谬的总量：库里 3 条、offset=180 → Total=180，
// 前端据此渲染出 9 个空页。

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"go_wp/internal/builder/core"
	productcontract "go_wp/internal/module/product/contract"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/public/test/support"
)

// TestResolveCollectionOutOfRangeTotalIsRealCount 越界页的 Total 必须是真实总数。
func TestResolveCollectionOutOfRangeTotalIsRealCount(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "分页越界")

	// 只造 3 条商品。
	for i := 0; i < 3; i++ {
		if err := db.Exec(`INSERT INTO products (id, project_id, name, slug, sku_code, status, type, sort, create_time, update_time)
			VALUES (?, ?, ?, ?, ?, 'published', 'variant', ?, now(), now())`,
			uuid.NewString(), projectID, "商品"+uuid.NewString()[:4], "p-"+uuid.NewString()[:8],
			"SKU-"+uuid.NewString()[:8], i+1).Error; err != nil {
			t.Fatalf("造数据失败：%v", err)
		}
	}

	svc := productservice.NewService(productmodel.NewModel(db), listPageProjectStub{
		rows: []projectdto.ProjectResp{{ID: projectID, Name: "分页越界"}},
	})
	ctx := core.WithBuildProjectID(context.Background(), projectID)

	// offset=180 远超 3 条：本页必然为空。
	page, err := svc.ResolveCollectionPage(ctx, productcontract.CollectionSourceProduct, core.CollectionQuery{Offset: 180, Limit: 20})
	if err != nil {
		t.Fatalf("取数失败：%v", err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("越界页不该有数据，实际 %d 条", len(page.Items))
	}
	if page.Total != 3 {
		t.Fatalf("越界页的 Total = %d，期望 3（真实总数，不是 offset）—— "+
			"按 offset 算会让前端渲染出 %d 个空页", page.Total, page.Total/20)
	}
}
