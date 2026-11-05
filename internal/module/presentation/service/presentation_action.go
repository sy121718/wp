package presentationservice

import (
	"context"
	"errors"
	"fmt"
	publish "go_wp/pkg/publish"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	pagecontract "go_wp/internal/module/page/contract"
	presentationenums "go_wp/internal/module/presentation/enums"
	"go_wp/pkg/builder"
)

func (s *Service) SetPublishedPageLocator(locator pagecontract.PublishedPageLocator) {
	s.actionPages = locator
}

func (s *Service) PublishedInstancePath(ctx context.Context, projectID, instanceID, lang string) (string, error) {
	if uuid.Validate(projectID) != nil || uuid.Validate(instanceID) != nil || strings.TrimSpace(lang) == "" {
		return "", presentationenums.Err(presentationenums.ErrInvalidParam)
	}
	instance, err := s.m.GetInstance(ctx, projectID, instanceID)
	if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && instance.DeletedAt != nil) {
		return "", presentationenums.Err(presentationenums.ErrNotFound)
	}
	if err != nil {
		return "", err
	}
	publication, err := s.m.GetPublication(ctx, instanceID, lang)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if publication.ArtifactID == nil || *publication.ArtifactID == "" {
		return "", nil
	}
	return publish.ActivePublishedPath(s.publication, s.store, publication.ActivePath, publication.ArtifactHash)
}

func (s *Service) publishedActionOptions(ctx context.Context, projectID, lang string) builder.CompileOption {
	return builder.WithPublishedActionPaths(func(id string) (string, error) {
		if s.actionPages == nil {
			return "", fmt.Errorf("已发布页面目标解析未装配")
		}
		return s.actionPages.PublishedPagePath(ctx, projectID, id, lang)
	}, func(id string) (string, error) {
		return s.PublishedInstancePath(ctx, projectID, id, lang)
	})
}
