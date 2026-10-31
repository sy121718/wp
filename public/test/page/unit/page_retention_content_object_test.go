package unit

import (
	"context"
	"testing"

	"gorm.io/gorm"
)

func TestPurgeRetentionCollectsOrphanContentObjects(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()

	collectedPage := createPage(t, svc, projectID, "/gc-old", emptyRevDoc)
	keptPage := createPage(t, svc, projectID, "/gc-live", emptyRevDoc)
	collectedID := insertArtifactRow(t, db, collectedPage.ID, "hash-gc-old")
	keptID := insertArtifactRow(t, db, keptPage.ID, "hash-gc-live")

	if err := db.Exec("UPDATE page_artifacts SET payload_state = 'deleted', create_time = NOW() - INTERVAL '60 days' WHERE id = ?",
		collectedID).Error; err != nil {
		t.Fatalf("标记产物已回收失败: %v", err)
	}
	if err := db.Exec("UPDATE page_artifacts SET create_time = NOW() - INTERVAL '60 days' WHERE id = ?",
		keptID).Error; err != nil {
		t.Fatalf("回拨产物创建时间失败: %v", err)
	}

	seedContentObjectWithClosure(t, db, collectedID, "hash-gc-old-html")
	seedContentObjectWithClosure(t, db, collectedID, "hash-gc-old-json")
	seedContentObjectWithClosure(t, db, keptID, "hash-gc-live-html")
	seedContentObjectWithClosure(t, db, keptID, "hash-gc-live-json")

	if err := db.Exec("UPDATE content_objects SET create_time = NOW() - INTERVAL '60 days'").Error; err != nil {
		t.Fatalf("回拨内容对象创建时间失败: %v", err)
	}

	if _, err := svc.PurgeRetention(ctx); err != nil {
		t.Fatalf("保留期清理失败: %v", err)
	}

	for _, hash := range []string{"hash-gc-old-html", "hash-gc-old-json"} {
		if n := countContentObjects(t, db, hash); n != 0 {
			t.Fatalf("已回收产物引用的内容对象应被清理: %s 仍有 %d 行", hash, n)
		}
	}
	for _, hash := range []string{"hash-gc-live-html", "hash-gc-live-json"} {
		if n := countContentObjects(t, db, hash); n != 1 {
			t.Fatalf("仍在用产物引用的内容对象不得删除: %s 剩 %d 行", hash, n)
		}
	}
}

func seedContentObjectWithClosure(t *testing.T, db *gorm.DB, artifactID, hash string) {
	t.Helper()
	if err := db.Exec(
		"INSERT INTO content_objects (content_hash, provider, object_key, byte_size, create_time) VALUES (?, 'local', ?, 1024, now())",
		hash, "artifacts/"+hash+"/index.html").Error; err != nil {
		t.Fatalf("插入内容对象失败: %v", err)
	}
	if err := db.Exec(
		"INSERT INTO page_artifact_objects (artifact_id, content_hash) VALUES (?, ?)", artifactID, hash).Error; err != nil {
		t.Fatalf("插入对象闭包失败: %v", err)
	}
}

func countContentObjects(t *testing.T, db *gorm.DB, hash string) int64 {
	t.Helper()
	var n int64
	if err := db.Raw("SELECT COUNT(*) FROM content_objects WHERE content_hash = ?", hash).Scan(&n).Error; err != nil {
		t.Fatalf("统计内容对象失败: %v", err)
	}
	return n
}
