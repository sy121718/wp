package ordermcp

// order_status_tools.go — 订单状态机与后台备注的写工具。
//
// 这三个动作都只吃 orderId：工程作用域由 service 自己探测（`locateOrderProject` /
// `resolveOrderProject` 会逐工程找订单归属），**不要**让调用方传 projectId ——
// 传错工程时 orders 表的 FORCE 策略会让加锁读静默匹配 0 行，
// 症状是「订单不存在」，而订单明明在。
//
// 操作人身份从 ctx 注入（`mcp.UserIDFrom`），不从参数读：
// 模型会照用户口述的名字填，而状态流转日志是审计材料。

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go_wp/internal/mcp"
	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	"go_wp/internal/permission"
)

type statusWriter interface {
	ChangeStatus(ctx context.Context, req *orderdto.ChangeStatusReq) error
	CancelOrder(ctx context.Context, req *orderdto.CancelOrderReq) (*orderdto.CancelOrderResp, error)
	UpdateOrderNote(ctx context.Context, req *orderdto.UpdateOrderNoteReq) (*orderdto.OrderResp, error)
}

// StatusWriteTools 返回订单状态工具的写工具集（3 个）。
func StatusWriteTools(w statusWriter) ([]mcp.Tool, error) {
	if w == nil {
		return nil, errors.New("ordermcp: 订单状态写依赖缺失（装配期接线错误）")
	}
	return []mcp.Tool{orderStatus(w), orderCancel(w), orderNote(w)}, nil
}

// orderStatusArgs 只留 orderId / toStatus / remark —— 操作人由 ctx 注入。
type orderStatusArgs struct {
	OrderID  uint64 `json:"orderId"`
	ToStatus string `json:"toStatus"`
	Remark   string `json:"remark"`
}

func orderStatus(w statusWriter) mcp.Tool {
	return mcp.NewWrite("order_status", "改订单状态",
		"把订单推到下一个状态。可选目标：`paid` 已付款 / `shipped` 已发货 / "+
			"`completed` 已完成 / `pending` 待付款。\n"+
			"**取消和退款不走这里** —— 取消要归还库存、退款要记流水号，各有专门的入口："+
			"取消用 `order_cancel`，退款在后台订单页操作。传这两个值会被服务端拒绝。\n"+
			"服务端只接受合法的流转（比如没付款的订单不能直接发 `shipped`），"+
			"被拒时回执会说明当前状态能走到哪里 —— 别绕过它硬试。\n"+
			"改之前先 `order_get` 看清当前状态与金额，报给用户确认后再改。",
		permission.OrderStatus,
		mcp.Object("改订单状态参数", map[string]mcp.Schema{
			"orderId":  mcp.Integer("订单 id（用 order_find 拿，不是订单号）"),
			"toStatus": mcp.Enum("目标状态", "pending", "paid", "shipped", "completed"),
			"remark":   mcp.String("流转备注（可选，会写进状态日志）"),
		}, "orderId", "toStatus"),
		nil,
		func(ctx context.Context, args orderStatusArgs) (mcp.Result, error) {
			to := strings.TrimSpace(args.ToStatus)
			req := &orderdto.ChangeStatusReq{
				OrderID:      args.OrderID,
				ToStatus:     to,
				Remark:       strings.TrimSpace(args.Remark),
				OperatorType: "admin",
				OperatorID:   uint64(mcp.UserIDFrom(ctx)),
			}
			if err := w.ChangeStatus(ctx, req); err != nil {
				return mcp.Result{}, err
			}
			_, label := orderenums.OrderStatusLabel(to)
			note := ""
			switch to {
			case "paid":
				note = "付款时间已记下（对账要用）。"
			case "shipped":
				note = "完成时间已清空 —— 发货后订单回到「进行中」。"
			case "completed":
				note = "完成时间已记下（时效统计要用）。"
			}
			return mcp.Result{Text: fmt.Sprintf("订单 %d 已改为「%s」。%s"+
				"流转记录已写进订单状态日志。", args.OrderID, label, note)}, nil
		})
}

