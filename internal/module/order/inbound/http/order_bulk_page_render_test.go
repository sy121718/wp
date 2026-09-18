package orderhttp

// order_bulk_page_render_test.go — 订单 / 退货 / 优惠码三张列表页的**渲染冒烟**测试。
//
// 为什么非要渲染：Jet 的 if 遇到缺失或类型不符的键会**中断渲染但 HTTP 仍是 200**，
// 现象是页面后半截整块消失（本项目出过多次）。所以判据是 layout 的收尾标签 </html>
// 确实出现在输出里 —— 中断时它一定不在。外加「批量结构在 / 同维度下拉已删」的正反面断言。
//
// 测试走的是真实的模板文件（templates.NewJetHTMLRender 的开发模式 loader），
// 数据是各页 handler 注入键的最小副本：改坏模板结构或漏掉 isset 保护会在这里变红。

import (
	"net/http/httptest"
	"strings"
	"testing"

	"go_wp/internal/templates"
)

// bulkPageBase 三页共用的外壳数据（layout.html + partials/sidebar.html 需要的键）。
func bulkPageBase(menu, title string) map[string]any {
	return map[string]any{
		"lang": "zh-CN", "langs": []any{}, "lang_redirect": "/admin/" + menu,
		"title": title, "menu": menu,
		"t":           func(_, fallback string) string { return fallback },
		"csrf_token":  "tok",
		"PermSet":     map[string]any{},
		"NavGroups":   []any{},
		"HasSubnav":   false,
		"SidebarOpen": false,
	}
}

// renderAdminTemplate 渲染一个后台页面模板并把 HTML 交回给断言。
func renderAdminTemplate(t *testing.T, name string, data map[string]any) string {
	t.Helper()
	r := templates.NewJetHTMLRender("../../../../templates", true)
	rec := httptest.NewRecorder()
	if err := r.Instance(name, data).Render(rec); err != nil {
		t.Fatalf("%s 渲染失败：%v", name, err)
	}
	return rec.Body.String()
}

func assertContains(t *testing.T, out string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("渲染结果缺少 %q", w)
		}
	}
}

func assertAbsent(t *testing.T, out string, unwanted ...string) {
	t.Helper()
	for _, w := range unwanted {
		if strings.Contains(out, w) {
			t.Errorf("渲染结果仍然包含 %q（应已删除）", w)
		}
	}
}

// orderBulkPageData 订单列表页数据（与 OrdersPage 注入的键同名）。
func orderBulkPageData() map[string]any {
	d := bulkPageBase("orders", "订单管理")
	d["Projects"] = []any{map[string]any{"ID": "p1", "Name": "站点"}}
	d["SelectedProject"] = "p1"
	d["Statuses"] = []any{
		map[string]any{"Value": "", "Label": "全部", "Badge": "badge-mute", "Count": int64(1), "URL": "/admin/orders?project=p1", "Active": false},
		map[string]any{"Value": "paid", "Label": "已付款", "Badge": "badge-info", "Count": int64(1), "URL": "/admin/orders?project=p1&status=paid", "Active": true},
	}
	d["BulkTargets"] = []any{
		map[string]any{"Value": "paid", "Label": "已付款"},
		map[string]any{"Value": "shipped", "Label": "已发货"},
	}
	d["FilterStatus"] = "paid"
	d["FilterKeyword"] = "a@b.c"
	d["FilterPayment"] = "paypal"
	d["Rows"] = []any{map[string]any{
		"ID": "7", "OrderNo": "SO-1", "Status": "paid", "StatusLabel": "已付款", "Badge": "badge-info",
		"CustomerName": "客户", "CustomerEmail": "a@b.c", "TotalLabel": "￥12.00", "PaymentLabel": "paypal",
		"CreatedAt": "2026-01-01 10:00", "DetailURL": "/admin/orders?project=p1&orderId=7", "Expanded": false,
	}}
	d["Total"] = int64(1)
	d["Detail"] = map[string]any{}
	d["HasDetail"] = false
	d["Page"] = 1
	d["Limit"] = 20
	d["Err"] = ""
	d["Ok"] = ""
	d["Done"] = "已流转 1 个订单，跳过 1 个（状态不允许或已不存在）。"
	return d
}

