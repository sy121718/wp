package pageservice

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	pagecontract "go_wp/internal/module/page/contract"
	pagemodel "go_wp/internal/module/page/model"
	projectcontract "go_wp/internal/module/project/contract"

	"go_wp/internal/pipeline"

	artifactcontract "go_wp/internal/module/artifact/contract"
	pageenums "go_wp/internal/module/page/enums"
	pubcontract "go_wp/internal/module/publication/contract"

	"gorm.io/gorm"
)

var _ pagecontract.PageService = (*Service)(nil)

// Service Page 草稿、修订与发布业务服务。
//
// 发布链路持有 pipeline 内核（内存态可由数据库重建）与相邻模块契约，
// 不直接依赖 GORM 之外的模块实现。
type Service struct {
	model     *pagemodel.Model
	project   projectcontract.ProjectService
	artifacts artifactcontract.ArtifactService
	routes    pubcontract.PublicationService

	publisher *pipeline.Publisher
	store     *pipeline.LocalStore
}

// NewService 创建 Page 服务；同时初始化本地产物根（GO_WP_ARTIFACT_ROOT 可覆盖）。
func NewService(model *pagemodel.Model, artifacts artifactcontract.ArtifactService,
	routes pubcontract.PublicationService, project projectcontract.ProjectService) *Service {
	root := strings.TrimSpace(os.Getenv("GO_WP_ARTIFACT_ROOT"))
	if root == "" {
		root = filepath.Join("public", "runtime", "artifacts")
	}
	store := &pipeline.LocalStore{Root: root}
	publication := &pipeline.LocalPublicationStore{ActiveRoot: filepath.Join(root, "public", "active")}
	return &Service{
		model:     model,
		artifacts: artifacts,
		routes:    routes,
		project:   project,
		publisher: pipeline.NewPublisher(store, publication),
		store:     store,
	}
}

// getExistingPage 查询未删除页面，统一映射未找到错误。
func (s *Service) getExistingPage(ctx context.Context, id string) (page *pagemodel.PageEntity, err error) {
	page, err = s.model.GetByID(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New(pageenums.ErrPageNotFound)
	}
	if err != nil {
		return nil, err
	}
	return page, nil
}
