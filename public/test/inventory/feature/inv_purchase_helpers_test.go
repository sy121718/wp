package feature

import (
	"context"
	"fmt"
	"strings"
	"testing"

	productdto "go_wp/internal/module/product/dto"
	purchasedto "go_wp/internal/module/purchase/dto"
)

// —— 采购域小工具（2026-10 采购拆成独立模块后，库存用例经采购 service 造入库）——

// mustPurchaseSource 建一个货源并返回（失败即 fail）。
func mustPurchaseSource(t *testing.T, f *invFixture, code, name, sourceType string) *purchasedto.SourceResp {
	t.Helper()
	res, err := f.purchase.CreateSource(context.Background(), &purchasedto.CreateSourceReq{
		ProjectID: f.projectID, Code: code, Name: name, Type: sourceType,
	})
	if err != nil {
		t.Fatalf("建货源 %s 失败: %v", code, err)
	}
	return res
}

// purchaseLine 造一条采购行（SKU × 数量 × 单价）。
func purchaseLine(p *productdto.ProductResp, v *productdto.VariantResp, quantity int, unitPrice float64) purchasedto.PurchaseLineReq {
	return purchasedto.PurchaseLineReq{
		VariantID: v.ID, ProductID: p.ID, SKUCode: v.SKUCode,
		Quantity: quantity, UnitPrice: unitPrice,
	}
}

// mustPurchaseOrder 建采购单并要求成功。
func mustPurchaseOrder(t *testing.T, f *invFixture, code, sourceID, warehouseID string,
	lines []purchasedto.PurchaseLineReq) *purchasedto.PurchaseOrderResp {
	t.Helper()
	res, err := f.purchase.CreatePurchaseOrder(context.Background(), &purchasedto.CreatePurchaseOrderReq{
		ProjectID: f.projectID, Code: code, SourceID: sourceID, WarehouseID: warehouseID, Lines: lines,
	})
	if err != nil {
		t.Fatalf("建采购单 %s 失败: %v", code, err)
	}
	return res
}

// mustProductPriced 建一个带默认售价的商品（用于断言「成本价更新不动售价」）。
func mustProductPriced(t *testing.T, f *invFixture, name string, price float64) *productdto.ProductResp {
	t.Helper()
	slug := fmt.Sprintf("%s-%d", strings.ToLower(name), slugSeq.Add(1))
	p, err := f.products.Create(context.Background(), &productdto.CreateReq{
		ProjectID: f.projectID, Name: name, Slug: slug, DefaultPrice: &price,
	})
	if err != nil {
		t.Fatalf("建商品 %s 失败: %v", name, err)
	}
	return p
}

// mustReceiveLine 登记一条采购行收货并要求成功。
func mustReceiveLine(t *testing.T, f *invFixture, orderID, lineID string, quantity int,
	requestID string) *purchasedto.ReceiptResp {
	t.Helper()
	res, err := f.purchase.RegisterReceipt(context.Background(), &purchasedto.RegisterReceiptReq{
		ProjectID: f.projectID, OrderID: orderID, RequestID: requestID,
		Lines: []purchasedto.ReceiptLineReq{{LineID: lineID, Quantity: quantity}},
	})
	if err != nil {
		t.Fatalf("登记入库 %d 失败: %v", quantity, err)
	}
	return res
}

// variantCostIn 直读商品侧成本价（跨模块列，断言必须直查）。
func variantCostIn(t *testing.T, f *invFixture, variantID string) *float64 {
	t.Helper()
	var row struct {
		Cost *float64 `gorm:"column:cost"`
	}
	if err := f.db.Raw("SELECT cost_price AS cost FROM product_variants WHERE id = ?", variantID).
		Scan(&row).Error; err != nil {
		t.Fatalf("读商品侧成本价失败: %v", err)
	}
	return row.Cost
}
