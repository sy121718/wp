package unit

// helper_test.go — media service 层单元测试公共支撑：
// 隔离测试库（表结构来自生产迁移）+ 服务实例 + 种子数据。
// 每次调用 NewMigratedPGTestDB 复制一份跑完生产迁移的库，测试结束自动 DROP，测试幂等。
//
// 不再用 AutoMigrate 建表：它照 model 的 gorm 标签建列，与生产 DDL 静默分叉 ——
// 实测分类名长度上限与 extra_info 的 json 列在 AutoMigrate 下都建错，
// 于是「超长名应报错」「非法 JSON 应报错」这类断言在测试里全绿、到生产才暴露。

import (
	"testing"

	mediamodel "go_wp/internal/module/media/model"
	mediaservice "go_wp/internal/module/media/service"

	"go_wp/public/test/support"
	"gorm.io/gorm"
)

// newMediaUnitService 创建隔离 PG schema + media 两张表 + service 实例。
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

// seedCategory 直接插入一条分类记录（默认启用），返回实体。
// 注意：必须用 Exec 插入——gorm 的 default:1 标签会在 status=0 零值时忽略该列
// 走 DB default，导致「软删除」种子实际变成 status=1，无法构造软删场景。
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

// seedAttachment 直接插入一条附件记录（默认启用），返回 ID。
// extraInfo 非空时写入 ExtraInfo 列（需为合法 JSON，列类型为 json）。
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
