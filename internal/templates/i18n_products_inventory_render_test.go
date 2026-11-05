package templates

import (
	"fmt"
	productdto "go_wp/internal/module/product/dto"
	"strings"
	"testing"

	"github.com/CloudyKit/jet/v6"

	"go_wp/pkg/i18n"
)

func groupDSet(t *testing.T) *jet.Set {
	t.Helper()
	set := jet.NewSet(testTemplateLoader(), jet.WithTemplateNameExtensions([]string{"", ".html"}))

	injectGlobals(set)
	return set
}

func groupDRenderScope() RenderScope {
	return groupDRenderScopeFor("zh-CN")
}

func groupDRenderScopeFor(lang string) RenderScope {
	return RenderScope{
		Lang: lang, Translate: TranslateFunc(lang),
		TranslateNamed: func(key, fallback string, args map[string]any) string {
			params := make(map[string]string, len(args))
			for k, v := range args {
				params[k] = fmt.Sprint(v)
			}
			return i18n.FillTranslate(TranslateFunc(lang), key, fallback, params)
		},
	}
}

func groupDData(extra map[string]any) map[string]any {
	scope := groupDRenderScope()
	d := map[string]any{
		"lang": "zh-CN", "langs": LanguageOptions("zh-CN"),
		"title": "组D", "t": scope.Translate, "csrf_token": "tok",
		"tn": scope.TranslateNamed,
		"PermSet": map[string]bool{"product:rating_add": true, "product:rating_delete": true,
			"product:seo_score": true, "product:category_seo_score": true, "product:brand_seo_score": true},

		"Buttons": map[string]any{
			"contenttemplate.create": true, "contenttemplate.update": true,
			"product.create": true, "product.update": true,

			"product.variant_create": true, "product.variant_generate": true,
			"product.brand_create": true, "product.category_create": true,
			"product.tag_create": true, "product.attribute_create": true,
			"product.brand_update": true, "product.category_update": true,
			"product.tag_update": true, "product.attribute_update": true,
			"product.brand_delete": true, "product.category_delete": true,
			"product.tag_delete": true, "product.attribute_delete": true,
			"inventory.warehouse_create": true, "inventory.source_create": true,
			"inventory.warehouse_update": true, "inventory.source_update": true,
			"inventory.warehouse_delete": true, "inventory.source_delete": true,
			"inventory.purchase_create": true, "inventory.reason_create": true,
			"order.coupon_create": true,
		},

		"FilterKeyword": "", "FilterStatus": "", "Page": 1, "Limit": 20, "Total": 0,

		"ListQuery": "",
		"Statuses": []map[string]any{
			{"Value": "", "Label": "全部状态", "Selected": true},
			{"Value": "draft", "Label": "草稿", "Selected": false},
			{"Value": "published", "Label": "已上架", "Selected": false},
			{"Value": "archived", "Label": "已下架", "Selected": false},
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

func assertGroupDPage(t *testing.T, name string, data map[string]any, tail ...string) string {
	t.Helper()
	out, err := render(t, groupDSet(t), "admin/"+name, data, groupDRenderScope())
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
	data := productCatalogData(map[string]any{
		"SelectedProject": "pr1", "Err": "",
		"Projects":         groupDProjects(),
		"WarehouseOptions": []map[string]any{{"ID": "w1", "Name": "苏州仓", "Code": "SZ", "IsDefault": true}},
		"Products":         []*productdto.ProductResp{groupDProductRow()},
	})

	out := assertGroupDPage(t, "products", data,
		"商品列表", "价格区间", "分类", "品牌", "标签", "编辑", "预览", "删除")

	for _, want := range []string{"products-filter-keyword", "products-filter-status", "重置"} {
		if !strings.Contains(out, want) {
			t.Fatalf("列表页缺少筛选控件 %q", want)
		}
	}

	if !strings.Contains(out, "新品、热销") {
		t.Fatalf("标签列应显示标签名，实际输出未见 %q", "新品、热销")
	}
	for _, forbidden := range []string{"手工 1 · 自动 1"} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("标签列不该显示来源分解 %q", forbidden)
		}
	}

	if !strings.Contains(out, `href="/admin/products/detail?project=pr1&amp;product=p1"`) {
		t.Fatalf("列表页缺少「详情」链接")
	}

	if !strings.Contains(out, `href="/admin/products/edit?project=pr1&amp;product=p1"`) {
		t.Fatalf("列表页缺少「编辑」链接")
	}

	for _, want := range []string{"编辑", "预览", "删除"} {
		if !strings.Contains(out, want) {
			t.Fatalf("range 体内取词失效，缺少 %q", want)
		}
	}

	for _, forbidden := range []string{"<th>URL 段</th>", "/p-one", "/admin/products/translations?project=pr1&amp;product=p1"} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("列表页不该再出现 %q（URL 段列已删除、多语言入口已移入编辑页）", forbidden)
		}
	}

	for _, forbidden := range []string{"新建变体", "生成组合", "添加评分", "保存手工标签", "SEO 评分", "各仓库存"} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("列表页不该再渲染 %q（已移到商品详情页）", forbidden)
		}
	}
}

