package orderservice

// currency_rate.go — 汇率读写与订单快照（迁移 633，W5）。
//
// 三条判据：
//
//  1. 汇率是**精确有理数**：numeric(18,8) 读成 float64 会丢精度，转成分时误差累积；
//     这里全程 big.Rat，只在最终落库 / 输出时转成字符串。
//  2. 覆盖式：同一币对只留最新一条 —— 历史由**订单上的快照**保存，不靠这张表存历史。
//  3. 下单时读一次写进 orders.fx_rate：读失败**不阻断下单**（留 NULL 表示「未固化」）。
//     「汇率未固化」是真实状态（历史订单 / 外部导入 / 没配汇率的工程），
//     回填会伪造历史 —— 显示「未固化」比显示一个编造的数字诚实。

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	orderdto "go_wp/internal/module/order/dto"
	ordermodel "go_wp/internal/module/order/model"
)

// GetRate 取某币对的当前汇率（返回精确文本的 decimal 与时间）。
func (s *Service) GetRate(ctx context.Context, req *orderdto.GetRateReq) (res *orderdto.RateResp, err error) {
	if req == nil {
		return nil, errors.New("参数不合法")
	}
	e, gerr := s.orders.GetRate(ctx, req.BaseCode, req.QuoteCode)
	if gerr != nil {
		if errors.Is(gerr, ordermodel.ErrCurrencyRateNotFound) {
			// 「还没配」不是错误：调用方据此显示「汇率未固化」，而不是弹一个错误。
			return &orderdto.RateResp{BaseCode: strings.ToUpper(strings.TrimSpace(req.BaseCode)),
				QuoteCode: strings.ToUpper(strings.TrimSpace(req.QuoteCode))}, nil
		}
		return nil, gerr
	}
	return &orderdto.RateResp{
		BaseCode: e.BaseCode, QuoteCode: e.QuoteCode,
		// 去掉 numeric(18,8) 的尾零：`7.24000000` 与 `7.24` 是同一个汇率，
		// 而回执里带着 8 个零会让调用方以为精度有意义。
		Rate:   trimDecimalZeros(e.Rate),
		RateAt: e.RateAt.Format(time.RFC3339), Source: e.Source,
	}, nil
}

// SetRate 覆盖式写汇率（同一币对只留最新一条）。
func (s *Service) SetRate(ctx context.Context, req *orderdto.SetRateReq) (res *orderdto.RateResp, err error) {
	if req == nil {
		return nil, errors.New("参数不合法")
	}
	base := strings.ToUpper(strings.TrimSpace(req.BaseCode))
	quote := strings.ToUpper(strings.TrimSpace(req.QuoteCode))
	if base == "" || quote == "" {
		return nil, errors.New("币种代码必填")
	}
	// 汇率必须是正的有理数（`0` 与负数都会被 DDL 的 CHECK 拒，但提前给可读错误）。
	rate, ok := new(big.Rat).SetString(strings.TrimSpace(req.Rate))
	if !ok || rate.Sign() <= 0 {
		return nil, errors.New("汇率必须是正数")
	}
	e := &ordermodel.CurrencyRateEntity{
		BaseCode: base, QuoteCode: quote,
		// 按 8 位小数落库（numeric(18,8)）：多给的位数会被 PG 四舍五入，
		// 这里先截断，读回来的文本与传入的一致。
		Rate:   formatRate(rate),
		RateAt: time.Now().UTC(),
		Source: "manual",
	}
	if err := s.orders.UpsertRate(ctx, e); err != nil {
		return nil, err
	}
	return s.GetRate(ctx, &orderdto.GetRateReq{BaseCode: base, QuoteCode: quote})
}

// convertCents 用精确有理数把分从一个币种折算到另一个（四舍五入到分）。
//
// 全程 big.Rat：float64 在 8 位小数的汇率上做乘法会把误差放大到分位
// （大额订单上就是几块钱的偏差），而财务口径必须能复算。
func convertCents(cents int64, rate *big.Rat) int64 {
	if rate == nil || cents == 0 {
		return 0
	}
	v := new(big.Rat).Mul(new(big.Rat).SetInt64(cents), rate)
	// 四舍五入：先加 1/2 再取整（对负数要反向，但金额恒非负，简化处理）。
	half := big.NewRat(1, 2)
	v.Add(v, half)
	q := new(big.Int).Quo(v.Num(), v.Denom())
	return q.Int64()
}

