package unit

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mediamodel "go_wp/internal/module/media/model"
	mediaservice "go_wp/internal/module/media/service"

	"go_wp/public/test/support"
)

func TestReconcileSeparatesSupersededVariantsFromOrphans(t *testing.T) {
	storageDir := initVariantUploadForTest(t)
	db := support.NewMigratedPGTestDB(t)
	svc := mediaservice.NewService(
		mediamodel.NewAttachmentModel(db),
		mediamodel.NewFileCategoryModel(db),
		mediamodel.NewMediaVariantModel(db),
	)

	if err := os.WriteFile(filepath.Join(storageDir, "77.png"), []byte("x"), 0o644); err != nil {
		t.Fatalf("落盘原图失败: %v", err)
	}
	att := &mediamodel.AttachmentEntity{
		FileName: "77.png", FilePath: "77.png", FileSize: 1, FileType: "image",
		StorageType: "local", Status: mediamodel.AttachmentStatusEnabled,
	}
	if err := db.Create(att).Error; err != nil {
		t.Fatalf("种子附件插入失败: %v", err)
	}

	superseded := fmt.Sprintf("%d_thumb-1-ab12cd34.jpg", att.ID)
	if err := os.WriteFile(filepath.Join(storageDir, superseded), []byte("x"), 0o644); err != nil {
		t.Fatalf("落盘旧变体失败: %v", err)
	}

	orphan := "999999999_thumb-1-deadbeef.jpg"
	if err := os.WriteFile(filepath.Join(storageDir, orphan), []byte("x"), 0o644); err != nil {
		t.Fatalf("落盘孤儿文件失败: %v", err)
	}

	report, err := svc.ReconcileStorage(context.Background(), nil)
	if err != nil {
		t.Fatalf("对账失败: %v", err)
	}

	var supersededHit, orphanHit bool
	for _, item := range report.SupersededVariants {
		if item.Path == superseded {
			supersededHit = true
			if !strings.Contains(item.Detail, "删除会让线上图片 404") {
				t.Errorf("被取代产物的 Detail 必须让运维看出「删了会 404」，实际：%s", item.Detail)
			}
		}
	}
	for _, item := range report.OrphanFiles {
		if item.Path == superseded {
			t.Errorf("被取代的历史产物 %s 不该落进 orphan_file（那是可清理的一类）", superseded)
		}
		if item.Path == orphan {
			orphanHit = true
		}
	}
	if !supersededHit {
		t.Errorf("旧变体 %s 未被列为 superseded_variant：%+v", superseded, report.SupersededVariants)
	}
	if !orphanHit {
		t.Errorf("附件不存在的文件 %s 未被列为 orphan_file：%+v", orphan, report.OrphanFiles)
	}
}
