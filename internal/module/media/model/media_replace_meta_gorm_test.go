package mediamodel_test

import (
	"context"
	"testing"
	"time"

	mediamodel "go_wp/internal/module/media/model"
	"go_wp/public/test/support"
)

func TestReplaceContentMetaReturnsIncrementedGeneration(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)

	if err := db.Exec(`INSERT INTO sys_attachment (file_name, file_path, file_size, file_type, storage_type, generation, status)
		VALUES ('a.png', '/tmp/a.png', 11, 'image', 'local', 1, 1)`).Error; err != nil {
		t.Fatalf("插入附件失败: %v", err)
	}
	var id uint64
	if err := db.Raw(`SELECT id FROM sys_attachment WHERE file_name = 'a.png'`).Scan(&id).Error; err != nil {
		t.Fatalf("取回 id 失败: %v", err)
	}

	m := mediamodel.NewAttachmentModel(db)
	gen, err := m.ReplaceContentMeta(context.Background(), id, "d41d8cd98f00b204e9800998ecf8427e", 42, "image/png", time.Now().UTC())
	if err != nil {
		t.Fatalf("ReplaceContentMeta: %v", err)
	}
	if gen != 2 {
		t.Fatalf("返回的 generation = %d，期望 2（RETURNING 没写回就是 0）", gen)
	}

	var row struct {
		Generation int
		FileSize   int64
		MD5        string
	}
	if err := db.Raw(`SELECT generation, file_size, md5 FROM sys_attachment WHERE id = ?`, id).Scan(&row).Error; err != nil {
		t.Fatalf("复查失败: %v", err)
	}
	if row.Generation != 2 || row.FileSize != 42 || row.MD5 != "d41d8cd98f00b204e9800998ecf8427e" {
		t.Fatalf("落库值 = generation %d / size %d / md5 %q，期望 2 / 42 / d41d8cd…", row.Generation, row.FileSize, row.MD5)
	}
}
