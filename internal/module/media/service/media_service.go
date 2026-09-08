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
	vm *mediamodel.MediaVariantModel
}

// NewService 创建媒体服务（am/cm/vm 分别为附件、分类、变体表访问单元）。
func NewService(am *mediamodel.AttachmentModel, cm *mediamodel.FileCategoryModel, vm *mediamodel.MediaVariantModel) *Service {
	return &Service{am: am, cm: cm, vm: vm}
}
