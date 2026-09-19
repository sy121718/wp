package webhookservice

// webhook_service.go — webhook 模块 Service 本体（OSS-006 / SEC-015）。

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"encoding/json"
	webhookcontract "go_wp/internal/module/webhook/contract"
	webhookdto "go_wp/internal/module/webhook/dto"
	webhookenums "go_wp/internal/module/webhook/enums"
	webhookmodel "go_wp/internal/module/webhook/model"
	"go_wp/pkg/crypto"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
	"gorm.io/gorm"
	"net/http"
)

// Service webhook 域服务：只持本模块 model。
type Service struct {
	m *webhookmodel.WebhookModel
	// cipherSecret 端点签名密钥的加密密钥（装配期注入 app.secret）。
	cipherSecret string
	// client 出站 HTTP 客户端（测试可注入假 Transport）。
	client *http.Client
}

// NewService 构造。
func NewService(m *webhookmodel.WebhookModel) *Service {
	return &Service{m: m, client: newWebhookClient()}
}

// SetCipherSecret 注入敏感配置加密密钥。
func (s *Service) SetCipherSecret(secret string) { s.cipherSecret = secret }

// SetHTTPClient 测试注入口（注入受限客户端的替身）。
func (s *Service) SetHTTPClient(c *http.Client) { s.client = c }

// 编译期断言：本 service 实现模块对外契约。
var (
	_ webhookcontract.EndpointService = (*Service)(nil)
	_ webhookcontract.Dispatcher      = (*Service)(nil)
)

// ---- 端点管理（管理员后台配置白名单） ----

