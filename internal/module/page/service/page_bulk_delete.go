package pageservice

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	pagedto "go_wp/internal/module/page/dto"
	pageenums "go_wp/internal/module/page/enums"
	pagemodel "go_wp/internal/module/page/model"
	mediacontract "go_wp/internal/module/media/contract"
	pubcontract "go_wp/internal/module/publication/contract"
	"go_wp/internal/pipeline"
	"go_wp/pkg/bulkids"
	"go_wp/pkg/builder"
	"go_wp/pkg/facing"
	"go_wp/pkg/logger"
)

// pageBlockedError 是整批删除被拒绝时的事实：主消息只取一句
// （单条请求取该条的具体原因，多项请求取公共整批文案），
// 逐条 id / 路径 / 原因交给共享失败出口按 BlockedItems 输出。
type pageBlockedError struct {
	key   string
	items []facing.BlockedItem
}

func (e *pageBlockedError) Error() string { return e.key }

func (e *pageBlockedError) Facing() (string, string) { return e.key, "" }

func (e *pageBlockedError) HTTPStatus() int { return pageBlockedStatus(e.items, e.key) }

func (e *pageBlockedError) BlockedItems() []facing.BlockedItem { return e.items }

// pageBlockedStatus 从拒绝原因推导状态：冲突 409 / 缺失 404 / 其余按参数错误。
func pageBlockedStatus(items []facing.BlockedItem, key string) int {
	status := http.StatusBadRequest
	for _, item := range items {
		switch item.ReasonKey {
		case pageenums.ErrActionTargetInUse, pageenums.ErrPathOccupied:
			return http.StatusConflict
		case pageenums.ErrPageNotFound:
			status = http.StatusNotFound
		}
	}
	return status
}

// pageReject 构造整批拒绝事实：只有一个目标且只有它失败时给该条具体原因。
func pageReject(requested int, items []facing.BlockedItem) error {
	if len(items) == 0 {
		return nil
	}
	key := pageenums.ErrBatchReject
	if requested == 1 && len(items) == 1 {
		key = items[0].ReasonKey
	}
	return &pageBlockedError{key: key, items: items}
}

// errBulkDeleteRollback 把「已收集到拒绝原因」带回事务外，让整批写入回滚。
var errBulkDeleteRollback = errors.New("page: 批量删除被拒绝")

// BulkDelete 在同一事务里删除整批页面：先完整校验原始 id 集合，
// 再逐工程取顾问锁与行锁、锁内复核动作引用与访问路径，全部通过后才整批软删。
// 任一目标缺失 / 非法 / 仍被引用都整批拒绝，不返回部分成功。
//
// 访问面撤链依赖外部存储，事务内只登记「下线意图」；提交后按已登记身份逐条撤链，
// 失败的回执保持 pending 交收敛例程重判，不把已提交的删除改写成失败。
func (s *Service) BulkDelete(ctx context.Context, req *pagedto.BulkDeleteReq) (err error) {
	if req == nil {
		return ErrInvalidParam
	}
	ids, err := normalizePageDeleteIDs(req.IDs)
	if err != nil {
		return err
	}
	projectOf, err := s.locatePageOwners(ctx, ids)
	if err != nil {
		return err
	}
	ordered, byProject := groupPageIDsByProject(ids, projectOf)

	var (
		blocked     []facing.BlockedItem
		intents     []*deactivationIntent
		deletedIDs  []string
	)
	txErr := s.model.TransactionAny(ctx, func(tx *gorm.DB) error {
		blocked, intents, deletedIDs = nil, nil, nil
		for _, projectID := range ordered {
			if serr := s.model.ScopeTx(tx, projectID); serr != nil {
				return serr
			}
			if lerr := s.model.LockProjectEntityAdvisoryTx(ctx, tx, projectID); lerr != nil {
				return lerr
			}
		}
		for _, projectID := range ordered {
			items, projectIntents, verr := s.verifyBulkDeleteProjectTx(ctx, tx, projectID, byProject[projectID])
			if verr != nil {
				return verr
			}
			blocked = append(blocked, items...)
			intents = append(intents, projectIntents...)
		}
		if len(blocked) > 0 {
			return errBulkDeleteRollback
		}
		now := time.Now().UTC()
		for _, projectID := range ordered {
			if serr := s.model.ScopeTx(tx, projectID); serr != nil {
				return serr
			}
			for _, id := range byProject[projectID] {
				if derr := s.model.SoftDeleteTx(ctx, tx, projectID, id, now); derr != nil {
					return derr
				}
				deletedIDs = append(deletedIDs, id)
			}
		}
		for _, intent := range intents {
			if merr := s.markDeactivationCommitted(ctx, tx, intent); merr != nil {
				return merr
			}
		}
		return nil
	})
	if txErr != nil {
		if errors.Is(txErr, errBulkDeleteRollback) {
			return pageReject(len(ids), blocked)
		}
		logger.Scene("page").Error(txErr, "批量删除页面失败")
		return mapPersistenceError(txErr)
	}

	s.withdrawDeactivatedLinksAfterCommit(ctx, intents)
	s.syncMediaRefsAfterCommit(ctx, deletedIDs, byProject)
	return nil
}

