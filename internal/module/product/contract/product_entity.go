// product_entity.go — 商品域实体类型与字段白名单（issue #6；issue #12 扩到分类/品牌/标签/属性）。
//
// 本模块是自身实体类型字段白名单的唯一来源（与 content 模块的 contract 白名单同构）：
// 装配期把商品域实体类型注册进实体类型注册表（core.EntitySourceRegistry），内容模板与
// 发布实例据此做类型与字段校验，构建期解析器据此拒绝白名单之外的绑定（不变量 4）。
//
// 白名单只放「可展示」的字段：价格全部由变体派生（商品主体不存价格，issue #5 已定
// 语义），因此对外暴露的是派生值而不是原列。
//
// 多语言（issue #12）：可翻译字段的唯一来源是 translatableFields（下面五个类型集合），
// 构建期按构建语言取 sys_translation 译文（语境 实体类型.字段名，docs/06-D §7.5）。
// 标识类字段（slug / key / SKU / 价格 / 库存）永不参与翻译 —— 它们进筛选参数与 URL 段，
// 一旦被翻译，链接与筛选条件立刻失效（issue #12 验收 4/5）。
package productcontract

import (
	"sort"
	"strings"

	"go_wp/internal/builder/core"
	productenums "go_wp/internal/module/product/enums"
)

// 商品域实体类型标识（实体类型注册表与内容模板 entityType 的取值）。
//
// product 之外的四个类型（issue #12）让分类名 / 品牌名 / 标签名 / 属性值展示文本
// 拥有与其他实体同一套「取词 + 翻译」路径：它们的解析器由本模块提供，
// 构建层只认注册表、不认识具体领域（与 product 同构）。
const (
	EntityTypeProduct   = "product"
	EntityTypeCategory  = "product_category"
	EntityTypeBrand     = "product_brand"
	EntityTypeTag       = "product_tag"
	EntityTypeAttribute = "product_attribute"
)

// fieldWhitelist 各实体类型可绑定字段白名单（顺序即工作台下拉顺序）。
//
// 语义：
//   - product：name / subtitle / description —— 作者填写的文本，参与内容翻译；
//     imageAlts —— 图集每张图的可选 alt 文本（JSON 数组，与 images 逐位对应）；
//     slug / sku / unit —— 标识与单位，纯文本但不参与翻译；
//     images / defaultImage —— 图集（JSON 数组）与主图（URL）；
//     price / comparePrice / priceRange / minPrice / maxPrice —— 由变体派生的
//     只读价格值，纯数字与符号，永不参与翻译；
//     options / variants —— 由属性组与变体派生的规格数据（JSON），供 core.product
//     渲染规格选择器（单变体商品在前台不输出选择器）；
//     related —— 商品挂载的分类 / 品牌 / 标签 / 属性值的展示文本（JSON），
//     这些文本的译文按各自实体类型取词后填入，键（key/slug）原样保留。
//   - product_category / product_brand / product_tag：name 与描述 / SEO 文本；
//     slug 永不翻译。
//   - product_attribute：name（属性组名）与 values（属性值展示文本 JSON 数组）；
//     key 永不翻译 —— 它是筛选参数与规格组合的稳定标识。
var fieldWhitelist = map[string][]string{
	EntityTypeProduct: {
		"name", "subtitle", "description",
		"imageAlt", "imageAlts",
		"slug", "sku", "unit",
		"images", "defaultImage",
		"price", "comparePrice", "priceRange", "minPrice", "maxPrice",
		"options", "variants",
		"related",
		// tags 是标签展示名数组（issue #22：商品卡要显示标签）。
		// 与 related 里的 tags 不是重复：related 给的是 {slug,name} 完整结构（要链到标签页），
		// tags 只给名称数组（卡片上一行小标签），拿到的形状与用途都不同。
		"tags",
		// createdAt 是创建时间（RFC3339，issue #22：商品列表组件要按它排序）。
		// 集合源返回的默认序是 (sort, created_at, id)，但组件拿不到排序号 —— 想让「最新上架」
		// 成为可选项，就必须有一个能比较的时间值进集合项。
		"createdAt",
	},
	EntityTypeCategory: {
		"name", "slug", "description", "image",
		"seoTitle", "seoDescription",
	},
	EntityTypeBrand: {
		"name", "slug", "logo", "description",
		"seoTitle", "seoDescription",
	},
	EntityTypeTag: {
		"name", "slug",
	},
	EntityTypeAttribute: {
		"name", "key", "values",
	},
}

