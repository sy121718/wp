package masterdataservice

import (
	"context"
	"strings"
	"time"

	masterdatadto "go_wp/internal/module/masterdata/dto"
	masterdataenums "go_wp/internal/module/masterdata/enums"
	masterdatamodel "go_wp/internal/module/masterdata/model"
)

func (s *Service) ListChanges(ctx context.Context, req *masterdatadto.ListChangeReq) (list []*masterdatadto.ChangeResp, err error) {
	args, err := s.queryArgsOf(ctx, req)
	if err != nil {
		return nil, err
	}
	rows, err := s.m.List(ctx, args.filter, args.size, (args.page-1)*args.size)
	if err != nil {
		return nil, err
	}
	list = make([]*masterdatadto.ChangeResp, 0, len(rows))
	for _, row := range rows {
		list = append(list, toChangeResp(row))
	}
	return list, nil
}

func (s *Service) CountChanges(ctx context.Context, req *masterdatadto.ListChangeReq) (n int64, err error) {
	args, err := s.queryArgsOf(ctx, req)
	if err != nil {
		return 0, err
	}
	return s.m.Count(ctx, args.filter)
}

func (s *Service) ListEntities(ctx context.Context, req *masterdatadto.ListEntityReq) (list []*masterdatadto.EntityHistoryResp, err error) {
	if req == nil {
		req = &masterdatadto.ListEntityReq{}
	}
	args, err := s.resolveQuery(ctx, req.ProjectID, req.EntityType, req.EntityID, req.Field, req.Action,
		req.Keyword, req.OperatorID, req.Since, req.Until, req.Page, req.Size)
	if err != nil {
		return nil, err
	}
	rows, err := s.m.ListEntityHistories(ctx, args.filter, args.size, (args.page-1)*args.size)
	if err != nil {
		return nil, err
	}
	list = make([]*masterdatadto.EntityHistoryResp, 0, len(rows))
	for _, row := range rows {
		list = append(list, toEntityHistoryResp(row))
	}
	return list, nil
}

func (s *Service) CountEntities(ctx context.Context, req *masterdatadto.ListEntityReq) (n int64, err error) {
	if req == nil {
		req = &masterdatadto.ListEntityReq{}
	}
	args, err := s.resolveQuery(ctx, req.ProjectID, req.EntityType, req.EntityID, req.Field, req.Action,
		req.Keyword, req.OperatorID, req.Since, req.Until, req.Page, req.Size)
	if err != nil {
		return 0, err
	}
	return s.m.CountEntities(ctx, args.filter)
}

func (s *Service) EntityTimeline(ctx context.Context, req *masterdatadto.EntityTimelineReq) (res *masterdatadto.EntityTimelineResp, err error) {
	if req == nil {
		return nil, masterdataenums.Err(masterdataenums.ErrInvalidParam)
	}
	entityType := strings.TrimSpace(req.EntityType)
	if !masterdataenums.IsValidEntityType(entityType) {
		return nil, masterdataenums.Err(masterdataenums.ErrEntityTypeInvalid)
	}
	entityID := strings.TrimSpace(req.EntityID)
	if entityID == "" {
		return nil, masterdataenums.Err(masterdataenums.ErrEntityIDRequired)
	}
	args, err := s.resolveQuery(ctx, req.ProjectID, entityType, entityID, "", "", "", "", "", "",
		req.Page, req.Size)
	if err != nil {
		return nil, err
	}
	total, err := s.m.Count(ctx, args.filter)
	if err != nil {
		return nil, err
	}
	rows, err := s.m.List(ctx, args.filter, args.size, (args.page-1)*args.size)
	if err != nil {
		return nil, err
	}
	res = &masterdatadto.EntityTimelineResp{
		EntityType: entityType, EntityTypeLabel: masterdataenums.EntityTypeLabel(entityType).Fallback,
		EntityID: entityID, Total: total, Changes: make([]*masterdatadto.ChangeResp, 0, len(rows)),
	}
	for _, row := range rows {
		res.Changes = append(res.Changes, toChangeResp(row))
	}

	if len(rows) > 0 {
		res.EntityLabel = rows[0].EntityLabel
	}
	return res, nil
}

func (s *Service) queryArgsOf(ctx context.Context, req *masterdatadto.ListChangeReq) (args queryArgs, err error) {
	if req == nil {
		req = &masterdatadto.ListChangeReq{}
	}
	return s.resolveQuery(ctx, req.ProjectID, req.EntityType, req.EntityID, req.Field, req.Action,
		req.Keyword, req.OperatorID, req.Since, req.Until, req.Page, req.Size)
}

func toChangeResp(e *masterdatamodel.ChangeEntity) *masterdatadto.ChangeResp {
	if e == nil {
		return nil
	}
	return &masterdatadto.ChangeResp{
		ID: e.ID, ProjectID: e.ProjectID,
		EntityType: e.EntityType, EntityTypeLabel: masterdataenums.EntityTypeLabel(e.EntityType).Fallback,
		EntityID: e.EntityID, EntityLabel: e.EntityLabel,
		Action: e.Action, ActionLabel: masterdataenums.ActionLabel(e.Action).Fallback,
		Field: e.Field, FieldLabel: masterdataenums.FieldLabel(e.EntityType, e.Field).Fallback,
		OldValue: e.OldValue, NewValue: e.NewValue,
		Origin: e.Origin, OperatorID: e.OperatorID,
		CreatedAt: e.CreatedAt.Format(time.RFC3339),
	}
}

func toEntityHistoryResp(row *masterdatamodel.EntityHistoryRow) *masterdatadto.EntityHistoryResp {
	if row == nil {
		return nil
	}
	return &masterdatadto.EntityHistoryResp{
		EntityType: row.EntityType, EntityTypeLabel: masterdataenums.EntityTypeLabel(row.EntityType).Fallback,
		EntityID: row.EntityID, EntityLabel: row.EntityLabel,
		ChangeCount: row.ChangeCount,
		LastAction:  row.LastAction, LastActionLabel: masterdataenums.ActionLabel(row.LastAction).Fallback,
		LastField: row.LastField, LastFieldLabel: masterdataenums.FieldLabel(row.EntityType, row.LastField).Fallback,
		LastOperatorID: row.LastOperatorID,
		LastAt:         row.LastAt.Format(time.RFC3339),
	}
}
