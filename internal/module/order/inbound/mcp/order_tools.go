package ordermcp

// order_tools.go — 订单模块暴露给模型的可调用工具。
//
// 与 inbound/http 同构：一个模块的「对外能力」按消费者分目录，http 服务后台页面与
// 浏览器接口，mcp 服务 AI（进程内助手与外部 /mcp 两类消费者，见 docs/17 D2）。
// 装配期由上层汇总成一个注册表，运行期只读。

import (
	"context"
	"errors"
	"fmt"

	"go_wp/internal/mcp"
	ordercontract "go_wp/internal/module/order/contract"
	orderdto "go_wp/internal/module/order/dto"
	"go_wp/internal/permission"
)

// Tools 返回订单模块的工具集。
//
// 依赖收窄到 OrderRangeSummaryReader（一条只读方法）而不是整个 OrderService：
// 工具层手里握着 ChangeStatus / RefundOrder 时，「AI 顺手改个订单状态」会从
// 「需要显式加一个工具」退化成「随手就能写」—— 能力边界该由接口形状决定，
// 而不是靠下次写代码时记得住。
func Tools(summary ordercontract.OrderRangeSummaryReader) ([]mcp.Tool, error) {
	if summary == nil {
		return nil, errors.New("ordermcp: 订单区间摘要依赖缺失（装配期接线错误）")
	}
	return []mcp.Tool{ordersSummary(summary)}, nil
}

// ordersSummaryArgs orders_summary 的入参，字段名与 schema 一一对应。
//
// 用结构体而不是 map：字段名拼错是编译错误，而 map 是运行期「少取一个字段」——
// 后者表现为一路零值算出来的、看起来正常的数字。
type ordersSummaryArgs struct {
	ProjectID string `json:"projectId"`
	From      string `json:"from"`
	To        string `json:"to"`
}

func ordersSummary(summary ordercontract.OrderRangeSummaryReader) mcp.Tool {
	return mcp.New("orders_summary", "订单区间摘要",
		"按站点工程与日期区间统计订单：区间内创建的订单数、计入消费的订单数、净销售额"+
			"（净销售额 = 计入消费的订单金额 − 已实际收货的退款额；返回值为整数分，另有展示文案字段）。"+
			"时间区间为含当天的闭区间，跨度上限 366 天。",
		// 复用订单列表的权限点：工具的可见性必须与「这个人本来就看不看得到订单」一致，
		// 另造一个「order:ai_read」只会让同一份数据出现第二种授权口径。
		permission.OrderList,
		mcp.Object("订单区间摘要参数", map[string]mcp.Schema{
			"projectId": mcp.String("站点工程 id（uuid）"),
			"from":      mcp.String("起始日，格式 YYYY-MM-DD，含当天"),
			"to":        mcp.String("结束日，格式 YYYY-MM-DD，含当天；不得早于起始日"),
		}, "projectId", "from", "to"),
		func(ctx context.Context, args ordersSummaryArgs) (mcp.Result, error) {
			res, err := summary.SummaryByRange(ctx, &orderdto.OrderRangeSummaryReq{
				ProjectID: args.ProjectID,
				From:      args.From,
				To:        args.To,
			})
			if err != nil {
				return mcp.Result{}, err
			}
			// 正文给模型读，Data 给渲染层用：模型不该去解析自己的表格，
			// 而渲染层也不该去正则匹配一句中文。
			return mcp.Result{
				Text: fmt.Sprintf("订单区间摘要（%s ~ %s，含当天）：订单 %d 单，其中计入消费 %d 单；净销售额 %s（%d 分）。",
					res.From, res.To, res.OrderCount, res.PaidOrderCount, res.NetSalesLabel, res.NetSales),
				Data: res,
			}, nil
		})
}
