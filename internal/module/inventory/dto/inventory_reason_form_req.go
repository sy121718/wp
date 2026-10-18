package inventorydto

import (
	"net/url"
	"strings"

	inventoryenums "go_wp/internal/module/inventory/enums"
)

// ReasonFormReq 变动原因的新建 / 编辑表单。
//
// 编辑屏要区分「这个字段没提交」与「提交了空值」：内置原因只允许改状态与排序，
// 名称字段压根不出现在表单里 —— 两者都长成空串，混起来就会把「没改」当成「改成空」。
// 反射绑定看不出这个差别，所以本类型实现 FormBinder，把「出现过的键」记下来。
type ReasonFormReq struct {
	ProjectID string `form:"projectId"`
	ID        string `form:"id"`
	Builtin   string `form:"builtin"`
	Code      string `form:"code"`
	Name      string `form:"name"`
	Direction string `form:"direction"`
	Status    string `form:"status"`
	Sort      string `form:"sort"`

	submitted map[string]bool
}

// reasonFormKeys 需要判「是否提交」的键（其余字段只有一种语义，不必跟踪）。
var reasonFormKeys = []string{"code", "name", "direction", "status", "sort"}

// BindForm 记录本请求实际提交了哪些键。
func (r *ReasonFormReq) BindForm(values url.Values) error {
	r.submitted = make(map[string]bool, len(reasonFormKeys))
	for _, key := range reasonFormKeys {
		_, r.submitted[key] = values[key]
	}
	return nil
}

// Submitted 该键是否出现在本次提交里（未实现 BindForm 时一律 false）。
func (r *ReasonFormReq) Submitted(key string) bool { return r.submitted[key] }

// CreateReq 转成 service 的新建请求。
func (r *ReasonFormReq) CreateReq() *CreateReasonReq {
	return &CreateReasonReq{
		ProjectID: strings.TrimSpace(r.ProjectID),
		Code:      strings.TrimSpace(r.Code),
		Name:      strings.TrimSpace(r.Name),
		Direction: strings.TrimSpace(r.Direction),
		Sort:      formInt(r.Sort, 0),
	}
}

// UpdateReq 转成 service 的修改请求（只改本次提交、且非空的字段：nil = 本次不改）。
func (r *ReasonFormReq) UpdateReq() *UpdateReasonReq {
	req := &UpdateReasonReq{ProjectID: strings.TrimSpace(r.ProjectID), ID: strings.TrimSpace(r.ID)}
	if name := strings.TrimSpace(r.Name); r.Submitted("name") {
		req.Name = &name
	}
	if status := strings.TrimSpace(r.Status); status != "" {
		req.Status = &status
	}
	if sortRaw := strings.TrimSpace(r.Sort); r.Submitted("sort") && sortRaw != "" {
		sort := formInt(sortRaw, 0)
		req.Sort = &sort
	}
	return req
}

// ReasonBulkFormReq 批量启停（勾选 id 以同名键重复提交，状态来自按钮上的隐藏域）。
type ReasonBulkFormReq struct {
	ProjectID string   `form:"projectId"`
	Status    string   `form:"status"`
	IDs       []string `form:"ids"`
}

// ValidStatus 批量启停只接受启用 / 停用两个值（其余一律拒绝，不静默按启用处理）。
func (r *ReasonBulkFormReq) ValidStatus() bool {
	status := strings.TrimSpace(r.Status)
	return status == inventoryenums.StatusActive || status == inventoryenums.StatusDisabled
}

// StatusValue 归一后的目标状态。
func (r *ReasonBulkFormReq) StatusValue() string { return strings.TrimSpace(r.Status) }
