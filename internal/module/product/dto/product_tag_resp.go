// product_tag_resp.go — 商品标签出参（issue #11，service 返回）。
package productdto

import "encoding/json"

// TagResp 标签。
//
// RuleLabel 是规则参数翻成的人类可读描述（如「上架 30 天内」），由服务端算好 ——
// 后台不需要自己解释规则参数，也就不会出现第二套规则语义。
// Products 只在详情接口（GetTag）里带上（验收 4）；列表接口只给 ProductCount。
type TagResp struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	// Kind manual / rule。
	Kind string `json:"kind"`
	// RuleType 内置规则类型（手工标签为空串）。
	RuleType string `json:"ruleType"`
	// RuleParams 规则参数原样回读（归一化后的形态）。
	RuleParams json.RawMessage `json:"ruleParams"`
	// RuleLabel 规则的可读描述（手工标签为空串）。
	RuleLabel string `json:"ruleLabel"`
	// RecalcAt 最近一次按规则重算的时间（手工标签为空串）。
	RecalcAt string `json:"recalcAt"`
	// ProductCount 当前命中/挂载的商品数。
	ProductCount int `json:"productCount"`
	Sort         int `json:"sort"`
	// Products 命中商品（仅详情接口）。
	Products  []*TagProductResp `json:"products,omitempty"`
	CreatedAt string            `json:"createdAt"`
	UpdatedAt string            `json:"updatedAt"`
}

// TagProductResp 标签命中的商品（验收 4 的展示单元）。
type TagProductResp struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Slug   string `json:"slug"`
	Status string `json:"status"`
}

// TagProductsPageResp 命中商品的一页（审计 PERF-02）。
//
// 分页元数据与商品行分开：后台展开区要能显示「共 N 条，第 X-Y 条」，而商品行本身
// 只占一小段 —— 只回行不回元数据的话，翻页链接与总数就得由前端另算一次。
type TagProductsPageResp struct {
	TagID   string `json:"tagId"`
	TagName string `json:"tagName"`
	// Total 该标签命中的商品总数（不受本页条数影响）。
	Total int `json:"total"`
	// Page 当前页（从 1 开始）；TotalPage 至少为 1（空列表也算一页）。
	Page      int `json:"page"`
	PageSize  int `json:"pageSize"`
	TotalPage int `json:"totalPage"`
	// Items 本页的商品行。
	Items []*TagProductResp `json:"items"`
}

// TagRuleTypeResp 内置规则类型（后台规则下拉与参数说明的唯一来源）。
type TagRuleTypeResp struct {
	Type string `json:"type"`
	Name string `json:"name"`
	// Params 参数说明（人类可读，如「days：1~365 的整数」）。
	Params string `json:"params"`
}

// RecalcTagsResp 一次重算的结果。
type RecalcTagsResp struct {
	// Recalculated 本次实际重算的自动标签数（手工标签不计入）。
	Recalculated int `json:"recalculated"`
	// Products 本次重算写出的商品归属总条数（各标签命中数之和）。
	Products int `json:"products"`
	// Tags 重算后的标签（按工程内排序）。
	Tags []*TagResp `json:"tags"`
}
