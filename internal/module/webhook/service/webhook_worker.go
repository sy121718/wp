package webhookservice

// webhook_worker.go — worker 侧的投递执行：按投递 id 回库取端点与负载后出站并落定。
// 端点 URL / 密钥以投递时刻的库内值为准，管理员事后停用对未派发任务立即生效。

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	webhookenums "go_wp/internal/module/webhook/enums"
	"go_wp/pkg/logger"
	"gorm.io/gorm"
)

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
