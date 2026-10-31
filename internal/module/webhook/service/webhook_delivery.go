package webhookservice

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"gorm.io/gorm"

	webhookdto "go_wp/internal/module/webhook/dto"
	webhookenums "go_wp/internal/module/webhook/enums"
	webhookmodel "go_wp/internal/module/webhook/model"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

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

func (s *Service) RetryDelivery(ctx context.Context, id uint64) (err error) {
	if id == 0 {
		return webhookenums.Err(webhookenums.ErrInvalidParam)
	}
	d, gerr := s.m.GetDelivery(ctx, id)
	if gerr != nil {
		if errors.Is(gerr, gorm.ErrRecordNotFound) {
			return webhookenums.Err(webhookenums.ErrDeliveryNotFound)
		}
		return gerr
	}
	if d.Status == webhookenums.DeliveryStatusPending {
		return webhookenums.Err(webhookenums.ErrDeliveryNotPending)
	}
	if d.Status != webhookenums.DeliveryStatusFailed {
		return webhookenums.Err(webhookenums.ErrDeliveryNotFailed)
	}

	switched, uerr := s.m.MarkDeliveryRetryable(ctx, id, time.Now())
	if uerr != nil {
		return uerr
	}
	if !switched {
		if cur, gerr := s.m.GetDelivery(ctx, id); gerr == nil && cur != nil && cur.Status == webhookenums.DeliveryStatusPending {
			return webhookenums.Err(webhookenums.ErrDeliveryNotPending)
		}
		return webhookenums.Err(webhookenums.ErrDeliveryNotFailed)
	}

	return enqueueWebhookDeliver(WebhookDeliverPayload{DeliveryID: id})
}

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

func (s *Service) DispatchEvent(ctx context.Context, eventType string, payload any) (n int, err error) {
	body, merr := json.Marshal(payload)
	if merr != nil {
		return 0, fmt.Errorf("webhook 事件负载序列化失败: %w", merr)
	}
	if len(body) > MaxPayloadBytes {
		return 0, fmt.Errorf("webhook 事件负载 %d 字节超出上限 %d", len(body), MaxPayloadBytes)
	}

	endpoints, lerr := s.m.ListEndpoints(ctx, eventType, true)
	if lerr != nil {
		return 0, lerr
	}
	now := time.Now()

	rows := make([]*webhookmodel.WebhookDeliveryEntity, 0, len(endpoints))
	for _, ep := range endpoints {
		rows = append(rows, &webhookmodel.WebhookDeliveryEntity{
			EndpointID: ep.ID,
			EventType:  eventType,
			Payload:    string(body),
			Status:     webhookenums.DeliveryStatusPending,
			CreatedAt:  now,
			UpdatedAt:  now,
		})
	}
	if terr := s.m.Transaction(ctx, func(tx *gorm.DB) error {
		return s.m.CreateDeliveriesTx(ctx, tx, rows)
	}); terr != nil {
		return 0, terr
	}

	for _, d := range rows {
		if eerr := enqueueWebhookDeliver(WebhookDeliverPayload{DeliveryID: d.ID}); eerr != nil {

			return n, eerr
		}
		n++
	}
	return n, nil
}

const (
	DialTimeout     = 5 * time.Second
	TLSHandTimeout  = 5 * time.Second
	ClientTimeout   = 15 * time.Second
	MaxPayloadBytes = 64 << 10
	MaxResponseRead = 4 << 10
)

func newWebhookClient() *http.Client {
	return &http.Client{
		Timeout: ClientTimeout,

		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			DialContext:         dialWebhookContext,
			TLSHandshakeTimeout: TLSHandTimeout,
			TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
		},
	}
}

