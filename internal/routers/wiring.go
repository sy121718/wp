// wiring.go — 装配期端口清单与启动自检（审计 CQ-019）。
//
// 背景：跨模块能力经「装配期注入的可空端口」连接（SetXxx + 调用方 nil 判断）。
// 端口未注入时，一部分实现是**合理的可见降级**（片段读不到库存 → 页面上显示
// 「以结算时库存为准」，访客看得见）；另一部分是不可接受的**静默降级**
// （主数据变更记录端口未注入 → 审计静默缺失 —— 一切看起来都正常，排查时
// 几乎不会想到装配问题）。两类端口用的是同一种注入写法（setter + nil 判断），
// 照抄错的那种只需要一次复制粘贴。
//
// 本文件把「分类判断」变成可核对、可执行的资产：
//
//	wiringManifest      全部装配期端口的分类表（端口 / 提供方 / 消费方 / 类别 / 未注入后果）；
//	wiringMarks         装配期采集「本端口已接入」的事实（routes.go 逐条标记）；
//	CheckWiring         纯函数自检：必需端口缺失即报出端口名与后果；
//	mustAllPortsWired   装配末尾 fail-fast 入口（一次报出全部缺失项，不炸在第一个）；
//	logDegradedOptional 可选端口未接入写进启动日志 —— 降级必须是**可见的**
//	                    （用户能看到 / 日志有记录），而不是只在注释里写一句。
//
// 分类口径（判据可复算，不靠印象）：
//
//	required-contract  消费方要求提供方实现某条**收窄契约**。未实现 = 装配缺陷。
//	required-port      端口实现在本进程内恒定可得（装配顺序确定、由本仓模块提供），
//	                   且未注入的后果是**功能性静默缺失或错误数据** —— 用户看到的是
//	                   「永远是 0」「永远暂不可用」「审计缺记录」，而不是一句降级提示。
//	optional-degraded  未注入有明确可见表现（面向用户的人话 / 明确错误 / 日志告警），
//	                   或未注入时的行为与接入前**逐字一致**（等价于功能开关关闭）。
//
// 反向判据同样成立：一个端口只要能说清「未注入 → 用户看到什么」，它就该是
// optional-degraded；说不清（只能说出「功能没了」）的就是 required-port。
// 新增端口时按这条判据填表，不要凭「感觉它不重要」写进 optional。
//
// 刻意**不做** fail-fast 的两处（评估过，判定保持现状，理由写在这里免得反复讨论）：
//
//	默认主题补齐失败（routes.go）：幂等补齐动作，不影响已有工程，新建工程仍即时
//	  获得默认主题 —— 失败阻断启动的代价（整个站起不来）远大于收益；
//	组件注册表版本比对失败：只影响「标记待重建」这条提示，重建本来也可由运维触发。
//
// 两者都记 Error 级日志，属可见降级，不满足 required-port 的判据。
package routers

import (
	"sort"
	"strings"

	"go_wp/pkg/logger"
)