func TestGroupDProductEditPageRenders(t *testing.T) {
	row := groupDProductRow()
	row.Subtitle = "副标题"
	row.Unit = "件"
	row.SEOTitle = "SEO 标题"
	row.SEODescription = "SEO 描述"
	data := productCatalogData(map[string]any{
		"SelectedProject": "pr1", "Err": "",
		"Projects": groupDProjects(),

		"WarehouseOptions": []map[string]any{{"ID": "w1", "Name": "苏州仓", "Code": "SZ", "IsDefault": true}},
		"ProductID":        "p1", "HasProduct": true, "Product": row,
		"BackURL": "/admin/products?project=pr1",

		"AttributeChecks": []map[string]any{{"ID": "a1", "Label": "颜色（color）", "IsVariation": true, "Checked": true}},
		"ImagesInput":     "https://cdn.example.com/1.jpg",
		"ImageAltsInput":  "图一",
		"WeightInput":     "0.5", "DefaultPriceInput": "10.00",
		"TplPanel": map[string]any{
			"Available": true, "InstanceID": "inst1", "TemplateID": "tpl1", "Status": "published",
			"RenderMode": "template", "AffectedTemplateCount": 3, "HasAffectedTemplateCount": true,
			"Snapshots": []map[string]any{},
		},
	})
	out := assertGroupDPage(t, "product_edit", data,
		"基本信息", "属性引用", "分类与品牌", "标签", "保存",

		"变体", "保存变体清单", "生成组合", "添加评分", "SEO 评分")

	for _, want := range []string{
		`hx-post="/api/product/update"`,
		`hx-target="#product-edit-result"`,
		`data-write-builder="product-edit"`,
		`name="name"`, `name="subtitle"`, `name="slug"`, `name="sku"`, `name="status"`,
		`name="defaultPrice"`, `name="unit"`, `name="weight"`,

		`name="descriptionHtml"`,
		`name="images"`, `name="imageAlts"`,
		`name="attributeIds"`, `name="categoryIds"`, `name="primaryCategoryId"`,
		`name="brandId"`, `name="tagIds"`,

		"/admin/products/translations?project=pr1&amp;product=p1",

		"/admin/products/detail?project=pr1&amp;product=p1",

		`hx-post="/api/product/variant/save"`,
		`data-write-builder="product-variant-rows"`,
		`data-preview-url="/api/product/variant/preview"`,
		`hx-post="/api/product/variant/create"`,
		`hx-post="/api/product/rating/add"`,
		`hx-post="/api/product/rating/delete"`,

		`data-drawer-open="#tpl-product-seo"`,
		`hx-post="/api/product/seo-score"`,
		`hx-target="find [data-write-result]"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("商品编辑页缺少 %q", want)
		}
	}

}

func TestGroupDProductDetailPageRenders(t *testing.T) {
	data := productCatalogData(map[string]any{
		"SelectedProject": "pr1", "Err": "",
		"Projects":         groupDProjects(),
		"WarehouseOptions": []map[string]any{{"ID": "w1", "Name": "苏州仓", "Code": "SZ", "IsDefault": true}},
		"ProductID":        "p1", "HasProduct": true, "Product": groupDProductRow(),
		"BackURL": "/admin/products?project=pr1",
		"TplPanel": map[string]any{
			"Available": true, "InstanceID": "inst1", "TemplateID": "tpl1", "Status": "published",
			"RenderMode": "template",
		},
	})
	out := assertGroupDPage(t, "product_detail", data,
		"基本信息", "属性引用", "标签", "变体", "评分", "各仓库存",
		"自动标签（按规则重算维护，不能手工改动）")

	if !strings.Contains(out, `action="/admin/products/template/preview"`) || !strings.Contains(out, `data-product-preview-form`) {
		t.Fatalf("详情页缺少「预览」表单（只读渲染的唯一 POST）")
	}

	for _, forbidden := range []string{
		`action="/admin/products/attributes"`,
		`action="/admin/products/taxonomy"`,
		`action="/admin/products/tags"`,
		`action="/admin/products/seo-score"`,
		`hx-post="/api/product/variant/save"`,
		`hx-post="/api/product/variant/create"`,
		`hx-post="/api/product/rating/add"`,
		`hx-post="/api/product/rating/delete"`,
		`hx-post="/api/product/detail-template/reapply-preset"`,
		`hx-post="/api/product/detail-template/rollback-document"`,
		`action="/admin/products/delete"`,
		"保存属性引用", "保存分类与品牌", "保存手工标签", "保存变体清单",
		"新建变体", "生成组合", "添加评分", "SEO 评分",
	} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("详情页是只读页，不该出现写入口 %q", forbidden)
		}
	}

}

func TestGroupDPricingPageRenders(t *testing.T) {
	data := groupDData(map[string]any{
		"Scope":     map[string]any{"ProjectID": "pr1", "Projects": groupDProjects()},
		"Rules":     []map[string]any{{"Type": "cost_markup", "RequiresCost": true}},
		"Roundings": []map[string]any{{"Value": "none"}},
		"History": []map[string]any{{
			"RuleType": "cost_markup", "RuleValues": map[string]any{"Value": 1.0, "HasValue": true},
			"Scope": "sku", "Rounding": "none",
			"ChangedCount": 1, "VariantCount": 1, "CreatedAt": "2026-01-01T10:00:00Z", "OperatorID": "u1",
			"FilterParts": []map[string]any{}, "Note": "国庆前提价",
			"Items": []map[string]any{{
				"ProductName": "商品一", "SKUCode": "SKU1", "OldPrice": 10.00, "NewPrice": 12.00, "Diff": +2.00,
			}},
		}},
	})
	assertGroupDPage(t, "product_pricing", data,
		"内置定价规则", "应用调价（落库）", "试算预览（未落库）", "调价留痕", "备注：国庆前提价")
}

func TestGroupDAttributesPageRenders(t *testing.T) {
	data := taxonomyAttributes(&productdto.AttributeResp{ID: "a1", Name: "颜色", Key: "color", IsVariation: true, Values: []productdto.AttributeValueResp{{ID: "av1", Key: "red", Label: "红", Enabled: true}}})
	assertGroupDPage(t, "product_attributes", data, "参与变体", "属性组", "保存属性组", "保存属性值", "删除")
}

func TestGroupDBrandsPageRenders(t *testing.T) {
	assertGroupDPage(t, "product_brands", taxonomyBrands(&productdto.BrandResp{ID: "b1", Name: "山野", Slug: "shanye"}), "商品品牌", "品牌列表", "保存品牌", "SEO 评分", "删除")
}

func TestGroupDCategoriesPageRenders(t *testing.T) {
	assertGroupDPage(t, "product_categories", taxonomyCategories(&productdto.CategoryResp{ID: "c1", ProjectID: "pr1", Name: "男装", Slug: "mens"}), "分类树", "（顶级分类）", "保存分类", "删除", "批量删除")
}

func TestGroupDTagsPageRenders(t *testing.T) {
	data := taxonomyTags(&productdto.TagResp{ID: "t1", Name: "新品", Slug: "new", Kind: "rule", RuleType: "new_arrival", RuleValues: productdto.RuleValuesResp{Days: 30, HasDays: true}, ProductCount: 1})
	assertGroupDPage(t, "product_tags", data, "内置规则类型", "标签", "保存标签", "重算", "删除")
}

func TestGroupDTranslationsPageRenders(t *testing.T) {
	assertGroupDPage(t, "product_translations", translationPageData(), "商品多语言", "完成度", "查看整个工程", "字段", "保存全部", "写入 sys_translation")
}
func TestGroupDDetailTemplatePageRenders(t *testing.T) {
	assertGroupDPage(t, "product_detail_template", detailTemplatePageData(true), "商品详情页模板", "当前使用", "切换模板并重新发布", "改 URL", "命名模板（", "新建命名模板")
	assertGroupDPage(t, "product_detail_template", detailTemplatePageData(false), "未发布", "使用模板", "发布详情页", "新建命名模板")
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
		"库存管理", "登记库存变动", "生产入库", "提交变动", "库存流水（最近 ")
}

func TestGroupDWarehousesPageRenders(t *testing.T) {
	data := groupDData(map[string]any{
		"Err": "", "Ok": "", "SelectedProject": "pr1", "Projects": groupDProjects(),
		"Warehouses": []map[string]any{{
			"ID": "w1", "Name": "苏州仓", "Code": "SZ", "IsDefault": false,
			"StatusLabel": "启用中", "Sort": 0, "Status": "active",
		}},
	})

	out := assertGroupDPage(t, "inventory_warehouses", data,
		"仓库管理", "苏州仓", "保存仓库", "设为默认仓", "删除", "批量删除")
	for _, want := range []string{
		`action="/admin/inventory/warehouses/bulk-delete?`,
		`name="ids"`, "data-check-all", "data-check-item", "data-bulk-bar",
		`form="wh-del-w1"`, `id="wh-del-w1"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("仓库管理页缺少 %q", want)
		}
	}
}

func TestGroupDReasonsPageRenders(t *testing.T) {
	data := groupDData(map[string]any{
		"Err": "", "Ok": "", "SelectedProject": "pr1", "Projects": groupDProjects(),
		"Directions": []map[string]any{{"Value": "in", "Label": "入库"}},
		"Reasons": []map[string]any{{
			"Code": "purchase_in", "DirectionLabel": "入库", "Name": "采购入库",
			"IsBuiltin": true, "StatusLabel": "启用",
		}},
	})
	assertGroupDPage(t, "inventory_reasons", data,
		"变动原因字典", "采购入库", "内置", "新建自定义原因")
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
		"StatusOptions":   []map[string]any{{"Value": "partial"}},
		"VariantOptions":  []map[string]any{{"VariantID": "v1", "SKUCode": "SKU1", "Label": "商品一 / 红 / M"}},
		"Orders": []map[string]any{{
			"ID": "o1", "Code": "PO-1", "Status": "partial", "StatusLabel": "部分入库",
			"SourceName": "苏州通达", "SourceTypeLabel": "外部供应商", "WarehouseName": "苏州仓",
			"ReceivedQuantity": "1", "TotalQuantity": "2", "RequestID": "req1",
			"OutstandingQuantity": 1, "ProgressPercent": 50,

			"ReceivedLabel": "已入库 1 / 2", "OutstandingLabel": "未入库 1",
			"ProgressLabel": "50%", "OutstandingTone": "warn",
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

	assertGroupDPage(t, "purchase/purchases", data,
		"采购入库", "新建采购单", "登记入库", "如 PO-20260101-001")
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
		"TypeOptions":    []map[string]any{{"Value": "external"}},
		"RelatedOptions": []map[string]any{{"Value": "false"}},
		"StatusOptions":  []map[string]any{{"Value": "active"}},
		"FilterType":     "", "FilterRelated": "", "FilterStatus": "", "FilterKeyword": "", "FilterStatusAll": "all",
		"Sources": []map[string]any{{
			"ID": "s1", "Name": "苏州通达", "Code": "SZ_SUPPLIER", "Type": "external",
			"TypeLabel": "外部供应商", "RelatedParty": false, "RelatedPartyLabel": "非关联方",
			"Status": "active", "StatusLabel": "启用中", "HasSettlePrice": true,
			"SettlePrice": "8.00", "Sort": 0, "Config": "{}",
		}},
	})

	out := assertGroupDPage(t, "purchase/sources", data,
		"货源管理", "货源总数", "新建货源", "筛选", "保存货源", "删除", "批量删除")
	for _, want := range []string{
		`hx-post="/admin/inventory/sources/bulk-delete?`,
		`name="ids"`, "data-check-all", "data-check-item", "data-bulk-bar",
		`form="src-del-s1"`, `id="src-del-s1"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("货源管理页缺少 %q", want)
		}
	}
}

func TestGroupDEnglishSwitch(t *testing.T) {
	enT := func(key, fallback string) string { return "EN[" + key + "]" }
	enScope := RenderScope{
		Lang: "en-US", Translate: enT,
		TranslateArgs:  func(k, f string, _ []string) string { return enT(k, f) },
		TranslateNamed: func(k, f string, _ map[string]any) string { return enT(k, f) },
	}

	products := productCatalogData(map[string]any{
		"SelectedProject": "pr1", "Err": "",
		"Projects":         groupDProjects(),
		"WarehouseOptions": []map[string]any{{"ID": "w1", "Name": "苏州仓", "Code": "SZ", "IsDefault": true}},
		"Products":         []*productdto.ProductResp{groupDProductRow()},
	})
	products["t"] = enT
	out, err := render(t, groupDSet(t), "admin/product/products", products, RenderScope{
		Lang: "en-US", Translate: enT,
		TranslateArgs:  func(k, f string, _ []string) string { return enT(k, f) },
		TranslateNamed: func(k, f string, _ map[string]any) string { return enT(k, f) },
	})
	if err != nil {
		t.Fatalf("products 英文渲染失败: %v", err)
	}
	for _, want := range []string{
		"EN[pTitle]", "EN[pListTitle]",
		"EN[pHintAttrs]", "EN[pCatLink]",
		"EN[pBrandLink]", "EN[pTagLink]",
		"EN[pAttrLink]", "EN[pPricingLink]",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("products 英文渲染缺少 %q", want)
		}
	}

	for _, forbidden := range []string{"商品列表", "多语言", "预览详情页", "上一操作", "返回列表"} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("products 英文渲染残留中文文案 %q（该处未走 t()）", forbidden)
		}
	}

	detail := productCatalogData(map[string]any{
		"SelectedProject": "pr1", "Err": "",
		"Projects":         groupDProjects(),
		"WarehouseOptions": []map[string]any{{"ID": "w1", "Name": "苏州仓", "Code": "SZ", "IsDefault": true}},
		"ProductID":        "p1", "HasProduct": true, "Product": groupDProductRow(),
		"BackURL": "/admin/products?project=pr1",
	})
	detail["t"] = enT
	out, err = render(t, groupDSet(t), "admin/product/product_detail", detail, enScope)
	if err != nil {
		t.Fatalf("product_detail 英文渲染失败: %v", err)
	}
	// 判据是「英文请求走的是执行期 t」而不是某个 key 的字面名：
	// 短 key 整改后 key 会改名，这里只要求取词确实发生，且没有中文残留。
	if !strings.Contains(out, "EN[") {
		t.Fatal("product_detail 英文渲染里一个执行期取词都没发生")
	}
	for _, forbidden := range []string{"保存手工标签", "各仓库存", "添加评分", "返回列表", "上一操作", "保存变体清单"} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("product_detail 英文渲染残留中文文案 %q（该处未走 t()）", forbidden)
		}
	}

	purchases := productCatalogData(map[string]any{
		"Err": "", "Ok": "", "SelectedProject": "pr1", "Projects": groupDProjects(),
		"FilterStatus": "", "FilterSource": "", "FilterKeyword": "", "HistorySKU": "",
		"ReceiptRequestID": "r1", "ProductionRequestID": "r2",
		"Sources": []map[string]any{}, "Warehouses": []map[string]any{}, "DraftLines": []map[string]any{},
		"InternalSources": []map[string]any{}, "StatusOptions": []map[string]any{},
		"VariantOptions": []map[string]any{}, "Orders": []map[string]any{}, "History": []map[string]any{},
	})
	purchases["t"] = enT
	out, err = render(t, groupDSet(t), "admin/purchase/purchases", purchases, enScope)
	if err != nil {
		t.Fatalf("inventory_purchases 空数据渲染失败: %v", err)
	}

	for _, want := range []string{
		"EN[admin.inventory_purchases.title]", "EN[admin.inventory_purchases.create.title]",
		"EN[admin.inventory_purchases.noSources.heading]", "EN[admin.inventory_purchases.filter.submit]",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("inventory_purchases 空数据渲染缺少 %q", want)
		}
	}
}
