package pageservice

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"go_wp/pkg/builder"
	presentationcontract "go_wp/internal/module/presentation/contract"
	"go_wp/internal/pipeline"
)

func (s *Service) SetPublishedInstanceLocator(locator presentationcontract.PublishedInstanceLocator) {
	s.actionInstances = locator
}

func (s *Service) PublishedPagePath(ctx context.Context, projectID, pageID, lang string) (string, error) {
	if uuid.Validate(projectID) != nil || uuid.Validate(pageID) != nil || strings.TrimSpace(lang) == "" {
		return "", ErrInvalidParam
	}
	if _, err := s.model.GetByID(ctx, pageID, projectID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", ErrPageNotFound
		}
		return "", err
	}
	publication, err := s.model.GetPublication(ctx, pageID, lang)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if publication.ArtifactID == nil || *publication.ArtifactID == "" {
		return "", nil
	}
	return pipeline.ActivePublishedPath(s.publication, s.store, publication.ActivePath, publication.ArtifactHash)
}

func (s *Service) publishedActionOptions(ctx context.Context, projectID, lang string) builder.CompileOption {
	return s.buildActionOptions(ctx, projectID, lang, "", "")
}

func (s *Service) buildActionOptions(ctx context.Context, projectID, lang, pageID, frozenPath string) builder.CompileOption {
	return builder.WithPublishedActionPaths(func(id string) (string, error) {
		if pageID != "" && id == pageID {
			return pipeline.NormalizeURL(frozenPath)
		}
		return s.PublishedPagePath(ctx, projectID, id, lang)
	}, func(id string) (string, error) {
		if s.actionInstances == nil {
			return "", fmt.Errorf("已发布实例目标解析未装配")
		}
		return s.actionInstances.PublishedInstancePath(ctx, projectID, id, lang)
	})
}
