// live_price.go — 商品实时价格核对片段（BIZ-2）。
//
// 一个 capability：productLivePrice GET anonymous —— 按变体 id 核对「静态产物里的价」
// 与「商品库里的当前价」，不一致时给访客一句明确的交代。
//
// **为什么是核对而不是直接展示当前价**（两种语义二选一，这里选第二种）：
//
//	· 定价工具（AGENTS.md 商品模块 #13）改价**只落库、不进构建管线** —— 售价写回
//	  product_variants.price 之后，已发布的产物仍是按旧价渲染的那份字节，直到该页面
//	  被重建。于是「产物里的价」与「库里的价」在时间窗内必然可能不一致，这是设计使然，
//	  不是 bug；
//	· 若片段直接展示当前价，页面上就会同时存在两个价（产物里那段 + 片段这段），
//	  两个都「没错」——访客不知道该信哪个；而且产物里那段价还进了结构化数据与分享卡片，
//	  片段改了反而让页面自相矛盾；
//	· 核对方案只做一件产物做不到的事：**把事实说清楚**（价格已更新，以结算为准）。
//	  商品列表与详情页的价格展示、排序、筛选口径一个字都不动，产物仍可被 CDN 缓存与索引。
//
// 构建期价格怎么来的：组件在烘变体 id 的同时把**当时那份价（分）**一起拼进片段 URL
// （见 product/productselector 组件的 LivePriceGet），片段据此逐变体比对。
//
// 降级：端口未注入 / 变体查不到 / 没烘构建期价格 → 返回**空片段**（不报错、不 500）。
// 空片段在这个设计下是正确结果而不是失败：目标节点是紧贴价格旁的提示位（初始为空），
// 没有话说就什么都不显示，访客看到的仍是产物里的构建期价格。
package runtimefragment

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"go_wp/internal/builder/core"
	productcontract "go_wp/internal/module/product/contract"
	rfenums "go_wp/internal/module/runtimefragment/enums"
	"go_wp/internal/templates"
	"go_wp/pkg/money"

	"github.com/google/uuid"
)

func init() {
	Register(Spec{
		Type:   "productLivePrice",
		Method: "GET",
		Auth:   AuthAnonymous,
		Render: renderProductLivePrice,
	})
}

// 参数名。
const (
	livePriceParamIDs = "variantIds"
	// livePriceParamProjectID 站点工程 id（**实例配置**，构建期由商品组件烘进 URL，见
	// builder/components/product/jet.go 的 livePriceFragmentURL）。
	//
	// 为什么工程必须进 URL 而不是由片段自己反查：runtimefragment.Request 里没有工程字段
	// （只有 Params / Cookies / UserID），而「建一个只服务价格核对的工程反查端口」等于
	// 让片段自己猜工程 —— 猜错的后果是跨工程读价，方向与 DB-009 相反。所以由构建期
	// （唯一知道工程的地方）把工程烘进产物，片段只负责读它。
	//
	// 与 productList 片段的 projectId 同名同值、同源：都取构建上下文里的 ProjectID。
	livePriceParamProjectID = "projectId"
	livePriceParamPrices    = "prices"
	livePriceParamCurrency  = "currency"
	// livePriceDefaultCurrency 货币符号缺省值（与商品组件侧同一口径；组件会把实际符号传进来）。
	livePriceDefaultCurrency = "¥"
)

// livePriceItem 单个变体要给访客看的一句话（空 = 无话可说）。
type livePriceItem struct {
	VariantID string
	Message   string
	Changed   bool
}

// livePriceView 片段模板数据。
type livePriceView struct {
	Items []livePriceItem
}

