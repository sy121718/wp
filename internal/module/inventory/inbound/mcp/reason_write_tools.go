package inventorymcp

// reason_write_tools.go — 自定义库存变动原因的增改（stock_change 的前置能力）。
//
// 为什么需要它：stock_change 的 reasonCode 只能从 ListReasons 里选，而内置的九个
// 覆盖的是通用场景（采购 / 退货 / 调拨 / 盘亏…）。真实运营很快会遇到「这个工程想要的
// 原因内置里没有」—— 在没有这个工具之前，模型唯一能做的是告诉用户「去后台加一个」，
// 而用户正在跟助手对话，他期待的就是别切页面。
//
// 两条来自 service 的硬约束，写进描述里：
//   · code 工程内唯一，且与内置占用**同一个命名空间** —— 撞名会拒；
//   · 内置原因**不能改名**（它的 i18n key 由系统按 code 派生，改了会两头对不上），
//     只能改启停与排序。所以 update 用之前要先看 isBuiltin。

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go_wp/internal/mcp"
	inventorycontract "go_wp/internal/module/inventory/contract"
	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	"go_wp/internal/permission"
)

// ReasonTools 返回变动原因字典的写工具集。
func ReasonTools(w inventorycontract.ReasonWriter) ([]mcp.Tool, error) {
	if w == nil {
		return nil, errors.New("inventorymcp: 变动原因写依赖缺失（装配期接线错误）")
	}
	return []mcp.Tool{reasonCreate(w), reasonUpdate(w)}, nil
}

type reasonCreateArgs struct {
	ProjectID string `json:"projectId"`
	Code      string `json:"code"`
	Name      string `json:"name"`
	Direction string `json:"direction"`
	Sort      int    `json:"sort"`
}

func reasonCreate(w inventorycontract.ReasonWriter) mcp.Tool {
	return mcp.NewWrite("inventory_reason_create", "新建库存变动原因",
		"给某个工程新增一个自定义的库存变动原因，之后 stock_change 的 reasonCode 就能用它。\n"+
			"先想清楚方向再建：**direction 一经确定不可改**（同一个 code 在入库与出库两个方向上是两个不同的业务含义，"+
			"让它可改等于让历史流水的解释跟着变）。入/出都用得上就建两条。\n"+
			"code 只能用小写字母、数字、下划线（1–32 个字符），且在**整个工程内唯一** —— "+
			"内置原因也占这个命名空间，叫 purchase_in 之类会被拒。\n"+
			"建之前先用 stock_reasons 看看内置的够不够用：能用内置就别新建，少一个自定义原因就少一处将来对不上账的地方。",
		permission.InventoryReasonCreate,
		mcp.Object("新建变动原因参数", map[string]mcp.Schema{
			"projectId": mcp.String("站点工程 id（uuid，必填）"),
			"code":      mcp.String("原因代码（小写字母 / 数字 / 下划线，1–32 字符，工程内唯一且不能与内置撞名）"),
			"name":      mcp.String("原因名称（后台下拉里显示给人看的中文，如「门店自提出库」）"),
			"direction": mcp.Enum("适用方向：in=入库，out=出库，adjust=盘点调整（建后不可改）",
				inventoryenums.DirectionIn, inventoryenums.DirectionOut, inventoryenums.DirectionAdjust),
			"sort": mcp.Integer("排序值，越小越靠前（可选，默认 0）"),
		}, "projectId", "code", "name", "direction"),
		nil,
		func(ctx context.Context, args reasonCreateArgs) (mcp.Result, error) {
			res, err := w.CreateReason(ctx, &inventorydto.CreateReasonReq{
				ProjectID: strings.TrimSpace(args.ProjectID),
				Code:      strings.TrimSpace(args.Code),
				Name:      strings.TrimSpace(args.Name),
				Direction: strings.TrimSpace(args.Direction),
				Sort:      args.Sort,
			})
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: createdReasonText(res)}, nil
		})
}

// createdReasonText 建原因的回执。
//
// 必须把 code 原样写出来：下一步 stock_change 要用的正是这个字符串，
// 而它可能被服务端归一（转小写、去空白）—— 让模型复述自己传进去的那个写法，
// 下一次调用就会因为大小写对不上而失败。
func createdReasonText(res *inventorydto.ReasonResp) string {
	if res == nil {
		return "已新建变动原因。"
	}
	var b strings.Builder
	// 用 reasonName 而不是 res.Name：自定义原因的 Name 在库里存的是
	// inventory.reason.custom.<工程>.<code> 这个 i18n key（与内置原因同一形态），
	// 直接印出来用户看到的就是一串路径，认不出自己刚建了什么。
	fmt.Fprintf(&b, "已新建变动原因「%s」（code = %s，方向 %s）。",
		reasonName(res), strings.TrimSpace(res.Code), directionText(res.Direction))
	b.WriteString("\n之后变动库存时把 reasonCode 传成 ")
	b.WriteString(strings.TrimSpace(res.Code))
	b.WriteString(" 就能选到它。")
	return b.String()
}