// normalizePageDeleteIDs 做完整批校验：空项、非法 UUID 都整批拒绝，
// 合法项按规范化 UUID 去重后保序，超限返回共享上限事实。
func normalizePageDeleteIDs(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return nil, ErrInvalidParam
	}
	seen := make(map[string]bool, len(raw))
	ids := make([]string, 0, len(raw))
	invalid := make([]facing.BlockedItem, 0)
	for _, item := range raw {
		text := strings.TrimSpace(item)
		if text == "" {
			invalid = append(invalid, facing.BlockedItem{ReasonKey: pageenums.ErrInvalidParam})
			continue
		}
		parsed, perr := uuid.Parse(text)
		if perr != nil {
			invalid = append(invalid, facing.BlockedItem{ID: text, ReasonKey: pageenums.ErrInvalidParam})
			continue
		}
		canonical := parsed.String()
		if seen[canonical] {
			continue
		}
		seen[canonical] = true
		ids = append(ids, canonical)
	}
	if len(invalid) > 0 {
		return nil, pageReject(len(raw), invalid)
	}
	if len(ids) == 0 {
		return nil, ErrInvalidParam
	}
	if len(ids) > bulkids.MaxIDs {
		return nil, &bulkids.IDsError{Count: len(ids), Max: bulkids.MaxIDs}
	}
	return ids, nil
}

// locatePageOwners 逐工程定位每个 id 的归属；任一缺失整批拒绝。
func (s *Service) locatePageOwners(ctx context.Context, ids []string) (map[string]string, error) {
	projectIDs, err := s.fanoutProjectIDs(ctx)
	if err != nil {
		return nil, err
	}
	sort.Strings(projectIDs)
	projectOf := make(map[string]string, len(ids))
	for _, projectID := range projectIDs {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		pending := make([]string, 0, len(ids))
		for _, id := range ids {
			if _, ok := projectOf[id]; !ok {
				pending = append(pending, id)
			}
		}
		if len(pending) == 0 {
			break
		}
		for _, id := range pending {
			if _, gerr := s.model.GetByID(ctx, id, projectID); gerr == nil {
				projectOf[id] = projectID
			}
		}
	}
	missing := make([]facing.BlockedItem, 0)
	for _, id := range ids {
		if _, ok := projectOf[id]; !ok {
			missing = append(missing, facing.BlockedItem{ID: id, ReasonKey: pageenums.ErrPageNotFound})
		}
	}
	if len(missing) > 0 {
		return nil, pageReject(len(ids), missing)
	}
	return projectOf, nil
}

func groupPageIDsByProject(ids []string, projectOf map[string]string) ([]string, map[string][]string) {
	byProject := make(map[string][]string, len(projectOf))
	ordered := make([]string, 0, len(projectOf))
	for _, id := range ids {
		pid := projectOf[id]
		if _, ok := byProject[pid]; !ok {
			ordered = append(ordered, pid)
		}
		byProject[pid] = append(byProject[pid], id)
	}
	sort.Strings(ordered)
	for _, list := range byProject {
		sort.Strings(list)
	}
	return ordered, byProject
}