// renderProductLivePrice 渲染价格核对结论。
func renderProductLivePrice(ctx context.Context, r *Request) (string, error) {
	if r == nil {
		return "", fmt.Errorf("片段请求为空")
	}
	pairs := livePricePairs(r)
	if len(pairs) == 0 {
		// 参数为空 / 全是非法 id 形状：没有可核对的对象，输出空片段（调用方保留产物里的价）。
		return "", nil
	}
	// 工程作用域：有可核对对象才要求它 —— 参数为空 / 全是非法 id 时上面已经沉默返回，
	// 那种请求本来就没有结论可言（不把参数缺失与「没对象」混成同一个 500）。
	//
	// 缺工程时**显式报错**（与 product_list 的 projectId 同一条形状）：产物里的 URL 没带
	// 工程是组件升级缺口，报错能立刻看见；若降级沉默，表现会是「价格核对永远没有结论」——
	// 那正是本批要消灭的 fail-silent。
	projectID := strings.TrimSpace(r.Params[livePriceParamProjectID])
	if projectID == "" {
		return "", fmt.Errorf("缺少参数 %s", livePriceParamProjectID)
	}
	currency := strings.TrimSpace(r.Params[livePriceParamCurrency])
	if currency == "" {
		currency = livePriceDefaultCurrency
	}
	if deps.VariantSnapshotProvider == nil {
		// 端口未接入：无从核对。降级为「不说话」，绝不把未知说成「价格没变」。
		return "", nil
	}
	ids := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		ids = append(ids, pair.id)
	}
	// 工程进上下文（与 product_list 片段同一形状）：端口本身按形参取作用域，上下文里
	// 也带上工程，供下游任何按 core.BuildProjectID(ctx) 取数的路径使用 —— 一处读、一处传，
	// 不让「工程从哪来」在调用链上分叉。
	scoped := core.WithBuildProjectID(ctx, projectID)
	snaps, err := deps.VariantSnapshotProvider.VariantSnapshots(scoped, ids, projectID)
	if err != nil {
		return "", err
	}
	byID := make(map[string]*productcontract.VariantSnapshot, len(snaps))
	for _, s := range snaps {
		if s != nil && s.VariantID != "" {
			byID[s.VariantID] = s
		}
	}
	view := livePriceView{Items: make([]livePriceItem, 0, len(pairs))}
	for _, pair := range pairs {
		snap, ok := byID[pair.id]
		if !ok {
			// 查不到（已删除 / 跨工程）：不提示 —— 把未知说成「价格已更新」是假信息。
			continue
		}
		if !snap.Enabled {
			// 已下架是产物里没有的事实，且与「价格对不对」无关，独立提示。
			view.Items = append(view.Items, livePriceItem{
				VariantID: pair.id,
				Message:   r.tr(rfenums.LivePriceDelisted, "该规格已下架，以结算为准"),
				Changed:   true,
			})
			continue
		}
		if !pair.hasPrice || pair.declared == snap.Price {
			// 一致 → 没有话要说；没烘构建期价 → 无从对比（此时展示当前价等于让页面上
			// 出现两个价，正是本方案要避免的）。
			continue
		}
		amount := currency + centsToYuanText(snap.Price)
		view.Items = append(view.Items, livePriceItem{
			VariantID: pair.id,
			Message: fmt.Sprintf(
				r.tr(rfenums.LivePriceUpdated, "价格已更新为 %s，以结算为准"), amount),
			Changed: true,
		})
	}
	if len(view.Items) == 0 {
		return "", nil
	}
	return templates.RenderFragment("product_live_price", view)
}

// livePricePair 一个变体 id 与它对应的「产物里的价」（分）。
type livePricePair struct {
	id       string
	declared int64
	hasPrice bool
}

// livePricePairs 解析变体 id 与构建期价格，并**丢弃非法 id 形状**。
//
// 形状过滤不能省：片段参数来自 URL（任何人都能写 variantIds=abc），而 VariantSnapshots
// 端口会把 id 直接带进 uuid 列的 `WHERE id IN (?)` —— PostgreSQL 遇到非 uuid 字面量直接报
// invalid input syntax for type uuid（SQLSTATE 22P02），一个手写 URL 就能把页面打成 500。
// 端口本身不加校验是合理的（它的既有消费方传的是自己刚读出来的 id），所以入口这一层兜。
//
// 过滤时按**位置**配对：prices 与 variantIds 同序，丢一个 id 必须同时丢掉它那份价，
// 否则后面每个变体都会拿到上一个人的价，变成一片假提示。
func livePricePairs(r *Request) []livePricePair {
	if r == nil {
		return nil
	}
	rawIDs := splitVariantIDs(r.Params[livePriceParamIDs])
	prices := splitDeclaredPrices(r.Params[livePriceParamPrices])
	out := make([]livePricePair, 0, len(rawIDs))
	for i, id := range rawIDs {
		if _, err := uuid.Parse(id); err != nil {
			continue
		}
		pair := livePricePair{id: id}
		if cents, ok := declaredPriceAt(prices, i); ok {
			pair.declared, pair.hasPrice = cents, true
		}
		out = append(out, pair)
	}
	return out
}

// splitDeclaredPrices 解析「构建期价格」参数：逗号分隔的**分**值，与 variantIds 同序同长。
//
// 用分而不是元：跨模块边界的金额一律整数分（与 VariantSnapshotPort 同口径），
// 少一次浮点解析就少一次「99.90 被读成 99.9」这类对不上的假提示。
// 解析失败的项记 noDeclaredPrice，调用方按「没给这个变体的价」处理。
func splitDeclaredPrices(raw string) []int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]int64, 0, len(parts))
	for _, part := range parts {
		token := strings.TrimSpace(part)
		if token == "" {
			out = append(out, noDeclaredPrice)
			continue
		}
		cents, err := strconv.ParseInt(token, 10, 64)
		if err != nil || cents < 0 {
			out = append(out, noDeclaredPrice)
			continue
		}
		out = append(out, cents)
	}
	return out
}

// noDeclaredPrice 构建期价格缺失的标记（价格不可能为负，负值安全地承担这个语义）。
const noDeclaredPrice int64 = -1

// declaredPriceAt 取第 i 个变体的构建期价格。
func declaredPriceAt(prices []int64, i int) (cents int64, ok bool) {
	if i < 0 || i >= len(prices) {
		return 0, false
	}
	if prices[i] == noDeclaredPrice {
		return 0, false
	}
	return prices[i], true
}

// centsToYuanText 分 → 元展示文本，与商品字段解析器 formatPrice **同一口径**
// （99.50 元 → "99.5"）。
//
// 刻意不自己补两位小数：产物里那段价就是这个格式，两处格式不同会让「同一个价看起来变了」。
// 实现收敛到 pkg/money.CentsToYuanText（审计 CQ-013：四处金额格式化里的「分 → 元」
// 那一路，与展示口径共用同一个底层格式化）。
func centsToYuanText(cents int64) string {
	return money.CentsToYuanText(cents)
}
