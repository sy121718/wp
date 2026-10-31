package unit

import (
	"testing"

	mediamodel "go_wp/internal/module/media/model"
	mediaservice "go_wp/internal/module/media/service"

	"go_wp/public/test/support"
	"gorm.io/gorm"
)

func newMediaUnitService(t *testing.T) (*gorm.DB, *mediaservice.Service) {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	svc := mediaservice.NewService(
		mediamodel.NewAttachmentModel(db),
		mediamodel.NewFileCategoryModel(db),
		mediamodel.NewMediaVariantModel(db),
	)
	return db, svc
}

func seedCategory(t *testing.T, db *gorm.DB, name string, parentID uint64, status int) mediamodel.FileCategoryEntity {
	t.Helper()
	var id uint64
	code := "seed_" + name
	if err := db.Raw(`INSERT INTO sys_file_category
		(category_name, category_code, parent_id, sort_order, status, create_time, update_time)
		VALUES (?, ?, ?, 0, ?, NOW(), NOW()) RETURNING id`,
		name, code, parentID, status).Scan(&id).Error; err != nil {
		t.Fatalf("种子分类插入失败: %v", err)
	}
	return mediamodel.FileCategoryEntity{
		ID: id, CategoryName: name, CategoryCode: code, ParentID: parentID, Status: status,
	}
}

func seedAttachment(t *testing.T, db *gorm.DB, categoryID *uint64, name, fileType, extraInfo string) uint64 {
	t.Helper()
	e := mediamodel.AttachmentEntity{
		CategoryID:  categoryID,
		FileName:    name,
		FilePath:    "seed/" + name,
		FileSize:    1024,
		FileType:    fileType,
		StorageType: "local",
		Status:      mediamodel.AttachmentStatusEnabled,
	}
	if extraInfo != "" {
		e.ExtraInfo = &extraInfo
	}
	if err := db.Create(&e).Error; err != nil {
		t.Fatalf("种子附件插入失败: %v", err)
	}
	return e.ID
}
