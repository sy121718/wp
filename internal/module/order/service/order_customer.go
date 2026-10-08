package orderservice

//
// 为什么放在订单模块而不是让后台页面自己拼：订单表是本模块的私有数据，
// 页面直接 join 会绕过「订单状态口径」这一层（哪些状态算消费、金额单位怎么换算），
// 而这些东西正是订单模块唯一的解释权所在。
//
// 这是**只读**能力，且收窄到一条方法（见 ordercontract.CustomerOrderSummaryReader）：
// 后台客户页需要的是「这个客户下过几单、花了多少、最后一次是什么时候」，
// 不是订单列表、更不是订单写能力。

//
// 这一层只做三件事：归一化窗口、算复购率、把分/百分比格式化成展示串。
// 口径（谁是新人、什么算复购）全在 model 的 orderCustomerGrowthSQL 里，这里不复制第二份。

//
// CustomerGrowthByRange 回答「有多少人」，这里回答「是哪些人」。
// 两者在 model 层共用 orderCustomerCTEs，所以口径不分叉 —— 客户概览页的数字与
// 客户列表按同条件筛出来的条数必须相等（docs/17 §P7 的对账闸门）。

//
// 这一层做四件事：归一化窗口、判分段白名单、把 RecencyDays 与展示串算出来、
// 把两段查询（全量分段计数 + 一页明细）组装成一份结果。
// 打分与分段口径全在 model 的 rfmScoredCTE 里，这里不复制第二份。

//
// 分群与「某月有购买」的事实全在 model 的一条 SQL 里，这一层只做三件事：
// 收敛列数、把稀疏的格铺成矩阵、把比例与展示串算出来。
// **不重算任何口径** —— 页面上「共 N 个新客户」与客户概览页的「新客」必须是同一个数，
// 各算一次等于埋一个迟早会响的分叉。

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
	"go_wp/pkg/utils"
)

// CustomerOrderSummaryOf 按「工程 + 客户」取订单聚合（数量 / 累计消费 / 最近一单）。
//
// 客户从来没有下过单是**正常结果**（HasOrders=false），不是错误：
// 刚注册的账号就长这样，把它当错误会让页面显示一句看不懂的提示。
func (s *Service) CustomerOrderSummaryOf(ctx context.Context, req *orderdto.CustomerOrderSummaryReq) (res *orderdto.CustomerOrderSummaryResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" || req.UserID == 0 {
		return nil, errors.New(orderenums.ErrInvalidParam)
	}
	// 聚合三值与最近一单在**同一条 SQL** 里取回（model.SummaryByUser 的窗口函数查询）。
	// 拆成三条（聚合 / 列表里的分页计数 / 列表取一单）时，三条之间落的新单会让摘要
	// 自相矛盾；其中分页计数那条在摘要场景里连结果都用不上，白扫一遍全量行。
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
	// 最近一单为空 ⇔ 这个客户在该工程下一单都没有（查询挂单行哨兵，恒返回一行）。
	// HasOrders 由它推出，而不是从聚合数字猜：0 单 + 0 元与「查不到」在数字上无法区分。
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

