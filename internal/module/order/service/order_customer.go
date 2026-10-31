package orderservice

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
	"go_wp/pkg/utils"
)

func (s *Service) CustomerOrderSummaryOf(ctx context.Context, req *orderdto.CustomerOrderSummaryReq) (res *orderdto.CustomerOrderSummaryResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" || req.UserID == 0 {
		return nil, orderenums.Err(orderenums.ErrInvalidParam)
	}

	row, err := s.orders.SummaryByUser(ctx, req.ProjectID, req.UserID)
	if err != nil {
		return nil, err
	}
	res = &orderdto.CustomerOrderSummaryResp{
		UserID:           req.UserID,
		ProjectID:        req.ProjectID,
		OrderCount:       row.OrderCount,
		PaidOrderCount:   row.PaidOrderCount,
		TotalAmount:      row.TotalAmount,
		TotalAmountLabel: centsToYuanLabel(row.TotalAmount),
	}

	if row.LastOrderID != nil && row.LastOrderTime != nil {
		res.HasOrders = true
		res.LastOrderID = *row.LastOrderID
		res.LastOrderTime = utils.NewJSONTimePtr(row.LastOrderTime)
		res.LastOrderTimeText = row.LastOrderTime.Local().Format("2006-01-02 15:04")
		if row.LastOrderNo != nil {
			res.LastOrderNo = *row.LastOrderNo
		}
		if row.LastOrderStatus != nil {
			res.LastOrderStatus = *row.LastOrderStatus
		}
	}
	return res, nil
}

func (s *Service) CustomerGrowthByRange(ctx context.Context, req *orderdto.CustomerGrowthReq) (res *orderdto.CustomerGrowthResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, orderenums.Err(orderenums.ErrProjectRequired)
	}
	from, to, err := normalizeRangeWindow(req.From, req.To, time.Now())
	if err != nil {
		return nil, err
	}
	row, err := s.orders.CustomerGrowthByRange(ctx, req.ProjectID, from, to)
	if err != nil {
		return nil, err
	}
	rate := repurchaseRatePct(row.NewRepurchasers, row.ReturningCustomers, row.OrderingCustomers)
	return &orderdto.CustomerGrowthResp{
		ProjectID: req.ProjectID,
		From:      from.Format(utils.LayoutDay),

		To:                  to.AddDate(0, 0, -1).Format(utils.LayoutDay),
		OrderingCustomers:   row.OrderingCustomers,
		NewCustomers:        row.NewCustomers,
		ReturningCustomers:  row.ReturningCustomers,
		Repurchasers:        row.Repurchasers,
		NewRepurchasers:     row.NewRepurchasers,
		RepurchaseRatePct:   rate,
		RepurchaseRateLabel: fmt.Sprintf("%.1f%%", rate),
	}, nil
}

func repurchaseRatePct(newRepurchasers, returning, ordering int64) float64 {
	if ordering <= 0 {
		return 0
	}
	return math.Round(float64(newRepurchasers+returning)*1000/float64(ordering)) / 10
}

const (
	customerSegmentDefaultLimit = 50
	customerSegmentMaxLimit     = 500
)

var customerSegmentMinOrdersAllowed = map[int]bool{2: true, 3: true, 5: true, 10: true}

var knownCustomerSegments = map[string]ordermodel.CustomerSegment{
	"new":          ordermodel.CustomerSegmentNew,
	"returning":    ordermodel.CustomerSegmentReturning,
	"repurchasing": ordermodel.CustomerSegmentRepurchasing,
}

func (s *Service) CustomerSegmentIDsByRange(ctx context.Context, req *orderdto.CustomerSegmentIDsReq) (res *orderdto.CustomerSegmentIDsResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, orderenums.Err(orderenums.ErrProjectRequired)
	}

	name := strings.TrimSpace(req.Segment)
	minOrders := req.MinOrders
	if minOrders != 0 && !customerSegmentMinOrdersAllowed[minOrders] {
		return nil, orderenums.Err(orderenums.ErrInvalidParam)
	}
	if name == "" && minOrders > 0 {
		name = string(ordermodel.CustomerSegmentRepurchasing)
	}
	segment, ok := knownCustomerSegments[name]
	if !ok {
		return nil, orderenums.Err(orderenums.ErrInvalidParam)
	}
	from, to, err := normalizeRangeWindow(req.From, req.To, time.Now())
	if err != nil {
		return nil, err
	}
	limit, offset := utils.NormalizeLimitOffset(req.Limit, req.Offset, customerSegmentDefaultLimit, customerSegmentMaxLimit)

	ids, total, err := s.orders.CustomerSegmentIDsByRange(ctx, req.ProjectID, from, to, segment, minOrders, limit, offset)
	if err != nil {
		return nil, err
	}
	if ids == nil {
		ids = []int64{}
	}
	return &orderdto.CustomerSegmentIDsResp{
		ProjectID: req.ProjectID,
		From:      from.Format(utils.LayoutDay),

		To:        to.AddDate(0, 0, -1).Format(utils.LayoutDay),
		Segment:   string(segment),
		MinOrders: minOrders,
		UserIDs:   ids,
		Total:     total,
	}, nil
}

