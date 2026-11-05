package routers

import (
	"sort"
	"strings"

	"go_wp/pkg/logger"
)

const (
	portContentCollectionSource   = "content.CollectionSourceProvider"
	portProductCollectionSource   = "product.CollectionSourceProvider"
	portProductContentStore       = "product.SetContentStore"
	portContentContentStore       = "content.SetContentStore"
	portMailCipherSecret          = "mail.SetCipherSecret"
	portWebhookCipherSecret       = "webhook.SetCipherSecret"
	portWebhookDispatcher         = "order.SetWebhookDispatcher"
	portProductInventoryService   = "product.SetInventoryService"
	portProductInventoryModel     = "product.SetInventory"
	portInventoryVariantStockRefs = "inventory.SetVariantWarehouseStocks"
	portPurchaseVariantCost       = "purchase.SetVariantCost"
	// 商品侧是**构造参数注入**（SetupProductRoutes(..., changes)），不是 setter；
	// 采购侧仍是 setter。名字按真实注入方式给，别再用 product.SetMasterDataChanges
	// 这种「曾经长这样」的值 —— 找代码时会 grep 不到。
	portProductMasterDataChanges  = "product.MasterDataChanges"
	portPurchaseMasterDataChanges = "purchase.SetMasterDataChanges"
	portProductAvailability       = "product.SetAvailabilityPort"
	portProductArchiveEnsurer     = "product.SetArchiveInstanceEnsurer"
	portProductPublishedLocator   = "product.SetPublishedEntityLocator"
	portProductPurchaseChecker    = "product.SetPurchaseChecker"

	portProductPageWarehouses     = "product.SetWarehouseOptionsPort"
	portProductPageTemplates      = "product.SetEntityTemplatePort"
	portProductPageDetailTemplate = "product.SetDetailTemplatePort"

	portProductDetailTemplateWriteDeps = "product.SetDetailTemplateWriteDeps"

	portProductDetailTemplateMode = "product.SetDetailTemplateModePort"

	portAdminI18nPageMarker        = "admin.SetPageMarker"
	portPluginAdminAuthz           = "plugin.SetAdminAuthz"
	portProjectLocaleRetire        = "project.SetLocaleRetirePort"
	portPageExternalArtifactOwners = "page.SetExternalArtifactOwners"
	portPageArtifactReader         = "page.SetPageArtifacts"
	portPageBlueprints             = "page.SetBlueprints"
	portPageBuildQueue             = "page.SetBuildQueue"
	portPageProductDataSource      = "page.SetProductDataSource"
	portPageCheckoutCountries      = "page.SetCheckoutCountries"
	portPageI18nStalePeer          = "page.SetI18nStalePeer"
	portPageStructureTemplates     = "page.SetStructureTemplatePort"

	portOrderShipmentModels     = "order.SetShipmentModels"
	portOrderShipmentCounter    = "order.SetShipmentCounter"
	portOrderStockAlerts        = "inventory.SetStockAlerts"
	portOrderSalesStockAlerts   = "order.SetSalesStockAlerts"
	portOrderAttachmentResolver = "order.SetAttachmentResolver"
	portOrderStockRowLocator    = "inventory.SetStockRowLocator"
	portOrderWarehouseResolver  = "inventory.SetWarehouseResolver"

	portOrderShipStockProbe = "order.SetShipStockProbe"

	portOrderReturnWarehouseSource = "order.SetWarehouseSource"

	portOrderCurrencyLister = "order.SetCurrencyLister"

	portBlockPresetResolver        = "block.SetPresetResolver"
	portBlockStalePropagator       = "block.SetStalePropagator"
	portBlockReferenceUsageChecker = "block.SetReferenceUsageChecker"

	portI18nValueLoader               = "i18n.SetValueLoader"
	portDataRuleDeptResolver          = "builtin.SetDataRuleDeptResolver"
	portInventoryProductCatalog       = "inventory.SetProductCatalog"
	portPurchaseVariantCatalog        = "purchase.SetVariantCatalog"
	portAdminSideEffectWorker         = "admin.StartSideEffectWorker"
	portWorkbenchInstanceOverrideDeps = "workbench.SetInstanceOverrideDeps"
	portWorkbenchNavigationPicker     = "workbench.SetNavigationPicker"
	portContentTemplateImpactPort     = "contenttemplate.SetImpactPort"
	portProjectStructureTemplates     = "project.SetStructureTemplateOptionsPort"

	portMediaStaleMarkerPage             = "media.SetStaleMarker(page)"
	portMediaStaleMarkerPresentation     = "media.SetStaleMarker(presentation)"
	portPresentationBuildQueue           = "presentation.SetBuildQueue"
	portPresentationProductDataSource    = "presentation.SetProductDataSource"
	portPresentationCheckoutCountries    = "presentation.SetCheckoutCountries"
	portPresentationSiteAssembly         = "presentation.SetNavigationService/SetMediaProbe/SetPluginService"
	portPagePublishedInstanceLocator     = "page.SetPublishedInstanceLocator"
	portPresentationPublishedPageLocator = "presentation.SetPublishedPageLocator"
	portActionTargetReferences           = "page/presentation.SetActionTargetReferences/SetActionTargetInvalidator"
	portPipelinePageRebuilder            = "publish.Fanout.SetRebuilder(page)"
	portPipelinePresentationRebuilder    = "publish.Fanout.SetRebuilder(presentation)"
	portContentDependencyInvalidator     = "content.SetDependencyInvalidator"
	portProductDependencyInvalidator     = "product.SetDependencyInvalidator"
	portContentTemplateInvalidator       = "contenttemplate.SetInvalidator"
	portNavigationSourceResolver         = "navigation.SetSourceResolver"
	portNavigationMenuDispatcher         = "navigation.SetMenuStaleDispatcher"

	portNavigationTranslationWriter = "navigation.SetTranslationWriteService"
	portNavigationPageStalePort     = "navigation.TranslationWriter(page)"
	portNavigationInstanceStalePort = "navigation.TranslationWriter(presentation)"
	portNavigationMenuStaleEnqueue  = "navigation.TranslationWriter(build)"

	portNavigationMenuWritePorts = "navigation.SetWritePorts"
	portDashboardBlueprints      = "dashboard.SetBlueprints"

	portDashboardOverview   = "dashboard.SetOverviewPorts(frontend)"
	portPublicSearchContent = "content.PublicSearchService.articles"
	portPublicSearchProduct = "content.PublicSearchService.products"
	portPublicSearchIndex   = "content.PublicSearchService.publications"

	portAccessGuardLoginProbe = "builtin.SetAccessGuardLoginProbe"

	portOrderMembershipReader    = "order.SetMembershipReader"
	portMembershipPurchaseSource = "membership.SetPurchaseSource"
	portCartMembershipReader     = "cart.SetMembershipReader"

	portCartShippingPolicy = "cart.SetShippingPolicyReader"

	portCommentEntityPolicy = "comment.SetEntityPolicy"
)

