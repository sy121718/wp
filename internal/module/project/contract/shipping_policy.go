package projectcontract

// shipping_policy.go — 站点运费规则的**收窄只读端口**（消费者是 cart 的结算）。
//
// 为什么端口留在 project 侧、由 cart 消费：运费规则存在 projects.settings（project 的表），
// 而消费方是结算（cart）—— **cart 不允许 import project 的 service/model**。
// 所以这里只给一条「读这个工程的运费规则」的只读方法：结算链路上拿不到工程 CRUD、
// 站点设置写入，或任何别的字段。与 order 的 CustomerOrderSummaryReader 同一条思路 ——
// 越权防护靠接口形状，不靠调用方自觉。
//
// 与 project 既有的 LocaleRetirePort / StructureTemplateOptionsPort 的区别：
// 那两个是「project 消费、产物侧实现」；本端口是「project 实现、cart 消费」，
// 所以声明位置一样，实现方是本模块自己的 service（编译期断言在 service 里）。

import (
	"context"

	projectdto "go_wp/internal/module/project/dto"
)

// ShippingPolicy 站点运费规则的不可变视图（单位分；零值 = 不收运费）。
//
// 重导出同一份 dto：跨模块调用方只依赖 contract，不直接 import project/dto
// （与 SiteSettings 的既有写法一致，避免同一结构出现两份定义）。
type ShippingPolicy = projectdto.ShippingPolicy

// ShippingPolicyReader 站点运费规则的只读端口（一条只读方法）。
type ShippingPolicyReader interface {
	// ShippingPolicyOf 返回该工程当前的运费规则（分）。
	//
	// 工程不存在、存储值非法时返回**零值**（= 不收运费）而不报错：这两种情形下
	// 「这单该收多少运费」的正确答案就是「不知道」，而编造一个金额比按 0 走更糟 ——
	// 详见 cart 侧 cart_shipping.go 的失效方向判据。error 只留给基础设施故障，
	// 由调用方决定失效方向。
	ShippingPolicyOf(ctx context.Context, projectID string) (policy ShippingPolicy, err error)
}
