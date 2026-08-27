// Package projectservice 实现 project 模块业务用例。
package projectservice

import (
	projectcontract "go_wp/internal/module/project/contract"
	projectmodel "go_wp/internal/module/project/model"
)

var _ projectcontract.ProjectService = (*Service)(nil)

// Service 站点工程业务服务。
type Service struct {
	model *projectmodel.Model
}

// NewService 创建站点工程服务。
func NewService(model *projectmodel.Model) *Service {
	return &Service{model: model}
}
