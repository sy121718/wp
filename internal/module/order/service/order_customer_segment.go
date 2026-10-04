package orderservice

// order_customer_segment.go — 按客户分段取 user_id 列表（客户列表筛选用）。
//
// 与 order_customer_growth.go 的分工：那边回答「有多少人」，这边回答「是哪些人」。
// 两者在 model 层共用 orderCustomerCTEs，所以口径不分叉 —— 客户概览页的数字与
// 客户列表按同条件筛出来的条数必须相等（docs/17 §P7 的对账闸门）。

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
	segment, ok := knownCustomerSegments[strings.TrimSpace(req.Segment)]
	if !ok {
		return nil, errors.New(orderenums.ErrInvalidParam)
	}
	from, to, err := normalizeRangeWindow(req.From, req.To, time.Now())
	if err != nil {
		return nil, err
	}
	limit, offset := utils.NormalizeLimitOffset(req.Limit, req.Offset, customerSegmentDefaultLimit, customerSegmentMaxLimit)

	ids, total, err := s.orders.CustomerSegmentIDsByRange(ctx, req.ProjectID, from, to, segment, limit, offset)
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
		To:      to.AddDate(0, 0, -1).Format(utils.LayoutDay),
		Segment: string(segment),
		UserIDs: ids,
		Total:   total,
	}, nil
}
