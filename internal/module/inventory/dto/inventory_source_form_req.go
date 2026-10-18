package inventorydto

import (
	"strconv"
	"strings"

	inventoryenums "go_wp/internal/module/inventory/enums"
	json "go_wp/pkg/jsonx"
)

// SourceFormReq 货源新建 / 编辑表单（编辑屏多 id 与 status）。
//
// 三处语义刻意与旧形状一致，改动会静默改变账目含义：
//   - 关联方空串 = 未指定（新建按类型默认、编辑不改）；
//   - 结算价空串 = 没有结算价（编辑屏是最终状态：留空即清空，不是「本次不改」）；
//   - 对接配置空串 = 未填（service 落成 {}）。
type SourceFormReq struct {
	ProjectID    string `form:"projectId"`
	ID           string `form:"id"`
	Code         string `form:"code"`
	Name         string `form:"name"`
	Type         string `form:"type"`
	Status       string `form:"status"`
	RelatedParty string `form:"relatedParty"`
	SettlePrice  string `form:"settlePrice"`
	Sort         string `form:"sort"`
	Config       string `form:"config"`
}

// Related 关联方下拉：空串 / 非法值 = 未指定（nil）。
func (r *SourceFormReq) Related() *bool {
	switch strings.ToLower(strings.TrimSpace(r.RelatedParty)) {
	case "true", "1":
		yes := true
		return &yes
	case "false", "0":
		no := false
		return &no
	default:
		return nil
	}
}

// Settle 结算价：空串 = 无值（nil），非法数字 = 参数错误（不静默清零）。
func (r *SourceFormReq) Settle() (*float64, error) {
	raw := strings.TrimSpace(r.SettlePrice)
	if raw == "" {
		return nil, nil
	}
	parsed, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return nil, inventoryenums.Err(inventoryenums.ErrSourceSettleInvalid)
	}
	return &parsed, nil
}

// ConfigValue 对接配置原文（空串 = 未填，返回 nil 让 service 落成 {}）。
func (r *SourceFormReq) ConfigValue() json.RawMessage {
	raw := strings.TrimSpace(r.Config)
	if raw == "" {
		return nil
	}
	return json.RawMessage(raw)
}

// CreateReq 转成 service 的新建请求。
func (r *SourceFormReq) CreateReq() (*CreateSourceReq, error) {
	price, err := r.Settle()
	if err != nil {
		return nil, err
	}
	return &CreateSourceReq{
		ProjectID:    strings.TrimSpace(r.ProjectID),
		Code:         strings.TrimSpace(r.Code),
		Name:         strings.TrimSpace(r.Name),
		Type:         strings.TrimSpace(r.Type),
		Status:       strings.TrimSpace(r.Status),
		RelatedParty: r.Related(),
		Config:       r.ConfigValue(),
		Sort:         formInt(r.Sort, 0),
		SettlePrice:  price,
	}, nil
}

// UpdateReq 转成 service 的修改请求（表单即最终状态：结算价留空 = 清空）。
func (r *SourceFormReq) UpdateReq() (*UpdateSourceReq, error) {
	code, name := strings.TrimSpace(r.Code), strings.TrimSpace(r.Name)
	sourceType, status := strings.TrimSpace(r.Type), strings.TrimSpace(r.Status)
	sort := formInt(r.Sort, 0)
	req := &UpdateSourceReq{
		ID:           strings.TrimSpace(r.ID),
		Code:         &code,
		Name:         &name,
		Type:         &sourceType,
		Status:       &status,
		Sort:         &sort,
		RelatedParty: r.Related(),
		Config:       r.ConfigValue(),
	}
	price, err := r.Settle()
	if err != nil {
		return nil, err
	}
	if price == nil {
		req.ClearSettlePrice = true
	} else {
		req.SettlePrice = price
	}
	return req, nil
}

// SourceDeleteFormReq 删除货源。
type SourceDeleteFormReq struct {
	ID string `form:"id"`
}

// SourceBulkFormReq 批量删除（勾选 id 以同名键重复提交）。
type SourceBulkFormReq struct {
	ProjectID string   `form:"projectId"`
	IDs       []string `form:"ids"`
}
