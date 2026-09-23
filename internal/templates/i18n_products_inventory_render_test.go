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
			// 变体入口（新建变体 / 生成组合）在商品详情页，权限码与列表页时期相同：
			// 测试外壳给全集，避免每挪一个入口就要往各页数据里补一个权限码。
			"product:variant_create": true, "product:variant_generate": true,
			"product:brand_create": true, "product:category_create": true,
			"product:tag_create": true, "product:attribute_create": true,
			"product:brand_update": true, "product:category_update": true,
			"product:tag_update": true, "product:attribute_update": true,
			"product:brand_delete": true, "product:category_delete": true,
			"product:tag_delete": true, "product:attribute_delete": true,
			"inventory:warehouse_create": true, "inventory:source_create": true,
			"inventory:warehouse_update": true, "inventory:source_update": true,
			"inventory:warehouse_delete": true, "inventory:source_delete": true,
			"inventory:purchase_create": true, "inventory:reason_create": true,
			"order:coupon_create": true,
		},
		// 商品列表页的筛选与分页键（handler 总会注入；测试外壳给一份中性值）。
		// 直接渲染模板时缺键会让 Jet 在那一行**中断**（HTTP 仍 200，后半截整块消失）——
		// 本文件存在的理由就是盯住这件事，故这些键必须显式给全。
		"FilterKeyword": "", "FilterStatus": "", "Page": 1, "Limit": 20, "Total": 0,
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
		// 列表列只放值：分类列给一个分类名 + 其余分类数（+N 徽章），品牌列给品牌名，
		// 标签列给标签名（多于 3 个退化成数量），不做「手工 N · 自动 N」的来源分解。
		"CategoryCell": "男装", "CategoryOthers": 0, "TagLabel": "新品、热销",
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
	// 拆页后列表页只有商品表：变体表 / 评分表与它们的入口、四个商品级表单全部移出。
	// 完整性锚点用**稳定存在**的文案：详情入口已由名称列承担、详情页模板移出操作列，
	// 不再拿它们当判据（按钮一改就红，而那不是渲染中断）。
	out := assertGroupDPage(t, "products", data,
		"商品列表", "价格区间", "分类", "品牌", "标签", "编辑", "预览", "删除")
	// 筛选栏（关键词 + 状态 + 查询 / 重置）与分页条都在列表卡内：纯服务端渲染的 GET 表单，
	// 无 JS 也能用。控件 id 是写死的（不是自动生成的），故可以按 id 断言。
	for _, want := range []string{"products-filter-keyword", "products-filter-status", "重置"} {
		if !strings.Contains(out, want) {
			t.Fatalf("列表页缺少筛选控件 %q", want)
		}
	}
	// 标签列放的是标签名本身（数据里的名字），不是「手工 N · 自动 N」这种来源分解。
	if !strings.Contains(out, "新品、热销") {
		t.Fatalf("标签列应显示标签名，实际输出未见 %q", "新品、热销")
	}
	for _, forbidden := range []string{"手工 1 · 自动 1"} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("标签列不该显示来源分解 %q", forbidden)
		}
	}
	// 「详情」是列表页进入实体的唯一入口，链接形态写死断言（拆页的落点）。
	if !strings.Contains(out, `href="/admin/products/detail?project=pr1&amp;product=p1"`) {
		t.Fatalf("列表页缺少「详情」链接")
	}
	// 「编辑」是商品基本字段与多语言的入口（多语言不再单独占操作列一格）。
	if !strings.Contains(out, `href="/admin/products/edit?project=pr1&amp;product=p1"`) {
		t.Fatalf("列表页缺少「编辑」链接")
	}
	// range 体内取词（{{ .["t"] }} 在 range 内仍指向根数据）确实生效：
	// 「编辑」「预览」「删除」都来自 range 体内，缺一则说明该写法在真实模板里失效。
	for _, want := range []string{"编辑", "预览", "删除"} {
		if !strings.Contains(out, want) {
			t.Fatalf("range 体内取词失效，缺少 %q", want)
		}
	}
	// URL 段不再作为列表列（要看它去详情页）：表头与取值都不该出现在列表页。
	// 注意「URL 段」四个字本身仍会出现 —— 新建抽屉的 slug 输入框占位符就是它，
	// 所以判据钉在**表头与单元格**上，而不是整个页面的文本。
	// 多语言同理：「多语言」出现在页头悬浮说明里是正常的，判据是**操作列不再有它的入口**。
	for _, forbidden := range []string{"<th>URL 段</th>", "/p-one", "/admin/products/translations?project=pr1&amp;product=p1"} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("列表页不该再出现 %q（URL 段列已删除、多语言入口已移入编辑页）", forbidden)
		}
	}
	// 这些属于商品详情（变体 / 评分是某个商品的子资源），不该出现在列表页。
	for _, forbidden := range []string{"新建变体", "生成组合", "添加评分", "保存手工标签", "SEO 评分", "各仓库存"} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("列表页不该再渲染 %q（已移到商品详情页）", forbidden)
		}
	}
}

