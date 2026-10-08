package webhookservice

// 投递记录只读展示，重投把失败记录重新入队，不改写历史状态。

// 只入队、不出站：慢目标或超时不会拖住发布方的事务。

// 安全护栏全部固定在客户端里：
//   · 每次投递前重新过 validateWebhookURL（端点 URL 可能事后被改，不能只信入库时）；
//   · 不跟随重定向 —— 302 指向内网会绕过 DNS 检查，直接拒绝；
//   · 连接 / TLS / 整体三级超时全部有硬上限；
//   · 请求头是固定集合（签名 + 事件元数据），不接收、不合并任何调用方传入的头，
//     系统内部凭据（会话密钥、API key）因此没有透传通道。

// 端点 URL / 密钥以投递时刻的库内值为准，管理员事后停用对未派发任务立即生效。

// # 为什么「入队」不能和「写投递日志」放在同一个事务里
//
// 投递链上有两处写：
//
//   1. PostgreSQL 的 webhook_deliveries（pending 行 = 这次投递的存在证明，也是唯一真源）；
//   2. asynq / Redis 的任务队列（让 worker 尽快取走它）。
//
// 它们**跨系统**，没有共同的事务边界。两种顺序都有洞：
//
//   · 先入队、后写库（或把入队塞进事务）：worker 可能在事务提交前取到任务，
//     回库读不到那一行 → DeliverDelivery 按「日志不存在 = 任务无意义」返回成功 →
//     这次投递被静默丢掉，而日志行随后才提交，看起来一切正常。
//   · 先写库、后入队：入队在提交后失败（Redis 抖动 / 队列未启用），
//     那一行停在 pending 且**永远**不会被取走。
//
// 本模块选后者，并用「pending 行 = outbox」把第二个洞补上：
//
//   · 真源是**未落定的行**（pending 或 delivering），不是队列里的任务 —— 队列只是加速器，
//     丢了不影响正确性；
//   · ReplayPendingDeliveries 把两类「早就该有结果的」行重新入队：
//     ① pending 且超过正常入队延迟（入队丢了 / 队列从未启用）；
//     ② delivering 且认领租约已过期（worker 崩溃 / 卡死）。
//     **幂等**（worker 的认领守卫是「只有 pending、或租约已过期的 delivering 才投」，
//     重复入队最坏是多投一次 —— 这与队列自身重试的语义一致）+ **留痕**（逐条结构化日志，
//     抢占类单独计数）+ **可重放**（反复调用直到两类都清零，不会造成状态叠加）。
//
// 为什么不做「跨系统补偿事务」：补偿只在能精确判定「对方一定没做」时才有意义，
// 而队列无法查询「这条任务在不在」。所以这里不做猜测式的回滚，只做可重放的重投。

// 为什么必须有这个文件：webhook_replay.go 的 ReplayPendingDeliveries 此前**没有任何
// 调用方**，而它存在的理由恰恰是「入队在提交后失败（Redis 抖动 / 队列未启用）时那一行
// 会永远停在 pending」（见该文件头部对两种写入顺序与各自的洞的分析）。没有时间驱动，
// 那个「永远」就是字面意思 —— 只有人恰好发现才会重投。
//
// 形状与既有调度器逐项一致（page_publish_converge.go / analytics_retention.go /
// context.Background()、目标方法报错只记日志、任何 panic 一律 recover 不拖垮进程。
//
// 纳入判断（与 media 的变体补偿同一把尺子）：ReplayPendingDeliveries 的文档注释明写
// **幂等**（重放只入队、不改任何投递行状态；worker 的守卫是「只有 pending 才投」，
// 重复入队最坏是多投一次）+ **可重放**（pending 未清零时再调一次即可继续），
// 且它重放的是「尚未落定的投递任务」这一**派生的工作项**，不是篡改已定案的数据 ——
// 满足「幂等 + 只动可重新生成的东西」，因此纳入调度。
//
// 本文件不复制任何判据：陈旧阈值仍是 webhook_replay.go 的 replayMinAge，
// 这里只决定「多久驱动一次」。

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"gorm.io/gorm"

	"go_wp/internal/module/webhook/dto"
	"go_wp/internal/module/webhook/enums"
	"go_wp/internal/module/webhook/model"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
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

// 投递硬上限（OSS-006 安全模型第 3/4 条）。
const (
	DialTimeout     = 5 * time.Second
	TLSHandTimeout  = 5 * time.Second
	ClientTimeout   = 15 * time.Second
	MaxPayloadBytes = 64 << 10 // 请求体（事件 JSON）上限
	MaxResponseRead = 4 << 10  // 响应体只读 4KB（仅用于错误信息，不信任内容）
)

