package templates

import (
	"strings"
	"testing"

	"github.com/CloudyKit/jet/v6"
)

// i18n_products_inventory_render_test.go — 组 D（商品 / 库存类后台模板）i18n 快照回归。
//
// 为什么非要渲染：Jet 的 if 遇到缺失或类型不符的键会**中断渲染但 HTTP 仍是 200**，
// 现象是页面后半截整块消失（表格、表单全没了）。所以每页都断言**尾部标记**确实出现在输出里。
//
// 本文件同时锁住两件事：
//  1. 兜底：i18n 未初始化（测试进程不连库）时渲染中文原文，不报错；
//  2. 命中：注入「按 key 返回 EN[key]」的翻译函数后，文案整体切英文 —— 若某个 key 没有被 t() 包住
//     （或 key 写法写坏导致静默回退 fallback），中文原文会残留，断言就会失败。

func groupDSet(t *testing.T) *jet.Set {
	t.Helper()
	return jet.NewSet(jet.NewOSFileSystemLoader("."), jet.WithTemplateNameExtensions([]string{"", ".html"}))
}

func groupDData(extra map[string]any) map[string]any {
	d := map[string]any{
		"lang": "zh-CN", "langs": LanguageOptions("zh-CN"),
		"title": "组D", "t": TranslateFunc("zh-CN"), "csrf_token": "tok",
		// 列表页的新建/编辑入口改走抽屉后，按钮按权限显隐（运行时由 shell.Prepare 注入，
		// 测试外壳给一份全集）：否则每加一个入口就要往各页数据里补一个权限码，
		// 漏补的表现是「按钮不渲染、wants 断言红」，与模板本身无关。
		"PermSet": map[string]any{
			"contenttemplate:create": true, "contenttemplate:update": true,
			"product:create": true, "product:update": true,
			"product:brand_create": true, "product:category_create": true,
			"product:tag_create": true, "product:attribute_create": true,
			"inventory:warehouse_create": true, "inventory:source_create": true,
			"inventory:purchase_create": true, "inventory:reason_create": true,
			"order:coupon_create": true,
		},
	}
	for k, v := range extra {
		d[k] = v
	}
	return d
}

func groupDProjects() []map[string]any {
	return []map[string]any{{"ID": "pr1", "Name": "站点"}}
}

func groupDProductRow() map[string]any {
	return map[string]any{
		"ID": "p1", "Name": "商品一", "Slug": "p-one", "Status": "draft",
		"VariantCount": 1, "PriceMin": "10.00", "PriceMax": "20.00", "AttributeIDsCSV": "a1",
		"Attributes": []map[string]any{{"Name": "颜色", "IsVariation": true, "ValueCount": 2}},
		"Variants": []map[string]any{{
			"ID": "v1", "SKUCode": "SZ_TEE_001", "Spec": "红 / M", "Price": "19.00",
			"ComparePrice": "29.00", "CostPrice": "8.00", "Enabled": true, "StockTotal": 5,
		}},
		"HasRating": true, "RatingAvg": "4.50", "RatingCount": 2,
		"Ratings": []map[string]any{{"ID": "r1", "Score": "5", "Source": "manual", "CreatedAt": "2026-01-01"}},
		"VariationAttributes": []map[string]any{{
			"ID": "a1", "Name": "颜色",
			"Values": []map[string]any{{"ID": "av1", "Label": "红", "Enabled": true}},
		}},
		"CategoryIDs": []string{"c1"}, "PrimaryCategoryName": "男装", "BrandName": "山野",
		"CategoryChecks": []map[string]any{{"ID": "c1", "Label": "男装", "Checked": true}},
		"PrimaryOptions": []map[string]any{{"ID": "c1", "Label": "男装", "Selected": true}},
		"BrandOptions":   []map[string]any{{"ID": "b1", "Label": "山野", "Selected": true}},
		"TagChecks":      []map[string]any{{"ID": "t1", "Name": "新品", "Checked": true}},
		"AutoTags":       []map[string]any{{"Name": "热销", "RuleLabel": "销量前 10"}},
	}
}

// assertGroupDPage 渲染一页并断言尾部标记存在（中断会丢尾部）且无模板语法残留。
func assertGroupDPage(t *testing.T, name string, data map[string]any, tail ...string) string {
	t.Helper()
	out, err := render(t, groupDSet(t), "admin/"+name, data)
	if err != nil {
		t.Fatalf("%s 渲染失败: %v", name, err)
	}
	for _, want := range tail {
		if !strings.Contains(out, want) {
			t.Fatalf("%s 缺少尾部标记 %q（模板可能中途中断）", name, want)
		}
	}
	if strings.Contains(out, "{{/") {
		t.Fatalf("%s 输出残留模板语法字面量", name)
	}
	if strings.Contains(out, "EN[") {
		t.Fatalf("%s 中文兜底渲染不应出现英文标记", name)
	}
	return out
}

