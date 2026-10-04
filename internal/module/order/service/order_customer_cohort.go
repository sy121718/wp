package orderservice

// order_customer_cohort.go — 群组留存矩阵的组装（Cohort 分析页）。
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
	"go_wp/pkg/utils"
)

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
