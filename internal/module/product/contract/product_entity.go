// product_entity.go — 商品实体类型与字段白名单（issue #6）。
//
// 本模块是自身实体类型字段白名单的唯一来源（与 content 模块的 contract 白名单同构）：
// 装配期把 product 注册进实体类型注册表（core.EntitySourceRegistry），内容模板与发布
// 实例据此做类型与字段校验，构建期商品解析器据此拒绝白名单之外的绑定（不变量 4）。
//
// 白名单只放「详情页可展示」的字段：价格全部由变体派生（商品主体不存价格，
// issue #5 已定语义），因此对外暴露的是派生值而不是原列。
package productcontract

import "sort"

// EntityTypeProduct 商品实体类型标识。
const EntityTypeProduct = "product"

// fieldWhitelist 商品详情可绑定字段白名单（顺序即工作台下拉顺序）。
//
// 语义：
//   - name / subtitle / description —— 作者填写的文本，参与内容翻译；
//   - slug / sku / unit —— 标识与单位，纯文本但不参与翻译；
//   - images / defaultImage —— 图集（JSON 数组）与主图（URL）；
//   - price / comparePrice / priceRange / minPrice / maxPrice —— 由变体派生的
//     只读价格值，纯数字与符号，永不参与翻译。
var fieldWhitelist = []string{
	"name", "subtitle", "description",
	"slug", "sku", "unit",
	"images", "defaultImage",
	"price", "comparePrice", "priceRange", "minPrice", "maxPrice",
}

// translatableFields 参与内容翻译（sys_translation）的字段。
//
// 取词语境固定为 "product.<字段名>"（docs/06-D §7.5；spec #2「商品字段的取值路径
// 必须按 实体.字段名 的语境界定接入翻译」）。slug / SKU / 价格 / 库存不在此列。
var translatableFields = map[string]bool{
	"name":        true,
	"subtitle":    true,
	"description": true,
}

// EntityTypes 本模块注册的实体类型（当前只有商品）。
func EntityTypes() []string { return []string{EntityTypeProduct} }

// IsValidType 商品类型是否合法。
func IsValidType(t string) bool { return t == EntityTypeProduct }

// FieldWhitelist 返回字段白名单的只读拷贝（调用方不得篡改唯一来源）。
func FieldWhitelist(entityType string) []string {
	if !IsValidType(entityType) {
		return nil
	}
	out := make([]string, len(fieldWhitelist))
	copy(out, fieldWhitelist)
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
	if !IsValidType(entityType) {
		return false
	}
	for _, f := range fieldWhitelist {
		if f == field {
			return true
		}
	}
	return false
}

// IsTranslatableField 字段是否参与内容翻译。
func IsTranslatableField(entityType, field string) bool {
	return IsValidType(entityType) && translatableFields[field]
}
