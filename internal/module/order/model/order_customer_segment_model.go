package model

// order_customer_segment_model.go — 按客户分段取 **user_id 列表**（客户列表筛选用）。
//
// 与 order_customer_growth_model.go 共用 orderCustomerCTEs 与三个分段条件常量：
// 客户概览页的「新客 12 人」与客户列表按「新客」筛出来的条数必须相等（docs/17 §P7 的对账闸门）。
// 这只有两边查的是**同一段 SQL** 才能保证 —— 各写一份时，把 `>=` 改成 `>` 不会让另一处变红，
// 只会让两个页面的数字悄悄差一个人。
//
// 为什么返回 id 列表而不是「在订单表上直接 JOIN 出客户行」：orders 与 users 属于不同模块，
// 跨模块 JOIN 会绕过两边各自的表归属（也让 RLS 作用域变成两套）。这里只交出「谁在这段里」，
// 客户行仍旧由客户模块自己查。

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

// ErrSegmentUnknown 认不出的分段名（白名单之外一律拒，不静默回落到「全部」——
// 静默回落会让 UI 上一个拼错的分段名显示成「全部客户」，而看起来是对的）。
var ErrSegmentUnknown = errors.New("order: 未知的客户分段")

// CustomerSegment 客户分段名（对外取值）。
type CustomerSegment string

const (
	// CustomerSegmentNew 首单落在区间内。
	CustomerSegmentNew CustomerSegment = "new"
	// CustomerSegmentReturning 首单在区间之前（老客）。
	CustomerSegmentReturning CustomerSegment = "returning"
	// CustomerSegmentRepurchasing 区间内下单 ≥ 2 单。
	CustomerSegmentRepurchasing CustomerSegment = "repurchasing"
)

// customerSegmentWhere 段名 → SQL 条件片段（**白名单**，不接受调用方拼进来的任何字符串）。
//
// 返回的第二个值表示「这个条件是否需要区间上界参数」—— 拼 SQL 与拼参数必须同步进行，
// 让调用方自己数参数个数迟早会数错，而数错的后果是参数错位（不报错、结果全错）。
func customerSegmentWhere(segment CustomerSegment) (where string, needsFrom bool, ok bool) {
	switch segment {
	case CustomerSegmentNew:
		return customerSegmentWhereNew, true, true
	case CustomerSegmentReturning:
		return customerSegmentWhereReturning, true, true
	case CustomerSegmentRepurchasing:
		return customerSegmentWhereRepurchasing, false, true
	}
	return "", false, false
}

// customerSegmentIDsSQL 分段内的 user_id + 过滤后的总数。
//
// 总数用 `COUNT(*) OVER ()` 而不是另跑一条 COUNT：分页窗口与总数必须来自**同一次**执行，
// 否则两条之间落的新单会让「共 12 条」与「列出来的 11 条」同时为真，而用户只看到列表少了人。
// 窗口函数在 LIMIT 之前求值，所以这个数是过滤后的全量，不受 limit 影响。
//
// ORDER BY r.user_id 是分页正确性的前提：没有稳定排序时 PostgreSQL 每次可以给出不同的
// 前 N 行，翻页会漏人（而不报错）。
const customerSegmentIDsSQL = orderCustomerCTEs + `
SELECT r.user_id, COUNT(*) OVER () AS total
  FROM ranged r
  JOIN firsts f ON f.user_id = r.user_id
 WHERE `

// CustomerSegmentIDsByRange 取分段内的 user_id（按 user_id 升序分页）。
//
// projectID 必填、区间必填：orders 带 FORCE 策略，缺作用域在非超级角色下静默返回空集，
// 调用方拿到的会是「这个分段一个人都没有」—— 与真实的空分段无法区分。
func (m *OrderModel) CustomerSegmentIDsByRange(ctx context.Context, projectID string, from, to time.Time, segment CustomerSegment, limit, offset int) (ids []int64, total int64, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, 0, ErrProjectRequired
	}
	if from.IsZero() || to.IsZero() {
		return nil, 0, ErrRangeRequired
	}
	where, needsFrom, ok := customerSegmentWhere(segment)
	if !ok {
		return nil, 0, ErrSegmentUnknown
	}
	if limit <= 0 {
		return nil, 0, ErrRangeRequired
	}
	statuses := strings.Join(paidStatuses, ",")

	// 参数顺序：CTE 的 6 个（见 orderCustomerCTEs）→ 分段条件的区间上界（若有）→ limit / offset。
	args := []any{projectID, from, to, statuses, projectID, statuses}
	if needsFrom {
		args = append(args, from)
	}
	args = append(args, limit, offset)

	type row struct {
		UserID int64 `gorm:"column:user_id"`
		Total  int64 `gorm:"column:total"`
	}
	var rows []row
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Raw(customerSegmentIDsSQL+where+" ORDER BY r.user_id LIMIT ? OFFSET ?", args...).
			Scan(&rows).Error
	})
	if err != nil {
		return nil, 0, err
	}
	ids = make([]int64, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.UserID)
		total = r.Total // 每行都带全量计数，取最后一行的即可（空结果时保持 0）
	}
	return ids, total, nil
}
