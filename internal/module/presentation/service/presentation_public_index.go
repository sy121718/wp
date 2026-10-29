package presentationservice

import (
	"context"
	"github.com/google/uuid"
	"go_wp/internal/pipeline"
	"strings"
)

func (s *Service) PublishedEntityIndex(ctx context.Context, projectID, entityType, lang string) (map[string]string, error) {
	if uuid.Validate(projectID) != nil || strings.TrimSpace(entityType) == "" {
		return map[string]string{}, nil
	}
	if strings.TrimSpace(lang) == "" {
		lang = pipeline.DefaultLocale(ctx, s.project, projectID)
	}
	return s.m.ListPublicEntityPaths(ctx, projectID, entityType, lang)
}
