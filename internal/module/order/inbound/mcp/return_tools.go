package ordermcp

// return_tools.go — 退货申请的审阅与收货。
//
// 这条链路的每一步都有**不可逆的钱货后果**，描述里逐条写明：
//   - approve 带 autoReceive 时是「同意 + 当场入库 + 当场退款」一步到底；
//   - receive 是「货已到、入库 + 退款」；
//   - reject 是终态（客户的申请被驳回），remark 是给客户看的理由。
//
// 操作人身份从 ctx 注入（mcp.UserIDFrom），不从参数读 —— 审阅人是审计材料。

import (
	"context"
	"fmt"
	"strings"

	"go_wp/internal/mcp"
	orderdto "go_wp/internal/module/order/dto"
	"go_wp/internal/permission"
)

const (
	returnDefaultLimit = 10
	returnMaxLimit     = 50
)

// ReturnListArgs 退货列表筛选。
type ReturnListArgs struct {
	ProjectID string `json:"projectId"`
	Keyword   string `json:"keyword,omitempty"`
	Status    string `json:"status,omitempty"`
	OrderID   uint64 `json:"orderId,omitempty"`
	PageSize  int    `json:"pageSize,omitempty"`
	Offset    int    `json:"offset,omitempty"`
}

// ReturnGetArgs 看单条退货申请。
type ReturnGetArgs struct {
	ReturnID uint64 `json:"returnId"`
}

// ReturnReviewArgs 审阅（同意 / 驳回）。
type ReturnReviewArgs struct {
	ReturnID uint64 `json:"returnId"`
	Remark   string `json:"remark,omitempty"`
	// AutoReceive 同意后立刻完成「入库 + 退款」。见工具描述：这是一步到底，不是加速确认。
	AutoReceive   bool   `json:"autoReceive,omitempty"`
	WarehouseID   string `json:"warehouseId,omitempty"`
	TransactionID string `json:"transactionId,omitempty"`
}

// ReturnReceiveArgs 确认收货（入库 + 退款）。
type ReturnReceiveArgs struct {
	ReturnID      uint64 `json:"returnId"`
	WarehouseID   string `json:"warehouseId,omitempty"`
	TransactionID string `json:"transactionId,omitempty"`
	Remark        string `json:"remark,omitempty"`
}

// ReturnReader / ReturnWriter 是本模块对退货的窄接口。
//
// 刻意不含 ReturnableOfOrder（那是前台「这单还能不能退」的判断）与
// CancelReturn（撤销是**客户**的动作，后台代客户撤销要另想清楚身份问题）。
// 也不含 RefundReturn 之类的内部步骤：入库与退款由 receive 一次性编排，
// 单独暴露它们等于允许「只退款不收货」这种账实不符的状态。
type ReturnReader interface {
	ListReturns(ctx context.Context, req *orderdto.ReturnListReq) (*orderdto.ReturnListResp, error)
	GetReturn(ctx context.Context, returnID uint64) (*orderdto.ReturnDetailResp, error)
}

type ReturnWriter interface {
	ApproveReturn(ctx context.Context, req *orderdto.ReturnReviewReq) (*orderdto.ReturnResp, error)
	RejectReturn(ctx context.Context, req *orderdto.ReturnReviewReq) (*orderdto.ReturnResp, error)
	ReceiveReturn(ctx context.Context, req *orderdto.ReturnReceiveReq) (*orderdto.ReturnResp, error)
}