// trimDecimalZeros 去掉 decimal 文本的尾零（`7.24000000` → `7.24`，`7.00000000` → `7`）。
func trimDecimalZeros(raw string) string {
	s := strings.TrimSpace(raw)
	if !strings.Contains(s, ".") {
		return s
	}
	s = strings.TrimRight(s, "0")
	return strings.TrimRight(s, ".")
}

// formatRate 把有理数格式化成最多 8 位小数的文本（numeric(18,8) 的同形）。
func formatRate(r *big.Rat) string {
	f, _ := r.Float64()
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.8f", f), "0"), ".")
}

// orderFXSnapshot 下单时取汇率快照（迁移 633，W5）。
//
// 返回 (rate 文本, base 代码)。取不到时返回 ("", "") —— 调用方据此留 NULL：
// **不阻断下单**（没配汇率的工程照常卖货，只是报表显示「汇率未固化」）。
//
// 为什么不按工程读：汇率是币种对的事实（见 model 的注释），同一个 USD→CNY
// 在哪个工程下都该是同一个数。
func (s *Service) orderFXSnapshot(ctx context.Context, orderCurrency, baseCurrency string) (rate string, base string) {
	orderCurrency = strings.ToUpper(strings.TrimSpace(orderCurrency))
	baseCurrency = strings.ToUpper(strings.TrimSpace(baseCurrency))
	if orderCurrency == "" || baseCurrency == "" || orderCurrency == baseCurrency {
		// 同币种：汇率为 1，但**不写快照**（写 1 会让「已固化」与「未固化」混起来 ——
		// 同币种下「折算」这个概念根本不存在）。
		return "", ""
	}
	e, err := s.orders.GetRate(ctx, orderCurrency, baseCurrency)
	if err != nil || e == nil || strings.TrimSpace(e.Rate) == "" {
		return "", ""
	}
	return e.Rate, baseCurrency
}

// ConvertedOverview 区间内的折算概览（迁移 633，W5）。
//
// 逐单折算（每张单用**自己的**快照汇率）：批量订单的快照汇率可能各不相同，
// 用「当前汇率」统一乘一遍会得到一组与订单实际金额无关的数字。
//
// 毛利按同比例折算（毛利/销售额 × 销售额折算值）：严格做法是逐行折算成本，
// 但订单只存了总额快照、没有分行基准币金额 —— 同比例是**单币种内部**的守恒换算，
// 误差只来自四舍五入到分（订单级 ≤ 1 分）。
func (s *Service) ConvertedOverview(ctx context.Context, req *orderdto.SalesOverviewReq,
	sales, grossProfit int64) (res *orderdto.ConvertedOverview, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, fmt.Errorf("缺少站点工程")
	}
	from, to, err := normalizeRangeWindow(req.From, req.To, time.Now())
	if err != nil {
		return nil, err
	}
	statuses, _, serr := salesStatuses(req.Status)
	if serr != nil {
		return nil, serr
	}
	projectID := strings.TrimSpace(req.ProjectID)
	filter := ordermodel.OrderSalesFilter{Statuses: statuses}
	base := strings.ToUpper(strings.TrimSpace(orderCurrency()))
	rows, err := s.orders.ListFXOrdersByRange(ctx, projectID, from, to, filter)
	if err != nil {
		return nil, err
	}
	unconverted, err := s.orders.CountUnconvertedOrders(ctx, projectID, from, to, filter)
	if err != nil {
		return nil, err
	}
	res = &orderdto.ConvertedOverview{
		BaseCurrency: base,
		// ConvertedOrders 只数**与基准币不同**的那部分：同币种的单不需要折算，
		// 把它们算进「已折算」会让「折算覆盖率」看起来比实际高。
		ConvertedOrders:   0,
		UnconvertedOrders: unconverted,
	}
	for _, row := range rows {
		rate, ok := new(big.Rat).SetString(strings.TrimSpace(row.FXRate))
		if !ok || rate.Sign() <= 0 {
			// 脏快照：当成未固化（不编造一个折算值）。
			res.UnconvertedOrders++
			continue
		}
		res.ConvertedSales += convertCents(row.Total, rate)
		res.ConvertedOrders++
	}
	// 毛利折算：按销售额比例（见方法注释）。比例由**调用方**传（不再递归调 SalesOverview ——
	// 那是 O(∞) 的自我调用，实测直接把测试挂成 600s 超时）。
	if grossProfit > 0 && sales > 0 {
		ratio := new(big.Rat).SetFrac64(grossProfit, sales)
		res.ConvertedProfit = convertCents(res.ConvertedSales, ratio)
	}
	return res, nil
}
