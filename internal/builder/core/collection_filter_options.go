// collection_filter_options.go — 集合源的筛选选项（issue #27）。
//
// 列表组件在构建期要渲染筛选栏（分类树 / 品牌 / 标签 / 属性），但它只认识集合项数据，
// 不认识「这个工程有哪些分类、品牌、标签、属性组」。让组件自己去查商品库会越过模块边界，
// 所以由集合源按**能力探测**提供 —— 与 CollectionSchemaProvider 同一模式。
//
// 类型放在 core 而不是某个业务模块的 contract：它是「集合源的通用形状」，
// 构建器与组件包都不该依赖具体业务模块（那会把依赖方向反过来）。
package core

import "context"

// CollectionFilterChoice 一个可选的筛选值。
type CollectionFilterChoice struct {
	// ID 实体 id（分类 / 品牌 / 标签是 uuid）。
	ID string
	// Key 属性值 key（仅属性值非空 —— 属性值没有独立 id，它的标识就是 key）。
	Key string
	// Name 展示名（按构建语言取译文，与集合项字段同一套口径）。
	Name string
	// ParentID 分类的父分类 id（渲染分类树用；根节点为空）。
	ParentID string
}

// CollectionFilterAttributeGroup 一个可筛的属性组及其可选值。
type CollectionFilterAttributeGroup struct {
	// Key 属性组 key（筛选参数里写成 `option.<Key>=<值Key>`）。
	Key  string
	Name string
	// Values 可选值（按属性组定义顺序）。
	Values []CollectionFilterChoice
}

// CollectionFilterOptions 集合源给出的可选筛选项。
//
// 空切片表示该维度没有可选值（例如工程里一个品牌都没建）：组件据此**不渲染**那一块，
// 而不是渲染一个空标题。
type CollectionFilterOptions struct {
	Categories []CollectionFilterChoice
	Brands     []CollectionFilterChoice
	Tags       []CollectionFilterChoice
	Attributes []CollectionFilterAttributeGroup
}

// CollectionFilterOptionsProvider 可选能力：集合源能给出「这个工程有哪些可筛的值」。
//
// 未实现该能力的集合源：列表组件不渲染筛选栏（降级为纯粹的列表，不报错）——
// 契约缺失不该阻断构建，这条与 CollectionSchemaProvider 的处理一致。
type CollectionFilterOptionsProvider interface {
	CollectionFilterOptions(ctx context.Context, projectID string) (CollectionFilterOptions, error)
}
