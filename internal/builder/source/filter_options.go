package source

import "context"

// filter_options.go — 集合源给出的可选筛选值（issue #27 起）。
//
// 与 CollectionSchema 的分工：Schema 说「允许筛哪些维度」，本文件说「这个工程里
// 每个维度现在有哪些可选值」。前者是契约（编译期固定），后者是数据（随工程内容变）。

// CollectionFilterChoice 一个可选筛选项（分类 / 品牌 / 标签 / 属性值通用）。
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