// 端口名常量：与 wiringManifest 的 Port 字段一一对应。routes.go 只经这些常量标记，
// 手写字符串一旦拼错，自检就会「永远通过」——那比没有自检更糟。
const (
	portContentCollectionSource        = "content.CollectionSourceProvider"
	portProductCollectionSource        = "product.CollectionSourceProvider"
	portProductContentStore            = "product.SetContentStore"
	portContentContentStore            = "content.SetContentStore"
	portMailCipherSecret               = "mail.SetCipherSecret"
	portWebhookCipherSecret            = "webhook.SetCipherSecret"
	portWebhookDispatcher              = "order.SetWebhookDispatcher"
	portProductInventoryService        = "product.SetInventoryService"
	portProductInventoryModel          = "product.SetInventory"
	portInventoryVariantCost           = "inventory.SetVariantCost"
	portProductMasterDataChanges       = "product.SetMasterDataChanges"
	portInventoryMasterDataChanges     = "inventory.SetMasterDataChanges"
	portProductAvailability            = "product.SetAvailabilityPort"
	portProductArchiveEnsurer          = "product.SetArchiveInstanceEnsurer"
	portProductPublishedLocator        = "product.SetPublishedEntityLocator"
	portProductFragmentCacheBumper     = "product.SetFragmentCacheBumper"
	portPluginAdminAuthz               = "plugin.SetAdminAuthz"
	portProjectLocaleRetire            = "project.SetLocaleRetirePort"
	portPageExternalArtifactOwners     = "page.SetExternalArtifactOwners"
	portPageBlueprints                 = "page.SetBlueprints"
	portPageBuildQueue                 = "page.SetBuildQueue"
	portPageProductDataSource          = "page.SetProductDataSource"
	portPresentationBuildQueue         = "presentation.SetBuildQueue"
	portPresentationProductDataSource  = "presentation.SetProductDataSource"
	portPresentationSiteAssembly       = "presentation.SetNavigationService/SetSitePageResolver/SetMediaProbe/SetPluginService"
	portPipelinePageRebuilder          = "pipeline.Fanout.SetRebuilder(page)"
	portPipelinePresentationRebuilder  = "pipeline.Fanout.SetRebuilder(presentation)"
	portContentDependencyInvalidator   = "content.SetDependencyInvalidator"
	portNavigationSourceResolver       = "navigation.SetSourceResolver"
	portDashboardBlueprints            = "dashboard.SetBlueprints"
	portRuntimeFragBundle              = "runtimefragment.SetBundleProvider"
	portRuntimeFragVariantAvailability = "runtimefragment.SetVariantAvailabilityProvider"
	portRuntimeFragVariantSnapshot     = "runtimefragment.SetVariantSnapshotProvider"
	portRuntimeFragCart                = "runtimefragment.SetCartProvider"
	portRuntimeFragCollectionResolver  = "runtimefragment.SetCollectionResolver"
	portRuntimeFragProductDataSource   = "runtimefragment.SetProductDataSource"
	portRuntimeFragContentSearch       = "runtimefragment.SetContentSearchProvider"
	portRuntimeFragProductSearch       = "runtimefragment.SetProductSearchProvider"
	portRuntimeFragPublishedLocator    = "runtimefragment.SetPublishedEntityLocator"
	portRuntimeFragSitePageResolver    = "runtimefragment.SetSitePageResolver"
	portRuntimeFragProject             = "runtimefragment.SetFragmentProject"
	portRuntimeFragVisitorOrderReader  = "runtimefragment.SetVisitorOrderReader"
	portRuntimeFragVisitorReturn       = "runtimefragment.SetVisitorReturnProvider"
	portRuntimeFragVisitorIdentity     = "runtimefragment.SetVisitorIdentityMiddleware"
	portRuntimeFragVisitorAccount      = "runtimefragment.SetVisitorAccountPort"
)

// wiringKind 端口类别。
type wiringKind string

const (
	// wiringRequiredContract 消费方要求提供方实现某条收窄契约，未实现即装配缺陷。
	wiringRequiredContract wiringKind = "required-contract"
	// wiringRequiredPort 本进程内恒定可得、未注入即功能性静默缺失的端口。
	wiringRequiredPort wiringKind = "required-port"
	// wiringOptionalDegraded 未注入有可见表现或等价于功能未开启的端口。
	wiringOptionalDegraded wiringKind = "optional-degraded"
)

// wiringEntry 一条端口盘点记录。
//
// Consequence 是这张表的**唯一价值所在**：它必须写清「未注入 → 用户/运维看到什么」。
// required 的条目写不出可见表现（这正是它被判为 required 的理由），
// optional 的条目必须能写出可见表现（写不出就说明分类错了）。
type wiringEntry struct {
	// Port 端口名（与端口名常量逐字一致）。
	Port string
	// Provider 实现该端口的模块。
	Provider string
	// Consumer 消费该端口的模块。
	Consumer string
	// Kind 类别（见 wiringKind 的三个判据）。
	Kind wiringKind
	// Consequence 未注入时的后果。
	Consequence string
}

