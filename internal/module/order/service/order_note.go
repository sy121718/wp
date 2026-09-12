package orderservice

// order_note.go — 订单后台备注（BIZ-1）。
//
// 备注是「人对这张单的判断」：客服记下客户说了什么、仓库记下为什么改地址。
// 它**不是状态流转**，所以不写 status_logs —— 那条链回答的是「订单处在哪一步、什么时候变过」，
// 把备注变更混进去，会让「这单什么时候发的货」变成要翻记录才能看出来。

import (
	"context"
	"errors"
	"strings"
	"time"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
)

// maxAdminNoteLen 备注长度上限（与列宽一致，超了直接拒绝而不是静默截断）。
const maxAdminNoteLen = 500

// UpdateOrderNote 改订单的后台备注。
func (s *Service) UpdateOrderNote(ctx context.Context, req *orderdto.UpdateOrderNoteReq) (res *orderdto.OrderResp, err error) {
	if req == nil || req.OrderID == 0 {
		return nil, errors.New(orderenums.ErrInvalidParam)
	}
	note := strings.TrimSpace(req.AdminNote)
	if len(note) > maxAdminNoteLen {
		return nil, errors.New(orderenums.ErrNoteTooLong)
	}
	head, err := s.orders.GetByID(ctx, req.OrderID)
	if err != nil {
		return nil, err
	}
	if head == nil {
		return nil, errors.New(orderenums.ErrOrderNotFound)
	}
	if err = s.orders.UpdateFields(ctx, head.ID, map[string]any{
		"admin_note":  note,
		"update_time": time.Now(),
	}); err != nil {
		return nil, err
	}
	updated, err := s.orders.GetByID(ctx, head.ID)
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, errors.New(orderenums.ErrOrderNotFound)
	}
	return toOrderResp(updated), nil
}