// CustomerGrowthByRange 取区间内的客户增长事实。
func (s *Service) CustomerGrowthByRange(ctx context.Context, req *orderdto.CustomerGrowthReq) (res *orderdto.CustomerGrowthResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, errors.New(orderenums.ErrProjectRequired)
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
		// to 是半开上界（次日零点），减一天才是用户看到的「结束日」（同 SummaryByRange）。
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

// repurchaseRatePct 复购率（百分比，一位小数）。
//
// 分子 = 新客里在区间内复购的 + 区间内下单的老客。老客**只要下单**就算「回来的」，
// 不必再复购一次 —— 他在区间之前已经下过单了，这次下单本身就是「回来」。
// 分母 = 区间内下单的客户数。
//
// 分母为 0 时返回 0 而不是 NaN：那意味着「这段时间没有客户下过单」，与「复购率 0%」
// 在页面上是同一件事（没东西可看），而 NaN 会让模板渲染出「NaN%」并且**没有任何报错**。
func repurchaseRatePct(newRepurchasers, returning, ordering int64) float64 {
	if ordering <= 0 {
		return 0
	}
	return math.Round(float64(newRepurchasers+returning)*1000/float64(ordering)) / 10
}

// customerSegmentDefaultLimit / customerSegmentMaxLimit 分段取 id 的分页边界。
//
// 上限比普通列表小一个量级：这批 id 会被上层拿去当 `IN (...)` 的过滤条件，
// 一次几百个已经够列表页用，几千个会让 SQL 文本本身变成负担。
// 需要「全部人」的场合（导出、群发）应该另设一条流式接口，而不是把上限调大。
const (
	customerSegmentDefaultLimit = 50
	customerSegmentMaxLimit     = 500
)

// knownCustomerSegments 分段白名单（与 model 的三个常量同源处再列一次）。
//
// 这里再判一遍而不是直接交给 model：service 是**对外契约的边界**，
// 让一个拼错的分段名走到 model 才被拒，错误类型会从「参数不对」变成「模型层未知分段」，
// 调用方（HTTP / AI 工具）拿到的东西就不可判了。
// customerSegmentMinOrdersAllowed 复购次数的可选档位（下拉给固定几档，不收任意值）。
//
// 收任意值的话，「≥ 7 次」这种档位会出现在 URL 里并被分享、被回放 ——
// 而运营真正需要的档位只有下面这几个。不在这里的一律拒（不静默回落到默认门槛：
// 回落会让 URL 上写着 7 的筛选实际跑的是 2，结果看起来正常却少了一半人）。
var customerSegmentMinOrdersAllowed = map[int]bool{2: true, 3: true, 5: true, 10: true}

var knownCustomerSegments = map[string]ordermodel.CustomerSegment{
	"new":          ordermodel.CustomerSegmentNew,
	"returning":    ordermodel.CustomerSegmentReturning,
	"repurchasing": ordermodel.CustomerSegmentRepurchasing,
}

// CustomerSegmentIDsByRange 取分段内的客户 id（分页）。
//
// 空分段返回**空切片而不是 nil**（Total 为 0）：调用方会把 UserIDs 直接当过滤条件用，
// nil 与「这一段没人」在 JSON 上都是 `[]`，但在 Go 侧 nil 会被某些查询构造器忽略掉
// （于是变成「不过滤 = 全部客户」，与「没人」正好相反）。空切片没有这个歧义。
func (s *Service) CustomerSegmentIDsByRange(ctx context.Context, req *orderdto.CustomerSegmentIDsReq) (res *orderdto.CustomerSegmentIDsResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, errors.New(orderenums.ErrProjectRequired)
	}
	// 只给了次数（没给分段名）时按「复购」处理：这是同一个筛选（区间内下单 ≥ N 单），
	// 单独开一个分段名会让「什么算复购」在代码里出现第二个定义。
	name := strings.TrimSpace(req.Segment)
	minOrders := req.MinOrders
	if minOrders != 0 && !customerSegmentMinOrdersAllowed[minOrders] {
		return nil, errors.New(orderenums.ErrInvalidParam)
	}
	if name == "" && minOrders > 0 {
		name = string(ordermodel.CustomerSegmentRepurchasing)
	}
	segment, ok := knownCustomerSegments[name]
	if !ok {
		return nil, errors.New(orderenums.ErrInvalidParam)
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
		// to 是半开上界（次日零点），减一天才是用户看到的「结束日」（同 CustomerGrowthByRange）。
		To:        to.AddDate(0, 0, -1).Format(utils.LayoutDay),
		Segment:   string(segment),
		MinOrders: minOrders,
		UserIDs:   ids,
		Total:     total,
	}, nil
}

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

// CustomerRfmSegmentIDsByRange 取某个 RFM 分段内的客户 id（客户列表按 RFM 分段筛用）。
//
// **打分批与明细行都来自同一批 SQL**（model 的 CustomerRfmSegmentIDs）：这里只做
// 窗口归一化、分段白名单与分页收敛，不重算任何分数。
func (s *Service) CustomerRfmSegmentIDsByRange(ctx context.Context, req *orderdto.CustomerRfmSegmentIDsReq) (res *orderdto.CustomerRfmSegmentIDsResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, errors.New(orderenums.ErrProjectRequired)
	}
	segment := strings.TrimSpace(req.Segment)
	// 与 RFM 页同一条口径：认不出的分段当场拒，不静默回落成「全部」——
	// 回落会让一个写错的筛选显示成完整报表，而那时用户以为自己看的是「高价值客户」。
	if _, ok := knownRfmSegments[segment]; !ok {
		return nil, errors.New(orderenums.ErrInvalidParam)
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
		// to 是半开上界（次日零点），减一天才是用户看到的「结束日」。
		To:      to.AddDate(0, 0, -1).Format(utils.LayoutDay),
		Segment: segment,
		UserIDs: ids,
		Total:   total,
	}, nil
}

