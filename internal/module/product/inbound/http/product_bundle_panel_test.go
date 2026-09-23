package producthttp

// product_bundle_panel_test.go — 商品详情页「捆绑构成」区块（按商品类型出现）。
//
// 用真实 Jet 模板渲染（与生产同一 template root），断言的是「页面里到底渲染了什么」：
//   · type=bundle 才有这一块，type=variant 整块不渲染、页面也不被中途截断；
//   · 成员的成本（后台口径）与挂牌价（参考值）分开呈现，挂牌价列头写明「不参与套餐价」；
//   · 空配置、SKU 已删除、已停用都有可读提示，不静默丢项。

import (
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	productdto "go_wp/internal/module/product/dto"
)

// bundlePanelEditLink 「编辑捆绑构成」的落点（含 HTML 转义后的 &）。
// 参数名与商品页其余表单一致：projectId / productId。
//
// 它现在渲染在**编辑页**：详情页只读展示构成表（成员、必选、数量、成本、参考价），
// 改配置的入口在编辑页 —— 详情页是「看」的地方。
const bundlePanelEditLink = "href=\"/admin/products/bundle?projectId=proj-1&amp;productId=p1\""

// bundlePanelRow 造一行区块视图数据（默认一个正常成员，按需覆盖字段）。
func bundlePanelRow(over map[string]any) gin.H {
	row := gin.H{
		"VariantID": "v1", "SKUCode": "ADDON_001", "ProductName": "配件包",
		"Required": true, "DefaultQty": 2, "MinQty": 1, "MaxQty": 3,
		"Available": 6, "ItemPrice": "45.5", "CostPrice": "20", "Enabled": true, "Missing": false,
	}
	for k, v := range over {
		row[k] = v
	}
	return row
}

// bundleDetailPage 渲染商品详情页（bundle 为 nil 表示不给 Bundle 键 = variant 商品）。
func bundleDetailPage(t *testing.T, bundle gin.H) string {
	t.Helper()
	product := productRowForRender()
	if bundle != nil {
		product["Bundle"] = bundle
	}
	return renderAdminTemplate(t, "admin/product/product_detail.html", productPageLayoutData(gin.H{
		"title": "商品详情", "menu": "products",
		"Projects": []gin.H{}, "SelectedProject": "proj-1",
		"WarehouseOptions": []gin.H{}, "Err": "",
		"HasProduct": true, "ProductID": "p1", "BackURL": "/admin/products",
		"Product": product,
	}))
}

// bundleEditPage 渲染商品编辑页 —— 捆绑构成的**编辑入口**在这里（详情页只读）。
func bundleEditPage(t *testing.T, product gin.H) string {
	t.Helper()
	// 编辑表单要的几个键（真实 handler 由 productRow 与回填文本给出）。
	product["Subtitle"] = ""
	product["Unit"] = ""
	product["SEOTitle"] = ""
	product["SEODescription"] = ""
	return renderAdminTemplate(t, "admin/product/product_edit.html", productPageLayoutData(gin.H{
		"title": "编辑商品", "menu": "products",
		"Projects": []gin.H{}, "SelectedProject": "proj-1",
		"WarehouseOptions": []gin.H{}, "Err": "",
		"HasProduct": true, "ProductID": "p1", "BackURL": "/admin/products",
		"Product": product, "IsBundle": true,
		"Statuses":        []gin.H{{"Value": "draft", "Label": "草稿", "Selected": true}},
		"AttributeChecks": []gin.H{},
		"ImagesText":      "", "ImageAltsText": "", "WeightText": "", "DefaultPriceText": "",
	}))
}