func TestGroupDProductsPageRenders(t *testing.T) {
	data := groupDData(map[string]any{
		"SelectedProject": "pr1", "Err": "",
		"Projects":         groupDProjects(),
		"WarehouseOptions": []map[string]any{{"ID": "w1", "Label": "苏州仓"}},
		"Products":         []map[string]any{groupDProductRow()},
	})
	out := assertGroupDPage(t, "products", data,
		"商品列表", "多语言", "保存手工标签", "各仓库存", "评分 0~5", "生成全部组合", "自动标签（按规则重算维护，不能手工改动）")
	// range 体内取词（{{ .["t"] }} 在 range 内仍指向根数据）确实生效：
	// 「多语言」「各仓库存」「删除」都来自 range 体内，缺一则说明该写法在真实模板里失效。
	for _, want := range []string{"多语言", "各仓库存"} {
		if !strings.Contains(out, want) {
			t.Fatalf("range 体内取词失效，缺少 %q", want)
		}
	}
}

func TestGroupDPricingPageRenders(t *testing.T) {
	data := groupDData(map[string]any{
		"SelectedProject": "pr1", "Err": "", "Applied": "",
		"Projects": groupDProjects(),
		"Form": map[string]any{
			"RuleType": "markup", "Multiplier": "2", "Amount": "1", "Margin": "0.3",
			"Rounding": "none", "Scope": "sku", "TargetID": "", "Status": "",
			"Keyword": "", "CategoryID": "", "BrandID": "", "TagID": "", "Note": "",
		},
		"Rules":      []map[string]any{{"Type": "markup", "Name": "成本加价", "RequiresCost": true, "Params": "amount"}},
		"Roundings":  []map[string]any{{"Value": "none", "Name": "不处理"}},
		"HasPreview": true,
		"PreviewSummary": map[string]any{
			"RuleLabel": "成本加价", "RoundingLabel": "不处理", "ScopeLabel": "单个 SKU（变体 id）",
			"TargetCount": 1, "ChangedCount": 1, "UnchangedCount": 0, "SkippedCount": 0,
		},
		"PreviewRows": []map[string]any{{
			"ProductName": "商品一", "SKUCode": "SKU1", "CostPrice": "8.00", "OldPrice": "10.00",
			"NewPrice": "12.00", "Diff": "+2.00", "IsChanged": true, "StatusLabel": "已改价", "ReasonLabel": "",
		}},
		"History": []map[string]any{{
			"RuleLabel": "成本加价", "ScopeLabel": "单个 SKU", "RoundingLabel": "不处理",
			"ChangedCount": 1, "VariantCount": 1, "CreatedAtText": "2026-01-01", "OperatorID": "u1",
			"FilterLabel": "", "Note": "国庆前提价",
			"Items": []map[string]any{{
				"ProductName": "商品一", "SKUCode": "SKU1", "OldPrice": "10.00", "NewPrice": "12.00", "Diff": "+2.00",
			}},
		}},
	})
	assertGroupDPage(t, "product_pricing", data,
		"内置定价规则", "应用调价（落库）", "试算预览（未落库）", "调价留痕", "备注：国庆前提价")
}

func TestGroupDAttributesPageRenders(t *testing.T) {
	data := groupDData(map[string]any{
		"Err": "", "SelectedProject": "pr1", "Projects": groupDProjects(),
		"Attributes": []map[string]any{{
			"ID": "a1", "Name": "颜色", "Key": "color", "IsVariation": true, "ValueCount": 2, "Sort": 0,
			"RowsCtx": map[string]any{
				"GroupID": "a1",
				"Rows":    []map[string]any{{"ID": "av1", "Key": "red", "Label": "红", "Sort": 0, "Enabled": true}},
			},
		}},
		"NewRowsCtx": map[string]any{"GroupID": "new", "Rows": []map[string]any{}},
	})
	assertGroupDPage(t, "product_attributes", data,
		"参与变体", "属性组列表", "保存属性组", "保存属性值", "删除属性组")
}

func TestGroupDBrandsPageRenders(t *testing.T) {
	data := groupDData(map[string]any{
		"Err": "", "SelectedProject": "pr1", "Projects": groupDProjects(),
		"Brands": []map[string]any{{
			"ID": "b1", "Name": "山野", "Slug": "shanye", "Sort": 0, "Logo": "",
			"SEOTitle": "", "SEODescription": "", "Description": "",
		}},
	})
	assertGroupDPage(t, "product_brands", data, "商品品牌", "品牌列表", "保存品牌", "SEO 评分", "删除品牌")
}