func postWebhook(ctx context.Context, client *http.Client, targetURL, secret, eventType string, deliveryID uint64, body []byte) (status int, err error) {
	if len(body) > MaxPayloadBytes {
		return 0, fmt.Errorf("webhook 请求体 %d 字节超出上限 %d", len(body), MaxPayloadBytes)
	}
	if verr := validateURL(targetURL); verr != nil {
		return 0, verr
	}

	now := time.Now().UnixNano()
	req, rerr := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(body))
	if rerr != nil {
		return 0, fmt.Errorf("构造 webhook 请求失败: %w", rerr)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(HeaderEvent, eventType)
	req.Header.Set(HeaderDelivery, fmt.Sprintf("%d", deliveryID))
	req.Header.Set(HeaderTimestamp, TimestampHeaderValue(now))
	req.Header.Set(HeaderSignature, SignPayload(secret, now, body))

	resp, derr := client.Do(req)
	if derr != nil {
		return 0, fmt.Errorf("webhook 投递失败: %w", derr)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, MaxResponseRead))
	return resp.StatusCode, nil
}

const deliverLease = 5 * time.Minute

func (s *Service) DeliverDelivery(ctx context.Context, deliveryID uint64) (err error) {
	at := time.Now()
	claimed, cerr := s.m.ClaimDelivery(ctx, deliveryID, deliverLease, at)
	if cerr != nil {
		return cerr
	}
	if !claimed {

		logger.Scene("webhook").With("delivery_id", deliveryID).
			Debug("webhook 投递未认领：已落定、已被其它 worker 抢占或日志不存在")
		return nil
	}

	d, gerr := s.m.GetDelivery(ctx, deliveryID)
	if gerr != nil {

		if rerr := s.m.ReleaseDeliveryClaim(ctx, deliveryID, time.Now()); rerr != nil {
			logger.Scene("webhook").With("delivery_id", deliveryID).
				Error(rerr, "webhook 投递认领回退失败（该行会停在 delivering 直到租约过期）")
		}
		if errors.Is(gerr, gorm.ErrRecordNotFound) {
			return nil
		}
		return gerr
	}

	settledAt := time.Now()
	finalStatus := webhookenums.DeliveryStatusFailed
	respStatus := 0
	lastErr := ""
	var deliverErr error

	ep, gerr := s.m.GetEndpoint(ctx, d.EndpointID)
	if gerr != nil {

		lastErr = webhookenums.EncodeDeliveryErr(webhookenums.DeliveryErrEndpointMissing, nil)
	} else if secret, derr := s.decryptSecret(ep.SecretCipher); derr != nil {

		logger.Scene("webhook").With("endpoint_id", ep.ID).Error(derr, "webhook 投递：端点签名密钥不可用")
		lastErr = webhookenums.EncodeDeliveryErr(webhookenums.DeliveryErrCipherUnavailable, nil)
	} else {
		status, perr := postWebhook(ctx, s.client, ep.TargetURL, secret, d.EventType, d.ID, []byte(d.Payload))
		respStatus = status
		if perr == nil && status >= 200 && status < 300 {
			finalStatus = webhookenums.DeliveryStatusDelivered
		} else if perr != nil {
			lastErr = perr.Error()
			deliverErr = fmt.Errorf("webhook 投递未成功: %s", lastErr)
		} else {
			lastErr = webhookenums.EncodeDeliveryErr(webhookenums.DeliveryErrRemoteStatus,
				map[string]string{webhookenums.DeliveryErrArgHTTP: strconv.Itoa(status)})
			deliverErr = fmt.Errorf("webhook 投递未成功: %s", lastErr)
		}
	}

	affected, uerr := s.m.MarkDeliveryResult(ctx, d.ID, finalStatus, respStatus, lastErr, settledAt)
	if uerr != nil {
		return errors.Join(uerr, deliverErr)
	}
	if !affected {

		logger.Scene("webhook").With("delivery_id", d.ID).With("endpoint_id", d.EndpointID).
			With("settled_as", finalStatus).
			Warn("webhook 投递结果未落库：该行已不是 delivering（本次请求可能已被重复投递）")
	}

	return deliverErr
}

const (
	replayDefaultLimit = 200
	replayMaxLimit     = 2000

	replayMinAge = 5 * time.Minute
)

