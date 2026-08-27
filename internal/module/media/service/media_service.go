package mediaservice

import (
	mediacontract "go_wp/internal/module/media/contract"
	mediamodel "go_wp/internal/module/media/model"
)

var _ mediacontract.MediaService = (*Service)(nil)

// Service 媒体模块业务逻辑。
type Service struct {
	am *mediamodel.AttachmentModel
	cm *mediamodel.FileCategoryModel
}

// NewService 创建媒体服务。
func NewService(am *mediamodel.AttachmentModel, cm *mediamodel.FileCategoryModel) *Service {
	return &Service{am: am, cm: cm}
}
