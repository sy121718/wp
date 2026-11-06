package feature

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	blockcontract "go_wp/internal/module/block/contract"
	blockdto "go_wp/internal/module/block/dto"
	blockmodel "go_wp/internal/module/block/model"
	blockservice "go_wp/internal/module/block/service"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/pkg/facing"
	"go_wp/public/test/support"
)

// TestBlockBulkDeleteAllOrNothing 覆盖块批量删除：
// 跨工程同批一次事务全成、混合不存在整批拒绝、跨工程后段 DB 错误使整批回滚。
func TestBlockBulkDeleteAllOrNothing(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	ctx := context.Background()
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	svc := blockservice.NewService(blockmodel.NewBlockModel(db), projects)
	svc.SetReferenceUsageChecker(func(context.Context, string) ([]blockcontract.BlockUsage, error) {
		return nil, nil
	})
	svc.SetStalePropagator(func(context.Context, string) error { return nil })

	newProject := func(name string) string {
		t.Helper()
		pid := uuid.NewString()
		support.SeedProjectRow(t, db, pid, name)
		return pid
	}
	create := func(projectID, name string) string {
		t.Helper()
		res, err := svc.Create(ctx, &blockdto.CreateReq{ProjectID: projectID, Name: name})
		if err != nil {
			t.Fatalf("创建块失败 %q: %v", name, err)
		}
		return res.ID
	}
	exists := func(id string) bool {
		var n int64
		if err := db.Table("blocks").Where("id = ?", id).Count(&n).Error; err != nil {
			t.Fatalf("查块失败: %v", err)
		}
		return n > 0
	}

	projectA := newProject("原子删除站点 A")
	projectB := newProject("原子删除站点 B")

	t.Run("跨工程同批一次提交全成", func(t *testing.T) {
		a := create(projectA, "A-全成")
		b := create(projectB, "B-全成")
		if err := svc.BulkDelete(ctx, &blockdto.BulkDeleteReq{IDs: []string{a, b}}); err != nil {
			t.Fatalf("跨工程批量删除应整批成功：%v", err)
		}
		if exists(a) || exists(b) {
			t.Fatal("整批成功的行都该被删除")
		}
	})

	t.Run("混合不存在整批拒绝且已存在行不删", func(t *testing.T) {
		a := create(projectA, "A-混合")
		err := svc.BulkDelete(ctx, &blockdto.BulkDeleteReq{IDs: []string{a, uuid.NewString()}})
		if err == nil {
			t.Fatal("含不存在 id 必须整批拒绝")
		}
		var blocked facing.BlockedError
		if !errors.As(err, &blocked) || len(blocked.BlockedItems()) != 1 {
			t.Fatalf("应给一条结构化拒绝事实，实际 %T", err)
		}
		if !exists(a) {
			t.Fatal("整批拒绝后已存在的行不该被删")
		}
	})

	t.Run("后一个工程删除失败时前一个工程已删行也回滚", func(t *testing.T) {
		a := create(projectA, "A-回滚")
		b := create(projectB, "B-回滚")
		fn := "block_test_fail_del"
		trg := fn + "_trg"
		if err := db.Exec(`
CREATE OR REPLACE FUNCTION ` + fn + `() RETURNS trigger AS $$
BEGIN
    IF OLD.id = '` + b + `' THEN
        RAISE EXCEPTION 'injected block delete failure';
    END IF;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER ` + trg + ` BEFORE DELETE ON blocks
FOR EACH ROW EXECUTE FUNCTION ` + fn + `();`).Error; err != nil {
			t.Fatalf("装故障触发器失败: %v", err)
		}
		t.Cleanup(func() {
			_ = db.Exec(`DROP TRIGGER IF EXISTS ` + trg + ` ON blocks`).Error
			_ = db.Exec(`DROP FUNCTION IF EXISTS ` + fn + `()`).Error
		})

		err := svc.BulkDelete(ctx, &blockdto.BulkDeleteReq{IDs: []string{a, b}})
		if err == nil {
			t.Fatal("DB 错误必须整批失败")
		}
		for _, id := range []string{a, b} {
			if !exists(id) {
				t.Fatalf("整批回滚后 id=%s 不该被删", id)
			}
		}
	})
}