// ReturnTools 装配退货工具。
func ReturnTools(r ReturnReader, w ReturnWriter, store mcp.IdempotencyStore) ([]mcp.Tool, error) {
	if r == nil {
		return nil, fmt.Errorf("退货读取器不能为空")
	}
	if w == nil {
		return nil, fmt.Errorf("退货写入器不能为空")
	}
	if store == nil {
		return nil, fmt.Errorf("幂等存储不能为空")
	}

	list := mcp.New("return_list", "查退货申请",
		"列出退货 / 退款申请。默认按提交时间倒序。\n"+
			"**`status=pending` 是「等我们处理」的那一批** —— 客户已经提交，钱和货都还没动。\n"+
			"看单条详情用 return_get（列表不带商品明细与退款额拆解）。",
		permission.OrderReturnList,
		mcp.Object("查退货申请", map[string]mcp.Schema{
			"projectId": mcp.String("工程 id"),
			"status": mcp.Enum("按状态筛：pending 待处理 / approved 已同意（待收货）/ rejected 已驳回 / received 已收货 / refunded 已退款 / cancelled 已撤销；省略=全部",
				"pending", "approved", "rejected", "received", "refunded", "cancelled"),
			"keyword":  mcp.String("按退货单号 / 订单号 / 客户模糊匹配，可省略"),
			"orderId":  mcp.Integer("只看某一单的退货申请，可省略"),
			"pageSize": mcp.Integer(fmt.Sprintf("最多返回多少条（默认 %d，上限 %d）", returnDefaultLimit, returnMaxLimit)),
			"offset":   mcp.Integer("跳过多少条（翻页用，默认 0）"),
		}, "projectId"),
		func(ctx context.Context, args ReturnListArgs) (mcp.Result, error) {
			projectID := strings.TrimSpace(args.ProjectID)
			if projectID == "" {
				return mcp.Result{}, &mcp.ArgsError{Msg: "projectId 不能为空"}
			}
			res, err := r.ListReturns(ctx, &orderdto.ReturnListReq{
				ProjectID: projectID,
				Keyword:   strings.TrimSpace(args.Keyword),
				Status:    strings.TrimSpace(args.Status),
				OrderID:   args.OrderID,
				Limit:     clampReturnLimit(args.PageSize),
				Offset:    clampOffset(args.Offset),
			})
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: returnListText(res)}, nil
		})

	get := mcp.New("return_get", "看一条退货申请的详情",
		"取回退货申请的全部信息：客户、订单、申请理由、**逐条商品的退款额**、"+
			"以及审阅与收货的时间线。\n"+
			"同意或驳回之前先跑这一步 —— 退款额是按明细算出来的，不是整单金额。",
		permission.OrderReturnGet,
		mcp.Object("取退货详情", map[string]mcp.Schema{
			"returnId": mcp.Integer("退货申请 id（从 return_list 拿）"),
		}, "returnId"),
		func(ctx context.Context, args ReturnGetArgs) (mcp.Result, error) {
			if args.ReturnID == 0 {
				return mcp.Result{}, &mcp.ArgsError{Msg: "returnId 不能为空"}
			}
			res, err := r.GetReturn(ctx, args.ReturnID)
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: returnDetailText(res)}, nil
		})

	approve := mcp.NewWrite("return_approve", "同意退货",
		"同意客户的退货申请。**这一步会不会动钱和货，取决于 autoReceive**：\n"+
			"* `autoReceive=false`（默认）：只把状态改成「已同意」，等仓库确认收到货再走 return_receive。"+
			"货还没到手时用这个。\n"+
			"* `autoReceive=true`：**当场完成「入库 + 退款」一步到底** —— "+
			"库存加回去、钱退给客户。只在前台已经把货拿回来了、或者你确实要立刻退款的场景才用。\n"+
			"退款额是按申请明细逐条算的，不是整单金额；同意之前用 return_get 看清数字。"+
			"同意之后状态就是「已同意」，不能再改回待处理。",
		permission.OrderReturnApprove,
		mcp.Object("同意退货", map[string]mcp.Schema{
			"returnId":    mcp.Integer("退货申请 id"),
			"autoReceive": mcp.Boolean("true = 立刻完成入库与退款（一步到底）；false 或不填 = 只同意，等收货再走 return_receive"),
			"warehouseId": mcp.String("入库仓库 id，省略 = 该工程默认仓"),
			"remark":      mcp.String("给内部看的审阅备注，可省略"),
		}, "returnId"),
		store,
		func(ctx context.Context, args ReturnReviewArgs) (mcp.Result, error) {
			req := &orderdto.ReturnReviewReq{
				ReturnID:    args.ReturnID,
				Remark:      strings.TrimSpace(args.Remark),
				AutoReceive: args.AutoReceive,
				WarehouseID: strings.TrimSpace(args.WarehouseID),
			}
			fillReturnOperator(ctx, req)
			res, err := w.ApproveReturn(ctx, req)
			if err != nil {
				return mcp.Result{}, err
			}
			tail := "已停在「已同意」—— 等仓库点确认收货后再跑 return_receive，那时才入库与退款。"
			if args.AutoReceive {
				tail = "**入库与退款已经执行完毕**（库存已加回、钱已退给客户）。"
			}
			return mcp.Result{Text: "退货申请已同意：\n" + returnRespText(res) + "\n" + tail}, nil
		})

	reject := mcp.NewWrite("return_reject", "驳回退货申请",
		"驳回客户的退货申请。**这是终态**，客户看到的是「被拒绝」，之后要走也是重新提交一张新的申请。\n"+
			"`remark` 是**给客户看的理由**，不是内部备注 —— 空着等于让客户收到一个没有解释的拒绝。"+
			"驳回之前先 return_get 看清理由与明细，并把理由跟用户核对一遍。\n"+
			"驳回不动库存也不退款。",
		permission.OrderReturnReject,
		mcp.Object("驳回退货", map[string]mcp.Schema{
			"returnId": mcp.Integer("退货申请 id"),
			"remark":   mcp.String("驳回理由（会展示给客户，**必填**）"),
		}, "returnId", "remark"),
		store,
		func(ctx context.Context, args ReturnReviewArgs) (mcp.Result, error) {
			remark := strings.TrimSpace(args.Remark)
			if remark == "" {
				return mcp.Result{}, &mcp.ArgsError{Msg: "驳回必须给出理由（remark），客户会看到它"}
			}
			req := &orderdto.ReturnReviewReq{ReturnID: args.ReturnID, Remark: remark}
			fillReturnOperator(ctx, req)
			res, err := w.RejectReturn(ctx, req)
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: "退货申请已驳回：\n" + returnRespText(res) +
				"\n客户会看到理由：" + remark}, nil
		})

	receive := mcp.NewWrite("return_receive", "确认收货并退货",
		"确认已经收到退回来的货，并**当场执行两件事：库存加回、退款给客户**。"+
			"两步各自幂等，重复调用不会重复退款。\n"+
			"只在货真的在手上时用。申请得先是「已同意」状态（没同意过先走 return_approve）。\n"+
			"退款额按申请明细逐条算，不是整单金额 —— 部分退货只退那几件的钱。",
		permission.OrderReturnReceive,
		mcp.Object("确认收货", map[string]mcp.Schema{
			"returnId":      mcp.Integer("退货申请 id"),
			"warehouseId":   mcp.String("入库仓库 id，省略 = 该工程默认仓"),
			"transactionId": mcp.String("退款流水号，可省略（省略时由服务端生成）"),
			"remark":        mcp.String("备注，可省略"),
		}, "returnId"),
		store,
		func(ctx context.Context, args ReturnReceiveArgs) (mcp.Result, error) {
			req := &orderdto.ReturnReceiveReq{
				ReturnID:      args.ReturnID,
				WarehouseID:   strings.TrimSpace(args.WarehouseID),
				TransactionID: strings.TrimSpace(args.TransactionID),
				Remark:        strings.TrimSpace(args.Remark),
			}
			fillReturnReceiveOperator(ctx, req)
			res, err := w.ReceiveReturn(ctx, req)
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: "已确认收货：\n" + returnRespText(res) +
				"\n**库存已加回、钱已退给客户。**"}, nil
		})

	return []mcp.Tool{list, get, approve, reject, receive}, nil
}

