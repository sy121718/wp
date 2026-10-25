package productdto

import (
	"net/url"
	"strings"

	json "go_wp/pkg/jsonx"
)

// product_variant_req.go / product_variant_resp.go — 变体清单、组合预览与批量保存的入参与回执。

// PreviewVariantReq 组合生成预览（不落库）。
//
// Selections 为空 = 「生成全部组合」（与 GenerateVariants 的无表单路径同一语义：
// 全部参与变体的属性组 × 全部启用值）。
//
// ExistingOptionValues 是前端清单里已经有的组合（每行的 option_values 原样回传）：
// 服务端按自己的 optionKey 口径归一后与本次组合比对，命中的不再返回 ——
// 这样「重复点生成不重复追加」的判据只有服务端一份（前端不需要复刻 optionKey）。
type PreviewVariantReq struct {
	ProductID  string                `json:"productId" form:"productId" binding:"required"`
	ProjectID  string                `json:"projectId" form:"projectId"`
	Selections []VariantSelectionReq `json:"selections" form:"selections"`
	// WarehouseID 归属仓（issue #15）：与生成路径同一口径，仅用于主体 SKU 的仓码前缀。
	WarehouseID string `json:"warehouseId" form:"warehouseId"`
	// ExistingOptionValues 前端清单里每行的 option_values（JSON 对象）。
	ExistingOptionValues []json.RawMessage `json:"existingOptionValues" form:"existingOptionValues"`
}

// SaveVariantListReq 以清单为准保存变体（唯一落库动作）。
type SaveVariantListReq struct {
	ProductID string `json:"productId" form:"productId" binding:"required"`
	// ProjectID 是工程隔离（DB-009）的作用域来源；为空时由 service 走唯一工程兜底。
	ProjectID string `json:"projectId" form:"projectId"`
	// WarehouseID 新增行的归属仓（issue #15）：不选即兜底该工程的默认仓；
	// 既有变体不受影响（它们的库存记录早已存在）。
	WarehouseID string `json:"warehouseId" form:"warehouseId"`
	// Rows 清单全部行，顺序即期望的展示顺序。
	// Rows 走 BindForm 自己解析（表单体里是一个 JSON 字符串，元素含 optionValues map）：
	// gin 的 form 映射不支持点号嵌套 —— 见 BindForm 的注释。
	Rows []VariantListResp `json:"rows" form:"-"`
	// OperatorID 操作人（issue #19）：由 inbound 从会话覆盖写入，客户端传入的值被忽略。
	OperatorID string `json:"-" form:"-"`
}

// BindForm 表单体里 Rows 是一个 JSON 字符串（htmx 侧用 JSON.stringify 生成）。
//
// 与 SetBundleConfigReq.BindForm 同一条理由：gin 的 form 映射不支持点号嵌套，
// 而 Rows 的元素里还带 optionValues（一个 map）—— 只能整体按 JSON 解析。
//
// 空串 / 未提交 → 保持 nil（nil = 本次不改，与 JSON 省略同义）。
// 非空但不是合法 JSON → FieldValueError（FieldKindJSON），由出口按 400 报。
func (r *SaveVariantListReq) BindForm(vals url.Values) error {
	raw := strings.TrimSpace(vals.Get("rows"))
	if raw == "" {
		return nil
	}
	if !json.Valid([]byte(raw)) {
		return &FieldValueError{Field: "rows", Kind: FieldKindJSON}
	}
	if err := json.Unmarshal([]byte(raw), &r.Rows); err != nil {
		return &FieldValueError{Field: "rows", Kind: FieldKindJSON}
	}
	return nil
}
