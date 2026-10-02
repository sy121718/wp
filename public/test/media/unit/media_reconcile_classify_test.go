package unit

// media_reconcile_classify_test.go — 对账分类的验收：换图 / 重跑后留下的旧变体文件
// 必须落 **superseded_variant**（被取代的历史产物），不能混进 orphan_file（真孤儿）。
//
// 为什么这条值得一个用例：两类都是「有文件没记录」，运维看到的差别只有 Detail 一句话 ——
// 而处置动作完全相反（前者删了就线上 404，后者可以清）。分类掉进同一张清单，
// 就是「一个模块说必须留、另一个说建议删」的那种静默冲突。

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

	// 现存附件：原图在盘上（否则它会落进 MissingFiles，与本用例无关）。
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

	// ① 换图 / 重新生成变体后留下的旧文件名（带指纹，但不被任何记录引用）。
	//    文件名里的 id 必须是**这个附件**的 id（上传路径用的就是主键）。
	superseded := fmt.Sprintf("%d_thumb-1-ab12cd34.jpg", att.ID)
	if err := os.WriteFile(filepath.Join(storageDir, superseded), []byte("x"), 0o644); err != nil {
		t.Fatalf("落盘旧变体失败: %v", err)
	}
	// ② 真孤儿：文件名里的附件 id 在 sys_attachment 里不存在。
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