const (
	customerRfmDefaultLimit = 20
	customerRfmMaxLimit     = 200
)

var knownRfmSegments = map[string]string{
	ordermodel.RfmSegmentVip:       "高价值",
	ordermodel.RfmSegmentPotential: "潜力",
	ordermodel.RfmSegmentLowValue:  "一般",
}

func (s *Service) CustomerRfmByRange(ctx context.Context, req *orderdto.CustomerRfmReq) (res *orderdto.CustomerRfmResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, orderenums.Err(orderenums.ErrProjectRequired)
	}
	from, to, err := normalizeRangeWindow(req.From, req.To, time.Now())
	if err != nil {
		return nil, err
	}
	segment := strings.TrimSpace(req.Segment)

	if segment != "" {
		if _, ok := knownRfmSegments[segment]; !ok {
			return nil, orderenums.Err(orderenums.ErrInvalidParam)
		}
	}
	limit, offset := utils.NormalizeLimitOffset(req.Limit, req.Offset, customerRfmDefaultLimit, customerRfmMaxLimit)

	summary, err := s.orders.CustomerRfmSummary(ctx, req.ProjectID, from, to)
	if err != nil {
		return nil, err
	}
	rows, total, err := s.orders.CustomerRfmList(ctx, req.ProjectID, from, to, segment, limit, offset)
	if err != nil {
		return nil, err
	}

	end := to.AddDate(0, 0, -1)
	res = &orderdto.CustomerRfmResp{
		ProjectID: req.ProjectID,
		From:      from.Format(utils.LayoutDay),
		To:        end.Format(utils.LayoutDay),
		Customers: summary.Customers,
		Vip:       summary.Vip,
		Potential: summary.Potential,
		LowValue:  summary.LowValue,
		Total:     total,
		Items:     make([]orderdto.CustomerRfmItem, 0, len(rows)),
	}
	for _, r := range rows {
		res.Items = append(res.Items, orderdto.CustomerRfmItem{
			UserID:        r.UserID,
			LastOrderAt:   r.LastAt.Format(utils.LayoutDay),
			RecencyDays:   recencyDaysOf(r.LastAt, end),
			Frequency:     r.Freq,
			MonetaryCents: r.Monetary,
			MonetaryLabel: centsToYuanLabel(r.Monetary),
			RScore:        r.RScore,
			FScore:        r.FScore,
			MScore:        r.MScore,
			TotalScore:    r.TotalScore,
			Segment:       r.Segment,
			SegmentLabel:  segmentLabelOf(r.Segment),
		})
	}
	return res, nil
}

func recencyDaysOf(lastAt, end time.Time) int {
	days := int(end.Sub(lastAt).Hours() / 24)
	if days < 0 {
		return 0
	}
	return days
}

func segmentLabelOf(segment string) string {
	if label, ok := knownRfmSegments[segment]; ok {
		return label
	}
	return segment
}

func (s *Service) CustomerRfmSegmentIDsByRange(ctx context.Context, req *orderdto.CustomerRfmSegmentIDsReq) (res *orderdto.CustomerRfmSegmentIDsResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, orderenums.Err(orderenums.ErrProjectRequired)
	}
	segment := strings.TrimSpace(req.Segment)

	if _, ok := knownRfmSegments[segment]; !ok {
		return nil, orderenums.Err(orderenums.ErrInvalidParam)
	}
	from, to, err := normalizeRangeWindow(req.From, req.To, time.Now())
	if err != nil {
		return nil, err
	}
	limit, offset := utils.NormalizeLimitOffset(req.Limit, req.Offset, customerRfmDefaultLimit, customerRfmMaxLimit)

	ids, total, err := s.orders.CustomerRfmSegmentIDs(ctx, req.ProjectID, from, to, segment, limit, offset)
	if err != nil {
		return nil, err
	}
	if ids == nil {
		ids = []int64{}
	}
	return &orderdto.CustomerRfmSegmentIDsResp{
		ProjectID: req.ProjectID,
		From:      from.Format(utils.LayoutDay),

		To:      to.AddDate(0, 0, -1).Format(utils.LayoutDay),
		Segment: segment,
		UserIDs: ids,
		Total:   total,
	}, nil
}