// TestProductDetailBundlePanelRenders 区块的数据面：容器价、成员规则、成本与参考价。
func TestProductDetailBundlePanelRenders(t *testing.T) {
	out := bundleDetailPage(t, gin.H{
		"Loaded": true, "ErrorText": "", "BasePrice": "199",
		"Options": []gin.H{
			bundlePanelRow(nil),
			bundlePanelRow(map[string]any{
				"VariantID": "v2", "SKUCode": "ADDON_002", "ProductName": "赠品杯",
				"Required": false, "DefaultQty": 0, "MinQty": 0, "MaxQty": 0, "Available": 9,
			}),
		},
		"OptionCount": 2,
	})
	for _, want := range []string{
		"捆绑构成",
		"容器价（套餐价）", "199",
		// 成员事实：SKU + 所属商品名 + 必选/可选 + 默认/最小/最大 + 可用量。
		"ADDON_001", "配件包", "必选", "可选", "赠品杯",
		// 后台价格口径：成本（后台口径）与挂牌价（参考 · 不参与套餐价）分成两列。
		"成员成本（后台口径）", "成员挂牌价（参考 · 不参与套餐价）", "45.5", "20",
		// 最大数量填 0 = 不限，不是「最大 0 件」。
		"不限",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("捆绑构成区块缺少 %q", want)
		}
	}
	// 详情页只读：配置入口不在这一页，而在编辑页（两处都不缺才算「搬走了」）。
	if strings.Contains(out, bundlePanelEditLink) {
		t.Fatalf("详情页是只读页，捆绑配置入口不该在这里")
	}
	if edit := bundleEditPage(t, productRowForRender()); !strings.Contains(edit, bundlePanelEditLink) {
		t.Fatalf("编辑页缺少捆绑配置入口 %q", bundlePanelEditLink)
	}
	for _, want := range []string{"<td>2</td>", "<td>1</td>", "<td>3</td>", "<td>6</td>", "<td>9</td>"} {
		if !strings.Contains(out, want) {
			t.Fatalf("成员数量 / 可用量单元格缺少 %q", want)
		}
	}
	// 位置：商品级区块在「基本信息」之后、子资源表（变体）之前。
	// 只读页没有写表单 action 可当锚点，用区块标题当变体表的落点。
	basic, block := strings.Index(out, "基本信息"), strings.Index(out, "捆绑构成")
	variant := strings.Index(out, ">变体<")
	if basic < 0 || block < 0 || variant < 0 || !(basic < block && block < variant) {
		t.Fatalf("捆绑构成应在基本信息之后、变体之前（下标 %d / %d / %d）", basic, block, variant)
	}
}

// TestProductDetailBundlePanelEmptyAndBroken 空配置与读不出配置都要有可读提示。
func TestProductDetailBundlePanelEmptyAndBroken(t *testing.T) {
	empty := bundleDetailPage(t, gin.H{
		"Loaded": true, "ErrorText": "", "BasePrice": "199",
		"Options": []gin.H{}, "OptionCount": 0,
	})
	for _, want := range []string{"捆绑构成", "还没有配置捆绑构成"} {
		if !strings.Contains(empty, want) {
			t.Fatalf("空配置应给出提示，缺少 %q", want)
		}
	}
	// 空态下也要能走到配置入口 —— 它在编辑页。
	if edit := bundleEditPage(t, productRowForRender()); !strings.Contains(edit, bundlePanelEditLink) {
		t.Fatalf("空配置时编辑页仍应给出配置入口")
	}
	// 空态**要**渲染表头（表头常驻、空态整行落在 tbody 里）：这是本项目的空态表头约定，
	// 由 scripts/check-empty-state-table-head.sh 守门，且该门禁的允许清单里已明确记载
	// 「本文件各表真正的空态（bundle 成员表 :118 等）已按样板修复」。
	// 原先这里断言「空配置不该渲染成员表」，与那条约定正好相反 —— 是过时断言。
	if !strings.Contains(empty, "成员挂牌价（参考 · 不参与套餐价）") {
		t.Fatalf("空配置也应渲染表头（空态表头约定：表头常驻 + 空态整行进 tbody）")
	}

	// 读取失败（库存真源未接入 / 商品读不出来）：区块留在页面上并说明原因，
	// 容器价不拿成员价兜底（那是错的口径），只给 —。
	const reason = "配置读取失败：商品不存在，或库存真源端口未接入（无法给出可用量）。"
	broken := bundleDetailPage(t, gin.H{
		"Loaded": false, "ErrorText": reason, "BasePrice": "—",
		"Options": []gin.H{}, "OptionCount": 0,
	})
	if !strings.Contains(broken, reason) || !strings.Contains(broken, "role=\"alert\"") {
		t.Fatalf("读取失败应有可读提示（role=alert），实际 %s", broken)
	}
	if strings.Contains(broken, bundlePanelEditLink) {
		t.Fatalf("详情页是只读页，捆绑配置入口不该在这里")
	}
	if strings.Contains(broken, "容器价（套餐价）") {
		t.Fatalf("读取失败时容器价应留空（—），不该显示成员价兜底")
	}
}

