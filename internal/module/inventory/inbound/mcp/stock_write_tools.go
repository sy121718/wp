package inventorymcp

// stock_write_tools.go — 库存变动（写）。
//
// 与只读工具分开成文件，是因为写工具多出两件只读工具不需要的事：
//   · 每一个都必须在说明里写清**不可逆的程度**（这条落在流水上，能改回来但留痕）；
//   · 入参必须先能被模型填对 —— 所以 stock_change 之前要有一个 stock_reasons
//     让它查得到合法 reasonCode，而不是猜一个然后被「原因与方向不符」挡回来。
//
// 幂等与确认位不在这里：mcp.NewWrite 会往 schema 追加 confirm / idempotencyKey
// 并在任何业务代码之前校验（internal/mcp/write.go）。

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
	"go_wp/pkg/i18n"
)

const (
	stockChangeMaxLines = 50
	// stockReasonsInlineMax 一次最多列多少个原因。可选项太多时模型容易挑第一个，
	// 而「采购入库」与「退货入库」在流水里的区别是有意义的。
	stockReasonsInlineMax = 40
)

// WriteTools 库存模块的写工具集。
//
// reader 与 writer 分开收：写工具也要读（stock_change 出错时要把可用的原因列出来，
// 否则模型只拿到一句「reasonCode 不合法」却不知道合法的是什么）。
func WriteTools(w inventorycontract.StockWriter, r inventorycontract.StockReader, store mcp.IdempotencyStore) ([]mcp.Tool, error) {
	if w == nil || r == nil {
		return nil, errors.New("inventorymcp: 库存写工具依赖缺失（装配期接线错误）")
	}
	return []mcp.Tool{
		stockReasons(r),
		stockChange(w, r, store),
	}, nil
}

// —— 读：变动原因字典 ——

type stockReasonsArgs struct {
	ProjectID string `json:"projectId"`
	Direction string `json:"direction"`
}

func stockReasons(r inventorycontract.StockReader) mcp.Tool {
	return mcp.New("stock_reasons", "查库存变动原因",
		"列出可用的库存变动原因（code 与中文名）。**做任何库存变动之前先调它**："+
			"ChangeStock 要求 reasonCode 与变动方向匹配，而可用的 code 由内置项与各工程自建项共同决定，"+
			"猜一个会被拒。direction 可以不传（不传则三个方向的原因都列出来）。",
		permission.InventoryReasonList,
		mcp.Object("查变动原因参数", map[string]mcp.Schema{
			"projectId": mcp.String("站点工程 id（uuid）"),
			"direction": mcp.Enum("只看某个方向的原因（可选）", inventoryenums.DirectionIn, inventoryenums.DirectionOut, inventoryenums.DirectionAdjust),
		}, "projectId"),
		func(ctx context.Context, args stockReasonsArgs) (mcp.Result, error) {
			list, err := r.ListReasons(ctx, &inventorydto.ListReasonReq{
				ProjectID: args.ProjectID,
				Direction: args.Direction,
			})
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: reasonsText(args.Direction, list), Data: list}, nil
		})
}

// reasonName 把原因名写成用户能读的样子。
//
// **内置原因的 Name 是 i18n key**（`inventory.reason.<code>`，见 service 的
// builtinReasonKey），后台页面靠 tr() 翻成中文；工具调用没有请求语言，
// 直接输出会把裸 key 显示给用户 —— 实测回答里出现的正是
// 「purchase_in — inventory.reason.purchase_in」这种谁也不想看的东西。
// 这里按默认语言翻一次；翻不到就退回 code（一串点分 key 比 code 更难认）。
func reasonName(it *inventorydto.ReasonResp) string {
	name := strings.TrimSpace(it.Name)
	if !strings.HasPrefix(name, "inventory.reason.") {
		return name
	}
	// i18n.Translate 未命中时**返回 key 本身**（不是空串）—— 判据必须同时排除它，
	// 否则「翻译失败」会被当成「翻出来就是这串 key」，裸 key 照样漏给用户。
	if translated := strings.TrimSpace(i18n.Translate(name, "", i18n.GetDefaultLang())); translated != "" && translated != name {
		return translated
	}
	return it.Code
}

