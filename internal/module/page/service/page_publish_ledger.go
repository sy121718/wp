// page_publish_ledger.go —— 发布回执的登记/结案与启动恢复（审计 TX-009）。
//
// 问题：发布是「先切访问面（符号链接原子替换）→ 再写数据库活跃指针」。中间崩溃时
// 线上可能已经生效、也可能没有，而数据库里没有任何痕迹 —— 只能人工比对。
//
// 做法：切换前登记 pending 回执（publication_receipts，与路由回执同一张表），
// 成功后结案；启动时扫未结案的回执，按「符号链接实际指向哪个产物」判定：
//
//	指向本次要激活的产物 → 切换已生效、DB 没跟上 → 补完成（幂等）
//	指向别处或不存在     → 切换没发生 → 标已回滚（不碰文件与数据库）
package pageservice

import (
	"context"
	"errors"
	"strings"

	pagemodel "go_wp/internal/module/page/model"
	pubcontract "go_wp/internal/module/publication/contract"
	"go_wp/pkg/logger"
)

// ErrPublishLedgerUnavailable 发布回执登记失败（无法判定状态，因此不切换访问面）。
var ErrPublishLedgerUnavailable = errors.New("发布回执登记失败，未切换访问面")

// publishReceiptInput 登记 pending 回执的入参。
//
// 用结构体而不是 8 个位置参数：三种动作（发布 / 改 URL / 回滚）各有几个专属字段
// （改 URL 的 OldPath 与 Redirect、回滚的 FromArtifactID），位置参数下一个调用点
// 传错顺序编译器不会报错，而错的是「恢复按哪个路径补哪一步」。
type publishReceiptInput struct {
	// Action 见 pubcontract.ReceiptAction*：决定恢复流程分派到哪个补齐例程。
	Action         string
	ProjectID      string
	PageID         string
	Path           string
	Lang           string
	FromArtifactID string
	// ToArtifactID 本次要激活的产物行 id；Action 为 update_url 时必须留空
	// （产物按新路径现编译，登记时还不存在对应行，见 publication 侧字段注释）。
	ToArtifactID string
	// OldPath 切换前该语言的线上路径（改 URL / 回滚用它处置旧路径）。
	OldPath string
	// Redirect 旧路径是否登记为 301（仅 update_url 使用）。
	Redirect bool
}

// beginPublishReceipt 登记 pending 回执，返回回执 id 与失败原因（AR2-002 / TX-009）。
//
// 必须在访问面切换**之前**调用：切换是不可逆的副作用，登记放在之后，崩溃窗口里就
// 查不到「这次发布发生过」。拿不到 id 一律按硬失败返回 —— 带着未知状态去切访问面，
// 正是这条回执要消灭的分裂状态，调用方据此中止发布（不静默继续）。
//
// 唯一不算失败的是路由契约未装配（s.routes == nil）：此时发布链本身也不写路由行，
// 属于「这台实例没有回执设施」而不是「登记失败」，返回空 id + nil 让发布按原样继续。
func (s *Service) beginPublishReceipt(ctx context.Context, in publishReceiptInput) (string, error) {
	if s == nil || s.routes == nil {
		return "", nil
	}
	action := strings.TrimSpace(in.Action)
	if action == "" {
		action = pubcontract.ReceiptActionSwitchActive
	}
	id, err := s.routes.BeginPublishReceipt(ctx, &pubcontract.BeginPublishReceiptReq{
		ProjectID: in.ProjectID, Path: in.Path, PageID: in.PageID,
		FromArtifactID: in.FromArtifactID, ToArtifactID: in.ToArtifactID, Lang: in.Lang,
		Action: action, OldPath: in.OldPath, Redirect: in.Redirect,
	})
	if err == nil && strings.TrimSpace(id) == "" {
		err = errors.New("发布回执登记未返回 id")
	}
	if err != nil {
		logger.Scene("publication").With("pageId", in.PageID).With("path", in.Path).With("action", action).
			Error(err, "发布回执登记失败（不切换访问面）")
		return "", ErrPublishLedgerUnavailable
	}
	return id, nil
}

// publishedArtifactIDOf 取该语言当前激活产物 id（page_publications 为真源）。
//
// 回执要如实记录「切换前指着哪个产物」：from/to 两侧合起来才是这次发布是从哪个版本
// 切到哪个版本，只记 to 会让回滚与审计失去「从哪来」的依据。口径与 publishedPathOf
// 一致 —— 没有该语言的激活记录（本语言从未发布）返回空串，绝不拿别的语言的产物顶替。
func (s *Service) publishedArtifactIDOf(ctx context.Context, page *pagemodel.PageEntity, lang string) string {
	if s == nil || page == nil {
		return ""
	}
	pub, err := s.model.GetPublication(ctx, page.ID, lang)
	if err != nil || pub == nil || pub.ArtifactID == nil {
		return ""
	}
	return *pub.ArtifactID
}

