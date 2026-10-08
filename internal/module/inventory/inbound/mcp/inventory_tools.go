package inventorymcp

// 为什么补这两个：用户问「VIT-C 还有多少货」「哪个仓快没了」「最近出库了多少」
// 时，答案全在 inventory_stocks / inventory_stock_movements 里，而在此之前
// AI 侧看不到任何库存数据 —— 商品的 product_find 只给一个「不跟踪 / 合计 / 未入库」
// 的三态摘要，具体到哪个仓、多少件是答不出来的。
//
// 只读：依赖收窄到 StockReader（两个方法），手里没有 AdjustStock ——
// 「AI 顺手调一次库存」不会在某次改动里悄悄变得可能，而库存调整会直接改可卖量。
//
// 两个工具而不是一个：库存「现在有多少」与流水「什么时候动的」是两种问题，
// 而流水表随时间线性增长（一次拉太多会挤掉回答），该分开限制条数。

// 为什么需要它：stock_change 的 reasonCode 只能从 ListReasons 里选，而内置的九个
// 覆盖的是通用场景（采购 / 退货 / 调拨 / 盘亏…）。真实运营很快会遇到「这个工程想要的
// 原因内置里没有」—— 在没有这个工具之前，模型唯一能做的是告诉用户「去后台加一个」，
// 而用户正在跟助手对话，他期待的就是别切页面。
//
// 两条来自 service 的硬约束，写进描述里：
//   · code 工程内唯一，且与内置占用**同一个命名空间** —— 撞名会拒；
//   · 内置原因**不能改名**（它的 i18n key 由系统按 code 派生，改了会两头对不上），
//     只能改启停与排序。所以 update 用之前要先看 isBuiltin。

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
	"go_wp/internal/module/inventory/contract"
	"go_wp/internal/module/inventory/dto"
	"go_wp/internal/module/inventory/enums"
	"go_wp/internal/permission"
	"go_wp/pkg/i18n"
)

const (
	stockDefaultLimit = 15
	stockMaxLimit     = 50
)

// Tools 返回库存模块的只读工具集。
func Tools(reader inventorycontract.StockReader) ([]mcp.Tool, error) {
	if reader == nil {
		return nil, errors.New("inventorymcp: 库存查询依赖缺失（装配期接线错误）")
	}
	return []mcp.Tool{stockFind(reader), warehouseList(reader)}, nil
}

// —— 库存 ——

type stockFindArgs struct {
	ProjectID   string `json:"projectId"`
	WarehouseID string `json:"warehouseId"`
	ProductID   string `json:"productId"`
	SKUCode     string `json:"skuCode"`
	ExternalSKU string `json:"externalSku"`
	Limit       int    `json:"limit"`
}

func stockFind(reader inventorycontract.StockReader) mcp.Tool {
	return mcp.New("stock_find", "查库存",
		"按线索查库存，用于回答「VIT-C 还有多少货」「这个商品在哪个仓有货」「哪个仓快没了」。\n"+
			"一行 = 一个仓库里一个 SKU 的存量；线索可以给 SKU 编码、商品 id、仓库 id 或外部编码，任一条都能单独用。\n"+
			"**必须看 trackQuantity**：它为 false 时这个 SKU 不跟踪数量（卖多少都不会少），"+
			"此时 quantity 的 0 表示「无限」而不是「没货」—— 说成缺货会让人去做一次没必要的补货。\n"+
			"要仓库清单用 warehouse_list。要「什么时候动的」用 stock_movements（本工具只有当前存量）。",
		permission.InventoryStockList,
		mcp.Object("查库存参数", map[string]mcp.Schema{
			"projectId":   mcp.String("站点工程 id（uuid）"),
			"warehouseId": mcp.String("限定某个仓库（可选）"),
			"productId":   mcp.String("限定某个商品（可选）"),
			"skuCode":     mcp.String("SKU 编码（可选）"),
			"externalSku": mcp.String("仓库侧的外部 / 第三方编码（可选）"),
			"limit":       mcp.Integer("最多返回几行，默认 15，上限 50"),
		}, "projectId"),
		func(ctx context.Context, args stockFindArgs) (mcp.Result, error) {
			if strings.TrimSpace(args.ProjectID) == "" {
				return mcp.Result{}, &mcp.ArgsError{Msg: "projectId 必填；先用 site_projects 查站点工程 id"}
			}
			list, err := reader.ListStocks(ctx, &inventorydto.ListStockReq{
				ProjectID:   args.ProjectID,
				WarehouseID: args.WarehouseID,
				ProductID:   args.ProductID,
				SKUCode:     args.SKUCode,
				ExternalSKU: args.ExternalSKU,
				Page:        1,
				Size:        clampLimit(args.Limit),
			})
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: stockListText(list, args), Data: list}, nil
		})
}