func reasonsText(direction string, list []*inventorydto.ReasonResp) string {
	if len(list) == 0 {
		if direction == "" {
			return "这个工程没有可用的变动原因。请先在库存设置里建一个，再来做库存变动。"
		}
		return fmt.Sprintf("这个工程没有可用于方向 %q 的变动原因。去掉 direction 再查一次全部，"+
			"或者先在库存设置里建一个该方向的原因。", direction)
	}
	var b strings.Builder
	if direction == "" {
		fmt.Fprintf(&b, "共 %d 个可用原因（in=入库 / out=出库 / adjust=盘点）：\n", len(list))
	} else {
		fmt.Fprintf(&b, "方向 %s 可用 %d 个原因：\n", direction, len(list))
	}
	shown := list
	truncated := 0
	if len(shown) > stockReasonsInlineMax {
		truncated = len(shown) - stockReasonsInlineMax
		shown = shown[:stockReasonsInlineMax]
	}
	for _, it := range shown {
		if it == nil {
			continue
		}
		kind := "自建"
		if it.IsBuiltin {
			kind = "内置"
		}
		// id 必须给出来：inventory_reason_update 要的是 id 而不是 code，
		// 不印在这里，模型下一步就只能停下来问用户要（实测它说的正是
		// 「刚才 stock_reasons 返回里没给出它的 reasonId，我不能拿 code 代填」）。
		// 工具的输出里要带上**下一步动作需要的参数**，这条在 stock_find 漏 variantId
		// 那次已经吃过一回。
		fmt.Fprintf(&b, "- %s（%s，%s，id=%s）\n", it.Code, reasonName(it), kind, strings.TrimSpace(it.ID))
	}
	if truncated > 0 {
		fmt.Fprintf(&b, "……另有 %d 个未列出（原因很多时先按方向筛一次）。\n", truncated)
	}
	return strings.TrimRight(b.String(), "\n")
}

// —— 写：库存变动 ——

// stockChangeArgs stock_change 的业务入参。
//
// Lines 用**行数组**而不是「单商品单数量」：真实的入库常常一次进多个 SKU
// （一张采购单），拆成多次调用会生成多个 batch、流水里也看不出它们是一件事。
type stockChangeArgs struct {
	ProjectID   string            `json:"projectId"`
	WarehouseID string            `json:"warehouseId"`
	Direction   string            `json:"direction"`
	ReasonCode  string            `json:"reasonCode"`
	Remark      string            `json:"remark"`
	Lines       []stockChangeLine `json:"lines"`
}

type stockChangeLine struct {
	VariantID string `json:"variantId"`
	Quantity  int    `json:"quantity"`
	SKUCode   string `json:"skuCode"`
}

func stockChange(w inventorycontract.StockWriter, r inventorycontract.StockReader, store mcp.IdempotencyStore) mcp.Tool {
	return mcp.NewWrite("stock_change", "库存变动（入库 / 出库 / 盘点）",
		"对指定仓库执行一次库存变动，批量多行、整批同事务（任一行不合法则整批不生效）。\n"+
			"direction 的三种含义：in=入库（quantity 是本次增加量）、out=出库（quantity 是本次减少量，"+
			"库存不足会整批拒绝）、adjust=盘点（quantity 是**变动后的目标绝对量**，不是增减量）。\n"+
			"reasonCode 必须与 direction 匹配 —— 先调 stock_reasons 查可用取值，不要猜。\n"+
			"lines 里每行的 variantId 是**变体 id**（不是商品 id）：一个商品有多个变体时，库存是按变体记的，"+
			"不确定就先调 stock_find 或 product_find 拿到 variantId。\n"+
			"每一次变动都会写一条不可删除的库存流水（谁、什么时候、因为什么、变动前后各多少），"+
			"所以执行前请与用户确认清楚仓库、变体与数量。",
		permission.InventoryStockChange,
		mcp.Object("库存变动参数", map[string]mcp.Schema{
			"projectId":   mcp.String("站点工程 id（uuid）"),
			"warehouseId": mcp.String("仓库 id（必填）。不确定就先调 warehouse_list —— 只有一个仓时它会把默认仓标出来。"),
			"direction":   mcp.Enum("变动方向（必填）：in=入库 / out=出库 / adjust=盘点为目标量", inventoryenums.DirectionIn, inventoryenums.DirectionOut, inventoryenums.DirectionAdjust),
			"reasonCode":  mcp.String("变动原因 code（必填）。先调 stock_reasons 查；必须与 direction 匹配。"),
			"remark":      mcp.String("备注（可选）。写进流水，建议写明来源单据或这次变动的原因说明。"),
			"lines": mcp.Array("要变动的行（必填，1-50 行）", mcp.Object("一行", map[string]mcp.Schema{
				"variantId": mcp.String("变体 id（必填）"),
				"quantity":  mcp.Integer("数量（必填）：in/out 是增减量（正数），adjust 是目标绝对量（>= 0）"),
				"skuCode":   mcp.String("SKU 编码（可选，只用于流水里更好认）"),
			}, "variantId", "quantity")),
		}, "projectId", "warehouseId", "direction", "reasonCode", "lines"),
		store,
		func(ctx context.Context, args stockChangeArgs) (mcp.Result, error) {
			if len(args.Lines) == 0 {
				return mcp.Result{}, &mcp.ArgsError{Msg: "lines 至少要有一行"}
			}
			if len(args.Lines) > stockChangeMaxLines {
				return mcp.Result{}, &mcp.ArgsError{Msg: fmt.Sprintf("一次最多 %d 行（收到 %d）；分几次做，或者先确认是不是选错了范围",
					stockChangeMaxLines, len(args.Lines))}
			}
			lines := make([]inventorydto.StockChangeLineReq, 0, len(args.Lines))
			for i, line := range args.Lines {
				if strings.TrimSpace(line.VariantID) == "" {
					return mcp.Result{}, &mcp.ArgsError{Msg: fmt.Sprintf("第 %d 行缺 variantId", i+1)}
				}
				// adjust 允许 0（清零），in/out 必须是正数：负数在 in/out 下会产生
				// 方向相反的效果，而那与调用者写下的 direction 矛盾 —— 流水里
				// 会记成「入库 -5」，事后没人能一眼看出它其实是一次出库。
				if args.Direction == inventoryenums.DirectionAdjust {
					if line.Quantity < 0 {
						return mcp.Result{}, &mcp.ArgsError{Msg: fmt.Sprintf("第 %d 行：盘点（adjust）的 quantity 是目标量，不能为负", i+1)}
					}
				} else if line.Quantity <= 0 {
					return mcp.Result{}, &mcp.ArgsError{Msg: fmt.Sprintf("第 %d 行：%s 的 quantity 必须是正数（方向已经由 direction 决定）", i+1, args.Direction)}
				}
				lines = append(lines, inventorydto.StockChangeLineReq{
					WarehouseID: args.WarehouseID,
					VariantID:   line.VariantID,
					SKUCode:     line.SKUCode,
					Quantity:    line.Quantity,
				})
			}
			res, err := w.ChangeStock(ctx, &inventorydto.ChangeStockReq{
				ProjectID:   args.ProjectID,
				WarehouseID: args.WarehouseID,
				Direction:   args.Direction,
				ReasonCode:  args.ReasonCode,
				SourceType:  "manual",
				Remark:      args.Remark,
				Lines:       lines,
			})
			if err != nil {
				return mcp.Result{}, teachReasonMismatch(ctx, r, args, err)
			}
			return mcp.Result{Text: changeResultText(args.Direction, res), Data: res}, nil
		},
	)
}

