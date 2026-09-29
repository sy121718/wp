package commentservice

// comment_list.go — 列表用例（公开列表 + 后台审核队列）。
//
// 两条列表的**状态口径完全不同**，这是本文件最要紧的一件事：
//
//	· ListApproved（公开面）—— 只出 approved，且状态条件写在 SQL 里、不可由调用方配置；
//	· AdminList（控制面）—— 可筛任意状态（默认全部），因为审核员要看到待审与已驳回。
//
// 把这两件事合成一个方法、用一个 `status` 参数区分，等于把「公开列表只看已通过」
// 变成调用方记得传对参数的事 —— 漏传的后果是**未审核内容直接出现在线上页面**。

import (
	"context"
	"strings"
	"time"

	commentdto "go_wp/internal/module/comment/dto"
	commentenums "go_wp/internal/module/comment/enums"
	commentmodel "go_wp/internal/module/comment/model"
	"go_wp/pkg/utils"
)

// ListApproved 公开列表：某实体下**已通过**的评论（顶层分页 + 一级回复）。
func (s *Service) ListApproved(ctx context.Context, req *commentdto.ListReq) (res *commentdto.ListResp, err error) {
	if req == nil {
		return nil, errParam(commentenums.ErrInvalidParam)
	}
	if err = s.validateTarget(req.ProjectID, req.EntityType, req.EntityID); err != nil {
		return nil, err
	}
	page, pageSize := normalizePaging(req.Page, req.PageSize)
	offset := (page - 1) * pageSize

	tops, err := s.model.ListTopLevel(ctx, req.ProjectID, req.EntityType, req.EntityID, pageSize, offset)
	if err != nil {
		return nil, err
	}
	total, err := s.model.CountTopLevel(ctx, req.ProjectID, req.EntityType, req.EntityID)
	if err != nil {
		return nil, err
	}
	topIDs := make([]int64, 0, len(tops))
	for _, top := range tops {
		topIDs = append(topIDs, top.ID)
	}
	replies, err := s.model.ListRepliesOf(ctx, req.ProjectID, req.EntityType, req.EntityID, topIDs)
	if err != nil {
		return nil, err
	}

	res = &commentdto.ListResp{
		Items:    assembleItems(tops, replies),
		Total:    total,
		Page:     page,
		PageSize: pageSize,
		HasMore:  int64(offset+len(tops)) < total,
	}
	return res, nil
}

// assembleItems 把「顶层 + 回复」两段查询结果拼成两级结构。
//
// 为什么要两次查询而不是一次查全部再在内存里分组：一次查全部的前提是
// 「这个实体的评论不多」—— 帖子一旦有几千条回复，单次查询会把整段拉进内存，
// 而分页语义也失去意义（回复会跟着顶层一起被切在页边界外）。
func assembleItems(tops, replies []*commentmodel.Entity) []commentdto.Item {
	out := make([]commentdto.Item, 0, len(tops))
	index := make(map[int64]int, len(tops))
	for _, top := range tops {
		index[top.ID] = len(out)
		out = append(out, itemOf(top, false))
	}
	for _, reply := range replies {
		if reply.ParentID == nil {
			continue
		}
		pos, ok := index[*reply.ParentID]
		if !ok {
			// 回复的父评论不在本页（父评论被删 / 分页边界外）：**不丢数据**，
			// 而是把它提到顶层显示 —— 少显示一条用户能看见的评论，比多显示一条更糟。
			out = append(out, itemOf(reply, true))
			continue
		}
		out[pos].Replies = append(out[pos].Replies, itemOf(reply, true))
	}
	return out
}

// itemOf 把一行转成 dto（不含作者名，理由见 dto.Item 的注释）。
func itemOf(e *commentmodel.Entity, isReply bool) commentdto.Item {
	if e == nil {
		return commentdto.Item{}
	}
	return commentdto.Item{
		ID:        e.ID,
		Body:      e.Body,
		IsReply:   isReply,
		CreatedAt: jsonTime(e.CreateTime),
	}
}

// AdminList 后台审核队列（控制面，可按状态 / 实体类型 / 关键词筛选）。
func (s *Service) AdminList(ctx context.Context, req *commentdto.AdminListReq) (res *commentdto.AdminListResp, err error) {
	if req == nil {
		return nil, errParam(commentenums.ErrInvalidParam)
	}
	// 工程必填：评论按工程隔离（RLS 作用域也需要它），后台页因此必须先选工程。
	// 这里只校验工程 —— 队列本身不带实体 id（它是跨实体的审核视图）。
	if err = s.validateProject(req.ProjectID); err != nil {
		return nil, err
	}
	q := commentmodel.ReviewQuery{ProjectID: strings.TrimSpace(req.ProjectID)}
	if status := strings.TrimSpace(req.Status); status != "" {
		if !commentenums.IsValidStatus(status) {
			return nil, errParam(commentenums.ErrInvalidParam)
		}
		q.Statuses = []string{status}
	}
	q.EntityType = strings.TrimSpace(req.EntityType)
	if q.EntityType != "" && !s.IsRegisteredEntityType(q.EntityType) {
		return nil, errParam(commentenums.ErrEntityTypeUnknown)
	}
	kw := strings.TrimSpace(req.Keyword)
	if len([]rune(kw)) > commentenums.MaxKeywordLen {
		return nil, errParam(commentenums.ErrInvalidParam)
	}
	q.Keyword = kw

	page, pageSize := normalizePaging(req.Page, req.PageSize)
	q.Limit, q.Offset = pageSize, (page-1)*pageSize

	rows, total, err := s.model.ListForReview(ctx, q)
	if err != nil {
		return nil, err
	}
	items := make([]commentdto.AdminItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, adminItemOf(row))
	}
	return &commentdto.AdminListResp{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

// adminItemOf 后台行转换（状态展示名由 handler 按语言填，service 不取词）。
func adminItemOf(e *commentmodel.Entity) commentdto.AdminItem {
	if e == nil {
		return commentdto.AdminItem{}
	}
	item := commentdto.AdminItem{
		ID:         e.ID,
		Body:       e.Body,
		EntityType: e.EntityType,
		EntityID:   e.EntityID,
		UserID:     e.UserID,
		Status:     e.Status,
		CreateTime: jsonTime(e.CreateTime),
		IsReply:    e.ParentID != nil,
	}
	if e.ReviewedAt != nil {
		item.ReviewedAt = jsonTimePtr(e.ReviewedAt)
	}
	item.ReviewerID = e.ReviewedBy
	return item
}

// jsonTime 转对外 JSON 时间（只到秒；形态统一由 pkg/utils 决定）。
func jsonTime(t time.Time) utils.JSONTime { return utils.NewJSONTime(t) }

// jsonTimePtr 可空时间转换（nil 进 nil 出）。
func jsonTimePtr(t *time.Time) *utils.JSONTime { return utils.NewJSONTimePtr(t) }
