package orderdto

import (
	"net/url"
	"testing"
)

// 本文件锁 ShipOrderReq.BindForm 的三条语义（与 planShipment 的分工对齐）：
//  1. 所有行都没填 → Lines 为空，由 service 报 ErrShipLinesRequired；
//  2. 部分行有值 → 只收有值的行；
//  3. 显式填 0 → 保留，由 service 的 Quantity <= 0 报 ErrQuantityInvalid。

func TestShipOrderBindFormParsesThreeFields(t *testing.T) {
	vals := url.Values{
		"orderId":                  {"501"},
		"lines[1701][orderItemId]": {"1701"},
		"lines[1701][quantity]":    {"2"},
		"lines[1701][warehouseId]": {"wh-a"},
		"lines[1702][orderItemId]": {"1702"},
		"lines[1702][quantity]":    {"1"},
		"lines[1702][warehouseId]": {"wh-b"},
	}
	var req ShipOrderReq
	if err := req.BindForm(vals); err != nil {
		t.Fatalf("BindForm 报错: %v", err)
	}
	if len(req.Lines) != 2 {
		t.Fatalf("应收 2 行，实际 %d 行: %+v", len(req.Lines), req.Lines)
	}
	byID := map[uint64]ShipLineReq{}
	for _, l := range req.Lines {
		byID[l.OrderItemID] = l
	}
	if got := byID[1701]; got.Quantity != 2 || got.WarehouseID != "wh-a" {
		t.Errorf("1701 行解析不对: %+v", got)
	}
	if got := byID[1702]; got.Quantity != 1 || got.WarehouseID != "wh-b" {
		t.Errorf("1702 行解析不对: %+v", got)
	}
}

func TestShipOrderBindFormIgnoresUnknownFields(t *testing.T) {
	vals := url.Values{
		"lines[1701][orderItemId]": {"1701"},
		"lines[1701][quantity]":    {"3"},
		// 白名单外的键必须被忽略，而不是让整单报错。
		"lines[1701][sku]":      {"SKU-X"},
		"lines[1701][priceFen]": {"999"},
		"lines[9999][imageUrl]": {"http://example.test/a.png"},
	}
	var req ShipOrderReq
	if err := req.BindForm(vals); err != nil {
		t.Fatalf("多带键不应报错，实际: %v", err)
	}
	if len(req.Lines) != 1 {
		t.Fatalf("只有 1701 行有必填两项，应收 1 行，实际 %d 行: %+v", len(req.Lines), req.Lines)
	}
	if got := req.Lines[0]; got.OrderItemID != 1701 || got.Quantity != 3 {
		t.Errorf("解析结果不对: %+v", got)
	}
}

func TestShipOrderBindFormAllEmptyLeavesNoLines(t *testing.T) {
	vals := url.Values{
		"lines[1701][orderItemId]": {"1701"},
		"lines[1701][quantity]":    {""},
		"lines[1702][orderItemId]": {"1702"},
		"lines[1702][warehouseId]": {"wh-a"},
	}
	var req ShipOrderReq
	if err := req.BindForm(vals); err != nil {
		t.Fatalf("BindForm 报错: %v", err)
	}
	if len(req.Lines) != 0 {
		t.Fatalf("一行都没填数量时 Lines 必须为空（由 service 报 ErrShipLinesRequired），实际 %+v", req.Lines)
	}
}

func TestShipOrderBindFormKeepsExplicitZeroQuantity(t *testing.T) {
	vals := url.Values{
		"lines[1701][orderItemId]": {"1701"},
		"lines[1701][quantity]":    {"0"},
	}
	var req ShipOrderReq
	if err := req.BindForm(vals); err != nil {
		t.Fatalf("BindForm 报错: %v", err)
	}
	if len(req.Lines) != 1 || req.Lines[0].Quantity != 0 {
		t.Fatalf("显式填 0 必须保留（由 service 判参数错），实际 %+v", req.Lines)
	}
}

func TestShipOrderBindFormPrefersValueOverIndex(t *testing.T) {
	// 下标是服务端渲染的，被改了不该改变语义；OrderItemID 以值里的为准。
	vals := url.Values{
		"lines[88][orderItemId]": {"1701"},
		"lines[88][quantity]":    {"2"},
	}
	var req ShipOrderReq
	if err := req.BindForm(vals); err != nil {
		t.Fatalf("BindForm 报错: %v", err)
	}
	if len(req.Lines) != 1 {
		t.Fatalf("应收 1 行，实际 %+v", req.Lines)
	}
	if got := req.Lines[0].OrderItemID; got != 1701 {
		t.Fatalf("OrderItemID 必须以值 1701 为准（下标 88 只用于分组），实际 %d", got)
	}
}

func TestShipOrderBindFormIgnoresNonLineKeys(t *testing.T) {
	vals := url.Values{
		"orderId":                  {"501"},
		"carrierId":                {"c-1"},
		"trackingNo":               {"SF1"},
		"lines[1701][orderItemId]": {"1701"},
		"lines[1701][quantity]":    {"1"},
	}
	var req ShipOrderReq
	if err := req.BindForm(vals); err != nil {
		t.Fatalf("BindForm 报错: %v", err)
	}
	if len(req.Lines) != 1 {
		t.Fatalf("非行键不该产生行，实际 %+v", req.Lines)
	}
}
