// deps.go — 片段层的装配期依赖快照。
//
// 为什么不是二十来个包级 SetXxx：散装 setter 让「这一层到底依赖了什么」无法一眼看全，
// 也无法为测试构造独立实例；漏接一条只有运行时才暴露（表现为片段静默降级成文案）。
// 收敛成**一次** SetDependencies：装配方必须把全部字段一次到齐，新增依赖时
// 编译期就能点出所有需要跟进的装配点。
//
// 注入语义：装配期由 internal/routers 的装配链提交一次，此后全包只读。
// 字段顺序与装配链的赋值顺序一致（见 internal/routers/assembly*.go），便于逐项对照。
package runtimefragment

import (
	"sync/atomic"

	"github.com/gin-gonic/gin"

	"go_wp/internal/builder/core"
	cartcontract "go_wp/internal/module/cart/contract"
	commentcontract "go_wp/internal/module/comment/contract"
	contentcontract "go_wp/internal/module/content/contract"
	membershipcontract "go_wp/internal/module/membership/contract"
	ordercontract "go_wp/internal/module/order/contract"
	pagecontract "go_wp/internal/module/page/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	productcontract "go_wp/internal/module/product/contract"
	projectcontract "go_wp/internal/module/project/contract"
	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	usercontract "go_wp/internal/module/user/contract"
)

// Deps 是片段层的**全部**外部能力。
//
// 字段名与改造前的 SetXxx 一一对应（去掉 Set 前缀），端口名清单里的字面量
// （internal/routers/wiring.go 的 portRuntimeFrag*）也按同样的名字登记。
//
// 哪些字段可缺、缺了用户看到什么，判据在 internal/routers/wiring.go 的端口类别
// （required-* 缺失即装配缺陷并 fail-fast，optional-degraded 缺失写启动日志）。
// 这里**不再抄一份必填清单** —— 同一份判据留两份真源必然漂移。
type Deps struct {
	// BundleProvider 捆绑配置器端口（product；required-contract）。
	BundleProvider productcontract.BundleConfiguratorPort
	// VariantAvailabilityProvider 变体可用量（product；required-contract）。
	VariantAvailabilityProvider productcontract.VariantAvailabilityLookupPort
	// VariantSnapshotProvider 变体实时快照（product；required-contract）。
	VariantSnapshotProvider productcontract.VariantSnapshotPort
	// CartProvider 购物车与结算（cart；required-port）。
	CartProvider cartcontract.CartService
	// MembershipReader 会员身份读取（membership；可缺 → 会员片段显示「功能暂不可用」）。
	MembershipReader membershipcontract.Reader
	// MembershipFacingTexter 会员业务错误的文案出口（membership；可缺 → 本地通用文案，
	// 而不是把 err.Error() 里的表名 / 约束名 / SQLSTATE 抖到访客页面上）。
	MembershipFacingTexter membershipcontract.FacingTexter
	// CommentPort 评论读写端口（comment；可缺）。收窄到 FragmentPort：片段层拿不到
	// 后台审核（通过 / 驳回）与审核队列 —— 越权防护靠接口形状，不靠调用方自觉。
	CommentPort commentcontract.FragmentPort
	// CommentFacingTexter 评论业务错误的文案出口（comment；可缺 → 本地通用文案）。
	CommentFacingTexter commentcontract.FacingTexter
	// CommentSourceHasher 来源 IP → 带盐哈希（装配层闭包；可缺 → 空哈希）。
	//
	// 做成**注入的函数**而不是在本包算：哈希口径（盐从哪来、怎么拼、用什么摘要）
	// 属于评论模块 —— 本包 import 它的 service 会被架构门禁拦下
	// （internal/architecture 的 TestNoCrossModuleServiceModelImport：跨模块只允许 contract）。
	// 未注入时落**空哈希**（不是明文、也不是假哈希）：调用方据此略过来源维度的限流与取证，
	// 而「拿不到来源」与「来源是空串」在库里必须分得开（后者会让所有请求共用一个来源额度）。
	CommentSourceHasher func(ip string) string
	// CollectionResolver 集合源注册表（builder/core；required-port）。
	CollectionResolver core.CollectionResolver
	// ProductDataSource 商品构建期数据源（product；required-port）。
	// 取数口径必须与静态产物里的那份一致，否则「点筛选得到的」与「产物里的」会是两批数据。
	ProductDataSource productcontract.ProductDataSource
	// ContentSearchProvider 内容检索（content；required-contract）。
	ContentSearchProvider contentcontract.SearchPort
	// ProductSearchProvider 商品检索（product；required-contract）。
	ProductSearchProvider productcontract.SearchPort
	// PublishedEntityLocator 实体 → 已上线路径（presentation；required-contract）。
	PublishedEntityLocator presentationcontract.PublishedEntityLocator
	// SitePageResolver 系统页面槽位解析（page；可缺 → 槽位一律为空）。
	// 只依赖 page 模块的**受限读接口**：片段层拿不到发布 / 删除 / 改 URL 的能力。
	SitePageResolver pagecontract.SitePageResolver
	// FragmentProject 工程语言配置（project；可缺 → 只用默认语言）。
	FragmentProject projectcontract.ProjectService
	// VisitorOrderReader 访客订单查询（order；required-port）。归属校验在 order 模块的
	// SQL 条件里，不在这层。
	VisitorOrderReader ordercontract.VisitorOrderReader
	// CountryLabelReader 国家 / 地区字典只读口（sysconfig；可缺 → 显示代码）。
	// 一个展示标签的字典读不到，不该让访客的订单页失败。
	CountryLabelReader sysconfigcontract.DictReader
	// VisitorReturnProvider 访客退货（order；required-port）。类型是收窄过的
	// VisitorReturnPort：片段层拿不到「后台审核 / 入库 / 退款」那几条。
	VisitorReturnProvider ordercontract.VisitorReturnPort
	// VisitorIdentityMiddleware 访客身份解析中间件（尽力解析；未登录不阻断，
	// 必须登录的能力自己渲染引导文案）。
	VisitorIdentityMiddleware func(c *gin.Context)
	// VisitorAccountPort 访客账号事实读取（user；required-port）。片段层拿不到注册、
	// 改密码、踢出设备这些写能力。
	VisitorAccountPort usercontract.VisitorAccountPort
}