type ReplayPendingDeliveriesReq struct {
	Limit int

	MinAge time.Duration

	DeliverLease time.Duration
}

type ReplayItem struct {
	DeliveryID uint64 `json:"deliveryId"`
	EndpointID uint64 `json:"endpointId"`
	EventType  string `json:"eventType"`
	Attempts   int    `json:"attempts"`
	CreateTime string `json:"createTime"`

	Reclaimed bool   `json:"reclaimed"`
	Result    string `json:"result"`
}

type ReplayReport struct {
	PendingTotal int64 `json:"pendingTotal"`

	DeliveringTotal int64 `json:"deliveringTotal"`
	Scanned         int   `json:"scanned"`
	Requeued        int   `json:"requeued"`

	Reclaimed    int          `json:"reclaimed"`
	Failed       int          `json:"failed"`
	MinAge       string       `json:"minAge"`
	DeliverLease string       `json:"deliverLease"`
	Truncated    bool         `json:"truncated"`
	Items        []ReplayItem `json:"items"`
}

func (s *Service) ReplayPendingDeliveries(ctx context.Context, req *ReplayPendingDeliveriesReq) (*ReplayReport, error) {
	if req == nil {
		req = &ReplayPendingDeliveriesReq{}
	}
	limit := req.Limit
	if limit <= 0 {
		limit = replayDefaultLimit
	}
	if limit > replayMaxLimit {
		limit = replayMaxLimit
	}
	minAge := req.MinAge
	if minAge <= 0 {
		minAge = replayMinAge
	}
	lease := req.DeliverLease
	if lease <= 0 {
		lease = deliverLease
	}

	report := &ReplayReport{MinAge: minAge.String(), DeliverLease: lease.String(), Items: []ReplayItem{}}
	pendingTotal, err := s.m.CountDeliveriesByStatus(ctx, webhookenums.DeliveryStatusPending)
	if err != nil {
		return nil, err
	}
	report.PendingTotal = pendingTotal
	deliveringTotal, derr := s.m.CountDeliveriesByStatus(ctx, webhookenums.DeliveryStatusDelivering)
	if derr != nil {
		return nil, derr
	}
	report.DeliveringTotal = deliveringTotal
	if pendingTotal == 0 && deliveringTotal == 0 {
		return report, nil
	}

	now := time.Now()
	rows, lerr := s.m.ListStaleDeliveries(ctx, now.Add(-minAge), now.Add(-lease), limit)
	if lerr != nil {
		return nil, lerr
	}
	report.Scanned = len(rows)
	report.Truncated = pendingTotal+deliveringTotal > int64(limit)
	for _, d := range rows {
		if ctx.Err() != nil {
			break
		}
		reclaimed := d.Status == webhookenums.DeliveryStatusDelivering
		if eerr := enqueueWebhookDeliver(WebhookDeliverPayload{DeliveryID: d.ID}); eerr != nil {

			report.Failed++
			logger.Scene("webhook").With("delivery_id", d.ID).With("status", d.Status).
				With("queue_error", eerr.Error()).
				Error(eerr, "webhook 投递重放入队失败（该行状态未变，可再次重放）")
			continue
		}
		report.Requeued++
		if reclaimed {
			report.Reclaimed++
		}
		report.Items = append(report.Items, ReplayItem{
			DeliveryID: d.ID, EndpointID: d.EndpointID, EventType: d.EventType,
			Attempts:   d.Attempts,
			CreateTime: d.CreatedAt.Format(time.RFC3339),
			Reclaimed:  reclaimed,
			Result:     "已重新入队（worker 仅认领 pending 或租约过期的 delivering，重复入队幂等）",
		})
		if reclaimed {

			logger.Scene("webhook").With("delivery_id", d.ID).With("endpoint_id", d.EndpointID).
				With("event_type", d.EventType).With("attempts", d.Attempts).With("lease", lease.String()).
				Warn("webhook 投递重放：delivering 租约已过期，按 worker 崩溃/卡死抢占（该次投递可能已发出，留意重复）")
			continue
		}
		logger.Scene("webhook").With("delivery_id", d.ID).With("endpoint_id", d.EndpointID).
			With("event_type", d.EventType).With("attempts", d.Attempts).
			Info("webhook 投递重放：陈旧 pending 已重新入队")
	}
	logger.Scene("webhook").With("pending_total", report.PendingTotal).
		With("delivering_total", report.DeliveringTotal).
		With("scanned", report.Scanned).With("requeued", report.Requeued).
		With("reclaimed", report.Reclaimed).
		With("failed", report.Failed).With("min_age", report.MinAge).
		With("deliver_lease", report.DeliverLease).
		Warn("webhook 投递重放完成（只入队、不改状态；失败项下次可再重放）")
	return report, nil
}

