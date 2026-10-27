package purchasedto

import (
	"strconv"
	"strings"
)

// purchaseLineSkuField 采购行 SKU 选择器的表单字段名（模板与解析共用同一个字面量）。
//
// 它承载的是三段拼接的引用 —— "<变体ID>|<商品ID>|<仓库侧裸码>"，不是裸的 SKU 编码：
// 本页不引入自定义 JS（多端契约要求交互全走原生控件），一个 <select> 只能提交一个值，
// 而一行的变体 / 商品 / 仓库侧 SKU 必须**同源**（都取自候选列表的同一行）。
const purchaseLineSkuField = "lineSku"

// PurchaseCreateFormReq 采购单新建表单：单值字段 + 三列平行数组（行）。
//
// 三列同名数组必须整体按序号读（parseSKURef / formArrayAt）—— 反射绑定拆成三片后
// 看不出错位，而行数与列长不一致正是「一行填了 SKU 却没填数量」的常见形态。
type PurchaseCreateFormReq struct {
	// ID 只在**编辑**表单里提交（新建时为空）：建单与改单的字段集只差这一个键，
	// 单列一个类型只会让 CreateReq 的两处调用各自漂移，所以放在一起。
	ID          string `form:"id"`
	ProjectID   string `form:"projectId"`
	Code        string `form:"code"`
	SourceID    string `form:"sourceId"`
	WarehouseID string `form:"warehouseId"`
	Remark      string `form:"remark"`
	// AsDraft 勾选即「存为草稿」（670）：不勾直接下单。checkbox 未勾不提交，
	// 所以空值就是 false —— 默认路径（直接下单）不需要任何额外字段。
	AsDraft bool `form:"asDraft"`

	LineSku       []string `form:"lineSku"`
	LineQuantity  []string `form:"lineQuantity"`
	LineUnitPrice []string `form:"lineUnitPrice"`
}

// CreateReq 转成 service 的建单请求（空行跳过：未选 SKU 的行整行不算）。
//
// SKUCode 为空**不在这里兜底**：归一旦校验在 service（normalizeStockSKU），
// 这条路径只把表单原样翻译成入参（含空值），让唯一的规则在唯一的入口上生效。
func (r *PurchaseCreateFormReq) CreateReq(operatorID string) *CreatePurchaseOrderReq {
	lines := make([]PurchaseLineReq, 0, len(r.LineSku))
	for i, raw := range r.LineSku {
		variantID, productID, skuCode := parseSKURef(raw)
		if variantID == "" {
			continue
		}
		line := PurchaseLineReq{
			VariantID: variantID,
			ProductID: productID,
			SKUCode:   skuCode,
			Quantity:  formInt(formArrayAt(r.LineQuantity, i), 0),
			Sort:      i,
		}
		if price, ok := parseFloatOK(formArrayAt(r.LineUnitPrice, i)); ok {
			line.UnitPrice = price
		}
		lines = append(lines, line)
	}
	return &CreatePurchaseOrderReq{
		ProjectID:   strings.TrimSpace(r.ProjectID),
		Code:        strings.TrimSpace(r.Code),
		SourceID:    strings.TrimSpace(r.SourceID),
		WarehouseID: strings.TrimSpace(r.WarehouseID),
		Remark:      strings.TrimSpace(r.Remark),
		OperatorID:  operatorID,
		AsDraft:     r.AsDraft,
		Lines:       lines,
	}
}

// LineAt 第 i 行的原始三列（越界给空串：表单行数与数组长度不一致时不 panic、不错位）。
func (r *PurchaseCreateFormReq) LineAt(i int) (sku, quantity, unitPrice string) {
	return formArrayAtRaw(r.LineSku, i), formArrayAtRaw(r.LineQuantity, i), formArrayAtRaw(r.LineUnitPrice, i)
}

// PurchaseReceiptFormReq 登记采购收货（**整单一一次提交**：每行一个数量输入）。
//
// 从「一次一行」改成「一次整单」：dy_erp 的收货对话框就是整单一次提交，
// 每行填本次到货量、默认填满剩余量。逐行提交的形态下，一张 10 行的单要点 10 次、
// 失败在第 7 行时前 6 行已经落库 —— 用户看到的是「收了一半」，而不是「这次没收」。
//
// 三列同名数组（lineId / quantity / unitPrice）按序号读，与新建采购单的三列同构：
// 反射绑定拆片后看不出错位，而「填了第一行数量、第二行行号」正是这种错位的常见形态。
// 行数靠 lineId 决定（行号是行的身份，数量是它的属性）。
type PurchaseReceiptFormReq struct {
	ProjectID string   `form:"projectId"`
	OrderID   string   `form:"orderId"`
	RequestID string   `form:"requestId"`
	Remark    string   `form:"remark"`
	LineID    []string `form:"lineId"`
	Quantity  []string `form:"quantity"`
	UnitPrice []string `form:"unitPrice"`
}

