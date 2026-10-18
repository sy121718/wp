// product_tag_resp.go — 商品标签出参（issue #11，service 返回）。
package productdto

import json "go_wp/pkg/jsonx"

// TagResp 标签。
//
// 规则的**措辞**不在 Go 里：这里只回 RuleType 与 RuleParams（取值），
// 「上架 30 天内」这类句子由模板按规则类型取词拼出。
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
	// RuleValues 规则参数的取值（数字与「有没有」），不做任何措辞。
	RuleValues RuleValuesResp `json:"ruleValues"`
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
// 为什么在 dto 里而不是控制器里拼 gin.H：行里的每一项都是**数据** —— 类型、规则、
// 重算时间（原样时间串）。类型的中文名、「未重算过」这类**文案**一律在
// product_tags.html 里按数据分支取词，dto 不带任何成品串。
//
// EditForm 是抽屉片段的数据（product_tag_form.html 的数据根）。它必须留在
// **渲染期**装配：里面要 csrf_token（请求态）与 t/tf（由 text.Inject 注入），
// 而 Jet 在 struct 上不支持 `.["t"]`，所以那一层只能是 map。
type TagRowResp struct {
	ID   string
	Name string
	Slug string
	Kind string
	// IsRule/HasRuleParams 规则型标签（决定表单里规则字段显不显示）。
	IsRule        bool
	HasRuleParams bool
	// KindRule/KindManual 两个类型常量的镜像：模板里 select 的选中判断用。
	KindRule   string
	KindManual string
	RuleType   string
	// RuleValues 规则参数的取值（数字与「有没有」），措辞在 tag_rule_text.html。
	RuleValues RuleValuesResp
	// RecalcAt 上次重算的原样时间串（未重算过时为空串）。
	RecalcAt string
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
//
// 规则的展示名与参数说明都是**文案**，按 Type 写在模板里（product_tags.html 的
// 规则表、product_tag_form.html 的下拉），这里只回类型与它接受的参数键。
type TagRuleTypeResp struct {
	Type string `json:"type"`
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