type reasonUpdateArgs struct {
	ReasonID  string `json:"reasonId"`
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	Sort      *int   `json:"sort"`
}

func reasonUpdate(w inventorycontract.ReasonWriter) mcp.Tool {
	return mcp.NewWrite("inventory_reason_update", "修改库存变动原因",
		"改一个已有变动原因的名称、启停或排序。\n"+
			"**内置原因改不了名字**（它的词条 key 由系统按 code 派生），只能改 status 与 sort；"+
			"要改内置的名字请直接告诉用户「这个改不了，需要的话新建一个自定义的」。\n"+
			"**停用不是删除**：停用后它不再出现在新建变动的可选列表里，但历史流水仍然引用它的 code，"+
			"所以旧记录照常能显示 —— 回答里别把它说成「删掉了」。\n"+
			"reasonId 用 stock_reasons 拿到的 id（不是 code）。",
		permission.InventoryReasonUpdate,
		mcp.Object("修改变动原因参数", map[string]mcp.Schema{
			"reasonId":  mcp.String("要改的原因 id（用 stock_reasons 拿到的 id，不是 code）"),
			"projectId": mcp.String("站点工程 id（uuid，可选：只有一个工程时可以不带）"),
			"name":      mcp.String("新的原因名称（可选；内置原因改不了）"),
			"status":    mcp.Enum("启用状态（可选）：active=启用，disabled=停用（历史流水仍可显示）", inventoryenums.StatusActive, inventoryenums.StatusDisabled),
			"sort":      mcp.Integer("新的排序值（可选）"),
		}, "reasonId"),
		nil,
		func(ctx context.Context, args reasonUpdateArgs) (mcp.Result, error) {
			req := &inventorydto.UpdateReasonReq{
				ID:        strings.TrimSpace(args.ReasonID),
				ProjectID: strings.TrimSpace(args.ProjectID),
				Sort:      args.Sort,
			}
			// 指针字段：只有显式传了才带过去。
			// 传空字符串会让 service 把名字改成空 —— 那不是「没改」。
			if name := strings.TrimSpace(args.Name); name != "" {
				req.Name = &name
			}
			if status := strings.TrimSpace(args.Status); status != "" {
				req.Status = &status
			}
			if req.Name == nil && req.Status == nil && req.Sort == nil {
				return mcp.Result{}, &mcp.ArgsError{Msg: "至少要改一项：name / status / sort 三个都空的话这次调用没有意义。"}
			}
			res, err := w.UpdateReason(ctx, req)
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: updatedReasonText(args, res)}, nil
		})
}

// updatedReasonText 改原因的回执。
func updatedReasonText(args reasonUpdateArgs, res *inventorydto.ReasonResp) string {
	var b strings.Builder
	// 同上：优先用模型刚给的名字（那是人写的），否则回读服务端返回的 Name，
	// 但后者可能是 i18n key，要走 reasonName 翻一次。
	name := strings.TrimSpace(args.Name)
	if name == "" && res != nil {
		name = reasonName(res)
	}
	fmt.Fprintf(&b, "已更新变动原因「%s」。", name)
	if strings.EqualFold(strings.TrimSpace(args.Status), inventoryenums.StatusDisabled) {
		b.WriteString("\n它已停用：新建变动时选不到它，但引用它的历史流水照常显示 —— 数据没有动。")
	} else if strings.EqualFold(strings.TrimSpace(args.Status), inventoryenums.StatusActive) {
		b.WriteString("\n它已重新启用，新建变动时可以选到它了。")
	}
	return b.String()
}

// directionText 方向的中文说法（三种取值，与 enums 一一对应）。
func directionText(d string) string {
	switch strings.ToLower(strings.TrimSpace(d)) {
	case inventoryenums.DirectionIn:
		return "入库"
	case inventoryenums.DirectionOut:
		return "出库"
	case inventoryenums.DirectionAdjust:
		return "盘点调整"
	default:
		return d
	}
}