// fillReturnOperator 把操作人从 ctx 注入。
//
// 不从参数读：审阅人是审计材料，模型会照用户口述的名字填，而那个名字未必是登录身份。
func fillReturnOperator(ctx context.Context, req *orderdto.ReturnReviewReq) {
	if uid := mcp.UserIDFrom(ctx); uid > 0 {
		req.OperatorType = "admin"
		req.OperatorID = uint64(uid)
	}
}

func fillReturnReceiveOperator(ctx context.Context, req *orderdto.ReturnReceiveReq) {
	if uid := mcp.UserIDFrom(ctx); uid > 0 {
		req.OperatorType = "admin"
		req.OperatorID = uint64(uid)
	}
}

func clampReturnLimit(v int) int {
	if v <= 0 {
		return returnDefaultLimit
	}
	if v > returnMaxLimit {
		return returnMaxLimit
	}
	return v
}

// returnRespText 写操作回执用的简版：只有退货单本身，没有订单快照与明细。
func returnRespText(r *orderdto.ReturnResp) string {
	if r == nil {
		return "（服务端没有回传退货单）"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "* 退货单号 %s（id=%d），对应订单 %s\n",
		emptyAsDash(r.ReturnNo), r.ID, emptyAsDash(r.OrderNo))
	fmt.Fprintf(&b, "* 客户：%s %s\n", emptyAsDash(r.CustomerName), emptyAsDash(r.CustomerEmail))
	fmt.Fprintf(&b, "* 状态：%s\n", returnStatusText(r.Status, r.StatusLabel))
	fmt.Fprintf(&b, "* 退款额：%s", firstNonEmpty(r.RefundLabel, centsLabel(r.RefundAmount)))
	return b.String()
}