// verifyBulkDeleteProjectTx 在已持锁的事务内重验存在性、动作引用与访问路径，
// 同时登记下线意图（供提交后撤链使用）。返回拒绝明细与本次登记的下线意图。
func (s *Service) verifyBulkDeleteProjectTx(ctx context.Context, tx *gorm.DB, projectID string,
	ids []string) ([]facing.BlockedItem, []*deactivationIntent, error) {
	rows, err := s.model.LockPagesByIDsTx(ctx, tx, projectID, ids)
	if err != nil {
		return nil, nil, err
	}
	byID := make(map[string]*pagemodel.PageEntity, len(rows))
	for i := range rows {
		byID[rows[i].ID] = &rows[i]
	}
	items := make([]facing.BlockedItem, 0)
	for _, id := range ids {
		if _, ok := byID[id]; !ok {
			items = append(items, facing.BlockedItem{ID: id, ReasonKey: pageenums.ErrPageNotFound})
		}
	}
	if len(items) > 0 {
		return items, nil, nil
	}

	intents := make([]*deactivationIntent, 0, len(ids))
	for _, id := range ids {
		page := byID[id]
		if rerr := s.ensureActionTargetUnusedTx(ctx, tx, projectID, page); rerr != nil {
			reason := pageenums.ErrActionTargetInUse
			if fact, ok := facing.FactOf(rerr); ok && strings.TrimSpace(fact.Key) != "" {
				reason = fact.Key
			}
			items = append(items, facing.BlockedItem{ID: id, Name: page.DraftPath, ReasonKey: reason})
			continue
		}
		if s.routes == nil {
			continue
		}
		paths, perr := s.routes.ListActivePathsByPageTx(ctx, tx, &pubcontract.ListActivePathsReq{
			ProjectID: projectID, PageID: id,
		})
		if perr != nil {
			return nil, nil, perr
		}
		paths = normalizeOldPaths(paths)
		if len(paths) == 0 {
			continue
		}
		intent, ierr := s.registerDeactivationIntent(ctx, projectID, id, page.DraftPath, paths)
		if ierr != nil {
			return nil, nil, ierr
		}
		intents = append(intents, intent)
	}
	if len(items) > 0 {
		return items, nil, nil
	}
	return nil, intents, nil
}

// ensureActionTargetUnusedTx 在锁内复核动作目标引用；引用方是本次批量里的页面自身时不算。
func (s *Service) ensureActionTargetUnusedTx(ctx context.Context, tx *gorm.DB, projectID string,
	page *pagemodel.PageEntity) error {
	rows, err := s.model.ListDraftDocumentsTx(ctx, tx, projectID)
	if err != nil {
		return err
	}
	for i := range rows {
		if rows[i].ID == page.ID {
			continue
		}
		hit, herr := builder.ReferencesActionTarget(rows[i].DraftDocument, "page", page.ID)
		if herr != nil {
			return herr
		}
		if hit {
			return pageenums.ActionTargetInUse()
		}
	}
	key := pipeline.PageTargetKey(page.ID)
	compiled, cerr := s.model.ListPagesByDependencyTx(ctx, tx, projectID, key.Kind, key.Key)
	if cerr != nil {
		return cerr
	}
	for i := range compiled {
		if compiled[i].ID != page.ID {
			return pageenums.ActionTargetInUse()
		}
	}
	return nil
}

// withdrawDeactivatedLinksAfterCommit 在业务事务提交后逐条撤链。
// 撤链失败只让回执保持 pending，交收敛例程重放，不把已提交的删除改写成失败。
func (s *Service) withdrawDeactivatedLinksAfterCommit(ctx context.Context, intents []*deactivationIntent) {
	for _, intent := range intents {
		if intent == nil {
			continue
		}
		if werr := s.withdrawDeactivatedLinks(ctx, intent.ProjectID, intent.PageID, intent.ReceiptID); werr != nil {
			s.keepPublishReceiptPending(intent.ReceiptID, "批量删除已提交，撤链失败")
			logger.Scene("page").With("pageId", intent.PageID).
				Error(werr, "批量删除已提交但撤链失败（回执保持 pending 交收敛重放）")
			continue
		}
		if cerr := s.completePublishReceipt(ctx, intent.ReceiptID); cerr != nil {
			s.NotifyPendingReceipt()
		}
	}
}

// syncMediaRefsAfterCommit 在业务事务提交后清理媒体引用；失败只记日志。
func (s *Service) syncMediaRefsAfterCommit(ctx context.Context, pageIDs []string, titles map[string][]string) {
	if s.media == nil || len(pageIDs) == 0 {
		return
	}
	for _, id := range pageIDs {
		refTitle := ""
		for _, list := range titles {
			for _, candidate := range list {
				if candidate == id {
					refTitle = id
				}
			}
		}
		if _, rerr := s.media.SyncReferences(ctx, &mediacontract.SyncRefsInput{
			RefKind: "page", RefID: id, RefTitle: refTitle,
		}); rerr != nil {
			logger.Scene("page").With("pageId", id).Error(rerr, "批量删除后清理媒体引用失败")
		}
	}
}