func TestGroupDCategoriesPageRenders(t *testing.T) {
	data := groupDData(map[string]any{
		"Err": "", "SelectedProject": "pr1", "Projects": groupDProjects(),
		"Options": []map[string]any{{"ID": "c1", "Label": "男装"}},
		"Categories": []map[string]any{{
			"ID": "c1", "Label": "男装", "Slug": "mens", "Sort": 0, "SEOTitle": "",
			"ParentID": "", "Name": "男装", "Image": "", "SEODescription": "", "Description": "",
		}},
	})
	assertGroupDPage(t, "product_categories", data, "分类树", "（顶级分类）", "保存分类", "删除分类")
}

func TestGroupDTagsPageRenders(t *testing.T) {
	data := groupDData(map[string]any{
		"Err": "", "SelectedProject": "pr1", "Projects": groupDProjects(),
		"RuleTypes": []map[string]any{{"Type": "new", "Name": "新品", "Params": "days"}},
		"Tags": []map[string]any{{
			"ID": "t1", "Name": "新品", "Slug": "new", "IsRule": true, "KindLabel": "自动标签",
			"RuleLabel": "新品（days）", "ProductCount": 1, "RecalcAt": "2026-01-01", "Sort": 0,
			"RuleType": "new", "RuleDays": "30", "RuleMinPrice": "", "RuleMaxPrice": "",
			"Products": []map[string]any{{"Name": "商品一", "Slug": "p-one", "Status": "draft"}},
		}},
	})
	assertGroupDPage(t, "product_tags", data,
		"内置规则类型", "新建标签", "保存标签", "立即重算这个标签", "命中的商品", "删除标签")
}

func TestGroupDBundlePageRenders(t *testing.T) {
	data := groupDData(map[string]any{
		"Err": "", "SelectedProject": "pr1", "Projects": groupDProjects(),
		"SelectedProduct": "p1", "MaxOptions": 5, "SkuCount": 3,
		"Products": []map[string]any{{"ID": "p1", "Name": "套餐"}},
		"Detail": map[string]any{
			"ProductName": "套餐", "BasePrice": "99.00",
			"Config": map[string]any{"MaxOptions": 2, "MinTotalQty": 1, "MaxTotalQty": 5},
		},
		"Rows": []map[string]any{{
			"Options":    []map[string]any{{"ID": "v1", "Label": "SKU1", "Selected": true}},
			"Required":   true,
			"VariantID":  "v1",
			"Available":  10,
			"DefaultQty": 1, "MinQty": 1, "MaxQty": 3,
		}},
	})
	assertGroupDPage(t, "product_bundle", data, "捆绑配置", "选项规则", "保存配置", "前台配置器预览")
}

func TestGroupDTranslationsPageRenders(t *testing.T) {
	data := groupDData(map[string]any{
		"Errors": []string{}, "Saved": false, "SavedNote": "",
		"ProjectOptions": []map[string]any{{"ID": "pr1", "Name": "站点", "Selected": true}},
		"ProductID":      "p1", "ProjectID": "pr1", "Lang": "en-US", "Done": 1, "Total": 2,
		"Langs": []map[string]any{{"Code": "en-US", "Label": "en-US", "Active": true}},
		"Groups": []map[string]any{{
			"Title": "商品", "Subtitle": "商品自身",
			"Rows": []map[string]any{{
				"FieldLabel": "商品名", "Context": "product.name", "Source": "商品一",
				"Rich": false, "Target": "", "SourceHash": "h1", "Translated": false, "Engine": "",
			}},
		}},
	})
	assertGroupDPage(t, "product_translations", data,
		"商品多语言", "完成度", "查看整个工程", "字段", "保存全部", "写入 sys_translation")
}

func TestGroupDDetailTemplatePageRenders(t *testing.T) {
	data := groupDData(map[string]any{
		"Err": "", "Ready": true, "SelectedProject": "pr1", "Projects": groupDProjects(),
		"Product":           map[string]any{"ID": "p1", "Name": "商品一", "URLPath": "/p/p-one"},
		"InstanceExists":    true,
		"InstanceStatus":    "published",
		"BoundTemplateID":   "tpl1",
		"DefaultTemplateID": "tpl1",
		"TemplateCount":     1,
		"InstanceURL":       "/p/p-one",
		"Templates": []map[string]any{{
			"ID": "tpl1", "Name": "默认详情", "DraftVersion": 3,
			"IsDefault": true, "IsBound": true, "UpdatedAt": "2026-01-01",
		}},
	})
	assertGroupDPage(t, "product_detail_template", data,
		"商品详情页模板", "当前使用", "切换模板并重新发布", "改 URL", "命名模板（", "新建命名模板")

	// 未发布分支（.InstanceExists=false）走另一条表单：发布入口与「未发布」徽标必须渲染出来。
	data["InstanceExists"] = false
	assertGroupDPage(t, "product_detail_template", data,
		"未发布", "使用模板", "发布详情页", "新建命名模板")
}

