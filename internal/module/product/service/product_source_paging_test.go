package productservice_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"go_wp/pkg/builder/core"
	productcontract "go_wp/internal/module/product/contract"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/public/test/support"
)

func TestResolveCollectionOutOfRangeTotalIsRealCount(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "分页越界")

	for i := 0; i < 3; i++ {
		if err := db.Exec(`INSERT INTO products (id, project_id, name, slug, sku_code, status, sort, create_time, update_time)
			VALUES (?, ?, ?, ?, ?, 'published', ?, now(), now())`,
			uuid.NewString(), projectID, "商品"+uuid.NewString()[:4], "p-"+uuid.NewString()[:8],
			"SKU-"+uuid.NewString()[:8], i+1).Error; err != nil {
			t.Fatalf("造数据失败：%v", err)
		}
	}

	svc := productservice.NewService(productmodel.NewModel(db), listPageProjectStub{
		rows: []projectdto.ProjectResp{{ID: projectID, Name: "分页越界"}},
	}, stubMasterDataChanges{})
	ctx := core.WithBuildProjectID(context.Background(), projectID)

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
