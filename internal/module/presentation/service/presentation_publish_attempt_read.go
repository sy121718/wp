package presentationservice

import (
	"context"
	"strings"

	presentationcontract "go_wp/internal/module/presentation/contract"
	presentationenums "go_wp/internal/module/presentation/enums"
)

const (
	publishAttemptDefaultLimit = 10
	publishAttemptMaxLimit     = 100
)

var _ presentationcontract.PublishAttemptPort = (*Service)(nil)

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
