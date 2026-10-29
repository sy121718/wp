package pageservice

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"go_wp/internal/builder"
	presentationcontract "go_wp/internal/module/presentation/contract"
	"go_wp/internal/pipeline"
)

func (s *Service) SetPublishedInstanceLocator(locator presentationcontract.PublishedInstanceLocator) {
	s.actionInstances = locator
}

// PublishedPagePath 按本工程和精确语言的Publication取访问路径，不读draft_path或单值镜像。
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

// buildActionOptions 只允许本次构建自身使用冻结路径，首次发布不依赖尚未存在的自身Publication。
// 其它页面和语言仍必须通过真实激活状态；不提供通用未发布目标回退。
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
