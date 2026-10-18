// product_tag_resp.go — 商品标签出参（issue #11，service 返回）。
package productdto

import json "go_wp/pkg/jsonx"

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

// TagRowResp 标签管理页的一行（模板直接渲染的成品行）。
//
// 为什么在 dto 里而不是控制器里拼 gin.H：行里的每一项都是**数据** ——
// 文案只以「key + 中文兜底」的事实形态出现（KindLabelKey/KindLabelFallback、
// RecalcLabelKey/RecalcLabelFallback），取词在 product_tags.html 的 tr 里做。
// 映射表放在这里，控制器就只剩「读一页 → 填行 → 渲染」。
//
// EditForm 是抽屉片段的数据（product_tag_form.html 的数据根）。它必须留在
// **渲染期**装配：里面要 csrf_token（请求态）与 t/tf（由 adminui.PrepareBare 注入），
// 而 Jet 在 struct 上不支持 `.["t"]`，所以那一层只能是 map。
type TagRowResp struct {
	ID   string
	Name string
	Slug string
	Kind string
	// KindLabelKey/KindLabelFallback 类型标签的文案事实（productenums.TagKindLabel）。
	KindLabelKey      string
	KindLabelFallback string
	// KindTone 徽标分档（rule → info，manual → mute）。
	KindTone string
	// IsRule/HasRuleParams 规则型标签（决定表单里规则字段显不显示）。
	IsRule        bool
	HasRuleParams bool
	// KindRule/KindManual 两个类型常量的镜像：模板里 select 的选中判断用。
	KindRule   string
	KindManual string
	RuleType   string
	RuleLabel  string
	// RecalcAt 上次重算时间的数据串；RecalcLabelKey/RecalcLabelFallback 是「没重算过」的文案事实。
	RecalcAt            string
	RecalcLabelKey      string
	RecalcLabelFallback string
	// RuleDays/RuleMinPrice/RuleMaxPrice 规则参数的回填文本。
	RuleDays     string
	RuleMinPrice string
	RuleMaxPrice string
	ProductCount int
	Sort         int
	// EditForm 行内编辑抽屉的片段数据（渲染期装配，见类型注释）。
	EditForm map[string]any
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
	// ParamKeys 本规则接受的参数键（如 days / minPrice,maxPrice；无参规则为空）。
	//
	// 它是后台表单**组装 ruleParams 的唯一依据**：切换规则类型时要发的键跟着变，而服务端
	// 用白名单严格拒多余键（rejectUnknownRuleKeys）—— 前端写死一份映射就会与规则表漂移，
	// 症状是「切了类型保存就报参数不合法」。
	ParamKeys []string `json:"paramKeys"`
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