func returnListText(res *orderdto.ReturnListResp) string {
	if res == nil || len(res.List) == 0 {
		return "没有符合条件的退货申请。"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "共 %d 条，本页 %d 条：\n", res.Total, len(res.List))
	for i, r := range res.List {
		fmt.Fprintf(&b, "%d. id=%d %s · 订单 %s · 客户 %s · 退款 %s · %s · 提交 %s\n",
			i+1, r.ID, emptyAsDash(r.ReturnNo), emptyAsDash(r.OrderNo),
			firstNonEmpty(r.CustomerEmail, r.CustomerName, "-"),
			firstNonEmpty(r.RefundLabel, centsLabel(r.RefundAmount)),
			returnStatusText(r.Status, r.StatusLabel), r.CreateTime.String())
	}
	return b.String()
}

// returnDetailText 详情视图：ReturnDetailResp 是 {Return *ReturnResp, Order *OrderResp} 的组合，
// 明细挂在 Return.Items 上（不是外层）。
func returnDetailText(d *orderdto.ReturnDetailResp) string {
	if d == nil || d.Return == nil {
		return "没有这条退货申请。"
	}
	r := d.Return
	var b strings.Builder
	b.WriteString(returnRespText(r))
	b.WriteString("\n")
	fmt.Fprintf(&b, "* 申请理由：%s\n", emptyAsDash(r.Reason))
	if len(r.Items) > 0 {
		b.WriteString("* 退货明细（退款额是按这些逐条算的，不是整单金额）：\n")
		for _, it := range r.Items {
			fmt.Fprintf(&b, "  - %s %s ×%d，退 %s\n",
				emptyAsDash(it.ProductName), emptyAsDash(it.VariantLabel), it.Quantity,
				firstNonEmpty(it.RefundLabel, centsLabel(it.RefundAmount)))
		}
	}
	if d.Order != nil {
		fmt.Fprintf(&b, "* 订单：%s，状态 %s，金额 %s\n",
			emptyAsDash(d.Order.OrderNo), orderStatusText(d.Order.Status),
			moneyText(d.Order.Total, d.Order.Currency))
	}
	if strings.TrimSpace(r.AdminNote) != "" {
		fmt.Fprintf(&b, "* 审阅备注：%s\n", r.AdminNote)
	}
	if r.ReviewedAt != nil {
		fmt.Fprintf(&b, "* 审阅于 %s（%s）\n", r.ReviewedAt.String(), emptyAsDash(r.ReviewerName))
	}
	if r.ReceivedAt != nil {
		fmt.Fprintf(&b, "* 收货于 %s\n", r.ReceivedAt.String())
	}
	if r.RefundedAt != nil {
		fmt.Fprintf(&b, "* 退款于 %s（流水 %s）", r.RefundedAt.String(), emptyAsDash(r.TransactionID))
	}
	return b.String()
}

// returnStatusText 状态的中文说法。
//
// 优先用 service 给的口径标签；它缺席时兜底 —— 但**不能只回裸状态码**：
// 「已同意」与「已收货」在下游是两件不同的事（一个还没退钱，一个钱已退）。
func returnStatusText(status, label string) string {
	if s := strings.TrimSpace(label); s != "" {
		return s
	}
	switch strings.TrimSpace(status) {
	case "pending":
		return "待处理"
	case "approved":
		return "已同意（待收货）"
	case "rejected":
		return "已驳回"
	case "received":
		return "已收货（待退款）"
	case "refunded":
		return "已退款"
	case "cancelled":
		return "已撤销"
	}
	return emptyAsDash(status)
}