// teachReasonMismatch 在 reasonCode 被拒时把可用取值附在错误后面。
//
// 为什么值得单独做这一步：ChangeStock 拒绝原因是笼统的（「原因与方向不符」或
// 「原因 code 不合法」），而模型手里只有这一句话 —— 它的下一步通常是**重猜一次**，
// 于是同一次入库会连着失败两三次。把该方向的可用 code 直接写进错误里，
// 模型（和读日志的人）第一次就知道正确的是什么。
//
// 只在「疑似原因错误」时去查一次字典：其它错误（库存不足、仓库不存在）
// 附一份原因清单只会让人以为是原因的问题。
func teachReasonMismatch(ctx context.Context, r inventorycontract.StockReader, args stockChangeArgs, cause error) error {
	msg := cause.Error()
	if !strings.Contains(msg, "reason") && !strings.Contains(msg, "原因") {
		return cause
	}
	list, lerr := r.ListReasons(ctx, &inventorydto.ListReasonReq{
		ProjectID: args.ProjectID,
		Direction: args.Direction,
	})
	if lerr != nil || len(list) == 0 {
		return cause
	}
	codes := make([]string, 0, len(list))
	for _, it := range list {
		if it != nil {
			codes = append(codes, it.Code)
		}
	}
	return fmt.Errorf("%w（方向 %s 可用的 reasonCode：%s）", cause, args.Direction, strings.Join(codes, " / "))
}

// changeResultText 把变动结果写成模型能直接引用的几句。
//
// 逐行给出**变动前后**：只报「已入库 5 件」时，用户问「现在有多少」还得再查一次；
// 而 before/after 就在返回体里。
func changeResultText(direction string, res *inventorydto.StockChangeResp) string {
	if res == nil || len(res.Movements) == 0 {
		return "库存变动已提交，但没有返回流水明细 —— 请调 stock_find 确认当前数量。"
	}
	verb := map[string]string{
		inventoryenums.DirectionIn:     "入库",
		inventoryenums.DirectionOut:    "出库",
		inventoryenums.DirectionAdjust: "盘点调整",
	}[direction]
	if verb == "" {
		verb = direction
	}
	var b strings.Builder
	fmt.Fprintf(&b, "已完成%s，共 %d 行（批次 %s）：\n", verb, len(res.Movements), res.BatchID)
	for _, m := range res.Movements {
		if m == nil {
			continue
		}
		name := m.SKUCode
		if name == "" {
			name = m.VariantID
		}
		// 盘点要额外说清「从多少变成多少」——它的 quantity 是目标量，
		// 只说目标值时用户读不出这次到底动了多少。
		fmt.Fprintf(&b, "- %s：%d → %d（%+d）", name, m.QuantityBefore, m.QuantityAfter, m.Delta)
		if m.WarehouseName != "" {
			fmt.Fprintf(&b, "，仓库 %s", m.WarehouseName)
		}
		b.WriteString("\n")
	}
	if !res.CacheSynced {
		// 缓存没同步上不是致命错误（下次写回会纠），但必须说 —— 否则搜索/列表页
		// 显示的库存会与真实值短暂不一致。
		fmt.Fprintf(&b, "注意：有 %d 处汇总缓存未同步（%s），列表页的库存数可能要过一会儿才更新。",
			len(res.CacheFailures), strings.Join(res.CacheFailures, "、"))
	}
	return strings.TrimRight(b.String(), "\n")
}
