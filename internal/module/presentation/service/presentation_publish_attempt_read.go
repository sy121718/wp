package presentationservice

// presentation_publish_attempt_read.go — 发布历史读侧（迁移 673）。

import (
	"context"
	"strings"

	presentationcontract "go_wp/internal/module/presentation/contract"
	presentationenums "go_wp/internal/module/presentation/enums"
)

// 一页默认与上限。
//
// 默认 10：用户要求「默认只给出前面十次的」，加载更多再往后要。
const (
	publishAttemptDefaultLimit = 10
	publishAttemptMaxLimit     = 100
)

var _ presentationcontract.PublishAttemptPort = (*Service)(nil)

// PublishAttempts 读一个实体的发布尝试（新→旧）。
//
// 按实体而不是按实例：一个商品可能挂两张页面（详情 / 归档），运营问的是「这个商品发布过
// 几次」。entityType 空串归一成商品（本表目前只有商品一条产线），entityID 必填。
//
// 不提供游标：台账只增，从最新取前 N 条在交互式「加载更多」里是幂等的；游标会让前端
// 存一个隐形状态（刷新即丢），而这里 N 递增一次请求就够。
func (s *Service) PublishAttempts(ctx context.Context, projectID, entityType, entityID string,
	limit int) (res presentationcontract.PublishAttemptPage, err error) {
	projectID, err = s.resolveProjectID(ctx, projectID)
	if err != nil {
		return res, err
	}
	entityType = strings.TrimSpace(entityType)
	if entityType == "" {
		entityType = entityTypeProduct
	}
	entityID = strings.TrimSpace(entityID)
	if entityID == "" {
		return res, presentationenums.Err(presentationenums.ErrInvalidParam)
	}
	if limit <= 0 {
		limit = publishAttemptDefaultLimit
	}
	if limit > publishAttemptMaxLimit {
		limit = publishAttemptMaxLimit
	}

	total, err := s.m.CountPublishAttempts(ctx, projectID, entityType, entityID)
	if err != nil {
		return res, err
	}
	// 多取一条判「还有没有更早的」：靠 len(rows) == limit 推断会在「刚好整页」时
	// 多给一个点了没反应的空按钮。
	raw, err := s.m.ListPublishAttempts(ctx, projectID, entityType, entityID, limit+1)
	if err != nil {
		return res, err
	}
	res.Total = total
	res.HasMore = len(raw) > limit
	if res.HasMore {
		raw = raw[:limit]
	}
	res.Rows = make([]presentationcontract.PublishAttemptRow, 0, len(raw))
	for _, e := range raw {
		res.Rows = append(res.Rows, presentationcontract.PublishAttemptRow{
			ID:              e.ID,
			Trigger:         e.Trigger,
			Outcome:         e.Outcome,
			ErrorText:       e.ErrorText,
			Langs:           []string(e.Langs),
			Attempts:        e.Attempts,
			ArtifactVersion: e.ArtifactVersion,
			ArtifactHash:    e.ArtifactHash,
			OperatorID:      e.OperatorID,
			OperatorName:    e.OperatorName,
			DurationMs:      e.DurationMs,
			CreatedAt:       e.CreatedAt,
		})
	}
	return res, nil
}
