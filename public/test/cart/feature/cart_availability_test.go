package feature

import (
	"context"
	"errors"
	"testing"

	cartdto "go_wp/internal/module/cart/dto"
	cartenums "go_wp/internal/module/cart/enums"
	inventorydto "go_wp/internal/module/inventory/dto"
	productdto "go_wp/internal/module/product/dto"
)

func TestCartUntrackedStockRemainsUnknownAndTrackedZeroRejects(t *testing.T) {
	f := newCartFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	for _, tracked := range []bool{false, true} {
		name := "不跟踪"
		if tracked {
			name = "跟踪零库存"
		}
		p, err := f.products.Create(ctx, &productdto.CreateReq{
			ProjectID: f.projectID, Name: name, TrackQuantity: tracked,
		})
		if err != nil {
			t.Fatal(err)
		}
		detail, err := f.products.Get(ctx, &productdto.GetReq{ID: p.ID, ProjectID: f.projectID})
		if err != nil || len(detail.Variants) != 1 {
			t.Fatalf("新建商品变体: %v %+v", err, detail)
		}
		variantID := detail.Variants[0].ID
		stock, err := f.inventory.GetStock(ctx, &inventorydto.GetStockReq{VariantID: variantID, WarehouseID: f.warehouse})
		if err != nil || stock.TrackQuantity != tracked || stock.Quantity != 0 {
			t.Fatalf("新建库存事实与开关不一致: %v %+v", err, stock)
		}
		available, err := f.products.VariantAvailabilities(ctx, f.projectID, []string{variantID})
		if err != nil {
			t.Fatal(err)
		}
		q, known := available[variantID]
		if known != tracked || q != stock.Quantity {
			t.Fatalf("可用量事实=(%d,%t)，跟踪=%t 库存=%d", q, known, tracked, stock.Quantity)
		}
		snapshot, err := f.cart.Add(ctx, &cartdto.CartAddReq{ProjectID: f.projectID, VariantID: variantID, Quantity: 2})
		if tracked {
			if !errors.Is(err, cartenums.Err(cartenums.ErrOutOfStock)) {
				t.Fatalf("真实跟踪零库存应拒加购: %v", err)
			}
			continue
		}
		if err != nil || len(snapshot.Items) != 1 || snapshot.Items[0].Available != nil || snapshot.ItemCount != 2 {
			t.Fatalf("不跟踪库存应加购且available=null: %v %+v", err, snapshot)
		}
		read, err := f.cart.View(ctx, &cartdto.CartViewReq{ProjectID: f.projectID, Cookie: snapshot.Cookie})
		if err != nil || read.ItemCount != snapshot.ItemCount || read.Items[0].Available != snapshot.Items[0].Available {
			t.Fatalf("重新读取应保留相同库存事实: %v %+v", err, read)
		}
	}
}
