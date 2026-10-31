package orderservice

import (
	"context"
	"math"
	"strings"
	"time"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
	"go_wp/pkg/utils"
)

func (s *Service) SoldQuantityByRange(ctx context.Context, req *orderdto.OrderSoldQuantityReq) (res *orderdto.OrderSoldQuantityResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, orderenums.Err(orderenums.ErrProjectRequired)
	}
	from, to, err := normalizeRangeWindow(req.From, req.To, time.Now())
	if err != nil {
		return nil, err
	}
	row, err := s.orders.SoldQuantityByRange(ctx, req.ProjectID, from, to)
	if err != nil {
		return nil, err
	}
	return &orderdto.OrderSoldQuantityResp{
		ProjectID: req.ProjectID,
		From:      from.Format(utils.LayoutDay),

		To:         to.AddDate(0, 0, -1).Format(utils.LayoutDay),
		Quantity:   row.Quantity,
		OrderCount: row.OrderCount,
	}, nil
}

func (s *Service) StatusCounts(ctx context.Context, req *orderdto.OrderStatusCountsReq) (res *orderdto.OrderStatusCountsResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, orderenums.Err(orderenums.ErrProjectRequired)
	}
	counts, err := s.orders.CountByStatus(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}

	if counts == nil {
		counts = map[string]int64{}
	}
	var total int64
	for _, n := range counts {
		total += n
	}
	return &orderdto.OrderStatusCountsResp{
		ProjectID: req.ProjectID,
		Counts:    counts,

		PendingCount: counts[ordermodel.OrderStatusPending],

		ShipPendingCount: counts[ordermodel.OrderStatusPaid],
		TotalCount:       total,
	}, nil
}

const defaultTopProducts = 5

func (s *Service) TopProducts(ctx context.Context, req *orderdto.OrderTopProductsReq) (res *orderdto.OrderTopProductsResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, orderenums.Err(orderenums.ErrProjectRequired)
	}
	from, to, err := normalizeRangeWindow(req.From, req.To, time.Now())
	if err != nil {
		return nil, err
	}

	limit := req.Limit
	if limit <= 0 {
		limit = defaultTopProducts
	}
	if limit > ordermodel.MaxTopProductLimit {
		limit = ordermodel.MaxTopProductLimit
	}
	rows, err := s.orders.TopProductsByRange(ctx, req.ProjectID, from, to, limit)
	if err != nil {
		return nil, err
	}
	items := make([]orderdto.OrderTopProductItemDTO, 0, len(rows))
	for i, row := range rows {
		items = append(items, orderdto.OrderTopProductItemDTO{
			Rank:        i + 1,
			ProductID:   row.ProductID,
			ProductName: row.ProductName,
			SKU:         row.SKU,
			Quantity:    row.Quantity,
			Amount:      row.Amount,
			AmountLabel: centsToYuanLabel(row.Amount),
		})
	}
	return &orderdto.OrderTopProductsResp{
		ProjectID: req.ProjectID,
		From:      from.Format(utils.LayoutDay),
		To:        to.AddDate(0, 0, -1).Format(utils.LayoutDay),
		Limit:     limit,
		Items:     items,
	}, nil
}

const maxRangeDays = 366

const maxHourlyRangeDays = 2

func (s *Service) SummaryByRange(ctx context.Context, req *orderdto.OrderRangeSummaryReq) (res *orderdto.OrderRangeSummaryResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, orderenums.Err(orderenums.ErrProjectRequired)
	}
	from, to, err := normalizeRangeWindow(req.From, req.To, time.Now())
	if err != nil {
		return nil, err
	}
	row, err := s.orders.SummaryByRange(ctx, req.ProjectID, from, to)
	if err != nil {
		return nil, err
	}
	return &orderdto.OrderRangeSummaryResp{
		ProjectID: req.ProjectID,
		From:      from.Format(utils.LayoutDay),

		To:             to.AddDate(0, 0, -1).Format(utils.LayoutDay),
		OrderCount:     row.OrderCount,
		PaidOrderCount: row.PaidOrderCount,
		NetSales:       row.NetSales,
		NetSalesLabel:  centsToYuanLabel(row.NetSales),
	}, nil
}