func TestGroupDInventoryPageRenders(t *testing.T) {
	data := groupDData(map[string]any{
		"Err": "", "Ok": "", "SelectedProject": "pr1", "Projects": groupDProjects(),
		"SelectedVariant": "v1", "SelectedSKU": "SKU1",
		"Warehouses": []map[string]any{{
			"ID": "w1", "Name": "苏州仓", "Code": "SZ", "IsDefault": true,
			"StatusLabel": "启用中", "Sort": 0, "Status": "active",
		}},
		"VariantOptions": []map[string]any{{"VariantID": "v1", "Label": "商品一 / 红 / M"}},
		"StockRows": []map[string]any{{
			"WarehouseName": "苏州仓", "WarehouseCode": "SZ", "SKUCode": "SKU1",
			"Quantity": 5, "UpdatedAt": "2026-01-01",
		}},
		"Directions": []map[string]any{{"Value": "in", "Label": "入库"}},
		"Reasons": []map[string]any{{
			"Code": "purchase_in", "DirectionLabel": "入库", "Name": "采购入库",
			"IsBuiltin": true, "StatusLabel": "启用",
		}},
		"Movements": []map[string]any{{
			"CreatedAt": "2026-01-01", "SKUCode": "SKU1", "WarehouseName": "苏州仓",
			"WarehouseCode": "SZ", "DirectionLabel": "入库", "Direction": "in", "Quantity": 5,
			"QuantityBefore": 0, "QuantityAfter": 5, "ReasonName": "采购入库",
			"ReasonCode": "purchase_in", "SourceRef": "PO-1", "SourceType": "purchase", "OperatorID": "u1",
		}},
	})
	assertGroupDPage(t, "inventory", data,
		"库存管理", "保存仓库", "查看各仓库存", "提交变动", "新建自定义原因", "库存流水（最近 ")
}

func TestGroupDPurchasesPageRenders(t *testing.T) {
	data := groupDData(map[string]any{
		"Err": "", "Ok": "", "SelectedProject": "pr1", "Projects": groupDProjects(),
		"FilterStatus": "", "FilterSource": "", "FilterKeyword": "", "HistorySKU": "",
		"ReceiptRequestID": "r1", "ProductionRequestID": "r2",
		"Sources":         []map[string]any{{"ID": "s1", "Label": "苏州通达", "Name": "苏州通达"}},
		"Warehouses":      []map[string]any{{"ID": "w1", "Label": "苏州仓", "Name": "苏州仓"}},
		"DraftLines":      []map[string]any{{}},
		"InternalSources": []map[string]any{{"ID": "s2", "Label": "自家工厂"}},
		"StatusOptions":   []map[string]any{{"Value": "partial", "Label": "部分入库"}},
		"VariantOptions":  []map[string]any{{"VariantID": "v1", "SKUCode": "SKU1", "Label": "商品一 / 红 / M"}},
		"Orders": []map[string]any{{
			"ID": "o1", "Code": "PO-1", "Status": "partial", "StatusLabel": "部分入库",
			"SourceName": "苏州通达", "SourceTypeLabel": "外部供应商", "WarehouseName": "苏州仓",
			"ReceivedQuantity": "1", "TotalQuantity": "2", "RequestID": "req1",
			"OrderedAt": "2026-01-01", "OperatorID": "u1", "Remark": "",
			"Lines": []map[string]any{{
				"ID": "l1", "SKUCode": "SKU1", "Quantity": "2", "ReceivedQuantity": "1",
				"OutstandingQuantity": "1", "UnitPrice": "8.00", "Open": true,
			}},
		}},
		"History": []map[string]any{{
			"ReceivedAt": "2026-01-01", "ReceiptCode": "RC-1", "KindLabel": "采购收货",
			"OrderCode": "PO-1", "SourceName": "苏州通达", "SourceTypeLabel": "外部供应商",
			"WarehouseName": "苏州仓", "SKUCode": "SKU1", "Quantity": "1", "UnitPrice": "8.00",
			"CostUpdated": true, "OperatorID": "u1",
		}},
	})
	assertGroupDPage(t, "inventory_purchases", data,
		"采购入库", "新建采购单", "登记入库", "生产入库（自家工厂）", "进货历史", "查进货历史")
}