// translatableFields 各实体类型参与内容翻译（sys_translation）的字段集合。
//
// 取词语境固定为 "{实体类型}.{字段名}"（docs/06-D §7.5；spec #2「商品字段的取值路径
// 必须按 实体.字段名 的语境界定接入翻译」）。slug / SKU / key / 价格 / 库存不在此列。
//
// 数组类字段（product.imageAlts / product_attribute.values）逐元素取词：
// 元素是独立文本，各自有独立译文（同一句文本在别处已翻译时可直接复用）。
var translatableFields = map[string]map[string]bool{
	EntityTypeProduct: {
		"name":        true,
		"subtitle":    true,
		"description": true,
		"imageAlts":   true,
	},
	EntityTypeCategory: {
		"name":        true,
		"description": true,
		"seoTitle":    true,
	},
	EntityTypeBrand: {
		"name":        true,
		"description": true,
		"seoTitle":    true,
	},
	EntityTypeTag: {
		"name": true,
	},
	EntityTypeAttribute: {
		"name":   true,
		"values": true,
	},
}

// EntityTypes 本模块注册的实体类型（装配期逐个注册，顺序稳定）。
func EntityTypes() []string {
	return []string{EntityTypeProduct, EntityTypeCategory, EntityTypeBrand, EntityTypeTag, EntityTypeAttribute}
}

// IsValidType 实体类型是否属于商品域。
func IsValidType(t string) bool {
	_, ok := fieldWhitelist[t]
	return ok
}

// FieldWhitelist 返回字段白名单的只读拷贝（调用方不得篡改唯一来源）。
func FieldWhitelist(entityType string) []string {
	src, ok := fieldWhitelist[entityType]
	if !ok {
		return nil
	}
	out := make([]string, len(src))
	copy(out, src)
	return out
}

// SortedFieldWhitelist 白名单的字典序拷贝（集合源元数据等需要确定性顺序的场景）。
func SortedFieldWhitelist(entityType string) []string {
	out := FieldWhitelist(entityType)
	sort.Strings(out)
	return out
}

// IsValidField 字段是否在该类型白名单内。
func IsValidField(entityType, field string) bool {
	for _, f := range fieldWhitelist[entityType] {
		if f == field {
			return true
		}
	}
	return false
}

// IsTranslatableField 字段是否参与内容翻译。
func IsTranslatableField(entityType, field string) bool {
	return translatableFields[entityType][field]
}

// TranslatableFields 返回某实体类型的可翻译字段集合（只读拷贝；未知类型返回 nil）。
//
// 工作台（后台翻译页）与构建期解析器共用本函数 —— 不另写第二份白名单。
func TranslatableFields(entityType string) []string {
	src := translatableFields[entityType]
	if len(src) == 0 {
		return nil
	}
	out := make([]string, 0, len(src))
	for _, f := range fieldWhitelist[entityType] {
		if src[f] {
			out = append(out, f)
		}
	}
	return out
}

// EntityTypeLabel 实体类型的中文展示名（后台翻译页分组标题）。
func EntityTypeLabel(entityType string) string {
	switch entityType {
	case EntityTypeCategory:
		return "商品分类"
	case EntityTypeBrand:
		return "商品品牌"
	case EntityTypeTag:
		return "商品标签"
	case EntityTypeAttribute:
		return "商品属性"
	default:
		return "商品"
	}
}

// FieldLabel 可翻译字段的中文展示名（后台翻译页字段列；未知字段原样返回）。
func FieldLabel(entityType, field string) string {
	switch entityType + "." + field {
	case EntityTypeProduct + ".name":
		return "商品名"
	case EntityTypeProduct + ".subtitle":
		return "副标题"
	case EntityTypeProduct + ".description":
		return "描述"
	case EntityTypeProduct + ".imageAlts":
		return "图片 alt"
	case EntityTypeCategory + ".name":
		return "分类名"
	case EntityTypeCategory + ".description":
		return "分类描述"
	case EntityTypeCategory + ".seoTitle":
		return "分类 SEO 标题"
	case EntityTypeBrand + ".name":
		return "品牌名"
	case EntityTypeBrand + ".description":
		return "品牌描述"
	case EntityTypeBrand + ".seoTitle":
		return "品牌 SEO 标题"
	case EntityTypeTag + ".name":
		return "标签名"
	case EntityTypeAttribute + ".name":
		return "属性组名"
	case EntityTypeAttribute + ".values":
		return "属性值展示文本"
	default:
		return field
	}
}

// FieldContext 拼装内容译文的语境界定 "{实体类型}.{字段名}"（docs/06-D §7.5）。
//
// 与 core.ContentContext 同形，但它在这里由**具体实体类型 + 字段名**拼出，
// 不经过注册表查找 —— 解析器与工作台在同一处拼装，避免两个来源各拼各的。
// 任一段为空返回空串（空语境不参与取词，取词器直接回退原文）。
func FieldContext(entityType, field string) string {
	return core.ContentContextFor(entityType, field)
}

// —— 集合源（issue #9）——