func normalizeRangeWindow(rawFrom, rawTo string, now time.Time) (from, to time.Time, err error) {
	dayFrom, okFrom := parseDay(rawFrom)
	dayTo, okTo := parseDay(rawTo)
	if !okFrom || !okTo {
		return time.Time{}, time.Time{}, orderenums.Err(orderenums.ErrInvalidParam)
	}
	from, to = dayFrom, dayTo.AddDate(0, 0, 1)
	if tomorrow := dayStart(now).AddDate(0, 0, 1); to.After(tomorrow) {
		to = tomorrow
	}
	if !from.Before(to) {
		return time.Time{}, time.Time{}, orderenums.Err(orderenums.ErrInvalidParam)
	}
	if to.Sub(from) > time.Duration(maxRangeDays)*24*time.Hour {
		return time.Time{}, time.Time{}, orderenums.Err(orderenums.ErrInvalidParam)
	}
	return from, to, nil
}

func parseDay(raw string) (t time.Time, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	parsed, err := time.ParseInLocation(utils.LayoutDay, raw, time.UTC)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

func dayStart(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}

func (s *Service) DailySeries(ctx context.Context, req *orderdto.OrderDailySeriesReq) (res *orderdto.OrderDailySeriesResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, orderenums.Err(orderenums.ErrProjectRequired)
	}
	from, to, err := normalizeRangeWindow(req.From, req.To, time.Now())
	if err != nil {
		return nil, err
	}
	rows, err := s.orders.DailyByRange(ctx, req.ProjectID, from, to)
	if err != nil {
		return nil, err
	}
	return &orderdto.OrderDailySeriesResp{
		ProjectID: req.ProjectID,
		From:      from.Format(utils.LayoutDay),

		To:     to.AddDate(0, 0, -1).Format(utils.LayoutDay),
		Points: fillDailyPoints(from, to, rows),
	}, nil
}

func fillDailyPoints(from, to time.Time, rows []ordermodel.OrderDailyPoint) []orderdto.OrderDailyPointDTO {
	byDay := make(map[string]ordermodel.OrderDailyPoint, len(rows))
	for _, r := range rows {
		byDay[r.Day.UTC().Format(utils.LayoutDay)] = r
	}
	points := make([]orderdto.OrderDailyPointDTO, 0, maxRangeDays)
	for d := from; d.Before(to); d = d.AddDate(0, 0, 1) {
		key := d.UTC().Format(utils.LayoutDay)
		row := byDay[key]
		points = append(points, orderdto.OrderDailyPointDTO{
			Day:            key,
			OrderCount:     row.OrderCount,
			PaidOrderCount: row.PaidOrderCount,
			NetSales:       row.NetSales,
			NetSalesLabel:  centsToYuanLabel(row.NetSales),
		})
	}
	return points
}

func (s *Service) HourlySeries(ctx context.Context, req *orderdto.OrderDailySeriesReq) (res *orderdto.OrderDailySeriesResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, orderenums.Err(orderenums.ErrProjectRequired)
	}
	from, to, err := normalizeRangeWindow(req.From, req.To, time.Now())
	if err != nil {
		return nil, err
	}
	if to.Sub(from) > time.Duration(maxHourlyRangeDays)*24*time.Hour {
		return nil, orderenums.Err(orderenums.ErrInvalidParam)
	}
	rows, err := s.orders.HourlyByRange(ctx, req.ProjectID, from, to)
	if err != nil {
		return nil, err
	}
	return &orderdto.OrderDailySeriesResp{
		ProjectID: req.ProjectID,
		From:      from.Format(utils.LayoutDay),
		To:        to.AddDate(0, 0, -1).Format(utils.LayoutDay),
		Points:    fillHourlyPoints(from, to, rows),
	}, nil
}