func TestOrdersPageBulkStructure(t *testing.T) {
	out := renderAdminTemplate(t, "admin/orders.html", orderBulkPageData())
	assertContains(t, out, "</html>",
		"action=\"/admin/orders/bulk-status\"", "formaction=\"/admin/orders/bulk-cancel\"",
		"data-check-all", "data-check-item", "name=\"ids\" value=\"7\"",
		"data-bulk-bar", "data-bulk-count", "data-bulk-template", "data-confirm-danger",
		"aria-current=\"page\"",
		// 回跳上下文：批量表单必须带回当前筛选与窗口，否则提交后被弹回未筛选的首页。
		"name=\"keyword\" value=\"a@b.c\"",
		"name=\"paymentMethod\" value=\"paypal\"",
		// 批量结论。
		"已流转 1 个订单",
	)
	// 状态维度只留徽章：同维度的下拉必须已经删掉。
	assertAbsent(t, out, "orders-filter-status")
}

// TestOrdersPageDetailRenders 详情区（HasDetail）也要渲染到最后一字节。
//
// 详情区是列表页里最长的分支（.kv 网格 + 订单项表 + 流转链 + 三个写表单），
// 模板中断最常见的表现就是它整块消失，所以尾部标记逐项点一遍。
func TestOrdersPageDetailRenders(t *testing.T) {
	d := orderBulkPageData()
	d["HasDetail"] = true
	d["Detail"] = map[string]any{
		"Head": map[string]any{
			"ID": uint64(7), "OrderNo": "SO-1", "Status": "paid", "StatusLabel": "已付款", "Badge": "badge-info",
			"CustomerName": "客户", "CustomerEmail": "a@b.c", "CustomerPhone": "138",
			"SubtotalLabel": "￥12.00", "DiscountLabel": "￥0.00", "ShippingLabel": "￥0.00",
			"TaxLabel": "￥0.00", "TotalLabel": "￥12.00", "PaymentLabel": "paypal",
			"TransactionID": "TX-1", "PaidAt": "2026-01-01 10:00", "CompletedAt": "2026-01-01 10:00",
			"CreatedAt": "2026-01-01 10:00", "CreatedViaLabel": "后台", "IPAddress": "127.0.0.1",
			"UserAgent": "curl", "AdminNote": "已联系", "Remark": "尽快", "CancelReason": "—",
			"ShippingAddress": "深圳", "BillingAddress": "深圳",
		},
		"Items": []any{map[string]any{
			"ProductName": "商品", "VariantLabel": "红 / M", "SKU": "SKU-1", "UnitPrice": "￥12.00",
			"Quantity": 1, "LineSubtotal": "￥12.00", "LineDiscount": "￥0.00", "LineTax": "￥0.00", "LineTotal": "￥12.00",
		}},
		"Logs": []any{map[string]any{
			"Time": "2026-01-01 10:00", "FromLabel": "待付款", "ToLabel": "已付款",
			"OperatorTypeLabel": "管理员", "OperatorName": "admin", "Remark": "收款",
		}},
		"Transitions": []any{map[string]any{"Value": "shipped", "Label": "已发货"}},
		"CanCancel":   true,
		"CanRefund":   true,
		"Form": map[string]any{
			"Project": "p1", "OrderID": "7", "Status": "paid", "Keyword": "", "PaymentMethod": "",
			"Page": "1", "Limit": "20",
		},
	}
	out := renderAdminTemplate(t, "admin/orders.html", d)
	assertContains(t, out, "</html>",
		"class=\"kv\"", "订单项", "状态流转链", "保存备注", "name=\"toStatus\"",
		"action=\"/admin/orders/cancel\"", "action=\"/admin/orders/refund\"",
	)
}

func TestOrdersPageRendersWithoutBulkKeys(t *testing.T) {
	// 直接渲染模板的调用方不带这批可选键：缺它们不该让整页中断（isset 保护）。
	d := orderBulkPageData()
	delete(d, "Done")
	delete(d, "BulkTargets")
	out := renderAdminTemplate(t, "admin/orders.html", d)
	assertContains(t, out, "</html>", "action=\"/admin/orders/bulk-status\"")
}

