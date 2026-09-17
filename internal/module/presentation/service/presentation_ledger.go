// presentation_ledger.go — 多语言发布的发布回执与启动恢复（审计 AR2-003 / AR2-004）。
//
// 问题（AR2-004）：多语言发布的「逐语言激活」是访问面上不可逆的一步，而路由登记
// （page_routes）此前只是发布之后的 best-effort 副作用 —— 登记失败只写一条 Warn，
// publishAllLangs 照常返回成功。于是 URL 文件已可访问、路由账本却缺行：后续占用
// 预检 / 回滚 / 删除 / GC 依据路由表得出错误结论，留下无法管理的线上路径。
//
// 做法（与 page 侧 page_publish_ledger.go 同一张表、同一套状态机）：
//  1. 每语言在**切换访问面之前**登记 pending 回执（记 path / lang / from→to 产物）；
//  2. 激活 → 路由登记 → 结案写入都成功，才把回执标 committed；
//  3. 路由登记失败：回执保留 pending（访问面已切换，不能标 rolled_back 谎称没发生），
//     错误向上返回，不再静默成功；
//  4. 启动恢复逐条判定：访问面确实指向本次产物 → 幂等补齐路由登记后结案；
//     指向别处或路径已过期 → 标 rolled_back（不碰文件与数据库）。
//
// 两个恢复器各扫各的 source_type：page 侧写 pages 的活跃指针，presentation 侧的
// 活跃指针与语言账本在另外两张表上，用同一套落点会写错地方。
package presentationservice

import (
	"context"
	"errors"
	"strings"

	presentationmodel "go_wp/internal/module/presentation/model"
	pubcontract "go_wp/internal/module/publication/contract"
	"go_wp/internal/pipeline"
	"go_wp/pkg/logger"

	"gorm.io/gorm"
)

const (
	// presentationReceiptAction 访问面切换类回执的动作名（与 publication 侧登记时
	// 写死的 publishReceiptAction 逐字一致；恢复时按它筛掉普通路由变更回执）。
	presentationReceiptAction = "switch_active"
	// presentationReceiptSourceType 本模块回执的来源类型（publication_receipts.source_type）。
	presentationReceiptSourceType = "presentation"
	// artifactLocatorKeyPrefix 访问面 Locator.Key 的产物前缀（Inspect 还原 locator 后剥掉）。
	artifactLocatorKeyPrefix = "artifacts/"
)

// beginPublishReceipt 在**切换访问面之前**登记一条 pending 回执，返回回执 id。
//
// 登记失败必须让本次发布中止（返回错误，调用方不切换访问面）：没有回执就无从判定
// 「这次切换到底发生过没有」，崩溃窗口里只能人工比对符号链接与数据库（TX-009 的取舍，
// 与 page 侧一致）。routes 为 nil（降级装配 / 单元测试）时无账本可记账，返回空 id。
func (s *Service) beginPublishReceipt(ctx context.Context, inst *presentationmodel.InstanceEntity,
	path, lang, fromArtifactID, toArtifactID string) (receiptID string, err error) {
	if s.routes == nil {
		return "", nil
	}
	id, berr := s.routes.BeginPublishReceipt(ctx, &pubcontract.BeginPublishReceiptReq{
		ProjectID:      inst.ProjectID,
		Path:           path,
		PresentationID: inst.ID,
		FromArtifactID: strings.TrimSpace(fromArtifactID),
		ToArtifactID:   toArtifactID,
		Lang:           lang,
	})
	if berr != nil {
		logger.Scene("publication").With("instanceId", inst.ID).With("url", path).
			Error(berr, "多语言发布回执登记失败（不切换访问面）")
		return "", berr
	}
	return id, nil
}

// completePublishReceipt 结案（访问面、路由与数据库三者已一致）。
func (s *Service) completePublishReceipt(ctx context.Context, receiptID string) error {
	if s == nil || s.routes == nil || strings.TrimSpace(receiptID) == "" {
		return nil
	}
	return s.routes.CompletePublishReceipt(ctx, receiptID)
}

// abortPublishReceipt 结案为已回滚（切换没发生，或无副作用的失败）。
func (s *Service) abortPublishReceipt(ctx context.Context, receiptID, reason string) {
	if s == nil || s.routes == nil || strings.TrimSpace(receiptID) == "" {
		return
	}
	if err := s.routes.AbortPublishReceipt(ctx, receiptID); err != nil {
		logger.Scene("publication").With("receiptId", receiptID).
			Error(err, "多语言发布回执结案失败（"+reason+"）")
	}
}

