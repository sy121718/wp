// masterdata_query.go — 变更记录的查询（issue #19 验收 4：后台可按实体查询变更历史）。
//
// 三条读取路径，覆盖「审计」的三种问法：
//
//  1. ListChanges   —— 按条件拉字段级时间线（某实体 / 某字段 / 某动作 / 某时间窗）；
//  2. ListEntities  —— 先看「哪些实体被改过、各改了几次、最后一次是谁改的」（入口清单）；
//  3. EntityTimeline—— 单个实体的完整历史（后台从清单点进来的落地页）。
//
// 读取一律只读，且**没有任何改写入口**：本表的 append-only 由数据库触发器兜底。
package masterdataservice

import (
	"context"
	"errors"
	"strings"
	"time"

	masterdatadto "go_wp/internal/module/masterdata/dto"
	masterdataenums "go_wp/internal/module/masterdata/enums"
	masterdatamodel "go_wp/internal/module/masterdata/model"
)

// ListChanges 按条件查字段级变更（时间倒序）。
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

// CountChanges 同条件的总条数（分页用）。
func (s *Service) CountChanges(ctx context.Context, req *masterdatadto.ListChangeReq) (n int64, err error) {
	args, err := s.queryArgsOf(ctx, req)
	if err != nil {
		return 0, err
	}
	return s.m.Count(ctx, args.filter)
}

// ListEntities 按实体聚合的变更历史清单。
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

// CountEntities 同条件的实体数（分页用）。
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

// EntityTimeline 单个实体的完整变更时间线。
//
// 实体没有任何变更记录时返回空时间线（Total=0）而不是报错：变更记录表回答的是
// 「改过什么」，从没改过 = 没有记录，这不是「实体不存在」那种错误。
func (s *Service) EntityTimeline(ctx context.Context, req *masterdatadto.EntityTimelineReq) (res *masterdatadto.EntityTimelineResp, err error) {
	if req == nil {
		return nil, errors.New(masterdataenums.ErrInvalidParam)
	}
	entityType := strings.TrimSpace(req.EntityType)
	if !masterdataenums.IsValidEntityType(entityType) {
		return nil, errors.New(masterdataenums.ErrEntityTypeInvalid)
	}
	entityID := strings.TrimSpace(req.EntityID)
	if entityID == "" {
		return nil, errors.New(masterdataenums.ErrEntityIDRequired)
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
	// 实体展示名取最新一条记录的快照（实体已删除时就是删除那一刻的名称）。
	if len(rows) > 0 {
		res.EntityLabel = rows[0].EntityLabel
	}
	return res, nil
}

// queryArgsOf 把列表请求归一化成查询参数（nil 安全的字段访问）。
func (s *Service) queryArgsOf(ctx context.Context, req *masterdatadto.ListChangeReq) (args queryArgs, err error) {
	if req == nil {
		req = &masterdatadto.ListChangeReq{}
	}
	return s.resolveQuery(ctx, req.ProjectID, req.EntityType, req.EntityID, req.Field, req.Action,
		req.Keyword, req.OperatorID, req.Since, req.Until, req.Page, req.Size)
}

// toChangeResp 记录行 → 响应（展示文案统一在模块 enums 里取）。
//
// dto 里的 *Label 一律填**中文兜底**：service 层拿不到请求语言，而这些字段同时供
// JSON API 出口使用（契约保持「已可读的展示名」）。页面出口不复用它们 ——
// inbound/http 用行上的原始值（EntityType / Action / Field）重新取词，见 changeRow。
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

// toEntityHistoryResp 聚合行 → 响应。
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