// newWebhookClient 构造受限 HTTP 客户端（包内共享；测试可注入 Transport）。
func newWebhookClient() *http.Client {
	return &http.Client{
		Timeout: ClientTimeout,
		// 不跟随重定向：重定向目标未过 SSRF 校验，一律视为失败。
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

// postWebhook 发出一次签名投递，返回远端 HTTP 状态码。
//
// body 必须是已序列化的事件 JSON（调用方负责 ≤ MaxPayloadBytes）；
// secret 是端点解密后的明文密钥，只在本次调用内存中存在。
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
	// 固定头集：不透传任何内部凭据（安全模型第 6 条）。
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

	// last_error 一律落**可翻译的编码**（enums.EncodeDeliveryErr）：这一列会经
	// DeliveryItem.LastError 原样返回给调用方，落中文等于把语言固化进投递日志。
	// 出站客户端给出的原文（perr.Error() 可能带对方主机名）不编码，原样落 —— 那是外部事实。
	ep, gerr := s.m.GetEndpoint(ctx, d.EndpointID)
	if gerr != nil {
		// 端点不存在或已删除：重试无意义，落定 failed 且不返回错误（不让队列空转）。
		lastErr = webhookenums.EncodeDeliveryErr(webhookenums.DeliveryErrEndpointMissing, nil)
	} else if secret, derr := s.decryptSecret(ep.SecretCipher); derr != nil {
		// 密钥不可用：同样重试无意义。原文只进日志（crypto 的细节不是给调用方看的），
		// 对外给可翻译的归口原因。
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

// 重放参数默认值与上限。
const (
	replayDefaultLimit = 200
	replayMaxLimit     = 2000
	// replayMinAge 只重投「pending 已超过这个时长」的投递。
	//
	// 必须显著大于队列的正常投递延迟：刚派发完的 pending 很可能正在被 worker 处理，
	// 把它再入队一次就制造了一次重复投递。5 分钟对 webhook（秒级目标、15s 客户端超时）
	// 是足够宽松的余量 —— 真卡了 5 分钟，重投的收益远大于重复投一次的代价。
	replayMinAge = 5 * time.Minute
)

// ReplayPendingDeliveriesReq 重放参数。
type ReplayPendingDeliveriesReq struct {
	// Limit 本次最多重放多少条（默认 200，上限 2000）。
	Limit int
	// MinAge 覆盖默认的「pending 陈旧阈值」；<=0 用 replayMinAge。
	MinAge time.Duration
	// DeliverLease 覆盖默认的「delivering 认领租约」；<=0 用 deliverLease。
	//
	// 单独一个字段而不是复用 MinAge：两者判的不是同一件事（一个是「队列没把它捡起来」，
	// 一个是「捡起来的 worker 死了」）。合成一个可调参数，测试里就再也分不开这两种场景。
	DeliverLease time.Duration
}

// ReplayItem 单条重放记录（留痕）。
type ReplayItem struct {
	DeliveryID uint64 `json:"deliveryId"`
	EndpointID uint64 `json:"endpointId"`
	EventType  string `json:"eventType"`
	Attempts   int    `json:"attempts"`
	CreateTime string `json:"createTime"`
	// Reclaimed 这一条是从「租约过期的 delivering」抢回来的（worker 崩溃 / 卡死）。
	// 与「pending 重投」分开标：前者意味着**可能有一次已经发出去的请求**，
	// 排障时必须能一眼看出哪些行属于这种情形。
	Reclaimed bool   `json:"reclaimed"`
	Result    string `json:"result"`
}

// ReplayReport 重放报告。
//
// pending 与 delivering 分开计数：两种积压的**病因完全不同**（队列没启 / 端点没入队 vs
// worker 崩溃或卡死），压成一个数字会让排障的人看不出该去查哪里。
type ReplayReport struct {
	// PendingTotal 当前 pending 总数（含尚未达到陈旧阈值的，规模参考）。
	PendingTotal int64 `json:"pendingTotal"`
	// DeliveringTotal 当前 delivering 总数（正在投 / 认证领后卡住，规模参考）。
	DeliveringTotal int64 `json:"deliveringTotal"`
	Scanned         int   `json:"scanned"`
	Requeued        int   `json:"requeued"`
	// Reclaimed 其中来自「认领租约已过期」的条数（>0 意味着怀疑有 worker 死过，
	// 也意味着同一份事件可能被外部系统收到两次）。
	Reclaimed    int          `json:"reclaimed"`
	Failed       int          `json:"failed"`
	MinAge       string       `json:"minAge"`
	DeliverLease string       `json:"deliverLease"`
	Truncated    bool         `json:"truncated"`
	Items        []ReplayItem `json:"items"`
}

// ReplayPendingDeliveries 重放「早就该有结果」的投递：把长期停在 pending、或认领租约已过期的
// delivering 行重新入队。
//
// 幂等：重放**不改变任何投递行的状态**（只入队），worker 的认领守卫保证「已落定为
// delivered/failed 的行不会再投、租约未过期的 delivering 不会被别人抢」；重复调用只会对
// 同一批行再入队一次，最坏是多一次投递，不会叠加状态。
// 可重放：返回后两类仍有剩余（例如队列仍未恢复）时，再调一次即可继续。
//
// 抢占（把 delivering 抢回来）是**有代价**的：那条行的原认领者可能仍在网络上，请求可能已经
// 发出去了。所以它既在报告里单独计数、也在报告条目上打 Reclaimed，并单独记 Warn ——
// 让人知道「这一批里有多少条可能造成过重复投递」，而不是把它混进「pending 重投」这个
// 无害的数字里。
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

	// 两个阈值都相对同一个 now 取：分成两次 time.Now() 会让「待重投」的集合在两次取材之间漂移。
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
			// 队列不可用：**不动任何状态**（那一行仍是原状态，下次重放会再试一次），
			// 记录原因后继续处理后面的行 —— 一条失败不该让整批停下。
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
			// 抢占类单独一条 Warn：原认领者可能已经把那次请求发出去了，外部系统可能收到两份。
			// 这是**要人知道**的事，不能和常规重投混在一行 Info 里。
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
	// webhookReplayInterval 重放间隔。
	//
	// 取值必须**显著大于** replayMinAge（webhook_replay.go，5 分钟）：MinAge 是「陈旧」
	// 的判据 —— 比它新的 pending 很可能正在被 worker 处理，把它再入队一次就是制造一次
	// 重复投递。1 小时 = MinAge 的 12 倍：既给 worker 足够余量（正常投递是秒级、
	// 客户端超时 15s），又让「入队丢了」的投递在一个小时内被发现并重投 ——
	// 而这个窗口的缩短正是本调度的全部价值。
	//
	// 反过来，间隔若接近或小于 MinAge，每一轮都会对着同一批刚派发的 pending 重投，
	// 退化成「把重复投递做成了常态」。
	webhookReplayInterval = time.Hour
	// webhookReplayTimeout 单轮重放的超时。
	//
	// 一轮的重活全在入队（Redis 往返）上：即便队列整个不可用，失败也是快速返回，
	// 5 分钟足够处理上限 2000 条。
	webhookReplayTimeout = 5 * time.Minute
)

// webhookReplayRoundResult 一轮重放的结论（真实计数，供日志与用例断言）。
type webhookReplayRoundResult struct {
	err          error
	pendingTotal int64
	scanned      int
	requeued     int
	failed       int
	minAge       string
	truncated    bool
}

// runWebhookReplayRound 跑一轮重放：调既有的 ReplayPendingDeliveries，把 panic 收敛成
// 本轮失败。
//
// 参数用 nil 让 service 走它自己的默认值（limit / minAge）—— 调度器不该在这里
// 抄一份默认值，那是第二份真相。
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

// runWebhookReplayLoop 调度循环本体：先跑一次，再按 interval 等间隔重复。
//
// 抽成「接受一轮动作」的形状是为了可测：目标方法要数据库与队列，模块内单测不碰这些
// 依赖，但调度语义（首跑 / 等间隔 / 单轮 panic 不致命）与动作内容无关。
// 返回的 stop 关闭后循环退出（生产不调用，测试用它收尾）。
func runWebhookReplayLoop(round func(), interval time.Duration) (stop func()) {
	if interval <= 0 {
		interval = webhookReplayInterval
	}
	done := make(chan struct{})
	var once sync.Once
	go func() {
		// 首跑：与样板一致，先跑一次再等 ticker（不是先等一个间隔）。
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

// safeWebhookReplayRound 执行一轮并把 panic 收敛成日志。
//
// goroutine 里未被 recover 的 panic 会直接终止整个进程 —— 重放任务没有这种权力：
// 一轮动作的 bug 只该让这一轮没有结论，下一个间隔照常再来。
func safeWebhookReplayRound(round func()) {
	defer func() {
		if r := recover(); r != nil {
			logger.Scene("webhook").Error(fmt.Errorf("panic: %v", r),
				"webhook 投递重放单轮 panic（已收敛，下一轮照常；不影响进程）")
		}
	}()
	round()
}

// StartWebhookReplayScheduler 启动陈旧 pending 投递的重放调度（进程内 goroutine + ticker）。
func StartWebhookReplayScheduler(svc *Service) {
	if utils.IsTestProcess() {
		return // 测试进程不启动：调度首跑会动真实库与存储，测试的行为必须由用例自己触发（见 utils.IsTestProcess）。
	}
	startWebhookReplayScheduler(svc, webhookReplayInterval)
}

// StartWebhookReplaySchedulerWithInterval 同上，但可注入间隔（用例用）。
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

// logWebhookReplayRound 每轮记**一条**结构化日志（真实计数 + 耗时）。
//
// 与 service 内部日志的分工：ReplayPendingDeliveries 内部已有一条「本次重放做了什么」
// 的汇总 Warn，这一条回答「这一轮调度跑完了没、耗时多少」。两者读同一份报告结构体，
// 没有第二份判据。
//
// 分档：动作失败 → Error；有仍失败的 pending 或重放了任何一条 → Warn（重放条数 > 0
// 本身就意味着「有投递长期停在 pending」，那是需要人知道的不一致；失败条数 > 0 更是）；
// 否则 Info。
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
