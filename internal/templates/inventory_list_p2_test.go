package templates

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func inventoryListDOM(t *testing.T, name string, data map[string]any) *html.Node {
	t.Helper()
	out, err := render(t, groupDSet(t), "admin/inventory/"+name, groupDData(data))
	if err != nil {
		t.Fatalf("渲染 %s: %v", name, err)
	}
	root, err := html.Parse(strings.NewReader(out))
	if err != nil {
		t.Fatalf("解析 %s: %v", name, err)
	}
	return root
}

func TestInventoryListTitleOmitsZeroCount(t *testing.T) {
	for _, tc := range []struct {
		name, template, title, populatedTitle, listKey string
	}{
		{"仓库", "inventory_warehouses", "仓库管理", "仓库列表（1）", "Warehouses"},
		{"原因", "inventory_reasons", "变动原因字典", "变动原因字典（1）", "Reasons"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := map[string]any{
				"Err": "", "Ok": "", "SelectedProject": "pr1", "Projects": groupDProjects(),
				"Directions": []map[string]any{},
			}
			for _, count := range []int{0, 1} {
				rows := []map[string]any{}
				if count == 1 {
					rows = append(rows, map[string]any{
						"ID": "id1", "Name": "样例", "Code": "sample", "Status": "active",
						"StatusLabel": "启用", "DirectionLabel": "入库", "Sort": 0,
					})
				}
				data[tc.listKey] = rows
				root := inventoryListDOM(t, tc.template, data)
				titles := peAll(root, func(n *html.Node) bool { return peClass(n, "card-title") })
				if len(titles) != 1 {
					t.Fatalf("%d 行时卡片标题数 = %d", count, len(titles))
				}
				want := tc.title
				if count > 0 {
					want = tc.populatedTitle
				}
				if got := strings.TrimSpace(peText(titles[0])); got != want {
					t.Errorf("%d 行时标题 = %q，期望 %q", count, got, want)
				}
			}
		})
	}
}

func TestInventoryMovementTitleOmitsZeroCount(t *testing.T) {
	for _, tc := range []struct {
		name, lang, zeroTitle, populatedTitle string
		translations                          map[string]string
	}{
		{"中文词条", "zh-CN", "库存流水", "库存流水（最近 1 条）", map[string]string{
			"admin.inventory.moves.title":     "库存流水",
			"admin.inventory.moves.titleLead": "库存流水（最近 ",
			"admin.inventory.moves.titleTail": " 条）",
		}},
		{"英文词条", "en-US", "Stock ledger", "Stock ledger (latest 1)", map[string]string{
			"admin.inventory.moves.title":     "Stock ledger",
			"admin.inventory.moves.titleLead": "Stock ledger (latest ",
			"admin.inventory.moves.titleTail": ")",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := map[string]any{
				"Err": "", "Ok": "", "SelectedProject": "pr1", "SelectedVariant": "", "SelectedSKU": "",
				"Projects": groupDProjects(), "Warehouses": []map[string]any{},
				"Reasons": []map[string]any{}, "Directions": []map[string]any{},
				"VariantOptions": []map[string]any{}, "StockRows": []map[string]any{},
			}
			for _, count := range []int{0, 1} {
				movements := []map[string]any{}
				if count > 0 {
					movements = append(movements, map[string]any{
						"CreatedAt": "2026-01-01", "SKUCode": "SKU1", "WarehouseName": "仓一", "WarehouseCode": "W1",
						"Direction": "in", "DirectionLabel": "入库", "Quantity": 1, "QuantityBefore": 0,
						"QuantityAfter": 1, "ReasonName": "采购入库", "ReasonCode": "purchase_in",
						"SourceRef": "PO-1", "SourceType": "purchase", "OperatorID": "u1",
					})
				}
				data["Movements"] = movements
				data["t"] = func(key, fallback string) string {
					if value, ok := tc.translations[key]; ok {
						return value
					}
					return fallback
				}
				data["lang"] = tc.lang
				root := inventoryListDOM(t, "inventory", data)
				titles := peAll(root, func(n *html.Node) bool { return peClass(n, "card-title") })
				if len(titles) != 1 {
					t.Fatalf("%d 行时卡片标题数 = %d", count, len(titles))
				}
				want := tc.zeroTitle
				if count > 0 {
					want = tc.populatedTitle
				}
				if got := strings.TrimSpace(peText(titles[0])); got != want {
					t.Errorf("%d 行时标题 = %q，期望 %q", count, got, want)
				}
			}
		})
	}
}

func TestPurchaseListColumnsAlign(t *testing.T) {
	for _, tc := range []struct {
		name            string
		sources, orders []map[string]any
	}{
		{"无货源", nil, nil},
		{"有货源无采购单", []map[string]any{{"ID": "s1", "Label": "货源甲"}}, nil},
		{"有采购单", []map[string]any{{"ID": "s1", "Label": "货源甲"}}, []map[string]any{{
			"ID": "o1", "Code": "PO-1", "SourceName": "货源甲", "SourceTypeLabel": "外部供应商",
			"WarehouseName": "仓库甲", "Status": "partial", "StatusLabel": "部分入库",
			"ReceivedQuantity": 1, "TotalQuantity": 2, "OrderedAt": "2026-01-01", "Lines": []map[string]any{},
		}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := inventoryListDOM(t, "inventory_purchases", map[string]any{
				"Err": "", "Ok": "", "SelectedProject": "pr1", "Projects": groupDProjects(),
				"FilterStatus": "", "FilterSource": "", "FilterKeyword": "", "ReceiptRequestID": "r1",
				"Sources": tc.sources, "Orders": tc.orders, "StatusOptions": []map[string]any{},
				"Warehouses": []map[string]any{}, "DraftLines": []map[string]any{},
				"InternalSources": []map[string]any{}, "VariantOptions": []map[string]any{},
			})
			tables := peAll(root, func(n *html.Node) bool { return peEl(n, "table") && peClass(n, "purchase-page-table") })
			if len(tables) != 1 {
				t.Fatalf("采购表数量 = %d", len(tables))
			}
			headers := peAll(tables[0], func(n *html.Node) bool { return peEl(n, "th") })
			if len(headers) != 8 || strings.TrimSpace(peText(headers[1])) != "货源" || strings.TrimSpace(peText(headers[2])) != "类型" {
				t.Fatalf("表头列结构不正确：共 %d 列", len(headers))
			}
			body := peAll(tables[0], func(n *html.Node) bool { return peEl(n, "tbody") })
			rows := peAll(body[0], func(n *html.Node) bool { return peEl(n, "tr") })
			if len(rows) != 1 {
				t.Fatalf("tbody 行数 = %d", len(rows))
			}
			cells := peAll(rows[0], func(n *html.Node) bool { return peEl(n, "td") })
			if len(tc.orders) == 0 {
				if len(cells) != 1 || peAttr(cells[0], "colspan") != "8" {
					t.Fatalf("空态应跨 8 列，实际 %d 个单元格，colspan=%q", len(cells), peAttr(cells[0], "colspan"))
				}
				return
			}
			if len(cells) != len(headers) || strings.TrimSpace(peText(cells[1])) != "货源甲" || strings.TrimSpace(peText(cells[2])) != "外部供应商" {
				t.Fatalf("采购行应有 8 列且货源、类型分列，实际 %d 列", len(cells))
			}
			if peAttr(cells[2], "data-label") != strings.TrimSpace(peText(headers[2])) {
				t.Errorf("类型单元格的移动端标签与表头不一致")
			}
		})
	}
}
