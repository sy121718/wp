package pageservice

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	pagedto "go_wp/internal/module/page/dto"
	pageenums "go_wp/internal/module/page/enums"
	"go_wp/pkg/facing"
	"go_wp/pkg/logger"
	"go_wp/pkg/publish"
)

// redirectBulkBlockedError 是重定向整批删除被拒绝时的事实：主消息一句，
// 逐条路径与原因交给共享失败出口。
type redirectBulkBlockedError struct {
	key   string
	items []facing.BlockedItem
}

func (e *redirectBulkBlockedError) Error() string { return e.key }

func (e *redirectBulkBlockedError) Facing() (string, string) { return e.key, "" }

func (e *redirectBulkBlockedError) HTTPStatus() int { return 400 }

func (e *redirectBulkBlockedError) BlockedItems() []facing.BlockedItem { return e.items }

// BulkDeleteRedirect 在同一事务里删除整批重定向：逐条规范化路径、
// 锁内复核归属，全部通过后才整批删除；任一目标非法 / 不存在 / 归属变化都整批拒绝。
func (s *Service) BulkDeleteRedirect(ctx context.Context, req *pagedto.RedirectBulkDeleteReq) (err error) {
	if req == nil {
		return ErrInvalidParam
	}
	projectID := strings.TrimSpace(req.ProjectID)
	if projectID == "" {
		return ErrInvalidParam
	}
	if s.publication == nil || s.routes == nil {
		return ErrRedirectUnavailable
	}

	paths, blocked, err := normalizeRedirectBatch(req.Paths)
	if err != nil {
		return err
	}
	if len(blocked) > 0 {
		return redirectBatchReject(blocked)
	}

	intents := make([]*deactivationIntent, 0, len(paths))
	txErr := s.model.TransactionScoped(ctx, projectID, func(tx *gorm.DB) error {
		if aerr := s.model.LockProjectEntityAdvisoryTx(ctx, tx, projectID); aerr != nil {
			return aerr
		}
		// 先复核全部目标，再动任何一行：任一条不合格，整批一条都不删。
		for _, path := range paths {
			owner, oerr := s.routes.RouteOwnerTx(ctx, tx, projectID, path)
			if oerr != nil {
				if errors.Is(oerr, gorm.ErrRecordNotFound) {
					blocked = append(blocked, facing.BlockedItem{
						ID: path, ReasonKey: pageenums.ErrRedirectNotFound,
					})
					continue
				}
				return oerr
			}
			if owner == nil || owner.RouteKind != publisherRouteKindRedirect {
				blocked = append(blocked, facing.BlockedItem{
					ID: path, ReasonKey: pageenums.ErrRedirectConflict,
				})
			}
		}
		if len(blocked) > 0 {
			return errBulkDeleteRollback
		}

		for _, path := range paths {
			intent, ierr := s.registerProjectDeactivationIntent(ctx, projectID, path)
			if ierr != nil {
				return ierr
			}
			intents = append(intents, intent)
		}
		return s.withPaths(paths, func() error {
			for _, path := range paths {
				var intent *deactivationIntent
				for _, candidate := range intents {
					if len(candidate.Paths) == 1 && candidate.Paths[0] == path {
						intent = candidate
						break
					}
				}
				if intent == nil {
					return fmt.Errorf("%w: 路径 %s 缺少下线意图", ErrRedirectConflict, path)
				}
				if oerr := s.verifyProjectRedirectOwner(ctx, tx, projectID, path, intent.PathArtifacts[path]); oerr != nil {
					return oerr
				}
				n, derr := s.model.DeleteRedirectRouteTx(ctx, tx, projectID, path)
				if derr != nil {
					return derr
				}
				if n != 1 {
					return fmt.Errorf("%w: 路径 %s 的重定向行已不归本事务处置", ErrRedirectConflict, path)
				}
				if merr := s.markDeactivationCommitted(ctx, tx, intent); merr != nil {
					return merr
				}
			}
			return nil
		})
	})
	if txErr != nil {
		if errors.Is(txErr, errBulkDeleteRollback) {
			return redirectBatchReject(blocked)
		}
		for _, intent := range intents {
			s.abortPublishReceipt(ctx, intent.ReceiptID, "重定向批量删除的业务事务未提交，访问面保持原样")
		}
		logger.Scene("page").Error(txErr, "批量删除重定向失败（业务事务未提交）")
		return redirectDeleteError(txErr)
	}

	// 事务已提交：逐条撤链，失败的回执保持 pending 交收敛例程重判。
	for _, intent := range intents {
		if werr := s.withdrawProjectDeactivatedLinks(ctx, projectID, intent.ReceiptID); werr != nil {
			s.keepPublishReceiptPending(intent.ReceiptID, "重定向已删除，撤链失败")
			logger.Scene("page").With("path", firstNonEmptyPath(intent.Paths)).
				Error(werr, "重定向已删除但撤链失败（回执保持 pending 交收敛重放）")
			continue
		}
		if cerr := s.completePublishReceipt(ctx, intent.ReceiptID); cerr != nil {
			s.NotifyPendingReceipt()
		}
	}
	return nil
}

// normalizeRedirectBatch 规范化整批路径：空项、非法路径整批拒绝，合法项去重保序。
func normalizeRedirectBatch(raw []string) ([]string, []facing.BlockedItem, error) {
	seen := make(map[string]bool, len(raw))
	paths := make([]string, 0, len(raw))
	blocked := make([]facing.BlockedItem, 0)
	for _, item := range raw {
		text := strings.TrimSpace(item)
		if text == "" {
			blocked = append(blocked, facing.BlockedItem{ReasonKey: pageenums.ErrInvalidParam})
			continue
		}
		path, perr := publish.NormalizeURL(text)
		if perr != nil {
			blocked = append(blocked, facing.BlockedItem{ID: text, ReasonKey: pageenums.ErrInvalidPath})
			continue
		}
		if seen[path] {
			continue
		}
		seen[path] = true
		paths = append(paths, path)
	}
	if len(blocked) > 0 {
		return nil, blocked, nil
	}
	if len(paths) == 0 {
		return nil, nil, ErrInvalidParam
	}
	return paths, nil, nil
}

// redirectBatchReject 构造整批拒绝事实：单条请求取该条原因，多项给公共整批文案。
func redirectBatchReject(items []facing.BlockedItem) error {
	if len(items) == 0 {
		return nil
	}
	key := pageenums.ErrBatchReject
	if len(items) == 1 {
		key = items[0].ReasonKey
	}
	return &redirectBulkBlockedError{key: key, items: items}
}
