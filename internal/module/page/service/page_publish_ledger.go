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
	"time"

	pagemodel "go_wp/internal/module/page/model"
	pubcontract "go_wp/internal/module/publication/contract"
	"go_wp/pkg/logger"
)

// ErrPublishLedgerUnavailable 发布回执登记失败（无法判定状态，因此不切换访问面）。
var ErrPublishLedgerUnavailable = errors.New("发布回执登记失败，未切换访问面")

// beginPublishReceipt 登记 pending 回执；失败返回空串（调用方据此中止发布）。
func (s *Service) beginPublishReceipt(ctx context.Context, projectID, pageID, path, lang, toArtifactID string) string {
	if s == nil || s.routes == nil {
		return ""
	}
	id, err := s.routes.BeginPublishReceipt(ctx, &pubcontract.BeginPublishReceiptReq{
		ProjectID: projectID, Path: path, PageID: pageID,
		ToArtifactID: toArtifactID, Lang: lang,
	})
	if err != nil {
		logger.Scene("publication").With("pageId", pageID).With("path", path).
			Error(err, "发布回执登记失败（不切换访问面）")
		return ""
	}
	return id
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

// RecoverPendingPublications 启动恢复：判定未结案的发布回执该补完成还是该回滚。
//
// 只在证据充分时补完成（符号链接确实指向本次产物）：判定错会写出错误的活跃指针，
// 而「不判定、只告警」至少不会把状态改得更糟，所以证据不足一律走回滚分支并记日志。
func (s *Service) RecoverPendingPublications(ctx context.Context) (recovered, rolledBack int, err error) {
	if s == nil || s.routes == nil || s.publication == nil {
		return 0, 0, nil
	}
	pending, lerr := s.routes.ListPendingReceipts(ctx)
	if lerr != nil {
		return 0, 0, lerr
	}
	for _, item := range pending {
		// 只处理手工页面的访问面切换回执：自动发布实例的恢复属于 presentation 的职责
		// （它的活跃指针在别的表上，用同一套恢复逻辑会写错地方）。
		if item.Action != publishReceiptActionName || item.SourceType != "page" {
			continue
		}
		done, rerr := s.recoverOne(ctx, item)
		if rerr != nil {
			logger.Scene("publication").With("receiptId", item.ID).Error(rerr, "发布回执恢复失败")
			continue
		}
		if done {
			recovered++
		} else {
			rolledBack++
		}
	}
	if recovered > 0 || rolledBack > 0 {
		logger.Scene("publication").With("recovered", recovered).With("rolledBack", rolledBack).
			Info("发布回执恢复完成")
	}
	return recovered, rolledBack, nil
}

// publishReceiptActionName 与 publication 侧登记的 Action 一致（筛出切换类回执）。
const publishReceiptActionName = "switch_active"

// recoverOne 判定单条回执。返回 true 表示已补完成，false 表示已标回滚。
func (s *Service) recoverOne(ctx context.Context, item pubcontract.PendingReceiptResp) (bool, error) {
	state, ierr := s.publication.Inspect(item.Path)
	if ierr != nil {
		s.abortPublishReceipt(ctx, item.ID, "读取访问面状态失败")
		return false, nil
	}
	// 访问面当前指向的产物 hash（Kind 不是 page 时说明这个路径现在是重定向或空）。
	actualHash := ""
	if state != nil && state.Locator != nil {
		actualHash = strings.TrimPrefix(state.Locator.Key, "artifacts/")
	}
	expectedHash, herr := s.model.ArtifactHashByID(ctx, item.ToArtifactID)
	if herr != nil {
		s.abortPublishReceipt(ctx, item.ID, "读取回执产物失败")
		return false, nil
	}
	if actualHash == "" || expectedHash == "" || actualHash != expectedHash {
		// 切换没发生（或指向的还是旧产物）：数据库保持原样即可，只结案。
		logger.Scene("publication").With("receiptId", item.ID).With("path", item.Path).
			With("actual", actualHash).With("expected", expectedHash).
			Warn("未结案的发布回执判定为「未生效」，标记回滚")
		s.abortPublishReceipt(ctx, item.ID, "访问面未指向本次产物")
		return false, nil
	}
	// 切换已生效、数据库没跟上：补齐活跃指针与路由行（两步都是幂等写）。
	page, perr := s.model.GetByID(ctx, item.SourceID, "")
	if perr != nil {
		s.abortPublishReceipt(ctx, item.ID, "页面不存在")
		return false, nil
	}
	lang := buildLang(item.Lang)
	if merr := s.model.MarkPublishedLang(ctx, pagemodel.PublicationRecord{
		PageID: page.ID, Lang: lang, ActivePath: item.Path,
		ArtifactID: item.ToArtifactID, ArtifactHash: expectedHash, PublishedAt: time.Now().UTC(),
	}); merr != nil {
		return false, merr
	}
	if _, aerr := s.routes.Activate(ctx, &pubcontract.ActivateReq{
		ProjectID: page.ProjectID, Path: item.Path, PageID: page.ID, ArtifactID: item.ToArtifactID,
	}); aerr != nil {
		return false, aerr
	}
	if cerr := s.completePublishReceipt(ctx, item.ID); cerr != nil {
		return false, cerr
	}
	logger.Scene("publication").With("pageId", page.ID).With("path", item.Path).
		Info("发布在崩溃前已生效，已补齐数据库状态")
	return true, nil
}