func stockListText(list []*inventorydto.StockResp, args stockFindArgs) string {
	scope := describeStockScope(args)
	if len(list) == 0 {
		// 「没有库存行」与「这个 SKU 不存在」是两件事，但对用户的下一步动作是同一句：
		// 去商品 / 仓库那边确认。所以这里把替代解释一并说清，而不是只回一个空。
		return fmt.Sprintf("没有查到库存行（%s）。可能是这个 SKU 还没有入库记录，"+
			"也可能是筛选条件写得太窄 —— 用 product_find 或 warehouse_list 确认线索是否对得上。", scope)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "查到 %d 行库存（%s）：\n", len(list), scope)
	for i, s := range list {
		fmt.Fprintf(&b, "%d. %s · %s · %s%s\n",
			i+1, emptyAsDash(s.SKUCode), warehouseLabel(s), quantityText(s), externalSkuSuffix(s))
		// 这一行必须给出 **variantId**：它是下一个动作的钥匙，而且只能从库存行上拿到。
		// 实测踩到过 —— 模型用 product_find 找到商品、用 stock_find 找到库存行，
		// 但正文里只有 SKU 没有变体 id，于是它只能停下来说「你给我一下变体 id」。
		// 「工具的输出要让下一步动作有可能完成」在这里就是这一行。
		ids := "      变体 " + emptyAsDash(s.VariantID)
		if s.ProductID != "" {
			ids += " · 商品 " + s.ProductID
		}
		b.WriteString(ids + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func describeStockScope(args stockFindArgs) string {
	parts := make([]string, 0, 4)
	if args.WarehouseID != "" {
		parts = append(parts, "仓库 "+args.WarehouseID)
	}
	if args.ProductID != "" {
		parts = append(parts, "商品 "+args.ProductID)
	}
	if args.SKUCode != "" {
		parts = append(parts, "SKU "+args.SKUCode)
	}
	if args.ExternalSKU != "" {
		parts = append(parts, "外部编码 "+args.ExternalSKU)
	}
	if len(parts) == 0 {
		return "全部仓库"
	}
	return strings.Join(parts, "、")
}

func warehouseLabel(s *inventorydto.StockResp) string {
	name := firstNonEmpty(s.WarehouseName, s.WarehouseCode, s.WarehouseID)
	return name
}

// quantityText 存量 + 是否跟踪。
//
// 这两个必须一起说：quantity = 0 有两义（跟踪且卖光 / 不跟踪 = 无限），
// 只读数量会把「无限」在回答里写成「没货」。
func quantityText(s *inventorydto.StockResp) string {
	if !s.TrackQuantity {
		return "不跟踪数量（可无限售出）"
	}
	return fmt.Sprintf("%d 件", s.Quantity)
}

func externalSkuSuffix(s *inventorydto.StockResp) string {
	if strings.TrimSpace(s.ExternalSKU) == "" {
		return ""
	}
	return " · 外部编码 " + s.ExternalSKU
}

// —— 仓库 ——

type warehouseListArgs struct {
	ProjectID string `json:"projectId"`
}

func warehouseList(reader inventorycontract.StockReader) mcp.Tool {
	return mcp.New("warehouse_list", "仓库清单",
		"列出站点工程的仓库，用于回答「有几个仓」「默认仓是哪个」「有第三方仓吗」。\n"+
			"库存工具（stock_find）的结果里带仓库名，但用户会直接问仓库本身 —— "+
			"而且一个仓库还没建任何库存时，只有这个工具答得出来。",
		permission.InventoryWarehouseList,
		mcp.Object("列仓库参数", map[string]mcp.Schema{
			"projectId": mcp.String("站点工程 id（uuid）"),
		}, "projectId"),
		func(ctx context.Context, args warehouseListArgs) (mcp.Result, error) {
			if strings.TrimSpace(args.ProjectID) == "" {
				return mcp.Result{}, &mcp.ArgsError{Msg: "projectId 必填；先用 site_projects 查站点工程 id"}
			}
			list, err := reader.ListWarehouses(ctx, &inventorydto.ListWarehouseReq{ProjectID: args.ProjectID})
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: warehouseListText(list), Data: list}, nil
		})
}

func warehouseListText(list []*inventorydto.WarehouseResp) string {
	if len(list) == 0 {
		return "这个工程还没有建仓库。"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "共 %d 个仓库：\n", len(list))
	for i, w := range list {
		fmt.Fprintf(&b, "%d. id=%s %s（编码 %s，类型 %s，状态 %s）%s\n",
			i+1, w.ID, emptyAsDash(w.Name), emptyAsDash(w.Code),
			emptyAsDash(w.Type), emptyAsDash(w.Status), defaultSuffix(w.IsDefault))
	}
	return strings.TrimRight(b.String(), "\n")
}

func defaultSuffix(isDefault bool) string {
	if isDefault {
		return " —— **默认仓**"
	}
	return ""
}

// —— 共用 ——

func clampLimit(n int) int {
	if n <= 0 {
		return stockDefaultLimit
	}
	if n > stockMaxLimit {
		return stockMaxLimit
	}
	return n
}

func emptyAsDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return "-"
}

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
