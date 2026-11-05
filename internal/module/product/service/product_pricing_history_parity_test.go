package productservice_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/google/uuid"
	productdto "go_wp/internal/module/product/dto"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/public/test/support"
)

func TestProductPricingHistoryRowsMatchPerBatchDetailPath(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "调价记录对账")
	seedPricingHistoryFixture(t, db, projectID, []int{2, 0, 3})
	s := productservice.NewService(productmodel.NewModel(db), listPageProjectStub{
		rows: []projectdto.ProjectResp{{ID: projectID, Name: "调价记录对账"}},
	}, stubMasterDataChanges{})
	ctx := context.Background()
	history, err := s.ListPriceAdjustments(ctx, &productdto.ListPriceAdjustmentReq{ProjectID: projectID, Limit: 10, IncludeItems: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 3 {
		t.Fatalf("调价记录数量=%d，期望3（含空批次）", len(history))
	}
	named, blank, total := 0, 0, 0
	for _, row := range history {
		want, err := s.GetPriceAdjustment(ctx, &productdto.GetPriceAdjustmentReq{ID: row.ID, ProjectID: projectID})
		if err != nil {
			t.Fatal(err)
		}

		if len(row.Items) == 0 && len(want.Items) == 0 {
			row.Items, want.Items = nil, nil
		}
		if !reflect.DeepEqual(row, want) {
			t.Fatalf("批量记录与单批详情不一致：got=%+v want=%+v", row, want)
		}
		if row.CreatedAt.IsZero() || row.Note == "" || row.RuleType == "" {
			t.Fatalf("夹具缺少批次事实：%+v", row)
		}
		total += len(row.Items)
		for i, item := range row.Items {
			if item.SKUCode == "" || item.OldPrice == 0 || item.NewPrice == 0 || item.Diff == 0 {
				t.Fatalf("夹具缺少明细事实：%+v", item)
			}
			if item.ProductName == "" {
				blank++
			} else {
				named++
			}
			if i > 0 && row.Items[i-1].SKUCode > item.SKUCode {
				t.Fatal("批量明细未保持SKU升序")
			}
		}
	}
	if total != 5 || named == 0 || blank == 0 {
		t.Fatalf("夹具明细=%d，有名=%d，无名=%d", total, named, blank)
	}
}
