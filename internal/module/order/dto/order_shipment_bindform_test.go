package orderdto

import (
	"net/url"
	"testing"
)

// 本文件锁 ShipOrderReq.BindForm 的语义（与 planShipment 的分工对齐）。
//
// 线上形状是三个不带动态下标的平行键，按控件出现顺序配对：
// lines[][orderItemId] / lines[][quantity] / lines[][warehouseId]。
// 关键约束：orderItemId 必须逐行来自该行自己提交的隐藏控件值，不能靠下标猜。

// lineValues 按给定顺序造一份与浏览器一致的表单值。
func lineValues(rows ...[3]string) url.Values {
	vals := url.Values{
		"orderId": {"501"},
	}
	for _, row := range rows {
		vals["lines[][orderItemId]"] = append(vals["lines[][orderItemId]"], row[0])
		vals["lines[][quantity]"] = append(vals["lines[][quantity]"], row[1])
		vals["lines[][warehouseId]"] = append(vals["lines[][warehouseId]"], row[2])
	}
	return vals
}

// bound 先用与真实入口一致的键集合手工填入平行数组（gin 绑定后的状态），
// 再跑 BindForm，确保装配只依赖已绑定的字段。
func bound(vals url.Values) (*ShipOrderReq, error) {
	req := &ShipOrderReq{
		LineItemIDs:      vals["lines[][orderItemId]"],
		LineQuantities:   vals["lines[][quantity]"],
		LineWarehouseIDs: vals["lines[][warehouseId]"],
	}
	return req, req.BindForm(vals)
}

// TestShipOrderBindFormPairsRowsInOrder 钉住最关键的一条：
// 两行顺序为 1702 → 1701，各自的 quantity / warehouse 必须跟着自己那一行的 id，
// 而不是按 id 大小或按任意 map 顺序分配。
func TestShipOrderBindFormPairsRowsInOrder(t *testing.T) {
	vals := lineValues(
		[3]string{"1702", "1", "wh-b"},
		[3]string{"1701", "2", "wh-a"},
	)
	req, err := bound(vals)
	if err != nil {
		t.Fatalf("BindForm 报错: %v", err)
	}
	if len(req.Lines) != 2 {
		t.Fatalf("应收 2 行，实际 %d 行: %+v", len(req.Lines), req.Lines)
	}
	first, second := req.Lines[0], req.Lines[1]
	if first.OrderItemID != 1702 || first.Quantity != 1 || first.WarehouseID != "wh-b" {
		t.Errorf("第一行（1702）配对错误: %+v", first)
	}
	if second.OrderItemID != 1701 || second.Quantity != 2 || second.WarehouseID != "wh-a" {
		t.Errorf("第二行（1701）配对错误: %+v", second)
	}
}

func TestShipOrderBindFormAllEmptyLeavesNoLines(t *testing.T) {
	vals := lineValues(
		[3]string{"1701", "", "wh-a"},
		[3]string{"1702", "", "wh-a"},
	)
	req, err := bound(vals)
	if err != nil {
		t.Fatalf("BindForm 报错: %v", err)
	}
	if len(req.Lines) != 0 {
		t.Fatalf("一行都没填数量时 Lines 必须为空（由 service 报 ErrShipLinesRequired），实际 %+v", req.Lines)
	}
}

func TestShipOrderBindFormKeepsExplicitZeroQuantity(t *testing.T) {
	req, err := bound(lineValues([3]string{"1701", "0", "wh-a"}))
	if err != nil {
		t.Fatalf("BindForm 报错: %v", err)
	}
	if len(req.Lines) != 1 || req.Lines[0].Quantity != 0 {
		t.Fatalf("显式填 0 必须保留（由 service 判参数错），实际 %+v", req.Lines)
	}
}

func TestShipOrderBindFormRejectsArrayLengthMismatch(t *testing.T) {
	// 少提交一个 quantity 控件（例如某行被误禁用）会让数组长度不一致；
	// 这时不能按下标硬配 —— 那会把后面的数量串到前一行的订单项上。
	vals := lineValues(
		[3]string{"1701", "2", "wh-a"},
		[3]string{"1702", "", "wh-b"},
	)
	vals["lines[][quantity]"] = []string{"2"}
	req, err := bound(vals)
	if err == nil {
		t.Fatalf("长度不一致必须报错，实际 %+v", req.Lines)
	}
}

// TestShipOrderBindFormValuesComeFromStruct 钉住装配数据来源：
// 行值来自 gin 绑定后的字段，而不是再次按键解析 —— 与真实请求路径一致。
func TestShipOrderBindFormValuesComeFromStruct(t *testing.T) {
	req := &ShipOrderReq{
		LineItemIDs:      []string{"1701"},
		LineQuantities:   []string{"3"},
		LineWarehouseIDs: []string{"wh-a"},
	}
	if err := req.BindForm(url.Values{}); err != nil {
		t.Fatalf("BindForm 报错: %v", err)
	}
	if len(req.Lines) != 1 || req.Lines[0].OrderItemID != 1701 ||
		req.Lines[0].Quantity != 3 || req.Lines[0].WarehouseID != "wh-a" {
		t.Fatalf("应只按已绑定字段装配，实际 %+v", req.Lines)
	}
}