const (
	customerCohortDefaultMonths = 6
	customerCohortMaxMonths     = 12
)

func (s *Service) CustomerCohortByRange(ctx context.Context, req *orderdto.CustomerCohortReq) (res *orderdto.CustomerCohortResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, orderenums.Err(orderenums.ErrProjectRequired)
	}
	from, to, err := normalizeRangeWindow(req.From, req.To, time.Now())
	if err != nil {
		return nil, err
	}
	months := req.Months
	if months <= 0 {
		months = customerCohortDefaultMonths
	}
	if months > customerCohortMaxMonths {
		months = customerCohortMaxMonths
	}

	rows, err := s.orders.CohortRetention(ctx, req.ProjectID, from, to)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	thisMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	out := &orderdto.CustomerCohortResp{
		ProjectID: req.ProjectID,
		From:      from.Format(utils.LayoutDay),
		To:        to.AddDate(0, 0, -1).Format(utils.LayoutDay),
		Rows:      make([]orderdto.CustomerCohortRow, 0, 8),
	}

	var cur *orderdto.CustomerCohortRow
	for _, r := range rows {
		cm := monthStartUTC(r.CohortMonth)
		if cur == nil || cur.CohortMonth != cm.Format(layoutMonth) {
			out.Rows = append(out.Rows, orderdto.CustomerCohortRow{
				CohortMonth: cm.Format(layoutMonth),
				CohortLabel: fmt.Sprintf("%d年%02d月", cm.Year(), int(cm.Month())),
				CohortSize:  r.CohortSize,
				Cells:       make([]orderdto.CustomerCohortCell, 0, months),
			})
			cur = &out.Rows[len(out.Rows)-1]
			out.Cohorts++
			out.Customers += r.CohortSize
		}
		idx := monthsBetween(cm, monthStartUTC(r.ActivityMonth))

		if idx < 0 || idx >= months {
			continue
		}
		pct := retentionPct(r.ActiveCustomers, r.CohortSize)
		cur.Cells = append(cur.Cells, orderdto.CustomerCohortCell{
			MonthIndex:       idx,
			MonthLabel:       fmt.Sprintf("%d年%02d月", monthStartUTC(r.ActivityMonth).Year(), int(monthStartUTC(r.ActivityMonth).Month())),
			ActiveCustomers:  r.ActiveCustomers,
			RetentionRatePct: pct,
			RetentionLabel:   retentionLabel(pct),
		})
	}

	if len(out.Rows) > 0 {
		first, perr := time.Parse(layoutMonth, out.Rows[0].CohortMonth)
		if perr == nil {
			out.Months = monthsBetween(monthStartUTC(first), thisMonth) + 1
		}
	}
	if out.Months < 1 {
		out.Months = 1
	}
	if out.Months > months {
		out.Months = months
	}

	for i := range out.Rows {
		row := &out.Rows[i]
		byIdx := make(map[int]orderdto.CustomerCohortCell, len(row.Cells))
		for _, c := range row.Cells {
			byIdx[c.MonthIndex] = c
		}
		full := make([]orderdto.CustomerCohortCell, 0, out.Months)
		for k := 0; k < out.Months; k++ {
			if c, ok := byIdx[k]; ok {
				c.Reached = true
				full = append(full, c)
				continue
			}

			start, perr := time.Parse(layoutMonth, row.CohortMonth)
			label := ""
			reached := false
			if perr == nil {
				cm := addMonths(monthStartUTC(start), k)
				label = fmt.Sprintf("%d年%02d月", cm.Year(), int(cm.Month()))
				reached = !cm.After(thisMonth)
			}
			full = append(full, orderdto.CustomerCohortCell{
				MonthIndex:     k,
				MonthLabel:     label,
				RetentionLabel: retentionLabel(0),
				Reached:        reached,

				ActiveCustomers: 0,
			})
		}
		row.Cells = full
	}
	return out, nil
}

func addMonths(t time.Time, n int) time.Time { return t.AddDate(0, n, 0) }

func monthStartUTC(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), 1, 0, 0, 0, 0, time.UTC)
}

const layoutMonth = "2006-01"

func monthsBetween(from, to time.Time) int {
	return (to.Year()-from.Year())*12 + int(to.Month()) - int(from.Month())
}

func retentionPct(active, size int64) float64 {
	if size <= 0 {
		return 0
	}
	return math.Round(float64(active)/float64(size)*1000) / 10
}

func retentionLabel(pct float64) string {
	return fmt.Sprintf("%.1f%%", pct)
}