// publishWindowFaultHit 触发「访问面已切换、数据库尚未写入」窗口的故障注入点
// （生产恒为 nil，只多一次判空；见 Service.publishWindowFault 字段注释）。
func (s *Service) publishWindowFaultHit() error {
	if s == nil || s.publishWindowFault == nil {
		return nil
	}
	return s.publishWindowFault()
}

// keepPublishReceiptPending 收敛「无法判定」的失败：访问面可能已经切换，此刻把回执
// 标成 rolled_back 会让恢复流程以为这次发布从未生效 —— 错误判定比不判定更糟。
// 因此只记日志、保留 pending，交给收敛例程按符号链接的实际指向补齐或回滚。
//
// 同时推一次进程内快通道：调用点都在**事务已经落定之后**（DB 事务失败的回滚已发生、
// 或文件系统那一步已失败），此刻正是「库里有 pending、线上状态未知」——
// 让收敛立刻跑一遍，而不是等下一个定时间隔（正常情况下毫秒级收敛，见
// page_publish_converge.go）。信号非阻塞，且收敛本身幂等。
func (s *Service) keepPublishReceiptPending(receiptID, reason string) {
	if s == nil || strings.TrimSpace(receiptID) == "" {
		return
	}
	logger.Scene("publication").With("receiptId", receiptID).
		Warn("发布中断在「已切换访问面、数据库未跟上」窗口，回执保持 pending 交收敛例程判定：" + reason)
	s.NotifyPendingReceipt()
}

// completePublishReceipt 结案（访问面与数据库已一致）。
func (s *Service) completePublishReceipt(ctx context.Context, receiptID string) error {
	if s == nil || s.routes == nil || receiptID == "" {
		return nil
	}
	return s.routes.CompletePublishReceipt(ctx, receiptID)
}

// abortPublishReceipt 结案为已回滚（切换没发生或无副作用失败）。
func (s *Service) abortPublishReceipt(ctx context.Context, receiptID, reason string) {
	if s == nil || s.routes == nil || receiptID == "" {
		return
	}
	if err := s.routes.AbortPublishReceipt(ctx, receiptID); err != nil {
		logger.Scene("publication").With("receiptId", receiptID).
			Error(err, "发布回执结案失败（"+reason+"）")
	}
}

// recoverableReceiptActions 本模块认得的访问面切换回执动作名（与 publication 的登记端
// 同一份词汇表）。
//
// 为什么必须显式在册：publication_receipts 是**共用表** —— 只处理手工页面的回执
// （自动发布实例的活跃指针在别的表上，用同一套恢复逻辑会写错地方），
// 而路由变更回执（Activate 的 activate / Redirect 的 redirect）也落在这里、
// 由各自事务内的结案与补偿处理：拿发布形态的判据去「补齐」一条路由回执，
// 会把路由变更纠正成发布状态。
//
// 三种动作「已切换访问面、数据库没跟上」时要补的数据库步骤不同，因此还必须按动作分派。
// 这份清单同时是**领取条件**（ClaimPendingReceipts 的 action IN）与**判定条件**
// （recoverableReceiptAction）——两处各写一套筛选条件，迟早出现「领得到却判不出来」的空转。
var recoverableReceiptActions = []string{
	pubcontract.ReceiptActionSwitchActive,
	pubcontract.ReceiptActionUpdateURL,
	pubcontract.ReceiptActionRollback,
}

// recoverableReceiptAction 判断某个动作是否在本模块的收敛范围内（不在册一律跳过，
// 而不是当成发布形态硬套）。
func recoverableReceiptAction(action string) bool {
	for _, known := range recoverableReceiptActions {
		if known == action {
			return true
		}
	}
	return false
}

// recoverOne 判定单条回执。返回 true 表示已补完成，false 表示已标回滚。
func (s *Service) recoverOne(ctx context.Context, item pubcontract.PendingReceiptResp) (bool, error) {
	// 新形态先分派：改 URL（路径迁移 + 旧路径处置）与回滚（活跃指针 + 旧路径下线）
	// 要补的步骤与发布不同，各自实现见 page_publish_recover.go。
	switch item.Action {
	case pubcontract.ReceiptActionUpdateURL:
		return s.recoverUpdateURLReceipt(ctx, item)
	case pubcontract.ReceiptActionRollback:
		return s.recoverRollbackReceipt(ctx, item)
	}
	return s.recoverSwitchActiveReceipt(ctx, item)
}