func fillHourlyPoints(from, to time.Time, rows []ordermodel.OrderDailyPoint) []orderdto.OrderDailyPointDTO {
	byHour := make(map[string]ordermodel.OrderDailyPoint, len(rows))
	for _, r := range rows {
		byHour[r.Day.UTC().Format(utils.LayoutHour)] = r
	}
	points := make([]orderdto.OrderDailyPointDTO, 0, maxHourlyRangeDays*24)
	for h := from; h.Before(to); h = h.Add(time.Hour) {
		key := h.UTC().Format(utils.LayoutHour)
		row := byHour[key]
		points = append(points, orderdto.OrderDailyPointDTO{
			Day:            key,
			OrderCount:     row.OrderCount,
			PaidOrderCount: row.PaidOrderCount,
			NetSales:       row.NetSales,
			NetSalesLabel:  centsToYuanLabel(row.NetSales),
		})
	}
	return points
}

const (
	salesDefaultMonthly = 6

	salesMaxMonthly = 24
)

var salesStatusWhitelist = []string{
	ordermodel.OrderStatusPaid,
	ordermodel.OrderStatusShipped,
	ordermodel.OrderStatusCompleted,
}

func (s *Service) SalesOverview(ctx context.Context, req *orderdto.SalesOverviewReq) (res *orderdto.SalesOverviewResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, orderenums.Err(orderenums.ErrProjectRequired)
	}
	from, to, err := normalizeRangeWindow(req.From, req.To, time.Now())
	if err != nil {
		return nil, err
	}
	statuses, statusText, err := salesStatuses(req.Status)
	if err != nil {
		return nil, err
	}
	monthly := salesMonthlyMonths(req.Monthly)
	filter := ordermodel.OrderSalesFilter{Statuses: statuses}

	row, err := s.orders.SalesOverviewByRange(ctx, req.ProjectID, from, to, filter)
	if err != nil {
		return nil, err
	}

	growth, err := s.orders.CustomerGrowthByRange(ctx, req.ProjectID, from, to)
	if err != nil {
		return nil, err
	}

	shippingTotal, err := s.orders.SalesShippingTotalByRange(ctx, req.ProjectID, from, to, filter)
	if err != nil {
		return nil, err
	}

	resp := &orderdto.SalesOverviewResp{
		Currency:           orderCurrency(),
		ProjectID:          req.ProjectID,
		From:               from.Format(utils.LayoutDay),
		To:                 to.AddDate(0, 0, -1).Format(utils.LayoutDay),
		Status:             statusText,
		Monthly:            monthly,
		OrderCount:         row.OrderCount,
		ItemRows:           row.ItemRows,
		Units:              row.Units,
		Sales:              row.Sales,
		AvgOrderValue:      ratioCents(row.Sales, row.OrderCount),
		AvgCustomerValue:   ratioCents(row.Sales, row.Customers),
		AvgItemsPerOrder:   ratioFloat(row.ItemRows, row.OrderCount),
		AvgSalesPerUnit:    ratioCents(row.Sales, row.Units),
		Customers:          row.Customers,
		NewCustomers:       growth.NewCustomers,
		ReturningCustomers: growth.ReturningCustomers,
		Repurchasers:       growth.Repurchasers,
		RepurchaseRatePct:  repurchaseRatePct(growth.NewRepurchasers, growth.ReturningCustomers, growth.OrderingCustomers),

		GrossProfit:        row.LineMargin - shippingTotal,
		CostIncompleteRows: row.CostIncompleteRows,
		CostIncomplete:     row.CostIncompleteRows > 0,
	}

	if cmp := s.salesCompare(ctx, req.ProjectID, from, to, filter, row); cmp != nil {
		resp.Compare = cmp
	}

	if converted, cerr := s.ConvertedOverview(ctx, req, resp.Sales, resp.GrossProfit); cerr == nil {
		resp.Converted = converted
	}

	points, err := s.salesMonthlyPoints(ctx, req.ProjectID, monthly, filter)
	if err != nil {
		return nil, err
	}
	resp.MonthlyPoints = points
	return resp, nil
}