// wiringManifest 全部装配期端口的分类表（审计 CQ-019 的盘点结论）。
//
// 顺序按 routes.go 的装配顺序排列，便于对照阅读。
var wiringManifest = []wiringEntry{
	// —— 装配期契约断言（提供方必须实现收窄契约）——
	{portContentCollectionSource, "content", "集合源注册表", wiringRequiredContract,
		"集合绑定在构建期解析不到内容数据源（core.cardstack 等集合组件产出空列表）"},
	{portProductCollectionSource, "product", "集合源注册表", wiringRequiredContract,
		"core.productList 解析不到商品集合"},

	// —— 模块自装配内部的注入（routes.go 只记录「Setup 调用发生」）——
	{portProductContentStore, "product（自装配，db）", "product / 构建期", wiringOptionalDegraded,
		"商品可翻译字段逐字节回退原文（产物与接入译文前一致）"},
	{portContentContentStore, "content（自装配，db）", "content / 构建期", wiringOptionalDegraded,
		"内容可翻译字段逐字节回退原文（同上）"},
	{portPluginAdminAuthz, "admin", "plugin", wiringOptionalDegraded,
		"插件读不到当前用户权限上下文（AdminAuthz() 返回 nil，调用方判空降级）"},
	{portMailCipherSecret, "config app.secret", "mail", wiringOptionalDegraded,
		"保存发信账号时明确报错（宁可不能用，也不用弱密钥把 SMTP 密码当明文存）"},
	{portWebhookCipherSecret, "config app.secret", "webhook", wiringOptionalDegraded,
		"保存集成端点时明确报错（同一理由：绝不用弱密钥把签名密钥当明文存）"},
	{portWebhookDispatcher, "webhook", "order", wiringRequiredPort,
		"订单支付落账不再对外派发任何事件（订阅端点在支付后收不到投递，投递日志恒空且无告警）"},

	// —— 商品 / 库存域 ——
	{portProductInventoryService, "inventory", "product", wiringRequiredPort,
		"建变体不生成库存记录、SKU 编码缺仓短码前缀（库存真源缺行）"},
	{portProductInventoryModel, "inventory model", "product", wiringRequiredPort,
		"后台商品库存列恒为 0；删变体时「仍有非零库存则拒绝」的守卫失效"},
	{portInventoryVariantCost, "product", "inventory", wiringRequiredPort,
		"采购收货 / 生产入库的单价不回写 cost_price（入库单行记 cost_error）"},
	{portProductMasterDataChanges, "masterdata", "product", wiringRequiredPort,
		"商品 / 变体字段级审计静默缺失（改价格、改编码都查不到留痕）"},
	{portInventoryMasterDataChanges, "masterdata", "inventory", wiringRequiredPort,
		"货源资料变更审计静默缺失"},
	{portProductAvailability, "inventory", "product", wiringRequiredPort,
		"套餐整单校验 fail-closed（一律拒单，而不是按「无上限」放行超卖）"},
	{portProductArchiveEnsurer, "presentation", "product", wiringRequiredPort,
		"分类新建 / 改名后归档页不跟上（线上仍是旧路径）"},
	{portProductPublishedLocator, "presentation", "product", wiringRequiredPort,
		"商品集合项的 url 全空（列表页商品没有链接）"},
	{portProductFragmentCacheBumper, "runtimefragment", "product", wiringRequiredPort,
		"商品写操作后片段 HTML 缓存不失效，前台继续显示过期价"},

	// —— 工程 / 页面 / 发布域 ——
	{portProjectLocaleRetire, "page", "project", wiringRequiredPort,
		"禁用语言只记日志不下路由，该语言的站点仍在线上可访问"},
	{portPageExternalArtifactOwners, "presentation", "page", wiringRequiredPort,
		"反向产物对账把自动发布实例的产物误报成孤儿（一份看不出真假的对账结果）"},
	{portPageBlueprints, "blueprint", "page", wiringRequiredPort,
		"「从蓝图建页」静默建出空白页（要等编辑者打开画布才发现）"},
	{portPresentationBuildQueue, "build", "presentation", wiringRequiredPort,
		"自动发布实例的失效重建仍在触发进程里持进程内锁同步执行，多实例部署下拦不住重复重建"},
	{portPageBuildQueue, "build", "page", wiringRequiredPort,
		"超出单次上限的自动重建永久保持 stale（只能等运维手动触发）"},
	{portPageProductDataSource, "product", "page", wiringRequiredPort,
		"页面里的商品组件回退按名路由（取数口径与集合源不一致）"},
	{portPresentationProductDataSource, "product", "presentation", wiringRequiredPort,
		"详情页模板里的商品组件回退按名路由"},
	{portPresentationSiteAssembly, "navigation/page/media/plugin", "presentation", wiringRequiredPort,
		"自动发布详情页缺头尾菜单 / 槽位链接 / 响应式 srcset / 插件组件（只有详情页受影响）"},
	{portPipelinePageRebuilder, "page", "pipeline.Fanout", wiringRequiredPort,
		"内容变更只标记 stale 不自动重建（线上内容停在旧版本）"},
	{portPipelinePresentationRebuilder, "presentation", "pipeline.Fanout", wiringRequiredPort,
		"自动发布的详情页不在失效扇出里：内容更新只重建手工页，详情页继续给旧字节（审计 AR2-001）"},
	{portContentDependencyInvalidator, "pipeline.Fanout", "content", wiringOptionalDegraded,
		"内容变更不触发精确失效（行为与本端口接入前逐字一致）"},
	{portNavigationSourceResolver, "navsource", "navigation", wiringOptionalDegraded,
		"来源菜单项退化为记录自身的 title/path（不解析目标实体）"},
	{portDashboardBlueprints, "blueprint", "dashboard", wiringOptionalDegraded,
		"新建页面表单不显示「从蓝图开始」下拉（建页照常走空白草稿）"},

	// —— 运行时片段层（访问面）——
	{portRuntimeFragBundle, "product", "runtimefragment", wiringRequiredContract,
		"前台套餐配置器渲染失败（500）"},
	{portRuntimeFragVariantAvailability, "product", "runtimefragment", wiringRequiredContract,
		"商品详情规格选择器永远显示「以结算时库存为准」"},
	{portRuntimeFragVariantSnapshot, "product", "runtimefragment", wiringRequiredContract,
		"实时价格核对永远保持沉默（空片段，访客看到产物里的构建期价格）"},
	{portRuntimeFragCart, "cart", "runtimefragment", wiringRequiredPort,
		"购物车 / 结算六个能力一律「暂不可用」"},
	{portRuntimeFragCollectionResolver, "集合源注册表（core）", "runtimefragment", wiringRequiredPort,
		"商品列表片段直接报「集合解析器未接入（装配缺陷）」"},
	{portRuntimeFragProductDataSource, "product", "runtimefragment", wiringRequiredPort,
		"片段侧商品组件取不到受限数据源（回退按名路由）"},
	{portRuntimeFragContentSearch, "content", "runtimefragment", wiringRequiredContract,
		"站内搜索永远没有内容结果"},
	{portRuntimeFragProductSearch, "product", "runtimefragment", wiringRequiredContract,
		"站内搜索永远没有商品结果"},
	{portRuntimeFragPublishedLocator, "presentation", "runtimefragment", wiringRequiredContract,
		"搜索结果永远不给链接（路径解析缺失，宁可不给链接也不输出死链）"},
	{portRuntimeFragSitePageResolver, "page", "runtimefragment", wiringOptionalDegraded,
		"槽位链接不渲染（不输出死链）+ 日志告警，其余片段照常"},
	{portRuntimeFragProject, "project", "runtimefragment", wiringOptionalDegraded,
		"片段语言回落默认语言（单语言站点即此形态，多语言站点才会看出来）"},
	{portRuntimeFragVisitorOrderReader, "order", "runtimefragment", wiringRequiredPort,
		"访客订单列表 / 详情永远只显示「服务暂不可用」"},
	{portRuntimeFragVisitorReturn, "order", "runtimefragment", wiringRequiredPort,
		"退货申请永远只显示「退货功能暂不可用」"},
	{portRuntimeFragVisitorIdentity, "user", "runtimefragment", wiringRequiredPort,
		"片段层永远按未登录渲染（已登录访客看到登录引导）"},
	{portRuntimeFragVisitorAccount, "user", "runtimefragment", wiringRequiredPort,
		"账号中心四块永远只显示「账号功能暂不可用」"},
}