// CollectionSourceProduct 商品集合源标识（集合类组件 collectionSource 属性的取值）。
//
// 沿用既有集合源命名空间 "content:{entityType}"：内容类型收敛为 article 后
// （issue #4），product 已不在 contents 表，该源由本模块实现 —— 集合源的标识
// 是组件侧的契约面，不随实现模块迁移而改名（现有工作台选项与文档历史值都不变）。
const CollectionSourceProduct = "content:product"

// CollectionLabel 集合源展示名（工作台集合源下拉）。
const CollectionLabel = "商品列表"

// 集合源过滤维度的键（issue #21）。
//
// 键名与集合项字段风格一致（驼峰）：工作台与文档里看到的是同一套名字。
const (
	// CollectionFilterStatus 上下架状态（枚举维度）。
	CollectionFilterStatus = "status"
	// CollectionFilterCategoryID 分类 id（商品挂载的分类中命中任意一个即算命中）。
	CollectionFilterCategoryID = "categoryId"
	// CollectionFilterBrandID 品牌 id。
	CollectionFilterBrandID = "brandId"
	// CollectionFilterTagID 标签 id（手工挂载与自动命中的都在 tag_ids 里）。
	CollectionFilterTagID = "tagId"

	// CollectionFilterOption 属性值维度的**前缀键**（issue #25）：真实维度写成
	// `option.<属性组key>=<属性值key>`（如 option.color=red），多个属性彼此 AND。
	//
	// 值落在**变体**上（product_variants.option_values 的 JSONB），不是商品行上的列 ——
	// 所以它不能像 categoryId 那样一个等值条件搞定，得逐属性下推 EXISTS 子查询。
	CollectionFilterOption = "option"

	// CollectionFilterOptionPrefix 前缀维度键的完整前缀（拼维度键用）。
	CollectionFilterOptionPrefix = CollectionFilterOption + "."
)

// collectionFilters 集合源允许的过滤维度（顺序即工作台下拉顺序）。
//
// 全部是**等值**维度且彼此 AND：解析期逐个下推到 SQL，白名单外的维度直接报错
// （不变量 4：不接受任意过滤表达式，只接受声明过的等值维度）。
//
// status 带 Enum（取值只有三个，工作台渲染成下拉）；三条 id 维度 Enum 为空
// （取值是工程内任意的分类 / 品牌 / 标签 id），服务层逐个做 uuid 形状校验。
var collectionFilters = []core.CollectionFilter{
	{Key: CollectionFilterStatus, Enum: []string{productenums.StatusDraft, productenums.StatusPublished, productenums.StatusArchived}},
	{Key: CollectionFilterCategoryID},
	{Key: CollectionFilterBrandID},
	{Key: CollectionFilterTagID},
	// 属性值维度（issue #25）：前缀维度，真实键是 `option.<属性组key>=<属性值key>`。
	// 属性组由用户自己建（数据驱动），维度键没法穷举，所以用前缀命名空间 + 服务端校验子键。
	{Key: CollectionFilterOption, Prefix: true},
}

// collectionOrderKeys 集合源允许的排序键白名单（顺序即默认排序优先级）。
//
// 字段名用驼峰（集合项字段风格，与 slug / defaultImage 一致）：
// sort（排序号）→ createdAt（创建时间），同值再按 id 兜底，保证产物确定性。
var collectionOrderKeys = []string{"sort", "createdAt"}

// CollectionFilters 过滤维度白名单的只读拷贝（调用方不得篡改唯一来源）。
func CollectionFilters() []core.CollectionFilter {
	out := make([]core.CollectionFilter, 0, len(collectionFilters))
	for _, f := range collectionFilters {
		copied := core.CollectionFilter{Key: f.Key, Prefix: f.Prefix}
		if len(f.Enum) > 0 {
			copied.Enum = make([]string, len(f.Enum))
			copy(copied.Enum, f.Enum)
		}
		out = append(out, copied)
	}
	return out
}

// IsCollectionFilterKey 过滤维度是否在集合源白名单内。
//
// 前缀维度按 "<Key>.<子键>" 判定（子键非空即形状合法）；子键的**语义**合法性
// （属性组 key 是否属于该商品）由解析器与服务层各自负责，白名单只管形状。
func IsCollectionFilterKey(key string) bool {
	for _, f := range collectionFilters {
		if f.Prefix {
			if sub, ok := strings.CutPrefix(key, f.Key+"."); ok && sub != "" {
				return true
			}
			continue
		}
		if f.Key == key {
			return true
		}
	}
	return false
}

// CollectionOrderBy 排序键白名单的只读拷贝（顺序即默认排序优先级）。
func CollectionOrderBy() []string {
	out := make([]string, len(collectionOrderKeys))
	copy(out, collectionOrderKeys)
	return out
}