type orderCancelArgs struct {
	OrderID uint64 `json:"orderId"`
	Reason  string `json:"reason"`
}

func orderCancel(w statusWriter) mcp.Tool {
	return mcp.NewWrite("order_cancel", "取消订单",
		"取消一笔订单。**它会归还库存**（同一事务里，库存回不来则整笔回滚、订单保持原状）"+
			"并释放已核销的优惠券。\n"+
			"**必须给原因** —— 取消原因会记进状态日志，事后对账要看。\n"+
			"已付款的订单取消前先 `order_get` 确认金额，并把「钱怎么退」跟用户讲清楚："+
			"这个动作只把订单置为已取消，**退款要走后台的退款入口**。\n"+
			"取消不可撤销（要恢复只能重新建单）。",
		permission.OrderCancel,
		mcp.Object("取消订单参数", map[string]mcp.Schema{
			"orderId": mcp.Integer("订单 id（用 order_find 拿，不是订单号）"),
			"reason":  mcp.String("取消原因（必填，写进状态日志）"),
		}, "orderId", "reason"),
		nil,
		func(ctx context.Context, args orderCancelArgs) (mcp.Result, error) {
			req := &orderdto.CancelOrderReq{
				OrderID:      args.OrderID,
				Reason:       strings.TrimSpace(args.Reason),
				OperatorType: "admin",
				OperatorID:   uint64(mcp.UserIDFrom(ctx)),
			}
			if _, err := w.CancelOrder(ctx, req); err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: fmt.Sprintf(
				"订单 %d 已取消（原因：%s）。占用的库存已归还、优惠券已释放。"+
					"如果这笔单已经收过款，退款要另外去后台订单页做 —— 这一步只改了订单状态。",
				args.OrderID, strings.TrimSpace(args.Reason))}, nil
		})
}

type orderNoteArgs struct {
	OrderID   uint64 `json:"orderId"`
	AdminNote string `json:"adminNote"`
}

func orderNote(w statusWriter) mcp.Tool {
	return mcp.NewWrite("order_note", "写订单后台备注",
		"改订单的后台备注（只有内部可见，客户看不到）。**整段覆盖**，不是追加 —— "+
			"要保留原有的备注，先用 `order_get` 读出当前内容，把新旧拼在一起再写。\n"+
			"备注**不是状态流转**：它不改变订单处在哪一步，所以不写状态日志。\n"+
			"把它当便签用（「客户要求周五前发出」「电话确认过地址」），"+
			"别把状态变更的想法写在这里 —— 那样它不会真的发生，只留下一句失效的话。",
		permission.OrderNote,
		mcp.Object("写订单备注参数", map[string]mcp.Schema{
			"orderId":   mcp.Integer("订单 id（用 order_find 拿，不是订单号）"),
			"adminNote": mcp.String("完整的新备注内容（会整段替换原备注；传空串 = 清空备注）"),
		}, "orderId", "adminNote"),
		nil,
		func(ctx context.Context, args orderNoteArgs) (mcp.Result, error) {
			req := &orderdto.UpdateOrderNoteReq{
				OrderID:      args.OrderID,
				AdminNote:    args.AdminNote,
				OperatorType: "admin",
				OperatorID:   uint64(mcp.UserIDFrom(ctx)),
			}
			res, err := w.UpdateOrderNote(ctx, req)
			if err != nil {
				return mcp.Result{}, err
			}
			if res == nil {
				return mcp.Result{Text: fmt.Sprintf("订单 %d 的备注已更新。", args.OrderID)}, nil
			}
			now := strings.TrimSpace(res.AdminNote)
			if now == "" {
				return mcp.Result{Text: fmt.Sprintf("订单 %s 的备注已清空。", res.OrderNo)}, nil
			}
			return mcp.Result{Text: fmt.Sprintf("订单 %s 的备注已更新，现在是：%s", res.OrderNo, now)}, nil
		})
}
