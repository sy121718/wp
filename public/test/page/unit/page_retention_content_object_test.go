package unit

import (
	"context"
	"testing"

	"gorm.io/gorm"
)

// TestPurgeRetentionCollectsOrphanContentObjects 固化保留期任务与内容对象 GC 的接线（IDX-016）。
//
// content_objects 只增不减的直接来源：产物行被回收之后，它闭包里的共享内容对象
// 再也没人引用，却留在表里。保留期任务跑一趟必须把它清掉 —— 同时不能碰仍在用的对象。
//
// 用例刻意不制造任何「受保护产物指针」：产物 GC 会因此在保护集合为空时提前返回，
// 而内容对象回收仍必须执行（它不依赖产物的保护集合，见 page_artifact_rebuild.go 的 defer）。
func TestPurgeRetentionCollectsOrphanContentObjects(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()

	collectedPage := createPage(t, svc, projectID, "/gc-old", emptyRevDoc)
	keptPage := createPage(t, svc, projectID, "/gc-live", emptyRevDoc)
	collectedID := insertArtifactRow(t, db, collectedPage.ID, "hash-gc-old")
	keptID := insertArtifactRow(t, db, keptPage.ID, "hash-gc-live")

	// 一条产物已被回收（文件已删），一条仍在用；两条都早于保留窗口。
	if err := db.Exec("UPDATE page_artifacts SET payload_state = 'deleted', create_time = NOW() - INTERVAL '60 days' WHERE id = ?",
		collectedID).Error; err != nil {
		t.Fatalf("标记产物已回收失败: %v", err)
	}
	if err := db.Exec("UPDATE page_artifacts SET create_time = NOW() - INTERVAL '60 days' WHERE id = ?",
		keptID).Error; err != nil {
		t.Fatalf("回拨产物创建时间失败: %v", err)
	}

	// 两套内容对象与闭包：一套随已回收产物成为孤儿，一套仍被可用产物引用。
	seedContentObjectWithClosure(t, db, collectedID, "hash-gc-old-html")
	seedContentObjectWithClosure(t, db, collectedID, "hash-gc-old-json")
	seedContentObjectWithClosure(t, db, keptID, "hash-gc-live-html")
	seedContentObjectWithClosure(t, db, keptID, "hash-gc-live-json")
	// 对象同样要早于保留窗口 —— 窗口内的对象受保留期保护，那是另一条规则（见 artifact 单测）。
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

// seedContentObjectWithClosure 直插一条共享内容对象与它的闭包引用行。
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

// countContentObjects 统计指定内容对象的行数。
func countContentObjects(t *testing.T, db *gorm.DB, hash string) int64 {
	t.Helper()
	var n int64
	if err := db.Raw("SELECT COUNT(*) FROM content_objects WHERE content_hash = ?", hash).Scan(&n).Error; err != nil {
		t.Fatalf("统计内容对象失败: %v", err)
	}
	return n
}