// TestGroupDProductEditPageRenders 商品编辑页：**商品域唯一的编辑界面**。
//
// 详情页只读之后，写动作全部收在这一页：基本字段（名称 / URL 段 / SKU / 状态 / 价格 /
// 单位 / 重量 / SEO / 图集）、属性引用、分类与品牌、手工标签、变体清单、评分、SEO 检查、
// 详情页模板的双轨动作。表单字段名与 UpdateReq 的 json 标签对齐；
// 尾部标记取页面末尾的区块与按钮：存在即说明整页没有被中途截断（Jet 缺键只中断、HTTP 仍 200）。
func TestGroupDProductEditPageRenders(t *testing.T) {
	row := groupDProductRow()
	row["Subtitle"] = "副标题"
	row["Unit"] = "件"
	row["SEOTitle"] = "SEO 标题"
	row["SEODescription"] = "SEO 描述"
	data := groupDData(map[string]any{
		"SelectedProject": "pr1", "Err": "",
		"Projects": groupDProjects(),
		// 变体的两个抽屉要仓库下拉（handler 总会注入）。
		"WarehouseOptions": []map[string]any{{"ID": "w1", "Label": "苏州仓"}},
		"ProductID":        "p1", "HasProduct": true, "Product": row,
		"BackURL": "/admin/products?project=pr1",
		// 属性组勾选态（服务端算好；模板只渲染）。
		"AttributeChecks": []map[string]any{{"ID": "a1", "Label": "颜色（color）", "IsVariation": true, "Checked": true}},
		"ImagesText":      "https://cdn.example.com/1.jpg",
		"ImageAltsText":   "图一",
		"WeightText":      "0.5", "DefaultPriceText": "10.00",
	})
	out := assertGroupDPage(t, "product_edit", data,
		"基本信息", "属性引用", "分类与品牌", "标签", "保存",
		// 从详情页搬来的三块：变体 / 评分 / SEO 检查（详情页只读，编辑能力必须在这里）。
		"变体", "保存变体清单", "生成组合", "添加评分", "SEO 评分")
	// 表单协议：action 与字段名与 UpdateReq 的 json 标签逐字对齐（改字段名等于改协议）。
	for _, want := range []string{
		`action="/admin/products/update"`,
		`name="name"`, `name="subtitle"`, `name="slug"`, `name="sku"`, `name="status"`,
		`name="defaultPrice"`, `name="unit"`, `name="weight"`,
		`name="seoTitle"`, `name="seoDescription"`,
		`name="images"`, `name="imageAlts"`,
		`name="attributeIds"`, `name="categoryIds"`, `name="primaryCategoryId"`,
		`name="brandId"`, `name="tagIds"`,
		// 多语言入口在本页（列表操作列不再单占一格）。
		"/admin/products/translations?project=pr1&amp;product=p1",
		// 详情入口也在：改完去看只读的构成（变体 / 评分 / 捆绑）。
		"/admin/products/detail?project=pr1&amp;product=p1",
		// 变体 / 评分的写协议与搬页前逐字相同（改版式不改协议）。
		`action="/admin/products/variant/save"`,
		`data-preview-url="/admin/products/variant/preview"`,
		`action="/admin/products/variant/create"`,
		`action="/admin/products/rating/add"`,
		`action="/admin/products/rating/delete"`,
		`action="/admin/products/seo-score"`, `id="product-seo-score-p1"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("商品编辑页缺少 %q", want)
		}
	}
	// 空态分支（商品不存在 / 链接里的商品在另一个工程）也要整页渲染完，不中断。
	// WarehouseOptions 是 handler 无条件注入的键（页面顶部先取仓库清单，取不到走 PageError），
	// 测试外壳按同一形状给全 —— 缺它会在模板第一行就中断，看到的却是「空态没渲染出来」。
	empty := groupDData(map[string]any{
		"SelectedProject": "pr1", "Err": "",
		"Projects": groupDProjects(), "ProductID": "p1", "HasProduct": false,
		"WarehouseOptions": []map[string]any{},
		"BackURL":          "/admin/products?project=pr1",
	})
	emptyOut := assertGroupDPage(t, "product_edit", empty, "商品不存在", "返回列表")
	if strings.Contains(emptyOut, `action="/admin/products/update"`) {
		t.Fatalf("商品不存在时不该渲染保存表单")
	}
}

// TestGroupDProductDetailPageRenders 商品详情页：**只读**。
//
// 判据分两半：① 三块构成（变体 / 评分 / 捆绑构成）与只读事实（基本信息 / 标签）都在；
// ② **任何写表单都不在** —— 它们全在编辑页。第二半是这次改造的核心：
// 只读页里残留一个「保存手工标签」，用户就会以为改得动。
func TestGroupDProductDetailPageRenders(t *testing.T) {
	data := groupDData(map[string]any{
		"SelectedProject": "pr1", "Err": "",
		"Projects":         groupDProjects(),
		"WarehouseOptions": []map[string]any{{"ID": "w1", "Label": "苏州仓"}},
		"ProductID":        "p1", "HasProduct": true, "Product": groupDProductRow(),
		"BackURL": "/admin/products?project=pr1",
	})
	out := assertGroupDPage(t, "product_detail", data,
		"基本信息", "属性引用", "标签", "变体", "评分", "各仓库存",
		"自动标签（按规则重算维护，不能手工改动）")
	// 唯一的 POST 是「预览」：渲染前台效果、不写库。
	if !strings.Contains(out, `action="/admin/products/template/preview"`) {
		t.Fatalf("详情页缺少「预览」表单（只读渲染的唯一 POST）")
	}
	// 写表单一个都不该出现：它们全在编辑页（admin/product_edit.html）。
	for _, forbidden := range []string{
		`action="/admin/products/attributes"`,
		`action="/admin/products/taxonomy"`,
		`action="/admin/products/tags"`,
		`action="/admin/products/seo-score"`,
		`action="/admin/products/variant/save"`,
		`action="/admin/products/variant/create"`,
		`action="/admin/products/rating/add"`,
		`action="/admin/products/rating/delete"`,
		`action="/admin/products/reapply-preset"`,
		`action="/admin/products/rollback-document"`,
		`action="/admin/products/delete"`,
		"保存属性引用", "保存分类与品牌", "保存手工标签", "保存变体清单",
		"新建变体", "生成组合", "添加评分", "SEO 评分",
	} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("详情页是只读页，不该出现写入口 %q", forbidden)
		}
	}
	// 空态分支（商品不存在）走的另一条路：渲染 .empty-state + 返回列表，不整页中断。
	empty := groupDData(map[string]any{
		"SelectedProject": "pr1", "Err": "",
		"Projects": groupDProjects(), "WarehouseOptions": []map[string]any{},
		"ProductID": "missing", "HasProduct": false, "Product": map[string]any{},
		"BackURL": "/admin/products?project=pr1",
	})
	assertGroupDPage(t, "product_detail", empty, "商品不存在", "返回列表")
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
	// 三个抽屉的表单实例数据：片段是**带参数 include** 的（数据由调用点给，取不到页面 data，
	// 连取词函数 t 与 csrf 都要显式传），形状与 handle 的 attrRowDrawerForms /
	// attrCreateDrawerForm 逐键一致 —— 缺键不再是「少渲一块」，而是整块渲染失败。
	tr := TranslateFunc("zh-CN")
	attrRows := func(id string) map[string]any {
		return map[string]any{
			"GroupID": id,
			"Rows":    []map[string]any{{"ID": "av1", "Key": "red", "Label": "红", "Sort": 0, "Enabled": true}},
		}
	}
	data := groupDData(map[string]any{
		"Err": "", "SelectedProject": "pr1", "Projects": groupDProjects(),
		"Attributes": []map[string]any{{
			"ID": "a1", "Name": "颜色", "Key": "color", "IsVariation": true, "ValueCount": 2, "Sort": 0,
			"EditForm": map[string]any{
				"Csrf": "tok", "Project": "pr1", "t": tr,
				"Mode": "edit", "Action": "/admin/product-attributes/update", "IsCreate": false,
				"GroupID": "a1", "Name": "颜色", "Key": "color", "Sort": 0, "VariationChecked": true,
				"RowsCtx": attrRows("a1"), "InDrawer": true,
			},
			"ValuesForm": map[string]any{
				"Csrf": "tok", "Project": "pr1", "t": tr,
				"Action": "/admin/product-attributes/set-values", "GroupID": "a1",
				"RowsCtx": attrRows("a1"), "InDrawer": true,
			},
		}},
		"AttrCreateForm": map[string]any{
			"Csrf": "tok", "Project": "pr1", "t": tr,
			"Mode": "create", "Action": "/admin/product-attributes/create", "IsCreate": true,
			"GroupID": "new", "Name": "", "Key": "", "Sort": 0, "VariationChecked": true,
			"RowsCtx": map[string]any{"GroupID": "new", "Rows": []map[string]any{}}, "InDrawer": true,
		},
	})
	// 行内按钮不再重复实体名（admin-ui-logic §3）：列表已是表格，行已指明是谁。
	assertGroupDPage(t, "product_attributes", data,
		"参与变体", "属性组", "保存属性组", "保存属性值", "删除")
}

func TestGroupDBrandsPageRenders(t *testing.T) {
	data := groupDData(map[string]any{
		"Err": "", "SelectedProject": "pr1", "Projects": groupDProjects(),
		"Brands": []map[string]any{{
			"ID": "b1", "Name": "山野", "Slug": "shanye", "Sort": 0, "Logo": "",
			"SEOTitle": "", "SEODescription": "", "Description": "",
		}},
	})
	// 行内按钮不再重复实体名（admin-ui-logic §3）：列表里是表格行，行已指明是谁，按钮写「删除」。
	assertGroupDPage(t, "product_brands", data, "商品品牌", "品牌列表", "保存品牌", "SEO 评分", "删除")
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
	// 行内按钮不再重复实体名（admin-ui-logic §3）；列表现已是标准表格 + 首列勾选 + 批量动作。
	assertGroupDPage(t, "product_categories", data, "分类树", "（顶级分类）", "保存分类", "删除", "批量删除")
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
	// 同上：标签列表转表格后，行内按钮是「重算」与「删除」（不重复实体名）。
	assertGroupDPage(t, "product_tags", data,
		"内置规则类型", "标签", "保存标签", "重算", "删除")
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
	// 本页现在只做「看流水 + 改库存」：仓库管理 / 变动原因字典各自独立成页，
	// 两个写操作（登记变动 / 生产入库）走右侧抽屉 —— 断言随之改成这一版页面里
	// 真正存在的尾部标记（旧断言的 "保存仓库" 已随仓库管理页迁走）。
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
	// 列表改成标准表格 + 右侧操作列后，行内按钮按操作列惯例收短（「删除」而不是「删除仓库」
	// —— 行本身已经指明是谁，按钮再重复一遍实体名是噪音）。
	// 批量协议写死断言：勾选框就是批量表单的字段（name="ids"），提交目标只有本页的批量端点；
	// 行内表单已挪到表格外（form 不能嵌套），按钮靠 form="<id>" 关联。
	out := assertGroupDPage(t, "inventory_warehouses", data,
		"仓库管理", "苏州仓", "保存仓库", "设为默认仓", "删除", "批量删除")
	for _, want := range []string{
		`action="/admin/inventory/warehouses/bulk-delete"`,
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
	// 尾部锚点选抽屉模板里的字段（页面最后一屏）——它出现即说明整页（含订单展开行与收货表单）
	// 没有被中途截断。本页只剩「采购单 / 对单收货」两件事（进货历史就是库存页的流水筛选）。
	//
	// 2026-09 第四轮评审又收了一次：**入库 / 出库 / 调整不再由库存管理页直接录**，必须来自单据
	//（采购入库走本页、销售出库走发货、退货入库走退货单、盘盈亏走盘点单）；库存管理页退化为只读视图
	// + 一个受权限约束的「库存调整（盘点/报损）」入口。此前那句「生产入库移到库存管理页」已过时：
	// 该页面入口已随本轮下线（API 保留），断言随之收窄。
	// 行内 label 已经写明「采购单号」，placeholder 不再重复前缀；列表改标准表格 + 操作列
	// 后「登记入库」进操作列抽屉。断言跟随实现。
	assertGroupDPage(t, "inventory_purchases", data,
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
	// 筛选区文案按评审判据收短（"筛选（报表区分维度）" → "筛选"，口径说明进 .help）；
	// 列表改标准表格 + 右侧操作列后，行内按钮按操作列惯例收短（"删除货源" → "删除"）。
	// 批量协议同仓库管理页：勾选框即批量表单字段，行内删除表单在表格外。
	out := assertGroupDPage(t, "inventory_sources", data,
		"货源管理", "货源总数", "新建货源", "筛选", "保存货源", "删除", "批量删除")
	for _, want := range []string{
		`action="/admin/inventory/sources/bulk-delete"`,
		`name="ids"`, "data-check-all", "data-check-item", "data-bulk-bar",
		`form="src-del-s1"`, `id="src-del-s1"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("货源管理页缺少 %q", want)
		}
	}
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
	out, err := render(t, groupDSet(t), "admin/product/products", products)
	if err != nil {
		t.Fatalf("products 英文渲染失败: %v", err)
	}
	for _, want := range []string{
		"EN[admin.products.title]", "EN[admin.products.row.edit]",
		"EN[admin.products.row.preview]",
		"EN[admin.products.col.categories]",
		"EN[admin.products.col.brand]", "EN[admin.products.col.priceRange]",
		"EN[admin.products.hint.attrLead]", "EN[admin.products.rating.label]",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("products 英文渲染缺少 %q", want)
		}
	}
	// 页面级文案不应再有中文残留（数据里的中文如「商品一」仍会出现，故只查固定文案）。
	for _, forbidden := range []string{"商品列表", "多语言", "预览详情页", "上一操作", "返回列表"} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("products 英文渲染残留中文文案 %q（该处未走 t()）", forbidden)
		}
	}

	// 拆出去的详情页同样整体切英文：页头动作、三个区块与三个抽屉的文案都要走 t()。
	detail := groupDData(map[string]any{
		"SelectedProject": "pr1", "Err": "",
		"Projects":         groupDProjects(),
		"WarehouseOptions": []map[string]any{{"ID": "w1", "Label": "苏州仓"}},
		"ProductID":        "p1", "HasProduct": true, "Product": groupDProductRow(),
		"BackURL": "/admin/products?project=pr1",
	})
	detail["t"] = enT
	out, err = render(t, groupDSet(t), "admin/product/product_detail", detail)
	if err != nil {
		t.Fatalf("product_detail 英文渲染失败: %v", err)
	}
	for _, want := range []string{
		"EN[admin.product_detail.basic.title]", "EN[admin.product_detail.help.label]",
		"EN[admin.product_detail.back]", "EN[admin.products.variant.stockLink]",
		// 详情页只读：留在页面上的取词是只读区块的标题（写入口的 key 已随表单搬到编辑页）。
		"EN[admin.products.tags.title]", "EN[admin.products.variants.title]",
		"EN[admin.products.rating.label]",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("product_detail 英文渲染缺少 %q", want)
		}
	}
	for _, forbidden := range []string{"保存手工标签", "各仓库存", "添加评分", "返回列表", "上一操作", "保存变体清单"} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("product_detail 英文渲染残留中文文案 %q（该处未走 t()）", forbidden)
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
	out, err = render(t, groupDSet(t), "admin/inventory/inventory_purchases", purchases)
	if err != nil {
		t.Fatalf("inventory_purchases 空数据渲染失败: %v", err)
	}
	// 空货源 + 空采购单：页头标题、新建入口、筛选按钮与空状态都要走 t()
	// （生产入库 / 进货历史两块已不在本页，见 TestGroupDPurchasesPageRenders 的说明）。
	for _, want := range []string{
		"EN[admin.inventory_purchases.title]", "EN[admin.inventory_purchases.create.title]",
		"EN[admin.inventory_purchases.noSources.heading]", "EN[admin.inventory_purchases.filter.submit]",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("inventory_purchases 空数据渲染缺少 %q", want)
		}
	}
}
