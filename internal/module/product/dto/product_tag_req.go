// product_tag_req.go — 商品标签入参（issue #11，inbound 绑定用）。
//
// 手工标签与自动标签共用一套入参：kind 决定形态（manual 不带规则；rule 必须带内置
// 规则类型 + 参数）。规则参数走 JSONB 对象，键集合由规则类型自己定义，服务端严格校验
// （未知键、类型不符、越界一律拒绝 —— 不接受自由表达式）。
package productdto

import "encoding/json"

// CreateTagReq 新建标签。
type CreateTagReq struct {
	ProjectID string `json:"projectId"`
	Name      string `json:"name" binding:"required"`
	Slug      string `json:"slug"`
	// Kind 留空按 manual 处理（最保守：不会因为一条规则自动改动商品归属）。
	Kind string `json:"kind"`
	// RuleType 内置规则类型（kind=rule 时必填；manual 时给了即拒绝）。
	RuleType string `json:"ruleType"`
	// RuleParams 规则参数对象（如 {"days":30}）；kind=manual 时必须为空。
	RuleParams json.RawMessage `json:"ruleParams"`
	Sort       int             `json:"sort"`
}

// UpdateTagReq 修改标签（可选字段为 nil 表示不变）。
//
// 类型切换语义：
//   - manual → rule：必须给出合法的规则类型与参数，改完立刻按新规则重算一次；
//   - rule → manual：规则定义整体清掉，当前命中结果**保留为手工归属**（管理员可以再手工调整）。
type UpdateTagReq struct {
	ID         string          `json:"id" binding:"required"`
	Name       *string         `json:"name"`
	Slug       *string         `json:"slug"`
	Kind       *string         `json:"kind"`
	RuleType   *string         `json:"ruleType"`
	RuleParams json.RawMessage `json:"ruleParams"`
	Sort       *int            `json:"sort"`
}

// GetTagReq 按 ID 查询标签（返回规则说明与命中商品）。
type GetTagReq struct {
	ID string `form:"id" binding:"required"`
}

// ListTagReq 标签列表（按工程过滤；kind 为空表示手工与自动都要）。
type ListTagReq struct {
	ProjectID string `form:"projectId"`
	Kind      string `form:"kind"`
	Keyword   string `form:"keyword"`
}

// ListTagProductsReq 查某标签命中的商品（验收 4：后台可查看某标签命中哪些商品）。
type ListTagProductsReq struct {
	TagID string `form:"id" binding:"required"`
	// Limit 展示上限（<=0 用服务端默认上限）。
	Limit int `form:"limit"`
}

// DeleteTagReq 删除标签（连同它在商品上的引用一起解绑）。
type DeleteTagReq struct {
	ID string `json:"id" binding:"required"`
}

// RecalcTagsReq 手动触发重算（重算时机之一）。
//
// TagID 为空表示重算该工程下全部自动标签；填了 TagID 时 ProjectID 可省略（按标签自己的工程）。
type RecalcTagsReq struct {
	ProjectID string `json:"projectId"`
	TagID     string `json:"tagId"`
}
