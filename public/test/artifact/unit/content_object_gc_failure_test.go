package unit

// content_object_gc_failure_test.go — 「删除静默不生效」必须可见。
//
// 背景：内容对象 GC 的失败口径是「记进统计 + 打日志」，不向调用方返回 error。
// 真实成因有三种：闭包外键挡下删除（迁移 204 修的正是它）、权限不足、触发器吞掉删除。
// 它们与「并发归档重新引用」在**只看 DELETE ... RETURNING 差集**时长得一模一样 ——
// 而差集默认被解释成后者，于是异常长期埋在统计里（内容对象表只增不减）。
// 本文件用一个 BEFORE DELETE 触发器把删除静默吞掉，验证它被归到失败而不是被认领，
// 并验证失败率出现在响应里（外部只需按 FailedRate > 0 告警）。

import (
	"context"
	"testing"
)

func TestContentObjectGCSurfacesSilentDeleteFailure(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	rec := mustRecord(t, svc, validReq())
	ageContentObjects(t, svc, gcAgeDays)
	setArtifactState(t, svc, rec.ID, "deleted", gcAgeDays)

	db := svc.Model().DB(ctx)
	var hashes []string
	if err := db.Raw("SELECT content_hash FROM content_objects ORDER BY content_hash").Scan(&hashes).Error; err != nil {
		t.Fatalf("读取内容对象失败: %v", err)
	}
	if len(hashes) < 2 {
		t.Fatalf("本用例需要至少 2 个内容对象，实得 %d", len(hashes))
	}
	// 取字典序第一个作为「删不掉」的那条：不硬编码 hash 名，闭包内容变了也照样成立。
	blocked := hashes[0]

	// 触发器对目标 hash 直接 RETURN NULL：DELETE 语句不报错，行却删不掉 ——
	// 这正是外键挡删在生产上留下的形态（语句失败被记成统计，行留在表里）。
	if err := db.Exec(`CREATE OR REPLACE FUNCTION t_block_content_object_delete() RETURNS trigger AS $$
BEGIN
	IF OLD.content_hash = '` + blocked + `' THEN
		RETURN NULL;
	END IF;
	RETURN OLD;
END $$ LANGUAGE plpgsql`).Error; err != nil {
		t.Fatalf("建触发器函数失败: %v", err)
	}
	if err := db.Exec(`CREATE TRIGGER trg_block_content_object_delete BEFORE DELETE ON content_objects
		FOR EACH ROW EXECUTE FUNCTION t_block_content_object_delete()`).Error; err != nil {
		t.Fatalf("建触发器失败: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Exec("DROP TRIGGER IF EXISTS trg_block_content_object_delete ON content_objects").Error
		_ = db.Exec("DROP FUNCTION IF EXISTS t_block_content_object_delete()").Error
	})

	res, err := svc.GarbageCollectContentObjects(ctx, gcReq(false))
	if err != nil {
		t.Fatalf("回收失败: %v", err)
	}

	if !contentObjectExists(t, svc, blocked) {
		t.Fatalf("前提不成立：被触发器拦下的行不该消失（触发器没生效？）")
	}
	if res.Deleted != int64(len(hashes)-1) {
		t.Errorf("其余内容对象应被删掉: deleted=%d，期望 %d", res.Deleted, len(hashes)-1)
	}
	if res.Failed != 1 {
		t.Fatalf("被静默吞掉的那条必须计入失败，否则异常永远看不见: failed=%d", res.Failed)
	}
	if res.FailedRate <= 0 {
		t.Errorf("失败率必须非零，外部才可能告警: failedRate=%v", res.FailedRate)
	}

	// 逐条结论也要如实：标成 delete_failed，而不是「删除前已被新产物引用，本轮保留」。
	action := ""
	for _, it := range res.Items {
		if it.ContentHash == blocked {
			action = it.Action
		}
	}
	if action != "delete_failed" {
		t.Errorf("被吞掉的那条应报 delete_failed（删除未生效），实得 %q", action)
	}
}