type wiringKind string

const (
	wiringRequiredContract wiringKind = "required-contract"

	wiringRequiredPort wiringKind = "required-port"

	wiringOptionalDegraded wiringKind = "optional-degraded"
)

type wiringEntry struct {
	Port string

	Provider string

	Consumer string

	Kind wiringKind

	Consequence string
}

var wiringManifest = []wiringEntry{

	{portContentCollectionSource, "content", "集合源注册表", wiringRequiredContract,
		"集合绑定在构建期解析不到内容数据源（core.cardstack 等集合组件产出空列表）"},
	{portProductCollectionSource, "product", "集合源注册表", wiringRequiredContract,
		"core.productList 解析不到商品集合"},

	{portProductContentStore, "product（自装配，db）", "product / 构建期", wiringOptionalDegraded,
		"商品可翻译字段逐字节回退原文（产物与接入译文前一致）"},
	{portProductPageWarehouses, "inventory（经 product.SetInventoryDeps 转交）", "product 页面读模型（新建页 / 列表页）", wiringOptionalDegraded,
		"商品新建页 / 列表页没有「归属仓」与「从仓库选」两块（页面照常 200，只是少这两块）——与接线前逐字一致"},
	{portProductPageTemplates, "contenttemplate（经 product.SetDetailTemplateDeps 转交）", "product 页面读模型（新建页 / 列表页）", wiringOptionalDegraded,
		"商品新建页的模板候选为空并给「模板能力未装配」提示（同上：与接线前逐字一致）"},
	{portProductPageDetailTemplate, "contenttemplate + presentation（经 product.SetDetailTemplateDeps 转交）", "product 页面读模型（详情页模板页）", wiringOptionalDegraded,
		"详情页模板页的模板清单读不到 = 整页 500（它是那一屏的主数据），不再退化成空清单 —— 这一档与接线前逐字一致（接线前同样把 List 失败算装载失败）"},
	{portProductDetailTemplateWriteDeps, "contenttemplate + presentation + product（经 product.SetupProductRoutes 后置转交）", "product 详情页模板写动作（新建 / 首次发布 / 切换 / 改 URL）", wiringOptionalDegraded,
		"四个写动作全部拿不到依赖 → 端点回 500（不发静默成功）；页面其余部分照常"},
	{portProductDetailTemplateMode, "presentation（assembly_publish.go 后置绑定）", "product 详情页双轨写动作（迁移 282）", wiringRequiredPort,
		"双轨写端点拿不到 presentation 能力 → 整条不可用（回 500）；product 的接口面早于 presentation 装配，所以这条只能是后置绑定"},
	{portAdminI18nPageMarker, "page", "admin 词条页（词条变更 → 站点待重建）", wiringRequiredPort,
		"改了词条站点不更新，且**没有任何报错** —— 与页 / 商品 / 导航译文、站点设置同一条失效链，故按必需端口接线（consumer 侧最小接口 adminI18nPageMarker）"},
	{portContentContentStore, "content（自装配，db）", "content / 构建期", wiringOptionalDegraded,
		"内容可翻译字段逐字节回退原文（同上）"},

	{portOrderShipmentModels, "order（自装配，db）", "order / 发货单读写", wiringRequiredPort,
		"发货单的建单与查询拿不到 model 句柄 → 发货入口整条不可用（页面能开，点发货 500）"},
	{portOrderShipmentCounter, "order（svc 自实现）", "order 销售总览 / 待发货块", wiringOptionalDegraded,
		"销售总览的「待发货」一格显示为空（其余三卡照常）"},
	{portOrderStockAlerts, "inventory（经订单装配注入）", "order 库存预警数据源", wiringOptionalDegraded,
		"库存预警读不到（销售总览那一格留空，其余三卡照常）"},
	{portOrderSalesStockAlerts, "order（装配层后置注入）", "order 销售总览 / 库存预警块", wiringOptionalDegraded,
		"销售总览的「库存预警」一格显示为空；订单模块不 import 库存 dto，缺接线时只是少一格"},
	{portOrderAttachmentResolver, "media（经订单装配注入）", "order 发货凭证附件", wiringOptionalDegraded,
		"发货凭证的附件链接不可点（附件记录仍在，只是解析器没接上）"},
	{portOrderStockRowLocator, "inventory（经订单装配注入）", "order 发货明细 / 库存行快照", wiringOptionalDegraded,
		"发货明细的 stock_id 留空（该列只用于追溯，发货照常进行）"},
	{portOrderWarehouseResolver, "inventory（经订单装配注入）", "order 发货仓解析", wiringRequiredPort,
		"发货时解析不到发货仓 → 整条发货失败（预占与扣减必须落在同一个仓）"},
	{portOrderShipStockProbe, "inventory（经订单装配注入）", "order 待发货页可发守卫", wiringOptionalDegraded,
		"待发货页不再禁用发不出去的单：按钮一律可点，点了由发货接口拒绝（行为退回加守卫之前）"},
	{portOrderReturnWarehouseSource, "inventory（经订单装配注入）", "order 退货页原发货仓 / 入库仓下拉", wiringOptionalDegraded,
		"退货页的「原发货仓」列恒显示「—」（id 换不出名字），入库仓下拉为空；退货与退款照常"},
	{portOrderCurrencyLister, "sysconfig（经 order/outbound/currencydict 适配）", "order 建单币种校验 / 建单页币种下拉", wiringOptionalDegraded,
		"建单不再校验币种（写进 orders.currency 的可能是字典里没有的码），建单页币种下拉只剩「用站点默认币」一项；外币单仍可建"},
	{portPluginAdminAuthz, "admin", "plugin", wiringOptionalDegraded,
		"插件读不到当前用户权限上下文（AdminAuthz() 返回 nil，调用方判空降级）"},
	{portMailCipherSecret, "config app.secret", "mail", wiringOptionalDegraded,
		"保存发信账号时明确报错（宁可不能用，也不用弱密钥把 SMTP 密码当明文存）"},
	{portWebhookCipherSecret, "config app.secret", "webhook", wiringOptionalDegraded,
		"保存集成端点时明确报错（同一理由：绝不用弱密钥把签名密钥当明文存）"},
	{portWebhookDispatcher, "webhook", "order", wiringRequiredPort,
		"订单支付落账不再对外派发任何事件（订阅端点在支付后收不到投递，投递日志恒空且无告警）"},

	{portProductInventoryService, "inventory", "product", wiringRequiredPort,
		"建变体不生成库存记录、SKU 编码缺仓短码前缀（库存真源缺行）"},
	{portProductInventoryModel, "inventory ProductStockReader", "product", wiringRequiredPort,
		"变体库存投影恒为 0；删变体时的非零库存守卫失效"},
	{portInventoryVariantStockRefs, "product VariantWarehouseStockPort", "inventory", wiringRequiredPort,
		"可用量 / 成本快照 / 出库跨仓分配读不到「这个变体引用了哪些货行」（页面显示 0 且不报错）"},
	{portPurchaseVariantCost, "product", "purchase", wiringRequiredPort,
		"采购收货 / 生产入库的单价不回写 cost_price（入库单行记 cost_error）"},
	{portProductMasterDataChanges, "masterdata → product 构造参数", "product", wiringRequiredPort,
		"商品 / 变体字段级审计静默缺失（改价格、改编码都查不到留痕）"},
	{portPurchaseMasterDataChanges, "masterdata", "purchase", wiringRequiredPort,
		"货源资料变更审计静默缺失"},
	{portProductAvailability, "inventory", "product", wiringRequiredPort,
		"套餐整单校验 fail-closed（一律拒单，而不是按「无上限」放行超卖）"},
	{portProductArchiveEnsurer, "presentation", "product", wiringRequiredPort,
		"分类新建 / 改名后归档页不跟上（线上仍是旧路径）"},
	{portProductPublishedLocator, "presentation", "product", wiringRequiredPort,
		"商品集合项的 url 全空（列表页商品没有链接）"},
	{portProductPurchaseChecker, "order", "product", wiringOptionalDegraded,
		"「商品评论必须买过」不生效：任何登录访客都能提交商品评论（提交仍进审核队列）"},

	{portProjectLocaleRetire, "page", "project", wiringRequiredPort,
		"禁用语言只记日志不下路由，该语言的站点仍在线上可访问"},
	{portPageI18nStalePeer, "presentation", "page", wiringRequiredPort,
		"改了词条 / 译文后自动发布实例不跟进重建（商品页继续渲染旧字节，且**没有任何报错**）"},
	{portPageExternalArtifactOwners, "presentation", "page", wiringRequiredPort,
		"反向产物对账把自动发布实例的产物误报成孤儿（一份看不出真假的对账结果）"},
	{portPageArtifactReader, "artifact", "page", wiringRequiredPort,
		"依赖归属校验直接失败（构建被打回，而不是把依赖记到别的工程的产物上）；孤儿对账退化为只按本模块清单判定"},
	{portPageBlueprints, "blueprint", "page", wiringRequiredPort,
		"「从蓝图建页」静默建出空白页（要等编辑者打开画布才发现）"},
	{portI18nValueLoader, "sysconfig", "pkg/i18n（全局默认值）", wiringRequiredPort,
		"全局默认语言 / 站点语言 URL 方案 / 默认币种读不到 —— 静默使用代码内常量（站点行为与配置不一致，日志里只有一行载入失败）"},
	{portDataRuleDeptResolver, "admin", "builtin（数据权限中间件）", wiringRequiredPort,
		"按部门的数据权限规则静默失效：该被拦的数据放行或被误拦，页面上看不出区别"},
	{portAdminSideEffectWorker, "admin", "admin（自带 worker）", wiringRequiredPort,
		"策略重载与会话吊销退化为「只做一次、失败就留在表里没人重试」：权限变更要等下次重启才在全进程生效"},
	{portInventoryProductCatalog, "product", "inventory（商品 → 变体下拉）", wiringOptionalDegraded,
		"库存页的「商品 → 变体」下拉为空（用户看到空列表，不是错误）"},
	{portPurchaseVariantCatalog, "product + inventory", "purchase（采购行 SKU 下拉）", wiringOptionalDegraded,
		"采购入库页的 SKU 下拉为空（用户看到空列表，不是错误；入库入口仍可手工填编码）"},
	{portWorkbenchInstanceOverrideDeps, "presentation", "workbench", wiringOptionalDegraded,
		"实例编辑模式（?instance= 画布改覆盖文档）的保存走不通：编辑者改了画布不生效"},
	{portWorkbenchNavigationPicker, "navigation", "workbench", wiringOptionalDegraded,
		"检查器的「具体菜单项」下拉为空（nav 组件绑定导航时要手输 id）"},
	{portContentTemplateImpactPort, "page + presentation", "contenttemplate", wiringOptionalDegraded,
		"模板影响面与删除保护退化为仅查本模块表与实例外键（删模板不再拦页面引用）"},
	{portProjectStructureTemplates, "contenttemplate", "project（主题设置）", wiringOptionalDegraded,
		"主题设置页的「选结构模板」下拉只有「不绑定」"},
	{portBlockPresetResolver, "builder", "block", wiringRequiredPort,
		"首次业务预设无法解析，创建不能获得权威文档"},
	{portBlockStalePropagator, "block", "page / presentation", wiringRequiredPort,
		"块变更不传播 → 改了页眉块站点停旧字节且无报错"},
	{portBlockReferenceUsageChecker, "block", "block（删除保护）", wiringRequiredPort,
		"删除保护整体失效或只覆盖一部分：块被删掉后，引用它的页面到下一次构建才暴露（且不报错）"},
	{portPageStructureTemplates, "contenttemplate", "page", wiringRequiredPort,
		"页眉 / 页脚绑定的结构模板静默失效：构建期回退到旧块绑定，站点上仍是旧页眉（或没有页眉），且没有任何报错"},

	{portMediaStaleMarkerPage, "page", "media", wiringRequiredPort,
		"换图后已发布页面永不更新：产物 srcset 指向旧变体名，旧文件按设计保留且 immutable 长缓存，访客一直看到旧图"},
	{portMediaStaleMarkerPresentation, "presentation", "media", wiringRequiredPort,
		"换图后自动发布实例永不更新（判据同上：旧变体名仍返回旧字节且 immutable）"},
	{portPresentationBuildQueue, "build", "presentation", wiringRequiredPort,
		"自动发布实例的失效重建仍在触发进程里持进程内锁同步执行，多实例部署下拦不住重复重建"},
	{portPageBuildQueue, "build", "page", wiringRequiredPort,
		"超出单次上限的自动重建永久保持 stale（只能等运维手动触发）"},
	{portPageProductDataSource, "product", "page", wiringRequiredPort,
		"页面里的商品组件回退按名路由（取数口径与集合源不一致）"},
	{portPresentationProductDataSource, "product", "presentation", wiringRequiredPort,
		"详情页模板里的商品组件回退按名路由"},
	{portPageCheckoutCountries, "sysconfig", "page", wiringRequiredPort,
		"含结算表单（且表单收集国家）的页面构建失败，报「未注入国家清单」（手工页整页发不出去）"},
	{portPresentationCheckoutCountries, "sysconfig", "presentation", wiringRequiredPort,
		"自动发布详情页里的结算表单构建失败（与手工页面同一条判据，来源实例是同一个）"},
	{portPagePublishedInstanceLocator, "presentation", "page", wiringRequiredPort,
		"页面无法解析已发布实例的显式动作目标，发布被拒绝"},
	{portPresentationPublishedPageLocator, "page", "presentation", wiringRequiredPort,
		"实例无法解析已发布页面的显式动作目标，发布被拒绝"},
	{portActionTargetReferences, "发布源引用与pipeline失效扇出", "page / presentation", wiringRequiredPort,
		"显式目标删除保护或改址下线失效通知缺失，可能留下死链接"},
	{portPresentationSiteAssembly, "navigation/page/media/plugin", "presentation", wiringRequiredPort,
		"自动发布详情页缺头尾菜单 / 槽位链接 / 响应式 srcset / 插件组件（只有详情页受影响）"},
	{portPipelinePageRebuilder, "page", "publish.Fanout", wiringRequiredPort,
		"内容变更只标记 stale 不自动重建（线上内容停在旧版本）"},
	{portPipelinePresentationRebuilder, "presentation", "publish.Fanout", wiringRequiredPort,
		"自动发布的详情页不在失效扇出里：内容更新只重建手工页，详情页继续给旧字节（审计 AR2-001）"},
	{portContentDependencyInvalidator, "publish.Fanout", "content", wiringOptionalDegraded,
		"内容变更不触发精确失效（行为与本端口接入前逐字一致）"},
	{portProductDependencyInvalidator, "publish.Fanout", "product", wiringRequiredPort,
		"商品写路径的静态产物失效事件无人消费：详情页与集合列表页永远停在旧字节（审计 ARCH-01）"},
	{portContentTemplateInvalidator, "publish.Fanout", "contenttemplate", wiringOptionalDegraded,
		"模板产生新版本 / 切换生效后不触发精确失效：引用它的页面与实例停留在旧字节"},
	{portNavigationSourceResolver, "navsource", "navigation", wiringOptionalDegraded,
		"来源菜单项退化为记录自身的 title/path（不解析目标实体）"},
	{portNavigationMenuDispatcher, "publish.Fanout", "navigation", wiringRequiredPort,
		"改公开站点导航不派发任何失效：已发布页面的页眉/页脚永远停在旧菜单（全站可见且无报错）"},

	{portNavigationTranslationWriter, "navigation（TranslationWriter 构造于装配层）", "navigation.inbound/http", wiringRequiredPort,
		"译文保存回退到旧的三次独立提交路径或整体不可用：写成功而 stale/入队丢失，站点停在旧菜单标签且无报错"},
	{portNavigationPageStalePort, "page", "navigation.TranslationWriter", wiringRequiredPort,
		"译文写成功但手工页面不标 stale：已发布页面永远停在旧菜单文字"},
	{portNavigationInstanceStalePort, "presentation", "navigation.TranslationWriter", wiringRequiredPort,
		"译文写成功但自动发布实例不标 stale：详情页/商品页停在旧菜单文字，线上与库里都看不出异常"},
	{portNavigationMenuStaleEnqueue, "build", "navigation.TranslationWriter", wiringRequiredPort,
		"译文写成功但重建任务不入队：既无页面级重建也无实例级重建，只能等下一次人工触发"},

	{portNavigationMenuWritePorts, "page/build", "navigation（菜单 CRUD）", wiringRequiredPort,
		"菜单增删改排序只写数据不派发失效：已发布页面/实例停在旧菜单上且无报错"},
	{portDashboardBlueprints, "blueprint", "dashboard", wiringOptionalDegraded,
		"新建页面表单不显示「从蓝图开始」下拉（建页照常走空白草稿）"},
	{portDashboardOverview, "order/analytics/page", "dashboard", wiringOptionalDegraded,
		"概览页的 KPI / 趋势 / 热销榜 / 文章浏览量显示为空态（页面壳与页面列表照常）"},

	{portPublicSearchContent, "content", "content.PublicSearchService", wiringRequiredContract,
		"站内搜索永远没有内容结果"},
	{portPublicSearchProduct, "product", "content.PublicSearchService", wiringRequiredContract,
		"站内搜索永远没有商品结果"},
	{portPublicSearchIndex, "presentation", "content.PublicSearchService", wiringRequiredContract,
		"搜索结果永远不给链接（路径解析缺失，宁可不给链接也不输出死链）"},

	{portAccessGuardLoginProbe, "user", "访问面守卫（builtin.AccessGuardMiddleware）", wiringOptionalDegraded,
		"「登录用户可见」的页面把所有人挡在守卫页外（含已登录访客）；密码保护的页面不受影响"},

	{portOrderMembershipReader, "membership", "order", wiringOptionalDegraded,
		"会员折扣恒为 0：订单金额与本端口接入前逐字一致（会员等级照样显示，只是不参与算钱）"},
	{portMembershipPurchaseSource, "order", "membership", wiringRequiredPort,
		"会员归属的自动升级整体不可用：日结调度器禁用（启动日志有 Warn）、后台「立即重算」报错，等级只能手工维护"},
	{portCartMembershipReader, "membership", "cart", wiringOptionalDegraded,
		"会员免运费不生效：结算运费与接入前逐字一致（有基准运费时会员照付）"},
	{portCartShippingPolicy, "project", "cart", wiringOptionalDegraded,
		"站点级基础运费与满额免运费都不生效：结算运费恒为 0（与接入前逐字一致，故无回归，但后台配了运费会看起来没生效）"},

	{portCommentEntityPolicy, "product（或其它拥有实体的模块）", "comment", wiringOptionalDegraded,
		"差异化评论规则（如「商品评论必须买过」）未启用：所有提交一律进审核队列，启动日志有 Warn"},
}

