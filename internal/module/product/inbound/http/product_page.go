// Package producthttp product 模块 HTTP 入口（issue #5 / T3a）。
package producthttp

// product_page.go — 商品域控制器的**模块入口**：两个处理器类型与它们的构造，外加文件索引。
//
// 本文件只放各控制器共用的处理器类型与构造：页面面是 productPageHandle（后台页面的 handler
// 都是它的方法），接口面是 Handle（`product_router.go` 里 `handle.Xxx` 的接收者）——
// 于是「商品的写动作一共有哪些入口」在类型定义上一眼看完。**具体功能分在下面这些文件里**：
// 控制器可以有多个，路由只有一份（`product_router.go`）。
//
//	product_core.go               商品本体：接口面 CRUD / 变体 + 列表 / 新建 / 编辑 / 详情页 / 批量
//	product_attributes.go         属性库（属性组 / 属性值 + 三个抽屉表单）+ 属性接口面
//	product_taxonomy.go           三套归类：分类树 / 品牌 / 标签（后台页面 + 接口面）
//	product_bundle.go             捆绑商品（配置页 + 成员解析 + 详情页面板）+ 捆绑接口面
//	product_pricing.go            定价工具（预览 → 应用 → 台账）+ 定价接口面
//	product_detail_template.go    详情页模板（双轨写入 / 回滚 / 预览）+ SEO 评分
//	product_translations.go       商品译文工作台
//	product_page_util.go          共用装配原语：formEcho 一族、下拉选项形状、分页与取值小工具
//	product_jump.go               共享出口原语：回跳地址 / 提示页 / 回执文案 / 错误事实与文案出口
//
// 两个处理器各自只依赖 product / project 两个契约，以及自己那几个窄端口
//（SetInventoryDeps / SetSeoTitleSources / SetDetailTemplate* 注入）：页面面不复用接口面的
// svc 字段，于是「只做后台页面」的那一屏不会被迫认识接口面的依赖。
//
// 整页 GET 的**取数编排**不在这里：一屏一个读模型方法在 service（`product_page.go` 的
// ProductNewPage 等），页面数据形状在 dto，控制器只做「读参数 → 调读模型 → 渲染」。
// 下面两个只读端口是本文件与它们的接缝：SetInventoryDeps / SetDetailTemplateDeps 收到
// 依赖后**同时**交给处理器自己（编辑页 / 详情页 / 捆绑页这些还没下沉的老路径）与
// service 的读模型，见 productPagePortWiring。

import (
	contentcontract "go_wp/internal/module/content/contract"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	inventorycontract "go_wp/internal/module/inventory/contract"
	pagecontract "go_wp/internal/module/page/contract"
	productcontract "go_wp/internal/module/product/contract"
	inventoryoutbound "go_wp/internal/module/product/outbound/inventory"
	projectcontract "go_wp/internal/module/project/contract"
)

// variantSelectionPrefix 组合生成表单里「属性组 → 勾选值」的字段名前缀。

// 字段名形如 attr:<属性组 id>（多选 checkbox），一个属性组一组值；
// 提交时按前缀收拢成 service 的 selections。
const variantSelectionPrefix = "attr:"

// productPageHandle 商品后台页处理器。
type productPageHandle struct {
	products productcontract.ProductService
	projects projectcontract.ProjectService
	// templates / instances 供「详情页模板」页（issue #14）消费：模板列表与版本、
	// 商品发布实例的模板绑定与预览渲染。经 SetDetailTemplateDeps 注入 ——
	// 未注入时该页给出装配提示，不影响商品列表页与既有测试的构造签名。
	templates contenttemplatecontract.ContentTemplateService
	instances ProductDetailTemplatePort
	// modePort 双轨能力（迁移 282）：独立文档 / 重新套用预设 / 回滚 / 影响面计数。
	// 单独字段而不是并入 instances：这六条动作属于另一条收窄端口
	//（presentationcontract.DetailTemplateModePort），装配期直接注入（实参
	// presentationSvc 已嵌入该端口，无需断言）；未注入时降级为只有基础面板。
	modePort ProductDetailTemplateModePort
	// inventories → 已随「整页 GET 取数下沉」拆成两条**页面只读端口**（见下面两个字段）：
	// 页面处理器不再直接持有库存 / 内容模板的读写能力，只拿「这一屏要显示哪些选项」。
	//
	// warehouses / entityTemplates 是**还没走读模型的老路径**（编辑页、详情页、捆绑配置页
	// 的抽屉）用的端口：那几屏的取数尚未下沉，这里先共用同一份口径（标签怎么拼、候选取
	// 多少条都在 outbound 适配器里唯一一份），避免页面自己再拼一遍。
	warehouses      productcontract.WarehouseOptionsPort
	entityTemplates productcontract.EntityTemplatePort
	// seoPages / seoContents 编辑期 title 唯一性检查的另外两个数据源（审计 SEO-018）：
	// 页面草稿的 SEO 标题与文章标题都算「同站点已存在的内容」，只比商品域会漏掉
	// 跨内容的重复。经 SetSeoTitleSources 注入 —— 可空，未注入时索引退化为商品域。
	seoPages    pagecontract.PageService
	seoContents contentcontract.ContentService
}

// productPagePortWiring 装配期把两条页面只读端口交到**本模块 service 读模型**的接线面。
//
// 为什么由页面处理器转交、而不是装配段直接调 service 的 Set*：这两条依赖的到达点本来就
// 是 SetInventoryDeps / SetDetailTemplateDeps（既有装配与既有页面测试都只调它们），
// 再加一条平行的「装配段直接注入 service」就是同一份接线两处写 —— 漏一处时表现是
// 「新页看不到仓库下拉 / 模板候选」，不报错、门禁也不红。
//
// 不放进 contract：它不是「能力」，而是「装配动作」，只有本模块的页面处理器需要它。
type productPagePortWiring interface {
	SetWarehouseOptionsPort(productcontract.WarehouseOptionsPort)
	SetEntityTemplatePort(productcontract.EntityTemplatePort)
	SetDetailTemplatePort(productcontract.DetailTemplatePort)
}

// NewProductPageHandle 构造。
func NewProductPageHandle(products productcontract.ProductService, projects projectcontract.ProjectService) *productPageHandle {
	return &productPageHandle{products: products, projects: projects}
}

// SetInventoryDeps 注入仓库清单依赖（issue #15，装配期调用）。
//
// 两个去向，同一次注入：① 本处理器的老路径（编辑页 / 详情页 / 捆绑页抽屉）拿走端口；
// ② 交给本模块 service 的读模型（新建页 / 列表页），否则新页会静默少掉仓库下拉。
// 未注入时两处都是「没有仓库」的降级（不是失败）。
func (h *productPageHandle) SetInventoryDeps(inventory inventorycontract.InventoryService) {
	port := inventoryoutbound.New(inventory)
	h.warehouses = port
	if wiring, ok := h.products.(productPagePortWiring); ok {
		wiring.SetWarehouseOptionsPort(port)
	}
}

// ==========================================================================
// 接口面处理器（JSON handler 的接收者）。
// 页面面处理器是 productPageHandle（见 product_page.go）。
// ==========================================================================
// Handle product 接口处理器。
type Handle struct {
	svc productcontract.ProductService
}

// NewHandle 构造。
func NewHandle(svc productcontract.ProductService) *Handle {
	return &Handle{svc: svc}
}
