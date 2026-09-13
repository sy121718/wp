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

	productcontract "go_wp/internal/module/product/contract"
	"go_wp/internal/templates"

	"github.com/google/uuid"
)

// variantSnapshotProvider 变体当前事实（价格 / 启用态）的只读端口。
//
// 直接复用订单域的 VariantSnapshotPort：它是「按变体 id（分单位）读当前事实」的收窄
// 只读能力，正是本片段需要的形状 —— 不为「读个价」再造一条几乎相同的端口，
// 端口数量本身就是维护成本。未注入（nil）时走降级：不提示。
var variantSnapshotProvider productcontract.VariantSnapshotPort

// SetVariantSnapshotProvider 注入变体快照端口（装配期调用；传 nil 表示未接入）。
func SetVariantSnapshotProvider(port productcontract.VariantSnapshotPort) {
	variantSnapshotProvider = port
}

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
	livePriceParamIDs      = "variantIds"
	livePriceParamPrices   = "prices"
	livePriceParamCurrency = "currency"
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
	currency := strings.TrimSpace(r.Params[livePriceParamCurrency])
	if currency == "" {
		currency = livePriceDefaultCurrency
	}
	if variantSnapshotProvider == nil {
		// 端口未接入：无从核对。降级为「不说话」，绝不把未知说成「价格没变」。
		return "", nil
	}
	ids := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		ids = append(ids, pair.id)
	}
	snaps, err := variantSnapshotProvider.VariantSnapshots(ctx, ids)
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
				VariantID: pair.id, Message: "该规格已下架，以结算为准", Changed: true,
			})
			continue
		}
		if !pair.hasPrice || pair.declared == snap.Price {
			// 一致 → 没有话要说；没烘构建期价 → 无从对比（此时展示当前价等于让页面上
			// 出现两个价，正是本方案要避免的）。
			continue
		}
		view.Items = append(view.Items, livePriceItem{
			VariantID: pair.id,
			Message:   "价格已更新为 " + currency + centsToYuanText(snap.Price) + "，以结算为准",
			Changed:   true,
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
// （strconv.FormatFloat(v,'f',-1,64)：99.50 元 → "99.5"）。
//
// 刻意不自己补两位小数：产物里那段价就是这个格式，两处格式不同会让「同一个价看起来变了」。
func centsToYuanText(cents int64) string {
	return strconv.FormatFloat(float64(cents)/100, 'f', -1, 64)
}