// 矩阵列数边界（从首单当月算起，含当月）。
//
// 默认 6 列而不是 12：一屏放不下十几列，窄屏上会退化成横向滚动，而横向滚动里的
// 留存率几乎没人会逐格看。上限 12 是兜底（一年的观察窗）。
const (
	customerCohortDefaultMonths = 6
	customerCohortMaxMonths     = 12
)

// CustomerCohortByRange 取群组留存矩阵。
func (s *Service) CustomerCohortByRange(ctx context.Context, req *orderdto.CustomerCohortReq) (res *orderdto.CustomerCohortResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, errors.New(orderenums.ErrProjectRequired)
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
	// rows 已按 (cohort_month, activity_month) 升序，所以同一个群的格是连续的：
	// 只在 cohort_month 变化时新开一行，不必先分组再排序（再排一次就是第二份排序口径）。
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
		// 超出展示窗的格丢掉：留着会让矩阵多出几列全是 0 的空位，
		// 而「第 11 个月」这种列在 6 列的窗口里没有任何参照意义。
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
	// 观察窗列数 = 最早那个群到现在过了几个月（含首月），再夹到上下界。
	//
	// 用「最早的群」而不是「最长的行」：稀疏结果里某个群可能整月零活跃（不出行），
	// 按最长行定列数会让那些月份整列消失 —— 而零活跃恰恰是最该被看见的一列。
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
	// 把稀疏的行补齐成定长：模板要按列顺序渲染，而 Jet 里没法按序号索引切片
	//（`{{range}}` 只能顺序遍历）—— 补齐必须在 Go 侧做，否则模板要么写不了，
	// 要么得靠「行内再查一次」这种把口径搬进模板的写法。
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
			// 没有活跃客户的月份不畸形：出一格 0（已到达）或一格空格（还没到）。
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
				// 只有「已到达且确实没人来」才是 0%；还没到的月份保留 0 数值但不展示。
				ActiveCustomers: 0,
			})
		}
		row.Cells = full
	}
	return out, nil
}

// addMonths 月首加 n 个月（天数差异不影响：入参已归一到月首）。
func addMonths(t time.Time, n int) time.Time { return t.AddDate(0, n, 0) }

// monthStartUTC 把任意时刻归一到所在自然月的月首（UTC）。
//
// 模型返回的 `date_trunc('month', …)` 已经是月首，但它的**时区**取决于列类型
// （无时区 timestamp 扫出来是 UTC，带时区则可能带偏移）。归一到 UTC 月首之后，
// 「相差几个月」就只是两个 year/month 的算术，不受扫描时区影响。
func monthStartUTC(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// layoutMonth 月份串格式（"2026-10"）。
//
// 不复用 pkg/utils 的 layout：那边是「时刻」的格式（秒/天/JSON），没有「月份」这一档，
// 而往 utils 里加一个只被本文件用的常量会把一个页面级约定升成全局约定。
// 展示用的中文月份串（"2026年10月"）在下面直接拼 —— 它是文案，不是数据格式。
const layoutMonth = "2006-01"

// monthsBetween 两个（已归一到月首的）时刻相差几个自然月。
func monthsBetween(from, to time.Time) int {
	return (to.Year()-from.Year())*12 + int(to.Month()) - int(from.Month())
}

// retentionPct 留存率（百分比，保留一位小数）；分母为 0 给 0。
//
// 分母理论上不会为 0（每个群至少有一个首单客户），但**宁可给 0 也不给 NaN**：
// NaN 会让模板渲染出「NaN%」且不报错，而那个页面看起来是正常的。
func retentionPct(active, size int64) float64 {
	if size <= 0 {
		return 0
	}
	return math.Round(float64(active)/float64(size)*1000) / 10
}

// retentionLabel 百分比展示串。
func retentionLabel(pct float64) string {
	return fmt.Sprintf("%.1f%%", pct)
}
