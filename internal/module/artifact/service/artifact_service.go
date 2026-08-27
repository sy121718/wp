package artifactservice

import (
	artifactcontract "go_wp/internal/module/artifact/contract"
	artifactmodel "go_wp/internal/module/artifact/model"
)

var _ artifactcontract.ArtifactService = (*Service)(nil)

// Service 构建产物归档业务服务。
type Service struct {
	model *artifactmodel.Model
}

// NewService 创建 Artifact 服务。
func NewService(model *artifactmodel.Model) *Service {
	return &Service{model: model}
}

// Model 暴露底层 model 供测试检查闭包表。
func (s *Service) Model() *artifactmodel.Model {
	return s.model
}
