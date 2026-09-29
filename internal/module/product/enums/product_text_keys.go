package productenums

// product_text_keys.go — 后台界面**静态文案**的 i18n key（点分新式常量）。
//
// 与 product_enums.go 里那批 `MsgXxx = "MsgXxx"` / `ErrXxx = "ErrXxx"` 的老式常量**不同批**：
// 老式常量的值就是常量名本身（sys_i18n 里存同名 key），本文件的常量值一律是 `admin.*` 点分 key，
// 与词条表的 item_key **逐字相等**（词条见迁移 449_i18n_product_inventory_go_texts）。
//
// 为什么收在这里：调用点原先直接写 key 字面量（`tr("admin.products.status.draft", "草稿")`），
// 「谁在用这个 key」「改词条要动哪几处」都只能靠字符串检索，改 key 时容易漏。收到 enums 之后
// key 与常量名一一对应；**中文兜底仍留在调用点原地**（词条缺失时的回落，不搬进 enums）。
//
// 模板（.jet / .html）不在此列：Jet 里引用不到 Go 常量，那里的 key 必然内联。
//
// 命名：key 去掉 `admin.` 前缀后的语义路径转驼峰，缩写 SEO / URL / SKU / ID 全大写。
// **常量值必须逐字等于原字面量** —— 改错一个字符就等于换了 key，词条取不到、页面回落中文。
const (
	// —— 商品列表与商品详情 ——
	ProductsStatusAll               = "admin.products.status.all"
	ProductsStatusDraft             = "admin.products.status.draft"
	ProductsStatusPublished         = "admin.products.status.published"
	ProductsStatusArchived          = "admin.products.status.archived"
	ProductsListTagCount            = "admin.products.list.tagCount"
	ProductsCreateSubmit            = "admin.products.create.submit"
	ProductsOptionNoPrimaryCategory = "admin.products.option.noPrimaryCategory"
	ProductsOptionNoBrand           = "admin.products.option.noBrand"
	ProductDetailTitle              = "admin.product_detail.title"

	// —— 多语言译文（entityType / field 是译文域的字段白名单，与模板同一份对应关系）——
	ProductTranslationsTitle                    = "admin.product_translations.title"
	ProductTranslationsSavedNote                = "admin.product_translations.savedNote"
	ProductTranslationsSavedNone                = "admin.product_translations.savedNone"
	ProductTranslationsEntityTypeCategory       = "admin.product_translations.entityType.category"
	ProductTranslationsEntityTypeBrand          = "admin.product_translations.entityType.brand"
	ProductTranslationsEntityTypeTag            = "admin.product_translations.entityType.tag"
	ProductTranslationsEntityTypeAttribute      = "admin.product_translations.entityType.attribute"
	ProductTranslationsEntityTypeProduct        = "admin.product_translations.entityType.product"
	ProductTranslationsFieldProductName         = "admin.product_translations.field.productName"
	ProductTranslationsFieldProductSubtitle     = "admin.product_translations.field.productSubtitle"
	ProductTranslationsFieldProductDescription  = "admin.product_translations.field.productDescription"
	ProductTranslationsFieldProductImageAlts    = "admin.product_translations.field.productImageAlts"
	ProductTranslationsFieldCategoryName        = "admin.product_translations.field.categoryName"
	ProductTranslationsFieldCategoryDescription = "admin.product_translations.field.categoryDescription"
	ProductTranslationsFieldCategorySeoTitle    = "admin.product_translations.field.categorySeoTitle"
	ProductTranslationsFieldBrandName           = "admin.product_translations.field.brandName"
	ProductTranslationsFieldBrandDescription    = "admin.product_translations.field.brandDescription"
	ProductTranslationsFieldBrandSeoTitle       = "admin.product_translations.field.brandSeoTitle"
	ProductTranslationsFieldTagName             = "admin.product_translations.field.tagName"
	ProductTranslationsFieldAttributeName       = "admin.product_translations.field.attributeName"
	ProductTranslationsFieldAttributeValues     = "admin.product_translations.field.attributeValues"

	// —— 标签 ——
	ProductTagsTitle      = "admin.product_tags.title"
	ProductTagsKindRule   = "admin.product_tags.kind.rule"
	ProductTagsKindManual = "admin.product_tags.kind.manual"
	ProductTagsRecalcNone = "admin.product_tags.recalcNone"

	// —— 分类 / 品牌 / 属性 / 详情页模板 / 商品编辑 ——
	ProductCategoriesTitle             = "admin.product_categories.title"
	ProductCategoriesChildrenPageLabel = "admin.product_categories.children.pageLabel"
	ProductBrandsTitle                 = "admin.product_brands.title"
	ProductAttributesTitle             = "admin.product_attributes.title"
	ProductDetailTemplateTitle         = "admin.product_detail_template.title"
	ProductEditErrDefaultPriceInvalid  = "admin.product_edit.err.defaultPriceInvalid"
	ProductEditErrWeightInvalid        = "admin.product_edit.err.weightInvalid"

	// —— 捆绑配置 ——
	ProductBundleTitle                   = "admin.product_bundle.title"
	ProductBundleOptionNone              = "admin.product_bundle.option.none"
	ProductBundleSourceProduct           = "admin.product_bundle.source.product"
	ProductBundleSourceWarehouse         = "admin.product_bundle.source.warehouse"
	ProductBundleSourceAttributes        = "admin.product_bundle.source.attributes"
	ProductBundleSourceNone              = "admin.product_bundle.source.none"
	ProductBundleSourceWarehouseSKULabel = "admin.product_bundle.source.warehouseSkuLabel"
	ProductBundleSourceExternalSKULabel  = "admin.product_bundle.source.externalSkuLabel"

	// —— 定价工具 ——
	ProductPricingTitle            = "admin.product_pricing.title"
	ProductPricingLineChanged      = "admin.product_pricing.line.changed"
	ProductPricingLineUnchanged    = "admin.product_pricing.line.unchanged"
	ProductPricingLineSkipped      = "admin.product_pricing.line.skipped"
	ProductPricingSkipCostMissing  = "admin.product_pricing.skip.costMissing"
	ProductPricingSkipOutOfRange   = "admin.product_pricing.skip.outOfRange"
	ProductPricingFilterStatus     = "admin.product_pricing.filter.status"
	ProductPricingFilterKeyword    = "admin.product_pricing.filter.keyword"
	ProductPricingFilterCategoryID = "admin.product_pricing.filter.categoryId"
	ProductPricingFilterBrandID    = "admin.product_pricing.filter.brandId"
	ProductPricingFilterTagID      = "admin.product_pricing.filter.tagId"

	// —— SEO 评分 ——
	//
	// key 落在 `admin.seo.*` 这个共享命名空间（空态 / 问题行 / SERP 行的兜底文案在本文件）。
	// **等级文案（颜色 → 文案）的 key 与中文兜底不在此处**：它是 content / project / product
	// 三个模块共用的唯一映射，见 seoscore.ScoreGradeLabels（internal/seo/score_grade.go）；
	// product 侧调用点在 product_seo_score_page.go 的 entityScoreView。
	SEOEmptyMissingProduct     = "admin.seo.empty.missingProduct"
	SEOEmptyProductUnreadable  = "admin.seo.empty.productUnreadable"
	SEOEmptyMissingCategory    = "admin.seo.empty.missingCategory"
	SEOEmptyCategoryUnreadable = "admin.seo.empty.categoryUnreadable"
	SEOEmptyMissingBrand       = "admin.seo.empty.missingBrand"
	SEOEmptyBrandUnreadable    = "admin.seo.empty.brandUnreadable"
	SEOIssueLine               = "admin.seo.issue.line"
	SEOSerpTitleEmpty          = "admin.seo.serp.titleEmpty"
	SEOSerpURLEmpty            = "admin.seo.serp.urlEmpty"
	SEOSerpDescEmpty           = "admin.seo.serp.descEmpty"
	SEOIndexArticle            = "admin.seo.index.article"
)

// —— 跨模块引用的词条常量，**不在本文件定义** ——
//
// 商品列表页与库存页共用「· 默认仓」后缀：key 落在库存域（`admin.inventory.*`），且**词条与常量
// 都只有一条**（`inventoryenums.InventoryChangeWarehouseDefaultSuffix`，词条见迁移 191）——
// 商品侧直接 import 那个常量（先例见 product/service/product_crud.go）。
// 本模块曾自留一份同 key 常量：同一个 key 在两处定义，改一处另一处静默不动。