// CreateEndpoint 新建端点：URL 立即过 SSRF 校验，密钥加密落库。
//
// 密钥**必填**：出站请求恒带 HMAC 签名，没有密钥的端点是个发不出可验签请求的坏条目 ——
// 与其静默建一条收件方永远验不过的记录，不如在建的时候就说清楚。
func (s *Service) CreateEndpoint(ctx context.Context, req *webhookdto.SaveEndpointReq) (item *webhookdto.EndpointItem, err error) {
	eventType := strings.TrimSpace(req.EventType)
	if eventType == "" {
		return nil, errors.New(webhookenums.ErrEventTypeRequired)
	}
	targetURL := strings.TrimSpace(req.TargetURL)
	if targetURL == "" {
		return nil, errors.New(webhookenums.ErrTargetURLRequired)
	}
	if verr := validateWebhookURL(targetURL); verr != nil {
		return nil, verr
	}
	if req.Secret == "" {
		return nil, errors.New(webhookenums.ErrSecretRequired)
	}
	cipher, cerr := s.encryptSecret(req.Secret)
	if cerr != nil {
		return nil, cerr
	}
	now := time.Now()
	e := &webhookmodel.WebhookEndpointEntity{
		EventType:    eventType,
		TargetURL:    targetURL,
		SecretCipher: cipher,
		Description:  req.Description,
		Status:       webhookenums.EndpointStatusEnabled,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if req.Status != nil {
		e.Status = *req.Status
	}
	if derr := s.m.CreateEndpoint(ctx, e); derr != nil {
		return nil, derr
	}
	return toEndpointItem(e), nil
}

// UpdateEndpoint 更新端点（PATCH 语义：空字段即不改）。
//
// 先确认端点存在再改：Update 对不存在的 id 影响 0 行且不报错，
// 调用方会拿到「保存成功」而其实什么都没发生。改 URL / 密钥时重新校验与重新加密。
func (s *Service) UpdateEndpoint(ctx context.Context, req *webhookdto.SaveEndpointReq) (item *webhookdto.EndpointItem, err error) {
	if req.ID == 0 {
		return nil, errors.New(webhookenums.ErrInvalidParam)
	}
	if _, gerr := s.m.GetEndpoint(ctx, req.ID); gerr != nil {
		if errors.Is(gerr, gorm.ErrRecordNotFound) {
			return nil, errors.New(webhookenums.ErrEndpointNotFound)
		}
		return nil, gerr
	}

	fields := map[string]any{"update_time": time.Now()}
	if et := strings.TrimSpace(req.EventType); et != "" {
		fields["event_type"] = et
	}
	if u := strings.TrimSpace(req.TargetURL); u != "" {
		if verr := validateWebhookURL(u); verr != nil {
			return nil, verr
		}
		fields["target_url"] = u
	}
	if req.Description != "" {
		fields["description"] = req.Description
	}
	if req.Secret != "" {
		cipher, cerr := s.encryptSecret(req.Secret)
		if cerr != nil {
			return nil, cerr
		}
		fields["secret_cipher"] = cipher
	}
	if req.Status != nil {
		fields["status"] = *req.Status
	}
	if uerr := s.m.UpdateEndpoint(ctx, req.ID, fields); uerr != nil {
		return nil, uerr
	}
	e, gerr := s.m.GetEndpoint(ctx, req.ID)
	if gerr != nil {
		return nil, gerr
	}
	return toEndpointItem(e), nil
}

// DeleteEndpoint 删除端点（投递日志由外键 ON DELETE CASCADE 一并清理）。
func (s *Service) DeleteEndpoint(ctx context.Context, id uint64) error {
	if id == 0 {
		return errors.New(webhookenums.ErrInvalidParam)
	}
	return s.m.DeleteEndpoint(ctx, id)
}

// SetEndpointStatus 启停端点。
//
// 停用只影响**新**投递的派发（DispatchEvent 只取启用端点）；
// 已经入队的投递仍会执行 —— 改状态不该让在途任务凭空消失。
func (s *Service) SetEndpointStatus(ctx context.Context, id uint64, status int) (err error) {
	if id == 0 || (status != webhookenums.EndpointStatusEnabled && status != webhookenums.EndpointStatusDisabled) {
		return errors.New(webhookenums.ErrInvalidParam)
	}
	return s.m.UpdateEndpoint(ctx, id, map[string]any{
		"status":      status,
		"update_time": time.Now(),
	})
}

// ListEndpoints 列出端点（eventType 为空即全部）。
//
// 取**全部**而不只取启用的：后台要看得见停用的端点并把它重新启用。
// 只取启用是派发侧的需求（DispatchEvent 走 model 自行传 onlyEnabled=true）。
func (s *Service) ListEndpoints(ctx context.Context, eventType string) (list []*webhookdto.EndpointItem, err error) {
	rows, lerr := s.m.ListEndpoints(ctx, eventType, false)
	if lerr != nil {
		return nil, lerr
	}
	list = make([]*webhookdto.EndpointItem, 0, len(rows))
	for _, e := range rows {
		list = append(list, toEndpointItem(e))
	}
	return list, nil
}

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

// toEndpointItem Entity → 对外条目。**刻意不含密钥**：只给「配没配」的布尔。
func toEndpointItem(e *webhookmodel.WebhookEndpointEntity) *webhookdto.EndpointItem {
	return &webhookdto.EndpointItem{
		ID:          e.ID,
		EventType:   e.EventType,
		TargetURL:   e.TargetURL,
		Description: e.Description,
		Status:      e.Status,
		HasSecret:   e.SecretCipher != "",
		CreateTime:  utils.NewJSONTime(e.CreatedAt),
		UpdateTime:  utils.NewJSONTime(e.UpdatedAt),
	}
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

// ---- 事件分发 ----

// DispatchEvent 向某事件类型的全部启用端点派发一次投递：
// 每个端点建一条 pending 投递日志并入队，由 worker 异步签名发送。
// 返回成功入队的端点数。事件负载超限整体拒绝（不静默截断）。
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
	// 扇出的 N 条投递日志**同一个事务**（AGENTS.md「写操作的事务与回滚」）：
	// 逐条各自提交时中途失败会留下「一部分端点有日志、一部分没有」—— 少了日志的那些
	// 端点**永远不会**收到这次事件（没有任何东西会再来派发它），而调用方看到的错误
	// 只是「本次派发失败」，重试又会给已经拿到日志的端点再发一遍。
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

	// 入队在事务**之外**：队列（asynq/Redis）是另一个系统，进不了 PG 事务。
	// 队列侧的「入队」与 DB 侧的「投递日志」不是真正的两处持久化写入，而是
	// 「真源 + 派生动作」：日志是唯一真源（pending 即待投），入队只是让它更快被取走。
	// 所以这里不做跨系统补偿事务，而是靠可重放的对账入口补齐（webhook_replay.go）。
	//
	// 为什么不能反过来把入队塞进事务：worker 可能在事务提交前就取到任务，
	// 回库读不到那一行 → DeliverDelivery 按「日志不存在 = 任务无意义」返回成功 →
	// 这次投递被静默丢掉，且日志行随后才提交，看起来完全正常。
	for _, d := range rows {
		if eerr := enqueueWebhookDeliver(WebhookDeliverPayload{DeliveryID: d.ID}); eerr != nil {
			// 队列不可用：日志保留 pending，返回错误让调用方感知，不假装已派发。
			// 已入队的部分不回收（回收会让它们再也不被投递）；未入队的那部分由
			// ReplayPendingDeliveries 重放（幂等：worker 只认领 pending 或租约过期的 delivering）。
			return n, eerr
		}
		n++
	}
	return n, nil
}

// deliverLease 一次「投递中」认领的租约。
//
// 下界来自出站 HTTP 客户端超时（ClientTimeout = 15s，见 deliver.go）：租约必须**显著大于**
// 一次正常投递可能花的时间，否则一个慢目标会被判成「worker 卡死」并被抢占 —— 那等于把
// 「并发重复投递」换成「慢目标重复投递」，而抢占态本来就是为了消除重复投递。
// 5 分钟 = 20 倍余量：真卡了 5 分钟，重投的收益远大于重复投一次的代价。
//
// 与 replayMinAge 的关系：那个判的是「pending 多久算入队丢了」，这个判的是「delivering
// 多久算认领者死了」。两者量级相同但**判的不是同一件事**，所以各留一个常量 —— 合并之后
// 改任何一个都会静默改变另一条语义（这正是「一个常量服务两处判据」的典型代价）。
const deliverLease = 5 * time.Minute

// DeliverDelivery worker 入口：按投递日志执行一次签名投递并回写结果。
//
// 顺序是**先认领、再投递、最后落定**（本批修掉的窄窗口）：此前是「先读出来看是不是
// pending、再发请求」，两个 worker 可以同时通过那道读检查，于是同一个外部系统收到两份
// 同样的签名请求，而代码只在事后留一条警告日志。认领（ClaimDelivery）把「谁在投这一条」
// 变成行上的一次原子更新：第二个 worker 拿到 0 行直接退出，**根本不会发出请求**。
//
// 返回 error 时队列层按重试上限继续重试。
func (s *Service) DeliverDelivery(ctx context.Context, deliveryID uint64) (err error) {
	at := time.Now()
	claimed, cerr := s.m.ClaimDelivery(ctx, deliveryID, deliverLease, at)
	if cerr != nil {
		return cerr
	}
	if !claimed {
		// 0 行 = 日志不存在 / 已被别的 worker 认领且租约未过期 / 已落定。
		// 三者对本次调用的含义相同：不投。**刻意不区分** —— 要区分只能再多读一次，
		// 而那次读的结论立刻就会过期。只留 Debug 级痕迹让「任务到过 worker」可查，
		// 不把「重复入队」这种正常现象记成告警（否则每次 asynq 重试都会刷一条 Warn）。
		logger.Scene("webhook").With("delivery_id", deliveryID).
			Debug("webhook 投递未认领：已落定、已被其它 worker 抢占或日志不存在")
		return nil
	}

	d, gerr := s.m.GetDelivery(ctx, deliveryID)
	if gerr != nil {
		// 认领成功后读不到同一行：要么读取本身失败，要么这一行在 UPDATE 与 SELECT 之间
		// 被删了（端点删除级联到投递日志）。两种都要把认领**退回去** —— 留着 delivering
		// 会让这一条白等一整个租约才被重放捡起来。
		if rerr := s.m.ReleaseDeliveryClaim(ctx, deliveryID, time.Now()); rerr != nil {
			logger.Scene("webhook").With("delivery_id", deliveryID).
				Error(rerr, "webhook 投递认领回退失败（该行会停在 delivering 直到租约过期）")
		}
		if errors.Is(gerr, gorm.ErrRecordNotFound) {
			return nil // 日志不存在：任务无意义，直接成功避免无谓重试。
		}
		return gerr
	}

	// 先把「这次投递的结论」算出来，最后**只落一次库**。
	//
	// 三点理由：
	//   · 结果回写只发生一次，不可能出现「先写 failed 再写 delivered」这类自相矛盾的序列；
	//   · 真正的 HTTP 投递（慢、外部、不可回滚）留在事务外，且不占任何行锁；
	//   · 落定走条件更新（WHERE status='delivering'）——它是幂等守卫，也是并发下的
	//     「这条已经被别人处理过 / 被抢占后由别人落定」的判据（见 model 的 MarkDeliveryResult）。
	//
	// 落定时刻与上面的认领时刻刻意不是同一个值：认领时刻是**租约的起点**（写进 update_time），
	// 这里写的是这次投递真正结束的时刻。
	settledAt := time.Now()
	finalStatus := webhookenums.DeliveryStatusFailed
	respStatus := 0
	lastErr := ""
	var deliverErr error

	ep, gerr := s.m.GetEndpoint(ctx, d.EndpointID)
	if gerr != nil {
		// 端点不存在或已删除：重试无意义，落定 failed 且不返回错误（不让队列空转）。
		lastErr = "端点不存在或已删除"
	} else if secret, derr := s.decryptSecret(ep.SecretCipher); derr != nil {
		// 密钥不可用：同样重试无意义。
		lastErr = derr.Error()
	} else {
		status, perr := postWebhook(ctx, s.client, ep.TargetURL, secret, d.EventType, d.ID, []byte(d.Payload))
		respStatus = status
		if perr == nil && status >= 200 && status < 300 {
			finalStatus = webhookenums.DeliveryStatusDelivered
		} else if perr != nil {
			lastErr = perr.Error()
			deliverErr = fmt.Errorf("webhook 投递未成功: %s", lastErr)
		} else {
			lastErr = fmt.Sprintf("远端返回非 2xx 状态（HTTP %d）", status)
			deliverErr = fmt.Errorf("webhook 投递未成功: %s", lastErr)
		}
	}

	affected, uerr := s.m.MarkDeliveryResult(ctx, d.ID, finalStatus, respStatus, lastErr, settledAt)
	if uerr != nil {
		return errors.Join(uerr, deliverErr)
	}
	if !affected {
		// 0 行 = 这一行已不是 delivering：要么别的 worker（或人工重投后新派出的 worker）
		// 先落定了，要么我们是被抢占的那一个（租约过期后由新认领者投完并落定）。
		// 这是并发下的正常现象，但必须留痕 —— 否则「同一条投递投了两遍」这件事
		// 在日志里完全看不出来（外部系统会收到两次同样的签名请求）。
		//
		// 注意：走到这里的**前提**是本 worker 已经真的把请求发出去了（落定只在投递之后），
		// 所以这条 Warn 是「可能已重复投递」的信号，而不只是记账冲突。
		logger.Scene("webhook").With("delivery_id", d.ID).With("endpoint_id", d.EndpointID).
			With("settled_as", finalStatus).
			Warn("webhook 投递结果未落库：该行已不是 delivering（本次请求可能已被重复投递）")
	}
	// 非 2xx / 网络错误：返回错误交给队列按上限重试。
	// 注意 failed 落定之后，队列的重试会因**认领守卫**直接跳过（既非 pending 也非租约过期的
	// delivering，ClaimDelivery 给 0 行）—— 因此「再试一次」实际由**人工重投**
	//（RetryDelivery 把 failed 改回 pending）与 ReplayPendingDeliveries 承担；这是既有语义。
	return deliverErr
}

// encryptSecret / decryptSecret 密钥加解密（cipherSecret 未配置时明确报错）。
func (s *Service) encryptSecret(plain string) (string, error) {
	if s.cipherSecret == "" {
		return "", errors.New("未配置加密密钥，无法保存 webhook 签名密钥")
	}
	return crypto.Encrypt(plain, s.cipherSecret)
}

func (s *Service) decryptSecret(cipherText string) (string, error) {
	if s.cipherSecret == "" {
		return "", errors.New("未配置加密密钥，无法解密 webhook 签名密钥")
	}
	return crypto.Decrypt(cipherText, s.cipherSecret)
}
