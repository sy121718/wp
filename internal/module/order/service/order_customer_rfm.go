package orderservice

// order_customer_rfm.go — 客户 RFM 分层（分析页 + 列表按分段筛选）。
//
// 这一层做四件事：归一化窗口、判分段白名单、把 RecencyDays 与展示串算出来、
// 把两段查询（全量分段计数 + 一页明细）组装成一份结果。
// 打分与分段口径全在 model 的 rfmScoredCTE 里，这里不复制第二份。

import (
	"context"
	"errors"
	"strings"
	"time"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
	"go_wp/pkg/utils"
)

// customerRfmDefaultLimit / customerRfmMaxLimit RFM 明细分页边界。
const (
	customerRfmDefaultLimit = 20
	customerRfmMaxLimit     = 200
)

// knownRfmSegments 分段白名单（三个值，其余一律拒）。
var knownRfmSegments = map[string]string{
	ordermodel.RfmSegmentVip:       "高价值",
	ordermodel.RfmSegmentPotential: "潜力",
	ordermodel.RfmSegmentLowValue:  "一般",
}

// CustomerRfmByRange 取区间内的 RFM 分层。
func (s *Service) CustomerRfmByRange(ctx context.Context, req *orderdto.CustomerRfmReq) (res *orderdto.CustomerRfmResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, errors.New(orderenums.ErrProjectRequired)
	}
	from, to, err := normalizeRangeWindow(req.From, req.To, time.Now())
	if err != nil {
		return nil, err
	}
	segment := strings.TrimSpace(req.Segment)
	// 认不出的分段当场拒：静默回落成「全部」会让一个写错的筛选显示成完整报表，
	// 而那时用户以为自己看的是「高价值客户」。
	if segment != "" {
		if _, ok := knownRfmSegments[segment]; !ok {
			return nil, errors.New(orderenums.ErrInvalidParam)
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

	// RecencyDays 的基准是**用户看到的结束日**（to 是半开上界，次日零点）。
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

// recencyDaysOf 距今多少天（负数视为 0）。
//
// 负数理论上不该出现（last_at 是已发生的下单时刻），但脏数据或时钟漂移会让它出现，
// 而负天数在页面上会渲染成「-3 天前」这种读不通的东西。
func recencyDaysOf(lastAt, end time.Time) int {
	days := int(end.Sub(lastAt).Hours() / 24)
	if days < 0 {
		return 0
	}
	return days
}

// segmentLabelOf 分段名 → 展示文案（认不出的原样返回：那是口径变了而这里没跟上，
// 显示原文比显示一个空白更能说明问题）。
func segmentLabelOf(segment string) string {
	if label, ok := knownRfmSegments[segment]; ok {
		return label
	}
	return segment
}
