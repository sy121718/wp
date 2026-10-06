package artifactmodel_test

// artifact_orphan_delete_gorm_test.go — DeleteOrphanContentObjects 从 Raw+RETURNING
// 改成 GORM clause.Returning 之后，**返回的 hash 列表是否真的被写回**必须带库验一次。
//
// 为什么：GORM 不会为 `Delete(...)` 自动扫回 RETURNING 的结果 —— 需要
// clause.Returning 与被 Delete 的 slice 同时在场才把列值填进去。少任何一半都不报错，
// 只会让 deleted 恒为空；而调用方拿空列表去报「一个都没删」，比真删错了更难查
//（磁盘上对象没了、日志说没删）。这里同时钉住「真被删除」与「没被引用的才删」。

import (
	"context"
	"testing"
	"time"

	artifactmodel "go_wp/internal/module/artifact/model"

	"go_wp/public/test/support"
)

func TestDeleteOrphanContentObjectsReturnsDeletedHashes(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	m := artifactmodel.NewArtifactModel(db)
	ctx := context.Background()

	old := time.Now().UTC().Add(-48 * time.Hour)
	insert := func(hash string, at time.Time) {
		t.Helper()
		if err := db.Exec(`INSERT INTO content_objects (content_hash, provider, object_key, byte_size, create_time)
			VALUES (?, 's3', ?, 12, ?)`, hash, "k/"+hash, at).Error; err != nil {
			t.Fatalf("插入内容对象 %s 失败: %v", hash, err)
		}
	}
	insert("orphan-old", old)
	insert("recent", time.Now().UTC()) // 保留窗口内，不该被删

	deleted, err := m.DeleteOrphanContentObjects(ctx, []string{"orphan-old", "recent"}, time.Now().UTC().Add(-time.Hour))
	if err != nil {
		t.Fatalf("DeleteOrphanContentObjects: %v", err)
	}
	if len(deleted) != 1 || deleted[0] != "orphan-old" {
		t.Fatalf("返回 deleted = %v，期望 [orphan-old]（RETURNING 没写回就是空）", deleted)
	}

	var left int64
	if err := db.Raw(`SELECT COUNT(*) FROM content_objects`).Scan(&left).Error; err != nil {
		t.Fatalf("复查失败: %v", err)
	}
	if left != 1 {
		t.Fatalf("剩余行数 = %d，期望 1（只有 recent 留下）", left)
	}
}