// TestProductDetailBundlePanelMissingAndDisabled 已删除 / 已停用的成员都不能静默丢项。
func TestProductDetailBundlePanelMissingAndDisabled(t *testing.T) {
	out := bundleDetailPage(t, gin.H{
		"Loaded": true, "ErrorText": "", "BasePrice": "199",
		"Options": []gin.H{
			// 变体已被删除：读模型里只剩 variantId（SKUCode 为空）。
			bundlePanelRow(map[string]any{
				"VariantID": "v-dead", "SKUCode": "", "ProductName": "",
				"ItemPrice": "—", "CostPrice": "—", "Available": 0, "Enabled": false, "Missing": true,
			}),
			bundlePanelRow(map[string]any{"VariantID": "v-off", "SKUCode": "ADDON_OFF", "Enabled": false}),
		},
		"OptionCount": 2,
	})
	for _, want := range []string{"成员 SKU 已删除", "v-dead", "ADDON_OFF", "停用", "启用"} {
		if !strings.Contains(out, want) {
			t.Fatalf("已删除 / 已停用的成员应有可读标注，缺少 %q", want)
		}
	}
}

// TestProductDetailBundlePanelAbsentForVariant type=variant 的商品没有 Bundle 键：
// 整块不渲染，页面其余部分照常输出（缺键若直接参与 if 会中途截断渲染）。
func TestProductDetailBundlePanelAbsentForVariant(t *testing.T) {
	out := bundleDetailPage(t, nil)
	if strings.Contains(out, "捆绑构成") {
		t.Fatalf("variant 商品不该渲染捆绑构成区块")
	}
	// 尾部锚点：渲染没有在中间中断（否则变体表与评分表整块消失）。
	// 只读页的锚点是变体表与评分表的**内容**；写入口在编辑页（同一份数据的另一处落点）。
	for _, want := range []string{"各仓库存", "变体", "评分"} {
		if !strings.Contains(out, want) {
			t.Fatalf("variant 商品的详情页被截断，缺少 %q", want)
		}
	}
	edit := bundleEditPage(t, productRowForRender())
	for _, want := range []string{"action=\"/admin/products/variant/create\"", "action=\"/admin/products/rating/add\"", "保存变体清单"} {
		if !strings.Contains(edit, want) {
			t.Fatalf("编辑页缺少 %q", want)
		}
	}
}

// TestBundlePanelRowsBackofficePriceShape 行数据的价格口径：成本是后台口径、
// 挂牌价只是参考值，变体已删除时两者都给 —（0 会被读成「这个成员不要钱」）。
func TestBundlePanelRowsBackofficePriceShape(t *testing.T) {
	cost := 20.0
	// 取词用直通函数（本用例断言的是价格口径，不是文案）：来源标签在批次 C 引入。
	passthrough := func(key, fallback string) string { return fallback }
	rows := bundlePanelRows(passthrough, []*productdto.BundleOptionDetail{
		{SKUCode: "A1", ItemPrice: 45.5, CostPrice: &cost, Enabled: true},
		{SKUCode: "A2", ItemPrice: 10, CostPrice: nil, Enabled: false},
		{SKUCode: "", ItemPrice: 0, CostPrice: nil, Enabled: false},
		nil,
	})
	if len(rows) != 3 {
		t.Fatalf("nil 项应被跳过，实际 %d 行", len(rows))
	}
	if rows[0]["ItemPrice"] != "45.5" || rows[0]["CostPrice"] != "20" {
		t.Fatalf("正常成员的价格口径不对：%+v", rows[0])
	}
	if rows[1]["CostPrice"] != "—" || rows[1]["Missing"] != false || rows[1]["Enabled"] != false {
		t.Fatalf("成本缺失应显示 —，且停用与删除要能分开：%+v", rows[1])
	}
	if rows[2]["Missing"] != true || rows[2]["ItemPrice"] != "—" || rows[2]["CostPrice"] != "—" {
		t.Fatalf("已删除的成员应标 Missing 且价格全为 —：%+v", rows[2])
	}
}