// wiringMarks 装配期已接入端口的集合（端口名 → true）。
type wiringMarks map[string]bool

// newWiringMarks 创建空的标记集合。
func newWiringMarks() wiringMarks { return wiringMarks{} }

// mark 记录一个端口已接入（routes.go 在每次注入后立即调用）。
func (m wiringMarks) mark(port string) { m[port] = true }

// CheckWiring 对「已接入端口集合」做自检（纯函数：不依赖装配过程、数据库与网络）。
//
// 返回两个切片：
//
//	missing —— 必需端口（required-contract / required-port）缺失的描述，
//	           形如「端口名（提供方 → 消费方）：未注入后果」；
//	unknown —— 出现在集合里、却不在清单里的端口名（清单漂移，同样要修）。
//
// 两者都按端口名排序，保证报错文案稳定可比对。
func CheckWiring(marks wiringMarks) (missing, unknown []string) {
	known := make(map[string]bool, len(wiringManifest))
	for _, e := range wiringManifest {
		known[e.Port] = true
		if marks[e.Port] {
			continue
		}
		// 判据见文件头：必需的两类缺失即报，可选的不报（只在日志里留痕）。
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

// describeWiring 把一条端口记录渲染成可读的一行（报错与日志共用同一份文案）。
func describeWiring(e wiringEntry) string {
	return e.Port + "（" + e.Provider + " → " + e.Consumer + "）：" + e.Consequence
}

// mustAllPortsWired 装配末尾调用：必需端口缺失即 fail-fast。
//
// 一次报出**全部**缺失项而不是炸在第一个：一次装配就能拿到完整清单，
// 省掉「补一个、再跑一次、再炸下一个」的往返。
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

// logDegradedOptional 把未接入的**可选**端口写进启动日志。
//
// 审计口径：可选降级必须可见。只写一句代码注释不算 —— 运维看日志时得能查到
// 「这台机器上哪些能力是关着的、各自会让用户看到什么」。
func logDegradedOptional(marks wiringMarks) {
	for _, e := range wiringManifest {
		if e.Kind != wiringOptionalDegraded || marks[e.Port] {
			continue
		}
		logger.Scene("init").With("port", e.Port).With("provider", e.Provider).
			Warn("可选端口未接入，按降级路径运行：" + e.Consequence)
	}
}

// RequireWiringPort 必需的装配期端口确认（供 routes.go 内联断言使用）。
//
// 与 CheckWiring 的分工：这里管「提供方必须实现该契约」（拿到 ok 的当场炸掉，
// 后面的代码不必再带着 invalid 值跑）；CheckWiring 管「清单里的端口有没有人接」。
func RequireWiringPort(port string, ok bool) {
	if ok {
		return
	}
	panic("装配自检失败：必需端口未接入 " + port +
		"\n  提供方未实现该契约；缺它时功能静默失效，见 internal/routers/wiring.go 的 wiringManifest")
}
