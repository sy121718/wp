package navigationservice

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/internal/module/navigation/dto"
	"go_wp/internal/module/navigation/enums"
	"go_wp/internal/module/navigation/model"
)

const (
	dirUp   = "up"
	dirDown = "down"
)

const swapReasonBoundary = "boundary"

func (s *Service) SwapMove(ctx context.Context, req *navigationdto.SwapMoveReq) (res *navigationdto.SwapMoveResp, err error) {
	if req == nil {
		return nil, navigationenums.Err(navigationenums.ErrInvalidParam)
	}
	id := strings.TrimSpace(req.ID)
	dir := strings.TrimSpace(req.Dir)
	if id == "" || (dir != dirUp && dir != dirDown) {
		return nil, navigationenums.Err(navigationenums.ErrInvalidParam)
	}

	item, err := s.locateNavigation(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, navigationenums.Err(navigationenums.ErrNotFound)
		}
		return nil, err
	}
	if want := strings.TrimSpace(req.ProjectID); want != "" && want != item.ProjectID {

		return nil, navigationenums.Err(navigationenums.ErrInvalidParam)
	}
	var (
		moved   bool
		confID  string
		snapIDs []*navigationdto.NavigationNode
	)
	at := time.Now().UTC()

	err = s.withMenuWriteTx(ctx, item.ProjectID, func(tx *gorm.DB) error {

		rows, lerr := s.m.ListTx(ctx, tx, item.ProjectID, item.Kind)
		if lerr != nil {
			return lerr
		}
		cur := findNavRow(rows, item.ID)
		if cur == nil {
			return navigationenums.Err(navigationenums.ErrNotFound)
		}
		nodes := buildTree(rows)
		snapIDs = nodes
		siblings := navSiblingsOf(nodes, cur.ParentID)
		idx := -1
		for i, node := range siblings {
			if node.ID == cur.ID {
				idx = i
				break
			}
		}
		if idx < 0 {

			return navigationenums.Err(navigationenums.ErrNotFound)
		}
		next := idx - 1
		if dir == dirDown {
			next = idx + 1
		}
		if next < 0 || next >= len(siblings) {

			moved = false
			return nil
		}
		a, b := siblings[idx], siblings[next]
		if a.SortOrder == b.SortOrder {

			return navigationenums.Err(navigationenums.ErrSortTied)
		}
		expA, terr := parseSnapshotToken(a.UpdatedAt)
		if terr != nil {
			return terr
		}
		expB, terr := parseSnapshotToken(b.UpdatedAt)
		if terr != nil {
			return terr
		}
		if serr := s.m.SwapSortOrdersTx(ctx, tx, item.ProjectID,
			navigationmodel.SwapRow{ID: a.ID, SortOrder: b.SortOrder, Expected: expA},
			navigationmodel.SwapRow{ID: b.ID, SortOrder: a.SortOrder, Expected: expB}); serr != nil {
			var conflict *navigationmodel.SwapConflictError
			if errors.As(serr, &conflict) {
				confID = conflict.ID
			}
			return serr
		}
		if merr := s.markMenuStaleInTx(ctx, tx, item.ProjectID, item.Kind, at); merr != nil {
			return merr
		}
		if cur.ParentID != nil && strings.TrimSpace(*cur.ParentID) != "" {

			if merr := s.markNavigationStaleInTx(ctx, tx, item.ProjectID, strings.TrimSpace(*cur.ParentID)); merr != nil {
				return merr
			}
		}
		if merr := s.markNavigationStaleInTx(ctx, tx, item.ProjectID, a.ID); merr != nil {
			return merr
		}
		if merr := s.markNavigationStaleInTx(ctx, tx, item.ProjectID, b.ID); merr != nil {
			return merr
		}
		moved = true
		return nil
	})
	if err != nil {
		if confID != "" {
			return nil, staleVersionMessage(s.staleTitle(ctx,
				&navigationmodel.NavigationEntity{ProjectID: item.ProjectID, ID: confID,
					Title: snapshotTitle(snapIDs, confID)}))
		}
		return nil, err
	}
	if !moved {
		return &navigationdto.SwapMoveResp{Moved: false, Reason: swapReasonBoundary}, nil
	}
	return &navigationdto.SwapMoveResp{Moved: true}, nil
}

func parseSnapshotToken(raw string) (time.Time, error) {
	t, err := time.ParseInLocation(time.RFC3339Nano, strings.TrimSpace(raw), time.Local)
	if err != nil {

		return time.Time{}, fmt.Errorf("导航树快照 token 解析失败: %w", err)
	}
	return t, nil
}

func navSiblingsOf(nodes []*navigationdto.NavigationNode, parentID *string) []*navigationdto.NavigationNode {
	if parentID == nil || *parentID == "" {
		return nodes
	}
	for _, n := range nodes {
		if n.ID == *parentID {
			return n.Children
		}
		if found := navSiblingsOf(n.Children, parentID); found != nil {
			return found
		}
	}
	return nil
}

func findNavRow(rows []*navigationmodel.NavigationEntity, id string) *navigationmodel.NavigationEntity {
	for _, r := range rows {
		if r != nil && r.ID == id {
			return r
		}
	}
	return nil
}

func snapshotTitle(nodes []*navigationdto.NavigationNode, id string) string {
	for _, n := range nodes {
		if n.ID == id {
			return n.Title
		}
		if t := snapshotTitle(n.Children, id); t != "" {
			return t
		}
	}
	return ""
}
