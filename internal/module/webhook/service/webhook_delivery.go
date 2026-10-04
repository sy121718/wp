package webhookservice

// webhook_delivery.go — 投递记录查询与失败重投（后台可观测面）。
// 投递记录只读展示，重投把失败记录重新入队，不改写历史状态。

import (
	"context"
	"errors"
	"time"

	webhookdto "go_wp/internal/module/webhook/dto"
	webhookenums "go_wp/internal/module/webhook/enums"
	webhookmodel "go_wp/internal/module/webhook/model"
	"go_wp/pkg/utils"
	"gorm.io/gorm"
)

// ListDeliveries 投递日志（排障视图）。
func (s *Service) ListDeliveries(ctx context.Context, req *webhookdto.DeliveryListReq) (res *webhookdto.DeliveryListResp, err error) {
	if req == nil {
		req = &webhookdto.DeliveryListReq{}
	}
	page, size := normalizePage(req.Page, req.PageSize)
	total, cerr := s.m.CountDeliveries(ctx, req.EndpointID, req.EventType, req.Status)
	if cerr != nil {
		return nil, cerr
	}
	rows, lerr := s.m.ListDeliveries(ctx, req.EndpointID, req.EventType, req.Status, (page-1)*size, size)
	if lerr != nil {
		return nil, lerr
	}
	res = &webhookdto.DeliveryListResp{Items: make([]*webhookdto.DeliveryItem, 0, len(rows)), Total: total}
	for _, d := range rows {
		res.Items = append(res.Items, toDeliveryItem(d))
	}
	return res, nil
}

// RetryDelivery 重投一次失败的投递。
//
// worker 的认领守卫是「只有 pending、或租约已过期的 delivering 才投」，所以重投
// **必须先真的把状态改回 pending** —— 只入队的话任务到 worker 就被静默跳过，
// 界面上会显示「已重新入队」而实际什么都没发。
// attempts 不清零：它是「这条投递一共试过几次」的历史，清零会让排障看不出它失败过。
func (s *Service) RetryDelivery(ctx context.Context, id uint64) (err error) {
	if id == 0 {
		return errors.New(webhookenums.ErrInvalidParam)
	}
	d, gerr := s.m.GetDelivery(ctx, id)
	if gerr != nil {
		if errors.Is(gerr, gorm.ErrRecordNotFound) {
			return errors.New(webhookenums.ErrDeliveryNotFound)
		}
		return gerr
	}
	if d.Status == webhookenums.DeliveryStatusPending {
		return errors.New(webhookenums.ErrDeliveryNotPending)
	}
	if d.Status != webhookenums.DeliveryStatusFailed {
		return errors.New(webhookenums.ErrDeliveryNotFailed)
	}
	// 状态回退走**条件更新**（WHERE status='failed'）而不是「读出来判断再写回去」：
	// 后者在两个重投请求（或重投与 worker 收尾）并发时会双写，且两次入队让同一条投递
	// 被投两遍。受影响行数 0 说明状态在刚才那一瞬间变了（别处已经重投或已落定），
	// 按「这条已经不是 failed」拒绝，别硬写。
	switched, uerr := s.m.MarkDeliveryRetryable(ctx, id, time.Now())
	if uerr != nil {
		return uerr
	}
	if !switched {
		if cur, gerr := s.m.GetDelivery(ctx, id); gerr == nil && cur != nil && cur.Status == webhookenums.DeliveryStatusPending {
			return errors.New(webhookenums.ErrDeliveryNotPending)
		}
		return errors.New(webhookenums.ErrDeliveryNotFailed)
	}
	// 入队放在状态回写**之后**且事务之外（队列是 Redis 侧的写，跨系统）：
	// worker 的认领守卫是「只有 pending、或租约已过期的 delivering 才投」，所以「先真的改回 pending，再入队」
	// 是唯一能让任务不被静默跳过的顺序。入队失败时那一行仍是 pending ——
	// 由 ReplayPendingDeliveries 重放补齐（本函数返回错误让调用方感知）。
	return enqueueWebhookDeliver(WebhookDeliverPayload{DeliveryID: id})
}

// toDeliveryItem Entity → 排障条目，负载只给预览。
func toDeliveryItem(d *webhookmodel.WebhookDeliveryEntity) *webhookdto.DeliveryItem {
	return &webhookdto.DeliveryItem{
		ID:             d.ID,
		EndpointID:     d.EndpointID,
		EventType:      d.EventType,
		PayloadPreview: truncateRunes(d.Payload, webhookdto.PayloadPreviewBytes),
		PayloadBytes:   len(d.Payload),
		Status:         d.Status,
		Attempts:       d.Attempts,
		ResponseStatus: d.ResponseStatus,
		LastError:      d.LastError,
		CreateTime:     utils.NewJSONTime(d.CreatedAt),
		UpdateTime:     utils.NewJSONTime(d.UpdatedAt),
	}
}

// truncateRunes 按**字符**截断。按字节切会把多字节字符劈成非法 UTF-8，
// 响应序列化时变成替换符，排障看到的负载首行就是乱的。
func truncateRunes(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return string(r[:limit])
}

// normalizePage 分页兜底口径（与其它模块一致：默认 1 页 20 条，上限 200）。
func normalizePage(page, size int) (p, s int) {
	p = page
	if p < 1 {
		p = 1
	}
	s = size
	if s < 1 {
		s = 20
	}
	if s > 200 {
		s = 200
	}
	return p, s
}
