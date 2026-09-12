// collection_filter_options.go — 集合源的筛选选项（issue #27）。
//
// 列表组件在构建期要渲染筛选栏（分类树 / 品牌 / 标签 / 属性），但它只认识集合项数据，
// 不认识「这个工程有哪些分类、品牌、标签、属性组」。让组件自己去查商品库会越过模块边界，
// 所以由集合源按**能力探测**提供 —— 与 CollectionSchemaProvider 同一模式。
//
// 类型放在 core 而不是某个业务模块的 contract：它是「集合源的通用形状」，
// 构建器与组件包都不该依赖具体业务模块（那会把依赖方向反过来）。
package core

import (
	"go_wp/internal/builder/source"
)

// CollectionFilterChoice — 定义已搬到 internal/builder/source（issue #35），
// 此处保留别名：既有引用（组件、构建管线、工作台）不必跟着改。
type CollectionFilterChoice = source.CollectionFilterChoice

// CollectionFilterAttributeGroup — 定义已搬到 internal/builder/source（issue #35），
// 此处保留别名：既有引用（组件、构建管线、工作台）不必跟着改。
type CollectionFilterAttributeGroup = source.CollectionFilterAttributeGroup

// CollectionFilterOptions — 定义已搬到 internal/builder/source（issue #35），
// 此处保留别名：既有引用（组件、构建管线、工作台）不必跟着改。
type CollectionFilterOptions = source.CollectionFilterOptions

// CollectionFilterOptionsProvider — 定义已搬到 internal/builder/source（issue #35），
// 此处保留别名：既有引用（组件、构建管线、工作台）不必跟着改。
type CollectionFilterOptionsProvider = source.CollectionFilterOptionsProvider
