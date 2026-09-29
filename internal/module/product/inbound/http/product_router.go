// product_router.go — product 模块路由自装配（issue #5 / T3a）。
// 挂 authorizedAPI 三层链（SessionAuth + CSRF + Casbin）。
package producthttp

import (
	"gorm.io/gorm"

	productcontract "go_wp/internal/module/product/contract"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/permission"
	"go_wp/pkg/i18n"
)

// SetupProductRoutes 装配 product 模块路由，返回模块契约。
// project 用于解析商品所属工程（products.project_id 为 NOT NULL 外键）。
//
// 装配同时注入内容译文读取端口（sys_translation）：构建期商品可翻译字段
// （name/subtitle/description）按构建语言取译文，语境 product.<字段名>。
// 端口在这里注入是因为本模块的 service 不持有 *gorm.DB（表隔离约定）。
//
// issue #32：归属仓解析与库存记录生成不再经跨模块端口 —— 商品与库存同属一个模块，
// 装配时把库存 service 直接交给商品用例（传 nil 表示不生成库存记录，纯商品单测路径）。
func SetupProductRoutes(rg *permission.RouteGroup, db *gorm.DB, project projectcontract.ProjectService) productcontract.ProductService {
	svc := productservice.NewService(productmodel.NewModel(db), project)
	svc.SetContentStore(i18n.NewDBContentStore(db))
	handle := NewHandle(svc)

	g := rg.Group("/product")
	// 取词注入：service 层的展示文案（内置规则名 / 描述等）按请求语言渲染。
	g.Use(productTranslateMiddleware())
	g.GET("/list", permission.ProductList, handle.List)
	g.GET("/get", permission.ProductGet, handle.Get)
	g.POST("/create", permission.ProductCreate, handle.Create)
	g.POST("/update", permission.ProductUpdate, handle.Update)
	g.POST("/delete", permission.ProductDelete, handle.Delete)
	g.POST("/variant/create", permission.ProductVariantCreate, handle.CreateVariant)
	// 组合生成（issue #8）：勾选属性值 → 笛卡尔积写变体（幂等，超上限整体拒绝）。
	g.POST("/variant/generate", permission.ProductVariantGenerate, handle.GenerateVariants)
	g.POST("/variant/update", permission.ProductVariantUpdate, handle.UpdateVariant)
	g.POST("/variant/delete", permission.ProductVariantDelete, handle.DeleteVariant)

	// 属性组与属性值（issue #7）：属性组可跨商品复用，故独立于商品资源。
	// 属性值用 set-values 整体保存（全量替换），比逐行接口少一半请求、也避免半截状态。
	g.GET("/attribute/list", permission.ProductAttributeList, handle.ListAttributes)
	g.GET("/attribute/get", permission.ProductAttributeGet, handle.GetAttribute)
	g.POST("/attribute/create", permission.ProductAttributeCreate, handle.CreateAttribute)
	g.POST("/attribute/update", permission.ProductAttributeUpdate, handle.UpdateAttribute)
	g.POST("/attribute/set-values", permission.ProductAttributeSetValues, handle.SetAttributeValues)
	g.POST("/attribute/delete", permission.ProductAttributeDelete, handle.DeleteAttribute)

	// 分类与品牌（issue #10）：分类是树（list 返回树、get 取单节点），品牌是平铺列表。
	// 分类与品牌都可跨商品复用，故与商品资源并列而不是嵌在商品路径下。
	g.GET("/category/list", permission.ProductCategoryList, handle.ListCategories)
	g.GET("/category/get", permission.ProductCategoryGet, handle.GetCategory)
	g.POST("/category/create", permission.ProductCategoryCreate, handle.CreateCategory)
	g.POST("/category/update", permission.ProductCategoryUpdate, handle.UpdateCategory)
	g.POST("/category/delete", permission.ProductCategoryDelete, handle.DeleteCategory)
	g.GET("/brand/list", permission.ProductBrandList, handle.ListBrands)
	g.GET("/brand/get", permission.ProductBrandGet, handle.GetBrand)
	g.POST("/brand/create", permission.ProductBrandCreate, handle.CreateBrand)
	g.POST("/brand/update", permission.ProductBrandUpdate, handle.UpdateBrand)
	g.POST("/brand/delete", permission.ProductBrandDelete, handle.DeleteBrand)

	// 标签与自动规则（issue #11）：手工标签手工挂载，自动标签按内置规则类型 + 参数维护，
	// 归属由明确的重算时机更新；products 是「某标签命中哪些商品」（后台核对）。
	// 标签可跨商品复用，故与商品资源并列而不是嵌在商品路径下。
	g.GET("/tag/list", permission.ProductTagList, handle.ListTags)
	g.GET("/tag/get", permission.ProductTagGet, handle.GetTag)
	g.GET("/tag/products", permission.ProductTagProducts, handle.ListTagProducts)
	g.GET("/tag/rule-types", permission.ProductTagRuleTypes, handle.ListTagRuleTypes)
	g.POST("/tag/create", permission.ProductTagCreate, handle.CreateTag)
	g.POST("/tag/update", permission.ProductTagUpdate, handle.UpdateTag)
	g.POST("/tag/delete", permission.ProductTagDelete, handle.DeleteTag)
	g.POST("/tag/recalc", permission.ProductTagRecalc, handle.RecalcTags)

	// 捆绑品（issue #20）：选项规则在 products.bundle_items，选项来自跨商品挑选的已存在 SKU。
	// validate 是后端硬校验（与前台片段共用同一份校验逻辑），skus 是配置器的数据源。
	g.GET("/bundle/get", permission.ProductBundleGet, handle.GetBundleConfig)
	g.POST("/bundle/set", permission.ProductBundleSet, handle.SetBundleConfig)
	g.POST("/bundle/validate", permission.ProductBundleValidate, handle.ValidateBundleSelection)
	g.GET("/bundle/skus", permission.ProductBundleSkus, handle.ListBundleSKUs)

	// 定价工具（issue #13）：四种内置规则 + 尾数处理，可对单个 SKU / 单商品全部变体 /
	// 筛选集批量应用。preview 与 apply 共用同一份规则入参（预览不落库，应用落库 + 留痕）。
	// 结果写回 product_variants.price，不参与构建期计算。
	g.GET("/pricing/rules", permission.ProductPricingRules, handle.ListPricingRuleTypes)
	g.GET("/pricing/roundings", permission.ProductPricingRoundings, handle.ListPricingRoundingOptions)
	g.POST("/pricing/preview", permission.ProductPricingPreview, handle.PreviewPricing)
	g.POST("/pricing/apply", permission.ProductPricingApply, handle.ApplyPricing)
	g.GET("/pricing/history", permission.ProductPricingHistory, handle.ListPriceAdjustments)
	g.GET("/pricing/adjustment", permission.ProductPricingAdjustment, handle.GetPriceAdjustment)
	return svc
}
