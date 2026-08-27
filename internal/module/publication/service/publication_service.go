package pubservice

import (
	pubcontract "go_wp/internal/module/publication/contract"
	pubmodel "go_wp/internal/module/publication/model"
)

var _ pubcontract.PublicationService = (*Service)(nil)

// Service URL 占用与发布控制业务服务。
type Service struct {
	model *pubmodel.Model
}

// NewService 创建 Publication 服务。
func NewService(model *pubmodel.Model) *Service {
	return &Service{model: model}
}

// Model 暴露底层 model 供测试检查路由与回执表。
func (s *Service) Model() *pubmodel.Model {
	return s.model
}
