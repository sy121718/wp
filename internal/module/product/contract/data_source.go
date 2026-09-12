package productcontract

import "go_wp/internal/builder/source"

// data_source.go — 商品对外暴露的**构建期数据源**（issue #35）。
//
// 与 ProductService 的区别是「谁能拿到什么」：
//
//	· ProductService 是后台/API 用的完整契约（含 Create / Update / Delete / 定价 / 批量…）；
//	· ProductDataSource 只有读能力，交给构建期（组件渲染页面时取数据）。
//
// 越权防护靠**接口形状**而不是调用方自觉：
//
//	· 写方法一个都不在 —— 不是「约定不要调」，是拿不到；
//	· 读集合走白名单字段 + 白名单过滤维度（ResolveCollection 内部就是这么实现的），
//	  cost_price 这类字段不在构建期字段白名单里，取不到；
//	· 不接受 SQL / 过滤表达式 / 任意 endpoint（不变量 4：Binding 不是 Query DSL）。
//
// 能力形状复用 internal/builder/source（零依赖的共享形状包），所以本接口的
// 定义位置在业务契约这边、形状定义在 source 那边，两边都不依赖 builder。
type ProductDataSource interface {
	source.CollectionResolver
	source.CollectionSchemaProvider
	// CollectionFilterOptionsProvider 可选能力：给出本工程可筛的分类 / 品牌 / 标签 / 属性值。
	// 未实现时列表组件不渲染筛选栏（降级为纯列表，不报错）。
	source.CollectionFilterOptionsProvider
}