const (
	webhookReplayInterval = time.Hour

	webhookReplayTimeout = 5 * time.Minute
)

type webhookReplayRoundResult struct {
	err          error
	pendingTotal int64
	scanned      int
	requeued     int
	failed       int
	minAge       string
	truncated    bool
}

func runWebhookReplayRound(ctx context.Context, svc *Service) (res webhookReplayRoundResult) {
	if svc == nil {
		return res
	}
	defer func() {
		if r := recover(); r != nil {
			res.err = fmt.Errorf("panic: %v", r)
		}
	}()
	report, err := svc.ReplayPendingDeliveries(ctx, nil)
	if err != nil {
		res.err = err
		return res
	}
	res.pendingTotal = report.PendingTotal
	res.scanned = report.Scanned
	res.requeued = report.Requeued
	res.failed = report.Failed
	res.minAge = report.MinAge
	res.truncated = report.Truncated
	return res
}

func runWebhookReplayLoop(round func(), interval time.Duration) (stop func()) {
	if interval <= 0 {
		interval = webhookReplayInterval
	}
	done := make(chan struct{})
	var once sync.Once
	go func() {

		safeWebhookReplayRound(round)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				safeWebhookReplayRound(round)
			case <-done:
				return
			}
		}
	}()
	return func() { once.Do(func() { close(done) }) }
}

func safeWebhookReplayRound(round func()) {
	defer func() {
		if r := recover(); r != nil {
			logger.Scene("webhook").Error(fmt.Errorf("panic: %v", r),
				"webhook 投递重放单轮 panic（已收敛，下一轮照常；不影响进程）")
		}
	}()
	round()
}

func StartWebhookReplayScheduler(svc *Service) {
	if utils.IsTestProcess() {
		return
	}
	startWebhookReplayScheduler(svc, webhookReplayInterval)
}

func StartWebhookReplaySchedulerWithInterval(svc *Service, interval time.Duration) {
	startWebhookReplayScheduler(svc, interval)
}

func startWebhookReplayScheduler(svc *Service, interval time.Duration) {
	if svc == nil {
		return
	}
	runWebhookReplayLoop(func() {
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), webhookReplayTimeout)
		defer cancel()
		res := runWebhookReplayRound(ctx, svc)
		logWebhookReplayRound(res, time.Since(start))
	}, interval)
}

func logWebhookReplayRound(res webhookReplayRoundResult, cost time.Duration) {
	entry := logger.Scene("webhook").
		With("pending_total", res.pendingTotal).
		With("scanned", res.scanned).
		With("requeued", res.requeued).
		With("failed", res.failed).
		With("min_age", res.minAge).
		With("truncated", res.truncated).
		With("cost_ms", cost.Milliseconds())
	switch {
	case res.err != nil:
		entry.Error(res.err, "webhook 投递重放调度：本轮动作失败（下一轮重试；pending 行不受影响）")
	case res.failed > 0:
		entry.Warn("webhook 投递重放完成：部分投递入队失败，仍留在 pending 等待下一轮")
	case res.requeued > 0:
		entry.Warn("webhook 投递重放完成：有投递长期停在 pending，已重新入队（队列此前丢过任务，需人工看目标是否可达）")
	default:
		entry.Info("webhook 投递重放完成：没有陈旧 pending（队列正常）")
	}
}
