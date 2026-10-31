package orderservice

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

func (s *Service) GetRate(ctx context.Context, req *orderdto.GetRateReq) (res *orderdto.RateResp, err error) {
	if req == nil {
		return nil, errors.New("参数不合法")
	}
	e, gerr := s.orders.GetRate(ctx, req.BaseCode, req.QuoteCode)
	if gerr != nil {
		if errors.Is(gerr, ordermodel.ErrCurrencyRateNotFound) {

			return &orderdto.RateResp{BaseCode: strings.ToUpper(strings.TrimSpace(req.BaseCode)),
				QuoteCode: strings.ToUpper(strings.TrimSpace(req.QuoteCode))}, nil
		}
		return nil, gerr
	}
	return &orderdto.RateResp{
		BaseCode: e.BaseCode, QuoteCode: e.QuoteCode,

		Rate:   trimDecimalZeros(e.Rate),
		RateAt: e.RateAt.Format(time.RFC3339), Source: e.Source,
	}, nil
}

func (s *Service) SetRate(ctx context.Context, req *orderdto.SetRateReq) (res *orderdto.RateResp, err error) {
	if req == nil {
		return nil, errors.New("参数不合法")
	}
	base := strings.ToUpper(strings.TrimSpace(req.BaseCode))
	quote := strings.ToUpper(strings.TrimSpace(req.QuoteCode))
	if base == "" || quote == "" {
		return nil, errors.New("币种代码必填")
	}

	rate, ok := new(big.Rat).SetString(strings.TrimSpace(req.Rate))
	if !ok || rate.Sign() <= 0 {
		return nil, errors.New("汇率必须是正数")
	}
	e := &ordermodel.CurrencyRateEntity{
		BaseCode: base, QuoteCode: quote,

		Rate:   formatRate(rate),
		RateAt: time.Now().UTC(),
		Source: "manual",
	}
	if err := s.orders.UpsertRate(ctx, e); err != nil {
		return nil, err
	}
	return s.GetRate(ctx, &orderdto.GetRateReq{BaseCode: base, QuoteCode: quote})
}

func convertCents(cents int64, rate *big.Rat) int64 {
	if rate == nil || cents == 0 {
		return 0
	}
	v := new(big.Rat).Mul(new(big.Rat).SetInt64(cents), rate)

	half := big.NewRat(1, 2)
	v.Add(v, half)
	q := new(big.Int).Quo(v.Num(), v.Denom())
	return q.Int64()
}

func trimDecimalZeros(raw string) string {
	s := strings.TrimSpace(raw)
	if !strings.Contains(s, ".") {
		return s
	}
	s = strings.TrimRight(s, "0")
	return strings.TrimRight(s, ".")
}

func formatRate(r *big.Rat) string {
	f, _ := r.Float64()
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.8f", f), "0"), ".")
}

func (s *Service) orderFXSnapshot(ctx context.Context, orderCurrency, baseCurrency string) (rate string, base string) {
	orderCurrency = strings.ToUpper(strings.TrimSpace(orderCurrency))
	baseCurrency = strings.ToUpper(strings.TrimSpace(baseCurrency))
	if orderCurrency == "" || baseCurrency == "" || orderCurrency == baseCurrency {

		return "", ""
	}
	e, err := s.orders.GetRate(ctx, orderCurrency, baseCurrency)
	if err != nil || e == nil || strings.TrimSpace(e.Rate) == "" {
		return "", ""
	}
	return e.Rate, baseCurrency
}

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

	rows, err := s.orders.ListFXOrdersByRange(ctx, projectID, from, to, filter, base)
	if err != nil {
		return nil, err
	}
	unconverted, err := s.orders.CountUnconvertedOrders(ctx, projectID, from, to, filter, base)
	if err != nil {
		return nil, err
	}
	res = &orderdto.ConvertedOverview{
		BaseCurrency: base,

		ConvertedOrders:   0,
		UnconvertedOrders: unconverted,
	}
	for _, row := range rows {
		rate, ok := new(big.Rat).SetString(strings.TrimSpace(row.FXRate))
		if !ok || rate.Sign() <= 0 {

			res.UnconvertedOrders++
			continue
		}
		res.ConvertedSales += convertCents(row.Total, rate)
		res.ConvertedOrders++
	}

	if grossProfit > 0 && sales > 0 {
		ratio := new(big.Rat).SetFrac64(grossProfit, sales)
		res.ConvertedProfit = convertCents(res.ConvertedSales, ratio)
	}
	return res, nil
}