func TestGroupDSourcesPageRenders(t *testing.T) {
	data := groupDData(map[string]any{
		"Err": "", "Ok": "", "SelectedProject": "pr1", "Projects": groupDProjects(),
		"HasSummary": true,
		"Summary": map[string]any{
			"Total": 1, "Internal": 0, "External": 1, "RelatedParty": 0,
			"Unrelated": 1, "SettlePriced": 0,
			"Groups": []map[string]any{{
				"TypeLabel": "外部供应商", "Type": "external", "RelatedPartyLabel": "非关联方", "Count": 1,
			}},
		},
		"TypeOptions":    []map[string]any{{"Value": "external", "Label": "外部供应商"}},
		"RelatedOptions": []map[string]any{{"Value": "false", "Label": "非关联方"}},
		"StatusOptions":  []map[string]any{{"Value": "active", "Label": "启用中"}},
		"FilterType":     "", "FilterRelated": "", "FilterStatus": "", "FilterKeyword": "", "FilterStatusAll": "all",
		"Sources": []map[string]any{{
			"ID": "s1", "Name": "苏州通达", "Code": "SZ_SUPPLIER", "Type": "external",
			"TypeLabel": "外部供应商", "RelatedParty": false, "RelatedPartyLabel": "非关联方",
			"Status": "active", "StatusLabel": "启用中", "HasSettlePrice": true,
			"SettlePrice": "8.00", "Sort": 0, "Config": "{}",
		}},
	})
	assertGroupDPage(t, "inventory_sources", data,
		"货源管理", "货源总数", "新建货源", "筛选（报表区分维度）", "保存货源", "删除货源")
}

// TestGroupDEnglishSwitch 注入「按 key 返回 EN[key]」的翻译函数，确认文案整体切英文。
// 若有 key 没被 t() 包住、或 key 写坏导致回退 fallback，中文原文会残留 —— 断言即失败。
func TestGroupDEnglishSwitch(t *testing.T) {
	enT := func(key, fallback string) string { return "EN[" + key + "]" }

	products := groupDData(map[string]any{
		"SelectedProject": "pr1", "Err": "",
		"Projects":         groupDProjects(),
		"WarehouseOptions": []map[string]any{{"ID": "w1", "Label": "苏州仓"}},
		"Products":         []map[string]any{groupDProductRow()},
	})
	products["t"] = enT
	out, err := render(t, groupDSet(t), "admin/products", products)
	if err != nil {
		t.Fatalf("products 英文渲染失败: %v", err)
	}
	for _, want := range []string{
		"EN[admin.products.title]", "EN[admin.products.row.translations]",
		"EN[admin.products.variant.stockLink]", "EN[admin.products.tags.save]",
		"EN[admin.products.hint.attrLead]", "EN[admin.products.taxonomy.attachedMid]",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("products 英文渲染缺少 %q", want)
		}
	}
	// 页面级文案不应再有中文残留（数据里的中文如「商品一」仍会出现，故只查固定文案）。
	for _, forbidden := range []string{"商品列表", "保存手工标签", "各仓库存", "多语言", "上一操作"} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("products 英文渲染残留中文文案 %q（该处未走 t()）", forbidden)
		}
	}

	purchases := groupDData(map[string]any{
		"Err": "", "Ok": "", "SelectedProject": "pr1", "Projects": groupDProjects(),
		"FilterStatus": "", "FilterSource": "", "FilterKeyword": "", "HistorySKU": "",
		"ReceiptRequestID": "r1", "ProductionRequestID": "r2",
		"Sources": []map[string]any{}, "Warehouses": []map[string]any{}, "DraftLines": []map[string]any{},
		"InternalSources": []map[string]any{}, "StatusOptions": []map[string]any{},
		"VariantOptions": []map[string]any{}, "Orders": []map[string]any{}, "History": []map[string]any{},
	})
	purchases["t"] = enT
	out, err = render(t, groupDSet(t), "admin/inventory_purchases", purchases)
	if err != nil {
		t.Fatalf("inventory_purchases 空数据渲染失败: %v", err)
	}
	for _, want := range []string{
		"EN[admin.inventory_purchases.title]", "EN[admin.inventory_purchases.create.title]",
		"EN[admin.inventory_purchases.production.title]", "EN[admin.inventory_purchases.history.title]",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("inventory_purchases 空数据渲染缺少 %q", want)
		}
	}
}
