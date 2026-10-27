package inventorydto

// inventory_transfer_req.go — 调拨单的入参（迁移 630，W3）。

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// TransferLineReq 一条调拨明细：搬源仓的哪条库存行、多少件。
//
// StockID 而不是 VariantID：调拨搬的是**源仓里那一批具体的货**，指向库存行才能表达
// 「无归属的货也能调拨」（迁移 624 的 variant_id IS NULL 行）。
type TransferLineReq struct {
	StockID  string `form:"stockId" json:"stockId" binding:"required"`
	Quantity int    `form:"quantity" json:"quantity"`
}

// CreateTransferReq 建调拨单：两个仓 + 明细 + 可选申请凭证。
//
// 两个仓都必填（与 ERP 的差异见 TransferService 的注释）；from <> to 由 DDL 兜底，
// 应用层先给可读的错误（不让运营看到一句 CHECK 约束原文）。
type CreateTransferReq struct {
	ProjectID       string            `form:"projectId" json:"projectId"`
	FromWarehouseID string            `form:"fromWarehouseId" json:"fromWarehouseId" binding:"required"`
	ToWarehouseID   string            `form:"toWarehouseId" json:"toWarehouseId" binding:"required"`
	Remark          string            `form:"remark" json:"remark"`
	OperatorID      int64             `form:"operatorId" json:"operatorId"`
	RequestID       string            `form:"requestId" json:"requestId"`
	Lines           []TransferLineReq `form:"lines" json:"lines" binding:"required"`
	// AttachmentIDs 申请凭证（可选）：有的工程要求先有申请单才建调拨。
	// 表单体是**同名多值**（每个附件一个隐藏域），[]int64 反射绑得上，不需 BindForm。
	AttachmentIDs []int64 `form:"attachmentIds" json:"attachmentIds"`
}

// BindForm 收表单体里的明细行（lines[0].stockId / lines[0].quantity）。
//
// 为什么必须自己收：Go 的表单绑定**没有嵌套数组语法** —— lines[0].stockId=x 绑不出
// []TransferLineReq（实测：反射那条路整条绑成空切片，服务端只会报「至少要有一行明细」，
// 看上去像用户没填，实际是绑定没生效）。JSON 请求体不受影响（MCP / 脚本发 JSON）。
func (r *CreateTransferReq) BindForm(vals url.Values) error {
	lines := make([]TransferLineReq, 0, 4)
	for idx := 0; idx < maxTransferFormLines; idx++ {
		base := fmt.Sprintf("lines[%d].", idx)
		stockID := strings.TrimSpace(vals.Get(base + "stockId"))
		if stockID == "" {
			// 遇到空行只跳过：前端删掉中间一行时下标会留空，硬要求连续会让「删一行再提交」失败。
			continue
		}
		qty := 0
		if raw := strings.TrimSpace(vals.Get(base + "quantity")); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil {
				// 返回非 nil 即触发 binding 的 ParamError（400）：dto 不取词、不造界面文案。
				return fmt.Errorf("quantity of %s is not an integer", base)
			}
			qty = n
		}
		lines = append(lines, TransferLineReq{StockID: stockID, Quantity: qty})
	}
	if len(lines) > 0 {
		r.Lines = lines
	}
	return nil
}

// maxTransferFormLines 表单体里最多收多少行明细（比 service 的 maxBatchLines 放宽一档：
// 多收进来的行由 service 判「行数过多」；前端截断会让「填了 120 行只提交 100 行」变成静默丢数据）。
const maxTransferFormLines = 200

// TransferShipReq 出货（**必须带出货凭证**）。
type TransferShipReq struct {
	ProjectID     string  `form:"projectId" json:"projectId"`
	ID            string  `form:"id" json:"id" binding:"required"`
	Remark        string  `form:"remark" json:"remark"`
	OperatorID    int64   `form:"operatorId" json:"operatorId"`
	AttachmentIDs []int64 `form:"attachmentIds" json:"attachmentIds"`
}

// TransferReceiveReq 签收（**必须带签收凭证**）。
type TransferReceiveReq struct {
	ProjectID     string  `form:"projectId" json:"projectId"`
	ID            string  `form:"id" json:"id" binding:"required"`
	Remark        string  `form:"remark" json:"remark"`
	OperatorID    int64   `form:"operatorId" json:"operatorId"`
	AttachmentIDs []int64 `form:"attachmentIds" json:"attachmentIds"`
}

// TransferApproveReq 同意调拨（仅 requested；迁移 651）。
//
// 审批时可以改目标仓：申请只说「这批货要从哪走」，真正发往哪里往往在同意那一刻才定。
// ToWarehouseID 留空 = 沿用申请时的目标仓。
type TransferApproveReq struct {
	ProjectID     string `form:"projectId" json:"projectId"`
	ID            string `form:"id" json:"id" binding:"required"`
	ToWarehouseID string `form:"toWarehouseId" json:"toWarehouseId"`
	Remark        string `form:"remark" json:"remark"`
	// OperatorID / OperatorName 是**审计字段**：服务端从会话无条件覆盖（见 handler），
	// 客户端传什么都不看（对齐 order_shipment_req 的同一口径）。
	OperatorID   int64  `form:"operatorId" json:"operatorId"`
	OperatorName string `form:"operatorName" json:"operatorName"`
}

// TransferRejectReq 驳回调拨（仅 requested；迁移 651）。
//
// Reason 必填：驳回是给申请人的一个答复，没有理由的驳回等于让人重试同一件事。
type TransferRejectReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
	ID        string `form:"id" json:"id" binding:"required"`
	Reason    string `form:"reason" json:"reason" binding:"required"`
	// OperatorID / OperatorName 同上：会话覆盖的审计字段。
	OperatorID   int64  `form:"operatorId" json:"operatorId"`
	OperatorName string `form:"operatorName" json:"operatorName"`
}

// TransferCancelReq 取消（requested / pending 可取消）。
type TransferCancelReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
	ID        string `form:"id" json:"id" binding:"required"`
	Remark    string `form:"remark" json:"remark"`
}

// TransferStockOptionReq 建单抽屉的候选：某仓里可售的库存行（按编码 / 名称搜）。
//
// 单独一个读口而不是复用 /api/inventory/stock/list：那个接口要 inventory:stock_list，
// 而建单人可能只有 transfer_create —— 让他为了「选一行货」去要库存台账的权限，
// 等于把权限口径搅在一起。这里只回答一个问题：这个仓里哪些货能调、各有多少。
type TransferStockOptionReq struct {
	ProjectID   string `form:"projectId" json:"projectId"`
	WarehouseID string `form:"warehouseId" json:"warehouseId" binding:"required"`
	Keyword     string `form:"keyword" json:"keyword"`
	// Limit 上限（默认 200，最大 500）：下拉不该一次拉整个仓。
	Limit int `form:"limit" json:"limit"`
}

// ListTransferReq 调拨单列表（状态 / 关键词 + 分页）。
type ListTransferReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
	Status    string `form:"status" json:"status"`
	Keyword   string `form:"keyword" json:"keyword"`
	Page      int    `form:"page" json:"page"`
	Size      int    `form:"size" json:"size"`
}

// GetTransferReq 调拨单详情。
type GetTransferReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
	ID        string `form:"id" json:"id" binding:"required"`
}