func salesStatuses(raw string) (statuses []string, effective string, err error) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return nil, "", nil
	}
	allowed := make(map[string]bool, len(salesStatusWhitelist))
	for _, st := range salesStatusWhitelist {
		allowed[st] = true
	}
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '，'
	})
	seen := make(map[string]bool, len(fields))
	picked := make([]string, 0, len(fields))
	for _, f := range fields {
		if !allowed[f] || seen[f] {
			continue
		}
		seen[f] = true
		picked = append(picked, f)
	}
	if len(picked) == 0 {

		return nil, "", nil
	}
	return picked, strings.Join(picked, ","), nil
}

func salesMonthlyMonths(raw int) int {
	if raw <= 0 {
		return salesDefaultMonthly
	}
	if raw > salesMaxMonthly {
		return salesMaxMonthly
	}
	return raw
}

func (s *Service) salesCompare(ctx context.Context, projectID string, from, to time.Time, f ordermodel.OrderSalesFilter, cur ordermodel.OrderSalesOverviewRow) *orderdto.SalesCompareDTO {
	span := to.Sub(from)
	prevTo := from
	prevFrom := from.Add(-span)

	prev, err := s.orders.SalesOverviewByRange(ctx, projectID, prevFrom, prevTo, f)
	if err != nil {
		return nil
	}
	if prev.OrderCount == 0 && prev.Sales == 0 && prev.Customers == 0 {
		return nil
	}
	return &orderdto.SalesCompareDTO{
		From:                prevFrom.Format(utils.LayoutDay),
		To:                  prevTo.AddDate(0, 0, -1).Format(utils.LayoutDay),
		OrderCount:          prev.OrderCount,
		Sales:               prev.Sales,
		Customers:           prev.Customers,
		OrderCountChangePct: changePct(prev.OrderCount, cur.OrderCount),
		SalesChangePct:      changePct(prev.Sales, cur.Sales),
		CustomersChangePct:  changePct(prev.Customers, cur.Customers),
	}
}

func (s *Service) salesMonthlyPoints(ctx context.Context, projectID string, months int, f ordermodel.OrderSalesFilter) ([]orderdto.SalesMonthlyPointDTO, error) {
	now := time.Now().UTC()
	thisMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	start := thisMonth.AddDate(0, -(months - 1), 0)
	end := thisMonth.AddDate(0, 1, 0)

	rows, err := s.orders.SalesMonthlyByRange(ctx, projectID, start, end, f)
	if err != nil {
		return nil, err
	}
	byMonth := make(map[string]ordermodel.OrderSalesMonthlyRow, len(rows))
	for _, r := range rows {
		byMonth[r.Month.UTC().Format("2006-01")] = r
	}
	points := make([]orderdto.SalesMonthlyPointDTO, 0, months)
	for i := 0; i < months; i++ {
		key := start.AddDate(0, i, 0).Format("2006-01")
		p := orderdto.SalesMonthlyPointDTO{Month: key}
		if r, ok := byMonth[key]; ok {
			p.OrderCount = r.OrderCount
			p.Sales = r.Sales
			p.Customers = r.Customers
			p.NewOrderCount = r.NewOrderCount
			p.ReturningOrderCount = r.ReturningOrderCount
			p.GuestOrderCount = r.GuestOrderCount
			p.NewSales = r.NewSales
			p.ReturningSales = r.ReturningSales
			p.GuestSales = r.GuestSales
			p.NewCustomers = r.NewCustomers
			p.ReturningCustomers = r.ReturningCustomers
		}
		points = append(points, p)
	}
	return points, nil
}

func ratioCents(numerator, denominator int64) int64 {
	if denominator <= 0 {
		return 0
	}
	return int64(math.Round(float64(numerator) / float64(denominator)))
}

func ratioFloat(numerator, denominator int64) float64 {
	if denominator <= 0 {
		return 0
	}
	return math.Round(float64(numerator)*100/float64(denominator)) / 100
}

func changePct(prev, cur int64) *float64 {
	if prev == 0 {
		return nil
	}
	v := math.Round(float64(cur-prev)*1000/float64(prev)) / 10
	return &v
}
