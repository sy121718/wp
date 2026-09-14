package runtimefragment

// returns.go — 访客退货申请片段（BIZ-1 退货入库）。
//
// 与订单列表同一条边界：站点是已编译的静态产物，「我要退哪一件」每个人都不同。
// 差别在于这里**会落库**（写路径），所以要说清它不是 CSRF 缺口：
//
//   · 申请必须带**访客会话**（身份由中间件解出、写进 orderdto 的 UserID）；
//   · order 模块的 RequestReturn 把 userID 钉死在契约里，并要求订单归属一致 ——
//     伪造的跨站表单拿不到受害者的会话 cookie（SameSite=Lax 让跨站 POST 不带 cookie），
//     就算带上了也无从知道受害者买了什么（orderItemId 由服务端按订单校验，不属于本单一律拒绝）；
//   · 金额不由客户端决定：退款额 = 订单项快照单价 × 数量，全部服务端算。
//
// 未登录时**不报 401**（与订单列表同一理由：HTMX 默认不替换 401 响应的目标节点），
// 而是渲染一句引导。

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	ordercontract "go_wp/internal/module/order/contract"
	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	"go_wp/internal/templates"
)

// visitorReturns 访客退货能力（装配期注入；nil = 未接入）。
//
// 类型是**收窄过的** VisitorReturnPort：片段层拿不到「后台审核 / 入库 / 退款」那几条 ——
// 越权防护靠接口形状，而不是靠调用方自觉。
var visitorReturns ordercontract.VisitorReturnPort

// SetVisitorReturnProvider 注入访客退货能力（装配期调用；传 nil 表示未接入）。
func SetVisitorReturnProvider(p ordercontract.VisitorReturnPort) { visitorReturns = p }

func init() {
	Register(Spec{Type: "returnRequest", Method: "POST", Auth: AuthAnonymous, Render: renderReturnRequest})
}

// returnResultData 退货提交结果片段的数据。
type returnResultData struct {
	Notice      string // 非空 = 失败（只渲染这句话）
	ReturnNo    string
	StatusLabel string
	RefundLabel string
	Reason      string
	StatusLine  string
	RefundNote  string
	Labels      returnResultLabels
}

// renderReturnRequest 客户提交退货申请。
func renderReturnRequest(ctx context.Context, r *Request) (string, error) {
	projectID := paramOf(r, "projectId")
	labels := returnResultLabelsOf(r)
	uid, ok := visitorIDOf(r)
	if !ok {
		return renderReturnResult(r, returnResultData{
			Notice: labels.NeedLogin,
			Labels: labels,
		})
	}
	if visitorReturns == nil {
		return renderReturnResult(r, returnResultData{Notice: labels.ProviderUnavailable, Labels: labels})
	}
	orderID, perr := strconv.ParseUint(strings.TrimSpace(paramOf(r, "orderId")), 10, 64)
	if perr != nil || orderID == 0 {
		return renderReturnResult(r, returnResultData{Notice: fragmentUserMessage(r, orderenums.ErrInvalidParam), Labels: labels})
	}
	items := returnItemsOf(r)
	if len(items) == 0 {
		return renderReturnResult(r, returnResultData{Notice: fragmentUserMessage(r, orderenums.ErrReturnItemsRequired), Labels: labels})
	}
	res, err := visitorReturns.RequestReturn(ctx, &orderdto.ReturnRequestReq{
		ProjectID: projectID,
		OrderID:   orderID,
		Items:     items,
		Reason:    paramOf(r, "reason"),
		RequestID: paramOf(r, "requestId"),
		UserID:    uid,
	})
	if err != nil {
		return renderReturnResult(r, returnResultData{Notice: orderUserMessage(r, err), Labels: labels})
	}
	if res == nil {
		return renderReturnResult(r, returnResultData{Notice: fragmentUserMessage(r, orderenums.ErrInternal), Labels: labels})
	}
	return renderReturnResult(r, returnResultData{
		ReturnNo:    res.ReturnNo,
		StatusLabel: res.StatusLabel,
		RefundLabel: res.RefundLabel,
		Reason:      res.Reason,
		StatusLine:  fmt.Sprintf(labels.StatusLine, res.ReturnNo, res.StatusLabel),
		RefundNote:  fmt.Sprintf(labels.RefundNote, res.RefundLabel),
		Labels:      labels,
	})
}

// returnItemsOf 解析并行数组（orderItemId / quantity 一一对应）。
//
// 为什么按位置配对而不是按键配对：表单里每行是一对输入，浏览器按 DOM 顺序提交，
// 数量为空串时**仍然占位**（占位是配对成立的前提）。数量为空或 0 表示「这行不退」，跳过。
func returnItemsOf(r *Request) []orderdto.ReturnItemReq {
	if r == nil {
		return nil
	}
	ids := r.Values["orderItemId"]
	qtys := r.Values["quantity"]
	if len(ids) == 0 || len(ids) != len(qtys) {
		return nil
	}
	items := make([]orderdto.ReturnItemReq, 0, len(ids))
	for i := range ids {
		qty, qerr := strconv.Atoi(strings.TrimSpace(qtys[i]))
		if qerr != nil || qty <= 0 {
			continue
		}
		id, ierr := strconv.ParseUint(strings.TrimSpace(ids[i]), 10, 64)
		if ierr != nil || id == 0 {
			continue
		}
		items = append(items, orderdto.ReturnItemReq{OrderItemID: id, Quantity: qty})
	}
	return items
}

// renderReturnResult 渲染提交结果。
func renderReturnResult(r *Request, data returnResultData) (string, error) {
	if data.Labels == (returnResultLabels{}) {
		data.Labels = returnResultLabelsOf(r)
	}
	return templates.RenderFragment("return_result", data)
}