// returnBulkPageData 退货列表页数据（与 ReturnsPage 注入的键同名）。
func returnBulkPageData() map[string]any {
	d := bulkPageBase("orders", "退货入库")
	d["Projects"] = []any{map[string]any{"ID": "p1", "Name": "站点"}}
	d["SelectedProject"] = "p1"
	d["Statuses"] = []any{
		map[string]any{"Value": "requested", "Label": "待审核", "Badge": "badge-warning", "Count": int64(1), "Highlight": true, "URL": "/admin/returns?project=p1&status=requested", "Active": true},
	}
	d["FilterStatus"] = "requested"
	d["FilterLabel"] = "待审核"
	d["FilterKeyword"] = ""
	d["FilterOrderID"] = ""
	d["ClearOrderURL"] = "/admin/returns?project=p1"
	d["PendingHint"] = ""
	d["Rows"] = []any{map[string]any{
		"ID": uint64(9), "ReturnNo": "RT-1", "OrderNo": "SO-1", "Status": "requested", "StatusLabel": "待审核",
		"Badge": "badge-warning", "CustomerName": "客户", "CustomerEmail": "a@b.c", "RefundLabel": "￥12.00",
		"CreatedAt": "2026-01-01 10:00", "DetailURL": "/admin/returns?project=p1&returnId=9", "Expanded": false,
	}}
	d["Total"] = int64(1)
	d["Warehouses"] = []any{}
	d["Detail"] = map[string]any{}
	d["HasDetail"] = false
	d["Page"] = 1
	d["Limit"] = 20
	d["Err"] = ""
	d["Ok"] = ""
	d["Done"] = "已同意 1 个退货申请。"
	return d
}

func TestReturnsPageBulkStructure(t *testing.T) {
	out := renderAdminTemplate(t, "admin/returns.html", returnBulkPageData())
	assertContains(t, out, "</html>",
		"action=\"/admin/returns/bulk-approve\"", "formaction=\"/admin/returns/bulk-reject\"",
		"data-check-all", "data-check-item", "name=\"ids\" value=\"9\"",
		"data-bulk-bar", "data-bulk-count", "data-confirm-danger",
		"aria-current=\"page\"",
		"已同意 1 个退货申请",
	)
	assertAbsent(t, out, "returns-filter-status")
}

// couponBulkPageData 优惠码列表页数据（与 CouponsPage 注入的键同名）。
func couponBulkPageData() map[string]any {
	d := bulkPageBase("coupons", "优惠码管理")
	d["Projects"] = []any{map[string]any{"ID": "p1", "Name": "站点"}}
	d["SelectedProject"] = "p1"
	d["FilterOptions"] = []any{map[string]any{"Value": "enabled", "Label": "生效中"}}
	d["TypeOptions"] = []any{map[string]any{"Value": "percent", "Label": "按比例"}}
	d["StatusOptions"] = []any{
		map[string]any{"Value": "1", "Label": "启用"},
		map[string]any{"Value": "0", "Label": "停用"},
	}
	d["FilterStatus"] = ""
	d["FilterKeyword"] = ""
	d["Rows"] = []any{map[string]any{
		"Code": "WELCOME", "Name": "新客券", "DiscountLabel": "9 折", "MinSubtotalLabel": "不限",
		"UsageLabel": "0 / 100", "PerUserLabel": "1", "WindowLabel": "不限", "StatusLabel": "生效中",
		"Badge": "badge-success", "Remark": "—", "EditURL": "/admin/coupons?project=p1&couponId=3",
		"CollapseURL": "/admin/coupons?project=p1", "Expanded": false, "ToggleStatus": "0", "ToggleLabel": "停用",
		"Form": map[string]any{"ID": "3", "StatusValue": "1"},
		"Back": "project=p1",
	}}
	d["Total"] = int64(1)
	d["Detail"] = map[string]any{}
	d["HasDetail"] = false
	d["DetailCollapseURL"] = "/admin/coupons?project=p1"
	d["CreateBack"] = "project=p1"
	d["Redemptions"] = []any{}
	d["RedemptionTotal"] = int64(0)
	d["RedemptionLimit"] = 50
	d["Page"] = 1
	d["Limit"] = 20
	d["Err"] = ""
	d["Ok"] = ""
	d["Done"] = "已停用 2 个优惠码，跳过 1 个（状态不允许或已不存在）。"
	return d
}

func TestCouponsPageBulkStructure(t *testing.T) {
	out := renderAdminTemplate(t, "admin/coupons.html", couponBulkPageData())
	assertContains(t, out, "</html>",
		"action=\"/admin/coupons/bulk-toggle\"", "formaction=\"/admin/coupons/bulk-delete\"",
		"data-check-all", "data-check-item", "name=\"ids\" value=\"3\"",
		"data-bulk-bar", "data-bulk-count", "data-confirm-danger",
		"已停用 2 个优惠码",
		// 本页没有状态计数徽章：状态筛选下拉是该维度的唯一入口，必须保留。
		"id=\"coupons-filter-status\"",
	)
}
