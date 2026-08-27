package pageservice

import (
	pagecontract "go_wp/internal/module/page/contract"
	pagemodel "go_wp/internal/module/page/model"
	projectcontract "go_wp/internal/module/project/contract"
)

var _ pagecontract.PageService = (*Service)(nil)

// Service Page 草稿与修订业务服务。
type Service struct {
	model   *pagemodel.Model
	project projectcontract.ProjectService
}

// NewService 创建 Page 服务。
func NewService(model *pagemodel.Model, project projectcontract.ProjectService) *Service {
	return &Service{model: model, project: project}
}