// RecoverPendingPublications 启动恢复：判定未结案的多语言发布回执该补齐还是该回滚。
//
// 判据与 page 侧同源（符号链接实际指向哪个产物），只在证据充分时补齐：
// 判定错会写出错误的发布账本，而「不判定、只告警」至少不会把状态改得更糟，
// 所以证据不足一律走回滚分支并记日志。
func (s *Service) RecoverPendingPublications(ctx context.Context) (recovered, rolledBack int, err error) {
	if s == nil || s.routes == nil || s.publication == nil {
		return 0, 0, nil
	}
	pending, lerr := s.routes.ListPendingReceipts(ctx)
	if lerr != nil {
		return 0, 0, lerr
	}
	for _, item := range pending {
		if item.Action != presentationReceiptAction || item.SourceType != presentationReceiptSourceType {
			continue
		}
		done, rerr := s.recoverOneReceipt(ctx, item)
		if rerr != nil {
			logger.Scene("publication").With("receiptId", item.ID).
				Error(rerr, "多语言发布回执恢复失败")
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
			Info("多语言发布回执恢复完成")
	}
	return recovered, rolledBack, nil
}

// recoverOneReceipt 判定单条回执。返回 true 表示已补齐，false 表示已标回滚。
func (s *Service) recoverOneReceipt(ctx context.Context, item pubcontract.PendingReceiptResp) (bool, error) {
	inst, ierr := s.locateInstanceForReceipt(ctx, item.ProjectID, item.SourceID)
	if ierr != nil {
		s.abortPublishReceipt(ctx, item.ID, "实例不存在")
		return false, nil
	}
	expectedHash, herr := s.artifactHashByID(ctx, item.ToArtifactID)
	if herr != nil || expectedHash == "" {
		s.abortPublishReceipt(ctx, item.ID, "回执记录的产物行不存在")
		return false, nil
	}
	// 过期回执：该语言的当前访问路径已经不是回执记录的位置（改过 URL、或站点语言
	// 清单变了）。按旧路径补齐会写出一条不属于当前发布的账本行，宁可只结案。
	if !s.receiptPathStillCurrent(ctx, inst, item) {
		logger.Scene("publication").With("instanceId", inst.ID).With("url", item.Path).
			Warn("未结案的多语言发布回执路径已过期，标记回滚")
		s.abortPublishReceipt(ctx, item.ID, "回执路径已非该语言的当前访问路径")
		return false, nil
	}
	actualHash := s.activeArtifactHash(item.Path)
	if actualHash == "" || actualHash != expectedHash {
		logger.Scene("publication").With("instanceId", inst.ID).With("url", item.Path).
			With("actual", actualHash).With("expected", expectedHash).
			Warn("未结案的多语言发布回执判定为「未生效」，标记回滚")
		s.abortPublishReceipt(ctx, item.ID, "访问面未指向本次产物")
		return false, nil
	}
	// 访问面已切换、路由账本没跟上：补齐登记（同归属者幂等）后结案。
	if rerr := s.registerRoute(ctx, inst, item.Path, item.ToArtifactID); rerr != nil {
		return false, rerr
	}
	if cerr := s.completePublishReceipt(ctx, item.ID); cerr != nil {
		return false, cerr
	}
	logger.Scene("publication").With("instanceId", inst.ID).With("url", item.Path).
		Info("多语言发布在失败/崩溃前已切换访问面，已补齐路由登记")
	return true, nil
}

// artifactHashByID 按产物行 id 取内容哈希（空 id 视为无记录）。
func (s *Service) artifactHashByID(ctx context.Context, artifactID string) (string, error) {
	id := strings.TrimSpace(artifactID)
	if id == "" {
		return "", nil
	}
	art, err := s.m.GetArtifact(ctx, id)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(art.ArtifactHash), nil
}

// activeArtifactHash 访问面上该路径当前指向的产物哈希（未激活 / 重定向 / 读取失败一律空串）。
func (s *Service) activeArtifactHash(path string) string {
	if s == nil || s.publication == nil {
		return ""
	}
	state, err := s.publication.Inspect(path)
	if err != nil || state == nil || state.Locator == nil {
		return ""
	}
	// 只有「页面产物」才算本次切换生效：重定向（改 URL 的旧路径 301）不是本次发布的落点。
	if state.Kind != pipeline.PublicationPage {
		return ""
	}
	return strings.TrimPrefix(state.Locator.Key, artifactLocatorKeyPrefix)
}

// receiptPathStillCurrent 回执记录的路径是否仍是该语言的当前访问路径。
func (s *Service) receiptPathStillCurrent(ctx context.Context, inst *presentationmodel.InstanceEntity,
	item pubcontract.PendingReceiptResp) bool {
	lang := strings.TrimSpace(item.Lang)
	if lang == "" {
		// 无语言信息的回执（非本模块登记）：不做路径守卫，交由访问面判据决定。
		return true
	}
	rule := pipeline.LangURLRuleForProject(ctx, s.project, inst.ProjectID)
	path, perr := pipeline.SitePath(rule, lang, s.instanceLogicalPath(ctx, inst))
	if perr != nil {
		return false
	}
	return path == item.Path
}

// locateInstanceForReceipt 定位回执所属实例：回执带工程时先直查，否则逐工程遍历
// （presentation_instances 带 FORCE 策略，无作用域查询在非超级角色下静默 0 行）。
func (s *Service) locateInstanceForReceipt(ctx context.Context, projectID, instanceID string) (*presentationmodel.InstanceEntity, error) {
	if id := strings.TrimSpace(projectID); id != "" {
		inst, err := s.m.GetInstance(ctx, id, instanceID)
		if err == nil {
			return inst, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	return s.findOneInstanceAnyProject(ctx, instanceID)
}