type wiringMarks map[string]bool

func newWiringMarks() wiringMarks { return wiringMarks{} }

func (m wiringMarks) mark(port string) { m[port] = true }

func CheckWiring(marks wiringMarks) (missing, unknown []string) {
	known := make(map[string]bool, len(wiringManifest))
	for _, e := range wiringManifest {
		known[e.Port] = true
		if marks[e.Port] {
			continue
		}

		if e.Kind == wiringRequiredContract || e.Kind == wiringRequiredPort {
			missing = append(missing, describeWiring(e))
		}
	}
	for port := range marks {
		if !known[port] {
			unknown = append(unknown, port)
		}
	}
	sort.Strings(missing)
	sort.Strings(unknown)
	return missing, unknown
}

func describeWiring(e wiringEntry) string {
	return e.Port + "（" + e.Provider + " → " + e.Consumer + "）：" + e.Consequence
}

func mustAllPortsWired(marks wiringMarks) {
	missing, unknown := CheckWiring(marks)
	if len(missing) == 0 && len(unknown) == 0 {
		return
	}
	var b strings.Builder
	b.WriteString("装配自检失败：端口接线不完整")
	if len(missing) > 0 {
		b.WriteString("\n必需端口未接入：\n  - " + strings.Join(missing, "\n  - "))
	}
	if len(unknown) > 0 {
		b.WriteString("\n清单漂移（已标记但未登记，请补 internal/routers/wiring.go 的 wiringManifest）：\n  - " +
			strings.Join(unknown, "\n  - "))
	}
	panic(b.String())
}

func logDegradedOptional(marks wiringMarks) {
	for _, e := range wiringManifest {
		if e.Kind != wiringOptionalDegraded || marks[e.Port] {
			continue
		}
		logger.Scene("init").With("port", e.Port).With("provider", e.Provider).
			Warn("可选端口未接入，按降级路径运行：" + e.Consequence)
	}
}

func RequireWiringPort(port string, ok bool) {
	if ok {
		return
	}
	panic("装配自检失败：必需端口未接入 " + port +
		"\n  提供方未实现该契约；缺它时功能静默失效，见 internal/routers/wiring.go 的 wiringManifest")
}