// RegisterReq 转成 service 的收货请求。
//
// 本次到货量为 0 / 空的行**整行跳过**（不落零数量行）：一次整单提交里，
// 用户只会填他要收的那几行，其余留着默认值被清空是常态 —— 把 0 当成
// 「这次收 0 件」送去校验，会让一个合法的部分收货被 ErrReceiptQuantityInvalid 拒掉。
// 一格都没填时 Lines 为空，由服务层报「没有可收货的行」。
func (r *PurchaseReceiptFormReq) RegisterReq(operatorID string) *RegisterReceiptReq {
	lines := make([]ReceiptLineReq, 0, len(r.LineID))
	for i, rawLineID := range r.LineID {
		lineID := strings.TrimSpace(rawLineID)
		if lineID == "" {
			continue
		}
		quantity := formInt(formArrayAt(r.Quantity, i), 0)
		if quantity <= 0 {
			continue
		}
		line := ReceiptLineReq{LineID: lineID, Quantity: quantity}
		if price, ok := parseFloatOK(formArrayAt(r.UnitPrice, i)); ok {
			line.UnitPrice = &price
		}
		lines = append(lines, line)
	}
	return &RegisterReceiptReq{
		ProjectID:  strings.TrimSpace(r.ProjectID),
		OrderID:    strings.TrimSpace(r.OrderID),
		RequestID:  strings.TrimSpace(r.RequestID),
		Remark:     strings.TrimSpace(r.Remark),
		OperatorID: operatorID,
		Lines:      lines,
	}
}

// ProductionFormReq 自家工厂生产入库表单（无采购单，成本价手工填写）。
type ProductionFormReq struct {
	ProjectID   string `form:"projectId"`
	SourceID    string `form:"sourceId"`
	WarehouseID string `form:"warehouseId"`
	LineSku     string `form:"lineSku"`
	Quantity    string `form:"quantity"`
	UnitCost    string `form:"unitCost"`
	RequestID   string `form:"requestId"`
	Remark      string `form:"remark"`
}

// ProductionReq 转成 service 的生产入库请求（成本价可空：留空 = 由 service 判）。
func (r *ProductionFormReq) ProductionReq(operatorID string) *ProductionInboundReq {
	variantID, productID, skuCode := parseSKURef(r.LineSku)
	req := &ProductionInboundReq{
		ProjectID:   strings.TrimSpace(r.ProjectID),
		SourceID:    strings.TrimSpace(r.SourceID),
		WarehouseID: strings.TrimSpace(r.WarehouseID),
		VariantID:   variantID,
		ProductID:   productID,
		SKUCode:     skuCode,
		Quantity:    formInt(r.Quantity, 0),
		RequestID:   strings.TrimSpace(r.RequestID),
		Remark:      strings.TrimSpace(r.Remark),
		OperatorID:  operatorID,
	}
	if cost, ok := parseFloatOK(r.UnitCost); ok {
		req.UnitCost = &cost
	}
	return req
}

// PurchaseStatusFormReq 采购单终态的页面表单（提前关单 / 取消共用）。
type PurchaseStatusFormReq struct {
	ProjectID string `form:"projectId"`
	ID        string `form:"id"`
	Remark    string `form:"remark"`
}

// StatusReq 转成 service 的终态请求。
func (r *PurchaseStatusFormReq) StatusReq() *PurchaseStatusReq {
	return &PurchaseStatusReq{
		ProjectID: strings.TrimSpace(r.ProjectID),
		ID:        strings.TrimSpace(r.ID),
		Remark:    strings.TrimSpace(r.Remark),
	}
}

// parseSKURef 拆开一段 "<变体ID>|<商品ID>|<仓库侧裸码>" 引用。
//
// 按 SplitN(…, 3) 拆：前两段是 uuid（不含 '|'），第三段（SKU 编码）原样收下 ——
// 运营自定义的编码里若真的出现 '|'，也只会在最后一段里，不会被误切。
// 段数不足时后面的段为空串（模板不会这么渲染，但**不能因此 panic 或静默错位**：
// 空值会一路走到 service 的归一校验并被明确拒绝）。
func parseSKURef(raw string) (variantID, productID, skuCode string) {
	parts := strings.SplitN(strings.TrimSpace(raw), "|", 3)
	variantID = strings.TrimSpace(parts[0])
	if len(parts) > 1 {
		productID = strings.TrimSpace(parts[1])
	}
	if len(parts) > 2 {
		skuCode = strings.TrimSpace(parts[2])
	}
	return variantID, productID, skuCode
}

// formArrayAt 取数组第 i 项（越界返回空串；取用时修剪空白）。
func formArrayAt(values []string, index int) string {
	return strings.TrimSpace(formArrayAtRaw(values, index))
}

// formArrayAtRaw 取数组第 i 项并保留原样（回显用：不能修剪，否则空行错位）。
func formArrayAtRaw(values []string, index int) string {
	if index < 0 || index >= len(values) {
		return ""
	}
	return values[index]
}

// parseFloatOK 解析可空小数（空串 / 非法返回 ok=false，交给服务端报参数错误）。
func parseFloatOK(raw string) (value float64, ok bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return 0, false
	}
	return parsed, true
}

// formInt 解析表单里的整数（空串 / 非法值用兜底）。
func formInt(v string, fallback int) int {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return fallback
	}
	return n
}