// deps 是 Deps 的唯一实例：装配期写入一次，之后全包只读。
//
// 为什么不用 atomic.Pointer / 不加锁：写入发生在 HTTP 服务启动**之前**
// （装配在主 goroutine 里跑完，handler 要到第一次请求才执行），
// 「写 → 读」的 happens-before 由 net/http 启动服务 goroutine 时建立。
// 反过来，「装配期之后还能改依赖」这项能力正是本次收敛要消灭的东西。
var deps Deps

// depsSealed 记录依赖快照是否已提交（装配缺陷检测，见 SetDependencies）。
var depsSealed atomic.Bool

// SetDependencies 提交片段层的依赖快照，**装配期只允许调用一次**。
//
// 重复调用直接 panic，而不是「记条日志后忽略」，理由：
//   - 没有正当场景：装配链是线性的且只跑一次（internal/routers 的 routes.go）；
//   - 「记录后忽略」会让第二个调用点看起来生效、实际被丢弃，症状是
//     「代码明明注入了，线上还是旧行为」这类最难查的静默失效；
//   - fail-fast 的代价只是启动即崩，收益是同一进程里不可能存在两份互相矛盾的依赖视图 ——
//     这与装配链既有的 mustAllPortsWired（缺端口即 panic）是同一套判据。
func SetDependencies(d Deps) {
	if !depsSealed.CompareAndSwap(false, true) {
		panic("runtimefragment: SetDependencies 重复调用（依赖快照只允许在装配期提交一次；" +
			"测试改依赖请用 MutateDepsForTest）")
	}
	deps = d
}

// MutateDepsForTest 在依赖快照上做一次局部变更，返回还原函数，**仅供测试**。
//
// 为什么是「局部变更」而不是「整体覆盖」：调用方要的是「在现有快照上换掉这一两条端口」，
// 整体覆盖会把没写到的字段一并归零 —— 等于让一个用例顺手拆掉同进程其它用例的依赖，
// 而这类连带破坏只在用例执行顺序变化时才现形。
//
// 典型用法（接管还原函数，用例结束自动复原）：
//
//	t.Cleanup(runtimefragment.MutateDepsForTest(func(d *runtimefragment.Deps) {
//		d.CommentPort = svc
//	}))
//
// 返回值可以丢弃 —— 那样变更一直保留到进程结束，语义与改造前逐个 setter 完全相同
// （装配期从未提交快照的单测进程里，这正是既有测试的用法）。
//
// 与 SetDependencies 的分工：不参与 depsSealed 判定，装配链先提交过也照样能改、能还原。
// 并发约束：可重复写，调用方必须保证同一时刻只有一个测试在改依赖 —— 与逐个 setter
// 时代完全相同（这批测试本来就不能 t.Parallel，因为被改的是包级状态）。
func MutateDepsForTest(mutate func(*Deps)) (restore func()) {
	prev := deps
	next := deps
	mutate(&next)
	deps = next
	return func() { deps = prev }
}
