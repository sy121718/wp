// product_tag_req.go — 商品标签入参（issue #11，inbound 绑定用）。
//
// 手工标签与自动标签共用一套入参：kind 决定形态（manual 不带规则；rule 必须带内置
// 规则类型 + 参数）。规则参数走 JSONB 对象，键集合由规则类型自己定义，服务端严格校验
// （未知键、类型不符、越界一律拒绝 —— 不接受自由表达式）。
package productdto

import (
	"net/url"
	"strings"

	json "go_wp/pkg/jsonx"
)

// CreateTagReq 新建标签。
//
// form tag 与 json tag 同名：同一端点既服务 JSON（MCP/AI）也服务 htmx 表单。
// RuleParams 是 json.RawMessage，反射绑定在**表单体**上覆盖不到（encoding/json 的
// UnmarshalJSON 期待一个 JSON 值，而表单给它的是字符串）—— 由 BindForm 接管。
type CreateTagReq struct {
	ProjectID string `json:"projectId" form:"projectId"`
	Name      string `json:"name" form:"name" binding:"required"`
	Slug      string `json:"slug" form:"slug"`
	// Kind 留空按 manual 处理（最保守：不会因为一条规则自动改动商品归属）。
	Kind string `json:"kind" form:"kind"`
	// RuleType 内置规则类型（kind=rule 时必填；manual 时给了即拒绝）。
	RuleType string `json:"ruleType" form:"ruleType"`
	// RuleParams 规则参数对象（如 {"days":30}）；kind=manual 时必须为空。
	RuleParams json.RawMessage `json:"ruleParams" form:"-"`
	Sort       int             `json:"sort" form:"sort"`
}

// BindForm 表单体里 RuleParams 是一个 JSON 字符串（htmx_form_body.js 用 JSON.stringify 生成）。
//
// 为什么不在反射绑定里解决：json.RawMessage 的 UnmarshalJSON 期待 JSON 值，表单给的是
// 字符串 —— 实测 ShouldBind 直接报错，整条请求 400。这里把它按 JSON 解析一次。
//
// 空串 / 未提交 → 保持 nil（= 本次没有规则参数，与 JSON 省略该字段同义）。
func (r *CreateTagReq) BindForm(vals url.Values) error {
	return bindRawJSONField(vals, "ruleParams", &r.RuleParams)
}

// UpdateTagReq 修改标签（可选字段为 nil 表示不变）。
//
// 类型切换语义：
//   - manual → rule：必须给出合法的规则类型与参数，改完立刻按新规则重算一次；
//   - rule → manual：规则定义整体清掉，当前命中结果**保留为手工归属**（管理员可以再手工调整）。
type UpdateTagReq struct {
	ID         string          `json:"id" form:"id" binding:"required"`
	ProjectID  string          `json:"projectId" form:"projectId"`
	Name       *string         `json:"name" form:"name"`
	Slug       *string         `json:"slug" form:"slug"`
	Kind       *string         `json:"kind" form:"kind"`
	RuleType   *string         `json:"ruleType" form:"ruleType"`
	RuleParams json.RawMessage `json:"ruleParams" form:"-"`
	Sort       *int            `json:"sort" form:"sort"`
}

// BindForm 见 CreateTagReq.BindForm（同一个 json.RawMessage 问题）。
func (r *UpdateTagReq) BindForm(vals url.Values) error {
	return bindRawJSONField(vals, "ruleParams", &r.RuleParams)
}

// GetTagReq 按 ID 查询标签（返回规则说明与命中商品）。
type GetTagReq struct {
	ID        string `form:"id" binding:"required"`
	ProjectID string `form:"projectId"`
}

// ListTagReq 标签列表（按工程过滤；kind 为空表示手工与自动都要）。
//
// Page / Size 是**新增的可选分页字段**：两个都是零值时语义与加字段前一致 —— 取全部
// （重算回执的标签清单、商品页的标签勾选项都要全量）。后台标签页显式给出两者，
// 分页下推到 model 的 LIMIT/OFFSET（见 service 的 optionalPaging）。
type ListTagReq struct {
	ProjectID string `form:"projectId"`
	Kind      string `form:"kind"`
	Keyword   string `form:"keyword"`
	// Page 从 1 开始（<=0 且 Size 也为 0 时不传分页）。
	Page int `form:"page"`
	// Size 每页条数（<=0 时用服务端默认；超过上限按上限截断，不由调用方决定）。
	Size int `form:"size"`
}

// ListTagProductsReq 查某标签命中的商品（验收 4：后台可查看某标签命中哪些商品）。
type ListTagProductsReq struct {
	TagID     string `form:"id" binding:"required"`
	ProjectID string `form:"projectId"`
	// Limit 展示上限（<=0 用服务端默认上限）。
	Limit int `form:"limit"`
}

// ListTagProductsPageReq 按页取某标签命中的商品（审计 PERF-02）。
//
// 与 ListTagProductsReq 的分工：那个是接口形态（只要一个上限），这个是后台页形态 ——
// 标签页首屏不再内联命中商品，展开某个标签时按页拉一段，页面 SQL 条数与标签数无关。
type ListTagProductsPageReq struct {
	TagID     string `form:"id"`
	ProjectID string `form:"project"`
	// Page 从 1 开始（<=0 归一为 1）。
	Page int `form:"page"`
	// Size 每页条数（<=0 用服务端默认；超过上限按上限截断，不由调用方决定）。
	Size int `form:"size"`
}

// BindQuery 兼容两套工程键名：标签页的片段链接给 project，接口调用方给 projectId。
func (r *ListTagProductsPageReq) BindQuery(q url.Values) error {
	if r.ProjectID == "" {
		r.ProjectID = strings.TrimSpace(q.Get("projectId"))
	}
	return nil
}

// DeleteTagReq 删除标签（连同它在商品上的引用一起解绑）。
type DeleteTagReq struct {
	ID string `json:"id" form:"id" binding:"required"`
	ProjectID string `json:"projectId" form:"projectId"`
}

// RecalcTagsReq 手动触发重算（重算时机之一）。
//
// TagID 为空表示重算该工程下全部自动标签；填了 TagID 时 ProjectID 可省略（按标签自己的工程）。
type RecalcTagsReq struct {
	ProjectID string `json:"projectId" form:"projectId"`
	TagID string `json:"tagId" form:"tagId"`
}

// bindRawJSONField 把表单里一个 JSON 字符串字段解析成 json.RawMessage。
//
// 存在的理由：json.RawMessage 实现 UnmarshalJSON，期待的是**一个 JSON 值**；而表单给的是
// 字符串（htmx_form_body.js 用 JSON.stringify 生成的 `{"days":30}`）。反射绑定拿到字符串
// 会整条失败（实测 err != nil），所以这类字段在 DTO 的 form tag 上标 `form:"-"`，
// 由 BindForm 自己接管。
//
// 空串 / 未提交 → 保持 nil（= 本次没有这个参数，与 JSON 省略字段同义）。
// 非空但不是合法 JSON → 返回 FieldValueError，由出口按 400 报参数错误
//（不能静默吞掉：那会让「填了规则参数却没生效」看起来像保存成功）。
func bindRawJSONField(vals url.Values, name string, dst *json.RawMessage) error {
	raw := strings.TrimSpace(vals.Get(name))
	if raw == "" {
		return nil
	}
	if !json.Valid([]byte(raw)) {
		return &FieldValueError{Field: name, Kind: FieldKindJSON}
	}
	*dst = json.RawMessage(raw)
	return nil
}
