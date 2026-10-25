package inventorydto

import (
	"strconv"
	"strings"

	inventoryenums "go_wp/internal/module/inventory/enums"
)

// 页面原生表单的传输形状（form tag 是契约，不是可选项）。
//
// 为什么不复用接口面的 *Req：
//   - 接口面 DTO 带 binding:"required"，缺字段直接 400 JSON；页面表单失败必须渲染成
//     「这一页 / 这个抽屉」（保住用户输入），所以页面面不设 required，由 service 判业务错误；
//   - 表单送来的数字、布尔都是字符串（sort 可能留空、复选框只送 "on"），转换留在本层，
//     控制器不再 parseIntOr、不再写 != ""。口径与页面既有行为一致：空串或不合法取兜底值。

// formBool 复选框语义：键存在且有非空值即为真（"on" / "1" / "true" 都算）。
func formBool(v string) bool { return strings.TrimSpace(v) != "" }

// formInt 数字语义：能解析就用，否则兜底（表单留空 = 用默认值）。
func formInt(v string, fallback int) int {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return fallback
	}
	return n
}

// DefaultDeductPriority 出库扣减优先级的缺省值（迁移 623 的列默认值，同口径）。
//
// 100 而不是 0：0 会让「没设过优先级的仓」排在任何显式正数之前，等于把「没配置」
// 当成「最高优先级」。页面上留空 = 用这个值。
const DefaultDeductPriority = 100

// WarehouseFormReq 仓库新建 / 编辑表单（两屏字段合并：编辑多 id 与 status）。
type WarehouseFormReq struct {
	ProjectID string `form:"projectId"`
	ID        string `form:"id"`
	Code      string `form:"code"`
	Name      string `form:"name"`
	Sort      string `form:"sort"`
	Status    string `form:"status"`
	Type      string `form:"type"`
	IsDefault string `form:"isDefault"`
	// DeductPriority 出库扣减优先级（迁移 623）：值小先扣；留空按 100（= 未设置）。
	DeductPriority string `form:"deductPriority"`

	Provider        string `form:"provider"`
	ExternalCode    string `form:"externalCode"`
	Address         string `form:"address"`
	Contact         string `form:"contact"`
	AllowsShipping  string `form:"allowsShipping"`
	APICredential   string `form:"apiCredential"`
	ClearCredential string `form:"clearCredential"`
	SecretRef       string `form:"secretRef"`
}

// ThirdParty 第三方仓对接配置（非第三方仓给 nil：那份配置会由 service 判成不合法）。
//
// apiCredential 收的是**明文**，只在这一次请求里存在：service 立刻加密落库、回显永远是掩码；
// 留空 / 传回掩码都表示「不改凭据」。
func (r *WarehouseFormReq) ThirdParty(typ string) *WarehouseThirdPartyReq {
	if typ != inventoryenums.WarehouseTypeThirdParty {
		return nil
	}
	return &WarehouseThirdPartyReq{
		Provider:        strings.TrimSpace(r.Provider),
		ExternalCode:    strings.TrimSpace(r.ExternalCode),
		Address:         strings.TrimSpace(r.Address),
		Contact:         strings.TrimSpace(r.Contact),
		AllowsShipping:  formBool(r.AllowsShipping),
		APICredential:   strings.TrimSpace(r.APICredential),
		ClearCredential: formBool(r.ClearCredential),
		SecretRef:       strings.TrimSpace(r.SecretRef),
	}
}

// CreateReq 转成 service 的建仓请求。
func (r *WarehouseFormReq) CreateReq() *CreateWarehouseReq {
	typ := strings.TrimSpace(r.Type)
	return &CreateWarehouseReq{
		ProjectID:      strings.TrimSpace(r.ProjectID),
		Code:           strings.TrimSpace(r.Code),
		Name:           strings.TrimSpace(r.Name),
		Type:           typ,
		IsDefault:      formBool(r.IsDefault),
		Sort:           formInt(r.Sort, 0),
		DeductPriority: formInt(r.DeductPriority, DefaultDeductPriority),
		ThirdParty:     r.ThirdParty(typ),
	}
}

// UpdateReq 转成 service 的改仓请求（表单是最终状态：空串归一成默认值，不是「本次不改」）。
func (r *WarehouseFormReq) UpdateReq() *UpdateWarehouseReq {
	code, name := strings.TrimSpace(r.Code), strings.TrimSpace(r.Name)
	status, typ := strings.TrimSpace(r.Status), strings.TrimSpace(r.Type)
	if status == "" {
		status = inventoryenums.StatusActive
	}
	if typ == "" {
		typ = inventoryenums.WarehouseTypeSelf
	}
	sort := formInt(r.Sort, 0)
	priority := formInt(r.DeductPriority, DefaultDeductPriority)
	return &UpdateWarehouseReq{
		ID:             strings.TrimSpace(r.ID),
		Code:           &code,
		Name:           &name,
		Status:         &status,
		Type:           &typ,
		Sort:           &sort,
		DeductPriority: &priority,
		ThirdParty:     r.ThirdParty(typ),
	}
}

// WarehouseDefaultFormReq 设默认仓（表单只有一个隐藏 id）。
type WarehouseDefaultFormReq struct {
	ID string `form:"id"`
}

// WarehouseDeleteFormReq 删除仓库。
type WarehouseDeleteFormReq struct {
	ID        string `form:"id"`
	ProjectID string `form:"projectId"`
}

// WarehouseBulkFormReq 批量操作（勾选的 id 以同名键重复提交）。
type WarehouseBulkFormReq struct {
	ProjectID string   `form:"projectId"`
	IDs       []string `form:"ids"`
}
